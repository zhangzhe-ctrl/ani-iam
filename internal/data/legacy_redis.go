package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const (
	legacyOIDCStatePrefix  = "oidc:state:"
	legacyJWTBlockPrefix   = "jwt:blocklist:"
	legacyAPIKeyRatePrefix = "api-key:rate:"
)

type LegacyRedisConfig struct {
	Address   string
	Password  string
	DB        int
	Namespace string
}

// LegacyRedis keeps the old logical key names inside a mandatory per-run
// namespace. It exposes no FLUSH operation and can only scan its own prefix.
type LegacyRedis struct {
	client    *redis.Client
	namespace string
}

func OpenLegacyRedis(ctx context.Context, cfg LegacyRedisConfig) (*LegacyRedis, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	if err := validateRedisNamespace(cfg.Namespace); err != nil {
		return nil, err
	}
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.Address,
		Password:        cfg.Password,
		DB:              cfg.DB,
		DisableIdentity: true,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, unavailable("redis", "ping", err)
	}
	return &LegacyRedis{client: client, namespace: cfg.Namespace}, nil
}

func validateRedisNamespace(namespace string) error {
	if !strings.HasPrefix(namespace, "cp0:04:") || !strings.HasSuffix(namespace, ":") || len(namespace) <= len("cp0:04::") {
		return fmt.Errorf("redis namespace must be a unique cp0:04:<run-id>: prefix")
	}
	if strings.ContainsAny(namespace, "*?[\\]") {
		return fmt.Errorf("redis namespace contains glob characters")
	}
	return nil
}

func (r *LegacyRedis) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *LegacyRedis) PhysicalKey(logicalKey string) string {
	if r == nil {
		return ""
	}
	return r.namespace + logicalKey
}

func (r *LegacyRedis) SetOIDCState(ctx context.Context, state string, value []byte, ttl time.Duration) error {
	return r.set(ctx, legacyOIDCStatePrefix+state, value, ttl)
}

func (r *LegacyRedis) ConsumeOIDCState(ctx context.Context, state string) ([]byte, error) {
	key := r.PhysicalKey(legacyOIDCStatePrefix + state)
	value, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, unavailable("redis", "read oidc state", err)
	}
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return nil, unavailable("redis", "consume oidc state", err)
	}
	return value, nil
}

func (r *LegacyRedis) SetJWTBlocklist(ctx context.Context, jti string, ttl time.Duration) error {
	return r.set(ctx, legacyJWTBlockPrefix+jti, []byte("revoked"), ttl)
}

func (r *LegacyRedis) IsJWTBlocked(ctx context.Context, jti string) (bool, error) {
	exists, err := r.client.Exists(ctx, r.PhysicalKey(legacyJWTBlockPrefix+jti)).Result()
	if err != nil {
		return false, unavailable("redis", "read jwt blocklist", err)
	}
	return exists > 0, nil
}

func (r *LegacyRedis) DeleteJWTBlocklist(ctx context.Context, jti string) error {
	if err := r.client.Del(ctx, r.PhysicalKey(legacyJWTBlockPrefix+jti)).Err(); err != nil {
		return unavailable("redis", "delete jwt blocklist", err)
	}
	return nil
}

func (r *LegacyRedis) IncrementAPIKeyRate(ctx context.Context, keyHash string, ttl time.Duration) (int64, error) {
	pipe := r.client.TxPipeline()
	increment := pipe.Incr(ctx, r.PhysicalKey(legacyAPIKeyRatePrefix+keyHash))
	if ttl > 0 {
		pipe.Expire(ctx, r.PhysicalKey(legacyAPIKeyRatePrefix+keyHash), ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, unavailable("redis", "increment api key rate", err)
	}
	return increment.Val(), nil
}

func (r *LegacyRedis) TTL(ctx context.Context, logicalKey string) (time.Duration, error) {
	ttl, err := r.client.TTL(ctx, r.PhysicalKey(logicalKey)).Result()
	if err != nil {
		return 0, unavailable("redis", "read ttl", err)
	}
	return ttl, nil
}

func (r *LegacyRedis) NamespaceKeys(ctx context.Context) ([]string, error) {
	var cursor uint64
	var keys []string
	for {
		page, next, err := r.client.Scan(ctx, cursor, r.namespace+"*", 100).Result()
		if err != nil {
			return nil, unavailable("redis", "scan namespace", err)
		}
		for _, key := range page {
			keys = append(keys, strings.TrimPrefix(key, r.namespace))
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func (r *LegacyRedis) set(ctx context.Context, logicalKey string, value []byte, ttl time.Duration) error {
	if r == nil || r.client == nil {
		return unavailable("redis", "write key", errors.New("client is nil"))
	}
	if ttl <= 0 {
		return fmt.Errorf("redis TTL must be positive")
	}
	if err := r.client.Set(ctx, r.PhysicalKey(logicalKey), value, ttl).Err(); err != nil {
		return unavailable("redis", "write key", err)
	}
	return nil
}
