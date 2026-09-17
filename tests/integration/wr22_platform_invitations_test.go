//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"sync"
	"testing"
)

func TestWR22FormalPlatformInvitations(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	browser := b.browser(t)
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("BOSS state prerequisite")
	}
	status, body, _ := b.callback(t, browser, code, state)
	if status != 303 {
		t.Fatal("BOSS first admin prerequisite")
	}
	status, body, _ = b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS refresh prerequisite")
	}
	token := body["access_token"].(string)
	const base = "/iam/platform/invitations"
	const roles = "/iam/platform/roles"
	call := func(t *testing.T, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		status, body, response := b.request(t, browser, method, path, token, payload, headers)
		if status != want {
			t.Fatalf("Platform Invitation status=%d reason=%v want=%d", status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("Platform Invitation response cacheable")
		}
		return body
	}
	role := call(t, "POST", roles, uuid.NewString(), map[string]any{"name": "WR22 Platform invite readers", "permissions": []string{"iam.platform-memberships/read"}}, 201)["role_id"].(string)
	payload := map[string]any{"email": "Platform.Invitee+wr22@example.test", "role_ids": []string{role}, "locale": "zh-CN"}
	key := uuid.NewString()
	id := ""
	t.Run("concurrent_create_metadata_only_and_no_authority", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := call(t, "POST", base, key, payload, 201)
				value, _ := r["invitation_id"].(string)
				out <- value
			}()
		}
		wg.Wait()
		close(out)
		for v := range out {
			if v == "" {
				t.Fatal("Invitation identity missing")
			}
			if id == "" {
				id = v
			}
			if id != v {
				t.Fatal("concurrent Invitation duplicated intent")
			}
		}
		var count, delivery, audit, human, membership int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_invitations WHERE id=$1),(SELECT count(*) FROM platform_invitation_outbox WHERE invitation_id=$1),(SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action='iam.platform.createPlatformIAMInvitation' AND boundary='platform' AND tenant_id IS NULL AND caller_principal_id IS NOT NULL),(SELECT count(*) FROM verified_emails WHERE normalized_email='platform.invitee+wr22@example.test'),(SELECT count(*) FROM platform_memberships WHERE principal_id IN (SELECT principal_id FROM verified_emails WHERE normalized_email='platform.invitee+wr22@example.test'))`, id).Scan(&count, &delivery, &audit, &human, &membership) != nil || count != 1 || delivery != 1 || audit != 1 || human != 0 || membership != 0 {
			t.Fatal("Platform Invitation atomic intent created authority or duplicates")
		}
		r := call(t, "POST", base, uuid.NewString(), payload, 201)
		raw, _ := json.Marshal(r)
		if r["invitation_id"] != id || r["normalized_email_hint"] != "p***@example.test" || r["delivery_status"] != "pending" || r["delivery_generation"] != float64(1) || strings.Contains(string(raw), "platform.invitee+wr22") || strings.Contains(string(raw), "invitation_token") || strings.Contains(string(raw), "tenant_id") {
			t.Fatal("Platform metadata leaked mailbox/token or acquired Tenant boundary")
		}
	})
	if id == "" || t.Failed() {
		return
	}
	t.Run("boundary_role_filter_and_cursor", func(t *testing.T) {
		console, consoleToken, _, _ := b.login(t, 0)
		status, _, _ = b.wr22Environment.request(t, console, "GET", "/iam/tenants/"+referenceTenants[0].String()+"/roles", consoleToken, nil, nil)
		if status != 200 {
			t.Fatal("own Console credential prerequisite invalid")
		}
		status, denial, _ := b.wr22Environment.request(t, console, "GET", base+"/"+id, consoleToken, nil, nil)
		if status != 401 || denial["code"] != "CREDENTIAL_INVALID" {
			t.Fatalf("Console-to-Platform rejection status=%d reason=%v", status, denial["code"])
		}
		call(t, "GET", base+"/"+id, "", nil, 200)
		call(t, "GET", base+"/"+uuid.Must(uuid.NewV7()).String(), "", nil, 404)
		call(t, "POST", base, uuid.NewString(), map[string]any{"email": "cross-boundary@example.test", "role_ids": []string{b.adminRoles[0].String()}}, 404)
		_, err := b.owner.Exec(ctx, `INSERT INTO platform_invitation_roles(invitation_id,role_id) VALUES($1,$2)`, id, b.adminRoles[0])
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23503" {
			t.Fatal("Platform Role FK accepted Tenant Role")
		}
		call(t, "POST", base, uuid.NewString(), map[string]any{"email": "second-platform@example.test", "role_ids": []string{role}}, 201)
		r := call(t, "GET", base+"?limit=1&status=pending", "", nil, 200)
		cursor, ok := r["next_cursor"].(string)
		if !ok || len(r["items"].([]any)) != 1 {
			t.Fatal("Platform Invitation pagination missing")
		}
		next := call(t, "GET", base+"?limit=1&status=pending&cursor="+cursor, "", nil, 200)
		if len(next["items"].([]any)) != 1 {
			t.Fatal("Platform Invitation next page missing")
		}
		call(t, "GET", base+"?status=expired&cursor="+cursor, "", nil, 400)
		call(t, "GET", base+"?status=invalid", "", nil, 400)
		call(t, "DELETE", roles+"/"+role+"?expected_version=1", uuid.NewString(), nil, 409)
	})
	t.Run("role_set_conflict_resend_and_positive_audit_rollback", func(t *testing.T) {
		var builtin string
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin' AND system_role`).Scan(&builtin) != nil {
			t.Fatal("Platform builtin role prerequisite")
		}
		r := call(t, "POST", base, uuid.NewString(), map[string]any{"email": "platform.invitee+wr22@example.test", "role_ids": []string{builtin}}, 409)
		if r["code"] != "INVITATION_CONFLICT" {
			t.Fatal("Platform conflict reason changed")
		}
		path := base + "/" + id + "/resend"
		rk := uuid.NewString()
		r = call(t, "POST", path, rk, map[string]any{"expected_version": 1}, 200)
		if r["version"] != float64(2) || r["delivery_generation"] != float64(2) {
			t.Fatal("Platform resend did not rotate")
		}
		call(t, "POST", path, rk, map[string]any{"expected_version": 1}, 200)
		call(t, "POST", path, uuid.NewString(), map[string]any{"expected_version": 1}, 409)
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_platform_invitation_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_platform_invitation_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_platform_invitation_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.resendPlatformIAMInvitation' THEN PERFORM nextval('wr22_platform_invitation_fault_hits'); RAISE EXCEPTION 'isolated Invitation Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_platform_invitation_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_platform_invitation_fault()`)
		if err != nil {
			t.Fatal("own Platform Audit fault setup")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_platform_invitation_fault ON iam_audit_events; DROP FUNCTION wr22_platform_invitation_fault(); DROP SEQUENCE wr22_platform_invitation_fault_hits`); err != nil {
				t.Fatal("own Platform Audit fault restore")
			}
		}()
		call(t, "POST", path, uuid.NewString(), map[string]any{"expected_version": 2}, 503)
		var hit bool
		var version, deliveries, cleared int
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_platform_invitation_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("Platform Audit fault not reached")
		}
		if b.owner.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM platform_invitation_outbox WHERE invitation_id=$1),(SELECT count(*) FROM platform_invitation_outbox WHERE invitation_id=$1 AND delivery_generation=1 AND status='cancelled' AND payload_ciphertext IS NULL) FROM platform_invitations WHERE id=$1`, id).Scan(&version, &deliveries, &cleared) != nil || version != 2 || deliveries != 2 || cleared != 1 {
			t.Fatal("Platform Audit failure changed rotation or preserved old secret")
		}
	})
	t.Run("concurrent_cancel_resend_and_current_authority", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan int, 2)
		for _, op := range []string{"cancel", "resend"} {
			wg.Add(1)
			go func(op string) {
				defer wg.Done()
				status, _, _ := b.request(t, browser, "POST", base+"/"+id+"/"+op, token, map[string]any{"expected_version": 2}, map[string]string{"Idempotency-Key": uuid.NewString()})
				out <- status
			}(op)
		}
		wg.Wait()
		close(out)
		wins, conflicts := 0, 0
		for status := range out {
			switch status {
			case 200, 204:
				wins++
			case 409:
				conflicts++
			default:
				t.Fatal("Platform transition returned unexpected status")
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("Platform transition race had multiple winners")
		}
		var member string
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_memberships WHERE status='active'`).Scan(&member) != nil {
			t.Fatal("own Platform member prerequisite")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='suspended',version=version+1,updated_at=now() WHERE id=$1`, member); err != nil {
			t.Fatal("own Platform authority fault")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='active',version=version+1,updated_at=now() WHERE id=$1`, member); err != nil {
				t.Fatal("own Platform authority restore")
			}
		}()
		call(t, "POST", base, key, payload, 403)
	})
	t.Run("synthetic_expiry_releases_references_without_losing_history", func(t *testing.T) {
		var builtin string
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin' AND system_role`).Scan(&builtin) != nil {
			t.Fatal("Platform expiry builtin prerequisite")
		}
		call(t, "POST", base, uuid.NewString(), map[string]any{"email": "list-expiry-platform@example.test", "role_ids": []string{builtin}}, 201)
		tx, err := b.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own Platform expiry tx")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `ALTER TABLE platform_invitations DISABLE TRIGGER platform_invitation_identity; UPDATE platform_invitations SET created_at=now()-interval '8 days',expires_at=now()-interval '1 day' WHERE status='pending'; ALTER TABLE platform_invitations ENABLE TRIGGER platform_invitation_identity`); err != nil {
			t.Fatal("own Platform synthetic expiry")
		}
		if tx.Commit(ctx) != nil {
			t.Fatal("own Platform expiry commit")
		}
		call(t, "DELETE", roles+"/"+role+"?expected_version=1", uuid.NewString(), nil, 204)
		r := call(t, "GET", base+"?status=expired", "", nil, 200)
		if len(r["items"].([]any)) < 1 {
			t.Fatal("expired Platform history missing")
		}
		pending := call(t, "GET", base+"?status=pending", "", nil, 200)
		if len(pending["items"].([]any)) != 0 {
			t.Fatal("expired Platform invite remained pending")
		}
		var live int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM platform_invitation_roles WHERE role_id=$1`, role).Scan(&live) != nil || live != 0 {
			t.Fatal("terminal Platform reference retained")
		}
	})
}
