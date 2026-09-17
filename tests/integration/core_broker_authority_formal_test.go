//go:build integration && !governance

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR23ResumeFormalCurrentBrokerAuthority(t *testing.T) {
	started := time.Now().UTC()
	e := newWR23FormalEnvironment(t)
	b := e.boss
	ctx := context.Background()
	a := e.broker.configuration.Authority
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("current-authority owner %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	member := call("GET", "/iam/platform/members?limit=1", nil, 200)["items"].([]any)[0].(map[string]any)
	role := call("POST", "/iam/platform/roles", map[string]any{"name": "WR23 authority fault Core creator", "permissions": []string{"tenants/create"}}, 201)
	call("POST", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role["role_id"], "expected_membership_version": member["version"]}, 204)
	create := func() (string, string, string) {
		t.Helper()
		doc := call("POST", "/admin/tenants", map[string]any{"name": "wr23-auth-" + uuid.NewString()[:12], "display_name": "WR23 authority", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "authority-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, 200)
		tenant := doc["id"].(string)
		var event, digest string
		if e.core.QueryRow(ctx, `SELECT event_id::text,encode(payload_sha256,'hex') FROM core_integration_outbox WHERE tenant_id=$1 AND subject='ani.integration.tenant.lifecycle.v1'`, tenant).Scan(&event, &digest) != nil {
			t.Fatal("actual owner lifecycle payload missing")
		}
		return tenant, event, digest
	}
	wait := func(label string, probe func() bool) {
		t.Helper()
		until := time.Now().Add(20 * time.Second)
		for time.Now().Before(until) {
			if probe() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(label)
	}
	var count int
	goodTenant, goodEvent, _ := create()
	wait("initial actual producer/receiver authority did not accept owner source", func() bool {
		return b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, a.ConsumerID, goodTenant, goodEvent).Scan(&count) == nil && count == 1
	})
	var lifecycleRoute uuid.UUID
	for _, route := range a.Routes {
		if route.Subject == "ani.integration.tenant.lifecycle.v1" {
			lifecycleRoute = route.ID
		}
	}
	results := []map[string]any{}
	for _, kind := range []string{"producer_principal", "consumer_principal", "producer_binding", "consumer_binding", "publish_grant", "receive_grant"} {
		binding := a.ConsumerBindingID
		if strings.HasPrefix(kind, "producer") || kind == "publish_grant" {
			binding = a.Routes[0].ProducerBindingID
		}
		var principal uuid.UUID
		var public string
		if b.owner.QueryRow(ctx, `SELECT principal_id,nkey_public FROM core_broker_bindings WHERE id=$1 AND environment=$2 AND trust_domain=$3`, binding, a.Environment, a.TrustDomain).Scan(&principal, &public) != nil {
			t.Fatal("missing exact own broker binding")
		}
		var version int64
		var grant data.CoreBrokerGrantChange
		switch {
		case strings.HasSuffix(kind, "principal"):
			if b.owner.QueryRow(ctx, `SELECT p.version FROM principals p JOIN workload_principals w ON w.principal_id=p.id WHERE p.id=$1 AND p.principal_type='workload' AND p.status='active' AND w.owner_type='platform' AND w.environment=$2 AND w.trust_domain=$3`, principal, a.Environment, a.TrustDomain).Scan(&version) != nil {
				t.Fatal("exact fresh provisioned platform Workload missing")
			}
		case strings.HasSuffix(kind, "binding"):
			if b.owner.QueryRow(ctx, `SELECT version FROM core_broker_bindings WHERE id=$1 AND status='active'`, binding).Scan(&version) != nil {
				t.Fatal("own active Broker binding missing")
			}
		default:
			action := "receive"
			if kind == "publish_grant" {
				action = "publish"
			}
			if b.owner.QueryRow(ctx, `SELECT id,version FROM core_broker_grants WHERE principal_id=$1 AND binding_id=$2 AND route_id=$3 AND action=$4 AND status='active'`, principal, binding, lifecycleRoute, action).Scan(&grant.ID, &version) != nil {
				t.Fatal("own exact lifecycle Grant missing")
			}
			grant.PrincipalID, grant.BindingID, grant.RouteID, grant.Action = principal, binding, lifecycleRoute, action
		}
		change := func(active bool) {
			t.Helper()
			status := "revoked"
			if active {
				status = "active"
			}
			if strings.HasSuffix(kind, "principal") {
				// This is an explicit negative state fault on the run's already approved
				// Workload. It is neither identity seeding nor a new management interface.
				principalStatus, expected := "disabled", "active"
				if active {
					principalStatus, expected = "active", "disabled"
				}
				changed, err := b.owner.Exec(ctx, `UPDATE principals p SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE p.id=$1 AND p.version=$3 AND p.status=$4 AND p.principal_type='workload' AND EXISTS(SELECT 1 FROM workload_principals w WHERE w.principal_id=p.id AND w.owner_type='platform' AND w.environment=$5 AND w.trust_domain=$6)`, principal, principalStatus, version, expected, a.Environment, a.TrustDomain)
				if err != nil || changed.RowsAffected() != 1 {
					t.Fatal("own Principal fault CAS rejected")
				}
			} else {
				m := data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Reason: "WR23 exact current authority " + kind + " fault and restoration", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: a}
				if strings.HasSuffix(kind, "binding") {
					m.Mode = "bindings"
					m.Bindings = []data.CoreBrokerBindingChange{{ID: binding, PrincipalID: principal, NKeyPublic: public, ExpectedVersion: version, Status: status}}
				} else {
					m.Mode = "grants"
					grant.ExpectedVersion = version
					grant.Status = status
					m.Grants = []data.CoreBrokerGrantChange{grant}
				}
				file := filepath.Join(b.run, "private", "authority-"+m.ID.String()+".json")
				sha := wr23PrivateJSON(t, file, m)
				wr23PrivateCommand(t, b.run, "authority-"+m.ID.String(), exec.Command(b.iam.binary, "provision-core-broker", "--manifest", file, "--approved-manifest-sha256", sha, "--environment", a.Environment, "--trust-domain", a.TrustDomain, "--dsn-file", filepath.Join(b.iam.directory, "provisioner.secret")))
			}
			recordReference(t, b.run, map[string]any{"kind": "controlled-current-authority-fault", "target_kind": kind, "principal_id": principal, "binding_id": binding, "before_version": version, "after_version": version + 1, "restored": active, "at": time.Now().UTC()})
			version++
		}
		change(false)
		tenant, event, digest := create()
		var dlqID, retainedSHA string
		var brokerSequence int64
		wait("actual owner source did not reach immutable current-authority quarantine: "+kind, func() bool {
			return b.owner.QueryRow(ctx, `SELECT id::text,broker_sequence,encode(raw_sha256,'hex') FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.lifecycle.v1' AND convert_from(raw_payload,'utf8')::jsonb->'envelope'->>'event_id'=$2 AND last_error='authority_denied'`, a.ConsumerID, event).Scan(&dlqID, &brokerSequence, &retainedSHA) == nil
		})
		if digest != retainedSHA {
			t.Fatal("quarantine did not preserve actual owner payload")
		}
		wait("native broker publication receipt missing during IAM business denial", func() bool {
			var published bool
			return e.core.QueryRow(ctx, `SELECT state='published' AND broker_sequence=$2 FROM core_integration_outbox WHERE event_id=$1`, event, brokerSequence).Scan(&published) == nil && published
		})
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, a.ConsumerID, tenant, event).Scan(&count) != nil || count != 0 {
			t.Fatal("current revoked authority committed lifecycle receipt")
		}
		change(true)
		time.Sleep(500 * time.Millisecond)
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, a.ConsumerID, tenant, event).Scan(&count) != nil || count != 0 {
			t.Fatal("restored authority automatically replayed rejected source")
		}
		freshTenant, freshEvent, _ := create()
		wait("new source did not use restored current authority: "+kind, func() bool {
			return b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, a.ConsumerID, freshTenant, freshEvent).Scan(&count) == nil && count == 1
		})
		results = append(results, map[string]any{"kind": kind, "result": "pass", "denied_event_id": event, "dlq_id": dlqID, "broker_sequence": brokerSequence, "original_payload_sha256": digest, "restoration_did_not_replay": true, "fresh_current_source_accepted": true})
	}
	raw, _ := json.MarshalIndent(map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "cases": results, "grade": "actual owner Core/outbox/NATS to formal IAM current authority denials and durable quarantine", "native_revocation": "separately verified by exact NKey ACL/reload scenarios; IAM state alone does not disconnect native clients", "dlq_replay": "not_verified; original denied sources intentionally retained, never bypassed"}, "", "  ")
	if os.WriteFile(filepath.Join(b.run, "formal-authority-results.json"), append(raw, '\n'), 0600) != nil {
		t.Fatal("write current authority proof")
	}
}
