//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestWR22FormalPlatformAdministrationReads(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	c := b.browser(t)
	location, state := b.beginOIDC(t, c, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("BOSS state mismatch")
	}
	status, body, _ := b.callback(t, c, code, state)
	if status != 303 {
		t.Fatalf("BOSS login status=%d reason=%v", status, body["code"])
	}
	status, body, _ = b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS refresh failed")
	}
	token, _ := body["access_token"].(string)
	get := func(t *testing.T, path string, want int) map[string]any {
		t.Helper()
		status, body, _ := b.request(t, c, "GET", path, token, nil, nil)
		if status != want {
			t.Fatalf("Platform GET %s status=%d reason=%v want=%d", path, status, body["code"], want)
		}
		return body
	}
	var membership, role string
	t.Run("real_owner_lists_and_gets_Platform_members_and_roles", func(t *testing.T) {
		v := get(t, "/iam/platform/members?limit=1", 200)
		items, _ := v["items"].([]any)
		if len(items) != 1 {
			t.Fatal("first Platform Membership missing")
		}
		m := items[0].(map[string]any)
		membership, _ = m["membership_id"].(string)
		boundary, _ := m["boundary"].(map[string]any)
		if boundary["type"] != "platform" || len(boundary) != 1 {
			t.Fatal("Platform Membership has Tenant fields")
		}
		get(t, "/iam/platform/members/"+membership, 200)
		v = get(t, "/iam/platform/roles?limit=1", 200)
		items, _ = v["items"].([]any)
		if len(items) != 1 {
			t.Fatal("first admin role missing")
		}
		r := items[0].(map[string]any)
		role, _ = r["role_id"].(string)
		if r["code"] != "platform-admin" || r["system"] != true {
			t.Fatal("builtin Platform role differs")
		}
		get(t, "/iam/platform/roles/"+role, 200)
	})
	if t.Failed() {
		return
	}
	t.Run("exact_catalog_cursor_and_target_TenantAccess", func(t *testing.T) {
		v := get(t, "/iam/platform/permissions?limit=1", 200)
		values, _ := v["permissions"].([]any)
		cursor, _ := v["next_cursor"].(string)
		if len(values) != 1 || cursor == "" {
			t.Fatal("Platform catalog cursor missing")
		}
		get(t, "/iam/platform/permissions?limit=1&cursor="+cursor, 200)
		get(t, "/iam/platform/roles?cursor="+cursor, 400)
		v = get(t, "/iam/tenants/"+referenceTenants[0].String()+"/access", 200)
		if v["tenant_id"] != referenceTenants[0].String() || v["status"] != "active" {
			t.Fatal("Platform TenantAccess target differs")
		}
		get(t, "/iam/tenants/"+mustV7(t).String()+"/access", 404)
	})
	t.Run("Tenant_IDs_do_not_read_Platform_relations", func(t *testing.T) {
		var id uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT id FROM tenant_memberships LIMIT 1`).Scan(&id) != nil {
			t.Fatal("own Tenant prerequisite missing")
		}
		get(t, "/iam/platform/members/"+id.String(), 404)
		get(t, "/iam/platform/roles/"+b.adminRoles[0].String(), 404)
	})
	t.Run("query_audit_failure_is_fail_closed", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_platform_read_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_platform_read_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_platform_read_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.getPlatformIAMMember' THEN PERFORM nextval('wr22_platform_read_fault_hits'); RAISE EXCEPTION 'isolated Platform read Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_platform_read_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_platform_read_fault()`)
		if err != nil {
			t.Fatal("own read audit fault failed")
		}
		get(t, "/iam/platform/members/"+membership, 503)
		var called bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_platform_read_fault_hits`).Scan(&called) != nil || !called {
			t.Fatal("read Audit fault not reached")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_platform_read_fault ON iam_audit_events; DROP FUNCTION wr22_platform_read_fault(); DROP SEQUENCE wr22_platform_read_fault_hits`); err != nil {
			t.Fatal("own read Audit restoration failed")
		}
		get(t, "/iam/platform/members/"+membership, 200)
	})
	t.Run("current_Permission_rechecked_on_every_management_read", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND resource='iam.platform-memberships' AND action='read'`, role); err != nil {
			t.Fatal("own permission fault failed")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.platform-memberships','read',now())`, role); err != nil {
				t.Error("own permission restore failed")
			}
		}()
		get(t, "/iam/platform/members", 403)
	})
	if !t.Failed() {
		var audits int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE boundary='platform' AND reason='PLATFORM_ADMIN_READ'`).Scan(&audits) != nil || audits < 6 {
			t.Fatal("Platform management read audit missing")
		}
		recordReference(t, b.run, map[string]any{"stage": "A10-read", "formal_Platform_management_reads": "pass", "audit_events": audits, "mutations": "not_verified", "summary": "member and role read via formal Gateway and IAM"})
	}
}
