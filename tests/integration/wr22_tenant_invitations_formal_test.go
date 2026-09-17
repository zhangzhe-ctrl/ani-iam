//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"testing"
)

func TestWR22FormalTenantInvitations(t *testing.T) {
	e := newWR22Environment(t)
	client, token, _, _ := e.login(t, 0)
	other, otherToken, _, _ := e.login(t, 1)
	ctx := context.Background()
	base := "/iam/tenants/" + referenceTenants[0].String() + "/invitations"
	otherBase := "/iam/tenants/" + referenceTenants[1].String() + "/invitations"
	roles := "/iam/tenants/" + referenceTenants[0].String() + "/roles"
	request := func(t *testing.T, c *http.Client, bearer, method, path string, body any, key string, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		code, result, response := e.request(t, c, method, path, bearer, body, headers)
		if code != want {
			t.Fatalf("invitation management status=%d reason=%s want=%d", code, result["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("invitation management response cacheable")
		}
		return result
	}
	createRole := request(t, client, token, "POST", roles, map[string]any{"name": "WR22 invitation readers", "permissions": []string{"instances/read"}}, uuid.NewString(), 201)
	role := createRole["role_id"].(string)
	body := map[string]any{"email": "New.Invitee+wr22@example.test", "role_ids": []string{role}, "locale": "en-US"}
	key := uuid.NewString()
	var id string
	t.Run("metadata_only_create_replay_dedup_and_audit", func(t *testing.T) {
		r := request(t, client, token, "POST", base, body, key, 201)
		id = r["invitation_id"].(string)
		if r["normalized_email_hint"] != "n***@example.test" || r["status"] != "pending" || r["version"] != float64(1) || r["delivery_status"] != "pending" || r["delivery_generation"] != float64(1) {
			t.Fatal("invitation metadata differs")
		}
		for _, k := range []string{key, uuid.NewString()} {
			r = request(t, client, token, "POST", base, body, k, 201)
			if r["invitation_id"] != id {
				t.Fatal("same invitation intent duplicated")
			}
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "invitation_token") || strings.Contains(string(raw), "ani_inv_") || strings.Contains(string(raw), "new.invitee+wr22") {
			t.Fatal("invitation response leaked delivery secret or full mailbox")
		}
		var intents, outbox, human, member int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2),(SELECT count(*) FROM verified_emails WHERE normalized_email='new.invitee+wr22@example.test'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id IN (SELECT principal_id FROM verified_emails WHERE normalized_email='new.invitee+wr22@example.test'))`, referenceTenants[0], id).Scan(&intents, &outbox, &human, &member) != nil || intents != 1 || outbox != 1 || human != 0 || member != 0 {
			t.Fatal("Invitation created authority or duplicate delivery")
		}
		conflict := request(t, client, token, "POST", base, map[string]any{"email": "new.invitee+wr22@example.test", "role_ids": []string{e.adminRoles[0].String()}}, uuid.NewString(), 409)
		if conflict["code"] != "INVITATION_CONFLICT" {
			t.Fatal("Invitation conflict lost stable reason")
		}
	})
	if id == "" || t.Failed() {
		return
	}
	t.Run("scope_roles_status_and_bound_cursor", func(t *testing.T) {
		request(t, client, "", "GET", base, nil, "", 401)
		request(t, other, otherToken, "GET", base+"/"+id, nil, "", 403)
		request(t, other, otherToken, "GET", otherBase+"/"+id, nil, "", 404)
		request(t, client, token, "POST", base, map[string]any{"email": "foreign-role@example.test", "role_ids": []string{e.adminRoles[1].String()}}, uuid.NewString(), 404)
		request(t, client, token, "POST", base, map[string]any{"email": "Second@example.test", "role_ids": []string{role}}, uuid.NewString(), 201)
		page := request(t, client, token, "GET", base+"?limit=1&status=pending", nil, "", 200)
		cursor, ok := page["next_cursor"].(string)
		if !ok || len(page["items"].([]any)) != 1 {
			t.Fatal("Invitation cursor absent")
		}
		next := request(t, client, token, "GET", base+"?limit=1&status=pending&cursor="+cursor, nil, "", 200)
		if len(next["items"].([]any)) != 1 {
			t.Fatal("Invitation cursor skipped next record")
		}
		request(t, client, token, "GET", base+"?status=expired&cursor="+cursor, nil, "", 400)
		request(t, other, otherToken, "GET", otherBase+"?status=pending&cursor="+cursor, nil, "", 400)
		request(t, client, token, "GET", base+"?status=invalid", nil, "", 400)
		request(t, client, token, "DELETE", roles+"/"+role+"?expected_version=1", nil, uuid.NewString(), 409)
	})
	t.Run("resend_rotation_and_positive_audit_rollback", func(t *testing.T) {
		path := base + "/" + id + "/resend"
		rk := uuid.NewString()
		r := request(t, client, token, "POST", path, map[string]any{"expected_version": 1}, rk, 200)
		if r["version"] != float64(2) || r["delivery_generation"] != float64(2) {
			t.Fatal("resend did not rotate generation")
		}
		request(t, client, token, "POST", path, map[string]any{"expected_version": 1}, rk, 200)
		request(t, client, token, "POST", path, map[string]any{"expected_version": 1}, uuid.NewString(), 409)
		_, err := e.owner.Exec(ctx, `CREATE SEQUENCE wr22_invitation_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_invitation_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_invitation_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.invitation.resent' THEN PERFORM nextval('wr22_invitation_fault_hits'); RAISE EXCEPTION 'isolated Invitation Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_invitation_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_invitation_fault()`)
		if err != nil {
			t.Fatal("own Audit fault setup")
		}
		defer func() {
			if _, err := e.owner.Exec(ctx, `DROP TRIGGER wr22_invitation_fault ON iam_audit_events; DROP FUNCTION wr22_invitation_fault(); DROP SEQUENCE wr22_invitation_fault_hits`); err != nil {
				t.Fatal("own Audit fault restoration")
			}
		}()
		request(t, client, token, "POST", path, map[string]any{"expected_version": 2}, uuid.NewString(), 503)
		var hit bool
		var version, outbox int
		if e.owner.QueryRow(ctx, `SELECT is_called FROM wr22_invitation_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("positive Audit fault not reached")
		}
		if e.owner.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2) FROM tenant_invitations WHERE tenant_id=$1 AND id=$2`, referenceTenants[0], id).Scan(&version, &outbox) != nil || version != 2 || outbox != 2 {
			t.Fatal("Audit failure left rotation or outbox")
		}
	})
	t.Run("concurrent_cancel_resend_one_transition", func(t *testing.T) {
		outcomes := make(chan int, 2)
		for _, op := range []string{"cancel", "resend"} {
			go func(op string) {
				code, _, _ := e.request(t, client, "POST", base+"/"+id+"/"+op, token, map[string]any{"expected_version": 2}, map[string]string{"Idempotency-Key": uuid.NewString()})
				outcomes <- code
			}(op)
		}
		wins, conflicts := 0, 0
		for i := 0; i < 2; i++ {
			switch <-outcomes {
			case 200, 204:
				wins++
			case 409:
				conflicts++
			default:
				t.Fatal("unexpected concurrent Invitation response")
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("concurrent Invitation transitions both committed")
		}
	})
	t.Run("current_authority_before_create_receipt", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2`, referenceTenants[0], e.humans[0]); err != nil {
			t.Fatal("own authority fault setup")
		}
		defer func() {
			if _, err := e.owner.Exec(ctx, `UPDATE tenant_memberships SET status='active',version=version+1,updated_at=now() WHERE tenant_id=$1 AND principal_id=$2`, referenceTenants[0], e.humans[0]); err != nil {
				t.Fatal("own authority restoration")
			}
		}()
		request(t, client, token, "POST", base, body, key, 403)
	})
	t.Run("synthetic_expiry_reconciles_list_and_role_delete", func(t *testing.T) {
		request(t, client, token, "POST", base, map[string]any{"email": "list-expiry@example.test", "role_ids": []string{e.adminRoles[0].String()}}, uuid.NewString(), 201)
		// Age only this run's pending rows using the migration owner. Re-enable the
		// immutable-history trigger before testing formal process behavior.
		tx, err := e.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own synthetic age transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_invitations DISABLE TRIGGER tenant_invitation_identity`); err != nil {
			t.Fatal("own synthetic age trigger pause")
		}
		if _, err = tx.Exec(ctx, `UPDATE tenant_invitations SET created_at=now()-interval '8 days',expires_at=now()-interval '1 day' WHERE tenant_id=$1 AND status='pending'`, referenceTenants[0]); err != nil {
			t.Fatal("own synthetic expiry input")
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE tenant_invitations ENABLE TRIGGER tenant_invitation_identity`); err != nil {
			t.Fatal("own synthetic age trigger restore")
		}
		if tx.Commit(ctx) != nil {
			t.Fatal("own synthetic age commit")
		}
		// Public delete must reconcile pending expiry even before an Invitation read.
		request(t, client, token, "DELETE", roles+"/"+role+"?expected_version=1", nil, uuid.NewString(), 204)
		page := request(t, client, token, "GET", base+"?status=expired", nil, "", 200)
		if len(page["items"].([]any)) < 1 {
			t.Fatal("expired history disappeared")
		}
		pending := request(t, client, token, "GET", base+"?status=pending", nil, "", 200)
		if len(pending["items"].([]any)) != 0 {
			t.Fatal("expired intent remained pending")
		}
		var refs int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND role_id=$2`, referenceTenants[0], role).Scan(&refs) != nil || refs != 0 {
			t.Fatal("expiry retained live role reference")
		}
	})
}
