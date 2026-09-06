//go:build integration

package integration_test

import (
	"context"
	"errors"
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

func TestRedisLoginThrottleEnforcesLimitAndTTL(t *testing.T) {
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

	throttle, err := data.NewRedisLoginThrottle(client, data.RedisLoginThrottleConfig{
		Namespace: "ani-iam:dp2-05:test-run",
		Limit:     2,
		Window:    time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := throttle.Allow(ctx, "user@example.com"); err != nil {
			t.Fatalf("Allow() attempt %d error = %v", attempt, err)
		}
	}
	if err := throttle.Allow(ctx, "user@example.com"); !errors.Is(err, biz.ErrAuthenticationRateLimited) {
		t.Fatalf("Allow() above limit error = %v, want %v", err, biz.ErrAuthenticationRateLimited)
	} else {
		var rateLimit *biz.AuthenticationRateLimitError
		if !errors.As(err, &rateLimit) || rateLimit.LimitScope != "password_account" || rateLimit.RetryAfter != time.Minute {
			t.Fatalf("Allow() rate-limit details = %#v, want password_account/1m", rateLimit)
		}
	}

	keys, err := client.Keys(ctx, "ani-iam:dp2-05:test-run:login:account:*").Result()
	if err != nil {
		t.Fatalf("read isolated Redis keys: %v", err)
	}
	if len(keys) != 1 || strings.Contains(keys[0], "user@example.com") {
		t.Fatalf("isolated Redis keys = %#v, want one non-PII account digest", keys)
	}
	ttl, err := client.PTTL(ctx, keys[0]).Result()
	if err != nil {
		t.Fatalf("read isolated Redis TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Fatalf("Redis TTL = %s, want (0, 1m]", ttl)
	}

	if err := throttle.Reset(ctx, "user@example.com"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	exists, err := client.Exists(ctx, keys[0]).Result()
	if err != nil {
		t.Fatalf("read reset Redis state: %v", err)
	}
	if exists != 0 {
		t.Fatalf("Redis key exists after reset = %d, want 0", exists)
	}
	if err := throttle.Allow(ctx, "user@example.com"); err != nil {
		t.Fatalf("Allow() after reset error = %v", err)
	}

	stopTimeout := time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop Redis for failure injection: %v", err)
	}
	failureContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := throttle.Allow(failureContext, "user@example.com"); err == nil || errors.Is(err, biz.ErrAuthenticationRateLimited) {
		t.Fatalf("Allow() with Redis stopped error = %v, want dependency failure", err)
	}
}
