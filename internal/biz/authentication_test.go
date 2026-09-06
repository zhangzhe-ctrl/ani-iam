package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPasswordLoginCreatesActiveTenantSession(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	membershipID := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01")
	now := time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)
	ids := &fixedIDs{values: []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7001-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000005"),
		uuid.MustParse("0198f062-b76d-7001-9000-000000000006"),
	}}
	state := PasswordLoginState{
		PrincipalID:      principalID,
		PrincipalStatus:  PrincipalStatusActive,
		MembershipID:     membershipID,
		MembershipStatus: MembershipStatusActive,
		TenantAccess:     TenantAccessStatusActive,
		Lifecycle:        TenantLifecycleStatusActive,
		LifecycleFresh:   true,
		PasswordHash:     "test-phc-hash",
	}
	uow := &recordingLoginUnitOfWork{}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: state},
		acceptingPasswordVerifier{},
		allowingLoginThrottle{},
		uow,
		staticTokenIssuer{token: "signed-access-token"},
		staticSecretGenerator{secret: "opaque-refresh-secret"},
		ids,
		fixedAuthClock{now: now},
	)

	result, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        " User@Example.COM ",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       tenantID,
		DeviceName:     "browser",
		IdempotencyKey: "login-1",
	})
	if err != nil {
		t.Fatalf("PasswordLogin() error = %v", err)
	}
	if result.AccessToken != "signed-access-token" || result.RefreshToken != "opaque-refresh-secret" {
		t.Fatalf("tokens = access:%q refresh:%q", result.AccessToken, result.RefreshToken)
	}
	if result.Principal.ID != principalID || result.Principal.Status != PrincipalStatusActive {
		t.Fatalf("principal = %#v", result.Principal)
	}
	if result.Session.Status != SessionStatusActive || result.Grant.Status != GrantStatusActive || result.Grant.Version != 1 {
		t.Fatalf("session/grant = %#v / %#v", result.Session, result.Grant)
	}
	if result.Session.ID == uuid.Nil || result.Grant.ID == uuid.Nil || result.AccessTokenExpiresAt != now.Add(15*time.Minute) {
		t.Fatalf("result = %#v", result)
	}
	if uow.committed == nil || uow.committed.Session.ID != result.Session.ID || uow.committed.Audit.Action != AuditActionPasswordLoginSucceeded {
		t.Fatalf("committed mutation = %#v", uow.committed)
	}
	if got := uow.committed.NormalizedAccount; got != "user@example.com" {
		t.Fatalf("normalized account = %q", got)
	}
}

func TestPasswordLoginFailsClosedWhenThrottleIsUnavailable(t *testing.T) {
	dependencyErr := errors.New("redis unavailable")
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{},
		acceptingPasswordVerifier{},
		failingLoginThrottle{err: dependencyErr},
		&recordingLoginUnitOfWork{},
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IdempotencyKey: "login-redis-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, dependencyErr) {
		t.Fatalf("PasswordLogin() error = %v, want dependency classification and cause", err)
	}
}

func TestPasswordLoginPreservesRateLimitClassification(t *testing.T) {
	rateLimit := &AuthenticationRateLimitError{
		LimitScope: "password_ip",
		RetryAfter: 15 * time.Minute,
	}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{},
		acceptingPasswordVerifier{},
		failingLoginThrottle{err: rateLimit},
		&recordingLoginUnitOfWork{},
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IdempotencyKey: "login-rate-limit",
	})
	if !errors.Is(err, ErrAuthenticationRateLimited) || errors.Is(err, ErrAuthenticationDependency) {
		t.Fatalf("PasswordLogin() error = %v, want rate limit without dependency classification", err)
	}
	var got *AuthenticationRateLimitError
	if !errors.As(err, &got) || got.LimitScope != "password_ip" || got.RetryAfter != 15*time.Minute {
		t.Fatalf("PasswordLogin() rate-limit metadata = %#v, want %#v", got, rateLimit)
	}
}

func TestPasswordLoginClassifiesPostgresFailureAsDependencyUnavailable(t *testing.T) {
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{err: ErrPersistenceUnavailable},
		acceptingPasswordVerifier{},
		allowingLoginThrottle{},
		&recordingLoginUnitOfWork{},
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		IdempotencyKey: "login-postgres-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("PasswordLogin() error = %v, want dependency classification and cause", err)
	}
}

type staticPasswordLoginReader struct {
	state PasswordLoginState
	err   error
}

func (r staticPasswordLoginReader) LookupPasswordLogin(context.Context, TenantScope, string) (PasswordLoginState, error) {
	return r.state, r.err
}

type acceptingPasswordVerifier struct{}

func (acceptingPasswordVerifier) Verify(string, string) (bool, error) { return true, nil }

type allowingLoginThrottle struct{}

func (allowingLoginThrottle) Allow(context.Context, string) error { return nil }
func (allowingLoginThrottle) Reset(context.Context, string) error { return nil }

type failingLoginThrottle struct{ err error }

func (t failingLoginThrottle) Allow(context.Context, string) error { return t.err }
func (t failingLoginThrottle) Reset(context.Context, string) error { return t.err }

type recordingLoginUnitOfWork struct{ committed *LoginMutation }

func (u *recordingLoginUnitOfWork) CommitLogin(_ context.Context, _ TenantScope, mutation LoginMutation) error {
	u.committed = &mutation
	return nil
}

type staticTokenIssuer struct{ token string }

func (i staticTokenIssuer) Issue(context.Context, AccessTokenClaims) (string, error) {
	return i.token, nil
}

type staticSecretGenerator struct{ secret string }

func (g staticSecretGenerator) NewSecret() (string, error) { return g.secret, nil }

type fixedAuthClock struct{ now time.Time }

func (c fixedAuthClock) Now() time.Time { return c.now }

type fixedIDs struct{ values []uuid.UUID }

func (g *fixedIDs) NewID() (uuid.UUID, error) {
	value := g.values[0]
	g.values = g.values[1:]
	return value, nil
}
