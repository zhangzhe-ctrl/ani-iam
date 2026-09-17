//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR23FormalCoreOutboxAdministration(t *testing.T) {
	e := newWR23CoreEnvironment(t)
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	principal, err := b.iam.auth.ValidatePrincipal(ctx, &iamv1.ValidatePrincipalRequest{Credential: &iamv1.BearerCredential{Value: access}, OperationId: "getTenantOutboxDelivery", PolicyRevision: data.TargetPolicyRevision})
	if err != nil || principal.GetPrincipal().GetBoundary().GetPlatform() == nil {
		t.Fatal("formal Platform identity unavailable")
	}
	actor := principal.GetPrincipal().GetPrincipalId()
	request := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"X-ANI-Actor-User-ID": uuid.NewString(), "X-ANI-Tenant-ID": uuid.NewString()})
		if status != want {
			t.Fatalf("%s %s: %d code=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	member := func() map[string]any {
		t.Helper()
		v := request("GET", "/iam/platform/members?limit=10", nil, 200)
		items, _ := v["items"].([]any)
		if len(items) != 1 {
			t.Fatal("expected only the formal first administrator")
		}
		return items[0].(map[string]any)
	}
	bind := func(role string) {
		t.Helper()
		m := member()
		request("POST", "/iam/platform/members/"+m["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role, "expected_membership_version": m["version"]}, 204)
	}
	grant := func(name string, permissions []string) string {
		t.Helper()
		r := request("POST", "/iam/platform/roles", map[string]any{"name": name, "permissions": permissions}, 201)
		id := r["role_id"].(string)
		bind(id)
		return id
	}
	grant("Core provisioning", []string{"tenants/create"})
	created := request("POST", "/admin/tenants", map[string]any{"name": "wr23-outbox-tenant", "display_name": "Core outbox formal gate", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "wr23-outbox-admin@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, 200)
	tenant := created["id"].(string)
	var source int64
	var event string
	var original []byte
	if err := e.core.QueryRow(ctx, `SELECT source_sequence,event_id::text,payload FROM core_integration_outbox WHERE tenant_id=$1 AND subject='ani.integration.tenant.iam-bootstrap.v1'`, tenant).Scan(&source, &event, &original); err != nil {
		t.Fatal("formal Core Bootstrap outbox missing")
	}
	base := fmt.Sprintf("/admin/tenant-outbox/%d", source)
	command := map[string]any{"event_id": event, "expected_attempt": 20, "reason": "ISOLATED_DEPENDENCY_RESTORED", "idempotency_key": uuid.NewString()}
	t.Run("dedicated_read_and_recovery_permissions", func(t *testing.T) {
		request("GET", base, nil, 403)
		request("POST", base+"/recover", command, 403)
		grant("Core outbox reader", []string{"tenant-outbox/read"})
		doc := request("GET", base, nil, 200)
		if doc["event_id"] != event || doc["tenant_id"] != tenant || doc["state"] != "pending" || doc["attempt_count"] != float64(0) {
			t.Fatal("formal redacted metadata mismatch")
		}
		for _, key := range []string{"payload", "admin_email", "normalized_email", "credential", "lease_id"} {
			if _, ok := doc[key]; ok {
				t.Fatal("sensitive payload in status response")
			}
		}
		request("POST", base+"/recover", command, 403)
	})
	if t.Failed() {
		return
	}
	recoveryRole := grant("Core outbox recovery", []string{"tenant-outbox/recover"})
	t.Run("pending_delivery_cannot_be_recovered", func(t *testing.T) { request("POST", base+"/recover", command, 409) })

	// Only this newly provisioned task message is injected into a coherent
	// failed-delivery state. These rows do not claim real NATS attempts or time.
	tx, err := e.core.Begin(ctx)
	if err != nil {
		t.Fatal("fault setup begin")
	}
	defer tx.Rollback(ctx)
	for n := 1; n <= 20; n++ {
		lease := uuid.NewString()
		if _, err = tx.Exec(ctx, `INSERT INTO core_outbox_attempts(source_sequence,attempt_number,lease_id,worker_id,started_at,lease_until) VALUES($1,$2,$3,$4,clock_timestamp()-interval '31 seconds',clock_timestamp()-interval '1 second')`, source, n, lease, uuid.NewString()); err != nil {
			t.Fatal("isolated attempt metadata injection")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO core_outbox_outcomes(source_sequence,attempt_number,lease_id,outcome,error_code,finished_at) VALUES($1,$2,$3,'failed','broker_unavailable',clock_timestamp())`, source, n, lease); err != nil {
			t.Fatal("isolated outcome metadata injection")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE core_integration_outbox SET state='attention_required',attempt_count=20,last_error='broker_unavailable' WHERE source_sequence=$1 AND event_id=$2 AND state='pending' AND attempt_count=0`, source, event); err != nil {
		t.Fatal("isolated attention injection")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("commit isolated attention metadata")
	}
	sum := sha256.Sum256(original)
	recordReference(t, b.run, map[string]any{"kind": "wr23-Core-outbox-attention-fault", "source_sequence": source, "event_id": event, "tenant_id": tenant, "payload_sha256": hex.EncodeToString(sum[:]), "test_only_metadata_injection": true, "real_NATS_attempts": false})
	metrics := func() string {
		t.Helper()
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get(e.adminURL + "/metrics")
		if err != nil {
			t.Fatal("formal runtime metrics unavailable")
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
		if err != nil || response.StatusCode != 200 {
			t.Fatal("formal metrics response")
		}
		return string(raw)
	}
	waitMetric := func(value string) {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			m := metrics()
			if strings.Contains(m, "ani_core_tenant_outbox_observation_success 1") && strings.Contains(m, "ani_core_tenant_outbox_attention_required "+value+"\n") {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatal("formal outbox observation did not converge")
	}
	t.Run("formal_process_alert_and_metric", func(t *testing.T) {
		waitMetric("1")
		files, err := filepath.Glob(filepath.Join(b.run, "private/wr23-core-gateway-*.log"))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal("read own formal log")
			}
			found = found || bytes.Contains(raw, []byte("CORE_TENANT_OUTBOX_ATTENTION_REQUIRED"))
		}
		if !found {
			t.Fatal("formal attention alert missing")
		}
		recordReference(t, b.run, map[string]any{"kind": "wr23-Core-outbox-alert", "code": "CORE_TENANT_OUTBOX_ATTENTION_REQUIRED", "count": 1, "formal_process_log": true, "real_metrics": true, "external_alert_routing": false})
	})
	t.Run("owner_receipt_write_failure_rolls_back", func(t *testing.T) {
		if _, err := e.core.Exec(ctx, `REVOKE INSERT ON core_outbox_recoveries FROM wr23_core_runtime`); err != nil {
			t.Fatal("inject runtime write failure")
		}
		defer func() {
			if _, err := e.core.Exec(ctx, `GRANT INSERT ON core_outbox_recoveries TO wr23_core_runtime`); err != nil {
				t.Fatal("restore own runtime privilege")
			}
		}()
		request("POST", base+"/recover", command, 503)
		var count int
		if err := e.core.QueryRow(ctx, `SELECT count(*) FROM core_outbox_recoveries WHERE source_sequence=$1`, source).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial recovery receipt")
		}
		if v := request("GET", base, nil, 200); v["state"] != "attention_required" {
			t.Fatal("failed recovery changed state")
		}
	})
	var receipt map[string]any
	t.Run("concurrent_recovery_preserves_payload_and_linked_history", func(t *testing.T) {
		type result struct {
			status int
			doc    map[string]any
		}
		ch := make(chan result, 4)
		var wg sync.WaitGroup
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, doc, _ := b.request(t, browser, "POST", base+"/recover", access, command, nil)
				ch <- result{status, doc}
			}()
		}
		wg.Wait()
		close(ch)
		seen := 0
		for r := range ch {
			seen++
			if r.status != 200 {
				t.Fatalf("concurrent recovery status %d", r.status)
			}
			if receipt == nil {
				receipt = r.doc
			} else if !reflect.DeepEqual(receipt, r.doc) {
				t.Fatal("multiple recovery receipts")
			}
		}
		if seen != 4 {
			t.Fatal("incomplete concurrent requests")
		}
		var count, previous, attempts int
		var savedActor string
		var payload []byte
		if err := e.core.QueryRow(ctx, `SELECT count(*) FROM core_outbox_recoveries WHERE source_sequence=$1`, source).Scan(&count); err != nil || count != 1 {
			t.Fatal("duplicate recovery effects")
		}
		if err := e.core.QueryRow(ctx, `SELECT actor_id::text,previous_attempt FROM core_outbox_recoveries WHERE source_sequence=$1`, source).Scan(&savedActor, &previous); err != nil || savedActor != actor || previous != 20 {
			t.Fatal("recovery audit lost actor or attempt link")
		}
		if err := e.core.QueryRow(ctx, `SELECT count(*) FROM core_outbox_attempts WHERE source_sequence=$1`, source).Scan(&attempts); err != nil || attempts != 20 {
			t.Fatal("attempt history changed")
		}
		if err := e.core.QueryRow(ctx, `SELECT payload FROM core_integration_outbox WHERE source_sequence=$1`, source).Scan(&payload); err != nil || !bytes.Equal(payload, original) {
			t.Fatal("recovery changed payload")
		}
		conflict := map[string]any{}
		for k, v := range command {
			conflict[k] = v
		}
		conflict["reason"] = "DIFFERENT_INTENT"
		request("POST", base+"/recover", conflict, 409)
		view := request("GET", base, nil, 200)
		if view["state"] != "pending" || view["cycle_start_attempt"] != float64(20) || view["attempt_count"] != float64(20) {
			t.Fatal("recovery reset history")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("revocation_precedes_cached_recovery_receipt", func(t *testing.T) {
		m := member()
		path := fmt.Sprintf("/iam/platform/members/%s/role-bindings/%s?expected_membership_version=%.0f", m["membership_id"], recoveryRole, m["version"])
		request("DELETE", path, nil, 204)
		request("POST", base+"/recover", command, 403)
		request("GET", base, nil, 200)
		bind(recoveryRole)
		again := request("POST", base+"/recover", command, 200)
		if !reflect.DeepEqual(again, receipt) {
			t.Fatal("regrant changed original receipt")
		}
	})
	t.Run("cleared_alert_and_restart_receipt", func(t *testing.T) {
		waitMetric("0")
		e.gateway.stop()
		e.gateway = e.startGateway()
		again := request("POST", base+"/recover", command, 200)
		if !reflect.DeepEqual(again, receipt) {
			t.Fatal("formal restart lost receipt")
		}
		request("GET", "/admin/tenant-outbox/9223372036854775807", nil, 404)
		request("GET", "/admin/tenant-outbox/0", nil, 400)
		bad := map[string]any{}
		for k, v := range command {
			bad[k] = v
		}
		bad["payload"] = map[string]any{}
		request("POST", base+"/recover", bad, 400)
		recordReference(t, b.run, map[string]any{"kind": "wr23-Core-outbox-recovery", "source_sequence": source, "event_id": event, "recovery_id": receipt["recovery_id"], "actor_id": actor, "formal_recovery_permission": true, "current_revocation_before_receipt": true, "restart_receipt": true, "payload_unchanged": true, "NATS_delivery": false})
	})
}
