//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestPostgresConcurrentPasswordFailuresProduceOneDurableLock(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	auditIDs := []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7601-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000005"),
	}
	start := make(chan struct{})
	errorsByAttempt := make(chan error, len(auditIDs))
	var waitGroup sync.WaitGroup
	for index, auditID := range auditIDs {
		waitGroup.Add(1)
		go func(index int, auditID uuid.UUID) {
			defer waitGroup.Done()
			usecase := biz.NewAuthenticationUsecase(
				data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
				passwordHasher,
				allowingIntegrationLoginThrottle{},
				data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
				&recordingAccessTokenIssuer{},
				staticIntegrationSecretGenerator{},
				&fixedIDGenerator{ids: []uuid.UUID{auditID}},
				fixedClock{now: now},
			)
			<-start
			_, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
				Account:        "user@example.com",
				Password:       "wrong-password",
				Audience:       biz.AudienceConsole,
				TenantID:       tenantA,
				SourceIP:       netip.MustParseAddr("203.0.113.22"),
				IdempotencyKey: fmt.Sprintf("concurrent-password-failure-%d", index),
			})
			errorsByAttempt <- err
		}(index, auditID)
	}
	close(start)
	waitGroup.Wait()
	close(errorsByAttempt)
	for err := range errorsByAttempt {
		if !errors.Is(err, biz.ErrInvalidCredential) {
			t.Fatalf("concurrent PasswordLogin() error = %v, want %v", err, biz.ErrInvalidCredential)
		}
	}

	var failedAttempts, auditCount int
	var lockedUntil time.Time
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&failedAttempts, &lockedUntil, &credentialVersion); err != nil {
		t.Fatalf("query concurrent failure state: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.login.failed'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("query concurrent failure Audits: %v", err)
	}
	if failedAttempts != 5 || credentialVersion != 6 || !lockedUntil.Equal(now.Add(15*time.Minute)) || auditCount != 5 {
		t.Fatalf("concurrent failure result = attempts:%d version:%d lock:%s audits:%d", failedAttempts, credentialVersion, lockedUntil, auditCount)
	}
}

func TestPostgresPasswordFailureAuditConflictRollsBackDurableState(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 10, 45, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	duplicateAuditID := uuid.MustParse("0198f062-b76d-7701-9000-000000000001")
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `
		INSERT INTO iam_audit_events (
			tenant_id, event_id, actor_id, authentication_method, boundary,
			action, target_type, target_id, target_version, result, reason,
			request_id, correlation_id, decision_id, source_service,
			occurred_at, recorded_at
		) VALUES (
			$1, $2, $3, 'password', 'tenant', 'test.seed.audit',
			'password_credential', $3, 1, 'succeeded', 'TEST_SEED',
			'test-seed', 'test-seed', 'test-seed', 'iam-service', $4, $4
		)
	`, tenantA, duplicateAuditID, actorID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed conflicting Audit identity: %v", err)
	}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{},
		staticIntegrationSecretGenerator{},
		&fixedIDGenerator{ids: []uuid.UUID{duplicateAuditID}},
		fixedClock{now: now},
	)
	_, err = usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.23"),
		IdempotencyKey: "password-failure-audit-conflict",
	})
	if !errors.Is(err, biz.ErrAuthenticationDependency) || !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("PasswordLogin() error = %v, want dependency plus Audit conflict", err)
	}
	var failedAttempts int
	var lockCleared bool
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until IS NULL, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&failedAttempts, &lockCleared, &credentialVersion); err != nil {
		t.Fatalf("query rolled-back password failure state: %v", err)
	}
	if failedAttempts != 0 || !lockCleared || credentialVersion != 1 {
		t.Fatalf("rolled-back password failure state = attempts:%d lock-cleared:%t version:%d", failedAttempts, lockCleared, credentialVersion)
	}
	var failedAuditCount int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.login.failed'
	`).Scan(&failedAuditCount); err != nil {
		t.Fatalf("query rolled-back password failure Audit: %v", err)
	}
	if failedAuditCount != 0 {
		t.Fatalf("rolled-back password failure Audit count = %d, want 0", failedAuditCount)
	}
}

func TestUnknownPasswordAccountUsesDummyArgonAndPersistsRedactedAudit(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 9, 30, 0, 0, time.UTC)
	auditID := uuid.MustParse("0198f062-b76d-7301-9000-000000000011")
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		data.NewArgon2idPasswordHasher(),
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{},
		staticIntegrationSecretGenerator{},
		&fixedIDGenerator{ids: []uuid.UUID{auditID}},
		fixedClock{now: now},
	)
	_, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        "unknown@example.com",
		Password:       "do-not-persist-this-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.21"),
		IdempotencyKey: "unknown-password-login",
	})
	if !errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("PasswordLogin(unknown account) error = %v, want %v", err, biz.ErrInvalidCredential)
	}
	var tenantMissing, actorMissing bool
	var serializedAudit string
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT tenant_id IS NULL, actor_id IS NULL, to_jsonb(iam_audit_events)::text
		FROM iam_audit_events
		WHERE event_id = $1
	`, auditID).Scan(&tenantMissing, &actorMissing, &serializedAudit); err != nil {
		t.Fatalf("query unknown-account Audit: %v", err)
	}
	if !tenantMissing || !actorMissing || strings.Contains(serializedAudit, "unknown@example.com") || strings.Contains(serializedAudit, "do-not-persist-this-password") {
		t.Fatalf("unknown-account Audit boundary/redaction = tenant-missing:%t actor-missing:%t row:%s", tenantMissing, actorMissing, serializedAudit)
	}
}

func TestPostgresPasswordFailuresLockAfterFiveAttemptsWithAudit(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	ids := &fixedIDGenerator{ids: []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7401-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7401-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7401-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7401-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7401-9000-000000000005"),
	}}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{},
		staticIntegrationSecretGenerator{},
		ids,
		fixedClock{now: now},
	)
	for failure := 1; failure <= 5; failure++ {
		_, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
			Account:        "user@example.com",
			Password:       "wrong-password",
			Audience:       biz.AudienceConsole,
			TenantID:       tenantA,
			SourceIP:       netip.MustParseAddr("203.0.113.20"),
			IdempotencyKey: fmt.Sprintf("postgres-password-failure-%d", failure),
		})
		if !errors.Is(err, biz.ErrInvalidCredential) {
			t.Fatalf("PasswordLogin() failure %d error = %v, want %v", failure, err, biz.ErrInvalidCredential)
		}
	}

	var failedAttempts int
	var lockedUntil time.Time
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&failedAttempts, &lockedUntil, &credentialVersion); err != nil {
		t.Fatalf("query durable password failure state: %v", err)
	}
	if failedAttempts != 5 || !lockedUntil.Equal(now.Add(15*time.Minute)) || credentialVersion != 6 {
		t.Fatalf("durable password failure state = attempts:%d lock:%s version:%d", failedAttempts, lockedUntil, credentialVersion)
	}
	var auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*)
		FROM iam_audit_events
		WHERE action = 'iam.password.login.failed'
		  AND result = 'failed'
		  AND reason = 'INVALID_CREDENTIAL'
		  AND authentication_method = 'anonymous'
		  AND actor_id IS NULL
	`).Scan(&auditCount); err != nil {
		t.Fatalf("query password failure Audits: %v", err)
	}
	if auditCount != 5 {
		t.Fatalf("password failure Audit count = %d, want 5", auditCount)
	}
}

func TestPostgresPasswordLoginSuccessResetsDurableFailuresAtomically(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `
		UPDATE password_credentials
		SET failed_attempts = 4, locked_until = NULL, version = 5
		WHERE principal_id = $1
	`, actorID); err != nil {
		t.Fatalf("seed durable password failures: %v", err)
	}
	loginIDs := &fixedIDGenerator{ids: []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7501-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7501-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7501-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7501-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7501-9000-000000000005"),
		uuid.MustParse("0198f062-b76d-7501-9000-000000000006"),
	}}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{token: "test-access-token"},
		staticIntegrationSecretGenerator{secret: "test-refresh-secret"},
		loginIDs,
		fixedClock{now: now},
	)
	if _, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.20"),
		IdempotencyKey: "postgres-password-success-reset",
	}); err != nil {
		t.Fatalf("PasswordLogin() error = %v", err)
	}

	var failedAttempts int
	var lockCleared bool
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until IS NULL, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&failedAttempts, &lockCleared, &credentialVersion); err != nil {
		t.Fatalf("query successful-login password state: %v", err)
	}
	if failedAttempts != 0 || !lockCleared || credentialVersion != 6 {
		t.Fatalf("successful-login password state = attempts:%d lock-cleared:%t version:%d, want 0/true/6", failedAttempts, lockCleared, credentialVersion)
	}
	var sessionCount, auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id = $1`, actorID).Scan(&sessionCount); err != nil {
		t.Fatalf("query successful-login Session: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.login.succeeded'`).Scan(&auditCount); err != nil {
		t.Fatalf("query successful-login Audit: %v", err)
	}
	if sessionCount != 1 || auditCount != 1 {
		t.Fatalf("successful-login Session/Audit count = %d/%d, want 1/1", sessionCount, auditCount)
	}
}

func TestPostgresPasswordLoginAuditConflictRollsBackFailureResetAndSession(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 11, 15, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	duplicateAuditID := uuid.MustParse("0198f062-b76d-7601-9000-000000000006")
	if _, err := seedPool.Exec(ctx, `
		UPDATE password_credentials
		SET failed_attempts = 4, locked_until = NULL, version = 5
		WHERE principal_id = $1
	`, actorID); err != nil {
		t.Fatalf("seed durable password failures: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
		INSERT INTO iam_audit_events (
			tenant_id, event_id, actor_id, authentication_method, boundary,
			action, target_type, target_id, target_version, result, reason,
			request_id, correlation_id, decision_id, source_service,
			occurred_at, recorded_at
		) VALUES (
			$1, $2, $3, 'password', 'tenant', 'test.seed.audit',
			'password_credential', $3, 1, 'succeeded', 'TEST_SEED',
			'test-seed', 'test-seed', 'test-seed', 'iam-service', $4, $4
		)
	`, tenantA, duplicateAuditID, actorID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed conflicting Audit identity: %v", err)
	}
	loginIDs := &fixedIDGenerator{ids: []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7601-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7601-9000-000000000005"),
		duplicateAuditID,
	}}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{token: "test-access-token"},
		staticIntegrationSecretGenerator{secret: "test-refresh-secret"},
		loginIDs,
		fixedClock{now: now},
	)
	_, err = usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.20"),
		IdempotencyKey: "postgres-password-success-audit-conflict",
	})
	if !errors.Is(err, biz.ErrAuthenticationDependency) || !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("PasswordLogin() error = %v, want dependency plus Audit conflict", err)
	}

	var failedAttempts int
	var lockCleared bool
	var credentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until IS NULL, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&failedAttempts, &lockCleared, &credentialVersion); err != nil {
		t.Fatalf("query rolled-back successful-login password state: %v", err)
	}
	if failedAttempts != 4 || !lockCleared || credentialVersion != 5 {
		t.Fatalf("rolled-back successful-login password state = attempts:%d lock-cleared:%t version:%d, want 4/true/5", failedAttempts, lockCleared, credentialVersion)
	}
	var sessionCount, successAuditCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id = $1`, actorID).Scan(&sessionCount); err != nil {
		t.Fatalf("query rolled-back successful-login Session: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action = 'iam.password.login.succeeded'`).Scan(&successAuditCount); err != nil {
		t.Fatalf("query rolled-back successful-login Audit: %v", err)
	}
	if sessionCount != 0 || successAuditCount != 0 {
		t.Fatalf("rolled-back successful-login Session/Audit count = %d/%d, want 0/0", sessionCount, successAuditCount)
	}
}

func TestPasswordLoginPostgresFailurePreservesRealRedisThrottleState(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 2, 0, 0, 0, time.UTC)
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)

	duplicateAuditID := uuid.MustParse("0198f062-b76d-7602-9000-000000000006")
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `
		INSERT INTO iam_audit_events (
			tenant_id, event_id, actor_id, authentication_method, boundary,
			action, target_type, target_id, target_version, result, reason,
			request_id, correlation_id, decision_id, source_service,
			occurred_at, recorded_at
		) VALUES (
			$1, $2, $3, 'password', 'tenant', 'test.seed.audit',
			'password_credential', $3, 1, 'succeeded', 'TEST_SEED',
			'test-seed', 'test-seed', 'test-seed', 'iam-service', $4, $4
		)
	`, tenantA, duplicateAuditID, actorID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed conflicting Audit identity: %v", err)
	}

	redisContainer, err := testcontainers.Run(
		ctx,
		redisImage,
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		t.Fatalf("start pinned Redis container: %v", err)
	}
	t.Cleanup(func() {
		terminateContext, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := testcontainers.TerminateContainer(redisContainer, testcontainers.StopContext(terminateContext)); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})
	redisEndpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("Redis endpoint: %v", err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisEndpoint, MaxRetries: -1})
	t.Cleanup(func() { _ = redisClient.Close() })
	const namespace = "ani-iam:dp2-06:postgres-failure"
	throttle, err := data.NewRedisLoginThrottle(redisClient, data.RedisLoginThrottleConfig{
		Namespace: namespace,
		Limit:     5,
		Window:    15 * time.Minute,
		BaseDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}
	attempt := biz.LoginThrottleAttempt{
		NormalizedAccount: "user@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.24"),
	}
	for failure := 1; failure <= 4; failure++ {
		if err := throttle.RecordFailure(ctx, attempt); err != nil {
			t.Fatalf("seed Redis failure %d: %v", failure, err)
		}
	}
	time.Sleep(100 * time.Millisecond)

	loginIDs := &fixedIDGenerator{ids: []uuid.UUID{
		uuid.MustParse("0198f062-b76d-7602-9000-000000000001"),
		uuid.MustParse("0198f062-b76d-7602-9000-000000000002"),
		uuid.MustParse("0198f062-b76d-7602-9000-000000000003"),
		uuid.MustParse("0198f062-b76d-7602-9000-000000000004"),
		uuid.MustParse("0198f062-b76d-7602-9000-000000000005"),
		duplicateAuditID,
	}}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		throttle,
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{token: "test-access-token"},
		staticIntegrationSecretGenerator{secret: "test-refresh-secret"},
		loginIDs,
		fixedClock{now: now},
	)
	_, err = usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        attempt.NormalizedAccount,
		Password:       "correct-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       attempt.SourceIP,
		IdempotencyKey: "postgres-failure-preserves-redis-throttle",
	})
	if !errors.Is(err, biz.ErrAuthenticationDependency) || !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("PasswordLogin() error = %v, want dependency plus Audit conflict", err)
	}

	accountKeys, err := redisClient.Keys(ctx, namespace+":login:account:*").Result()
	if err != nil || len(accountKeys) != 1 {
		t.Fatalf("account throttle keys = %#v, error = %v, want one preserved key", accountKeys, err)
	}
	accountFailures, err := redisClient.HGet(ctx, accountKeys[0], "count").Int64()
	if err != nil {
		t.Fatalf("read preserved account throttle: %v", err)
	}
	if accountFailures != 4 {
		t.Fatalf("account throttle failures = %d, want 4 after PostgreSQL rollback", accountFailures)
	}
}

func TestPasswordLoginMasksDurablePostgresLockAsInvalidCredential(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	passwordHash, err := data.NewArgon2idPasswordHasher().Hash("correct-password")
	if err != nil {
		t.Fatalf("hash password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	if _, err := seedPool.Exec(ctx, `
		UPDATE password_credentials
		SET failed_attempts = 5, locked_until = $1, version = version + 1
		WHERE principal_id = $2
	`, now.Add(15*time.Minute), actorID); err != nil {
		t.Fatalf("seed durable password lock: %v", err)
	}
	var beforeFailedAttempts int
	var beforeLockedUntil time.Time
	var beforeCredentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&beforeFailedAttempts, &beforeLockedUntil, &beforeCredentialVersion); err != nil {
		t.Fatalf("query seeded durable password lock: %v", err)
	}

	redisClient := newVerticalSliceRedisClient(t, ctx)
	throttle, err := data.NewRedisLoginThrottle(redisClient, data.RedisLoginThrottleConfig{
		Namespace: "ani-iam:dp2-06:durable-lock-mask",
		Limit:     5,
		Window:    15 * time.Minute,
		BaseDelay: time.Second,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}
	auditID := uuid.MustParse("0198f062-b76d-7a01-9000-000000000001")
	verifier := &recordingIntegrationPasswordVerifier{}
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		verifier,
		throttle,
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{},
		staticIntegrationSecretGenerator{},
		&fixedIDGenerator{ids: []uuid.UUID{auditID}},
		fixedClock{now: now},
	)
	command := biz.PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.20"),
		IdempotencyKey: "durable-password-lock",
	}
	_, err = usecase.PasswordLogin(ctx, command)
	if !errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("PasswordLogin() error = %v, want non-enumerating invalid credential", err)
	}
	if errors.Is(err, biz.ErrAuthenticationRateLimited) {
		t.Fatalf("PasswordLogin() exposed known-account durable lock: %v", err)
	}
	if verifier.calls != 0 {
		t.Fatalf("password verifier calls = %d, want 0", verifier.calls)
	}
	if verifier.unknownCalls != 1 {
		t.Fatalf("dummy password verifier calls = %d, want 1", verifier.unknownCalls)
	}
	var afterFailedAttempts int
	var afterLockedUntil time.Time
	var afterCredentialVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT failed_attempts, locked_until, version
		FROM password_credentials
		WHERE principal_id = $1
	`, actorID).Scan(&afterFailedAttempts, &afterLockedUntil, &afterCredentialVersion); err != nil {
		t.Fatalf("query durable password lock after rejected login: %v", err)
	}
	if afterFailedAttempts != beforeFailedAttempts || !afterLockedUntil.Equal(beforeLockedUntil) ||
		afterCredentialVersion != beforeCredentialVersion {
		t.Fatalf(
			"durable lock changed after rejected login = attempts:%d lock:%s version:%d, want %d/%s/%d",
			afterFailedAttempts, afterLockedUntil, afterCredentialVersion,
			beforeFailedAttempts, beforeLockedUntil, beforeCredentialVersion,
		)
	}
	var auditCount int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*)
		FROM iam_audit_events
		WHERE event_id = $1
		  AND tenant_id IS NULL
		  AND actor_id IS NULL
		  AND boundary = 'principal'
		  AND action = 'iam.password.login.failed'
		  AND target_id = $1
		  AND result = 'failed'
		  AND reason = 'INVALID_CREDENTIAL'
		  AND request_id = 'durable-password-lock'
	`, auditID).Scan(&auditCount); err != nil {
		t.Fatalf("query durable-lock redacted Audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("durable-lock redacted Audit count = %d, want 1", auditCount)
	}

	command.IdempotencyKey = "durable-password-lock-retry"
	_, err = usecase.PasswordLogin(ctx, command)
	assertRedisLoginRateLimit(t, err, "password_account", time.Second+100*time.Millisecond)

	command.Account = "unknown@example.com"
	command.IdempotencyKey = "durable-password-lock-shared-ip"
	_, err = usecase.PasswordLogin(ctx, command)
	assertRedisLoginRateLimit(t, err, "password_ip", time.Second+100*time.Millisecond)
	if verifier.unknownCalls != 1 {
		t.Fatalf("dummy verifier calls after Redis cooldown checks = %d, want 1", verifier.unknownCalls)
	}
}

type recordingIntegrationPasswordVerifier struct {
	calls        int
	unknownCalls int
}

func (v *recordingIntegrationPasswordVerifier) Verify(string, string) (bool, error) {
	v.calls++
	return true, nil
}

func (v *recordingIntegrationPasswordVerifier) VerifyUnknown(string) error {
	v.unknownCalls++
	return nil
}
func (*recordingIntegrationPasswordVerifier) Hash(string) (string, error) {
	return "test-password-hash", nil
}
