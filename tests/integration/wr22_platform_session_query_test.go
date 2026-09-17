//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestWR22FormalPlatformSessionQuery(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	var token string
	for i := 0; i < 2; i++ {
		browser := b.browser(t)
		location, state := b.beginOIDC(t, browser, "")
		code, returned := b.dexAuthorize(t, location)
		if state != returned {
			t.Fatal("state differs")
		}
		status, _, _ := b.callback(t, browser, code, state)
		if status != 303 {
			t.Fatal("formal BOSS login failed")
		}
		status, body, _ := b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("formal BOSS refresh failed")
		}
		token, _ = body["access_token"].(string)
	}
	b.wr22Environment.login(t, 0)
	browser := b.browser(t)
	get := func(t *testing.T, path string, want int) map[string]any {
		t.Helper()
		status, body, _ := b.request(t, browser, "GET", path, token, nil, nil)
		if status != want {
			t.Fatalf("BOSS Session list status=%d reason=%v want=%d", status, body["code"], want)
		}
		return body
	}
	var human uuid.UUID
	if b.owner.QueryRow(ctx, `SELECT principal_id FROM platform_memberships`).Scan(&human) != nil {
		t.Fatal("own Platform identity missing")
	}
	seen := map[string]bool{}
	t.Run("real_BOSS_sessions_have_explicit_Platform_grants_and_owned_pagination", func(t *testing.T) {
		path := "/auth/sessions?limit=1"
		for i := 0; i < 2; i++ {
			v := get(t, path, 200)
			items, _ := v["items"].([]any)
			if len(items) != 1 {
				t.Fatal("BOSS Session page cardinality")
			}
			s := items[0].(map[string]any)
			id, _ := s["session_id"].(string)
			if id == "" || seen[id] {
				t.Fatal("unstable BOSS Session cursor")
			}
			seen[id] = true
			var owner uuid.UUID
			if b.owner.QueryRow(ctx, `SELECT principal_id FROM sessions WHERE id=$1`, id).Scan(&owner) != nil || owner != human {
				t.Fatal("other Human Session exposed")
			}
			grants, _ := s["grants"].([]any)
			if len(grants) != 1 {
				t.Fatal("BOSS Grant summary missing")
			}
			g := grants[0].(map[string]any)
			boundary, _ := g["boundary"].(map[string]any)
			if boundary["type"] != "platform" || len(boundary) != 1 {
				t.Fatal("BOSS Grant mixed with Tenant")
			}
			cursor, _ := v["next_cursor"].(string)
			if i == 0 && cursor == "" {
				t.Fatal("missing bounded cursor")
			}
			if i == 1 && cursor != "" {
				t.Fatal("foreign Session leaked into pagination")
			}
			path = "/auth/sessions?limit=1&cursor=" + cursor
		}
		get(t, "/auth/sessions?cursor=not-a-cursor", 400)
	})
	t.Run("own_Session_query_requires_current_Membership_without_management_permission", func(t *testing.T) {
		var role uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin'`).Scan(&role) != nil {
			t.Fatal("own builtin role missing")
		}
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND resource='iam.platform-memberships' AND action='read'`, role); err != nil {
			t.Fatal("own permission fault")
		}
		get(t, "/auth/sessions", 200)
		if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.platform-memberships','read',now())`, role); err != nil {
			t.Fatal("own permission restoration")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='suspended' WHERE principal_id=$1`, human); err != nil {
			t.Fatal("own Membership fault")
		}
		get(t, "/auth/sessions", 401)
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='active' WHERE principal_id=$1`, human); err != nil {
			t.Fatal("own Membership restoration")
		}
		get(t, "/auth/sessions", 200)
	})
	t.Run("PG_authority_read_failure_is_not_empty_success", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `REVOKE SELECT ON platform_session_grants FROM ani_iam_runtime`); err != nil {
			t.Fatal("own PG fault")
		}
		get(t, "/auth/sessions", 503)
		if _, err := b.owner.Exec(ctx, `GRANT SELECT ON platform_session_grants TO ani_iam_runtime`); err != nil {
			t.Fatal("own PG restoration")
		}
		get(t, "/auth/sessions", 200)
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A13", "formal_BOSS_own_Session_query": "pass", "real_BOSS_sessions": 2, "foreign_Console_Human": "formal password login", "Platform_grants": "explicit boundary without Tenant ID"})
	}
}
