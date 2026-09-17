//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestWR22FormalInvitationBootstrap(t *testing.T) {
	e := newWR22Environment(t)
	ctx := context.Background()
	recipient, credential, _, _ := e.login(t, 1)
	wrong, wrongCredential, _, _ := e.login(t, 0)
	type fixture struct {
		tenant, operation, invitation, role uuid.UUID
		secret                              string
		payload                             []byte
	}
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := e.owner.Exec(ctx, sql, args...); err != nil {
			t.Fatal("own Bootstrap Invitation prerequisite mutation failed")
		}
	}
	// Component prerequisites ONLY: retained Core operation, lifecycle and its
	// waiting invitation are created in this run's isolated database. The raw
	// invitation secret is private fixture input; no Core delivery/SMTP claim.
	seed := func(t *testing.T, linked bool) fixture {
		t.Helper()
		f := fixture{tenant: mustV7(t), operation: mustV7(t), invitation: mustV7(t), role: mustV7(t)}
		f.secret = "ani_inv_t." + f.tenant.String() + "." + f.invitation.String() + "." + strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
		f.payload, _ = json.Marshal(map[string]any{"tenant_id": f.tenant.String(), "intended_administrator": map[string]string{"normalized_email": e.accounts[1]}})
		exec(t, `INSERT INTO tenant_lifecycle_projections(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until) VALUES($1,'active',1,now(),now(),now()+interval '1 day')`, f.tenant)
		exec(t, `INSERT INTO tenant_access(tenant_id,status,version,created_at,updated_at) VALUES($1,'bootstrap_pending',1,now(),now())`, f.tenant)
		exec(t, `INSERT INTO tenant_roles(tenant_id,id,code,display_name,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,$2,'tenant-admin','Tenant administrator',true,1,1,now(),now())`, f.tenant, f.role)
		exec(t, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) SELECT $1,$2,scope,resource,action,now() FROM permission_catalog WHERE scope='tenant'`, f.tenant, f.role)
		var link any
		if linked {
			sum := sha256.Sum256(f.payload)
			exec(t, `INSERT INTO tenant_bootstrap_operations(tenant_id,id,source_kind,intended_email,payload_fingerprint,payload,status,version,created_at,updated_at) VALUES($1,$2,'core',$3,$4,$5,'pending',1,now(),now())`, f.tenant, f.operation, e.accounts[1], "sha256:"+hex.EncodeToString(sum[:]), f.payload)
			exec(t, `UPDATE tenant_bootstrap_operations SET status='waiting_for_principal_verification',version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, f.tenant, f.operation)
			link = f.operation
		}
		digest := sha256.Sum256([]byte(f.secret))
		exec(t, `INSERT INTO tenant_invitations(tenant_id,id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at,bootstrap_operation_id) VALUES($1,$2,$3,$4,'en-US','pending',$5,1,now()+interval '7 days',1,$6,now(),now(),$7)`, f.tenant, f.invitation, e.accounts[1], []uuid.UUID{f.role}, digest[:], e.humans[0], link)
		exec(t, `INSERT INTO tenant_invitation_roles(tenant_id,invitation_id,role_id) VALUES($1,$2,$3)`, f.tenant, f.invitation, f.role)
		exec(t, `INSERT INTO tenant_invitation_outbox(tenant_id,id,invitation_id,delivery_generation,payload_key_version,payload_ciphertext,status,available_at,version,created_at,updated_at) VALUES($1,$2,$3,1,'fixture',$4,'pending',now(),1,now(),now())`, f.tenant, mustV7(t), f.invitation, []byte(strings.Repeat("x", 64)))
		return f
	}
	accept := func(t *testing.T, c *http.Client, token string, f fixture, key string, want int) map[string]any {
		t.Helper()
		status, body, response := e.request(t, c, "POST", "/iam/tenants/"+f.tenant.String()+"/invitations/"+f.invitation.String()+"/accept", token, map[string]string{"invitation_token": f.secret}, map[string]string{"Idempotency-Key": key})
		if status != want {
			t.Fatalf("Bootstrap Invitation status=%d reason=%v want=%d", status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("Bootstrap acceptance cacheable")
		}
		return body
	}
	pending := func(t *testing.T, f fixture) {
		t.Helper()
		var access, members, bindings, invites, deliveries, refs, completed int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1 AND status='bootstrap_pending'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1),(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$2 AND status='pending' AND accepted_principal_id IS NULL),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2 AND status='pending' AND payload_ciphertext IS NOT NULL),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$2),(SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND status='succeeded')`, f.tenant, f.invitation).Scan(&access, &members, &bindings, &invites, &deliveries, &refs, &completed) != nil || access != 1 || members != 0 || bindings != 0 || invites != 1 || deliveries != 1 || refs != 1 || completed != 0 {
			t.Fatal("denied Bootstrap Invitation left partial authority or consumed intent")
		}
	}
	t.Run("ordinary_Invitation_cannot_activate_pending_Access", func(t *testing.T) {
		f := seed(t, false)
		accept(t, recipient, credential, f, uuid.NewString(), 503)
		pending(t, f)
	})
	t.Run("exact_original_recipient_and_waiting_state", func(t *testing.T) {
		f := seed(t, true)
		accept(t, wrong, wrongCredential, f, uuid.NewString(), 403)
		exec(t, `UPDATE tenant_bootstrap_operations SET status='attention_required',version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, f.tenant, f.operation)
		accept(t, recipient, credential, f, uuid.NewString(), 409)
		pending(t, f)
	})
	t.Run("builtin_definition_and_current_Console_login_required", func(t *testing.T) {
		f := seed(t, true)
		exec(t, `DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND (resource,action)=(SELECT resource,action FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 LIMIT 1)`, f.tenant, f.role)
		accept(t, recipient, credential, f, uuid.NewString(), 409)
		pending(t, f)
		f = seed(t, true)
		exec(t, `UPDATE password_credentials SET locked_until=now()+interval '1 hour' WHERE principal_id=$1`, e.humans[1])
		accept(t, recipient, credential, f, uuid.NewString(), 409)
		pending(t, f)
		exec(t, `UPDATE password_credentials SET locked_until=NULL WHERE principal_id=$1`, e.humans[1])
	})
	t.Run("current_Core_projection_gate_preserves_waiting_intent", func(t *testing.T) {
		f := seed(t, true)
		exec(t, `UPDATE tenant_lifecycle_projections SET observed_at=now()-interval '2 minutes',fresh_until=now()-interval '1 minute' WHERE tenant_id=$1`, f.tenant)
		r := accept(t, recipient, credential, f, uuid.NewString(), 503)
		if r["code"] != "TENANT_LIFECYCLE_STALE" {
			t.Fatal("stale Bootstrap target reason lost")
		}
		exec(t, `UPDATE tenant_lifecycle_projections SET status='suspended',fresh_until=now()+interval '1 day',lifecycle_version=lifecycle_version+1 WHERE tenant_id=$1`, f.tenant)
		accept(t, recipient, credential, f, uuid.NewString(), 403)
		pending(t, f)
	})
	if t.Failed() {
		return
	}
	f := seed(t, true)
	t.Run("positive_Bootstrap_Audit_failure_rolls_back_entire_completion", func(t *testing.T) {
		exec(t, `CREATE SEQUENCE wr22_bootstrap_accept_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_bootstrap_accept_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_bootstrap_accept_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.bootstrap.completed' THEN PERFORM nextval('wr22_bootstrap_accept_fault_hits'); RAISE EXCEPTION 'isolated Bootstrap acceptance Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_bootstrap_accept_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_bootstrap_accept_fault()`)
		defer exec(t, `DROP TRIGGER wr22_bootstrap_accept_fault ON iam_audit_events; DROP FUNCTION wr22_bootstrap_accept_fault(); DROP SEQUENCE wr22_bootstrap_accept_fault_hits`)
		accept(t, recipient, credential, f, uuid.NewString(), 503)
		var hit bool
		if e.owner.QueryRow(ctx, `SELECT is_called FROM wr22_bootstrap_accept_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("Bootstrap Audit fault was not reached")
		}
		pending(t, f)
		var audits, receipts int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1),(SELECT count(*) FROM tenant_mutation_results WHERE tenant_id=$1)`, f.tenant).Scan(&audits, &receipts) != nil || audits != 0 || receipts != 0 {
			t.Fatal("Bootstrap Audit rollback left receipt or acceptance Audit")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("concurrent_acceptance_completes_original_once_and_real_login", func(t *testing.T) {
		key := uuid.NewString()
		var wg sync.WaitGroup
		out := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := accept(t, recipient, credential, f, key, 200)
				m, _ := r["membership_id"].(string)
				out <- m
			}()
		}
		wg.Wait()
		close(out)
		member := ""
		for m := range out {
			if m == "" {
				t.Fatal("accepted Bootstrap membership missing")
			}
			if member == "" {
				member = m
			}
			if member != m {
				t.Fatal("concurrent Bootstrap created duplicate result")
			}
		}
		accept(t, recipient, credential, f, uuid.NewString(), 409)
		var access, members, bindings, completed, invites, audits, refs, deliveries int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1 AND status='active' AND version=2),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND id=$3 AND status='active'),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND membership_id=$3 AND role_id=$4),(SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$5 AND status='succeeded' AND version=3 AND membership_id=$3 AND principal_id=$2 AND intended_email=$6 AND payload=$7::jsonb),(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$8 AND status='accepted' AND accepted_membership_id=$3),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND actor_id=$2 AND action IN ('iam.invitation.accepted','iam.bootstrap.completed')),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND status='cancelled' AND payload_ciphertext IS NULL)`, f.tenant, e.humans[1], member, f.role, f.operation, e.accounts[1], f.payload, f.invitation).Scan(&access, &members, &bindings, &completed, &invites, &audits, &refs, &deliveries) != nil || access != 1 || members != 1 || bindings != 1 || completed != 1 || invites != 1 || audits != 2 || refs != 0 || deliveries != 1 {
			t.Fatal("Bootstrap acceptance immutable original or atomic result differs")
		}
		browser := e.browser(t)
		status, login, _ := e.request(t, browser, "POST", "/auth/password/login", "", map[string]any{"account": e.accounts[1], "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": f.tenant.String()}}, nil)
		if status != 200 {
			t.Fatal("accepted initial administrator cannot log in")
		}
		status, _, _ = e.request(t, browser, "GET", "/iam/tenants/"+f.tenant.String()+"/roles", login["access_token"].(string), nil, nil)
		if status != 200 {
			t.Fatal("accepted initial administrator lacks actual current role authority")
		}
	})
}
