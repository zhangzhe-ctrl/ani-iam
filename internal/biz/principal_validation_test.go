package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidatePrincipalAuthenticatesAPIKeyWithoutSessionOrGrant(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-1000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199d080-1000-7001-9000-000000000002")
	keyID := uuid.MustParse("0199d080-1000-7001-9000-000000000003")
	decisionID := uuid.MustParse("0199d080-1000-7001-9000-000000000004")
	auditID := uuid.MustParse("0199d080-1000-7001-9000-000000000005")
	raw := "ani_" + keyID.String() + "_principal-validation-secret"
	reader := &recordingPrincipalValidationReader{
		registry: staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAccessToken, CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman, PrincipalTypeService},
		}},
		boundaryTenantID: tenantID,
		apiKeyState: APIKeyAuthorizationState{
			APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true, Version: 1},
			TenantID: tenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
			TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		},
	}
	uow := &recordingLoginUnitOfWork{}
	usecase := newPrincipalValidationUsecaseWithUOW(reader, uow, &fixedIDs{values: []uuid.UUID{decisionID, auditID}}, time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC))

	result, err := usecase.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision,
		RequestID: "request-api-key", CorrelationID: "correlation-api-key",
	})
	if err != nil {
		t.Fatalf("ValidatePrincipal() error = %v", err)
	}
	if result.DecisionID != decisionID || result.PolicyRevision != testPolicyRevision {
		t.Fatalf("result identity = %#v", result)
	}
	if result.Principal.ID != principalID || result.Principal.Type != PrincipalTypeService || result.Principal.TenantID != tenantID ||
		result.Principal.SessionID != uuid.Nil || result.Principal.GrantID != uuid.Nil ||
		len(result.Principal.AuthnMethods) != 1 || result.Principal.AuthnMethods[0] != AuditAuthenticationMethodAPIKey {
		t.Fatalf("principal context = %#v", result.Principal)
	}
	if reader.boundaryCalls != 1 || reader.apiKeyCalls != 1 || reader.apiKeyUseCalls != 1 {
		t.Fatalf("boundary/lookup/use calls = %d/%d/%d", reader.boundaryCalls, reader.apiKeyCalls, reader.apiKeyUseCalls)
	}
	if uow.tenantPrincipalValidation == nil || uow.tenantPrincipalValidation.ID != auditID ||
		uow.tenantPrincipalValidation.ActorID != principalID ||
		uow.tenantPrincipalValidation.AuthenticationMethod != AuditAuthenticationMethodAPIKey ||
		uow.tenantPrincipalValidation.Action != AuditActionPrincipalValidationSucceeded ||
		uow.tenantPrincipalValidation.TargetID != keyID ||
		uow.tenantPrincipalValidation.DecisionID != decisionID.String() ||
		uow.tenantPrincipalValidation.RequestID != "request-api-key" ||
		uow.tenantPrincipalValidation.CorrelationID != "correlation-api-key" {
		t.Fatalf("principal validation audit = %#v", uow.tenantPrincipalValidation)
	}
}

func TestValidatePrincipalFailsClosedWhenRequiredAPIKeyUsageObserverIsMissing(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-1050-7001-9000-000000000001")
	principalID := uuid.MustParse("0199d080-1050-7001-9000-000000000002")
	keyID := uuid.MustParse("0199d080-1050-7001-9000-000000000003")
	raw := "ani_" + keyID.String() + "_missing-observer-secret"
	reader := &recordingPrincipalValidationReader{
		registry: staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
		}},
		boundaryTenantID: tenantID,
		apiKeyState: APIKeyAuthorizationState{
			APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true, Version: 1},
			TenantID: tenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
			TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		},
	}
	usecase := NewAuthenticationUsecase(
		reader,
		acceptingPasswordVerifier{},
		allowingLoginThrottle{},
		&recordingLoginUnitOfWork{},
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{},
		fixedAuthClock{now: time.Date(2026, 9, 9, 1, 5, 0, 0, time.UTC)},
		nil,
	)

	_, err := usecase.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision,
	})
	if !errors.Is(err, ErrAuthenticationDependency) {
		t.Fatalf("ValidatePrincipal() error = %v, want %v", err, ErrAuthenticationDependency)
	}
}

func TestValidatePrincipalRejectsDisallowedAPIKeyBeforeCredentialLookup(t *testing.T) {
	reader := &recordingPrincipalValidationReader{registry: staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
		OperationID: "createIAMAPIKey", Resource: "iam.api-keys", Actions: []string{"create"}, Scope: PermissionScopeTenant,
		CredentialKinds: []CredentialKind{CredentialKindAccessToken}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman},
	}}}
	uow := &recordingLoginUnitOfWork{}
	usecase := newPrincipalValidationUsecaseWithUOW(reader, uow, &fixedIDs{values: []uuid.UUID{
		uuid.MustParse("0199d080-1100-7001-9000-000000000002"),
	}}, time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC))

	_, err := usecase.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{
		RawCredential: "ani_0199d080-1100-7001-9000-000000000001_secret", OperationID: "createIAMAPIKey", PolicyRevision: testPolicyRevision,
	})
	if !errors.Is(err, ErrAuthenticationCredentialKindDenied) {
		t.Fatalf("ValidatePrincipal() error = %v, want credential-kind denial", err)
	}
	if reader.boundaryCalls != 0 || reader.apiKeyCalls != 0 || reader.apiKeyUseCalls != 0 {
		t.Fatalf("credential lookup occurred before fail-closed denial: %d/%d/%d", reader.boundaryCalls, reader.apiKeyCalls, reader.apiKeyUseCalls)
	}
	if uow.unboundPrincipalValidation == nil || uow.unboundPrincipalValidation.Action != AuditActionPrincipalValidationFailed ||
		uow.unboundPrincipalValidation.AuthenticationMethod != AuditAuthenticationMethodAnonymous ||
		uow.unboundPrincipalValidation.Reason != AuditReasonInvalidCredential {
		t.Fatalf("failed principal validation audit = %#v", uow.unboundPrincipalValidation)
	}
}

func TestValidatePrincipalFailsClosedWhenRequiredAuditCannotBeRecorded(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-1150-7001-9000-000000000001")
	principalID := uuid.MustParse("0199d080-1150-7001-9000-000000000002")
	keyID := uuid.MustParse("0199d080-1150-7001-9000-000000000003")
	raw := "ani_" + keyID.String() + "_audit-failure-secret"
	reader := &recordingPrincipalValidationReader{
		registry: staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
		}},
		boundaryTenantID: tenantID,
		apiKeyState: APIKeyAuthorizationState{
			APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true, Version: 1},
			TenantID: tenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
			TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		},
	}
	uow := &recordingLoginUnitOfWork{principalValidationErr: ErrPersistenceUnavailable}
	usecase := newPrincipalValidationUsecaseWithUOW(reader, uow, &fixedIDs{values: []uuid.UUID{
		uuid.MustParse("0199d080-1150-7001-9000-000000000004"),
		uuid.MustParse("0199d080-1150-7001-9000-000000000005"),
	}}, time.Date(2026, 9, 9, 1, 30, 0, 0, time.UTC))

	result, err := usecase.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision,
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, ErrPersistenceUnavailable) || result.Principal.ID != uuid.Nil {
		t.Fatalf("ValidatePrincipal() = result:%#v error:%v", result, err)
	}
}

func TestValidatePrincipalAPIKeyFailureMatrix(t *testing.T) {
	tenantID := uuid.MustParse("0199d080-1200-7001-9000-000000000001")
	principalID := uuid.MustParse("0199d080-1200-7001-9000-000000000002")
	keyID := uuid.MustParse("0199d080-1200-7001-9000-000000000003")
	auditID := uuid.MustParse("0199d080-1200-7001-9000-000000000004")
	now := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	raw := "ani_" + keyID.String() + "_failure-matrix-secret"
	baseReader := func() *recordingPrincipalValidationReader {
		return &recordingPrincipalValidationReader{
			registry: staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
				OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
				CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
			}},
			boundaryTenantID: tenantID,
			apiKeyState: APIKeyAuthorizationState{
				APIKey: APIKey{
					ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive,
					Digest: sha256.Sum256([]byte(raw)), NeverExpires: true, Version: 1,
				},
				TenantID: tenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
				TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
			},
		}
	}
	tests := []struct {
		name          string
		rawCredential string
		mutate        func(*recordingPrincipalValidationReader)
		want          error
		wantTenant    bool
		wantUseCalls  int
	}{
		{name: "malformed", rawCredential: "ani_not-a-uuid_secret", want: ErrInvalidCredential},
		{name: "unknown", mutate: func(reader *recordingPrincipalValidationReader) { reader.boundaryErr = ErrAPIKeyNotFound }, want: ErrInvalidCredential},
		{name: "digest mismatch", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.APIKey.Digest = sha256.Sum256([]byte("other"))
		}, want: ErrInvalidCredential},
		{name: "revoked", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.APIKey.Status = APIKeyStatusRevoked
		}, want: ErrInvalidCredential},
		{name: "expired", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.APIKey.NeverExpires = false
			reader.apiKeyState.APIKey.ExpiresAt = now
		}, want: ErrInvalidCredential},
		{name: "principal inactive", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.PrincipalStatus = PrincipalStatusDisabled
		}, want: ErrPrincipalInactive, wantTenant: true},
		{name: "membership inactive", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.MembershipStatus = MembershipStatusRemoved
		}, want: ErrMembershipInactive, wantTenant: true},
		{name: "tenant access inactive", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.TenantAccess = TenantAccessStatusSuspended
		}, want: ErrTenantAccessInactive, wantTenant: true},
		{name: "lifecycle blocked", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.Lifecycle = TenantLifecycleStatus("suspended")
		}, want: ErrTenantLifecycleBlocked, wantTenant: true},
		{name: "lifecycle stale", mutate: func(reader *recordingPrincipalValidationReader) { reader.apiKeyErr = ErrTenantLifecycleStale }, want: ErrTenantLifecycleStale, wantTenant: true},
		{name: "tenant IAM not ready", mutate: func(reader *recordingPrincipalValidationReader) { reader.apiKeyErr = ErrTenantIAMNotReady }, want: ErrTenantIAMNotReady, wantTenant: true},
		{name: "storage unavailable", mutate: func(reader *recordingPrincipalValidationReader) { reader.apiKeyErr = ErrPersistenceUnavailable }, want: ErrAuthenticationDependency},
		{name: "usage observer unavailable", mutate: func(reader *recordingPrincipalValidationReader) { reader.apiKeyUseErr = ErrPersistenceUnavailable }, want: ErrAuthenticationDependency, wantUseCalls: 1},
		{name: "cross-boundary state", mutate: func(reader *recordingPrincipalValidationReader) {
			reader.apiKeyState.TenantID = uuid.MustParse("0199d080-1200-7001-9000-000000000099")
		}, want: ErrAuthenticationDependency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := baseReader()
			if test.mutate != nil {
				test.mutate(reader)
			}
			credential := raw
			if test.rawCredential != "" {
				credential = test.rawCredential
			}
			usecase := newPrincipalValidationUsecase(reader, &fixedIDs{values: []uuid.UUID{auditID}}, now)
			_, err := usecase.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{
				RawCredential: credential, OperationID: "createInstance", PolicyRevision: testPolicyRevision,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidatePrincipal() error = %v, want %v", err, test.want)
			}
			boundTenantID, hasTenant := TenantIDFromAuthenticationError(err)
			if hasTenant != test.wantTenant || (hasTenant && boundTenantID != tenantID) {
				t.Fatalf("bound tenant = %s/%t, want %s/%t", boundTenantID, hasTenant, tenantID, test.wantTenant)
			}
			if reader.apiKeyUseCalls != test.wantUseCalls {
				t.Fatalf("failed validation use calls = %d, want %d", reader.apiKeyUseCalls, test.wantUseCalls)
			}
		})
	}
}

type principalValidationTestReader interface {
	AuthenticationReader
	APIKeyUsageObserver
}

func newPrincipalValidationUsecase(reader principalValidationTestReader, ids IDGenerator, now time.Time) *AuthenticationUsecase {
	return newPrincipalValidationUsecaseWithUOW(reader, &recordingLoginUnitOfWork{}, ids, now)
}

func newPrincipalValidationUsecaseWithUOW(reader principalValidationTestReader, uow AuthenticationUnitOfWork, ids IDGenerator, now time.Time) *AuthenticationUsecase {
	return NewAuthenticationUsecase(
		reader,
		acceptingPasswordVerifier{},
		allowingLoginThrottle{},
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		ids,
		fixedAuthClock{now: now},
		reader,
	)
}

type recordingPrincipalValidationReader struct {
	*sessionTestReader
	registry         staticPolicyRegistry
	boundaryTenantID uuid.UUID
	boundaryErr      error
	boundaryCalls    int
	apiKeyState      APIKeyAuthorizationState
	apiKeyErr        error
	apiKeyCalls      int
	apiKeyUseErr     error
	apiKeyUseCalls   int
}

func (r *recordingPrincipalValidationReader) LookupPasswordLogin(context.Context, TenantScope, string) (PasswordLoginState, error) {
	return PasswordLoginState{}, ErrInvalidCredential
}

func (r *recordingPrincipalValidationReader) LookupPasswordActionTarget(context.Context, string, Audience) (PasswordActionTarget, bool, error) {
	return PasswordActionTarget{}, false, nil
}

func (r *recordingPrincipalValidationReader) LookupRefreshSession(context.Context, [sha256.Size]byte) (RefreshSessionState, error) {
	return RefreshSessionState{}, ErrInvalidCredential
}

func (r *recordingPrincipalValidationReader) LookupLogoutSession(context.Context, [sha256.Size]byte) (LogoutSessionState, bool, error) {
	return LogoutSessionState{}, false, nil
}

func (r *recordingPrincipalValidationReader) LookupTenantSwitch(context.Context, TenantScope, AccessTokenClaims) (TenantSwitchState, error) {
	return TenantSwitchState{}, ErrInvalidCredential
}

func (r *recordingPrincipalValidationReader) Revision() string { return r.registry.Revision() }

func (r *recordingPrincipalValidationReader) Lookup(operationID string) (AuthorizationPolicy, bool) {
	return r.registry.Lookup(operationID)
}

func (r *recordingPrincipalValidationReader) GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error) {
	r.boundaryCalls++
	return r.boundaryTenantID, r.boundaryErr
}

func (r *recordingPrincipalValidationReader) LookupAuthorization(context.Context, TenantScope, AuthorizationLookup) (AuthorizationState, error) {
	return AuthorizationState{}, nil
}

func (r *recordingPrincipalValidationReader) RecordDeniedAuthorization(context.Context, TenantScope, SecurityAuditEvent) error {
	return nil
}

func (r *recordingPrincipalValidationReader) RecordUnboundAuthorization(context.Context, SecurityAuditEvent) error {
	return nil
}

func (r *recordingPrincipalValidationReader) LookupAPIKeyCredential(_ context.Context, _ TenantScope, _ uuid.UUID, rawCredential string) (APIKey, error) {
	if r.apiKeyState.APIKey.Digest != sha256.Sum256([]byte(rawCredential)) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	return r.apiKeyState.APIKey, nil
}

func (r *recordingPrincipalValidationReader) LookupAPIKeyAuthorization(_ context.Context, _ TenantScope, _ uuid.UUID, _ string, _ []string) (APIKeyAuthorizationState, error) {
	r.apiKeyCalls++
	return r.apiKeyState, r.apiKeyErr
}

func (r *recordingPrincipalValidationReader) ObserveAPIKeyUse(context.Context, TenantScope, uuid.UUID, time.Time) error {
	r.apiKeyUseCalls++
	return r.apiKeyUseErr
}
