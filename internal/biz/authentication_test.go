package biz

import (
	"context"
	"errors"
	"net/netip"
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
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
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

func TestPasswordLoginRequiresSourceIPBeforeThrottle(t *testing.T) {
	throttle := &recordingLoginThrottle{}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{},
		acceptingPasswordVerifier{},
		throttle,
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
		IdempotencyKey: "login-missing-source-ip",
	})
	if !errors.Is(err, ErrSourceIPRequired) {
		t.Fatalf("PasswordLogin() error = %v, want %v", err, ErrSourceIPRequired)
	}
	if throttle.checkCalls != 0 {
		t.Fatalf("throttle Check calls = %d, want 0", throttle.checkCalls)
	}
}

func TestPasswordLoginRecordsUniformInvalidCredentialFailure(t *testing.T) {
	state := PasswordLoginState{
		PrincipalID:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		PrincipalStatus:  PrincipalStatusActive,
		MembershipID:     uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
		MembershipStatus: MembershipStatusActive,
		TenantAccess:     TenantAccessStatusActive,
		Lifecycle:        TenantLifecycleStatusActive,
		LifecycleFresh:   true,
		PasswordHash:     "test-phc-hash",
	}
	tests := []struct {
		name     string
		reader   AuthenticationReader
		verifier AuthenticationPassword
	}{
		{name: "unknown account", reader: staticPasswordLoginReader{err: ErrInvalidCredential}, verifier: acceptingPasswordVerifier{}},
		{name: "known account with wrong password", reader: staticPasswordLoginReader{state: state}, verifier: rejectingPasswordVerifier{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			throttle := &recordingLoginThrottle{}
			usecase := NewAuthenticationUsecase(
				test.reader,
				test.verifier,
				throttle,
				&recordingLoginUnitOfWork{},
				staticTokenIssuer{},
				staticSecretGenerator{},
				&fixedIDs{values: []uuid.UUID{uuid.MustParse("0198f062-b76d-7001-9000-000000000010")}},
				fixedAuthClock{},
			)

			_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
				Account:        " User@Example.COM ",
				Password:       "wrong-password",
				Audience:       AudienceConsole,
				TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
				SourceIP:       netip.MustParseAddr("::ffff:203.0.113.10"),
				IdempotencyKey: "login-invalid-credential",
			})
			if !errors.Is(err, ErrInvalidCredential) {
				t.Fatalf("PasswordLogin() error = %v, want %v", err, ErrInvalidCredential)
			}
			if throttle.checkCalls != 1 || throttle.failureCalls != 1 || throttle.resetCalls != 0 {
				t.Fatalf("throttle calls = check:%d failure:%d reset:%d, want 1/1/0", throttle.checkCalls, throttle.failureCalls, throttle.resetCalls)
			}
			want := LoginThrottleAttempt{NormalizedAccount: "user@example.com", SourceIP: netip.MustParseAddr("203.0.113.10")}
			if throttle.checked != want || throttle.failed != want {
				t.Fatalf("throttle attempts = checked:%#v failed:%#v, want %#v", throttle.checked, throttle.failed, want)
			}
		})
	}
}

func TestPasswordLoginUsesDummyVerificationForUnknownAccount(t *testing.T) {
	verifier := &recordingPasswordVerifier{}
	uow := &recordingLoginUnitOfWork{}
	auditID := uuid.MustParse("0198f062-b76d-7001-9000-000000000011")
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{err: ErrInvalidCredential},
		verifier,
		allowingLoginThrottle{},
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{auditID}},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "unknown@example.com",
		Password:       "wrong-password",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-unknown-account",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("PasswordLogin() error = %v, want %v", err, ErrInvalidCredential)
	}
	if verifier.unknownCalls != 1 || verifier.calls != 0 {
		t.Fatalf("password verifier calls = known:%d unknown:%d, want 0/1", verifier.calls, verifier.unknownCalls)
	}
	if uow.failed == nil || uow.failed.PrincipalID != uuid.Nil ||
		uow.failed.Audit.ID != auditID || uow.failed.Audit.TargetID != auditID ||
		uow.failed.Audit.Boundary != AuditBoundaryPrincipal ||
		uow.failed.Audit.AuthenticationMethod != AuditAuthenticationMethodAnonymous ||
		uow.failed.Audit.Action != AuditActionPasswordLoginFailed ||
		uow.failed.Audit.Result != AuditResultFailed {
		t.Fatalf("unknown-account failure mutation = %#v", uow.failed)
	}
}

func TestPasswordLoginRecordsKnownFailureAndAuditThroughUnitOfWork(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	auditID := uuid.MustParse("0198f062-b76d-7001-9000-000000000009")
	uow := &recordingLoginUnitOfWork{}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:       principalID,
			PrincipalStatus:   PrincipalStatusActive,
			MembershipID:      uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			MembershipStatus:  MembershipStatusActive,
			TenantAccess:      TenantAccessStatusActive,
			Lifecycle:         TenantLifecycleStatusActive,
			LifecycleFresh:    true,
			PasswordHash:      "test-phc-hash",
			CredentialVersion: 4,
		}},
		rejectingPasswordVerifier{},
		allowingLoginThrottle{},
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{auditID}},
		fixedAuthClock{now: now},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-known-failure",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("PasswordLogin() error = %v, want %v", err, ErrInvalidCredential)
	}
	if uow.failed == nil {
		t.Fatal("login failure mutation was not committed")
	}
	if uow.failed.PrincipalID != principalID || uow.failed.FailedAt != now || uow.failed.LockUntil != now.Add(15*time.Minute) {
		t.Fatalf("login failure mutation = %#v", uow.failed)
	}
	if uow.failed.Audit.ID != auditID || uow.failed.Audit.ActorID != uuid.Nil ||
		uow.failed.Audit.AuthenticationMethod != AuditAuthenticationMethodAnonymous ||
		uow.failed.Audit.Action != AuditActionPasswordLoginFailed ||
		uow.failed.Audit.Result != AuditResultFailed ||
		uow.failed.Audit.Reason != AuditReasonInvalidCredential {
		t.Fatalf("login failure Audit = %#v", uow.failed.Audit)
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
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-redis-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, dependencyErr) {
		t.Fatalf("PasswordLogin() error = %v, want dependency classification and cause", err)
	}
}

func TestPasswordLoginClassifiesRecordFailureOutageAsDependencyFailure(t *testing.T) {
	dependencyErr := errors.New("redis record failure unavailable")
	uow := &recordingLoginUnitOfWork{}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:       uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			PasswordHash:      "test-phc-hash",
			CredentialVersion: 1,
		}},
		rejectingPasswordVerifier{},
		selectiveFailingLoginThrottle{recordFailureErr: dependencyErr},
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{uuid.MustParse("0198f062-b76d-7001-9000-000000000012")}},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-redis-record-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, dependencyErr) || errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("PasswordLogin() error = %v, want dependency classification and cause without ordinary invalid-credential result", err)
	}
	if uow.failed == nil || uow.committed != nil {
		t.Fatalf("login UOW state = failure:%#v committed:%#v, want durable failure only", uow.failed, uow.committed)
	}
}

func TestPasswordLoginResetOutageAfterCommitDoesNotTurnSuccessIntoFailure(t *testing.T) {
	dependencyErr := errors.New("redis reset unavailable")
	uow := &recordingLoginUnitOfWork{}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:       uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			PrincipalStatus:   PrincipalStatusActive,
			MembershipID:      uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			MembershipStatus:  MembershipStatusActive,
			TenantAccess:      TenantAccessStatusActive,
			Lifecycle:         TenantLifecycleStatusActive,
			LifecycleFresh:    true,
			PasswordHash:      "test-phc-hash",
			CredentialVersion: 1,
		}},
		acceptingPasswordVerifier{},
		selectiveFailingLoginThrottle{resetErr: dependencyErr},
		uow,
		staticTokenIssuer{token: "signed-access-token"},
		staticSecretGenerator{secret: "opaque-refresh-secret"},
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7002-9000-000000000001"),
			uuid.MustParse("0198f062-b76d-7002-9000-000000000002"),
			uuid.MustParse("0198f062-b76d-7002-9000-000000000003"),
			uuid.MustParse("0198f062-b76d-7002-9000-000000000004"),
			uuid.MustParse("0198f062-b76d-7002-9000-000000000005"),
			uuid.MustParse("0198f062-b76d-7002-9000-000000000006"),
		}},
		fixedAuthClock{},
	)

	result, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-redis-reset-failure",
	})
	if err != nil {
		t.Fatalf("PasswordLogin() error = %v, want committed success despite conservative Redis reset failure", err)
	}
	if result.AccessToken != "signed-access-token" || uow.committed == nil || uow.failed != nil {
		t.Fatalf("login result/UOW = token:%q failure:%#v committed:%#v, want one committed success", result.AccessToken, uow.failed, uow.committed)
	}
}

func TestPasswordLoginCommitFailureDoesNotResetThrottle(t *testing.T) {
	commitErr := errors.New("postgres audit conflict")
	throttle := &recordingLoginThrottle{}
	uow := &recordingLoginUnitOfWork{commitErr: commitErr}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:       uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			PrincipalStatus:   PrincipalStatusActive,
			MembershipID:      uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			MembershipStatus:  MembershipStatusActive,
			TenantAccess:      TenantAccessStatusActive,
			Lifecycle:         TenantLifecycleStatusActive,
			LifecycleFresh:    true,
			PasswordHash:      "test-phc-hash",
			CredentialVersion: 1,
		}},
		acceptingPasswordVerifier{},
		throttle,
		uow,
		staticTokenIssuer{token: "signed-access-token"},
		staticSecretGenerator{secret: "opaque-refresh-secret"},
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7003-9000-000000000001"),
			uuid.MustParse("0198f062-b76d-7003-9000-000000000002"),
			uuid.MustParse("0198f062-b76d-7003-9000-000000000003"),
			uuid.MustParse("0198f062-b76d-7003-9000-000000000004"),
			uuid.MustParse("0198f062-b76d-7003-9000-000000000005"),
			uuid.MustParse("0198f062-b76d-7003-9000-000000000006"),
		}},
		fixedAuthClock{},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-postgres-commit-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, commitErr) {
		t.Fatalf("PasswordLogin() error = %v, want dependency classification and commit cause", err)
	}
	if throttle.resetCalls != 0 {
		t.Fatalf("throttle Reset calls = %d, want 0 before a durable login commit", throttle.resetCalls)
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
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
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

func TestPasswordLoginMasksDurableAccountLockWithDummyVerification(t *testing.T) {
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	verifier := &recordingPasswordVerifier{}
	throttle := &cooldownRecordingLoginThrottle{}
	uow := &recordingLoginUnitOfWork{}
	auditID := uuid.MustParse("0198f062-b76d-7001-9000-000000000071")
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			PrincipalStatus:  PrincipalStatusActive,
			MembershipID:     uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			MembershipStatus: MembershipStatusActive,
			TenantAccess:     TenantAccessStatusActive,
			Lifecycle:        TenantLifecycleStatusActive,
			LifecycleFresh:   true,
			PasswordHash:     "test-phc-hash",
			FailedAttempts:   5,
			LockedUntil:      now.Add(15 * time.Minute),
		}},
		verifier,
		throttle,
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{auditID}},
		fixedAuthClock{now: now},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-durable-lock",
	})
	if !errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrAuthenticationRateLimited) {
		t.Fatalf("PasswordLogin() error = %#v, want non-enumerating invalid credential", err)
	}
	if verifier.calls != 0 {
		t.Fatalf("password verifier calls = %d, want 0", verifier.calls)
	}
	if verifier.unknownCalls != 1 {
		t.Fatalf("dummy password verifier calls = %d, want 1", verifier.unknownCalls)
	}
	if throttle.failureCalls != 1 || throttle.failed.NormalizedAccount != "user@example.com" ||
		throttle.failed.SourceIP != netip.MustParseAddr("203.0.113.10") {
		t.Fatalf("durable-lock throttle failure = calls:%d attempt:%#v", throttle.failureCalls, throttle.failed)
	}
	if uow.failed == nil || uow.failed.PrincipalID != uuid.Nil ||
		uow.failed.Audit.ID != auditID || uow.failed.Audit.ActorID != uuid.Nil ||
		uow.failed.Audit.Boundary != AuditBoundaryPrincipal ||
		uow.failed.Audit.Action != AuditActionPasswordLoginFailed ||
		uow.failed.Audit.TargetID != auditID || uow.failed.Audit.RequestID != "login-durable-lock" {
		t.Fatalf("durable-lock redacted Audit mutation = %#v", uow.failed)
	}

	_, err = usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-durable-lock-retry",
	})
	if !errors.Is(err, ErrAuthenticationRateLimited) {
		t.Fatalf("second PasswordLogin() error = %#v, want rate limit matching an unknown-account retry", err)
	}
}

func TestPasswordLoginDurableLockAuditFailureStopsBeforeRedisMutation(t *testing.T) {
	now := time.Date(2026, 9, 6, 17, 0, 0, 0, time.UTC)
	throttle := &recordingLoginThrottle{}
	uow := &recordingLoginUnitOfWork{failureErr: ErrAuditConflict}
	usecase := NewAuthenticationUsecase(
		staticPasswordLoginReader{state: PasswordLoginState{
			PrincipalID:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
			PrincipalStatus:  PrincipalStatusActive,
			MembershipID:     uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			MembershipStatus: MembershipStatusActive,
			TenantAccess:     TenantAccessStatusActive,
			Lifecycle:        TenantLifecycleStatusActive,
			LifecycleFresh:   true,
			PasswordHash:     "test-phc-hash",
			FailedAttempts:   5,
			LockedUntil:      now.Add(15 * time.Minute),
		}},
		acceptingPasswordVerifier{},
		throttle,
		uow,
		staticTokenIssuer{},
		staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{uuid.MustParse("0198f062-b76d-7001-9000-000000000072")}},
		fixedAuthClock{now: now},
	)

	_, err := usecase.PasswordLogin(context.Background(), PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "unit-input",
		Audience:       AudienceConsole,
		TenantID:       uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"),
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		IdempotencyKey: "login-durable-lock-audit-failure",
	})
	if !errors.Is(err, ErrAuthenticationDependency) || !errors.Is(err, ErrAuditConflict) {
		t.Fatalf("PasswordLogin() error = %v, want dependency plus Audit failure", err)
	}
	if uow.failed == nil {
		t.Fatal("durable-lock Audit mutation was not attempted")
	}
	if throttle.failureCalls != 0 {
		t.Fatalf("Redis failure calls after failed Audit = %d, want 0", throttle.failureCalls)
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
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
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
func (staticPasswordLoginReader) LookupPasswordActionTarget(context.Context, string, Audience) (PasswordActionTarget, bool, error) {
	return PasswordActionTarget{}, false, nil
}

type acceptingPasswordVerifier struct{}

func (acceptingPasswordVerifier) Verify(string, string) (bool, error) { return true, nil }
func (acceptingPasswordVerifier) VerifyUnknown(string) error          { return nil }
func (acceptingPasswordVerifier) Hash(string) (string, error)         { return "test-password-hash", nil }

type rejectingPasswordVerifier struct{}

func (rejectingPasswordVerifier) Verify(string, string) (bool, error) { return false, nil }
func (rejectingPasswordVerifier) VerifyUnknown(string) error          { return nil }
func (rejectingPasswordVerifier) Hash(string) (string, error)         { return "test-password-hash", nil }

type recordingPasswordVerifier struct {
	calls        int
	unknownCalls int
}

func (v *recordingPasswordVerifier) Verify(string, string) (bool, error) {
	v.calls++
	return true, nil
}

func (v *recordingPasswordVerifier) VerifyUnknown(string) error {
	v.unknownCalls++
	return nil
}
func (*recordingPasswordVerifier) Hash(string) (string, error) { return "test-password-hash", nil }

type allowingLoginThrottle struct{}

func (allowingLoginThrottle) Check(context.Context, LoginThrottleAttempt) error         { return nil }
func (allowingLoginThrottle) RecordFailure(context.Context, LoginThrottleAttempt) error { return nil }
func (allowingLoginThrottle) Reset(context.Context, LoginThrottleAttempt) error         { return nil }

type recordingLoginThrottle struct {
	checkCalls   int
	failureCalls int
	resetCalls   int
	checked      LoginThrottleAttempt
	failed       LoginThrottleAttempt
}

type cooldownRecordingLoginThrottle struct {
	failureCalls int
	failed       LoginThrottleAttempt
}

func (t *cooldownRecordingLoginThrottle) Check(context.Context, LoginThrottleAttempt) error {
	if t.failureCalls == 0 {
		return nil
	}
	return &AuthenticationRateLimitError{LimitScope: "password_account", RetryAfter: time.Second}
}

func (t *cooldownRecordingLoginThrottle) RecordFailure(_ context.Context, attempt LoginThrottleAttempt) error {
	t.failureCalls++
	t.failed = attempt
	return nil
}

func (*cooldownRecordingLoginThrottle) Reset(context.Context, LoginThrottleAttempt) error { return nil }

func (t *recordingLoginThrottle) Check(_ context.Context, attempt LoginThrottleAttempt) error {
	t.checkCalls++
	t.checked = attempt
	return nil
}

func (t *recordingLoginThrottle) RecordFailure(_ context.Context, attempt LoginThrottleAttempt) error {
	t.failureCalls++
	t.failed = attempt
	return nil
}

func (t *recordingLoginThrottle) Reset(context.Context, LoginThrottleAttempt) error {
	t.resetCalls++
	return nil
}

type failingLoginThrottle struct{ err error }

func (t failingLoginThrottle) Check(context.Context, LoginThrottleAttempt) error { return t.err }
func (t failingLoginThrottle) RecordFailure(context.Context, LoginThrottleAttempt) error {
	return t.err
}
func (t failingLoginThrottle) Reset(context.Context, LoginThrottleAttempt) error { return t.err }

type selectiveFailingLoginThrottle struct {
	recordFailureErr error
	resetErr         error
}

func (selectiveFailingLoginThrottle) Check(context.Context, LoginThrottleAttempt) error { return nil }
func (t selectiveFailingLoginThrottle) RecordFailure(context.Context, LoginThrottleAttempt) error {
	return t.recordFailureErr
}
func (t selectiveFailingLoginThrottle) Reset(context.Context, LoginThrottleAttempt) error {
	return t.resetErr
}

type recordingLoginUnitOfWork struct {
	committed  *LoginMutation
	failed     *LoginFailureMutation
	commitErr  error
	failureErr error
}

func (u *recordingLoginUnitOfWork) CommitLogin(_ context.Context, _ TenantScope, mutation LoginMutation) error {
	u.committed = &mutation
	return u.commitErr
}

func (u *recordingLoginUnitOfWork) RecordLoginFailure(_ context.Context, _ TenantScope, mutation LoginFailureMutation) error {
	u.failed = &mutation
	return u.failureErr
}
func (*recordingLoginUnitOfWork) RequestPasswordAction(_ context.Context, mutation PasswordActionRequestMutation) (RequestPasswordActionResult, error) {
	return RequestPasswordActionResult{OperationID: mutation.OperationID, ExpiresAt: mutation.ExpiresAt}, nil
}
func (*recordingLoginUnitOfWork) CompletePasswordAction(context.Context, PasswordActionCompletion) (CompletePasswordActionResult, error) {
	return CompletePasswordActionResult{}, nil
}

type staticTokenIssuer struct{ token string }

func (i staticTokenIssuer) Issue(context.Context, AccessTokenClaims) (string, error) {
	return i.token, nil
}
func (staticTokenIssuer) IssuePasswordAction(context.Context, PasswordActionTokenClaims) (string, error) {
	return "test-password-action-token", nil
}
func (staticTokenIssuer) VerifyPasswordAction(context.Context, string) (PasswordActionTokenClaims, error) {
	return PasswordActionTokenClaims{}, ErrPasswordActionInvalid
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
