//go:build integration && !governance

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWR23ResumeFormalLifecycleChain(t *testing.T) {
	wr23RunFormalLifecycleChain(t, newWR23FormalEnvironment(t))
}

type wr23FormalActors struct {
	tenant, operation       string
	admin, boss             *http.Client
	adminToken              string
	adminID                 uuid.UUID
	member                  *http.Client
	memberToken, memberRole string
	memberID                uuid.UUID
}

func wr23RunFormalLifecycleChain(t *testing.T, e *wr23FormalEnvironment) wr23FormalActors {
	t.Helper()
	started := time.Now().UTC()
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	bossCall := func(method, path string, payload any, want int) map[string]any {
		t.Helper()
		status, body, _ := b.request(t, browser, method, path, access, payload, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("formal BOSS %s %s status=%d reason=%v want=%d", method, path, status, body["code"], want)
		}
		return body
	}
	email := "wr23-invited-" + mustV7(t).String() + "@example.test"
	input := map[string]any{"name": "wr23-chain-" + mustV7(t).String()[:12], "display_name": "WR23 formal lifecycle", "email": "contact@example.test", "plan_id": e.plan, "admin_email": email, "admin_locale": "en-US", "idempotency_key": uuid.NewString()}
	bossCall("POST", "/admin/tenants", input, 403)
	members := bossCall("GET", "/iam/platform/members?limit=1", nil, 200)["items"].([]any)
	if len(members) != 1 {
		t.Fatal("first administrator did not create exactly one Platform membership")
	}
	member := members[0].(map[string]any)
	role := bossCall("POST", "/iam/platform/roles", map[string]any{"name": "WR23 Core tenant manager", "permissions": []string{"tenants/read", "tenants/create", "tenants/update"}}, 201)
	bossCall("POST", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role["role_id"], "expected_membership_version": member["version"]}, 204)
	created := bossCall("POST", "/admin/tenants", input, 200)
	tenant, ok := created["id"].(string)
	if !ok {
		t.Fatal("Core did not return Tenant ID")
	}
	operation, ok := created["bootstrap_operation_id"].(string)
	if !ok {
		t.Fatal("Core did not return Bootstrap operation")
	}
	if created["status"] != "active" || created["lifecycle_version"] != float64(1) {
		t.Fatal("invalid Core owner state")
	}
	var tenants, quotas, outbox int
	if err := e.core.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenants WHERE id=$1),(SELECT count(*) FROM resource_quota WHERE tenant_id=$1),(SELECT count(*) FROM core_integration_outbox WHERE tenant_id=$1)`, tenant).Scan(&tenants, &quotas, &outbox); err != nil || tenants != 1 || quotas != 2 || outbox != 2 {
		t.Fatal("Core local transaction missing Tenant/Quota/outbox")
	}
	var invitation, adminRole string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b.owner.QueryRow(ctx, `SELECT invitation_id,role_id FROM core_bootstrap_worker_results WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&invitation, &adminRole) == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if invitation == "" {
		t.Fatal("formal broker/worker did not create Bootstrap Invitation")
	}
	var authorityReceipts int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2`, e.broker.configuration.Authority.ConsumerID, tenant).Scan(&authorityReceipts) != nil || authorityReceipts != 2 {
		t.Fatal("missing independent durable broker authority receipts")
	}
	receipt := func(source, kind string) string {
		t.Helper()
		query := `SELECT notification_id,id::text,payload_ciphertext IS NULL FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2 AND status='delivered' ORDER BY delivery_generation DESC LIMIT 1`
		args := []any{tenant, source}
		scope, typeName, scopeID := "tenant", "iam_tenant_invitation", tenant
		if kind == "code" {
			query = `SELECT notification_id,id::text,payload_ciphertext IS NULL FROM iam_invited_account_outbox WHERE challenge_id=$1 AND status='delivered'`
			args = []any{source}
			scope, typeName, scopeID = "email_verification", "iam_email_verification", source
		}
		until := time.Now().Add(30 * time.Second)
		for time.Now().Before(until) {
			var id, request string
			var scrubbed bool
			if b.owner.QueryRow(ctx, query, args...).Scan(&id, &request, &scrubbed) == nil && id != "" {
				var notifications, deliveries int
				if !scrubbed || e.notification.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE notification_id=$1 AND source_id=$2 AND producer_id='ani-iam' AND scope_kind=$3 AND notification_type=$4 AND scope_id=$5 AND request_id=$6),(SELECT count(*) FROM deliveries WHERE notification_id=$1)`, id, source, scope, typeName, scopeID, request).Scan(&notifications, &deliveries) != nil || notifications != 1 || deliveries != 1 {
					t.Fatal("Notification durable receipt mismatch")
				}
				return id
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("formal identity delivery receipt timeout kind=%s", kind)
		return ""
	}
	call := func(client *http.Client, token, method, path string, payload any, want int) map[string]any {
		t.Helper()
		status, body, _ := e.console.request(t, client, method, path, token, payload, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("formal Console %s %s status=%d reason=%v want=%d", method, path, status, body["code"], want)
		}
		return body
	}
	activate := func(email, invitation string) (*http.Client, string, uuid.UUID) {
		t.Helper()
		client := e.console.browser(t)
		invitationToken := e.notification.mailValue(t, email, receipt(invitation, "tenant"), "tenant")
		challenge := call(client, "", "POST", "/auth/invited-account-verifications", map[string]string{"account": email}, 202)["challenge_id"].(string)
		code := e.notification.mailValue(t, email, receipt(challenge, "code"), "code")
		call(client, "", "POST", "/auth/invited-accounts", map[string]string{"account": email, "challenge_id": challenge, "verification_code": code, "new_password": e.console.password}, 204)
		var human uuid.UUID
		var authorities int
		if b.owner.QueryRow(ctx, `SELECT principal_id FROM verified_emails WHERE normalized_email=$1`, email).Scan(&human) != nil {
			t.Fatal("formal activated Human missing")
		}
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE principal_id=$1)+(SELECT count(*) FROM platform_memberships WHERE principal_id=$1)+(SELECT count(*) FROM sessions WHERE principal_id=$1)`, human).Scan(&authorities) != nil || authorities != 0 {
			t.Fatal("account activation granted Membership or Session")
		}
		accepted := call(client, "", "POST", "/auth/invitation-acceptances", map[string]string{"account": email, "password": e.console.password, "invitation_token": invitationToken}, 200)
		if accepted["boundary"] != "tenant" || accepted["tenant_id"] != tenant || accepted["access_token"] != nil {
			t.Fatal("invitation acceptance violated independent boundary/Session rule")
		}
		token := call(client, "", "POST", "/auth/password/login", map[string]any{"account": email, "password": e.console.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant}}, 200)["access_token"].(string)
		call(client, token, "GET", "/iam/tenants/"+tenant+"/members", nil, 200)
		return client, token, human
	}
	admin, adminToken, adminID := activate(email, invitation)
	base := "/iam/tenants/" + tenant
	readerRole := call(admin, adminToken, "POST", base+"/roles", map[string]any{"name": "WR23 ordinary reader", "permissions": []string{"iam.memberships/read"}}, 201)["role_id"].(string)
	secondEmail := "wr23-member-" + mustV7(t).String() + "@example.test"
	secondInvitation := call(admin, adminToken, "POST", base+"/invitations", map[string]any{"email": secondEmail, "role_ids": []string{readerRole}}, 201)["invitation_id"].(string)
	second, secondToken, secondID := activate(secondEmail, secondInvitation)
	call(second, secondToken, "POST", base+"/invitations", map[string]any{"email": "forbidden@example.test", "role_ids": []string{readerRole}}, 403)
	var operationStatus string
	if b.owner.QueryRow(ctx, `SELECT status FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2`, tenant, operation).Scan(&operationStatus) != nil || operationStatus != "succeeded" {
		t.Fatal("original Bootstrap did not complete after accepted invitation")
	}
	result := map[string]any{"result": "pass", "grade": "formal Core Tenant/Quota/outbox to actual NATS and IAM Broker authority/worker, Notification/SMTP, invited account activation, separate acceptance, login/protected read and second member", "started_at": started, "finished_at": time.Now().UTC(), "tenant_id": tenant, "operation_id": operation, "administrator_id": adminID, "second_member_id": secondID, "bootstrap_role_id": adminRole, "broker_authority_receipts": authorityReceipts, "projection_mode": e.projectionMode, "enforcement": "not_verified", "active_24h": "not_started"}
	raw, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filepath.Join(b.run, "formal-chain-results.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return wr23FormalActors{tenant: tenant, operation: operation, admin: admin, boss: browser, adminToken: adminToken, adminID: adminID, member: second, memberToken: secondToken, memberRole: readerRole, memberID: secondID}
}
