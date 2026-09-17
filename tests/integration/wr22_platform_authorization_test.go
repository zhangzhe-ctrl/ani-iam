//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWR22FormalPlatformOnlineAuthorization(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	browser := b.browser(t)
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if returned != state {
		t.Fatal("BOSS state mismatch")
	}
	httpStatus, body, _ := b.callback(t, browser, code, state)
	if httpStatus != 303 {
		t.Fatalf("BOSS callback status=%d reason=%v", httpStatus, body["code"])
	}
	httpStatus, body, _ = b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
	if httpStatus != 200 {
		t.Fatalf("BOSS refresh status=%d reason=%v", httpStatus, body["code"])
	}
	access, _ := body["access_token"].(string)
	check := func(t *testing.T, operation, credential string) *iamv1.AuthorizationDecision {
		t.Helper()
		c, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		v, err := b.iam.authorization.CheckPermission(c, &iamv1.CheckPermissionRequest{Credential: &iamv1.BearerCredential{Value: credential}, OperationId: operation, PolicyRevision: data.TargetPolicyRevision, Target: &iamv1.AuthorizationTarget{}})
		if err != nil {
			t.Fatalf("formal Platform CheckPermission status=%s", status.Code(err))
		}
		if v.GetDecision().GetDecisionId() == "" || v.GetDecision().GetPolicyRevision() != data.TargetPolicyRevision {
			t.Fatal("missing exact policy decision")
		}
		return v.GetDecision()
	}
	var principal, grant uuid.UUID
	t.Run("real_BOSS_credential_authenticates_explicit_Platform", func(t *testing.T) {
		v, err := b.iam.auth.ValidatePrincipal(ctx, &iamv1.ValidatePrincipalRequest{Credential: &iamv1.BearerCredential{Value: access}, OperationId: "listPlatformIAMMembers", PolicyRevision: data.TargetPolicyRevision})
		if err != nil || v.GetPrincipal().GetBoundary().GetPlatform() == nil || v.GetPrincipal().GetBoundary().GetTenant() != nil {
			t.Fatalf("Platform ValidatePrincipal status=%s", status.Code(err))
		}
		principal = uuid.MustParse(v.GetPrincipal().GetPrincipalId())
		grant = uuid.MustParse(v.GetPrincipal().GetGrantId())
		decision := check(t, "listPlatformIAMMembers", access)
		if !decision.GetAllowed() || decision.GetPrincipal().GetBoundary().GetPlatform() == nil {
			t.Fatal("ordinary Platform permission denied")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("ordinary_admin_has_no_recovery_or_Auditor_authority", func(t *testing.T) {
		for _, op := range []string{"requestRecoveryBootstrap", "approveRecoveryBootstrap", "executeRecoveryBootstrap", "requestRestoreTenantAdmin", "listPlatformIAMSecurityAuditEvents"} {
			d := check(t, op, access)
			if d.GetAllowed() || d.GetReason() != "PERMISSION_DENIED" {
				t.Fatalf("dedicated capability unexpectedly granted for %s", op)
			}
		}
	})
	t.Run("Console_credential_cannot_enter_Platform", func(t *testing.T) {
		c := b.wr22Environment.browser(t)
		code, body, _ := b.wr22Environment.request(t, c, http.MethodPost, "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, nil)
		if code != 200 {
			t.Fatalf("Console prerequisite status=%d reason=%v", code, body["code"])
		}
		token, _ := body["access_token"].(string)
		_, err := b.iam.authorization.CheckPermission(ctx, &iamv1.CheckPermissionRequest{Credential: &iamv1.BearerCredential{Value: token}, OperationId: "listPlatformIAMMembers", PolicyRevision: data.TargetPolicyRevision, Target: &iamv1.AuthorizationTarget{}})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("Console-to-Platform status=%s", status.Code(err))
		}
	})
	t.Run("permission_removal_affects_next_decision_without_cache", func(t *testing.T) {
		var role uuid.UUID
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin'`).Scan(&role) != nil {
			t.Fatal("own admin role missing")
		}
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND scope='platform' AND resource='iam.platform-memberships' AND action='read'`, role); err != nil {
			t.Fatal("own permission fault failed")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.platform-memberships','read',now())`, role); err != nil {
				t.Error("own permission restoration failed")
			}
		}()
		d := check(t, "listPlatformIAMMembers", access)
		if d.GetAllowed() || d.GetReason() != "PERMISSION_DENIED" {
			t.Fatal("cached role authority survived removal")
		}
	})
	t.Run("current_membership_and_Grant_version_are_authoritative", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='suspended',version=version+1 WHERE principal_id=$1`, principal); err != nil {
			t.Fatal("own membership fault failed")
		}
		d := check(t, "listPlatformIAMMembers", access)
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='active',version=version+1 WHERE principal_id=$1`, principal); err != nil {
			t.Fatal("own membership restoration failed")
		}
		if d.GetAllowed() || d.GetReason() != "MEMBERSHIP_INACTIVE" {
			t.Fatal("suspended Membership accepted")
		}
		if _, err := b.owner.Exec(ctx, `UPDATE platform_session_grants SET version=version+1 WHERE id=$1`, grant); err != nil {
			t.Fatal("own Grant invalidation failed")
		}
		d = check(t, "listPlatformIAMMembers", access)
		if d.GetAllowed() || d.GetReason() != "GRANT_VERSION_MISMATCH" {
			t.Fatal("stale Grant version accepted")
		}
		code, body, _ := b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
		if code != 200 {
			t.Fatalf("refresh under current Grant status=%d reason=%v", code, body["code"])
		}
		access, _ = body["access_token"].(string)
		if !check(t, "listPlatformIAMMembers", access).GetAllowed() {
			t.Fatal("current Grant did not restore valid access")
		}
	})
	t.Run("Redis_outage_keeps_PG_authorization_but_blocks_refresh", func(t *testing.T) {
		container := b.redisContainer.GetContainerID()
		if exec.Command("docker", "pause", container).Run() != nil {
			t.Fatal("own Redis pause failed")
		}
		defer func() {
			if exec.Command("docker", "unpause", container).Run() != nil {
				t.Error("own Redis restore failed")
			}
		}()
		if !check(t, "listPlatformIAMMembers", access).GetAllowed() {
			t.Fatal("Redis incorrectly became permission authority")
		}
		code, body, _ := b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
		if code != 503 || body["code"] != "IAM_UNAVAILABLE" {
			t.Fatalf("Redis refresh fault status=%d reason=%v", code, body["code"])
		}
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A9", "formal_Platform_online_authorization": "pass", "test_owned_faults_restored": true, "platform_management_mutations": "not_verified"})
	}
}
