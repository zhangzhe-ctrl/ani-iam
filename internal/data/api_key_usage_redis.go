package data

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

var ErrInvalidRedisAPIKeyUsageConfiguration = errors.New("invalid Redis API key usage configuration")

type RedisAPIKeyUsageConfig struct {
	Namespace string
	BatchSize int64
}

type RedisAPIKeyUsageAggregator struct {
	client    redis.UniversalClient
	data      *Data
	key       string
	batchSize int64
}

var observeAPIKeyUse = redis.NewScript(`
local previous = redis.call("ZSCORE", KEYS[1], ARGV[1])
if not previous or tonumber(ARGV[2]) > tonumber(previous) then
  redis.call("ZADD", KEYS[1], ARGV[2], ARGV[1])
end
return 1
`)

var acknowledgeAPIKeyUse = redis.NewScript(`
local current = redis.call("ZSCORE", KEYS[1], ARGV[1])
if current and tonumber(current) <= tonumber(ARGV[2]) then
  return redis.call("ZREM", KEYS[1], ARGV[1])
end
return 0
`)

func NewRedisAPIKeyUsageAggregator(
	client redis.UniversalClient,
	data *Data,
	config RedisAPIKeyUsageConfig,
) (*RedisAPIKeyUsageAggregator, error) {
	namespace := strings.Trim(strings.TrimSpace(config.Namespace), ":")
	if client == nil || data == nil || data.pool == nil || namespace == "" || config.BatchSize <= 0 || config.BatchSize > 4096 {
		return nil, ErrInvalidRedisAPIKeyUsageConfiguration
	}
	return &RedisAPIKeyUsageAggregator{
		client: client, data: data, key: namespace + ":api-key:usage", batchSize: config.BatchSize,
	}, nil
}

func (a *RedisAPIKeyUsageAggregator) ObserveAPIKeyUse(
	ctx context.Context,
	scope biz.TenantScope,
	keyID uuid.UUID,
	observedAt time.Time,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if keyID == uuid.Nil || observedAt.IsZero() {
		return biz.ErrInvalidPersistenceState
	}
	observedAt = observedAt.UTC()
	member := apiKeyUsageMember(tenantID, keyID)
	if err := observeAPIKeyUse.Run(
		ctx, a.client, []string{a.key}, member, observedAt.UnixMilli(),
	).Err(); err != nil {
		return fmt.Errorf("observe Redis API key usage: %w", err)
	}
	return nil
}

// Flush persists at most one bounded batch. A successful acknowledgement
// removes only the exact observation that was written; a newer concurrent
// observation remains pending. PostgreSQL failure leaves the Redis entry
// untouched for the next lifecycle iteration.
func (a *RedisAPIKeyUsageAggregator) Flush(ctx context.Context) (int, error) {
	entries, err := a.client.ZRangeWithScores(ctx, a.key, 0, a.batchSize-1).Result()
	if err != nil {
		return 0, fmt.Errorf("read Redis API key usage batch: %w", err)
	}
	processed := 0
	for _, entry := range entries {
		member, ok := entry.Member.(string)
		if !ok || entry.Score < float64(math.MinInt64) || entry.Score > float64(math.MaxInt64) {
			return processed, errors.New("read Redis API key usage entry returned invalid state")
		}
		tenantID, keyID, err := parseAPIKeyUsageMember(member)
		if err != nil {
			return processed, err
		}
		observedAt := time.UnixMilli(int64(entry.Score)).UTC()
		recordedID, err := sqlcgen.New(a.data.pool).RecordAPIKeyUse(ctx, sqlcgen.RecordAPIKeyUseParams{
			ObservedAt: requiredTimestamptz(observedAt), TenantID: tenantID, KeyID: keyID,
		})
		if err != nil {
			return processed, mapPostgresError("flush API key usage", err, nil)
		}
		if recordedID != keyID {
			return processed, biz.ErrInvalidPersistenceState
		}
		if err := acknowledgeAPIKeyUse.Run(
			ctx, a.client, []string{a.key}, member, strconv.FormatInt(observedAt.UnixMilli(), 10),
		).Err(); err != nil {
			return processed, fmt.Errorf("acknowledge Redis API key usage: %w", err)
		}
		processed++
	}
	return processed, nil
}

func apiKeyUsageMember(tenantID, keyID uuid.UUID) string {
	return tenantID.String() + "/" + keyID.String()
}

func parseAPIKeyUsageMember(member string) (uuid.UUID, uuid.UUID, error) {
	parts := strings.Split(member, "/")
	if len(parts) != 2 {
		return uuid.Nil, uuid.Nil, errors.New("Redis API key usage member is invalid")
	}
	tenantID, tenantErr := uuid.Parse(parts[0])
	keyID, keyErr := uuid.Parse(parts[1])
	if tenantErr != nil || keyErr != nil || tenantID == uuid.Nil || keyID == uuid.Nil {
		return uuid.Nil, uuid.Nil, errors.New("Redis API key usage member is invalid")
	}
	return tenantID, keyID, nil
}

var _ biz.APIKeyUsageObserver = (*RedisAPIKeyUsageAggregator)(nil)
