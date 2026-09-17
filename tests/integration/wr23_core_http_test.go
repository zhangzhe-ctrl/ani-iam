//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// This gate proves formal HTTP/Core and current IAM authorization. It does not
// seed a projection or claim NATS/bootstrap delivery before the WR23 broker gate.
func TestWR23FormalCoreHTTP(t *testing.T) {
	e := newWR23CoreEnvironment(t)
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	decision, err := b.iam.auth.ValidatePrincipal(ctx, &iamv1.ValidatePrincipalRequest{Credential: &iamv1.BearerCredential{Value: access}, OperationId: "listTenants", PolicyRevision: data.TargetPolicyRevision})
	if err != nil || decision.GetPrincipal().GetBoundary().GetPlatform() == nil || decision.GetPrincipal().GetBoundary().GetTenant() != nil {
		t.Fatal("formal BOSS is not a Platform principal")
	}
	actor := decision.GetPrincipal().GetPrincipalId()
	countCore := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := e.core.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal("Core evidence query failed")
		}
		return n
	}
	countIAM := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := b.owner.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal("IAM evidence query failed")
		}
		return n
	}
	request := func(t *testing.T, method, path, token string, body any, want int) map[string]any {
		t.Helper()
		status, doc, response := b.request(t, browser, method, path, token, body, map[string]string{"X-ANI-Actor-User-ID": uuid.NewString(), "X-ANI-Tenant-ID": uuid.NewString()})
		if status != want {
			t.Fatalf("%s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		if want == 200 && response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("Core response must not be cached")
		}
		return doc
	}
	input := map[string]any{"name": "wr23-http-tenant", "display_name": "WR23 Core Tenant", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "wr23-admin@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}
	var created map[string]any
	var tenant string
	t.Run("explicit_Core_permission_role_before_tenant_creation", func(t *testing.T) {
		request(t, "POST", "/admin/tenants", access, input, 403)
		if countCore(`SELECT count(*) FROM tenants`) != 0 {
			t.Fatal("unauthorized actor created a Tenant")
		}
		status, members, _ := b.request(t, browser, "GET", "/iam/platform/members?limit=1", access, nil, nil)
		items, _ := members["items"].([]any)
		if status != 200 || len(items) != 1 {
			t.Fatal("formal first administrator membership lookup")
		}
		member := items[0].(map[string]any)
		membership, _ := member["membership_id"].(string)
		status, role, _ := b.request(t, browser, "POST", "/iam/platform/roles", access, map[string]any{"name": "WR23 Core tenant manager", "permissions": []string{"tenants/read", "tenants/create", "tenants/update"}}, nil)
		roleID, _ := role["role_id"].(string)
		if status != 201 || roleID == "" {
			t.Fatalf("formal Core Role creation status=%d reason=%v", status, role["code"])
		}
		status, bound, _ := b.request(t, browser, "POST", "/iam/platform/members/"+membership+"/role-bindings", access, map[string]any{"role_id": roleID, "expected_membership_version": member["version"]}, nil)
		if status != 204 {
			t.Fatalf("formal Core Role Binding status=%d reason=%v", status, bound["code"])
		}
		recordReference(t, b.run, map[string]any{"kind": "wr23-Core-permission-binding", "membership_id": membership, "role_id": roleID, "permissions": []string{"tenants/read", "tenants/create", "tenants/update"}, "formal_IAM_role_and_binding": true})
	})
	if t.Failed() {
		return
	}
	t.Run("formal_platform_creates_only_core_owned_state", func(t *testing.T) {
		created = request(t, "POST", "/admin/tenants", access, input, 200)
		tenant, _ = created["id"].(string)
		id, err := uuid.Parse(tenant)
		if err != nil || id.Version() != 7 || created["status"] != "active" || created["lifecycle_version"] != float64(1) || created["bootstrap_operation_id"] == "" {
			t.Fatal("invalid formal owner response")
		}
		if countCore(`SELECT count(*) FROM tenants WHERE id=$1`, tenant) != 1 || countCore(`SELECT count(*) FROM resource_quota WHERE tenant_id=$1`, tenant) != 2 || countCore(`SELECT count(*) FROM core_integration_outbox WHERE tenant_id=$1`, tenant) != 2 {
			t.Fatal("Core local atomic initialization is incomplete")
		}
		if countCore(`SELECT count(*) FROM core_tenant_audit WHERE tenant_id=$1 AND actor_id=$2 AND action='create'`, tenant, actor) != 1 {
			t.Fatal("Core audit actor did not come from current IAM decision")
		}
		if countIAM(`SELECT count(*) FROM principals WHERE principal_type='human'`) != 1 || countIAM(`SELECT count(*) FROM platform_memberships`) != 1 || countIAM(`SELECT count(*) FROM tenant_access`) != 0 || countIAM(`SELECT count(*) FROM tenant_memberships`) != 0 {
			t.Fatal("Core create wrote IAM identity or access without its consumer")
		}
		if countCore(`SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename IN ('users','user_roles','roles','tenant_auth','platform_users')`) != 0 {
			t.Fatal("target Core constructed legacy identity tables")
		}
		recordReference(t, b.run, map[string]any{"kind": "wr23-core-create", "tenant_id": tenant, "actor_id": actor, "bootstrap_operation_id": created["bootstrap_operation_id"], "tenant_quota_outbox_audit_atomic": true, "iam_projection_seed": false})
	})
	if t.Failed() {
		return
	}
	t.Run("retry_rechecks_permission_and_core_receipt", func(t *testing.T) {
		replay := request(t, "POST", "/admin/tenants", access, input, 200)
		if !reflect.DeepEqual(created, replay) {
			t.Fatal("Core idempotent receipt changed")
		}
		conflicting := map[string]any{}
		for k, v := range input {
			conflicting[k] = v
		}
		conflicting["admin_email"] = "different@example.test"
		request(t, "POST", "/admin/tenants", access, conflicting, 409)
		if countCore(`SELECT count(*) FROM core_tenant_operations WHERE tenant_id=$1`, tenant) != 1 {
			t.Fatal("retry or conflict created another operation")
		}
	})
	t.Run("current_core_queries_and_explicit_lifecycle_CAS", func(t *testing.T) {
		got := request(t, "GET", "/admin/tenants/"+tenant, access, nil, 200)
		for _, field := range []string{"user_count", "admin_count", "auth", "roles", "memberships"} {
			if _, present := got[field]; present {
				t.Fatal("Core view contains IAM data")
			}
		}
		list := request(t, "GET", "/admin/tenants?limit=1&search=CORE&status=active", access, nil, 200)
		items, ok := list["items"].([]any)
		if !ok || len(items) != 1 || items[0].(map[string]any)["id"] != tenant || list["next_cursor"] != nil {
			t.Fatal("Core list query differs from owner state")
		}
		for index, action := range []string{"freeze", "unfreeze", "disable"} {
			version := index + 1
			body := map[string]any{"version": version, "idempotency_key": uuid.NewString()}
			result := request(t, "POST", "/admin/tenants/"+tenant+"/"+action, access, body, 200)
			expected := []string{"frozen", "active", "disabled"}[index]
			if result["status"] != expected || result["lifecycle_version"] != float64(version+1) {
				t.Fatal("Core lifecycle intent result changed")
			}
			if !reflect.DeepEqual(result, request(t, "POST", "/admin/tenants/"+tenant+"/"+action, access, body, 200)) {
				t.Fatal("lifecycle retry was not stable")
			}
		}
		request(t, "POST", "/admin/tenants/"+tenant+"/freeze", access, map[string]any{"version": 1, "idempotency_key": uuid.NewString()}, 409)
		request(t, "POST", "/admin/tenants/"+tenant+"/unfreeze", access, map[string]any{"version": 4, "idempotency_key": uuid.NewString()}, 409)
		if countCore(`SELECT count(*) FROM core_tenant_operations WHERE tenant_id=$1`, tenant) != 4 || countCore(`SELECT count(*) FROM core_tenant_audit WHERE tenant_id=$1 AND actor_id=$2`, tenant, actor) != 4 || countCore(`SELECT count(*) FROM core_integration_outbox WHERE tenant_id=$1`, tenant) != 5 {
			t.Fatal("Core mutation receipts, audit and events disagree")
		}
	})
	t.Run("formal_restart_preserves_owner_receipt_and_heartbeat", func(t *testing.T) {
		e.gateway.stop()
		e.gateway = e.startGateway()
		if !reflect.DeepEqual(created, request(t, "POST", "/admin/tenants", access, input, 200)) {
			t.Fatal("Gateway restart lost Core receipt")
		}
		start := time.Now()
		before := countCore(`SELECT count(*) FROM core_integration_outbox WHERE tenant_id IS NULL`)
		deadline := time.Now().Add(14 * time.Second)
		for time.Now().Before(deadline) && countCore(`SELECT count(*) FROM core_integration_outbox WHERE tenant_id IS NULL`) == before {
			time.Sleep(200 * time.Millisecond)
		}
		if countCore(`SELECT count(*) FROM core_integration_outbox WHERE tenant_id IS NULL`) <= before {
			t.Fatal("formal Core heartbeat did not commit")
		}
		recordReference(t, b.run, map[string]any{"kind": "wr23-formal-heartbeat", "real_elapsed_ms": time.Since(start).Milliseconds(), "broker_delivery_verified": false})
	})
	t.Run("logout_revokes_cached_create_and_fresh_session_can_retry", func(t *testing.T) {
		status, doc, _ := b.request(t, browser, "POST", "/auth/logout", "", nil, nil)
		if status != http.StatusNoContent {
			t.Fatalf("formal logout status=%d reason=%v", status, doc["code"])
		}
		denied, err := b.iam.authorization.CheckPermission(ctx, &iamv1.CheckPermissionRequest{Credential: &iamv1.BearerCredential{Value: access}, OperationId: "createTenant", PolicyRevision: data.TargetPolicyRevision, Target: &iamv1.AuthorizationTarget{}})
		if err != nil || denied.GetDecision().GetAllowed() || denied.GetDecision().GetReason() != "SESSION_INACTIVE" {
			t.Fatal("formal CheckPermission did not observe current Session revocation")
		}
		recordReference(t, b.run, map[string]any{"kind": "wr23-revoked-Core-request", "decision_id": denied.GetDecision().GetDecisionId(), "reason": denied.GetDecision().GetReason(), "http_status": 403})
		request(t, "POST", "/admin/tenants", access, input, 403)
		if countCore(`SELECT count(*) FROM core_tenant_operations WHERE tenant_id=$1`, tenant) != 4 {
			t.Fatal("revoked session changed owner state")
		}
		access = e.login(t, browser)
		if !reflect.DeepEqual(created, request(t, "POST", "/admin/tenants", access, input, 200)) {
			t.Fatal("new session for same actor did not recover original owner receipt")
		}
	})
	t.Run("target_rejects_legacy_identity_input_and_unregistered_owner", func(t *testing.T) {
		request(t, "POST", "/admin/tenants", access, map[string]any{"admin_password": "not-an-accepted-field"}, 400)
		request(t, "GET", "/admin/tenants/"+tenant+"/auth", access, nil, 404)
		request(t, "GET", "/instances", access, nil, 404)
		if countIAM(`SELECT count(*) FROM tenant_access`) != 0 || countIAM(`SELECT count(*) FROM tenant_memberships`) != 0 {
			t.Fatal("formal HTTP gate silently seeded an IAM projection")
		}
	})
}
