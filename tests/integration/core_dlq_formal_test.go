//go:build integration && !governance

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var wr23DLQReviewRPCs = []string{"ListCoreDLQEntries", "GetCoreDLQEntry", "ReplayCoreDLQEntry"}

// Only this isolated runtime installs the three candidates under review. The
// source registration and every other enabled/disabled target remain unchanged.
func wr23DLQReviewRegistry(t *testing.T) string {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr23" {
		t.Fatal("WR23 review required")
	}
	source := filepath.Join(findRepositoryRoot(t), "registrations/workload-targets.v1.json")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("review registry decode")
	}
	selected := map[string]bool{}
	for _, rpc := range wr23DLQReviewRPCs {
		selected["/iam.v1.IAMAdminService/"+rpc] = false
	}
	for _, item := range doc["targets"].([]any) {
		row := item.(map[string]any)
		op, _ := row["operation"].(string)
		if _, ok := selected[op]; ok {
			if row["audience"] != "ani-iam" || row["rpc"] != op || row["mechanism"] != "direct" || row["grant_scope"] != "iam_ingress" {
				t.Fatal("DLQ target changed authority boundary")
			}
			row["enabled"] = true
			selected[op] = true
		}
	}
	for _, found := range selected {
		if !found {
			t.Fatal("DLQ exact target missing")
		}
	}
	file := filepath.Join(run, "private", "dlq-review-registry.json")
	digest := wr23PrivateJSON(t, file, doc)
	t.Setenv("WR23_DLQ_REVIEW_REGISTRY", file)
	recordReference(t, run, map[string]any{"kind": "dlq-exact-three-target-review", "source_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "review_registry_sha256": digest, "rpcs": wr23DLQReviewRPCs, "role_grants_automatic": false})
	return digest
}

func TestWR23ResumeFormalDLQ(t *testing.T) {
	started := time.Now().UTC()
	review := wr23DLQReviewRegistry(t)
	e := newWR23FormalEnvironment(t, func(_ string, _ *x509.Certificate, _ ed25519.PrivateKey, _ *conf.Bootstrap, m *biz.WorkloadBootstrapManifest) {
		if m.Workloads[0].Name != "gateway" {
			t.Fatal("exact Gateway review caller missing")
		}
		for _, rpc := range wr23DLQReviewRPCs {
			m.Workloads[0].Grants = append(m.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + rpc})
		}
	})
	b := e.boss
	ctx := context.Background()
	a := e.broker.configuration.Authority
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	call := func(method, path string, body any, key string, want int) map[string]any {
		t.Helper()
		if key == "" {
			key = uuid.NewString()
		}
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"Idempotency-Key": key})
		if status != want {
			t.Fatalf("DLQ %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	wait := func(label string, probe func() bool) {
		t.Helper()
		until := time.Now().Add(25 * time.Second)
		for time.Now().Before(until) {
			if probe() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(label)
	}
	bind := func(permission string) string {
		t.Helper()
		m := call("GET", "/iam/platform/members?limit=1", nil, "", 200)["items"].([]any)[0].(map[string]any)
		role := call("POST", "/iam/platform/roles", map[string]any{"name": "WR23 " + permission, "permissions": []string{permission}}, "", 201)["role_id"].(string)
		call("POST", "/iam/platform/members/"+m["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role, "expected_membership_version": m["version"]}, "", 204)
		return role
	}
	bind("tenants/create")
	var binding data.CoreBrokerBindingChange
	if b.owner.QueryRow(ctx, `SELECT id,principal_id,nkey_public,version FROM core_broker_bindings WHERE id=$1`, a.ConsumerBindingID).Scan(&binding.ID, &binding.PrincipalID, &binding.NKeyPublic, &binding.ExpectedVersion) != nil {
		t.Fatal("current exact Broker consumer missing")
	}
	change := func(state string) {
		t.Helper()
		binding.Status = state
		m := data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Mode: "bindings", Reason: "WR23 exact DLQ consumer denial and authorized restoration", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: a, Bindings: []data.CoreBrokerBindingChange{binding}}
		file := filepath.Join(b.run, "private", "dlq-binding-"+m.ID.String()+".json")
		sha := wr23PrivateJSON(t, file, m)
		wr23PrivateCommand(t, b.run, "dlq-binding-"+m.ID.String(), exec.Command(b.iam.binary, "provision-core-broker", "--manifest", file, "--approved-manifest-sha256", sha, "--environment", a.Environment, "--trust-domain", a.TrustDomain, "--dsn-file", filepath.Join(b.iam.directory, "provisioner.secret")))
		binding.ExpectedVersion++
	}
	change("revoked")
	created := call("POST", "/admin/tenants", map[string]any{"name": "wr23-dlq-" + uuid.NewString()[:12], "display_name": "WR23 DLQ", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "dlq-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, "", 200)
	tenant, operation := created["id"].(string), created["bootstrap_operation_id"].(string)
	var lifecycleID, bootstrapID, heartbeatID string
	wait("actual Core lifecycle and Bootstrap did not enter immutable DLQ", func() bool {
		return b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.lifecycle.v1' AND convert_from(raw_payload,'utf8')::jsonb->'envelope'->>'tenant_id'=$2`, a.ConsumerID, tenant).Scan(&lifecycleID) == nil && b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.iam-bootstrap.v1' AND convert_from(raw_payload,'utf8')::jsonb->'envelope'->>'tenant_id'=$2`, a.ConsumerID, tenant).Scan(&bootstrapID) == nil
	})
	wait("actual owner heartbeat did not enter immutable DLQ", func() bool {
		return b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.lifecycle-heartbeat.v1' ORDER BY broker_sequence LIMIT 1`, a.ConsumerID).Scan(&heartbeatID) == nil
	})
	base := "/iam/platform/core-dlq/" + a.ConsumerID.String() + "/entries"
	call("GET", base, nil, "", 403)
	call("GET", base+"/"+lifecycleID, nil, "", 403)
	bind("iam.dlq/read")
	page := call("GET", base+"?limit=1", nil, "", 200)
	entries := page["entries"].([]any)
	if len(entries) != 1 || page["next_cursor"] == "" {
		t.Fatal("bounded DLQ pagination missing")
	}
	if _, ok := entries[0].(map[string]any)["raw_payload"]; ok {
		t.Fatal("list exposed original bytes")
	}
	call("GET", base+"/"+lifecycleID+"?cursor="+page["next_cursor"].(string), nil, "", 400)
	call("GET", "/iam/platform/core-dlq/"+mustV7(t).String()+"/entries/"+lifecycleID, nil, "", 404)
	inspect := func(id string) map[string]any { return call("GET", base+"/"+id, nil, "", 200) }
	command := func(id string) map[string]any {
		t.Helper()
		entry := inspect(id)["entry"].(map[string]any)
		return map[string]any{"expected_raw_sha256": entry["raw_sha256"], "expected_attempt": entry["last_attempt"], "reason_code": "WR23_DLQ_RECOVERY"}
	}
	view := inspect(lifecycleID)
	original, err := base64.StdEncoding.DecodeString(view["raw_payload"].(string))
	if err != nil {
		t.Fatal("DLQ original base64")
	}
	sum := sha256.Sum256(original)
	if hex.EncodeToString(sum[:]) != view["entry"].(map[string]any)["raw_sha256"] {
		t.Fatal("DLQ original SHA mismatch")
	}
	var ownerRaw []byte
	if e.core.QueryRow(ctx, `SELECT payload FROM core_integration_outbox WHERE tenant_id=$1 AND subject='ani.integration.tenant.lifecycle.v1'`, tenant).Scan(&ownerRaw) != nil || !bytes.Equal(ownerRaw, original) {
		t.Fatal("DLQ did not retain actual Core original bytes")
	}
	replayPath := base + "/" + lifecycleID + "/replay"
	call("POST", replayPath, command(lifecycleID), "", 403)
	replayRole := bind("iam.dlq/replay")
	call("POST", replayPath, command(lifecycleID), "", 503)
	if inspect(lifecycleID)["entry"].(map[string]any)["last_attempt"] != float64(1) {
		t.Fatal("current Broker denial lost linked failed attempt")
	}
	change("active")
	var count int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2`, a.ConsumerID, tenant).Scan(&count) != nil || count != 0 {
		t.Fatal("authority restoration automatically replayed rejected source")
	}
	bad := command(lifecycleID)
	bad["expected_raw_sha256"] = strings.Repeat("c", 64)
	call("POST", replayPath, bad, "", 409)
	stale := command(lifecycleID)
	stale["expected_attempt"] = 0
	call("POST", replayPath, stale, "", 409)
	if _, err = b.owner.Exec(ctx, `CREATE FUNCTION wr23_dlq_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.replayCoreIAMDLQEntry' AND NEW.result='succeeded' THEN RAISE EXCEPTION 'WR23 own DLQ audit rollback' USING ERRCODE='23514'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr23_dlq_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr23_dlq_audit_fault()`); err != nil {
		t.Fatal("install own replay transaction fault")
	}
	call("POST", replayPath, command(lifecycleID), "", 503)
	if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr23_dlq_audit_fault ON iam_audit_events; DROP FUNCTION wr23_dlq_audit_fault()`); err != nil {
		t.Fatal("remove own replay audit fault")
	}
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2`, a.ConsumerID, tenant).Scan(&count) != nil || count != 0 {
		t.Fatal("failed replay committed partial receiver effects")
	}
	current := command(lifecycleID)
	key := uuid.NewString()
	type reply struct {
		status int
		doc    map[string]any
	}
	results := make(chan reply, 2)
	for i := 0; i < 2; i++ {
		go func() {
			status, doc, _ := b.request(t, browser, "POST", replayPath, access, current, map[string]string{"Idempotency-Key": key})
			results <- reply{status, doc}
		}()
	}
	first, second := <-results, <-results
	if first.status != 200 || second.status != 200 || first.doc["attempt_id"] == "" || first.doc["attempt_id"] != second.doc["attempt_id"] || first.doc["audit_id"] != second.doc["audit_id"] || first.doc["replayed"] == second.doc["replayed"] {
		t.Fatal("concurrent same-key replay was not one committed receiver result")
	}
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2`, a.ConsumerID, tenant).Scan(&count) != nil || count != 1 {
		t.Fatal("replay duplicated lifecycle receipt")
	}
	var linked int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_dlq_attempts d JOIN iam_audit_events e ON e.event_id=d.audit_event_id WHERE d.consumer_id=$1 AND d.entry_id=$2 AND d.id=$3 AND d.raw_sha256=sha256($4::bytea) AND e.action='iam.platform.replayCoreIAMDLQEntry' AND e.target_version=d.attempt_number AND e.reason=d.reason_code AND e.actor_id=d.actor_id AND e.result='succeeded'`, a.ConsumerID, lifecycleID, first.doc["attempt_id"], original).Scan(&linked) != nil || linked != 1 {
		t.Fatal("successful replay lost immutable attempt/audit association")
	}
	var failures int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_dlq_attempts WHERE consumer_id=$1 AND entry_id=$2 AND outcome='failed'`, a.ConsumerID, lifecycleID).Scan(&failures) != nil || failures != 4 {
		t.Fatal("failed requests were swallowed by business rollback")
	}
	again := call("POST", replayPath, current, key, 200)
	if again["attempt_id"] != first.doc["attempt_id"] || again["replayed"] != true {
		t.Fatal("committed response retry repeated effects")
	}
	// Dedicated permission removal invalidates cached receipt reuse immediately.
	m := call("GET", "/iam/platform/members?limit=1", nil, "", 200)["items"].([]any)[0].(map[string]any)
	call("DELETE", "/iam/platform/members/"+m["membership_id"].(string)+"/role-bindings/"+replayRole+"?expected_membership_version="+fmt.Sprint(m["version"]), nil, "", 204)
	call("POST", replayPath, current, key, 403)
	m = call("GET", "/iam/platform/members?limit=1", nil, "", 200)["items"].([]any)[0].(map[string]any)
	call("POST", "/iam/platform/members/"+m["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": replayRole, "expected_membership_version": m["version"]}, "", 204)
	bootstrap := call("POST", base+"/"+bootstrapID+"/replay", command(bootstrapID), "", 200)
	if bootstrap["operation_id"] != operation || bootstrap["tenant_id"] != tenant || bootstrap["outcome"] != "received" {
		t.Fatal("Bootstrap replay changed original operation or reported completion")
	}
	wait("existing Bootstrap dispatcher and worker did not continue the re-received original", func() bool {
		return b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2 AND status='waiting_for_principal_verification'`, tenant, operation).Scan(&count) == nil && count == 1
	})
	// Stop new owner facts by holding only this run's actual journal lock. A
	// heartbeat kept in DLQ must retain owner time after a real 31-second delay.
	held, err := e.core.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(ctx)
	if _, err = held.Exec(ctx, `SELECT committed_sequence FROM core_integration_journal WHERE singleton=true FOR UPDATE`); err != nil {
		t.Fatal("hold own owner fact source")
	}
	heldAt := time.Now().UTC()
	time.Sleep(31 * time.Second)
	heartbeat := call("POST", base+"/"+heartbeatID+"/replay", command(heartbeatID), "", 200)
	var occurred, progress time.Time
	if b.owner.QueryRow(ctx, `SELECT occurred_at FROM core_integration_receipts WHERE producer=$1 AND event_id=$2`, a.Producer, heartbeat["event_id"]).Scan(&occurred) != nil || !occurred.Before(heldAt) {
		t.Fatal("old heartbeat replay rewrote owner time")
	}
	if b.owner.QueryRow(ctx, `SELECT progress_at FROM core_lifecycle_pipelines WHERE producer=$1`, a.Producer).Scan(&progress) != nil || progress.After(heldAt) {
		t.Fatal("old heartbeat refreshed pipeline to replay time")
	}
	if err = held.Rollback(ctx); err != nil {
		t.Fatal("release own owner journal hold")
	}
	for _, table := range []string{"core_broker_dlq", "core_broker_dlq_context", "core_broker_dlq_attempts"} {
		if _, err = e.database.runtimePool.Exec(ctx, "DELETE FROM "+table+" WHERE consumer_id=$1", a.ConsumerID); err == nil {
			t.Fatal("runtime deleted immutable DLQ evidence")
		}
	}
	extraCases := wr23DLQFaultMatrix(t, e, browser, &access, base, tenant, wr23DLQTestAPI{call: call, command: command, inspect: inspect, change: change, wait: wait})
	final := inspect(lifecycleID)
	raw, _ := base64.StdEncoding.DecodeString(final["raw_payload"].(string))
	if !bytes.Equal(raw, original) {
		t.Fatal("inspection/replay changed original")
	}
	proof := map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "review_registry_sha256": review, "tenant_id": tenant, "operation_id": operation, "lifecycle_entry_id": lifecycleID, "bootstrap_entry_id": bootstrapID, "heartbeat_entry_id": heartbeatID, "grade": "actual Core/outbox/NATS originals; BOSS Gateway IAM synchronous receiver; restricted PostgreSQL audit and existing Bootstrap worker", "cases": []string{"dedicated_read_and_replay_permissions", "current_broker_denial", "immutable_original", "pagination_scope", "source_and_attempt_preconditions", "business_audit_rollback_with_failed_attempt", "concurrent_same_key_one_receipt", "current_human_denies_cached_receipt", "existing_bootstrap_worker", "old_heartbeat_owner_time", "runtime_immutable_relations"}, "remaining": []string{}, "complete_dlq_gate": true, "additional_cases": extraCases}
	output, _ := json.MarshalIndent(proof, "", "  ")
	if os.WriteFile(filepath.Join(b.run, "formal-dlq-results.json"), append(output, '\n'), 0644) != nil {
		t.Fatal("write public DLQ result")
	}
}
