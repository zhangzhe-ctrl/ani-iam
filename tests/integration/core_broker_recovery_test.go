//go:build integration && !governance

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

var wr23BootstrapReviewRPCs = []string{"GetTenantBootstrap", "ReissueTenantBootstrapInvitation", "RetryTenantBootstrapJob"}

func wr23ReviewDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read exact review registry")
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil {
		t.Fatal("decode exact review registry")
	}
	return d
}
func TestWR23ResumeFormalBootstrapRecovery(t *testing.T) {
	started := time.Now().UTC()
	run, goal := isolatedRun(t)
	if goal != "wr23" {
		t.Fatal("WR23 required")
	}
	registry := wr32Registry(t)
	for _, rpc := range wr23BootstrapReviewRPCs {
		if target, ok := registry.Lookup("ani-iam", "/iam.v1.IAMAdminService/"+rpc); !ok || !target.Enabled {
			t.Fatal("reviewed existing Bootstrap target disabled")
		}
	}
	digest := registry.Digest()
	e := newWR23FormalEnvironment(t)
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	call := func(method, path string, body any, key string, want int) map[string]any {
		t.Helper()
		if key == "" {
			key = uuid.NewString()
		}
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"Idempotency-Key": key})
		if status != want {
			t.Fatalf("formal recovery %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	bind := func(permission string) string {
		t.Helper()
		member := call("GET", "/iam/platform/members?limit=1", nil, "", 200)["items"].([]any)[0].(map[string]any)
		role := call("POST", "/iam/platform/roles", map[string]any{"name": "WR23 " + permission, "permissions": []string{permission}}, "", 201)["role_id"].(string)
		call("POST", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role, "expected_membership_version": member["version"]}, "", 204)
		return role
	}
	bind("tenants/create")
	// Fail a worker transaction at its durable result boundary, after the real
	// broker receiver commits. All partial Invitation/Access effects roll back.
	if _, err := b.owner.Exec(ctx, `CREATE FUNCTION wr23_bootstrap_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'WR23 isolated worker persistence fault' USING ERRCODE='23514'; END $$; CREATE TRIGGER wr23_bootstrap_fault BEFORE INSERT ON core_bootstrap_worker_results FOR EACH ROW EXECUTE FUNCTION wr23_bootstrap_fault()`); err != nil {
		t.Fatal("install own bounded worker persistence fault")
	}
	created := call("POST", "/admin/tenants", map[string]any{"name": "wr23-recovery-" + mustV7(t).String()[:12], "display_name": "WR23 recovery", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "recovery-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, "", 200)
	tenant, operation := created["id"].(string), created["bootstrap_operation_id"].(string)
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
	var attempt int32
	wait("real worker did not reach injected transaction failure", func() bool {
		return b.owner.QueryRow(ctx, `SELECT attempt_count FROM core_bootstrap_jobs WHERE tenant_id=$1 AND operation_id=$2 AND kind='initialize' AND state='pending' AND attempt_count>0`, tenant, operation).Scan(&attempt) == nil
	})
	var grant data.CoreBrokerGrantChange
	a := e.broker.configuration.Authority
	for _, route := range a.Routes {
		if route.Subject == "ani.integration.tenant.iam-bootstrap.v1" {
			grant.RouteID = route.ID
		}
	}
	if b.owner.QueryRow(ctx, `SELECT id,principal_id,binding_id,version FROM core_broker_grants WHERE route_id=$1 AND action='execute'`, grant.RouteID).Scan(&grant.ID, &grant.PrincipalID, &grant.BindingID, &grant.ExpectedVersion) != nil {
		t.Fatal("exact execution Grant missing")
	}
	grant.Action = "execute"
	change := func(status string) {
		t.Helper()
		grant.Status = status
		m := data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Mode: "grants", Reason: "WR23 controlled current execution authority fault and recovery", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: a, Grants: []data.CoreBrokerGrantChange{grant}}
		file := filepath.Join(run, "private", "recovery-grant-"+m.ID.String()+".json")
		sha := wr23PrivateJSON(t, file, m)
		wr23PrivateCommand(t, run, "recovery-grant-"+m.ID.String(), exec.Command(b.iam.binary, "provision-core-broker", "--manifest", file, "--approved-manifest-sha256", sha, "--environment", a.Environment, "--trust-domain", a.TrustDomain, "--dsn-file", filepath.Join(b.iam.directory, "provisioner.secret")))
		grant.ExpectedVersion++
	}
	change("revoked")
	if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr23_bootstrap_fault ON core_bootstrap_worker_results; DROP FUNCTION wr23_bootstrap_fault()`); err != nil {
		t.Fatal("remove own worker persistence fault")
	}
	wait("revoked execution did not quarantine original job", func() bool {
		return b.owner.QueryRow(ctx, `SELECT attempt_count FROM core_bootstrap_jobs WHERE tenant_id=$1 AND operation_id=$2 AND kind='initialize' AND state='attention_required' AND last_error='authority_unavailable'`, tenant, operation).Scan(&attempt) == nil
	})
	base := "/iam/platform/tenant-bootstraps/" + operation
	retry := map[string]any{"kind": "initialize", "generation": 1, "expected_attempt": attempt, "expected_version": 1, "reason_code": "WR23_EXECUTION_RECOVERY"}
	reissue := map[string]any{"expected_version": 2, "reason_code": "WR23_SAME_IDENTITY_REISSUE"}
	call("GET", base, nil, "", 403)
	call("POST", base+"/retry-job", retry, "", 403)
	call("POST", base+"/reissue-invitation", reissue, "", 403)
	bind("iam.tenant-bootstrap/read")
	view := call("GET", base, nil, "", 200)
	if view["operation_id"] != operation || view["tenant_id"] != tenant || view["status"] != "pending" {
		t.Fatal("Bootstrap read changed original identity")
	}
	bind("iam.tenant-bootstrap/retry")
	call("POST", base+"/retry-job", retry, "", 503)
	change("active")
	time.Sleep(2 * time.Second)
	var count int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&count) != nil || count != 0 {
		t.Fatal("restored Grant automatically replayed failed intent")
	}
	retryKey := uuid.NewString()
	call("POST", base+"/retry-job", retry, retryKey, 200)
	wait("audited original job retry did not create Invitation", func() bool {
		return b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&count) == nil && count == 1
	})
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_broker_approvals a JOIN core_bootstrap_job_recoveries r ON r.tenant_id=a.tenant_id AND r.id=a.recovery_id WHERE a.tenant_id=$1 AND a.operation_id=$2 AND a.kind='initialize' AND a.generation=1 AND r.reason_code='WR23_EXECUTION_RECOVERY'`, tenant, operation).Scan(&count) != nil || count != 1 {
		t.Fatal("retry lacks original-source linked authority approval")
	}
	first := call("GET", base, nil, "", 200)
	invitation := first["invitation"].(map[string]any)
	var intendedEmail, sourceFingerprint string
	if b.owner.QueryRow(ctx, `SELECT intended_email,payload_fingerprint FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2`, tenant, operation).Scan(&intendedEmail, &sourceFingerprint) != nil {
		t.Fatal("read original intended identity")
	}
	reissue["expected_version"] = first["version"]
	call("POST", base+"/reissue-invitation", reissue, "", 403)
	role := bind("iam.tenant-bootstrap/reissue")
	key := uuid.NewString()
	next := call("POST", base+"/reissue-invitation", reissue, key, 200)
	again := call("POST", base+"/reissue-invitation", reissue, key, 200)
	if !reflect.DeepEqual(next, again) {
		t.Fatal("same reissue key changed receipt")
	}
	current := next["invitation"].(map[string]any)
	if current["invitation_id"] == nil || current["normalized_email_hint"] == nil || current["invitation_id"] != invitation["invitation_id"] || current["normalized_email_hint"] != invitation["normalized_email_hint"] {
		t.Fatal("reissue changed intended identity")
	}
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_bootstrap_operations o JOIN tenant_invitations i ON i.tenant_id=o.tenant_id AND i.bootstrap_operation_id=o.id WHERE o.tenant_id=$1 AND o.id=$2 AND o.intended_email=$3 AND i.normalized_email=$3 AND o.payload_fingerprint=$4 AND i.id=$5`, tenant, operation, intendedEmail, sourceFingerprint, current["invitation_id"]).Scan(&count) != nil || count != 1 {
		t.Fatal("reissue changed immutable intended identity")
	}
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_broker_approvals a JOIN iam_audit_events e ON e.tenant_id=a.tenant_id AND e.event_id=a.effect_audit_id WHERE a.tenant_id=$1 AND a.operation_id=$2 AND a.kind='expire' AND a.generation=2 AND e.reason='WR23_SAME_IDENTITY_REISSUE'`, tenant, operation).Scan(&count) != nil || count != 1 {
		t.Fatal("same-identity reissue lacks linked immutable audit")
	}
	member := call("GET", "/iam/platform/members?limit=1", nil, "", 200)["items"].([]any)[0].(map[string]any)
	call("DELETE", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings/"+role+"?expected_membership_version="+fmt.Sprint(member["version"]), nil, "", 204)
	call("POST", base+"/reissue-invitation", reissue, key, 403)
	if _, err := e.database.runtimePool.Exec(ctx, `UPDATE core_bootstrap_broker_approvals SET reason_code='REWRITTEN' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("runtime rewrote authority approval")
	}
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE boundary='platform' AND target_id=$1 AND result='succeeded' AND action IN ('iam.platform.getTenantIAMBootstrap','iam.platform.retryTenantIAMBootstrapJob','iam.platform.reissueTenantIAMBootstrapInvitation')`, operation).Scan(&count) != nil || count != 4 {
		t.Fatal("three formal Bootstrap RPCs lack exact Platform audits")
	}
	raw, _ := json.MarshalIndent(map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "registry_sha256": digest, "reviewed_rpcs": wr23BootstrapReviewRPCs, "tenant_id": tenant, "operation_id": operation, "current_grant_restore_did_not_replay": true, "original_job_retry": true, "same_identity_reissue": true, "revoked_human_denies_receipt_reuse": true, "production_registration": "enabled_after_individual_formal_review", "grade": "formal Gateway IAM RPCs, actual Core NATS receipt and worker with immutable current-authority recovery"}, "", "  ")
	if os.WriteFile(filepath.Join(run, "formal-bootstrap-recovery-results.json"), append(raw, '\n'), 0600) != nil {
		t.Fatal("write recovery result")
	}
}
