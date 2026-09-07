package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var oidcTestTenantID = uuid.MustParse("0199c6fb-62e4-7c36-9e6e-d4b7caf78101")

func TestBeginOIDCLoginCreatesTenMinuteSingleUseOperationForExactRedirect(t *testing.T) {
	now := time.Date(2026, 9, 7, 5, 30, 0, 0, time.UTC)
	store := &recordingOIDCOperationStore{}
	provider := validatingOIDCProvider{
		name:             "dex",
		expectedState:    "test-state-secret",
		expectedNonce:    "test-nonce-secret",
		expectedVerifier: "test-verifier-secret",
		expectedRedirect: "https://console.test.example/auth/oidc/callback",
	}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider:                "dex",
		LoginRedirectURI:        "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, provider, store, &staticOIDCReader{}, &recordingOIDCUnitOfWork{}, &recordingOIDCAccessTokenIssuer{}, &sequenceOIDCSecretGenerator{values: []string{
		"test-state-secret",
		"test-nonce-secret",
		"test-verifier-secret",
	}}, &sequenceOIDCIDGenerator{values: []uuid.UUID{uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe001")}}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	result, err := usecase.BeginLogin(context.Background(), BeginOIDCLoginCommand{
		Audience:       AudienceConsole,
		TenantID:       oidcTestTenantID,
		RedirectURI:    "https://console.test.example/auth/oidc/callback",
		IdempotencyKey: "oidc-login-begin-1",
	})
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	if result.AuthorizationURL != "https://dex.test.example/auth?opaque=test-only" {
		t.Fatalf("AuthorizationURL = %q", result.AuthorizationURL)
	}
	if result.State != "test-state-secret" {
		t.Fatalf("State = %q", result.State)
	}
	if want := now.Add(10 * time.Minute); !result.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", result.ExpiresAt, want)
	}
	if store.created.Kind != OIDCFlowLogin || store.created.State != result.State || store.created.Nonce == "" || store.created.CodeVerifier == "" {
		t.Fatalf("stored operation = %#v", store.created)
	}
	if store.created.RequestFingerprint == "" || store.created.IdempotencyKey != "oidc-login-begin-1" {
		t.Fatalf("stored idempotency binding = %#v", store.created)
	}
}

func TestCompleteOIDCLoginAuthenticatesOnlyAnExistingIdentity(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 0, 0, 0, time.UTC)
	identityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe100")
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe101")
	membershipID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe102")
	ids := []uuid.UUID{
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe111"),
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe112"),
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe113"),
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe114"),
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe115"),
		uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe116"),
	}
	store := &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind:           OIDCFlowLogin,
		Provider:       "dex",
		Audience:       AudienceConsole,
		TenantID:       oidcTestTenantID,
		State:          "opaque-state",
		Nonce:          "opaque-nonce",
		CodeVerifier:   "opaque-verifier",
		RedirectURI:    "https://console.test.example/auth/oidc/callback",
		IdempotencyKey: "oidc-login-complete-1",
		CreatedAt:      now.Add(-time.Minute),
		ExpiresAt:      now.Add(9 * time.Minute),
	}}
	provider := validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
		Issuer:        "https://dex.test.example",
		Subject:       "dex-user-1",
		Email:         "User@Example.COM",
		EmailVerified: true,
	}}
	reader := &staticOIDCReader{login: OIDCLoginState{
		IdentityID:       identityID,
		PrincipalID:      principalID,
		PrincipalStatus:  PrincipalStatusActive,
		MembershipID:     membershipID,
		MembershipStatus: MembershipStatusActive,
		TenantAccess:     TenantAccessStatusActive,
		Lifecycle:        TenantLifecycleStatusActive,
		LifecycleFresh:   true,
		NormalizedEmail:  "user@example.com",
	}}
	uow := &recordingOIDCUnitOfWork{}
	tokens := &recordingOIDCAccessTokenIssuer{token: "signed-oidc-access-token"}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider:                "dex",
		LoginRedirectURI:        "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, provider, store, reader, uow, tokens, &sequenceOIDCSecretGenerator{values: []string{"refresh-secret"}}, &sequenceOIDCIDGenerator{values: ids}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	result, err := usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
		Code:        "authorization-code",
		State:       "opaque-state",
		RedirectURI: "https://console.test.example/auth/oidc/callback",
		DeviceName:  "browser",
	})
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if result.Principal.ID != principalID || result.AccessToken != "signed-oidc-access-token" || result.RefreshToken != "refresh-secret" {
		t.Fatalf("CompleteLogin() result = %#v", result)
	}
	if got := uow.login.Audit.AuthenticationMethod; got != AuditAuthenticationMethodOIDC {
		t.Fatalf("Audit authentication method = %q", got)
	}
	if len(result.Session.AuthnMethods) != 1 || result.Session.AuthnMethods[0] != AuditAuthenticationMethodOIDC {
		t.Fatalf("Session authn methods = %#v, want [oidc]", result.Session.AuthnMethods)
	}
	if uow.login.IdentityID != identityID {
		t.Fatalf("OIDC login mutation identity ID = %s, want %s", uow.login.IdentityID, identityID)
	}
	if got := uow.login.RefreshToken.Digest; got != sha256.Sum256([]byte("refresh-secret")) {
		t.Fatalf("refresh digest = %x", got)
	}
	if result.Session.Version != 1 || uow.login.Session.Version != 1 ||
		uow.login.RefreshFamily.Version != 1 ||
		uow.login.RefreshToken.Status != RefreshTokenStatusActive {
		t.Fatalf("OIDC session continuity state = result session:%#v mutation session:%#v family:%#v token:%#v",
			result.Session, uow.login.Session, uow.login.RefreshFamily, uow.login.RefreshToken)
	}
	if len(tokens.claims.AuthnMethods) != 1 || tokens.claims.AuthnMethods[0] != AuditAuthenticationMethodOIDC {
		t.Fatalf("access-token authn methods = %#v", tokens.claims.AuthnMethods)
	}
}

func TestNormalizeOIDCEmailNormalizesIDNDomainWithoutProviderAliases(t *testing.T) {
	normalized, err := normalizeOIDCEmail("  User+Invite@BÜCHER.Example  ")
	if err != nil {
		t.Fatalf("normalizeOIDCEmail() error = %v", err)
	}
	if normalized != "user+invite@xn--bcher-kva.example" {
		t.Fatalf("normalizeOIDCEmail() = %q", normalized)
	}
	if _, err := normalizeOIDCEmail("not-an-email"); err == nil {
		t.Fatal("normalizeOIDCEmail(invalid) error = nil")
	}
}

func TestCompleteOIDCLoginPreservesProviderAvailabilityClassification(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 15, 0, 0, time.UTC)
	auditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe171")
	uow := &recordingOIDCUnitOfWork{}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex", err: ErrOIDCDependency}, &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
		State: "opaque-state", Nonce: "opaque-nonce", CodeVerifier: "opaque-verifier",
		RedirectURI: "https://console.test.example/auth/oidc/callback", IdempotencyKey: "oidc-provider-unavailable",
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}, &staticOIDCReader{}, uow, &recordingOIDCAccessTokenIssuer{},
		&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{values: []uuid.UUID{auditID}}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	_, err = usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
		Code: "authorization-code", State: "opaque-state", RedirectURI: "https://console.test.example/auth/oidc/callback",
	})
	if !errors.Is(err, ErrOIDCDependency) || errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("CompleteLogin() error = %v, want only ErrOIDCDependency", err)
	}
	if uow.failure.ID != auditID || uow.failure.Action != AuditActionOIDCLoginFailed ||
		uow.failure.Result != AuditResultFailed || uow.failure.AuthenticationMethod != AuditAuthenticationMethodAnonymous ||
		uow.failure.TargetID != auditID || uow.failure.Boundary != AuditBoundaryTenant {
		t.Fatalf("OIDC dependency failure Audit = %#v", uow.failure)
	}
}

func TestCompleteOIDCLoginRejectsUnverifiedEmailBeforeMutation(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 20, 0, 0, time.UTC)
	auditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe181")
	uow := &recordingOIDCUnitOfWork{}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
		Issuer: "https://dex.test.example", Subject: "unverified-subject", Email: "user@example.com",
	}}, &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
		State: "opaque-state", Nonce: "opaque-nonce", CodeVerifier: "opaque-verifier",
		RedirectURI: "https://console.test.example/auth/oidc/callback", IdempotencyKey: "oidc-email-unverified",
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}, &staticOIDCReader{}, uow, &recordingOIDCAccessTokenIssuer{},
		&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{values: []uuid.UUID{auditID}}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	_, err = usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
		Code: "authorization-code", State: "opaque-state", RedirectURI: "https://console.test.example/auth/oidc/callback",
	})
	if !errors.Is(err, ErrOIDCEmailUnverified) || uow.login.Session.ID != uuid.Nil ||
		uow.failure.ID != auditID || uow.failure.Reason != AuditReasonOIDCLoginFailed {
		t.Fatalf("CompleteLogin() error/mutation = %v/%#v, want unverified-email rejection before mutation", err, uow.login)
	}
}

func TestCompleteOIDCLoginFailsClosedWhenFailureAuditCannotCommit(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 25, 0, 0, time.UTC)
	uow := &recordingOIDCUnitOfWork{err: errors.New("audit unavailable")}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex", err: errors.New("authorization code rejected")}, &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
		State: "opaque-state", Nonce: "opaque-nonce", CodeVerifier: "opaque-verifier",
		RedirectURI: "https://console.test.example/auth/oidc/callback", IdempotencyKey: "oidc-failure-audit",
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}, &staticOIDCReader{}, uow, &recordingOIDCAccessTokenIssuer{}, &sequenceOIDCSecretGenerator{},
		&sequenceOIDCIDGenerator{values: []uuid.UUID{uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe191")}}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	_, err = usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
		Code: "authorization-code", State: "opaque-state", RedirectURI: "https://console.test.example/auth/oidc/callback",
	})
	if !errors.Is(err, ErrOIDCDependency) || errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("CompleteLogin() error = %v, want fail-closed dependency result", err)
	}
}

func TestCompleteOIDCLoginAuditsTransactionTimeRejection(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 27, 0, 0, time.UTC)
	newUsecase := func(t *testing.T, uow *recordingOIDCUnitOfWork) *OIDCUsecase {
		t.Helper()
		identityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1a1")
		principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1a2")
		membershipID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1a3")
		ids := []uuid.UUID{
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b1"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b2"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b3"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b4"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b5"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b6"),
			uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe1b7"),
		}
		usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
			Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
			IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
			RecentReauthentication:  10 * time.Minute,
		}, validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
			Issuer: "https://dex.test.example", Subject: "dex-user-transaction-race",
			Email: "user@example.com", EmailVerified: true,
		}}, &recordingOIDCOperationStore{consumed: OIDCOperation{
			Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
			State: "transaction-race-state", Nonce: "transaction-race-nonce", CodeVerifier: "transaction-race-verifier",
			RedirectURI: "https://console.test.example/auth/oidc/callback", IdempotencyKey: "oidc-transaction-race",
			CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
		}}, &staticOIDCReader{login: OIDCLoginState{
			IdentityID: identityID, PrincipalID: principalID, PrincipalStatus: PrincipalStatusActive,
			MembershipID: membershipID, MembershipStatus: MembershipStatusActive,
			TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive,
			LifecycleFresh: true, NormalizedEmail: "user@example.com",
		}}, uow, &recordingOIDCAccessTokenIssuer{token: "signed-access-token"},
			&sequenceOIDCSecretGenerator{values: []string{"refresh-secret"}},
			&sequenceOIDCIDGenerator{values: ids}, fixedOIDCClock{now: now})
		if err != nil {
			t.Fatalf("NewOIDCUsecase() error = %v", err)
		}
		return usecase
	}

	t.Run("records the rolled-back rejection", func(t *testing.T) {
		uow := &recordingOIDCUnitOfWork{commitErr: ErrMembershipInactive}
		_, err := newUsecase(t, uow).CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
			Code: "authorization-code", State: "transaction-race-state",
			RedirectURI: "https://console.test.example/auth/oidc/callback",
		})
		if !errors.Is(err, ErrMembershipInactive) || errors.Is(err, ErrOIDCDependency) {
			t.Fatalf("CompleteLogin() error = %v, want only ErrMembershipInactive", err)
		}
		if uow.failure.Action != AuditActionOIDCLoginFailed || uow.failure.AuthenticationMethod != AuditAuthenticationMethodAnonymous ||
			uow.failure.RequestID != "oidc-transaction-race" {
			t.Fatalf("transaction-time failure Audit = %#v", uow.failure)
		}
	})

	t.Run("fails closed when rejection Audit cannot commit", func(t *testing.T) {
		uow := &recordingOIDCUnitOfWork{commitErr: ErrMembershipInactive, failureErr: errors.New("audit unavailable")}
		_, err := newUsecase(t, uow).CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
			Code: "authorization-code", State: "transaction-race-state",
			RedirectURI: "https://console.test.example/auth/oidc/callback",
		})
		if !errors.Is(err, ErrOIDCDependency) || errors.Is(err, ErrMembershipInactive) {
			t.Fatalf("CompleteLogin() error = %v, want fail-closed dependency result", err)
		}
	})
}

func TestBeginOIDCIdentityLinkRequiresRecentOnlineAuthentication(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 30, 0, 0, time.UTC)
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe201")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe202")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe203")
	store := &recordingOIDCOperationStore{}
	reader := &staticOIDCReader{reauthentication: OIDCReauthenticationState{
		PrincipalID:       principalID,
		PrincipalStatus:   PrincipalStatusActive,
		SessionID:         sessionID,
		SessionStatus:     SessionStatusActive,
		GrantID:           grantID,
		GrantStatus:       GrantStatusActive,
		GrantVersion:      3,
		TenantID:          oidcTestTenantID,
		ReauthenticatedAt: now.Add(-5 * time.Minute),
	}}
	tokens := &recordingOIDCAccessTokenIssuer{verifiedClaims: AccessTokenClaims{
		Subject: principalID, Audience: AudienceConsole, SessionID: sessionID,
		GrantID: grantID, GrantVersion: 3, TenantID: oidcTestTenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider:                "dex",
		LoginRedirectURI:        "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex"}, store, reader, &recordingOIDCUnitOfWork{}, tokens,
		&sequenceOIDCSecretGenerator{values: []string{"link-state", "link-nonce", "link-verifier"}},
		&sequenceOIDCIDGenerator{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	result, err := usecase.BeginIdentityLink(context.Background(), BeginOIDCIdentityLinkCommand{
		RawCredential:  "signed-access-token",
		Provider:       "dex",
		RedirectURI:    "https://console.test.example/auth/oidc/link/callback",
		IdempotencyKey: "oidc-link-begin-1",
	})
	if err != nil {
		t.Fatalf("BeginIdentityLink() error = %v", err)
	}
	if result.State != "link-state" || result.AuthorizationURL == "" || !result.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("BeginIdentityLink() result = %#v", result)
	}
	if store.created.Kind != OIDCFlowIdentityLink || store.created.PrincipalID != principalID || store.created.SessionID != sessionID {
		t.Fatalf("stored link operation = %#v", store.created)
	}
}

func TestOIDCIdentityLinkPreservesReauthenticationFailureClassification(t *testing.T) {
	now := time.Date(2026, 9, 7, 6, 45, 0, 0, time.UTC)
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe211")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe212")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe213")
	claims := AccessTokenClaims{
		Subject: principalID, Audience: AudienceConsole, SessionID: sessionID,
		GrantID: grantID, GrantVersion: 3, TenantID: oidcTestTenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex"}, &recordingOIDCOperationStore{},
		&staticOIDCReader{err: ErrOIDCReauthenticationRequired}, &recordingOIDCUnitOfWork{},
		&recordingOIDCAccessTokenIssuer{verifiedClaims: claims}, &sequenceOIDCSecretGenerator{},
		&sequenceOIDCIDGenerator{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	begin := BeginOIDCIdentityLinkCommand{
		RawCredential: "signed-access-token", Provider: "dex",
		RedirectURI: "https://console.test.example/auth/oidc/link/callback", IdempotencyKey: "oidc-link-reauth",
	}
	if _, err := usecase.BeginIdentityLink(context.Background(), begin); !errors.Is(err, ErrOIDCReauthenticationRequired) || errors.Is(err, ErrOIDCDependency) {
		t.Fatalf("BeginIdentityLink() error = %v, want only ErrOIDCReauthenticationRequired", err)
	}
	complete := CompleteOIDCIdentityLinkCommand{
		RawCredential: "signed-access-token", Code: "authorization-code", State: "opaque-state",
		RedirectURI: "https://console.test.example/auth/oidc/link/callback",
	}
	if _, err := usecase.CompleteIdentityLink(context.Background(), complete); !errors.Is(err, ErrOIDCReauthenticationRequired) || errors.Is(err, ErrOIDCDependency) {
		t.Fatalf("CompleteIdentityLink() error = %v, want only ErrOIDCReauthenticationRequired", err)
	}
}

func TestCompleteOIDCIdentityLinkCommitsIdentityAndAuditAtomically(t *testing.T) {
	now := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe301")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe302")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe303")
	identityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe304")
	auditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe305")
	claims := AccessTokenClaims{
		Subject: principalID, Audience: AudienceConsole, SessionID: sessionID,
		GrantID: grantID, GrantVersion: 2, TenantID: oidcTestTenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}
	store := &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowIdentityLink, Provider: "dex", Audience: AudienceConsole,
		TenantID: oidcTestTenantID, PrincipalID: principalID, SessionID: sessionID,
		State: "link-state", Nonce: "link-nonce", CodeVerifier: "link-verifier",
		RedirectURI:    "https://console.test.example/auth/oidc/link/callback",
		IdempotencyKey: "oidc-link-begin-2", CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}
	reader := &staticOIDCReader{reauthentication: OIDCReauthenticationState{
		PrincipalID: principalID, PrincipalStatus: PrincipalStatusActive,
		SessionID: sessionID, SessionStatus: SessionStatusActive,
		GrantID: grantID, GrantStatus: GrantStatusActive, GrantVersion: 2,
		TenantID: oidcTestTenantID, ReauthenticatedAt: now.Add(-5 * time.Minute),
	}}
	uow := &recordingOIDCUnitOfWork{}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
		Issuer: "https://dex.test.example", Subject: "dex-link-subject",
		Email: "User@Example.COM", EmailVerified: true,
	}}, store, reader, uow, &recordingOIDCAccessTokenIssuer{verifiedClaims: claims},
		&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{values: []uuid.UUID{identityID, auditID}}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	result, err := usecase.CompleteIdentityLink(context.Background(), CompleteOIDCIdentityLinkCommand{
		RawCredential: "signed-access-token", Code: "authorization-code", State: "link-state",
		RedirectURI: "https://console.test.example/auth/oidc/link/callback",
	})
	if err != nil {
		t.Fatalf("CompleteIdentityLink() error = %v", err)
	}
	if result.IdentityID != identityID || result.PrincipalID != principalID {
		t.Fatalf("CompleteIdentityLink() result = %#v", result)
	}
	if uow.link.IdentityID != identityID || uow.link.PrincipalID != principalID || uow.link.NormalizedEmail != "user@example.com" {
		t.Fatalf("link mutation = %#v", uow.link)
	}
	if uow.link.Audit.ID != auditID || uow.link.Audit.Action != AuditActionOIDCIdentityLinked || uow.link.Audit.AuthenticationMethod != AuditAuthenticationMethodPassword {
		t.Fatalf("link audit = %#v", uow.link.Audit)
	}
}

func TestCompleteOIDCIdentityLinkAuditsAuthenticatedFailures(t *testing.T) {
	now := time.Date(2026, 9, 7, 7, 10, 0, 0, time.UTC)
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe321")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe322")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe323")
	claims := AccessTokenClaims{
		Subject: principalID, Audience: AudienceConsole, SessionID: sessionID,
		GrantID: grantID, GrantVersion: 2, TenantID: oidcTestTenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}
	store := &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowIdentityLink, Provider: "dex", Audience: AudienceConsole,
		TenantID: oidcTestTenantID, PrincipalID: principalID, SessionID: sessionID,
		State: "link-failure-state", Nonce: "link-failure-nonce", CodeVerifier: "link-failure-verifier",
		RedirectURI: "https://console.test.example/auth/oidc/link/callback", IdempotencyKey: "oidc-link-failure",
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}
	reader := &staticOIDCReader{reauthentication: OIDCReauthenticationState{
		PrincipalID: principalID, PrincipalStatus: PrincipalStatusActive,
		SessionID: sessionID, SessionStatus: SessionStatusActive,
		GrantID: grantID, GrantStatus: GrantStatusActive, GrantVersion: 2,
		TenantID: oidcTestTenantID, ReauthenticatedAt: now.Add(-5 * time.Minute),
	}}
	tests := []struct {
		name           string
		provider       validatingOIDCProvider
		linkErr        error
		linkFailureErr error
		ids            []uuid.UUID
		want           error
		wantDependency bool
	}{
		{
			name: "provider rejection", provider: validatingOIDCProvider{name: "dex", err: errors.New("authorization code rejected")},
			ids: []uuid.UUID{uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe331")}, want: ErrInvalidCredential,
		},
		{
			name: "unverified email", provider: validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
				Issuer: "https://dex.test.example", Subject: "unverified-link", Email: "user@example.com",
			}}, ids: []uuid.UUID{uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe332")}, want: ErrOIDCEmailUnverified,
		},
		{
			name: "transaction conflict", provider: validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
				Issuer: "https://dex.test.example", Subject: "conflicting-link", Email: "user@example.com", EmailVerified: true,
			}}, linkErr: ErrOIDCEmailConflict, ids: []uuid.UUID{
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe333"),
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe334"),
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe335"),
			}, want: ErrOIDCEmailConflict,
		},
		{
			name: "transaction reauthentication race", provider: validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
				Issuer: "https://dex.test.example", Subject: "reauthentication-race-link", Email: "user@example.com", EmailVerified: true,
			}}, linkErr: ErrOIDCReauthenticationRequired, ids: []uuid.UUID{
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe337"),
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe338"),
				uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe339"),
			}, want: ErrOIDCReauthenticationRequired,
		},
		{
			name: "failure Audit unavailable", provider: validatingOIDCProvider{name: "dex", err: errors.New("authorization code rejected")},
			linkFailureErr: errors.New("audit unavailable"), ids: []uuid.UUID{uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe336")},
			want: ErrOIDCDependency, wantDependency: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uow := &recordingOIDCUnitOfWork{linkErr: test.linkErr, linkFailureErr: test.linkFailureErr}
			usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
				Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
				IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
				RecentReauthentication:  10 * time.Minute,
			}, test.provider, store, reader, uow, &recordingOIDCAccessTokenIssuer{verifiedClaims: claims},
				&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{values: test.ids}, fixedOIDCClock{now: now})
			if err != nil {
				t.Fatalf("NewOIDCUsecase() error = %v", err)
			}
			_, err = usecase.CompleteIdentityLink(context.Background(), CompleteOIDCIdentityLinkCommand{
				RawCredential: "signed-access-token", Code: "authorization-code", State: "link-failure-state",
				RedirectURI: "https://console.test.example/auth/oidc/link/callback",
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("CompleteIdentityLink() error = %v, want %v", err, test.want)
			}
			if !test.wantDependency && errors.Is(err, ErrOIDCDependency) {
				t.Fatalf("CompleteIdentityLink() error = %v, unexpected dependency classification", err)
			}
			if uow.linkFailure.Action != AuditActionOIDCIdentityLinkFailed || uow.linkFailure.ActorID != principalID ||
				uow.linkFailure.AuthenticationMethod != AuditAuthenticationMethodPassword ||
				uow.linkFailure.TargetID != uow.linkFailure.ID || uow.linkFailure.RequestID != "oidc-link-failure" {
				t.Fatalf("identity-link failure Audit = %#v", uow.linkFailure)
			}
		})
	}
}

func TestCompleteOIDCLoginRejectsStateAtItsExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 9, 7, 7, 30, 0, 0, time.UTC)
	store := &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
		State: "expiring-state", Nonce: "nonce", CodeVerifier: "verifier",
		RedirectURI: "https://console.test.example/auth/oidc/callback",
		CreatedAt:   now.Add(-10 * time.Minute), ExpiresAt: now,
	}}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{
		Issuer: "https://dex.test.example", Subject: "must-not-reach-provider", Email: "user@example.com", EmailVerified: true,
	}}, store, &staticOIDCReader{}, &recordingOIDCUnitOfWork{}, &recordingOIDCAccessTokenIssuer{},
		&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}
	_, err = usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{
		Code: "authorization-code", State: "expiring-state", RedirectURI: "https://console.test.example/auth/oidc/callback",
	})
	if !errors.Is(err, ErrOIDCStateInvalid) {
		t.Fatalf("CompleteLogin(expired state) error = %v, want ErrOIDCStateInvalid", err)
	}
}

func TestBeginOIDCLoginRejectsNonConfiguredRedirectWithoutPersistingState(t *testing.T) {
	store := &recordingOIDCOperationStore{}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider:                "dex",
		LoginRedirectURI:        "https://console.test.example/auth/oidc/callback",
		IdentityLinkRedirectURI: "https://console.test.example/auth/oidc/link/callback",
		RecentReauthentication:  10 * time.Minute,
	}, validatingOIDCProvider{name: "dex"}, store, &staticOIDCReader{}, &recordingOIDCUnitOfWork{}, &recordingOIDCAccessTokenIssuer{}, &sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{}, fixedOIDCClock{now: time.Now()})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	_, err = usecase.BeginLogin(context.Background(), BeginOIDCLoginCommand{
		Audience:       AudienceConsole,
		TenantID:       oidcTestTenantID,
		RedirectURI:    "https://attacker.example/callback",
		IdempotencyKey: "oidc-login-begin-redirect",
	})
	if !errors.Is(err, ErrOIDCRedirectInvalid) {
		t.Fatalf("BeginLogin() error = %v, want ErrOIDCRedirectInvalid", err)
	}
	if store.created.State != "" {
		t.Fatalf("state was persisted for invalid redirect: %#v", store.created)
	}
}

func TestOIDCMethodsRejectWhitespaceAroundConfiguredRedirect(t *testing.T) {
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	const loginRedirect = "https://console.test.example/auth/oidc/callback"
	const linkRedirect = "https://console.test.example/auth/oidc/link/callback"
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe401")
	sessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe402")
	grantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe403")
	store := &recordingOIDCOperationStore{consumed: OIDCOperation{
		Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceConsole, TenantID: oidcTestTenantID,
		State: "opaque-state", Nonce: "opaque-nonce", CodeVerifier: "opaque-verifier",
		RedirectURI: loginRedirect, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(9 * time.Minute),
	}}
	reader := &staticOIDCReader{reauthentication: OIDCReauthenticationState{
		PrincipalID: principalID, PrincipalStatus: PrincipalStatusActive, SessionID: sessionID,
		SessionStatus: SessionStatusActive, GrantID: grantID, GrantStatus: GrantStatusActive,
		GrantVersion: 1, TenantID: oidcTestTenantID, ReauthenticatedAt: now.Add(-time.Minute),
	}}
	tokens := &recordingOIDCAccessTokenIssuer{verifiedClaims: AccessTokenClaims{
		Subject: principalID, Audience: AudienceConsole, SessionID: sessionID, GrantID: grantID,
		GrantVersion: 1, TenantID: oidcTestTenantID, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}}
	usecase, err := NewOIDCUsecase(OIDCUsecaseConfig{
		Provider: "dex", LoginRedirectURI: loginRedirect, IdentityLinkRedirectURI: linkRedirect,
		RecentReauthentication: 10 * time.Minute,
	}, validatingOIDCProvider{name: "dex"}, store, reader, &recordingOIDCUnitOfWork{}, tokens,
		&sequenceOIDCSecretGenerator{}, &sequenceOIDCIDGenerator{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatalf("NewOIDCUsecase() error = %v", err)
	}

	for name, run := range map[string]func() error{
		"begin login": func() error {
			_, err := usecase.BeginLogin(context.Background(), BeginOIDCLoginCommand{Audience: AudienceConsole, TenantID: oidcTestTenantID, RedirectURI: " " + loginRedirect, IdempotencyKey: "begin-whitespace"})
			return err
		},
		"complete login": func() error {
			_, err := usecase.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{Code: "code", State: "opaque-state", RedirectURI: loginRedirect + " "})
			return err
		},
		"begin link": func() error {
			_, err := usecase.BeginIdentityLink(context.Background(), BeginOIDCIdentityLinkCommand{RawCredential: "token", Provider: "dex", RedirectURI: " " + linkRedirect, IdempotencyKey: "link-whitespace"})
			return err
		},
		"complete link": func() error {
			_, err := usecase.CompleteIdentityLink(context.Background(), CompleteOIDCIdentityLinkCommand{RawCredential: "token", Code: "code", State: "link-state", RedirectURI: linkRedirect + " "})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrOIDCRedirectInvalid) {
				t.Fatalf("error = %v, want ErrOIDCRedirectInvalid", err)
			}
		})
	}
}

type recordingOIDCOperationStore struct {
	created  OIDCOperation
	consumed OIDCOperation
}

func (s *recordingOIDCOperationStore) CreateOrGet(_ context.Context, operation OIDCOperation) (OIDCOperation, error) {
	s.created = operation
	return operation, nil
}

func (s *recordingOIDCOperationStore) Consume(_ context.Context, state string) (OIDCOperation, error) {
	if s.consumed.State != state {
		return OIDCOperation{}, ErrOIDCStateInvalid
	}
	return s.consumed, nil
}

type validatingOIDCProvider struct {
	name             string
	expectedState    string
	expectedNonce    string
	expectedVerifier string
	expectedRedirect string
	verified         OIDCVerifiedIdentity
	err              error
}

func (p validatingOIDCProvider) Name() string { return p.name }

func (p validatingOIDCProvider) AuthorizationURL(request OIDCAuthorizationRequest) (string, error) {
	if p.expectedState != "" && request.State != p.expectedState {
		return "", errors.New("unexpected state")
	}
	if p.expectedNonce != "" && request.Nonce != p.expectedNonce {
		return "", errors.New("unexpected nonce")
	}
	if p.expectedVerifier != "" && request.CodeVerifier != p.expectedVerifier {
		return "", errors.New("unexpected verifier")
	}
	if p.expectedRedirect != "" && request.RedirectURI != p.expectedRedirect {
		return "", errors.New("unexpected redirect")
	}
	return "https://dex.test.example/auth?opaque=test-only", nil
}

func (p validatingOIDCProvider) ExchangeAndVerify(context.Context, OIDCExchangeRequest) (OIDCVerifiedIdentity, error) {
	return p.verified, p.err
}

type sequenceOIDCSecretGenerator struct {
	values []string
	next   int
}

type fixedOIDCClock struct{ now time.Time }

func (c fixedOIDCClock) Now() time.Time { return c.now }

func (g *sequenceOIDCSecretGenerator) NewSecret() (string, error) {
	if g.next >= len(g.values) {
		return "", errors.New("no test secret available")
	}
	value := g.values[g.next]
	g.next++
	return value, nil
}

type sequenceOIDCIDGenerator struct {
	values []uuid.UUID
	next   int
}

func (g *sequenceOIDCIDGenerator) NewID() (uuid.UUID, error) {
	if g.next >= len(g.values) {
		return uuid.Nil, errors.New("no test ID available")
	}
	value := g.values[g.next]
	g.next++
	return value, nil
}

type staticOIDCReader struct {
	login            OIDCLoginState
	reauthentication OIDCReauthenticationState
	err              error
}

func (r *staticOIDCReader) LookupOIDCLogin(context.Context, TenantScope, string, string, string) (OIDCLoginState, error) {
	return r.login, r.err
}

func (r *staticOIDCReader) LookupOIDCReauthentication(context.Context, TenantScope, AccessTokenClaims) (OIDCReauthenticationState, error) {
	return r.reauthentication, r.err
}

type recordingOIDCUnitOfWork struct {
	login          OIDCLoginMutation
	link           OIDCIdentityLinkMutation
	failure        SecurityAuditEvent
	linkFailure    SecurityAuditEvent
	linkResult     OIDCIdentityLinkResult
	err            error
	commitErr      error
	failureErr     error
	linkErr        error
	linkFailureErr error
}

func (u *recordingOIDCUnitOfWork) RecordOIDCLoginFailure(_ context.Context, _ TenantScope, event SecurityAuditEvent) error {
	u.failure = event
	if u.failureErr != nil {
		return u.failureErr
	}
	return u.err
}

func (u *recordingOIDCUnitOfWork) CommitOIDCLogin(_ context.Context, _ TenantScope, mutation OIDCLoginMutation) error {
	u.login = mutation
	if u.commitErr != nil {
		return u.commitErr
	}
	return u.err
}

func (u *recordingOIDCUnitOfWork) RecordOIDCIdentityLinkFailure(_ context.Context, _ TenantScope, event SecurityAuditEvent) error {
	u.linkFailure = event
	return u.linkFailureErr
}

func (u *recordingOIDCUnitOfWork) LinkOIDCIdentity(_ context.Context, _ TenantScope, mutation OIDCIdentityLinkMutation) (OIDCIdentityLinkResult, error) {
	u.link = mutation
	if u.linkErr != nil {
		return OIDCIdentityLinkResult{}, u.linkErr
	}
	if u.err != nil {
		return OIDCIdentityLinkResult{}, u.err
	}
	if u.linkResult.IdentityID != uuid.Nil {
		return u.linkResult, nil
	}
	return OIDCIdentityLinkResult{IdentityID: mutation.IdentityID, PrincipalID: mutation.PrincipalID}, nil
}

type recordingOIDCAccessTokenIssuer struct {
	token          string
	claims         AccessTokenClaims
	verifiedClaims AccessTokenClaims
	err            error
}

func (i *recordingOIDCAccessTokenIssuer) Verify(context.Context, string) (AccessTokenClaims, error) {
	return i.verifiedClaims, i.err
}

func (i *recordingOIDCAccessTokenIssuer) Issue(_ context.Context, claims AccessTokenClaims) (string, error) {
	i.claims = claims
	return i.token, i.err
}
