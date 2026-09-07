//go:build integration

package integration_test

import (
	"context"
	"errors"
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

func TestRedisOIDCOperationStoreIsIdempotentSingleUseAndDigestKeyed(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.Run(
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
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(terminateContext)); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("Redis endpoint: %v", err)
	}
	client := redis.NewClient(&redis.Options{
		Addr: endpoint, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping pinned Redis container: %v", err)
	}
	store, err := data.NewRedisOIDCOperationStore(client, "ani-iam:dp2-07:test-run")
	if err != nil {
		t.Fatalf("NewRedisOIDCOperationStore() error = %v", err)
	}
	now := time.Now().UTC()
	operation := biz.OIDCOperation{
		Kind: biz.OIDCFlowLogin, Provider: "dex", Audience: biz.AudienceConsole,
		TenantID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe401"),
		State:    "raw-state-must-not-be-a-key", Nonce: "raw-nonce", CodeVerifier: "raw-verifier",
		RedirectURI:    "https://console.test.example/auth/oidc/callback",
		IdempotencyKey: "raw-idempotency-must-not-be-a-key", RequestFingerprint: strings.Repeat("a", 64),
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
	created, err := store.CreateOrGet(ctx, operation)
	if err != nil || created.State != operation.State {
		t.Fatalf("CreateOrGet(first) = %#v, %v", created, err)
	}
	replayed, err := store.CreateOrGet(ctx, biz.OIDCOperation{
		Kind: operation.Kind, Provider: operation.Provider, Audience: operation.Audience,
		TenantID: operation.TenantID, State: "different-generated-state", Nonce: "different-nonce",
		CodeVerifier: "different-verifier", RedirectURI: operation.RedirectURI,
		IdempotencyKey: operation.IdempotencyKey, RequestFingerprint: operation.RequestFingerprint,
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil || replayed.State != operation.State {
		t.Fatalf("CreateOrGet(replay) = %#v, %v", replayed, err)
	}
	conflict := operation
	conflict.State = "conflict-state"
	conflict.RequestFingerprint = strings.Repeat("b", 64)
	if _, err := store.CreateOrGet(ctx, conflict); !errors.Is(err, biz.ErrIdempotencyConflict) {
		t.Fatalf("CreateOrGet(conflict) error = %v, want ErrIdempotencyConflict", err)
	}

	keys, err := client.Keys(ctx, "ani-iam:dp2-07:test-run:oidc:*").Result()
	if err != nil || len(keys) != 2 {
		t.Fatalf("OIDC Redis keys = %#v, %v, want operation and idempotency keys", keys, err)
	}
	for _, key := range keys {
		if strings.Contains(key, operation.State) || strings.Contains(key, operation.IdempotencyKey) {
			t.Fatalf("OIDC Redis key contains raw secret/input: %q", key)
		}
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > 10*time.Minute {
			t.Fatalf("OIDC Redis TTL for %q = %s, %v", key, ttl, err)
		}
	}
	if _, err := store.Consume(ctx, " "+operation.State); !errors.Is(err, biz.ErrOIDCStateInvalid) {
		t.Fatalf("Consume(whitespace-prefixed state) error = %v, want ErrOIDCStateInvalid", err)
	}
	remainingAfterMalformedState, err := client.Keys(ctx, "ani-iam:dp2-07:test-run:oidc:*").Result()
	if err != nil || len(remainingAfterMalformedState) != 2 {
		t.Fatalf("OIDC Redis keys after malformed state = %#v, %v, want operation and idempotency keys", remainingAfterMalformedState, err)
	}

	const consumers = 16
	type consumeResult struct {
		operation biz.OIDCOperation
		err       error
	}
	results := make(chan consumeResult, consumers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(consumers)
	for range consumers {
		go func() {
			defer waitGroup.Done()
			consumed, err := store.Consume(ctx, operation.State)
			results <- consumeResult{operation: consumed, err: err}
		}()
	}
	waitGroup.Wait()
	close(results)
	successes := 0
	replays := 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			if result.operation.State != operation.State || result.operation.Nonce != operation.Nonce || result.operation.CodeVerifier != operation.CodeVerifier {
				t.Fatalf("successful Consume() = %#v", result.operation)
			}
		case errors.Is(result.err, biz.ErrOIDCStateInvalid):
			replays++
		default:
			t.Fatalf("concurrent Consume() error = %v", result.err)
		}
	}
	if successes != 1 || replays != consumers-1 {
		t.Fatalf("concurrent Consume() successes = %d, replays = %d", successes, replays)
	}
	remaining, err := client.Keys(ctx, "ani-iam:dp2-07:test-run:oidc:*").Result()
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining OIDC Redis keys = %#v, %v", remaining, err)
	}
}
