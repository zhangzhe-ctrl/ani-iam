//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWR22FormalTenantAdminRecovery(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	first := b.browser(t)
	location, state := b.beginOIDC(t, first, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("recovery BOSS OIDC state prerequisite")
	}
	status, _, _ := b.callback(t, first, code, state)
	if status != 303 {
		t.Fatal("recovery real first admin prerequisite")
	}
	status, body, _ := b.request(t, first, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("recovery BOSS refresh prerequisite")
	}
	firstToken := body["access_token"].(string)
	var firstMember, firstHuman, firstSession string
	if b.owner.QueryRow(ctx, `SELECT m.id,m.principal_id,s.id FROM platform_memberships m JOIN sessions s ON s.principal_id=m.principal_id WHERE s.audience='boss'`).Scan(&firstMember, &firstHuman, &firstSession) != nil {
		t.Fatal("recovery current BOSS graph missing")
	}
	const base = "/iam/platform/restore-tenant-admin-requests"
	tenant, target := referenceTenants[0], b.humans[1]
	fingerprint := func(tenant, target uuid.UUID, reason string) string {
		return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join([]string{"ani.iam.restore-tenant-admin/v1", tenant.String(), target.String(), reason}, "\n"))))
	}
	payload := func(tenant, target uuid.UUID) map[string]any {
		return map[string]any{"tenant_id": tenant.String(), "target_principal_id": target.String(), "reason_code": "ADMIN_LOST", "payload_hash": fingerprint(tenant, target, "ADMIN_LOST")}
	}
	call := func(t *testing.T, c *http.Client, token, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		status, body, response := b.request(t, c, method, path, token, payload, headers)
		if status != want {
			t.Fatalf("recovery response status=%d reason=%v want=%d", status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("recovery response cacheable")
		}
		return body
	}
	t.Run("ordinary_platform_admin_has_no_implicit_recovery", func(t *testing.T) {
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 403)
	})
	// All identities and relation prerequisites below belong to this isolated run.
	// Invitation acceptance is a separate gate; this second Platform Membership is
	// explicitly SQL-seeded, then authority is assigned through the formal API.
	otherMember := mustV7(t)
	other := b.browser(t)
	if _, err := b.owner.Exec(ctx, `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, otherMember, b.humans[0]); err != nil {
		t.Fatal("own second Platform Membership prerequisite")
	}
	role := call(t, first, firstToken, "POST", "/iam/platform/roles", uuid.NewString(), map[string]any{"name": "WR22 dedicated administrator recovery", "permissions": []string{"iam.restore-tenant-admin/request", "iam.restore-tenant-admin/approve", "iam.restore-tenant-admin/execute"}}, 201)["role_id"].(string)
	for _, member := range []string{firstMember, otherMember.String()} {
		call(t, first, firstToken, "POST", "/iam/platform/members/"+member+"/role-bindings", uuid.NewString(), map[string]any{"role_id": role, "expected_membership_version": 1}, 204)
	}
	status, body, _ = b.request(t, other, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, nil)
	if status != 200 {
		t.Fatal("second approver real password login prerequisite")
	}
	otherToken := body["access_token"].(string)
	hash := fingerprint(tenant, target, "ADMIN_LOST")
	approval := func(ref, hash string) map[string]any {
		return map[string]any{"approval_reference": ref, "payload_hash": hash}
	}
	execution := func(ref, hash, proof string) map[string]any {
		return map[string]any{"approval_reference": ref, "payload_hash": hash, "reauthentication_proof": proof}
	}
	t.Run("healthy_tenant_refused_and_console_credential_rejected", func(t *testing.T) {
		r := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 409)
		if r["code"] != "RECOVERY_CONFLICT" {
			t.Fatal("healthy administrator rejection reason")
		}
		console, token, _, _ := b.login(t, 1)
		status, denial, _ := b.wr22Environment.request(t, console, "POST", base, token, payload(tenant, target), map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 401 || denial["code"] != "CREDENTIAL_INVALID" {
			t.Fatal("Console crossed dedicated recovery boundary")
		}
	})
	var accessBefore, lifecycleBefore string
	if b.owner.QueryRow(ctx, `SELECT row_to_json(a)::text,row_to_json(l)::text FROM tenant_access a JOIN tenant_lifecycle_projections l USING(tenant_id) WHERE a.tenant_id=$1`, tenant).Scan(&accessBefore, &lifecycleBefore) != nil {
		t.Fatal("Tenant owner state prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND status='active'`, tenant); err != nil {
		t.Fatal("own lost Tenant administrator injection")
	}
	t.Run("target_and_hash_validation_create_no_authority", func(t *testing.T) {
		bad := payload(tenant, target)
		bad["payload_hash"] = fingerprint(referenceTenants[1], target, "ADMIN_LOST")
		call(t, first, firstToken, "POST", base, uuid.NewString(), bad, 409)
		bad = payload(tenant, target)
		bad["reason_code"] = "ADMIN LOST"
		call(t, first, firstToken, "POST", base, uuid.NewString(), bad, 400)
		var workload uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT id FROM principals WHERE principal_type='workload' LIMIT 1`).Scan(&workload) != nil {
			t.Fatal("own gateway Workload prerequisite")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, workload), 409)
		unverified := mustV7(t)
		if _, err := b.owner.Exec(ctx, `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, unverified); err != nil {
			t.Fatal("own unverified Human prerequisite")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, unverified), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE principals SET status='disabled',version=version+1,updated_at=now() WHERE id=$1`, target); err != nil {
			t.Fatal("own target disable")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE principals SET status='active',version=version+1,updated_at=now() WHERE id=$1`, target); err != nil {
			t.Fatal("own target restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=now()+interval '1 hour' WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own target lock")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=NULL WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own target unlock")
		}
		var n int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_admin_recovery_operations`).Scan(&n) != nil || n != 0 {
			t.Fatal("invalid target persisted a recovery")
		}
	})
	if t.Failed() {
		return
	}
	requestKey := uuid.NewString()
	id := ""
	t.Run("concurrent_request_single_intent_no_membership_or_session", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 202)
				v, _ := r["operation_id"].(string)
				out <- v
			}()
		}
		wg.Wait()
		close(out)
		for v := range out {
			if v == "" {
				t.Fatal("recovery intent missing")
			}
			if id == "" {
				id = v
			}
			if id != v {
				t.Fatal("concurrent recovery duplicate intents")
			}
		}
		var operations, members, audits int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_admin_recovery_operations WHERE id=$1 AND status='pending_approval'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$2 AND principal_id=$3),(SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action='iam.platform.requestRestoreTenantAdmin' AND caller_principal_id IS NOT NULL)`, id, tenant, target).Scan(&operations, &members, &audits) != nil || operations != 1 || members != 0 || audits != 1 {
			t.Fatal("request not atomic or created target authority")
		}
	})
	if t.Failed() || id == "" {
		return
	}
	reference := "WR22-" + uuid.NewString()
	t.Run("separate_approver_exact_hash_reference_and_missing_operation", func(t *testing.T) {
		call(t, first, firstToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, hash), 403)
		call(t, other, otherToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, fingerprint(tenant, b.humans[0], "ADMIN_LOST")), 409)
		call(t, other, otherToken, "POST", base+"/"+mustV7(t).String()+"/approve", uuid.NewString(), approval(reference, hash), 404)
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, firstToken), 409)
		key := uuid.NewString()
		r := call(t, other, otherToken, "POST", base+"/"+id+"/approve", key, approval(reference, hash), 200)
		if r["status"] != "approved" || r["version"] != float64(2) || r["approver_principal_id"] != b.humans[0].String() || r["requester_principal_id"] != firstHuman {
			t.Fatal("approval identity not bound")
		}
		expires, err := time.Parse(time.RFC3339Nano, r["expires_at"].(string))
		if err != nil || time.Until(expires) < 59*time.Minute || time.Until(expires) > time.Hour {
			t.Fatal("approval lifetime differs")
		}
		call(t, other, otherToken, "POST", base+"/"+id+"/approve", key, approval(reference, hash), 200)
		call(t, other, otherToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, hash), 409)
		duplicate := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 202)["operation_id"].(string)
		call(t, other, otherToken, "POST", base+"/"+duplicate+"/approve", uuid.NewString(), approval(reference, hash), 409)
	})
	t.Run("proof_subject_freshness_client_clock_and_refresh", func(t *testing.T) {
		path := base + "/" + id + "/execute"
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference, hash, otherToken), 403)
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference, hash, "invalid-proof"), 401)
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference+"-wrong", hash, firstToken), 409)
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference, fingerprint(tenant, b.humans[0], "ADMIN_LOST"), firstToken), 409)
		spoof := execution(reference, hash, firstToken)
		spoof["reauthenticated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		call(t, first, firstToken, "POST", path, uuid.NewString(), spoof, 400)
		if _, err := b.owner.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '20 minutes',reauthenticated_at=now()-interval '20 minutes',updated_at=now() WHERE id=$1`, firstSession); err != nil {
			t.Fatal("own reauthentication age fixture")
		}
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference, hash, firstToken), 403)
		status, r, _ := b.request(t, first, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("real refresh after authentication age")
		}
		firstToken = r["access_token"].(string)
		call(t, first, firstToken, "POST", path, uuid.NewString(), execution(reference, hash, firstToken), 403)
		// Real OIDC reauthentication creates a fresh, current BOSS Session.
		first = b.browser(t)
		location, state := b.beginOIDC(t, first, "")
		code, returned := b.dexAuthorize(t, location)
		if returned != state {
			t.Fatal("reauthentication state")
		}
		status, _, _ = b.callback(t, first, code, state)
		if status != 303 {
			t.Fatal("real OIDC reauthentication failed")
		}
		status, r, _ = b.request(t, first, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("reauthenticated BOSS Access Token missing")
		}
		firstToken = r["access_token"].(string)
	})
	if t.Failed() {
		return
	}
	t.Run("target_and_lost_admin_rechecked_after_approval", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='active',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2 AND status='suspended'`, tenant, b.humans[0]); err != nil {
			t.Fatal("own admin recovery race fixture")
		}
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, firstToken), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2 AND status='active'`, tenant, b.humans[0]); err != nil {
			t.Fatal("own admin loss restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=now()+interval '1 hour' WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own approved target lock")
		}
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, firstToken), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=NULL WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own approved target unlock")
		}
	})
	t.Run("positive_platform_audit_fault_rolls_back_tenant_effect_and_approval", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_recovery_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_recovery_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_recovery_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.executeRestoreTenantAdmin' THEN PERFORM nextval('wr22_recovery_fault_hits'); RAISE EXCEPTION 'isolated recovery Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_recovery_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_recovery_fault()`)
		if err != nil {
			t.Fatal("own recovery Audit fault setup")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_recovery_fault ON iam_audit_events; DROP FUNCTION wr22_recovery_fault(); DROP SEQUENCE wr22_recovery_fault_hits`); err != nil {
				t.Fatal("own recovery Audit fault restore")
			}
		}()
		key := uuid.NewString()
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", key, execution(reference, hash, firstToken), 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_recovery_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("recovery Audit fault not reached")
		}
		var approved, members, audits, receipts int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_admin_recovery_operations WHERE id=$1 AND status='approved' AND version=2 AND executed_at IS NULL),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$2 AND principal_id=$3),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$2 AND action='iam.recovery.tenant-admin.restored'),(SELECT count(*) FROM platform_mutation_results WHERE idempotency_key=$4)`, id, tenant, target, key).Scan(&approved, &members, &audits, &receipts) != nil || approved != 1 || members != 0 || audits != 0 || receipts != 0 {
			t.Fatal("recovery Audit failure left partial effect")
		}
	})
	if t.Failed() {
		return
	}
	executeKey := uuid.NewString()
	restoredMember := ""
	t.Run("concurrent_execution_once_and_real_target_login", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan int, 2)
		for _, key := range []string{executeKey, uuid.NewString()} {
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				s, _, _ := b.request(t, first, "POST", base+"/"+id+"/execute", firstToken, execution(reference, hash, firstToken), map[string]string{"Idempotency-Key": key})
				out <- s
			}(key)
		}
		wg.Wait()
		close(out)
		counts := map[int]int{}
		for s := range out {
			counts[s]++
		}
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatal("recovery execution race did not have exactly one winner")
		}
		var recordStatus, receiptKey string
		var ops, bindings, tenantAudits, platformAudits int
		if b.owner.QueryRow(ctx, `SELECT status,membership_id FROM tenant_admin_recovery_operations WHERE id=$1 AND version=3 AND executed_at IS NOT NULL`, id).Scan(&recordStatus, &restoredMember) != nil || recordStatus != "executed" {
			t.Fatal("executed recovery record missing")
		}
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status='active'),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND membership_id=$3 AND role_id=$4),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$3 AND action='iam.recovery.tenant-admin.restored' AND caller_principal_id IS NOT NULL),(SELECT count(*) FROM iam_audit_events WHERE target_id=$5 AND action='iam.platform.executeRestoreTenantAdmin' AND tenant_id IS NULL AND boundary='platform')`, tenant, target, restoredMember, b.adminRoles[0], id).Scan(&ops, &bindings, &tenantAudits, &platformAudits) != nil || ops != 1 || bindings != 1 || tenantAudits != 1 || platformAudits != 1 {
			t.Fatal("recovery effects and boundary Audits differ")
		}
		if b.owner.QueryRow(ctx, `SELECT idempotency_key FROM platform_mutation_results WHERE operation='executeRestoreTenantAdmin' AND result->>'target_id'=$1`, id).Scan(&receiptKey) != nil {
			t.Fatal("recovery receipt missing")
		}
		executeKey = receiptKey
		r := call(t, first, firstToken, "POST", base+"/"+id+"/execute", executeKey, execution(reference, hash, firstToken), 200)
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), firstToken) || strings.Contains(string(raw), "reauthentication_proof") || r["status"] != "executed" || r["kind"] != "restore_tenant_admin" {
			t.Fatal("recovery receipt leaked proof or wrong kind")
		}
		browser := b.wr22Environment.browser(t)
		s, login, _ := b.wr22Environment.request(t, browser, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[1], "password": b.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, nil)
		if s != 200 {
			t.Fatalf("recovered target real Console login status=%d reason=%v", s, login["code"])
		}
		s, _, _ = b.wr22Environment.request(t, browser, "GET", "/iam/tenants/"+tenant.String()+"/roles", login["access_token"].(string), nil, nil)
		if s != 200 {
			t.Fatal("recovered target lacks actual administrator authority")
		}
		var a, l string
		if b.owner.QueryRow(ctx, `SELECT row_to_json(a)::text,row_to_json(l)::text FROM tenant_access a JOIN tenant_lifecycle_projections l USING(tenant_id) WHERE a.tenant_id=$1`, tenant).Scan(&a, &l) != nil || a != accessBefore || l != lifecycleBefore {
			t.Fatal("recovery changed Tenant Access or Core Lifecycle")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("current_permission_before_receipt_and_removed_membership_new_identity", func(t *testing.T) {
		call(t, first, firstToken, "DELETE", "/iam/platform/members/"+firstMember+"/role-bindings/"+role+"?expected_membership_version=2", uuid.NewString(), nil, 204)
		call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 403)
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", executeKey, execution(reference, hash, firstToken), 403)
		call(t, first, firstToken, "POST", "/iam/platform/members/"+firstMember+"/role-bindings", uuid.NewString(), map[string]any{"role_id": role, "expected_membership_version": 3}, 204)
		// Only the new test relation is removed. A fresh approval cannot revive it.
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='removed',version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, restoredMember); err != nil {
			t.Fatal("own restored Membership removal fixture")
		}
		next := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 202)["operation_id"].(string)
		ref := "WR22-" + uuid.NewString()
		call(t, other, otherToken, "POST", base+"/"+next+"/approve", uuid.NewString(), approval(ref, hash), 200)
		call(t, first, firstToken, "POST", base+"/"+next+"/execute", uuid.NewString(), execution(ref, hash, firstToken), 200)
		var oldStatus, newMember string
		if b.owner.QueryRow(ctx, `SELECT status FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, tenant, restoredMember).Scan(&oldStatus) != nil || oldStatus != "removed" {
			t.Fatal("recovery revived removed history")
		}
		if b.owner.QueryRow(ctx, `SELECT membership_id FROM tenant_admin_recovery_operations WHERE id=$1 AND status='executed'`, next).Scan(&newMember) != nil || newMember == restoredMember {
			t.Fatal("recovery reused removed Membership identity")
		}
	})
	t.Run("synthetic_one_hour_expiry_blocks_approval_and_execution", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND status='active'`, tenant); err != nil {
			t.Fatal("own expiry lost-admin prerequisite")
		}
		age := func(t *testing.T, id string, approved bool) {
			t.Helper()
			tx, err := b.owner.Begin(ctx)
			if err != nil {
				t.Fatal("own recovery expiry transaction")
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, `ALTER TABLE tenant_admin_recovery_operations DISABLE TRIGGER tenant_admin_recovery_identity`); err != nil {
				t.Fatal("own expiry guard fixture")
			}
			if approved {
				_, err = tx.Exec(ctx, `UPDATE tenant_admin_recovery_operations SET created_at=now()-interval '3 hours',approved_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',updated_at=now() WHERE id=$1`, id)
			} else {
				_, err = tx.Exec(ctx, `UPDATE tenant_admin_recovery_operations SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',updated_at=now() WHERE id=$1`, id)
			}
			if err != nil {
				t.Fatal("own recovery synthetic expiry")
			}
			if _, err = tx.Exec(ctx, `ALTER TABLE tenant_admin_recovery_operations ENABLE TRIGGER tenant_admin_recovery_identity`); err != nil || tx.Commit(ctx) != nil {
				t.Fatal("own recovery expiry restore guard")
			}
		}
		pending := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 202)["operation_id"].(string)
		age(t, pending, false)
		call(t, other, otherToken, "POST", base+"/"+pending+"/approve", uuid.NewString(), approval("WR22-"+uuid.NewString(), hash), 409)
		approved := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 202)["operation_id"].(string)
		ref := "WR22-" + uuid.NewString()
		call(t, other, otherToken, "POST", base+"/"+approved+"/approve", uuid.NewString(), approval(ref, hash), 200)
		age(t, approved, true)
		call(t, first, firstToken, "POST", base+"/"+approved+"/execute", uuid.NewString(), execution(ref, hash, firstToken), 409)
	})
	t.Run("exact_direct_caller_grant_and_binding_before_replay", func(t *testing.T) {
		var grant, caller, binding uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT g.id,g.principal_id,b.id FROM workload_grants g JOIN workload_identity_bindings b ON b.principal_id=g.principal_id WHERE g.operation='/iam.v1.IAMAdminService/RequestRestoreTenantAdmin' AND g.status='active' AND b.status='active'`).Scan(&grant, &caller, &binding) != nil {
			t.Fatal("exact recovery caller prerequisite")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own exact recovery caller grant revoke")
		}
		call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 403)
		// Unrelated granted RPC remains available for the same real caller.
		call(t, first, firstToken, "GET", "/iam/platform/roles/"+role, "", nil, 200)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own exact recovery caller grant restore")
		}
		call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 202)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='revoked',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own recovery caller binding revoke")
		}
		call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 401)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='active',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own recovery caller binding restore")
		}
		call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 202)
	})
	t.Run("runtime_history_and_cross_tenant_constraints", func(t *testing.T) {
		runtime := mustPool(t, b.iam.config.Runtime.Postgresql.Dsn)
		defer runtime.Close()
		var runtimeRole string
		if runtime.QueryRow(ctx, `SELECT current_user`).Scan(&runtimeRole) != nil || runtimeRole != "ani_iam_runtime" {
			t.Fatal("formal runtime role prerequisite")
		}
		check := func(query, state string, args ...any) {
			t.Helper()
			tx, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal("runtime invariant transaction")
			}
			defer tx.Rollback(ctx)
			_, err = tx.Exec(ctx, query, args...)
			if err == nil {
				_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != state {
				t.Fatalf("recovery runtime invariant expected SQLSTATE=%s", state)
			}
		}
		check(`DELETE FROM tenant_admin_recovery_operations WHERE id=$1`, "42501", id)
		check(`UPDATE tenant_admin_recovery_operations SET target_principal_id=$2 WHERE id=$1`, "23514", id, b.humans[0])
		check(`UPDATE tenant_admin_recovery_operations SET status='approved',version=version+1 WHERE id=$1`, "23514", id)
		// Target/Member ownership is enforced even for a syntactically valid insert;
		// the initial-state guard rejects an inserted preapproved operation first.
		check(`INSERT INTO tenant_admin_recovery_operations(tenant_id,id,target_principal_id,requester_principal_id,approver_principal_id,approval_reference,reason_code,payload_fingerprint,status,version,created_at,updated_at,expires_at,approved_at) VALUES($1,$2,$3,$4,$5,$6,'ADMIN_LOST',$7,'approved',2,now(),now(),now()+interval '1 hour',now())`, "23514", tenant, mustV7(t), target, firstHuman, b.humans[0], "WR22-"+uuid.NewString(), hash)
		check(`INSERT INTO tenant_role_bindings(tenant_id,id,membership_id,role_id,version,created_at,updated_at) VALUES($1,$2,$3,$4,1,now(),now())`, "23503", referenceTenants[1], mustV7(t), restoredMember, b.adminRoles[1])
	})
	t.Run("independent_approvals_compete_for_same_lost_admin_condition", func(t *testing.T) {
		type approved struct{ ID, Reference, Hash string }
		intents := []approved{}
		for _, candidate := range []uuid.UUID{target, b.humans[0]} {
			h := fingerprint(tenant, candidate, "ADMIN_LOST")
			ref := "WR22-" + uuid.NewString()
			op := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, candidate), 202)["operation_id"].(string)
			call(t, other, otherToken, "POST", base+"/"+op+"/approve", uuid.NewString(), approval(ref, h), 200)
			intents = append(intents, approved{op, ref, h})
		}
		var wg sync.WaitGroup
		out := make(chan int, 2)
		for _, intent := range intents {
			wg.Add(1)
			go func(intent approved) {
				defer wg.Done()
				s, _, _ := b.request(t, first, "POST", base+"/"+intent.ID+"/execute", firstToken, execution(intent.Reference, intent.Hash, firstToken), map[string]string{"Idempotency-Key": uuid.NewString()})
				out <- s
			}(intent)
		}
		wg.Wait()
		close(out)
		counts := map[int]int{}
		for status := range out {
			counts[status]++
		}
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatal("separate recovery approvals both bypassed lost-admin condition")
		}
		var n int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships m JOIN tenant_role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN tenant_roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.tenant_id=$1 AND m.status='active' AND r.system_role AND r.code='tenant-admin'`, tenant).Scan(&n) != nil || n != 1 {
			t.Fatal("competing recoveries created more than one active administrator")
		}
	})

}
