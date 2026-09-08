package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testPolicyRevision = "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa"

func TestCheckPermissionAllowsRegisteredOperationOnce(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	sessionID := uuid.MustParse("0198f062-b76d-7001-9000-000000000001")
	grantID := uuid.MustParse("0198f062-b76d-7001-9000-000000000002")
	decisionID := uuid.MustParse("0198f062-b76d-7001-9000-000000000007")
	reader := &recordingAuthorizationReader{state: AuthorizationState{
		PrincipalStatus:   PrincipalStatusActive,
		MembershipStatus:  MembershipStatusActive,
		TenantAccess:      TenantAccessStatusActive,
		Lifecycle:         TenantLifecycleStatusActive,
		LifecycleFresh:    true,
		SessionStatus:     SessionStatusActive,
		GrantStatus:       GrantStatusActive,
		GrantVersion:      1,
		PermissionAllowed: true,
	}}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision},
		&staticAccessTokenVerifier{claims: AccessTokenClaims{
			Subject:      principalID,
			Audience:     AudienceConsole,
			SessionID:    sessionID,
			GrantID:      grantID,
			GrantVersion: 1,
			TenantID:     tenantID,
			ExpiresAt:    time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC),
			AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
		}},
		reader,
		&fixedIDs{values: []uuid.UUID{decisionID}},
		fixedAuthClock{now: time.Date(2026, 9, 4, 6, 50, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential:  "signed-access-token",
		OperationID:    "listInstances",
		PolicyRevision: testPolicyRevision,
		TargetTenantID: tenantID,
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if !decision.Allowed || decision.Reason != AuthorizationReasonAllowed || decision.DecisionID != decisionID {
		t.Fatalf("decision = %#v", decision)
	}
	if decision.Principal.ID != principalID || decision.Principal.SessionID != sessionID || decision.Principal.GrantID != grantID {
		t.Fatalf("principal context = %#v", decision.Principal)
	}
	if reader.calls != 1 || reader.lookup.Resource != "instances" || len(reader.lookup.Actions) != 1 || reader.lookup.Actions[0] != "read" {
		t.Fatalf("reader calls/lookup = %d / %#v", reader.calls, reader.lookup)
	}
}

func TestCheckPermissionAuthenticatesAPIKeyAsServicePrincipalWithoutSession(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-4000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-4000-7001-9000-000000000002")
	keyID := uuid.MustParse("0199ca10-4000-7001-9000-000000000003")
	decisionID := uuid.MustParse("0199ca10-4000-7001-9000-000000000004")
	raw := "ani_" + keyID.String() + "_service-secret"
	reader := &recordingAuthorizationReader{apiKeyState: APIKeyAuthorizationState{
		APIKey:          APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true},
		TenantID:        tenantID,
		PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive,
		LifecycleFresh: true, PermissionAllowed: true,
	}}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAccessToken, CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman, PrincipalTypeService},
		}}, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{decisionID}}, fixedAuthClock{now: time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)}, reader,
	)

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision, TargetTenantID: tenantID,
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if !decision.Allowed || decision.Principal.ID != principalID || decision.Principal.Type != PrincipalTypeService || decision.Principal.SessionID != uuid.Nil || decision.Principal.GrantID != uuid.Nil || reader.apiKeyCalls != 1 || reader.apiKeyUseCalls != 1 {
		t.Fatalf("API key decision = %#v lookup/use calls=%d/%d", decision, reader.apiKeyCalls, reader.apiKeyUseCalls)
	}
}

func TestCheckPermissionFailsClosedWhenRequiredAPIKeyUsageObserverIsMissing(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-4020-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-4020-7001-9000-000000000002")
	keyID := uuid.MustParse("0199ca10-4020-7001-9000-000000000003")
	raw := "ani_" + keyID.String() + "_missing-observer-secret"
	reader := &recordingAuthorizationReader{apiKeyState: APIKeyAuthorizationState{
		APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true},
		TenantID: tenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive,
		LifecycleFresh: true, PermissionAllowed: true,
	}}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
		}},
		&staticAccessTokenVerifier{},
		reader,
		&fixedIDs{},
		fixedAuthClock{now: time.Date(2026, 9, 8, 16, 2, 0, 0, time.UTC)},
		nil,
	)

	_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision, TargetTenantID: tenantID,
	})
	if !errors.Is(err, ErrAuthorizationDependency) {
		t.Fatalf("CheckPermission() error = %v, want %v", err, ErrAuthorizationDependency)
	}
}

func TestCheckPermissionAuthenticatesAPIKeyBeforeCrossTenantDenial(t *testing.T) {
	credentialTenantID := uuid.MustParse("0199ca10-4050-7001-9000-000000000001")
	targetTenantID := uuid.MustParse("0199ca10-4050-7001-9000-000000000002")
	principalID := uuid.MustParse("0199ca10-4050-7001-9000-000000000003")
	keyID := uuid.MustParse("0199ca10-4050-7001-9000-000000000004")
	decisionID := uuid.MustParse("0199ca10-4050-7001-9000-000000000005")
	auditID := uuid.MustParse("0199ca10-4050-7001-9000-000000000006")
	raw := "ani_" + keyID.String() + "_cross-tenant-secret"
	reader := &recordingAuthorizationReader{apiKeyState: APIKeyAuthorizationState{
		APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true},
		TenantID: credentialTenantID, PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true, PermissionAllowed: true,
	}}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
		}}, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{
			decisionID, auditID, uuid.MustParse("0199ca10-4050-7001-9000-000000000007"),
		}}, fixedAuthClock{now: time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision, TargetTenantID: targetTenantID,
	})
	if err != nil || decision.Allowed || decision.Reason != AuthorizationReasonTenantMismatch || decision.DecisionID != decisionID {
		t.Fatalf("cross-tenant decision = %#v, error = %v", decision, err)
	}
	if reader.apiKeyCalls != 1 || reader.apiKeyUseCalls != 0 {
		t.Fatalf("cross-tenant lookup/use calls = %d/%d", reader.apiKeyCalls, reader.apiKeyUseCalls)
	}
	if reader.deniedAudit == nil || reader.deniedAudit.ID != auditID || reader.deniedAudit.Action != AuditActionAuthorizationDenied ||
		reader.deniedAudit.ActorID != principalID || reader.deniedAudit.AuthenticationMethod != AuditAuthenticationMethodAPIKey ||
		reader.deniedAudit.TargetID != decisionID || reader.deniedAudit.DecisionID != decisionID.String() ||
		reader.deniedAudit.Reason != AuditReason(AuthorizationReasonTenantMismatch) {
		t.Fatalf("denied authorization audit = %#v", reader.deniedAudit)
	}

	_, err = usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: "ani_" + keyID.String() + "_wrong-secret", OperationID: "createInstance",
		PolicyRevision: testPolicyRevision, TargetTenantID: targetTenantID,
	})
	if !errors.Is(err, ErrAuthorizationCredentialInvalid) {
		t.Fatalf("wrong-secret cross-tenant error = %v, want credential invalid", err)
	}
	if !reader.unboundAudit || reader.deniedAudit == nil || reader.deniedAudit.Boundary != AuditBoundaryPrincipal ||
		reader.deniedAudit.AuthenticationMethod != AuditAuthenticationMethodAnonymous {
		t.Fatalf("wrong-secret audit trusted target tenant: %#v, unbound=%t", reader.deniedAudit, reader.unboundAudit)
	}
}

func TestCheckPermissionDeniesAPIKeyWhenOperationRequiresAccessToken(t *testing.T) {
	keyID := uuid.MustParse("0199ca10-4100-7001-9000-000000000001")
	reader := &recordingAuthorizationReader{}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createIAMAPIKey", Resource: "iam.api-keys", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAccessToken}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman},
		}}, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199ca10-4100-7001-9000-000000000002"),
			uuid.MustParse("0199ca10-4100-7001-9000-000000000004"),
		}}, fixedAuthClock{}, allowingAPIKeyUsageObserver{})

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: "ani_" + keyID.String() + "_secret", OperationID: "createIAMAPIKey",
		PolicyRevision: testPolicyRevision, TargetTenantID: uuid.MustParse("0199ca10-4100-7001-9000-000000000003"),
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if decision.Allowed || decision.Reason != AuthorizationReasonCredentialKindDenied || reader.apiKeyCalls != 0 {
		t.Fatalf("decision = %#v calls=%d", decision, reader.apiKeyCalls)
	}
	if reader.deniedAudit == nil || reader.deniedAudit.Action != AuditActionAuthorizationDenied ||
		reader.deniedAudit.AuthenticationMethod != AuditAuthenticationMethodAnonymous || !reader.unboundAudit ||
		reader.deniedAudit.Boundary != AuditBoundaryPrincipal {
		t.Fatalf("credential-kind denial audit = %#v", reader.deniedAudit)
	}
}

func TestCheckPermissionRejectsInvalidAPIKeyBeforeLifecycleLookup(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-4125-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-4125-7001-9000-000000000002")
	keyID := uuid.MustParse("0199ca10-4125-7001-9000-000000000003")
	raw := "ani_" + keyID.String() + "_credential-first"
	baseReader := func() *recordingAuthorizationReader {
		return &recordingAuthorizationReader{
			authorizationErr: ErrTenantLifecycleStale,
			apiKeyState: APIKeyAuthorizationState{
				APIKey:   APIKey{ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Digest: sha256.Sum256([]byte(raw)), NeverExpires: true, Version: 1},
				TenantID: tenantID,
			},
		}
	}
	registry := staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
		OperationID: "createInstance", Resource: "instances", Actions: []string{"create"}, Scope: PermissionScopeTenant,
		CredentialKinds: []CredentialKind{CredentialKindAPIKey}, PrincipalKinds: []PrincipalType{PrincipalTypeService},
	}}

	t.Run("wrong secret", func(t *testing.T) {
		reader := baseReader()
		usecase := NewAuthorizationUsecase(registry, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199ca10-4125-7001-9000-000000000004"),
		}}, fixedAuthClock{now: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)}, reader)
		_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
			RawCredential: "ani_" + keyID.String() + "_wrong", OperationID: "createInstance", PolicyRevision: testPolicyRevision, TargetTenantID: tenantID,
		})
		if !errors.Is(err, ErrAuthorizationCredentialInvalid) || reader.apiKeyCalls != 0 || !reader.unboundAudit {
			t.Fatalf("wrong-secret result error=%v auth-lookups=%d unbound=%t", err, reader.apiKeyCalls, reader.unboundAudit)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		reader := baseReader()
		reader.apiKeyState.APIKey.Status = APIKeyStatusRevoked
		usecase := NewAuthorizationUsecase(registry, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199ca10-4125-7001-9000-000000000005"),
		}}, fixedAuthClock{now: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)}, reader)
		_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
			RawCredential: raw, OperationID: "createInstance", PolicyRevision: testPolicyRevision, TargetTenantID: tenantID,
		})
		if !errors.Is(err, ErrAuthorizationCredentialInvalid) || reader.apiKeyCalls != 0 || !reader.unboundAudit {
			t.Fatalf("revoked result error=%v auth-lookups=%d unbound=%t", err, reader.apiKeyCalls, reader.unboundAudit)
		}
	})
}

func TestCheckPermissionInvalidAccessTokenCannotSelectTenantAudit(t *testing.T) {
	reader := &recordingAuthorizationReader{}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision},
		&staticAccessTokenVerifier{err: ErrInvalidCredential},
		reader,
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199ca10-4140-7001-9000-000000000001"),
			uuid.MustParse("0199ca10-4140-7001-9000-000000000002"),
		}},
		fixedAuthClock{now: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	for _, targetTenantID := range []uuid.UUID{
		uuid.MustParse("0199ca10-4140-7001-9000-000000000003"),
		uuid.MustParse("0199ca10-4140-7001-9000-000000000004"),
	} {
		_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
			RawCredential: "invalid-access-token", OperationID: "listInstances",
			PolicyRevision: testPolicyRevision, TargetTenantID: targetTenantID,
		})
		if !errors.Is(err, ErrAuthorizationCredentialInvalid) {
			t.Fatalf("target %s error = %v, want credential invalid", targetTenantID, err)
		}
	}
	if reader.tenantAuditCalls != 0 || reader.unboundAuditCalls != 2 || reader.deniedAudit == nil ||
		reader.deniedAudit.Boundary != AuditBoundaryPrincipal || reader.deniedAudit.AuthenticationMethod != AuditAuthenticationMethodAnonymous {
		t.Fatalf("invalid-token audit routing tenant=%d unbound=%d event=%#v", reader.tenantAuditCalls, reader.unboundAuditCalls, reader.deniedAudit)
	}
}

func TestCheckPermissionFailsClosedWhenDeniedAuthorizationAuditFails(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-4150-7001-9000-000000000001")
	reader := &recordingAuthorizationReader{deniedAuditErr: ErrPersistenceUnavailable}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "createIAMAPIKey", Resource: "iam.api-keys", Actions: []string{"create"}, Scope: PermissionScopeTenant,
			CredentialKinds: []CredentialKind{CredentialKindAccessToken}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman},
		}}, &staticAccessTokenVerifier{}, reader, &fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199ca10-4150-7001-9000-000000000002"),
			uuid.MustParse("0199ca10-4150-7001-9000-000000000003"),
		}}, fixedAuthClock{now: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: "ani_0199ca10-4150-7001-9000-000000000004_secret",
		OperationID:   "createIAMAPIKey", PolicyRevision: testPolicyRevision, TargetTenantID: tenantID,
	})
	if !errors.Is(err, ErrAuthorizationDependency) || !errors.Is(err, ErrPersistenceUnavailable) || decision.DecisionID != uuid.Nil {
		t.Fatalf("CheckPermission() = decision:%#v error:%v", decision, err)
	}
}

func TestCheckPermissionReturnsRegisteredTypedObligationsOnAllow(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	registry := staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
		OperationID:     "getInstance",
		Resource:        "instances",
		Actions:         []string{"get"},
		Scope:           PermissionScopeTenant,
		Obligations:     []AuthorizationObligation{{Type: AuthorizationObligationResourceTenantMatch, Handler: "core.resource_tenant"}},
		CredentialKinds: []CredentialKind{CredentialKindAccessToken},
		PrincipalKinds:  []PrincipalType{PrincipalTypeHuman},
	}}
	usecase := NewAuthorizationUsecase(
		registry,
		&staticAccessTokenVerifier{claims: AccessTokenClaims{
			Subject:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			SessionID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
			GrantID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
			GrantVersion: 1, TenantID: tenantID,
			ExpiresAt: time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC),
		}},
		&recordingAuthorizationReader{state: AuthorizationState{
			PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive,
			TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive,
			LifecycleFresh: true, SessionStatus: SessionStatusActive, GrantStatus: GrantStatusActive,
			GrantVersion: 1, PermissionAllowed: true,
		}},
		&fixedIDs{values: []uuid.UUID{uuid.MustParse("0198f062-b76d-7001-9000-000000000007")}},
		fixedAuthClock{now: time.Date(2026, 9, 4, 6, 50, 0, 0, time.UTC)}, allowingAPIKeyUsageObserver{})

	decision, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: "signed-access-token", OperationID: "getInstance",
		PolicyRevision: testPolicyRevision, TargetTenantID: tenantID, TargetResourceID: "instance-1",
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if len(decision.Obligations) != 1 || decision.Obligations[0].Type != AuthorizationObligationResourceTenantMatch || decision.Obligations[0].Handler != "core.resource_tenant" || decision.Obligations[0].ResourceID != "instance-1" || decision.Obligations[0].ExpectedTenantID != tenantID {
		t.Fatalf("decision obligations = %#v", decision.Obligations)
	}
	decision.Obligations[0].Handler = "mutated"
	policy, _ := registry.Lookup("getInstance")
	if policy.Obligations[0].Handler != "core.resource_tenant" {
		t.Fatalf("decision leaked registry obligation storage = %#v", policy.Obligations)
	}
}

func TestCheckPermissionRejectsNonTenantPolicyBeforeCredentialOrStorage(t *testing.T) {
	verifier := &staticAccessTokenVerifier{}
	reader := &recordingAuthorizationReader{}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision, policy: AuthorizationPolicy{
			OperationID: "getPlatformUser", Resource: "iam.platform-memberships",
			Actions: []string{"read"}, Scope: PermissionScopePlatform,
		}}, verifier, reader, &fixedIDs{}, fixedAuthClock{}, allowingAPIKeyUsageObserver{})

	_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential: "signed-access-token", OperationID: "getPlatformUser",
		PolicyRevision: testPolicyRevision, TargetTenantID: uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
	})
	if !errors.Is(err, ErrAuthorizationOperationUnregistered) {
		t.Fatalf("CheckPermission() error = %v, want fail-closed %v", err, ErrAuthorizationOperationUnregistered)
	}
	if verifier.calls != 0 || reader.calls != 0 {
		t.Fatalf("non-tenant policy reached dependencies: verifier=%d reader=%d", verifier.calls, reader.calls)
	}
}

func TestCheckPermissionFailsClosedOnPolicyRevisionMismatch(t *testing.T) {
	verifier := &staticAccessTokenVerifier{}
	reader := &recordingAuthorizationReader{}
	usecase := NewAuthorizationUsecase(
		staticPolicyRegistry{revision: testPolicyRevision},
		verifier,
		reader,
		&fixedIDs{},
		fixedAuthClock{}, allowingAPIKeyUsageObserver{})

	_, err := usecase.CheckPermission(context.Background(), CheckPermissionCommand{
		RawCredential:  "signed-access-token",
		OperationID:    "listInstances",
		PolicyRevision: "sha256:wrong",
		TargetTenantID: uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
	})
	if !errors.Is(err, ErrAuthorizationPolicyMismatch) {
		t.Fatalf("CheckPermission() error = %v, want %v", err, ErrAuthorizationPolicyMismatch)
	}
	if verifier.calls != 0 || reader.calls != 0 {
		t.Fatalf("dependencies called on policy mismatch: verifier=%d reader=%d", verifier.calls, reader.calls)
	}
}

type staticPolicyRegistry struct {
	revision string
	policy   AuthorizationPolicy
}

func (r staticPolicyRegistry) Revision() string { return r.revision }

func (r staticPolicyRegistry) Lookup(operationID string) (AuthorizationPolicy, bool) {
	if r.policy.OperationID != "" {
		if operationID != r.policy.OperationID {
			return AuthorizationPolicy{}, false
		}
		policy := r.policy
		policy.Actions = append([]string(nil), r.policy.Actions...)
		policy.Obligations = append([]AuthorizationObligation(nil), r.policy.Obligations...)
		return policy, true
	}
	if operationID != "listInstances" {
		return AuthorizationPolicy{}, false
	}
	return AuthorizationPolicy{
		OperationID: operationID, Resource: "instances", Actions: []string{"read"}, Scope: PermissionScopeTenant,
		CredentialKinds: []CredentialKind{CredentialKindAccessToken}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman},
	}, true
}

type staticAccessTokenVerifier struct {
	claims AccessTokenClaims
	err    error
	calls  int
}

func (v *staticAccessTokenVerifier) Verify(context.Context, string) (AccessTokenClaims, error) {
	v.calls++
	return v.claims, v.err
}

type recordingAuthorizationReader struct {
	state             AuthorizationState
	err               error
	calls             int
	lookup            AuthorizationLookup
	apiKeyState       APIKeyAuthorizationState
	boundaryErr       error
	authorizationErr  error
	apiKeyCalls       int
	apiKeyUseCalls    int
	deniedAudit       *SecurityAuditEvent
	deniedAuditErr    error
	unboundAudit      bool
	tenantAuditCalls  int
	unboundAuditCalls int
}

func (r *recordingAuthorizationReader) GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error) {
	if r.boundaryErr != nil {
		return uuid.Nil, r.boundaryErr
	}
	if r.apiKeyState.TenantID == uuid.Nil {
		return uuid.Nil, ErrAPIKeyNotFound
	}
	return r.apiKeyState.TenantID, nil
}

func (r *recordingAuthorizationReader) LookupAPIKeyCredential(_ context.Context, _ TenantScope, _ uuid.UUID, rawCredential string) (APIKey, error) {
	if r.apiKeyState.APIKey.Digest != sha256.Sum256([]byte(rawCredential)) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	return r.apiKeyState.APIKey, nil
}

func (r *recordingAuthorizationReader) LookupAPIKeyAuthorization(_ context.Context, _ TenantScope, _ uuid.UUID, _ string, _ []string) (APIKeyAuthorizationState, error) {
	r.apiKeyCalls++
	return r.apiKeyState, r.authorizationErr
}

func (r *recordingAuthorizationReader) ObserveAPIKeyUse(context.Context, TenantScope, uuid.UUID, time.Time) error {
	r.apiKeyUseCalls++
	return nil
}

func (r *recordingAuthorizationReader) LookupAuthorization(_ context.Context, _ TenantScope, lookup AuthorizationLookup) (AuthorizationState, error) {
	r.calls++
	r.lookup = lookup
	return r.state, r.err
}

func (r *recordingAuthorizationReader) RecordDeniedAuthorization(_ context.Context, _ TenantScope, event SecurityAuditEvent) error {
	r.tenantAuditCalls++
	r.deniedAudit = &event
	return r.deniedAuditErr
}

func (r *recordingAuthorizationReader) RecordUnboundAuthorization(_ context.Context, event SecurityAuditEvent) error {
	r.unboundAuditCalls++
	r.deniedAudit = &event
	r.unboundAudit = true
	return r.deniedAuditErr
}
