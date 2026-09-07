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
	BaseDelay time.Duration
}

type redisLoginThrottle struct {
	client    redis.UniversalClient
	namespace string
	limit     int64
	window    time.Duration
	baseDelay time.Duration
}

var checkLoginThrottle = redis.NewScript(`
local now_parts = redis.call("TIME")
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
for index, key in ipairs(KEYS) do
  local key_type = redis.call("TYPE", key)["ok"]
  if key_type == "string" then
    local count = tonumber(redis.call("GET", key) or "0")
    if count >= tonumber(ARGV[1]) then
      local ttl = redis.call("PTTL", key)
      if ttl < 1 then
        return {-1, ttl}
      end
      return {index, ttl}
    end
  elseif key_type == "hash" then
    local blocked_until = tonumber(redis.call("HGET", key, "blocked_until_ms") or "0")
    local remaining = blocked_until - now
    if remaining > 0 then
      return {index, remaining}
    end
  elseif key_type ~= "none" then
    return {-1, -1}
  end
end
return {0, 0}
`)

var recordLoginThrottleFailure = redis.NewScript(`
local window = tonumber(ARGV[1])
local base_delay = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local now_parts = redis.call("TIME")
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local counts = {}
for index, key in ipairs(KEYS) do
  local key_type = redis.call("TYPE", key)["ok"]
  local previous_count = 0
  if key_type == "string" then
    previous_count = tonumber(redis.call("GET", key) or "0")
    redis.call("DEL", key)
  elseif key_type == "hash" then
    previous_count = tonumber(redis.call("HGET", key, "count") or "0")
  elseif key_type ~= "none" then
    return redis.error_reply("invalid login throttle key type")
  end
  local count = previous_count + 1
  local delay = base_delay * (2 ^ math.max(0, math.min(count, limit) - 1))
  if count >= limit or delay > window then
    delay = window
  end
  redis.call("HSET", key, "count", count, "blocked_until_ms", now + delay)
  redis.call("PEXPIRE", key, window)
  counts[index] = count
end
return counts
`)

var checkRefreshThrottle = redis.NewScript(`
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

func NewRedisLoginThrottle(client redis.UniversalClient, config RedisLoginThrottleConfig) (biz.LoginThrottle, error) {
	namespace := strings.Trim(strings.TrimSpace(config.Namespace), ":")
	if client == nil || namespace == "" || config.Limit < 2 || config.Limit > 31 ||
		config.Window <= 0 || config.Window.Milliseconds() < 1 ||
		config.BaseDelay <= 0 || config.BaseDelay.Milliseconds() < 1 || config.BaseDelay >= config.Window {
		return nil, ErrInvalidRedisLoginThrottleConfiguration
	}
	return &redisLoginThrottle{
		client:    client,
		namespace: namespace,
		limit:     config.Limit,
		window:    config.Window,
		baseDelay: config.BaseDelay,
	}, nil
}

func (t *redisLoginThrottle) Check(ctx context.Context, attempt biz.LoginThrottleAttempt) error {
	keys, err := t.keys(attempt)
	if err != nil {
		return err
	}
	result, err := checkLoginThrottle.Run(ctx, t.client, keys, t.limit).Int64Slice()
	if err != nil {
		return fmt.Errorf("check Redis login throttle: %w", err)
	}
	if len(result) != 2 || result[0] < 0 {
		return errors.New("check Redis login throttle returned invalid state")
	}
	if result[0] == 0 {
		return nil
	}
	scope := "password_account"
	if result[0] == 2 {
		scope = "password_ip"
	} else if result[0] != 1 {
		return errors.New("check Redis login throttle returned invalid scope")
	}
	return &biz.AuthenticationRateLimitError{
		LimitScope: scope,
		RetryAfter: time.Duration(result[1]) * time.Millisecond,
	}
}

func (t *redisLoginThrottle) RecordFailure(ctx context.Context, attempt biz.LoginThrottleAttempt) error {
	keys, err := t.keys(attempt)
	if err != nil {
		return err
	}
	result, err := recordLoginThrottleFailure.Run(
		ctx,
		t.client,
		keys,
		t.window.Milliseconds(),
		t.baseDelay.Milliseconds(),
		t.limit,
	).Int64Slice()
	if err != nil {
		return fmt.Errorf("record Redis login throttle failure: %w", err)
	}
	if len(result) != len(keys) {
		return errors.New("record Redis login throttle failure returned invalid state")
	}
	return nil
}

func (t *redisLoginThrottle) Reset(ctx context.Context, attempt biz.LoginThrottleAttempt) error {
	keys, err := t.keys(attempt)
	if err != nil {
		return err
	}
	if err := t.client.Del(ctx, keys[0]).Err(); err != nil {
		return fmt.Errorf("reset Redis login throttle: %w", err)
	}
	return nil
}

func (t *redisLoginThrottle) CheckRefresh(ctx context.Context, attempt biz.RefreshThrottleAttempt) error {
	if attempt.Digest == ([sha256.Size]byte{}) {
		return errors.New("refresh-token digest is required")
	}
	key := t.namespace + ":refresh:token:" + hex.EncodeToString(attempt.Digest[:])
	result, err := checkRefreshThrottle.Run(ctx, t.client, []string{key}, t.window.Milliseconds(), t.limit).Int64Slice()
	if err != nil {
		return fmt.Errorf("check Redis refresh throttle: %w", err)
	}
	if len(result) != 2 || result[0] < 0 {
		return errors.New("check Redis refresh throttle returned invalid state")
	}
	if result[0] == 0 {
		return nil
	}
	if result[0] != 1 {
		return errors.New("check Redis refresh throttle returned invalid scope")
	}
	return &biz.AuthenticationRateLimitError{
		LimitScope: "refresh_token",
		RetryAfter: time.Duration(result[1]) * time.Millisecond,
	}
}

func (t *redisLoginThrottle) keys(attempt biz.LoginThrottleAttempt) ([]string, error) {
	normalizedAccount := strings.ToLower(strings.TrimSpace(attempt.NormalizedAccount))
	if normalizedAccount == "" || normalizedAccount != attempt.NormalizedAccount {
		return nil, errors.New("normalized login account is required")
	}
	if !attempt.SourceIP.IsValid() {
		return nil, errors.New("login source IP is required")
	}
	accountDigest := sha256.Sum256([]byte(normalizedAccount))
	ipDigest := sha256.Sum256([]byte(attempt.SourceIP.Unmap().String()))
	return []string{
		t.namespace + ":login:account:" + hex.EncodeToString(accountDigest[:]),
		t.namespace + ":login:ip:" + hex.EncodeToString(ipDigest[:]),
	}, nil
}

var _ biz.LoginThrottle = (*redisLoginThrottle)(nil)
