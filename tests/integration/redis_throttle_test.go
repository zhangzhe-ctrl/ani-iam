//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

const redisImage = "redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf"

func TestRedisLoginThrottleIsolatesHashedAccountAndSourceIPLocks(t *testing.T) {
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
		Addr:         endpoint,
		MaxRetries:   -1,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping pinned Redis container: %v", err)
	}

	const baseDelay = 10 * time.Millisecond
	throttle, err := data.NewRedisLoginThrottle(client, data.RedisLoginThrottleConfig{
		Namespace: "ani-iam:dp2-06:test-run",
		Limit:     5,
		Window:    15 * time.Minute,
		BaseDelay: baseDelay,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}
	lockedAttempt := biz.LoginThrottleAttempt{
		NormalizedAccount: "user@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.10"),
	}
	if err := throttle.Check(ctx, lockedAttempt); err != nil {
		t.Fatalf("Check() before failures error = %v", err)
	}
	for failure := 1; failure <= 5; failure++ {
		if err := throttle.RecordFailure(ctx, lockedAttempt); err != nil {
			t.Fatalf("RecordFailure() failure %d error = %v", failure, err)
		}
		if failure < 5 {
			wantDelay := baseDelay * time.Duration(1<<(failure-1))
			assertRedisLoginRateLimit(t, throttle.Check(ctx, lockedAttempt), "password_account", wantDelay+50*time.Millisecond)
			assertRedisLoginRateLimit(t, throttle.Check(ctx, biz.LoginThrottleAttempt{
				NormalizedAccount: "other@example.com",
				SourceIP:          lockedAttempt.SourceIP,
			}), "password_ip", wantDelay+50*time.Millisecond)
			time.Sleep(wantDelay + 5*time.Millisecond)
			if err := throttle.Check(ctx, lockedAttempt); err != nil {
				t.Fatalf("Check() after increasing delay %d elapsed error = %v", failure, err)
			}
		}
	}
	assertRedisLoginRateLimit(t, throttle.Check(ctx, biz.LoginThrottleAttempt{
		NormalizedAccount: "user@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.11"),
	}), "password_account", 15*time.Minute)
	assertRedisLoginRateLimit(t, throttle.Check(ctx, biz.LoginThrottleAttempt{
		NormalizedAccount: "other@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.10"),
	}), "password_ip", 15*time.Minute)

	refreshAttempt := biz.RefreshThrottleAttempt{Digest: sha256.Sum256([]byte("opaque-refresh-token"))}
	for attempt := 1; attempt <= 5; attempt++ {
		if err := throttle.CheckRefresh(ctx, refreshAttempt); err != nil {
			t.Fatalf("CheckRefresh() attempt %d error = %v", attempt, err)
		}
	}
	assertRedisLoginRateLimit(t, throttle.CheckRefresh(ctx, refreshAttempt), "refresh_token", 15*time.Minute)
	refreshKeys, err := client.Keys(ctx, "ani-iam:dp2-06:test-run:refresh:token:*").Result()
	if err != nil || len(refreshKeys) != 1 {
		t.Fatalf("refresh digest keys = %#v, error = %v, want one", refreshKeys, err)
	}
	if strings.Contains(refreshKeys[0], "opaque-refresh-token") {
		t.Fatalf("Redis refresh key contains raw credential: %q", refreshKeys[0])
	}
	if err := throttle.Check(ctx, biz.LoginThrottleAttempt{
		NormalizedAccount: "other@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.11"),
	}); err != nil {
		t.Fatalf("Check() unrelated account/IP error = %v", err)
	}

	keys, err := client.Keys(ctx, "ani-iam:dp2-06:test-run:login:*").Result()
	if err != nil {
		t.Fatalf("read isolated Redis keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("isolated Redis keys = %#v, want one account digest and one IP digest", keys)
	}
	for _, key := range keys {
		if strings.Contains(key, "user@example.com") || strings.Contains(key, "203.0.113.10") {
			t.Fatalf("Redis key contains raw abuse-control input: %q", key)
		}
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("read isolated Redis TTL: %v", err)
		}
		if ttl <= 0 || ttl > 15*time.Minute {
			t.Fatalf("Redis TTL = %s, want (0, 15m]", ttl)
		}
	}
	accountKeys, err := client.Keys(ctx, "ani-iam:dp2-06:test-run:login:account:*").Result()
	if err != nil || len(accountKeys) != 1 {
		t.Fatalf("account digest keys = %#v, error = %v, want one", accountKeys, err)
	}
	ipKeys, err := client.Keys(ctx, "ani-iam:dp2-06:test-run:login:ip:*").Result()
	if err != nil || len(ipKeys) != 1 {
		t.Fatalf("IP digest keys = %#v, error = %v, want one", ipKeys, err)
	}

	if err := throttle.Reset(ctx, lockedAttempt); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	exists, err := client.Exists(ctx, accountKeys[0]).Result()
	if err != nil {
		t.Fatalf("read reset account state: %v", err)
	}
	if exists != 0 {
		t.Fatalf("account Redis key exists after reset = %d, want 0", exists)
	}
	exists, err = client.Exists(ctx, ipKeys[0]).Result()
	if err != nil {
		t.Fatalf("read shared IP state after reset: %v", err)
	}
	if exists != 1 {
		t.Fatalf("shared IP Redis key exists after reset = %d, want 1", exists)
	}
	if err := throttle.Check(ctx, biz.LoginThrottleAttempt{
		NormalizedAccount: "user@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.11"),
	}); err != nil {
		t.Fatalf("Check() reset account with clean IP error = %v", err)
	}
	assertRedisLoginRateLimit(t, throttle.Check(ctx, biz.LoginThrottleAttempt{
		NormalizedAccount: "other@example.com",
		SourceIP:          netip.MustParseAddr("203.0.113.10"),
	}), "password_ip", 15*time.Minute)

	stopTimeout := time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop Redis for failure injection: %v", err)
	}
	assertStoppedRedisFailure := func(operation string, invoke func(context.Context) error) {
		t.Helper()
		failureContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := invoke(failureContext); err == nil || errors.Is(err, biz.ErrAuthenticationRateLimited) {
			t.Fatalf("%s with Redis stopped error = %v, want dependency failure", operation, err)
		}
	}
	assertStoppedRedisFailure("Check()", func(ctx context.Context) error {
		return throttle.Check(ctx, lockedAttempt)
	})
	assertStoppedRedisFailure("RecordFailure()", func(ctx context.Context) error {
		return throttle.RecordFailure(ctx, lockedAttempt)
	})
	assertStoppedRedisFailure("Reset()", func(ctx context.Context) error {
		return throttle.Reset(ctx, lockedAttempt)
	})
	assertStoppedRedisFailure("CheckRefresh()", func(ctx context.Context) error {
		return throttle.CheckRefresh(ctx, biz.RefreshThrottleAttempt{Digest: sha256.Sum256([]byte("stopped-refresh-token"))})
	})
}

func assertRedisLoginRateLimit(t *testing.T, err error, wantScope string, maxRetryAfter time.Duration) {
	t.Helper()
	if !errors.Is(err, biz.ErrAuthenticationRateLimited) {
		t.Fatalf("rate-limit error = %v, want %v", err, biz.ErrAuthenticationRateLimited)
	}
	var rateLimit *biz.AuthenticationRateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.LimitScope != wantScope || rateLimit.RetryAfter <= 0 || rateLimit.RetryAfter > maxRetryAfter {
		t.Fatalf("rate-limit details = %#v, want scope %q and retry in (0, %s]", rateLimit, wantScope, maxRetryAfter)
	}
}
