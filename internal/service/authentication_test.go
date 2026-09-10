package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type allowingServiceAPIKeyUsageObserver struct{}

func (allowingServiceAPIKeyUsageObserver) ObserveAPIKeyUse(context.Context, biz.TenantScope, uuid.UUID, time.Time) error {
	return nil
}

func TestPasswordLoginMapsFrozenContractWithoutBusinessLogic(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	sessionID := uuid.MustParse("0198f062-b76d-7101-9000-000000000001")
	grantID := uuid.MustParse("0198f062-b76d-7101-9000-000000000002")
	now := time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC)
	usecase := &recordingAuthenticationUsecase{result: biz.PasswordLoginResult{
		Principal: biz.Principal{ID: principalID, Status: biz.PrincipalStatusActive},
		Session: biz.Session{
			ID:             sessionID,
			PrincipalID:    principalID,
			Audience:       biz.AudienceConsole,
			Status:         biz.SessionStatusActive,
			AuthnMethods:   []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
			DeviceName:     "browser",
			IdleExpiresAt:  now.Add(7 * 24 * time.Hour),
			AbsoluteExpiry: now.Add(30 * 24 * time.Hour),
			CreatedAt:      now,
		},
		Grant: biz.SessionGrant{
			ID:           grantID,
			SessionID:    sessionID,
			MembershipID: uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			Status:       biz.GrantStatusActive,
			Version:      1,
		},
		AccessToken:          "signed-access-token",
		RefreshToken:         "opaque-refresh-secret",
		AccessTokenExpiresAt: now.Add(15 * time.Minute),
	}}
	service := NewAuthenticationService(usecase)

	response, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:  " User@Example.COM ",
		Password: "correct-password",
		Audience: iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{
			TenantId: tenantID.String(),
		}}},
		SourceIp:       "203.0.113.10",
		DeviceName:     "browser",
		IdempotencyKey: "login-service-1",
	})
	if err != nil {
		t.Fatalf("PasswordLogin() error = %v", err)
	}
	if usecase.command.TenantID != tenantID || usecase.command.Audience != biz.AudienceConsole || usecase.command.Account != " User@Example.COM " {
		t.Fatalf("mapped command = %#v", usecase.command)
	}
	if usecase.command.SourceIP.String() != "203.0.113.10" {
		t.Fatalf("mapped source IP = %q, want %q", usecase.command.SourceIP, "203.0.113.10")
	}
	if response.GetAccessToken() != "signed-access-token" || response.GetRefreshToken() != "opaque-refresh-secret" || response.GetExpiresInSeconds() != 900 {
		t.Fatalf("token response = %#v", response)
	}
	if response.GetPrincipal().GetPrincipalId() != principalID.String() || response.GetPrincipal().GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN {
		t.Fatalf("principal response = %#v", response.GetPrincipal())
	}
	if response.GetSession().GetSessionId() != sessionID.String() || response.GetGrant().GetGrantId() != grantID.String() {
		t.Fatalf("session/grant response = %#v / %#v", response.GetSession(), response.GetGrant())
	}
}

func TestValidatePrincipalMapsAPIKeyContextWithoutHumanSessionFields(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-2000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199d080-2000-7001-9000-000000000002")
	decisionID := uuid.MustParse("0199d080-2000-7001-9000-000000000003")
	usecase := &recordingAuthenticationUsecase{validateResult: biz.ValidatePrincipalResult{
		Principal: biz.TrustedPrincipalContext{
			ID: principalID, Type: biz.PrincipalTypeWorkload, Status: biz.PrincipalStatusActive,
			TenantID: tenantID, AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodAPIKey},
		},
		DecisionID: decisionID, PolicyRevision: "sha256:test-revision",
	}}

	response, err := NewAuthenticationService(usecase).ValidatePrincipal(context.Background(), &iamv1.ValidatePrincipalRequest{
		Credential:  &iamv1.BearerCredential{Value: "ani_key_secret"},
		OperationId: "createInstance", PolicyRevision: "sha256:test-revision",
	})
	if err != nil {
		t.Fatalf("ValidatePrincipal() error = %v", err)
	}
	if usecase.validateCommand.RawCredential != "ani_key_secret" || usecase.validateCommand.OperationID != "createInstance" ||
		usecase.validateCommand.PolicyRevision != "sha256:test-revision" {
		t.Fatalf("ValidatePrincipal() command = %#v", usecase.validateCommand)
	}
	if response.GetDecisionId() != decisionID.String() || response.GetPolicyRevision() != "sha256:test-revision" {
		t.Fatalf("ValidatePrincipal() response identity = %#v", response)
	}
	principal := response.GetPrincipal()
	if principal.GetPrincipalId() != principalID.String() || principal.GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD ||
		principal.GetBoundary().GetTenant().GetTenantId() != tenantID.String() || principal.GetSessionId() != "" || principal.GetGrantId() != "" ||
		len(principal.GetAuthnMethods()) != 1 || principal.GetAuthnMethods()[0] != iamv1.AuthnMethod_AUTHN_METHOD_API_KEY {
		t.Fatalf("ValidatePrincipal() principal = %#v", principal)
	}
}

func TestValidatePrincipalMapsAPIKeyFailureClassesToFrozenErrorInfo(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-2100-7001-9000-000000000001")
	request := &iamv1.ValidatePrincipalRequest{
		Credential:  &iamv1.BearerCredential{Value: "ani_0199d080-2100-7001-9000-000000000002_secret"},
		OperationId: "createInstance", PolicyRevision: "sha256:test-revision",
	}
	tests := []struct {
		name         string
		domainErr    error
		wantCode     codes.Code
		wantReason   string
		wantMetadata map[string]string
	}{
		{name: "malformed unknown expired or revoked", domainErr: biz.ErrInvalidCredential, wantCode: codes.Unauthenticated, wantReason: "CREDENTIAL_INVALID", wantMetadata: map[string]string{"credential_kind": "api_key"}},
		{name: "valid key with inactive boundary", domainErr: &biz.TenantBoundAuthenticationError{TenantID: tenantID, Cause: biz.ErrMembershipInactive}, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "createInstance", "decision_id": "not-issued"}},
		{name: "tenant IAM not ready", domainErr: &biz.TenantBoundAuthenticationError{TenantID: tenantID, Cause: biz.ErrTenantIAMNotReady}, wantCode: codes.Unavailable, wantReason: "TENANT_IAM_NOT_READY", wantMetadata: map[string]string{"tenant_id": tenantID.String()}},
		{name: "lifecycle stale", domainErr: &biz.TenantBoundAuthenticationError{TenantID: tenantID, Cause: biz.ErrTenantLifecycleStale}, wantCode: codes.Unavailable, wantReason: "TENANT_LIFECYCLE_STALE", wantMetadata: map[string]string{"tenant_id": tenantID.String(), "expected_version": "not_available", "observed_version": "not_available"}},
		{name: "IAM unavailable", domainErr: biz.ErrAuthenticationDependency, wantCode: codes.Unavailable, wantReason: "IAM_UNAVAILABLE", wantMetadata: map[string]string{"dependency": "authentication"}},
		{name: "timeout", domainErr: context.DeadlineExceeded, wantCode: codes.DeadlineExceeded, wantReason: "IAM_TIMEOUT", wantMetadata: map[string]string{"operation_id": "createInstance"}},
		{name: "credential kind denied", domainErr: biz.ErrAuthenticationCredentialKindDenied, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "createInstance", "decision_id": "not-issued"}},
		{name: "policy mismatch", domainErr: &biz.AuthorizationPolicyMismatchError{Expected: "sha256:expected", Actual: "sha256:test-revision"}, wantCode: codes.Unavailable, wantReason: "AUTHZ_POLICY_MISMATCH", wantMetadata: map[string]string{"expected_policy_revision": "sha256:expected", "actual_policy_revision": "sha256:test-revision"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthenticationService(&recordingAuthenticationUsecase{validateErr: test.domainErr})
			_, err := service.ValidatePrincipal(context.Background(), request)
			assertFrozenErrorInfo(t, err, test.wantCode, test.wantReason, test.wantMetadata)
		})
	}
}

func TestRefreshSessionMapsFrozenInternalRotationContract(t *testing.T) {
	now := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	tenantID := uuid.MustParse("0199c71e-d000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199c71e-d000-7001-9000-000000000002")
	sessionID := uuid.MustParse("0199c71e-d000-7001-9000-000000000003")
	grantID := uuid.MustParse("0199c71e-d000-7001-9000-000000000004")
	usecase := &recordingAuthenticationUsecase{refreshResult: biz.RefreshSessionResult{
		TenantID: tenantID, Principal: biz.Principal{ID: principalID, Status: biz.PrincipalStatusActive},
		Session: biz.Session{
			ID: sessionID, PrincipalID: principalID, Audience: biz.AudienceConsole,
			Status: biz.SessionStatusActive, Version: 2,
			AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
			CreatedAt:    now.Add(-time.Hour), UpdatedAt: now,
			IdleExpiresAt: now.Add(7 * 24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour),
		},
		Grant:       biz.SessionGrant{ID: grantID, SessionID: sessionID, Status: biz.GrantStatusActive, Version: 3},
		AccessToken: "rotated-access", RefreshToken: "rotated-refresh", AccessTokenExpiresAt: now.Add(15 * time.Minute),
	}}

	response, err := NewAuthenticationService(usecase).RefreshSession(context.Background(), &iamv1.RefreshSessionRequest{
		RefreshToken: "old-refresh", CsrfToken: "csrf-proof", Origin: "https://console.test.example", IdempotencyKey: "refresh-service-1",
	})
	if err != nil {
		t.Fatalf("RefreshSession() error = %v", err)
	}
	if usecase.refreshCommand.RefreshToken != "old-refresh" || usecase.refreshCommand.CSRFToken != "csrf-proof" ||
		usecase.refreshCommand.Origin != "https://console.test.example" || usecase.refreshCommand.IdempotencyKey != "refresh-service-1" {
		t.Fatalf("RefreshSession() command = %#v", usecase.refreshCommand)
	}
	if response.GetLogin().GetAccessToken() != "rotated-access" || response.GetLogin().GetRefreshToken() != "rotated-refresh" ||
		response.GetLogin().GetExpiresInSeconds() != 900 || response.GetLogin().GetGrant().GetVersion() != 3 {
		t.Fatalf("RefreshSession() response = %#v", response)
	}
}

func TestLogoutSessionMapsCookieProofAndKeepsIdempotentResponseOpaque(t *testing.T) {
	usecase := &recordingAuthenticationUsecase{}
	response, err := NewAuthenticationService(usecase).LogoutSession(context.Background(), &iamv1.LogoutSessionRequest{
		RefreshToken: "cookie-refresh", CsrfToken: "csrf-proof",
		Origin: "https://console.test.example", IdempotencyKey: "logout-service-1",
	})
	if err != nil {
		t.Fatalf("LogoutSession() error = %v", err)
	}
	if usecase.logoutCommand.RefreshToken != "cookie-refresh" || usecase.logoutCommand.CSRFToken != "csrf-proof" ||
		usecase.logoutCommand.Origin != "https://console.test.example" || usecase.logoutCommand.IdempotencyKey != "logout-service-1" {
		t.Fatalf("LogoutSession() command = %#v", usecase.logoutCommand)
	}
	if response.GetResult() == nil || response.GetResult().GetResourceId() != "" || response.GetResult().GetVersion() != 0 {
		t.Fatalf("LogoutSession() leaked state = %#v", response)
	}
}

func TestSwitchTenantMapsBearerAndReturnsTargetBoundary(t *testing.T) {
	now := time.Date(2026, 9, 7, 13, 30, 0, 0, time.UTC)
	targetTenantID := uuid.MustParse("0199c71e-d000-7101-9000-000000000001")
	principalID := uuid.MustParse("0199c71e-d000-7101-9000-000000000002")
	sessionID := uuid.MustParse("0199c71e-d000-7101-9000-000000000003")
	grantID := uuid.MustParse("0199c71e-d000-7101-9000-000000000004")
	usecase := &recordingAuthenticationUsecase{switchResult: biz.SwitchTenantResult{
		TenantID: targetTenantID, Principal: biz.Principal{ID: principalID, Status: biz.PrincipalStatusActive},
		Session: biz.Session{
			ID: sessionID, PrincipalID: principalID, Audience: biz.AudienceConsole,
			Status: biz.SessionStatusActive, Version: 2,
			AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodOIDC},
			CreatedAt:    now.Add(-time.Hour), UpdatedAt: now,
			IdleExpiresAt: now.Add(7 * 24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour),
		},
		Grant:       biz.SessionGrant{ID: grantID, SessionID: sessionID, Status: biz.GrantStatusActive, Version: 5},
		AccessToken: "target-access", RefreshToken: "target-refresh", AccessTokenExpiresAt: now.Add(15 * time.Minute),
	}}

	response, err := NewAuthenticationService(usecase).SwitchTenant(context.Background(), &iamv1.SwitchTenantRequest{
		Credential: &iamv1.BearerCredential{Value: "source-access"}, TenantId: targetTenantID.String(),
		IdempotencyKey: "switch-service-1",
	})
	if err != nil {
		t.Fatalf("SwitchTenant() error = %v", err)
	}
	if usecase.switchCommand.RawCredential != "source-access" || usecase.switchCommand.TargetTenantID != targetTenantID ||
		usecase.switchCommand.IdempotencyKey != "switch-service-1" {
		t.Fatalf("SwitchTenant() command = %#v", usecase.switchCommand)
	}
	if response.GetLogin().GetPrincipal().GetBoundary().GetTenant().GetTenantId() != targetTenantID.String() ||
		response.GetLogin().GetGrant().GetGrantId() != grantID.String() || response.GetLogin().GetGrant().GetVersion() != 5 ||
		response.GetLogin().GetAccessToken() != "target-access" || response.GetLogin().GetRefreshToken() != "target-refresh" {
		t.Fatalf("SwitchTenant() response = %#v", response)
	}
}

func TestSessionContinuityMapsFrozenFailures(t *testing.T) {
	targetTenantID := uuid.MustParse("0199c71e-d000-7201-9000-000000000001")
	tests := []struct {
		name         string
		usecase      *recordingAuthenticationUsecase
		invoke       func(*AuthenticationService) error
		wantCode     codes.Code
		wantReason   string
		wantMetadata map[string]string
	}{
		{
			name: "refresh reuse", usecase: &recordingAuthenticationUsecase{refreshErr: biz.ErrInvalidCredential},
			invoke: func(service *AuthenticationService) error {
				_, err := service.RefreshSession(context.Background(), &iamv1.RefreshSessionRequest{
					RefreshToken: "reused-refresh", CsrfToken: "csrf", Origin: "https://console.test.example", IdempotencyKey: "refresh-reuse",
				})
				return err
			},
			wantCode: codes.Unauthenticated, wantReason: "CREDENTIAL_INVALID", wantMetadata: map[string]string{"credential_kind": "refresh_token"},
		},
		{
			name: "switch membership denied", usecase: &recordingAuthenticationUsecase{switchErr: biz.ErrMembershipInactive},
			invoke: func(service *AuthenticationService) error {
				_, err := service.SwitchTenant(context.Background(), &iamv1.SwitchTenantRequest{
					Credential: &iamv1.BearerCredential{Value: "access"}, TenantId: targetTenantID.String(), IdempotencyKey: "switch-denied",
				})
				return err
			},
			wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "switchTenant", "decision_id": "not-issued"},
		},
		{
			name: "switch lifecycle stale", usecase: &recordingAuthenticationUsecase{switchErr: biz.ErrTenantLifecycleStale},
			invoke: func(service *AuthenticationService) error {
				_, err := service.SwitchTenant(context.Background(), &iamv1.SwitchTenantRequest{
					Credential: &iamv1.BearerCredential{Value: "access"}, TenantId: targetTenantID.String(), IdempotencyKey: "switch-stale",
				})
				return err
			},
			wantCode: codes.Unavailable, wantReason: "TENANT_LIFECYCLE_STALE", wantMetadata: map[string]string{"tenant_id": targetTenantID.String()},
		},
		{
			name: "refresh dependency", usecase: &recordingAuthenticationUsecase{refreshErr: biz.ErrAuthenticationDependency},
			invoke: func(service *AuthenticationService) error {
				_, err := service.RefreshSession(context.Background(), &iamv1.RefreshSessionRequest{
					RefreshToken: "refresh", CsrfToken: "csrf", Origin: "https://console.test.example", IdempotencyKey: "refresh-down",
				})
				return err
			},
			wantCode: codes.Unavailable, wantReason: "IAM_UNAVAILABLE", wantMetadata: map[string]string{"dependency": "authentication"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFrozenErrorInfo(t, test.invoke(NewAuthenticationService(test.usecase)), test.wantCode, test.wantReason, test.wantMetadata)
		})
	}
}

func TestBeginOIDCLoginMapsFrozenContractWithoutBusinessLogic(t *testing.T) {
	tenantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe601")
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	oidc := &recordingOIDCServiceUsecase{beginLoginResult: biz.BeginOIDCLoginResult{
		AuthorizationURL: "https://dex.test.example/auth?client_id=ani-console",
		State:            "opaque-state",
		ExpiresAt:        now.Add(10 * time.Minute),
	}}
	service := NewAuthenticationService(&recordingAuthenticationUsecase{}, oidc)
	response, err := service.BeginOIDCLogin(context.Background(), &iamv1.BeginOIDCLoginRequest{
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenantID.String()}}},
		RedirectUri:    "https://console.test.example/auth/oidc/callback",
		IdempotencyKey: "oidc-begin-service-1",
	})
	if err != nil {
		t.Fatalf("BeginOIDCLogin() error = %v", err)
	}
	if oidc.beginLoginCommand.TenantID != tenantID || oidc.beginLoginCommand.Audience != biz.AudienceConsole ||
		oidc.beginLoginCommand.RedirectURI != "https://console.test.example/auth/oidc/callback" {
		t.Fatalf("mapped command = %#v", oidc.beginLoginCommand)
	}
	if response.GetAuthorizationUrl() != oidc.beginLoginResult.AuthorizationURL || response.GetState() != "opaque-state" ||
		!response.GetExpiresAt().AsTime().Equal(oidc.beginLoginResult.ExpiresAt) {
		t.Fatalf("BeginOIDCLogin() response = %#v", response)
	}
}

func TestCompleteOIDCLoginMapsOIDCSessionResult(t *testing.T) {
	now := time.Date(2026, 9, 7, 8, 30, 0, 0, time.UTC)
	tenantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe611")
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe612")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe613")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe614")
	oidc := &recordingOIDCServiceUsecase{completeLoginResult: biz.LoginResult{
		TenantID:    tenantID,
		Principal:   biz.Principal{ID: principalID, Status: biz.PrincipalStatusActive},
		Session:     biz.Session{ID: sessionID, PrincipalID: principalID, Audience: biz.AudienceConsole, Status: biz.SessionStatusActive, AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodOIDC}, DeviceName: "browser", IdleExpiresAt: now.Add(7 * 24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour), ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now},
		Grant:       biz.SessionGrant{ID: grantID, SessionID: sessionID, Status: biz.GrantStatusActive, Version: 1},
		AccessToken: "signed-oidc-access-token", RefreshToken: "opaque-refresh-secret", AccessTokenExpiresAt: now.Add(15 * time.Minute),
	}}
	service := NewAuthenticationService(&recordingAuthenticationUsecase{}, oidc)
	response, err := service.CompleteOIDCLogin(context.Background(), &iamv1.CompleteOIDCLoginRequest{
		Code: "authorization-code", State: "opaque-state",
		RedirectUri: "https://console.test.example/auth/oidc/callback", DeviceName: "browser",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCLogin() error = %v", err)
	}
	if oidc.completeLoginCommand.Code != "authorization-code" || oidc.completeLoginCommand.State != "opaque-state" {
		t.Fatalf("mapped command = %#v", oidc.completeLoginCommand)
	}
	login := response.GetLogin()
	if login == nil || login.GetPrincipal().GetBoundary().GetTenant().GetTenantId() != tenantID.String() ||
		len(login.GetPrincipal().GetAuthnMethods()) != 1 || login.GetPrincipal().GetAuthnMethods()[0] != iamv1.AuthnMethod_AUTHN_METHOD_OIDC ||
		len(login.GetSession().GetAuthnMethods()) != 1 || login.GetSession().GetAuthnMethods()[0] != iamv1.AuthnMethod_AUTHN_METHOD_OIDC {
		t.Fatalf("CompleteOIDCLogin() response = %#v", response)
	}
}

func TestBeginOIDCIdentityLinkMapsAuthenticatedContract(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	oidc := &recordingOIDCServiceUsecase{beginLinkResult: biz.BeginOIDCIdentityLinkResult{
		AuthorizationURL: "https://dex.test.example/auth?link=1", State: "link-state", ExpiresAt: now.Add(10 * time.Minute),
	}}
	service := NewAuthenticationService(&recordingAuthenticationUsecase{}, oidc)
	response, err := service.BeginOIDCIdentityLink(context.Background(), &iamv1.BeginOIDCIdentityLinkRequest{
		Credential: &iamv1.BearerCredential{Value: "signed-access-token"}, Provider: "dex",
		RedirectUri: "https://console.test.example/auth/oidc/link/callback", IdempotencyKey: "oidc-link-service-1",
	})
	if err != nil {
		t.Fatalf("BeginOIDCIdentityLink() error = %v", err)
	}
	if oidc.beginLinkCommand.RawCredential != "signed-access-token" || oidc.beginLinkCommand.Provider != "dex" ||
		oidc.beginLinkCommand.IdempotencyKey != "oidc-link-service-1" {
		t.Fatalf("mapped command = %#v", oidc.beginLinkCommand)
	}
	if response.GetAuthorizationUrl() != oidc.beginLinkResult.AuthorizationURL || response.GetState() != "link-state" ||
		!response.GetExpiresAt().AsTime().Equal(oidc.beginLinkResult.ExpiresAt) {
		t.Fatalf("BeginOIDCIdentityLink() response = %#v", response)
	}
}

func TestCompleteOIDCIdentityLinkMapsAuthenticatedContract(t *testing.T) {
	identityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe621")
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe622")
	oidc := &recordingOIDCServiceUsecase{completeLinkResult: biz.OIDCIdentityLinkResult{
		IdentityID: identityID, PrincipalID: principalID,
	}}
	service := NewAuthenticationService(&recordingAuthenticationUsecase{}, oidc)
	response, err := service.CompleteOIDCIdentityLink(context.Background(), &iamv1.CompleteOIDCIdentityLinkRequest{
		Credential: &iamv1.BearerCredential{Value: "signed-access-token"}, Code: "authorization-code",
		State: "link-state", RedirectUri: "https://console.test.example/auth/oidc/link/callback",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCIdentityLink() error = %v", err)
	}
	if oidc.completeLinkCommand.RawCredential != "signed-access-token" || oidc.completeLinkCommand.Code != "authorization-code" || oidc.completeLinkCommand.State != "link-state" {
		t.Fatalf("mapped command = %#v", oidc.completeLinkCommand)
	}
	if response.GetIdentityId() != identityID.String() || response.GetPrincipalId() != principalID.String() {
		t.Fatalf("CompleteOIDCIdentityLink() response = %#v", response)
	}
}

func TestOIDCHandlersMapSecurityFailuresToFrozenErrorInfo(t *testing.T) {
	tenantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe631")
	tests := []struct {
		name         string
		domainErr    error
		invoke       func(*AuthenticationService) error
		wantCode     codes.Code
		wantReason   string
		wantMetadata map[string]string
	}{
		{
			name: "begin login exact redirect", domainErr: biz.ErrOIDCRedirectInvalid,
			invoke: func(service *AuthenticationService) error {
				_, err := service.BeginOIDCLogin(context.Background(), &iamv1.BeginOIDCLoginRequest{
					Audience: iamv1.Audience_AUDIENCE_CONSOLE, Boundary: tenantBoundary(tenantID),
					RedirectUri: "https://attacker.example/callback", IdempotencyKey: "oidc-error-begin",
				})
				return err
			},
			wantCode: codes.InvalidArgument, wantReason: "INVALID_ARGUMENT", wantMetadata: map[string]string{"field": "redirect_uri"},
		},
		{
			name: "complete login consumed state", domainErr: biz.ErrOIDCStateInvalid,
			invoke: func(service *AuthenticationService) error {
				_, err := service.CompleteOIDCLogin(context.Background(), &iamv1.CompleteOIDCLoginRequest{
					Code: "authorization-code", State: "consumed-state", RedirectUri: "https://console.test.example/auth/oidc/callback",
				})
				return err
			},
			wantCode: codes.Unauthenticated, wantReason: "CREDENTIAL_INVALID", wantMetadata: map[string]string{"credential_kind": "oidc"},
		},
		{
			name: "begin link reauthentication required", domainErr: biz.ErrOIDCReauthenticationRequired,
			invoke: func(service *AuthenticationService) error {
				_, err := service.BeginOIDCIdentityLink(context.Background(), &iamv1.BeginOIDCIdentityLinkRequest{
					Credential: &iamv1.BearerCredential{Value: "access-token"}, Provider: "dex",
					RedirectUri: "https://console.test.example/auth/oidc/link/callback", IdempotencyKey: "oidc-error-link",
				})
				return err
			},
			wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED",
			wantMetadata: map[string]string{"operation_id": "beginOIDCIdentityLink", "decision_id": "not-issued"},
		},
		{
			name: "complete link identity conflict", domainErr: biz.ErrOIDCIdentityConflict,
			invoke: func(service *AuthenticationService) error {
				_, err := service.CompleteOIDCIdentityLink(context.Background(), &iamv1.CompleteOIDCIdentityLinkRequest{
					Credential: &iamv1.BearerCredential{Value: "access-token"}, Code: "authorization-code",
					State: "link-state", RedirectUri: "https://console.test.example/auth/oidc/link/callback",
				})
				return err
			},
			wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED",
			wantMetadata: map[string]string{"operation_id": "completeOIDCIdentityLink", "decision_id": "not-issued"},
		},
		{
			name: "provider dependency", domainErr: biz.ErrOIDCDependency,
			invoke: func(service *AuthenticationService) error {
				_, err := service.CompleteOIDCLogin(context.Background(), &iamv1.CompleteOIDCLoginRequest{
					Code: "authorization-code", State: "state", RedirectUri: "https://console.test.example/auth/oidc/callback",
				})
				return err
			},
			wantCode: codes.Unavailable, wantReason: "IAM_UNAVAILABLE", wantMetadata: map[string]string{"dependency": "oidc"},
		},
		{
			name: "provider timeout", domainErr: errors.Join(biz.ErrOIDCDependency, context.DeadlineExceeded),
			invoke: func(service *AuthenticationService) error {
				_, err := service.CompleteOIDCLogin(context.Background(), &iamv1.CompleteOIDCLoginRequest{
					Code: "authorization-code", State: "state", RedirectUri: "https://console.test.example/auth/oidc/callback",
				})
				return err
			},
			wantCode: codes.DeadlineExceeded, wantReason: "IAM_TIMEOUT", wantMetadata: map[string]string{"operation_id": "completeOIDCLogin"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthenticationService(&recordingAuthenticationUsecase{}, &recordingOIDCServiceUsecase{err: test.domainErr})
			assertFrozenErrorInfo(t, test.invoke(service), test.wantCode, test.wantReason, test.wantMetadata)
		})
	}
}

type recordingOIDCServiceUsecase struct {
	beginLoginCommand    biz.BeginOIDCLoginCommand
	beginLoginResult     biz.BeginOIDCLoginResult
	completeLoginCommand biz.CompleteOIDCLoginCommand
	completeLoginResult  biz.LoginResult
	beginLinkCommand     biz.BeginOIDCIdentityLinkCommand
	beginLinkResult      biz.BeginOIDCIdentityLinkResult
	completeLinkCommand  biz.CompleteOIDCIdentityLinkCommand
	completeLinkResult   biz.OIDCIdentityLinkResult
	err                  error
}

func (u *recordingOIDCServiceUsecase) BeginLogin(_ context.Context, command biz.BeginOIDCLoginCommand) (biz.BeginOIDCLoginResult, error) {
	u.beginLoginCommand = command
	return u.beginLoginResult, u.err
}

func (u *recordingOIDCServiceUsecase) CompleteLogin(_ context.Context, command biz.CompleteOIDCLoginCommand) (biz.LoginResult, error) {
	u.completeLoginCommand = command
	return u.completeLoginResult, u.err
}

func (u *recordingOIDCServiceUsecase) BeginIdentityLink(_ context.Context, command biz.BeginOIDCIdentityLinkCommand) (biz.BeginOIDCIdentityLinkResult, error) {
	u.beginLinkCommand = command
	return u.beginLinkResult, u.err
}

func (u *recordingOIDCServiceUsecase) CompleteIdentityLink(_ context.Context, command biz.CompleteOIDCIdentityLinkCommand) (biz.OIDCIdentityLinkResult, error) {
	u.completeLinkCommand = command
	return u.completeLinkResult, u.err
}

func TestRequestPasswordActionMapsFrozenNonEnumeratingContract(t *testing.T) {
	operationID := uuid.MustParse("0198f062-b76d-7001-9000-000000000061")
	expiresAt := time.Date(2026, 9, 6, 12, 30, 0, 0, time.UTC)
	usecase := &recordingAuthenticationUsecase{requestResult: biz.RequestPasswordActionResult{
		OperationID: operationID,
		ExpiresAt:   expiresAt,
	}}

	response, err := NewAuthenticationService(usecase).RequestPasswordAction(context.Background(), &iamv1.RequestPasswordActionRequest{
		Account:        " User@Example.COM ",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		IdempotencyKey: "password-action-request-1",
	})
	if err != nil {
		t.Fatalf("RequestPasswordAction() error = %v", err)
	}
	if response.GetOperationId() != operationID.String() || !response.GetExpiresAt().AsTime().Equal(expiresAt) {
		t.Fatalf("RequestPasswordAction() response = %#v", response)
	}
	if usecase.requestCalls != 1 || usecase.requestCommand.Account != " User@Example.COM " ||
		usecase.requestCommand.Audience != biz.AudienceConsole || usecase.requestCommand.IdempotencyKey != "password-action-request-1" {
		t.Fatalf("RequestPasswordAction() mapped command = %#v calls=%d", usecase.requestCommand, usecase.requestCalls)
	}
}

func TestCompletePasswordActionMapsFrozenMutationContract(t *testing.T) {
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	usecase := &recordingAuthenticationUsecase{completeResult: biz.CompletePasswordActionResult{
		PrincipalID:       principalID,
		CredentialVersion: 2,
	}}

	response, err := NewAuthenticationService(usecase).CompletePasswordAction(context.Background(), &iamv1.CompletePasswordActionRequest{
		ActionToken:    "opaque-signed-action-token",
		NewPassword:    "new test password",
		IdempotencyKey: "password-action-complete-1",
	})
	if err != nil {
		t.Fatalf("CompletePasswordAction() error = %v", err)
	}
	if response.GetResult().GetResourceId() != principalID.String() || response.GetResult().GetVersion() != 2 {
		t.Fatalf("CompletePasswordAction() response = %#v", response)
	}
	if usecase.completeCalls != 1 || usecase.completeCommand.ActionToken != "opaque-signed-action-token" ||
		usecase.completeCommand.NewPassword != "new test password" || usecase.completeCommand.IdempotencyKey != "password-action-complete-1" {
		t.Fatalf("CompletePasswordAction() mapped command = %#v calls=%d", usecase.completeCommand, usecase.completeCalls)
	}
}

func TestPasswordActionMapsIdempotencyConflictToFrozenError(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		call        func() error
	}{
		{
			name:        "request",
			operationID: "requestPasswordAction",
			call: func() error {
				service := NewAuthenticationService(&recordingAuthenticationUsecase{requestErr: biz.ErrIdempotencyConflict})
				_, err := service.RequestPasswordAction(context.Background(), &iamv1.RequestPasswordActionRequest{
					Account:        "user@example.com",
					Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
					IdempotencyKey: "conflicting-action-key",
				})
				return err
			},
		},
		{
			name:        "complete",
			operationID: "completePasswordAction",
			call: func() error {
				service := NewAuthenticationService(&recordingAuthenticationUsecase{completeErr: biz.ErrIdempotencyConflict})
				_, err := service.CompletePasswordAction(context.Background(), &iamv1.CompletePasswordActionRequest{
					ActionToken:    "opaque-action-token",
					NewPassword:    "new test password",
					IdempotencyKey: "conflicting-action-key",
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != codes.AlreadyExists || len(grpcStatus.Details()) != 1 {
				t.Fatalf("idempotency conflict status = %s details=%#v", grpcStatus.Code(), grpcStatus.Details())
			}
			info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
			if !ok || info.GetReason() != "IDEMPOTENCY_CONFLICT" ||
				info.GetMetadata()["operation_id"] != test.operationID ||
				info.GetMetadata()["idempotency_key"] != "conflicting-action-key" {
				t.Fatalf("idempotency conflict ErrorInfo = %#v", info)
			}
		})
	}
}

func TestPasswordLoginMapsInvalidCredentialToStableErrorInfo(t *testing.T) {
	service := NewAuthenticationService(&recordingAuthenticationUsecase{err: biz.ErrInvalidCredential})
	_, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       tenantBoundary(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")),
		SourceIp:       "203.0.113.10",
		IdempotencyKey: "login-invalid-credential",
	})
	if err == nil {
		t.Fatal("PasswordLogin() unexpectedly succeeded")
	}
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.Unauthenticated {
		t.Fatalf("gRPC code = %s, want %s", grpcStatus.Code(), codes.Unauthenticated)
	}
	if errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("transport error leaked domain error: %v", err)
	}
	if len(grpcStatus.Details()) != 1 {
		t.Fatalf("error details = %#v", grpcStatus.Details())
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != "CREDENTIAL_INVALID" || info.GetDomain() != "iam.ani.internal" || info.GetMetadata()["credential_kind"] != "password" {
		t.Fatalf("ErrorInfo = %#v", info)
	}
}

func TestPasswordLoginMapsRateLimitToStableErrorInfo(t *testing.T) {
	service := NewAuthenticationService(&recordingAuthenticationUsecase{err: &biz.AuthenticationRateLimitError{
		LimitScope: "password_account",
		RetryAfter: 15 * time.Minute,
	}})
	_, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       tenantBoundary(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")),
		SourceIp:       "203.0.113.10",
		IdempotencyKey: "login-rate-limit",
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.ResourceExhausted || len(grpcStatus.Details()) != 1 {
		t.Fatalf("rate-limit status = %s details=%#v", grpcStatus.Code(), grpcStatus.Details())
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != "AUTH_RATE_LIMITED" || info.GetDomain() != "iam.ani.internal" || info.GetMetadata()["limit_scope"] != "password_account" || info.GetMetadata()["retry_after_seconds"] != "900" {
		t.Fatalf("rate-limit ErrorInfo = %#v", info)
	}
}

func TestPasswordLoginValidationUsesFrozenErrorInfo(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	tests := []struct {
		name      string
		request   *iamv1.PasswordLoginRequest
		wantField string
	}{
		{name: "missing request", wantField: "request"},
		{
			name:      "missing tenant boundary",
			request:   &iamv1.PasswordLoginRequest{Audience: iamv1.Audience_AUDIENCE_CONSOLE},
			wantField: "boundary",
		},
		{
			name: "invalid tenant boundary",
			request: &iamv1.PasswordLoginRequest{
				Audience: iamv1.Audience_AUDIENCE_CONSOLE,
				Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: "not-a-uuid"}}},
			},
			wantField: "boundary",
		},
		{
			name:      "invalid audience",
			request:   &iamv1.PasswordLoginRequest{Boundary: tenantBoundary(tenantID)},
			wantField: "audience",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthenticationService(&recordingAuthenticationUsecase{})
			_, err := service.PasswordLogin(context.Background(), test.request)
			assertFrozenErrorInfo(t, err, codes.InvalidArgument, "INVALID_ARGUMENT", map[string]string{"field": test.wantField})
		})
	}
}

func TestPasswordLoginRejectsMissingOrInvalidSourceIPBeforeUsecase(t *testing.T) {
	tests := []struct {
		name     string
		sourceIP string
	}{
		{name: "missing source IP"},
		{name: "invalid source IP", sourceIP: "not-an-ip"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &recordingAuthenticationUsecase{}
			request := validPasswordLoginRequest()
			request.SourceIp = test.sourceIP

			_, err := NewAuthenticationService(usecase).PasswordLogin(context.Background(), request)
			assertFrozenErrorInfo(t, err, codes.InvalidArgument, "INVALID_ARGUMENT", map[string]string{"field": "source_ip"})
			if usecase.calls != 0 {
				t.Fatalf("PasswordLogin usecase calls = %d, want 0", usecase.calls)
			}
		})
	}
}

func TestPasswordLoginRejectsBossAudienceWithTenantBoundaryBeforeUsecase(t *testing.T) {
	usecase := &recordingAuthenticationUsecase{}
	service := NewAuthenticationService(usecase)
	_, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "platform@example.com",
		Password:       "password",
		Audience:       iamv1.Audience_AUDIENCE_BOSS,
		Boundary:       tenantBoundary(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")),
		IdempotencyKey: "login-boss-tenant-boundary",
	})
	assertFrozenErrorInfo(t, err, codes.InvalidArgument, "INVALID_ARGUMENT", map[string]string{"field": "boundary"})
	if usecase.calls != 0 {
		t.Fatalf("PasswordLogin usecase calls = %d, want 0", usecase.calls)
	}
}

func TestPasswordLoginFailsClosedWhenBossPlatformSliceIsUnavailable(t *testing.T) {
	usecase := &recordingAuthenticationUsecase{}
	service := NewAuthenticationService(usecase)
	_, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "platform@example.com",
		Password:       "password",
		Audience:       iamv1.Audience_AUDIENCE_BOSS,
		Boundary:       &iamv1.Boundary{Boundary: &iamv1.Boundary_Platform{Platform: &iamv1.PlatformBoundary{}}},
		IdempotencyKey: "login-boss-platform-boundary",
	})
	assertFrozenErrorInfo(t, err, codes.Unavailable, "IAM_UNAVAILABLE", map[string]string{"dependency": "platform_authentication"})
	if usecase.calls != 0 {
		t.Fatalf("PasswordLogin usecase calls = %d, want 0", usecase.calls)
	}
}

func TestPasswordLoginMapsDomainValidationToFrozenErrorInfo(t *testing.T) {
	service := NewAuthenticationService(&recordingAuthenticationUsecase{err: biz.ErrAccountRequired})
	_, err := service.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "user@example.com",
		Password:       "password",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       tenantBoundary(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")),
		SourceIp:       "203.0.113.10",
		IdempotencyKey: "login-invalid-account",
	})
	assertFrozenErrorInfo(t, err, codes.InvalidArgument, "INVALID_ARGUMENT", map[string]string{"field": "account"})
}

func TestPasswordLoginMapsReachableDomainFailuresToFrozenErrorInfo(t *testing.T) {
	tests := []struct {
		name         string
		domainErr    error
		wantCode     codes.Code
		wantReason   string
		wantMetadata map[string]string
	}{
		{name: "password missing", domainErr: biz.ErrPasswordRequired, wantCode: codes.InvalidArgument, wantReason: "INVALID_ARGUMENT", wantMetadata: map[string]string{"field": "password"}},
		{name: "audience missing", domainErr: biz.ErrAudienceRequired, wantCode: codes.InvalidArgument, wantReason: "INVALID_ARGUMENT", wantMetadata: map[string]string{"field": "audience"}},
		{name: "idempotency key missing", domainErr: biz.ErrIdempotencyKeyRequired, wantCode: codes.InvalidArgument, wantReason: "INVALID_ARGUMENT", wantMetadata: map[string]string{"field": "idempotency_key"}},
		{name: "principal inactive", domainErr: biz.ErrPrincipalInactive, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "passwordLogin", "decision_id": "not-issued"}},
		{name: "membership inactive", domainErr: biz.ErrMembershipInactive, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "passwordLogin", "decision_id": "not-issued"}},
		{name: "tenant access inactive", domainErr: biz.ErrTenantAccessInactive, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "passwordLogin", "decision_id": "not-issued"}},
		{name: "tenant lifecycle blocked", domainErr: biz.ErrTenantLifecycleBlocked, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED", wantMetadata: map[string]string{"operation_id": "passwordLogin", "decision_id": "not-issued"}},
		{name: "tenant lifecycle stale", domainErr: biz.ErrTenantLifecycleStale, wantCode: codes.Unavailable, wantReason: "TENANT_LIFECYCLE_STALE", wantMetadata: map[string]string{"tenant_id": "0198f062-b76d-7f2a-b0ad-50a417bf1f70", "expected_version": "not_available", "observed_version": "not_available"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthenticationService(&recordingAuthenticationUsecase{err: test.domainErr})
			_, err := service.PasswordLogin(context.Background(), validPasswordLoginRequest())
			assertFrozenErrorInfo(t, err, test.wantCode, test.wantReason, test.wantMetadata)
		})
	}
}

func TestPasswordLoginRealUsecaseMapsRequiredAuditFailureToUnavailable(t *testing.T) {
	usecase := newServiceAuthenticationUsecase(allowingServiceLoginThrottle{}, failingServiceLoginUnitOfWork{err: biz.ErrAuditConflict})
	service := NewAuthenticationService(usecase)
	_, err := service.PasswordLogin(context.Background(), validPasswordLoginRequest())
	assertFrozenErrorInfo(t, err, codes.Unavailable, "IAM_UNAVAILABLE", map[string]string{"dependency": "authentication"})
}

func TestPasswordLoginRealUsecasePreservesRateLimitMetadata(t *testing.T) {
	usecase := newServiceAuthenticationUsecase(failingServiceLoginThrottle{err: &biz.AuthenticationRateLimitError{
		LimitScope: "password_ip",
		RetryAfter: 15 * time.Minute,
	}}, failingServiceLoginUnitOfWork{})
	service := NewAuthenticationService(usecase)
	_, err := service.PasswordLogin(context.Background(), validPasswordLoginRequest())
	assertFrozenErrorInfo(t, err, codes.ResourceExhausted, "AUTH_RATE_LIMITED", map[string]string{
		"limit_scope":         "password_ip",
		"retry_after_seconds": "900",
	})
}

func validPasswordLoginRequest() *iamv1.PasswordLoginRequest {
	return &iamv1.PasswordLoginRequest{
		Account:        "user@example.com",
		Password:       "password",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       tenantBoundary(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")),
		SourceIp:       "203.0.113.10",
		IdempotencyKey: "login-service-real-usecase",
	}
}

func newServiceAuthenticationUsecase(throttle biz.LoginThrottle, uow biz.AuthenticationUnitOfWork) *biz.AuthenticationUsecase {
	return biz.NewAuthenticationUsecase(
		servicePasswordLoginReader{state: biz.PasswordLoginState{
			PrincipalID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
			PrincipalStatus:  biz.PrincipalStatusActive,
			MembershipID:     uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
			MembershipStatus: biz.MembershipStatusActive,
			TenantAccess:     biz.TenantAccessStatusActive,
			Lifecycle:        biz.TenantLifecycleStatusActive,
			LifecycleFresh:   true,
			PasswordHash:     "test-phc",
		}},
		servicePasswordVerifier{},
		throttle,
		uow,
		serviceAccessTokenIssuer{},
		serviceSecretGenerator{},
		&serviceIDGenerator{ids: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7101-9000-000000000001"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000002"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000003"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000004"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000005"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000006"),
		}},
		serviceAuthenticationClock{}, allowingServiceAPIKeyUsageObserver{})

}

type servicePasswordLoginReader struct{ state biz.PasswordLoginState }

func (r servicePasswordLoginReader) LookupPasswordLogin(context.Context, biz.TenantScope, string) (biz.PasswordLoginState, error) {
	return r.state, nil
}
func (servicePasswordLoginReader) LookupPasswordActionTarget(context.Context, string, biz.Audience) (biz.PasswordActionTarget, bool, error) {
	return biz.PasswordActionTarget{}, false, nil
}
func (servicePasswordLoginReader) LookupRefreshSession(context.Context, [32]byte) (biz.RefreshSessionState, error) {
	return biz.RefreshSessionState{}, biz.ErrInvalidCredential
}
func (servicePasswordLoginReader) LookupLogoutSession(context.Context, [32]byte) (biz.LogoutSessionState, bool, error) {
	return biz.LogoutSessionState{}, false, nil
}
func (servicePasswordLoginReader) LookupTenantSwitch(context.Context, biz.TenantScope, biz.AccessTokenClaims) (biz.TenantSwitchState, error) {
	return biz.TenantSwitchState{}, biz.ErrInvalidCredential
}
func (servicePasswordLoginReader) Revision() string { return "test-policy-revision" }
func (servicePasswordLoginReader) Lookup(string) (biz.AuthorizationPolicy, bool) {
	return biz.AuthorizationPolicy{}, false
}
func (servicePasswordLoginReader) GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, biz.ErrAPIKeyNotFound
}
func (servicePasswordLoginReader) LookupAuthorization(context.Context, biz.TenantScope, biz.AuthorizationLookup) (biz.AuthorizationState, error) {
	return biz.AuthorizationState{}, biz.ErrAuthenticationDependency
}
func (servicePasswordLoginReader) RecordDeniedAuthorization(context.Context, biz.TenantScope, biz.SecurityAuditEvent) error {
	return biz.ErrAuthenticationDependency
}
func (servicePasswordLoginReader) RecordUnboundAuthorization(context.Context, biz.SecurityAuditEvent) error {
	return biz.ErrAuthenticationDependency
}
func (servicePasswordLoginReader) LookupAPIKeyCredential(context.Context, biz.TenantScope, uuid.UUID, string) (biz.APIKey, error) {
	return biz.APIKey{}, biz.ErrAPIKeyNotFound
}
func (servicePasswordLoginReader) LookupAPIKeyAuthorization(context.Context, biz.TenantScope, uuid.UUID, string, []string) (biz.APIKeyAuthorizationState, error) {
	return biz.APIKeyAuthorizationState{}, biz.ErrAPIKeyNotFound
}
func (servicePasswordLoginReader) RecordAPIKeyUse(context.Context, biz.TenantScope, uuid.UUID, time.Time) error {
	return biz.ErrAuthenticationDependency
}

type servicePasswordVerifier struct{}

func (servicePasswordVerifier) Verify(string, string) (bool, error) { return true, nil }
func (servicePasswordVerifier) VerifyUnknown(string) error          { return nil }
func (servicePasswordVerifier) Hash(string) (string, error)         { return "test-password-hash", nil }

type allowingServiceLoginThrottle struct{}

func (allowingServiceLoginThrottle) Check(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}
func (allowingServiceLoginThrottle) RecordFailure(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}
func (allowingServiceLoginThrottle) Reset(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}
func (allowingServiceLoginThrottle) CheckRefresh(context.Context, biz.RefreshThrottleAttempt) error {
	return nil
}

type failingServiceLoginThrottle struct{ err error }

func (t failingServiceLoginThrottle) Check(context.Context, biz.LoginThrottleAttempt) error {
	return t.err
}
func (t failingServiceLoginThrottle) RecordFailure(context.Context, biz.LoginThrottleAttempt) error {
	return t.err
}
func (t failingServiceLoginThrottle) Reset(context.Context, biz.LoginThrottleAttempt) error {
	return t.err
}
func (t failingServiceLoginThrottle) CheckRefresh(context.Context, biz.RefreshThrottleAttempt) error {
	return t.err
}

type failingServiceLoginUnitOfWork struct{ err error }

func (u failingServiceLoginUnitOfWork) CommitLogin(context.Context, biz.TenantScope, biz.LoginMutation) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RecordLoginFailure(context.Context, biz.TenantScope, biz.LoginFailureMutation) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RecordTenantPrincipalValidation(context.Context, biz.TenantScope, biz.SecurityAuditEvent) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RecordUnboundPrincipalValidation(context.Context, biz.SecurityAuditEvent) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RequestPasswordAction(context.Context, biz.PasswordActionRequestMutation) (biz.RequestPasswordActionResult, error) {
	return biz.RequestPasswordActionResult{}, u.err
}
func (u failingServiceLoginUnitOfWork) CompletePasswordAction(context.Context, biz.PasswordActionCompletion) (biz.CompletePasswordActionResult, error) {
	return biz.CompletePasswordActionResult{}, u.err
}
func (u failingServiceLoginUnitOfWork) RotateRefreshSession(context.Context, biz.RefreshSessionMutation) (biz.RefreshSessionMutationResult, error) {
	return biz.RefreshSessionMutationResult{}, u.err
}
func (u failingServiceLoginUnitOfWork) RevokeRefreshReuse(context.Context, biz.RefreshReuseMutation) (bool, error) {
	return false, u.err
}
func (u failingServiceLoginUnitOfWork) LogoutSession(context.Context, biz.LogoutSessionMutation) (biz.LogoutSessionMutationResult, error) {
	return biz.LogoutSessionMutationResult{}, u.err
}
func (u failingServiceLoginUnitOfWork) SwitchTenant(context.Context, biz.TenantScope, biz.TenantSwitchMutation) error {
	return u.err
}

type serviceAccessTokenIssuer struct{}

func (serviceAccessTokenIssuer) Issue(context.Context, biz.AccessTokenClaims) (string, error) {
	return "service-access-token", nil
}
func (serviceAccessTokenIssuer) IssuePasswordAction(context.Context, biz.PasswordActionTokenClaims) (string, error) {
	return "test-password-action-token", nil
}
func (serviceAccessTokenIssuer) Verify(context.Context, string) (biz.AccessTokenClaims, error) {
	return biz.AccessTokenClaims{}, biz.ErrAuthorizationCredentialInvalid
}
func (serviceAccessTokenIssuer) VerifyPasswordAction(context.Context, string) (biz.PasswordActionTokenClaims, error) {
	return biz.PasswordActionTokenClaims{}, biz.ErrPasswordActionInvalid
}

type serviceSecretGenerator struct{}

func (serviceSecretGenerator) NewSecret() (string, error) { return "service-refresh-token", nil }

type serviceIDGenerator struct {
	ids   []uuid.UUID
	index int
}

func (g *serviceIDGenerator) NewID() (uuid.UUID, error) {
	id := g.ids[g.index]
	g.index++
	return id, nil
}

type serviceAuthenticationClock struct{}

func (serviceAuthenticationClock) Now() time.Time {
	return time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC)
}

func assertFrozenErrorInfo(t *testing.T, err error, wantCode codes.Code, wantReason string, wantMetadata map[string]string) {
	t.Helper()
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != wantCode || len(grpcStatus.Details()) != 1 {
		t.Fatalf("status = %s details=%#v, want %s and one ErrorInfo", grpcStatus.Code(), grpcStatus.Details(), wantCode)
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != wantReason || info.GetDomain() != "iam.ani.internal" {
		t.Fatalf("ErrorInfo = %#v", info)
	}
	for key, value := range wantMetadata {
		if info.GetMetadata()[key] != value {
			t.Fatalf("metadata[%q] = %q, want %q", key, info.GetMetadata()[key], value)
		}
	}
}

type recordingAuthenticationUsecase struct {
	result          biz.PasswordLoginResult
	err             error
	command         biz.PasswordLoginCommand
	calls           int
	requestResult   biz.RequestPasswordActionResult
	requestErr      error
	requestCommand  biz.RequestPasswordActionCommand
	requestCalls    int
	completeResult  biz.CompletePasswordActionResult
	completeErr     error
	completeCommand biz.CompletePasswordActionCommand
	completeCalls   int
	refreshResult   biz.RefreshSessionResult
	refreshErr      error
	refreshCommand  biz.RefreshSessionCommand
	logoutErr       error
	logoutCommand   biz.LogoutSessionCommand
	switchResult    biz.SwitchTenantResult
	switchErr       error
	switchCommand   biz.SwitchTenantCommand
	validateResult  biz.ValidatePrincipalResult
	validateErr     error
	validateCommand biz.ValidatePrincipalCommand
}

func (u *recordingAuthenticationUsecase) ValidatePrincipal(_ context.Context, command biz.ValidatePrincipalCommand) (biz.ValidatePrincipalResult, error) {
	u.validateCommand = command
	return u.validateResult, u.validateErr
}

func (u *recordingAuthenticationUsecase) RefreshSession(_ context.Context, command biz.RefreshSessionCommand) (biz.RefreshSessionResult, error) {
	u.refreshCommand = command
	return u.refreshResult, u.refreshErr
}

func (u *recordingAuthenticationUsecase) LogoutSession(_ context.Context, command biz.LogoutSessionCommand) (biz.LogoutSessionResult, error) {
	u.logoutCommand = command
	return biz.LogoutSessionResult{}, u.logoutErr
}

func (u *recordingAuthenticationUsecase) SwitchTenant(_ context.Context, command biz.SwitchTenantCommand) (biz.SwitchTenantResult, error) {
	u.switchCommand = command
	return u.switchResult, u.switchErr
}

func (u *recordingAuthenticationUsecase) RequestPasswordAction(_ context.Context, command biz.RequestPasswordActionCommand) (biz.RequestPasswordActionResult, error) {
	u.requestCalls++
	u.requestCommand = command
	return u.requestResult, u.requestErr
}

func (u *recordingAuthenticationUsecase) CompletePasswordAction(_ context.Context, command biz.CompletePasswordActionCommand) (biz.CompletePasswordActionResult, error) {
	u.completeCalls++
	u.completeCommand = command
	return u.completeResult, u.completeErr
}

func (u *recordingAuthenticationUsecase) PasswordLogin(_ context.Context, command biz.PasswordLoginCommand) (biz.PasswordLoginResult, error) {
	u.calls++
	u.command = command
	return u.result, u.err
}
