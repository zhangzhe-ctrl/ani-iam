//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"net/http"
	"testing"
)

func TestWR22FormalTenantAdministratorLogin(t *testing.T) {
	e := newWR22Environment(t)
	ctx := context.Background()
	firstBrowser, firstToken, _, _ := e.login(t, 0)
	tenant := referenceTenants[0]
	base := "/iam/tenants/" + tenant.String() + "/members/"
	var first uuid.UUID
	if e.owner.QueryRow(ctx, `SELECT id FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenant, e.humans[0]).Scan(&first) != nil {
		t.Fatal("own first membership")
	}
	call := func(t *testing.T, c *http.Client, token, method, path string, payload any, want int) {
		t.Helper()
		s, b, _ := e.request(t, c, method, path, token, payload, map[string]string{"Idempotency-Key": uuid.NewString()})
		if s != want {
			t.Fatalf("%s %s status=%d reason=%v want=%d", method, path, s, b["code"], want)
		}
	}
	t.Run("another_Tenant_administrator_does_not_count", func(t *testing.T) {
		call(t, firstBrowser, firstToken, "PATCH", base+first.String(), map[string]any{"status": "suspended", "expected_version": 1}, 403)
	})
	second := mustV7(t)
	human := e.humans[1]
	// This additional relation is an explicit own-test prerequisite. Invitation
	// acceptance will later supply the product creation path independently.
	if _, err := e.owner.Exec(ctx, `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, tenant, second, human); err != nil {
		t.Fatal("own second membership prerequisite")
	}
	call(t, firstBrowser, firstToken, "POST", base+second.String()+"/role-bindings", map[string]any{"role_id": e.adminRoles[0].String(), "expected_membership_version": 1}, 204)
	secondBrowser := e.browser(t)
	s, b, _ := e.request(t, secondBrowser, "POST", "/auth/password/login", "", map[string]any{"account": e.accounts[1], "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, map[string]string{"Idempotency-Key": uuid.NewString()})
	if s != 200 {
		t.Fatalf("second real Tenant password login status=%d reason=%v", s, b["code"])
	}
	secondToken, _ := b["access_token"].(string)
	t.Run("disabled_password_and_untrusted_issuer_cannot_preserve_admin_capacity", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `UPDATE identities SET status='disabled' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
			t.Fatal("own identity fault")
		}
		call(t, firstBrowser, firstToken, "PATCH", base+first.String(), map[string]any{"status": "suspended", "expected_version": 1}, 403)
		if _, err := e.owner.Exec(ctx, `INSERT INTO identities(id,principal_id,provider,issuer,subject,status,version,created_at,updated_at) VALUES($1,$2,'dex','https://untrusted.wr22.test','tenant-guard','active',1,now(),now())`, mustV7(t), human); err != nil {
			t.Fatal("own untrusted identity fixture")
		}
		call(t, firstBrowser, firstToken, "DELETE", base+first.String()+"/role-bindings/"+e.adminRoles[0].String()+"?expected_membership_version=1", nil, 403)
		if _, err := e.owner.Exec(ctx, `UPDATE identities SET status='active' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
			t.Fatal("own identity restoration")
		}
	})
	t.Run("durable_password_lockout_does_not_count_as_available_login", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=now()+interval '15 minutes' WHERE principal_id=$1`, human); err != nil {
			t.Fatal("own lockout fixture")
		}
		call(t, firstBrowser, firstToken, "PATCH", base+first.String(), map[string]any{"status": "suspended", "expected_version": 1}, 403)
		if _, err := e.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=NULL WHERE principal_id=$1`, human); err != nil {
			t.Fatal("own lockout restoration")
		}
	})
	t.Run("two_real_password_sessions_cannot_concurrently_suspend_both_admins", func(t *testing.T) {
		type result struct {
			id     uuid.UUID
			status int
		}
		out := make(chan result, 2)
		go func() {
			s, _, _ := e.request(t, firstBrowser, "PATCH", base+first.String(), firstToken, map[string]any{"status": "suspended", "expected_version": 1}, map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- result{first, s}
		}()
		go func() {
			s, _, _ := e.request(t, secondBrowser, "PATCH", base+second.String(), secondToken, map[string]any{"status": "suspended", "expected_version": 2}, map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- result{second, s}
		}()
		a, z := <-out, <-out
		counts := map[int]int{a.status: 1}
		counts[z.status]++
		if counts[200] != 1 || counts[403] != 1 {
			t.Fatal("Tenant guard did not preserve exactly one capable administrator")
		}
		target := a.id
		if a.status != 200 {
			target = z.id
		}
		var version int
		if e.owner.QueryRow(ctx, `SELECT version FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, tenant, target).Scan(&version) != nil {
			t.Fatal("own restoration version")
		}
		actorBrowser, actorToken := firstBrowser, firstToken
		if target == first {
			actorBrowser, actorToken = secondBrowser, secondToken
		}
		call(t, actorBrowser, actorToken, "PATCH", base+target.String(), map[string]any{"status": "active", "expected_version": version}, 200)
	})
	if !t.Failed() {
		recordReference(t, e.run, map[string]any{"stage": "A16", "Tenant_login_capable_last_admin": "pass", "two_administrators": "real formal password Sessions; additional Membership SQL prerequisite", "foreign_Tenant_untrusted_identity_disabled_and_locked_password": "pass"})
	}
}
