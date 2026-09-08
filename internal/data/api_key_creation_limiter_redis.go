package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

var ErrInvalidRedisAPIKeyCreationLimiterConfiguration = errors.New("invalid Redis API key creation limiter configuration")

type RedisAPIKeyCreationLimiterConfig struct {
	Namespace string
	Limit     int64
	Window    time.Duration
}

type redisAPIKeyCreationLimiter struct {
	client    redis.UniversalClient
	namespace string
	limit     int64
	window    time.Duration
}

var acquireAPIKeyCreation = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
if ttl < 1 then
  return {-1, ttl}
end
if current > tonumber(ARGV[2]) then
  return {1, ttl}
end
return {0, ttl}
`)

func NewRedisAPIKeyCreationLimiter(
	client redis.UniversalClient,
	config RedisAPIKeyCreationLimiterConfig,
) (biz.APIKeyCreationLimiter, error) {
	namespace := strings.Trim(strings.TrimSpace(config.Namespace), ":")
	if client == nil || namespace == "" || config.Limit <= 0 || config.Window <= 0 || config.Window.Milliseconds() < 1 {
		return nil, ErrInvalidRedisAPIKeyCreationLimiterConfiguration
	}
	return &redisAPIKeyCreationLimiter{
		client: client, namespace: namespace, limit: config.Limit, window: config.Window,
	}, nil
}

func (l *redisAPIKeyCreationLimiter) Acquire(
	ctx context.Context,
	scope biz.TenantScope,
	principalID uuid.UUID,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if principalID == uuid.Nil {
		return biz.ErrInvalidPersistenceState
	}
	key := l.namespace + ":api-key:create:" + tenantID.String() + ":" + principalID.String()
	result, err := acquireAPIKeyCreation.Run(
		ctx, l.client, []string{key}, l.window.Milliseconds(), l.limit,
	).Int64Slice()
	if err != nil {
		return fmt.Errorf("acquire Redis API key creation limit: %w", err)
	}
	if len(result) != 2 || result[0] < 0 || result[1] < 1 {
		return errors.New("acquire Redis API key creation limit returned invalid state")
	}
	if result[0] == 0 {
		return nil
	}
	if result[0] != 1 {
		return errors.New("acquire Redis API key creation limit returned invalid scope")
	}
	return &biz.AuthenticationRateLimitError{
		LimitScope: "api_key_creation", RetryAfter: time.Duration(result[1]) * time.Millisecond,
	}
}

var _ biz.APIKeyCreationLimiter = (*redisAPIKeyCreationLimiter)(nil)
