package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

var ErrInvalidRedisLoginThrottleConfiguration = errors.New("invalid Redis login-throttle configuration")

type RedisLoginThrottleConfig struct {
	Namespace string
	Limit     int64
	Window    time.Duration
}

type redisLoginThrottle struct {
	client    redis.UniversalClient
	namespace string
	limit     int64
	window    time.Duration
}

var incrementLoginThrottle = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
local ttl = redis.call("PTTL", KEYS[1])
if count == 1 or ttl < 0 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`)

func NewRedisLoginThrottle(client redis.UniversalClient, config RedisLoginThrottleConfig) (biz.LoginThrottle, error) {
	namespace := strings.Trim(strings.TrimSpace(config.Namespace), ":")
	if client == nil || namespace == "" || config.Limit < 1 || config.Window <= 0 || config.Window.Milliseconds() < 1 {
		return nil, ErrInvalidRedisLoginThrottleConfiguration
	}
	return &redisLoginThrottle{
		client:    client,
		namespace: namespace,
		limit:     config.Limit,
		window:    config.Window,
	}, nil
}

func (t *redisLoginThrottle) Allow(ctx context.Context, normalizedAccount string) error {
	key, err := t.key(normalizedAccount)
	if err != nil {
		return err
	}
	count, err := incrementLoginThrottle.Run(ctx, t.client, []string{key}, t.window.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("increment Redis login throttle: %w", err)
	}
	if count > t.limit {
		return &biz.AuthenticationRateLimitError{
			LimitScope: "password_account",
			RetryAfter: t.window,
		}
	}
	return nil
}

func (t *redisLoginThrottle) Reset(ctx context.Context, normalizedAccount string) error {
	key, err := t.key(normalizedAccount)
	if err != nil {
		return err
	}
	if err := t.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("reset Redis login throttle: %w", err)
	}
	return nil
}

func (t *redisLoginThrottle) key(normalizedAccount string) (string, error) {
	if strings.TrimSpace(normalizedAccount) == "" {
		return "", errors.New("normalized login account is required")
	}
	digest := sha256.Sum256([]byte(normalizedAccount))
	return t.namespace + ":login:account:" + hex.EncodeToString(digest[:]), nil
}

var _ biz.LoginThrottle = (*redisLoginThrottle)(nil)
