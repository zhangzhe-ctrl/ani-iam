//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestWR22FormalPlatformAudit(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	browser := b.browser(t)
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("state differs")
	}
	status, _, _ := b.callback(t, browser, code, state)
	if status != 303 {
		t.Fatal("formal first admin login failed")
	}
	status, body, _ := b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS refresh prerequisite")
	}
	token, _ := body["access_token"].(string)
	call := func(t *testing.T, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		status, body, response := b.request(t, browser, method, path, token, payload, headers)
		if status != want {
			t.Fatalf("%s %s status=%d reason=%v want=%d", method, path, status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("Audit response is cacheable")
		}
		return body
	}
	const audit = "/iam/platform/audit-events"
	var first, initialization string
	if b.owner.QueryRow(ctx, `SELECT id FROM platform_memberships`).Scan(&first) != nil || b.owner.QueryRow(ctx, `SELECT event_id FROM iam_audit_events WHERE authentication_method='administrator_provisioner' LIMIT 1`).Scan(&initialization) != nil {
		t.Fatal("own first admin evidence prerequisite")
	}
	t.Run("ordinary_admin_has_no_implicit_Auditor_authority", func(t *testing.T) {
		call(t, "GET", audit, "", nil, 403)
		call(t, "GET", audit+"/"+initialization, "", nil, 403)
	})
	roleBody := call(t, "POST", "/iam/platform/roles", uuid.NewString(), map[string]any{"name": "WR22 explicit Platform Auditor", "permissions": []string{"iam.audit-events/read"}}, 201)
	role, _ := roleBody["role_id"].(string)
	call(t, "POST", "/iam/platform/members/"+first+"/role-bindings", uuid.NewString(), map[string]any{"role_id": role, "expected_membership_version": 1}, 204)
	b.wr22Environment.login(t, 0)
	b.wr22Environment.login(t, 0)
	b.wr22Environment.login(t, 1)
	unknownKey := uuid.NewString()
	status, _, _ = b.wr22Environment.request(t, b.wr22Environment.browser(t), "POST", "/auth/password-actions", "", map[string]any{"account": "wr22-unknown-" + uuid.NewString() + "@example.test", "audience": "console"}, map[string]string{"Idempotency-Key": unknownKey})
	if status != 202 {
		t.Fatalf("formal Console anonymous password-action event status=%d", status)
	}
	var principalEvent, roleEvent string
	if b.owner.QueryRow(ctx, `SELECT event_id FROM iam_audit_events WHERE boundary='principal' AND request_id=$1`, unknownKey).Scan(&principalEvent) != nil {
		t.Fatal("formal global password-action Audit missing")
	}
	if b.owner.QueryRow(ctx, `SELECT event_id FROM iam_audit_events WHERE target_id=$1 AND action='iam.platform.createPlatformIAMRole'`, role).Scan(&roleEvent) != nil {
		t.Fatal("formal role Audit missing")
	}
	t.Run("actual_event_boundaries_and_distinct_initialization_provenance", func(t *testing.T) {
		v := call(t, "GET", audit+"/"+initialization, "", nil, 200)
		if v["actor_type"] != "initialization" || v["authentication_method"] != "administrator_provisioner" || v["actor_principal_id"] != nil {
			t.Fatal("initialization was relabelled as a Principal or anonymous login")
		}
		v = call(t, "GET", audit+"/"+principalEvent, "", nil, 200)
		boundary, _ := v["boundary"].(map[string]any)
		if boundary["type"] != "principal" || len(boundary) != 1 || v["actor_type"] != "anonymous" {
			t.Fatal("Human-global anonymous event context differs")
		}
		v = call(t, "GET", audit+"/"+roleEvent, "", nil, 200)
		boundary, _ = v["boundary"].(map[string]any)
		caller, _ := v["caller"].(map[string]any)
		if boundary["type"] != "platform" || len(boundary) != 1 || v["target_version"] != float64(1) || caller["principal_id"] == nil || caller["binding_id"] == nil || caller["binding_version"] == nil || caller["grant_version"] == nil {
			t.Fatal("Platform event attribution missing")
		}
		raw, _ := json.Marshal(v)
		for _, secret := range []string{token, b.password, "password_hash", "request_hash", "action_url"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("Audit contains non-allowlisted credential material")
			}
		}
		call(t, "GET", audit+"/"+mustV7(t).String(), "", nil, 404)
	})
	t.Run("exact_Tenant_action_result_filter_and_cursor", func(t *testing.T) {
		base := audit + "?tenant_id=" + referenceTenants[0].String() + "&action=iam.password.login.succeeded&result=success&limit=1"
		v := call(t, "GET", base, "", nil, 200)
		items, _ := v["items"].([]any)
		cursor, _ := v["next_cursor"].(string)
		if len(items) != 1 || cursor == "" {
			t.Fatal("filtered Audit page missing")
		}
		first := items[0].(map[string]any)
		boundary, _ := first["boundary"].(map[string]any)
		if boundary["type"] != "tenant" || boundary["tenant_id"] != referenceTenants[0].String() {
			t.Fatal("Tenant filter returned another context")
		}
		v = call(t, "GET", base+"&cursor="+cursor, "", nil, 200)
		items, _ = v["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["event_id"] == first["event_id"] {
			t.Fatal("Audit cursor repeated or lost data")
		}
		call(t, "GET", strings.Replace(base, referenceTenants[0].String(), referenceTenants[1].String(), 1)+"&cursor="+cursor, "", nil, 400)
		call(t, "GET", audit+"?tenant_id=invalid", "", nil, 400)
		v = call(t, "GET", audit+"?tenant_id="+mustV7(t).String(), "", nil, 200)
		if len(v["items"].([]any)) != 0 {
			t.Fatal("missing Tenant filter widened query")
		}
	})
	t.Run("query_Audit_failure_is_confirmed_and_fail_closed", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_auditor_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_auditor_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_auditor_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.getPlatformIAMSecurityAuditEvent' THEN PERFORM nextval('wr22_auditor_fault_hits'); RAISE EXCEPTION 'isolated auditor query fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_auditor_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_auditor_fault()`)
		if err != nil {
			t.Fatal("own query Audit fault setup")
		}
		call(t, "GET", audit+"/"+roleEvent, "", nil, 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_auditor_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("query Audit fault not reached")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_auditor_fault ON iam_audit_events; DROP FUNCTION wr22_auditor_fault(); DROP SEQUENCE wr22_auditor_fault_hits`); err != nil {
			t.Fatal("own query Audit restoration")
		}
		call(t, "GET", audit+"/"+roleEvent, "", nil, 200)
	})
	t.Run("explicit_Auditor_binding_removal_affects_next_query", func(t *testing.T) {
		call(t, "DELETE", "/iam/platform/members/"+first+"/role-bindings/"+role+"?expected_membership_version=2", uuid.NewString(), nil, 204)
		call(t, "GET", audit, "", nil, 403)
		call(t, "GET", audit+"/"+roleEvent, "", nil, 403)
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A14", "formal_Platform_Auditor_query": "pass", "Tenant_and_Principal_event_contexts": "pass", "explicit_Auditor_binding": "formal Role and Binding APIs", "retention": "A4 synthetic 180/181-day gate repeated; production retention not_verified"})
	}
}
