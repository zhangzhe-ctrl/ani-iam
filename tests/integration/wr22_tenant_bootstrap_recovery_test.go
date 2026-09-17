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

func TestWR22FormalTenantBootstrapRecovery(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	sha256SumBytes := func(value []byte) []byte { sum := sha256.Sum256(value); return sum[:] }
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
	const base = "/iam/platform/recovery-bootstrap-requests"
	tenant, target := mustV7(t), b.humans[1]
	fingerprint := func(tenant, target uuid.UUID, reason string) string {
		return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join([]string{"ani.iam.recovery-bootstrap/v1", tenant.String(), target.String(), reason}, "\n"))))
	}
	payload := func(tenant, target uuid.UUID) map[string]any {
		return map[string]any{"tenant_id": tenant.String(), "target_principal_id": target.String(), "reason_code": "BOOTSTRAP_LOST", "payload_hash": fingerprint(tenant, target, "BOOTSTRAP_LOST")}
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
	role := call(t, first, firstToken, "POST", "/iam/platform/roles", uuid.NewString(), map[string]any{"name": "WR22 dedicated administrator recovery", "permissions": []string{"iam.recovery-bootstrap/request", "iam.recovery-bootstrap/approve", "iam.recovery-bootstrap/execute", "iam.restore-tenant-admin/request", "iam.restore-tenant-admin/approve", "iam.restore-tenant-admin/execute"}}, 201)["role_id"].(string)
	for _, member := range []string{firstMember, otherMember.String()} {
		call(t, first, firstToken, "POST", "/iam/platform/members/"+member+"/role-bindings", uuid.NewString(), map[string]any{"role_id": role, "expected_membership_version": 1}, 204)
	}
	status, body, _ = b.request(t, other, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, nil)
	if status != 200 {
		t.Fatal("second approver real password login prerequisite")
	}
	otherToken := body["access_token"].(string)
	hash := fingerprint(tenant, target, "BOOTSTRAP_LOST")
	approval := func(ref, hash string) map[string]any {
		return map[string]any{"approval_reference": ref, "payload_hash": hash}
	}
	execution := func(ref, hash, proof string) map[string]any {
		return map[string]any{"approval_reference": ref, "payload_hash": hash, "reauthentication_proof": proof}
	}

	t.Run("missing_Core_projection_and_established_Access_fail_closed", func(t *testing.T) {
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 503)
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(referenceTenants[0], target), 409)
		console, token, _, _ := b.login(t, 1)
		status, r, _ := b.wr22Environment.request(t, console, "POST", base, token, payload(tenant, target), map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 401 || r["code"] != "CREDENTIAL_INVALID" {
			t.Fatal("Console crossed Bootstrap recovery boundary")
		}
	})
	if t.Failed() {
		return
	}
	// This independent own projection row is only the local-recovery prerequisite.
	// Real Core publication and projection provenance are the separate WR23 gate.
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_lifecycle_projections(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until) VALUES($1,'active',1,now(),now(),now()+interval '1 day')`, tenant); err != nil {
		t.Fatal("own Core projection prerequisite without IAM Access")
	}
	requestKey := uuid.NewString()
	var id string
	t.Run("concurrent_intent_does_not_create_authority", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan string, 3)
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, first, firstToken, "POST", base, requestKey, payload(tenant, target), 202)
				out <- r["operation_id"].(string)
			}()
		}
		wg.Wait()
		close(out)
		for v := range out {
			if id == "" {
				id = v
			}
			if v != id || v == "" {
				t.Fatal("Bootstrap request did not converge")
			}
		}
		var access, roles, members, operations int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1),(SELECT count(*) FROM tenant_roles WHERE tenant_id=$1),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM tenant_bootstrap_recovery_operations WHERE tenant_id=$1)`, tenant).Scan(&access, &roles, &members, &operations) != nil || access != 0 || roles != 0 || members != 0 || operations != 1 {
			t.Fatal("Bootstrap request granted authority")
		}
	})
	if t.Failed() {
		return
	}
	reference := "WR22-BOOTSTRAP-" + uuid.NewString()
	t.Run("separate_approver_exact_payload_and_proof", func(t *testing.T) {
		call(t, first, firstToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, hash), 403)
		call(t, other, otherToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, fingerprint(tenant, b.humans[0], "BOOTSTRAP_LOST")), 409)
		call(t, other, otherToken, "POST", base+"/"+id+"/approve", uuid.NewString(), approval(reference, hash), 200)
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, otherToken), 403)
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, "invalid-proof"), 401)
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference+"-wrong", hash, firstToken), 409)
	})
	if t.Failed() {
		return
	}
	t.Run("positive_Audit_fault_rolls_back_whole_Bootstrap", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_bootstrap_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_bootstrap_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_bootstrap_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.recovery.bootstrap.completed' THEN PERFORM nextval('wr22_bootstrap_fault_hits'); RAISE EXCEPTION 'isolated Bootstrap Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_bootstrap_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_bootstrap_fault()`); err != nil {
			t.Fatal("own Bootstrap Audit fault setup")
		}
		call(t, first, firstToken, "POST", base+"/"+id+"/execute", uuid.NewString(), execution(reference, hash, firstToken), 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_bootstrap_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("Bootstrap Audit fault not reached")
		}
		var access, roles, members, operations, approved int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1),(SELECT count(*) FROM tenant_roles WHERE tenant_id=$1),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1),(SELECT count(*) FROM tenant_bootstrap_recovery_operations WHERE tenant_id=$1 AND id=$2 AND status='approved' AND executed_at IS NULL)`, tenant, id).Scan(&access, &roles, &members, &operations, &approved) != nil || access != 0 || roles != 0 || members != 0 || operations != 0 || approved != 1 {
			t.Fatal("Bootstrap Audit failure left partial authority or consumed approval")
		}
		if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_bootstrap_fault ON iam_audit_events; DROP FUNCTION wr22_bootstrap_fault(); DROP SEQUENCE wr22_bootstrap_fault_hits`); err != nil {
			t.Fatal("own Bootstrap Audit fault restore")
		}
	})
	if t.Failed() {
		return
	}
	key := uuid.NewString()
	t.Run("concurrent_execute_once_and_real_Console_administrator_login", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, first, firstToken, "POST", base+"/"+id+"/execute", key, execution(reference, hash, firstToken), 200)
				if r["status"] != "executed" || r["kind"] != "recovery_bootstrap" {
					t.Error("Bootstrap result kind or status differs")
				}
			}()
		}
		wg.Wait()
		var active, members, bindings, operations, audits int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1 AND status='active'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status='active'),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1),(SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$3 AND source_kind='recovery' AND status='succeeded' AND recovery_request_id=$3),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.recovery.bootstrap.completed')`, tenant, target, id).Scan(&active, &members, &bindings, &operations, &audits) != nil || active != 1 || members != 1 || bindings != 1 || operations != 1 || audits != 1 {
			t.Fatal("Bootstrap recovery did not atomically establish one authority")
		}
		c := b.wr22Environment.browser(t)
		status, r, _ := b.wr22Environment.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[1], "password": b.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, nil)
		if status != 200 {
			t.Fatal("recovered Bootstrap target cannot log in")
		}
		status, _, _ = b.wr22Environment.request(t, c, "GET", "/iam/tenants/"+tenant.String()+"/roles", r["access_token"].(string), nil, nil)
		if status != 200 {
			t.Fatal("recovered Bootstrap administrator lacks current authority")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(tenant, target), 409)
	})
	if t.Failed() {
		return
	}
	seedProjection := func(t *testing.T, tenant uuid.UUID) {
		t.Helper()
		if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_lifecycle_projections(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until) VALUES($1,'active',1,now(),now(),now()+interval '1 day')`, tenant); err != nil {
			t.Fatal("own additional Core projection prerequisite")
		}
	}
	t.Run("approval_reference_is_single_use_across_both_recovery_purposes", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND status='active'`, referenceTenants[0]); err != nil {
			t.Fatal("own Restore lost-admin prerequisite")
		}
		restoreHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join([]string{"ani.iam.restore-tenant-admin/v1", referenceTenants[0].String(), target.String(), "ADMIN_LOST"}, "\n"))))
		restore := call(t, first, firstToken, "POST", "/iam/platform/restore-tenant-admin-requests", uuid.NewString(), map[string]any{"tenant_id": referenceTenants[0].String(), "target_principal_id": target.String(), "reason_code": "ADMIN_LOST", "payload_hash": restoreHash}, 202)["operation_id"].(string)
		call(t, other, otherToken, "POST", "/iam/platform/restore-tenant-admin-requests/"+restore+"/approve", uuid.NewString(), approval(reference, restoreHash), 409)
		restoreRef := "WR22-RESTORE-" + uuid.NewString()
		call(t, other, otherToken, "POST", "/iam/platform/restore-tenant-admin-requests/"+restore+"/approve", uuid.NewString(), approval(restoreRef, restoreHash), 200)
		extra := mustV7(t)
		seedProjection(t, extra)
		recovery := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, target), 202)["operation_id"].(string)
		call(t, other, otherToken, "POST", base+"/"+recovery+"/approve", uuid.NewString(), approval(restoreRef, fingerprint(extra, target, "BOOTSTRAP_LOST")), 409)
		call(t, first, firstToken, "POST", "/iam/platform/restore-tenant-admin-requests/"+recovery+"/approve", uuid.NewString(), approval("WR22-"+uuid.NewString(), hash), 404)
	})
	if t.Failed() {
		return
	}
	originalTenant, originalID, originalInvitation := mustV7(t), mustV7(t), mustV7(t)
	originalRole := mustV7(t)
	seedProjection(t, originalTenant)
	originalPayload, _ := json.Marshal(map[string]any{"intended_administrator": map[string]string{"normalized_email": b.accounts[1]}, "tenant_id": originalTenant.String()})
	// A retained received Core operation and its initial Invitation are DB-seeded
	// prerequisites here; actual producer/consumer/bootstrap worker belongs to WR23.
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_access(tenant_id,status,version,created_at,updated_at) VALUES($1,'bootstrap_pending',1,now(),now())`, originalTenant); err != nil {
		t.Fatal("own pending Bootstrap Access prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_bootstrap_operations(tenant_id,id,source_kind,intended_email,payload_fingerprint,payload,status,version,created_at,updated_at) VALUES($1,$2,'core',$3,$4,$5,'pending',1,now(),now())`, originalTenant, originalID, b.accounts[1], "sha256:"+strings.Repeat("c", 64), originalPayload); err != nil {
		t.Fatal("own original Core Bootstrap prerequisite")
	}
	t.Run("same_original_identity_must_replay_original_operation", func(t *testing.T) {
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(originalTenant, target), 409)
	})
	// Recover to the other explicitly selected, verified Human; do not edit the
	// original intended identity or manufacture a new payload for its old ID.
	replacementTarget := b.humans[0]
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_roles(tenant_id,id,code,display_name,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,$2,'tenant-admin','Tenant administrator',true,1,1,now(),now())`, originalTenant, originalRole); err != nil {
		t.Fatal("own original builtin prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) SELECT $1,$2,scope,resource,action,now() FROM permission_catalog WHERE scope='tenant'`, originalTenant, originalRole); err != nil {
		t.Fatal("own original builtin catalog prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_invitations(tenant_id,id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at,bootstrap_operation_id) VALUES($1,$2,$3,$4,'en-US','pending',$5,1,now()+interval '7 days',1,$6,now(),now(),$7)`, originalTenant, originalInvitation, b.accounts[1], []uuid.UUID{originalRole}, sha256SumBytes([]byte("own-placeholder-bootstrap-token")), firstHuman, originalID); err != nil {
		t.Fatal("own exact Bootstrap Invitation prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_invitation_roles(tenant_id,invitation_id,role_id) VALUES($1,$2,$3)`, originalTenant, originalInvitation, originalRole); err != nil {
		t.Fatal("own Bootstrap live Role reference")
	}
	// Opaque pending delivery bytes are cancellation-state prerequisites only.
	if _, err := b.owner.Exec(ctx, `INSERT INTO tenant_invitation_outbox(tenant_id,id,invitation_id,delivery_generation,payload_key_version,payload_ciphertext,status,available_at,version,created_at,updated_at) VALUES($1,$2,$3,1,'fixture',$4,'pending',now(),1,now(),now())`, originalTenant, mustV7(t), originalInvitation, []byte(strings.Repeat("x", 64))); err != nil {
		t.Fatal("own pending Bootstrap delivery prerequisite")
	}
	replacementHash := fingerprint(originalTenant, replacementTarget, "BOOTSTRAP_LOST")
	replacement := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(originalTenant, replacementTarget), 202)["operation_id"].(string)
	replacementRef := "WR22-REPLACE-" + uuid.NewString()
	call(t, other, otherToken, "POST", base+"/"+replacement+"/approve", uuid.NewString(), approval(replacementRef, replacementHash), 200)
	t.Run("approved_identity_change_supersedes_exact_original_and_invitation", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_replace_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_replace_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_replace_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.recovery.bootstrap.completed' THEN PERFORM nextval('wr22_replace_fault_hits'); RAISE EXCEPTION 'isolated replacement Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_replace_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_replace_fault()`); err != nil {
			t.Fatal("own replacement Audit fault setup")
		}
		call(t, first, firstToken, "POST", base+"/"+replacement+"/execute", uuid.NewString(), execution(replacementRef, replacementHash, firstToken), 503)
		var faultHit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_replace_fault_hits`).Scan(&faultHit) != nil || !faultHit {
			t.Fatal("replacement Audit fault not reached")
		}
		var originalPending, invitationPending, liveReferences, ciphertext, members int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2 AND status='pending' AND superseded_by IS NULL),(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$3 AND status='pending'),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$3),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$3 AND status='pending' AND payload_ciphertext IS NOT NULL),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1)`, originalTenant, originalID, originalInvitation).Scan(&originalPending, &invitationPending, &liveReferences, &ciphertext, &members) != nil || originalPending != 1 || invitationPending != 1 || liveReferences != 1 || ciphertext != 1 || members != 0 {
			t.Fatal("replacement Audit failure did not preserve original intent and delivery")
		}
		if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_replace_fault ON iam_audit_events; DROP FUNCTION wr22_replace_fault(); DROP SEQUENCE wr22_replace_fault_hits`); err != nil {
			t.Fatal("own replacement Audit fault restore")
		}
		call(t, first, firstToken, "POST", base+"/"+replacement+"/execute", uuid.NewString(), execution(replacementRef, replacementHash, firstToken), 200)
		var oldState, oldEmail, invState string
		var successor uuid.UUID
		var principalMembers, targetMembers, refs, deliveries, audits int
		if b.owner.QueryRow(ctx, `SELECT status,intended_email,superseded_by FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2`, originalTenant, originalID).Scan(&oldState, &oldEmail, &successor) != nil || oldState != "superseded" || oldEmail != b.accounts[1] || successor.String() != replacement {
			t.Fatal("original Bootstrap identity or supersession link differs")
		}
		if b.owner.QueryRow(ctx, `SELECT status FROM tenant_invitations WHERE tenant_id=$1 AND id=$2`, originalTenant, originalInvitation).Scan(&invState) != nil || invState != "cancelled" {
			t.Fatal("old Bootstrap Invitation remains usable")
		}
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$3 AND status='active'),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$4),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$4 AND status='cancelled' AND payload_ciphertext IS NULL),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$4 AND action='iam.invitation.cancelled')`, originalTenant, target, replacementTarget, originalInvitation).Scan(&principalMembers, &targetMembers, &refs, &deliveries, &audits) != nil || principalMembers != 0 || targetMembers != 1 || refs != 0 || deliveries != 1 || audits != 1 {
			t.Fatal("Bootstrap identity replacement left old authority or pending delivery")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("runtime_immutable_reference_and_same_Tenant_links", func(t *testing.T) {
		runtime := mustPool(t, b.iam.config.Runtime.Postgresql.Dsn)
		defer runtime.Close()
		check := func(query, state string, args ...any) {
			t.Helper()
			tx, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal("own runtime constraint transaction")
			}
			defer tx.Rollback(ctx)
			_, err = tx.Exec(ctx, query, args...)
			if err == nil {
				_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != state {
				t.Fatalf("Bootstrap runtime invariant expected SQLSTATE=%s", state)
			}
		}
		check(`DELETE FROM iam_recovery_approval_references WHERE approval_reference=$1`, "42501", reference)
		check(`UPDATE iam_recovery_approval_references SET approval_reference=$2 WHERE approval_reference=$1`, "42501", reference, "different-reference")
		check(`UPDATE tenant_bootstrap_recovery_operations SET target_principal_id=$2 WHERE id=$1`, "23514", id, b.humans[0])
		check(`UPDATE tenant_bootstrap_operations SET intended_email='changed@example.test',version=version+1 WHERE tenant_id=$1 AND id=$2`, "23514", tenant, id)
		check(`UPDATE tenant_invitations SET bootstrap_operation_id=NULL,version=version+1 WHERE tenant_id=$1 AND id=$2`, "23514", originalTenant, originalInvitation)
		check(`INSERT INTO iam_recovery_approval_references(approval_reference,tenant_id,bootstrap_operation_id,created_at) VALUES($1,$2,$3,now())`, "23503", "WR22-FOREIGN-"+uuid.NewString(), referenceTenants[0], uuid.MustParse(id))
		check(`INSERT INTO tenant_invitations(tenant_id,id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at,bootstrap_operation_id) VALUES($1,$2,'foreign@example.test',$3,'en-US','pending',$4,1,now()+interval '7 days',1,$5,now(),now(),$6)`, "23503", referenceTenants[0], mustV7(t), []uuid.UUID{b.adminRoles[0]}, sha256SumBytes([]byte(uuid.NewString())), firstHuman, uuid.MustParse(id))
	})
	t.Run("synthetic_one_hour_approval_expiry", func(t *testing.T) {
		extra := mustV7(t)
		seedProjection(t, extra)
		extraHash := fingerprint(extra, target, "BOOTSTRAP_LOST")
		op := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, target), 202)["operation_id"].(string)
		ref := "WR22-EXPIRE-" + uuid.NewString()
		call(t, other, otherToken, "POST", base+"/"+op+"/approve", uuid.NewString(), approval(ref, extraHash), 200)
		tx, err := b.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own synthetic approval expiry transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_bootstrap_recovery_operations DISABLE TRIGGER tenant_bootstrap_recovery_identity`); err != nil {
			t.Fatal("own synthetic expiry fixture")
		}
		if _, err = tx.Exec(ctx, `UPDATE tenant_bootstrap_recovery_operations SET created_at=now()-interval '3 hours',approved_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',updated_at=now() WHERE id=$1`, op); err != nil {
			t.Fatal("own approval age injection")
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_bootstrap_recovery_operations ENABLE TRIGGER tenant_bootstrap_recovery_identity`); err != nil || tx.Commit(ctx) != nil {
			t.Fatal("own approval guard restore")
		}
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 409)
	})
	t.Run("current_target_and_projection_rechecked_after_approval", func(t *testing.T) {
		extra := mustV7(t)
		seedProjection(t, extra)
		extraHash := fingerprint(extra, target, "BOOTSTRAP_LOST")
		var workload uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT id FROM principals WHERE principal_type='workload' LIMIT 1`).Scan(&workload) != nil {
			t.Fatal("own Workload identity prerequisite")
		}
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, workload), 409)
		call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, mustV7(t)), 409)
		op := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, target), 202)["operation_id"].(string)
		ref := "WR22-CURRENT-" + uuid.NewString()
		call(t, other, otherToken, "POST", base+"/"+op+"/approve", uuid.NewString(), approval(ref, extraHash), 200)
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET observed_at=now()-interval '2 minutes',fresh_until=now()-interval '1 minute' WHERE tenant_id=$1`, extra); err != nil {
			t.Fatal("own stale Bootstrap projection fixture")
		}
		r := call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 503)
		// Required tenant/version metadata belongs to the gRPC ErrorInfo;
		// the frozen REST bridge exposes its stable code/message/request_id.
		if r["code"] != "TENANT_LIFECYCLE_STALE" {
			t.Fatal("Bootstrap stale error lost its stable reason")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET status='suspended',observed_at=now(),fresh_until=now()+interval '1 day' WHERE tenant_id=$1`, extra); err != nil {
			t.Fatal("own blocked Bootstrap projection fixture")
		}
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 403)
		if _, err := b.owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET status='active' WHERE tenant_id=$1`, extra); err != nil {
			t.Fatal("own Bootstrap projection restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE principals SET status='disabled',version=version+1,updated_at=now() WHERE id=$1`, target); err != nil {
			t.Fatal("own Bootstrap target disable")
		}
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE principals SET status='active',version=version+1,updated_at=now() WHERE id=$1`, target); err != nil {
			t.Fatal("own Bootstrap target restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=now()+interval '10 minutes' WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own Bootstrap target password lock")
		}
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 409)
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=NULL WHERE principal_id=$1`, target); err != nil {
			t.Fatal("own Bootstrap target password restore")
		}
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 200)
	})
	if t.Failed() {
		return
	}
	t.Run("independent_approvals_compete_for_one_missing_Access", func(t *testing.T) {
		extra := mustV7(t)
		seedProjection(t, extra)
		extraHash := fingerprint(extra, target, "BOOTSTRAP_LOST")
		ids, refs := []string{}, []string{}
		for i := 0; i < 2; i++ {
			op := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, target), 202)["operation_id"].(string)
			ref := "WR22-COMPETE-" + uuid.NewString()
			call(t, other, otherToken, "POST", base+"/"+op+"/approve", uuid.NewString(), approval(ref, extraHash), 200)
			ids = append(ids, op)
			refs = append(refs, ref)
		}
		out := make(chan int, 2)
		for i := 0; i < 2; i++ {
			go func(i int) {
				s, _, _ := b.request(t, first, "POST", base+"/"+ids[i]+"/execute", firstToken, execution(refs[i], extraHash, firstToken), map[string]string{"Idempotency-Key": uuid.NewString()})
				out <- s
			}(i)
		}
		a, b := <-out, <-out
		if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
			t.Fatal("two approved recoveries both established authority")
		}
	})
	t.Run("current_permission_and_exact_caller_before_recovery_receipt", func(t *testing.T) {
		path := base + "/" + id + "/execute"
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND resource='iam.recovery-bootstrap' AND action='execute'`, role); err != nil {
			t.Fatal("own Bootstrap permission revoke")
		}
		call(t, first, firstToken, "POST", path, key, execution(reference, hash, firstToken), 403)
		if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.recovery-bootstrap','execute',now())`, role); err != nil {
			t.Fatal("own Bootstrap permission restore")
		}
		var grant, caller, binding uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT g.id,g.principal_id,b.id FROM workload_grants g JOIN workload_identity_bindings b ON b.principal_id=g.principal_id WHERE g.operation='/iam.v1.IAMAdminService/ExecuteRecoveryBootstrap' AND g.status='active' AND b.status='active'`).Scan(&grant, &caller, &binding) != nil {
			t.Fatal("exact Bootstrap caller prerequisite")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own Bootstrap caller Grant revoke")
		}
		call(t, first, firstToken, "POST", path, key, execution(reference, hash, firstToken), 403)
		call(t, first, firstToken, "GET", "/iam/platform/roles/"+role, "", nil, 200)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1,updated_at=now() WHERE id=$1`, grant); err != nil {
			t.Fatal("own Bootstrap caller Grant restore")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='revoked',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own Bootstrap caller Binding revoke")
		}
		call(t, first, firstToken, "POST", path, key, execution(reference, hash, firstToken), 401)
		if _, err := b.owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='active',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller); err != nil {
			t.Fatal("own Bootstrap caller Binding restore")
		}
		call(t, first, firstToken, "POST", path, key, execution(reference, hash, firstToken), 200)
	})
	t.Run("stale_reauthentication_is_not_refreshed_by_token_rotation", func(t *testing.T) {
		extra := mustV7(t)
		seedProjection(t, extra)
		extraHash := fingerprint(extra, target, "BOOTSTRAP_LOST")
		op := call(t, first, firstToken, "POST", base, uuid.NewString(), payload(extra, target), 202)["operation_id"].(string)
		ref := "WR22-REAUTH-" + uuid.NewString()
		call(t, other, otherToken, "POST", base+"/"+op+"/approve", uuid.NewString(), approval(ref, extraHash), 200)
		spoof := execution(ref, extraHash, firstToken)
		spoof["reauthenticated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), spoof, 400)
		if _, err := b.owner.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '20 minutes',reauthenticated_at=now()-interval '20 minutes',updated_at=now() WHERE id=$1`, firstSession); err != nil {
			t.Fatal("own Bootstrap authentication age fixture")
		}
		status, r, _ := b.request(t, first, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("real refresh of aged Bootstrap actor")
		}
		firstToken = r["access_token"].(string)
		call(t, first, firstToken, "POST", base+"/"+op+"/execute", uuid.NewString(), execution(ref, extraHash, firstToken), 403)
	})

}
