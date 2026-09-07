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
		serviceAuthenticationClock{},
	)
}

type servicePasswordLoginReader struct{ state biz.PasswordLoginState }

func (r servicePasswordLoginReader) LookupPasswordLogin(context.Context, biz.TenantScope, string) (biz.PasswordLoginState, error) {
	return r.state, nil
}
func (servicePasswordLoginReader) LookupPasswordActionTarget(context.Context, string, biz.Audience) (biz.PasswordActionTarget, bool, error) {
	return biz.PasswordActionTarget{}, false, nil
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

type failingServiceLoginUnitOfWork struct{ err error }

func (u failingServiceLoginUnitOfWork) CommitLogin(context.Context, biz.TenantScope, biz.LoginMutation) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RecordLoginFailure(context.Context, biz.TenantScope, biz.LoginFailureMutation) error {
	return u.err
}
func (u failingServiceLoginUnitOfWork) RequestPasswordAction(context.Context, biz.PasswordActionRequestMutation) (biz.RequestPasswordActionResult, error) {
	return biz.RequestPasswordActionResult{}, u.err
}
func (u failingServiceLoginUnitOfWork) CompletePasswordAction(context.Context, biz.PasswordActionCompletion) (biz.CompletePasswordActionResult, error) {
	return biz.CompletePasswordActionResult{}, u.err
}

type serviceAccessTokenIssuer struct{}

func (serviceAccessTokenIssuer) Issue(context.Context, biz.AccessTokenClaims) (string, error) {
	return "service-access-token", nil
}
func (serviceAccessTokenIssuer) IssuePasswordAction(context.Context, biz.PasswordActionTokenClaims) (string, error) {
	return "test-password-action-token", nil
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
