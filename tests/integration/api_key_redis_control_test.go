//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestAPIKeyRuntimeControlsUseRealRedisAndAsynchronouslyFlushRestrictedPostgres(t *testing.T) {
	ctx := context.Background()
	redisClient := newVerticalSliceRedisClient(t, ctx)
	const namespace = "ani-iam:dp2-10:api-key-controls"

	limiter, err := data.NewRedisAPIKeyCreationLimiter(redisClient, data.RedisAPIKeyCreationLimiterConfig{
		Namespace: namespace, Limit: 2, Window: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRedisAPIKeyCreationLimiter() error = %v", err)
	}
	tenantA := uuid.MustParse("0199d080-5100-7001-9000-000000000001")
	tenantB := uuid.MustParse("0199d080-5100-7001-9000-000000000002")
	principalA := uuid.MustParse("0199d080-5100-7001-9000-000000000003")
	principalB := uuid.MustParse("0199d080-5100-7001-9000-000000000004")
	scopeA := mustTenantScope(t, tenantA)
	scopeB := mustTenantScope(t, tenantB)
	for attempt := 0; attempt < 2; attempt++ {
		if err := limiter.Acquire(ctx, scopeA, principalA); err != nil {
			t.Fatalf("Acquire() permitted attempt %d error = %v", attempt+1, err)
		}
	}
	var rateLimit *biz.AuthenticationRateLimitError
	if err := limiter.Acquire(ctx, scopeA, principalA); !errors.As(err, &rateLimit) ||
		rateLimit.LimitScope != "api_key_creation" || rateLimit.RetryAfter <= 0 || rateLimit.RetryAfter > time.Minute {
		t.Fatalf("Acquire() rate limit = %#v / %v", rateLimit, err)
	}
	if err := limiter.Acquire(ctx, scopeA, principalB); err != nil {
		t.Fatalf("Acquire() distinct principal error = %v", err)
	}
	if err := limiter.Acquire(ctx, scopeB, principalA); err != nil {
		t.Fatalf("Acquire() distinct tenant error = %v", err)
	}
	keys, err := redisClient.Keys(ctx, namespace+":api-key:create:*").Result()
	if err != nil || len(keys) != 3 {
		t.Fatalf("isolated creation-limit keys = %#v / %v", keys, err)
	}
	for _, key := range keys {
		ttl, err := redisClient.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > time.Minute {
			t.Fatalf("creation-limit key %q TTL = %s / %v", key, ttl, err)
		}
	}

	environment := newPostgresEnvironment(t)
	seededAt := time.Now().UTC().Truncate(time.Millisecond)
	actorID := uuid.MustParse("0199d080-5200-7001-9000-000000000001")
	actorMembershipID := uuid.MustParse("0199d080-5200-7001-9000-000000000002")
	roleID := uuid.MustParse("0199d080-5200-7001-9000-000000000003")
	tenantID := uuid.MustParse("0199d080-5200-7001-9000-000000000004")
	seedTenantAuthorizationBoundary(
		t, ctx, environment.runtimePool, tenantID, roleID, seededAt,
		[]uuid.UUID{actorID}, []uuid.UUID{actorMembershipID},
	)
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenant_lifecycle_projections
			(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until)
		VALUES ($1,'active',1,$2,$2,$3)
	`, tenantID, seededAt, seededAt.Add(time.Hour)); err != nil {
		t.Fatalf("seed tenant lifecycle projection: %v", err)
	}
	principalUsecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(data.NewData(environment.runtimePool)),
		data.NewUUIDv7Generator(), data.NewSystemClock(), limiter,
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "api-key-usage", CorrelationID: "api-key-usage", DecisionID: "api-key-usage",
	}
	principal, err := principalUsecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.CreateServicePrincipalCommand{
		Name: "Usage Aggregator Bot", RoleIDs: []uuid.UUID{roleID}, Actor: actor,
	})
	if err != nil {
		t.Fatalf("CreateServicePrincipal() error = %v", err)
	}
	apiKey, err := principalUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantID), biz.CreateAPIKeyCommand{
		PrincipalID: principal.Principal.ID, NeverExpires: true, IdempotencyKey: "api-key-usage", Actor: actor,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	aggregator, err := data.NewRedisAPIKeyUsageAggregator(
		redisClient, data.NewData(environment.runtimePool),
		data.RedisAPIKeyUsageConfig{Namespace: namespace, BatchSize: 32},
	)
	if err != nil {
		t.Fatalf("NewRedisAPIKeyUsageAggregator() error = %v", err)
	}
	observedEarlier := seededAt.Add(10 * time.Minute)
	observedLater := observedEarlier.Add(2 * time.Minute)
	decisionID := uuid.MustParse("0199d080-5200-7001-9000-000000000005")
	auditID := uuid.MustParse("0199d080-5200-7001-9000-000000000006")
	authentication := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)), nil, nil,
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)), nil, nil,
		&fixedIDGenerator{ids: []uuid.UUID{decisionID, auditID}}, fixedClock{now: observedEarlier}, aggregator,
	)
	validated, err := authentication.ValidatePrincipal(ctx, biz.ValidatePrincipalCommand{
		RawCredential: apiKey.Secret, OperationID: "createInstance", PolicyRevision: data.TargetPolicyRevision,
		RequestID: "api-key-usage", CorrelationID: "api-key-usage",
	})
	if err != nil || validated.Principal.ID != principal.Principal.ID || validated.DecisionID != decisionID {
		t.Fatalf("ValidatePrincipal() = %#v / %v", validated, err)
	}
	if err := aggregator.ObserveAPIKeyUse(ctx, mustTenantScope(t, tenantID), apiKey.APIKey.ID, observedLater); err != nil {
		t.Fatalf("ObserveAPIKeyUse(later) error = %v", err)
	}
	var before pgtype.Timestamptz
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2
	`, tenantID, apiKey.APIKey.ID).Scan(&before); err != nil {
		t.Fatalf("read last_used_at before flush: %v", err)
	}
	if before.Valid {
		t.Fatalf("last_used_at was updated synchronously: %s", before.Time)
	}
	if processed, err := aggregator.Flush(ctx); err != nil || processed != 1 {
		t.Fatalf("Flush() = %d / %v", processed, err)
	}
	var after pgtype.Timestamptz
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2
	`, tenantID, apiKey.APIKey.ID).Scan(&after); err != nil {
		t.Fatalf("read last_used_at after flush: %v", err)
	}
	if !after.Valid || !after.Time.Equal(observedLater) {
		t.Fatalf("last_used_at after flush = %#v, want %s", after, observedLater)
	}
	if pending, err := redisClient.ZCard(ctx, namespace+":api-key:usage").Result(); err != nil || pending != 0 {
		t.Fatalf("pending usage after flush = %d / %v", pending, err)
	}

	badPool, err := pgxpool.New(ctx, "postgres://ani_iam_runtime@127.0.0.1:1/unavailable?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("construct unavailable PostgreSQL pool: %v", err)
	}
	defer badPool.Close()
	failedAggregator, err := data.NewRedisAPIKeyUsageAggregator(
		redisClient, data.NewData(badPool),
		data.RedisAPIKeyUsageConfig{Namespace: namespace, BatchSize: 32},
	)
	if err != nil {
		t.Fatalf("construct failed aggregator: %v", err)
	}
	if err := failedAggregator.ObserveAPIKeyUse(ctx, mustTenantScope(t, tenantID), apiKey.APIKey.ID, observedLater.Add(time.Minute)); err != nil {
		t.Fatalf("queue usage for PostgreSQL failure: %v", err)
	}
	if processed, err := failedAggregator.Flush(ctx); err == nil || processed != 0 {
		t.Fatalf("Flush(unavailable PostgreSQL) = %d / %v", processed, err)
	}
	if pending, err := redisClient.ZCard(ctx, namespace+":api-key:usage").Result(); err != nil || pending != 1 {
		t.Fatalf("pending usage after PostgreSQL failure = %d / %v", pending, err)
	}

	if err := redisClient.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	if err := limiter.Acquire(ctx, scopeA, principalA); err == nil {
		t.Fatal("Acquire() succeeded with unavailable Redis")
	}
	if err := aggregator.ObserveAPIKeyUse(ctx, mustTenantScope(t, tenantID), apiKey.APIKey.ID, observedLater.Add(2*time.Minute)); err == nil {
		t.Fatal("ObserveAPIKeyUse() succeeded with unavailable Redis")
	}
}

var _ redis.UniversalClient = (*redis.Client)(nil)
