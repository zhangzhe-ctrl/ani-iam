package data

import (
	"context"
	"fmt"
	"time"
)

// LegacyStorage coordinates the frozen PostgreSQL jwt_blocklist row and Redis
// cache key. Redis failure rolls back PostgreSQL; commit failure compensates the
// cache key so callers never receive a successful partial revocation.
type LegacyStorage struct {
	Postgres *LegacyPostgres
	Redis    *LegacyRedis
	now      func() time.Time
}

func NewLegacyStorage(postgres *LegacyPostgres, redis *LegacyRedis) (*LegacyStorage, error) {
	if postgres == nil || redis == nil {
		return nil, fmt.Errorf("postgres and redis adapters are required")
	}
	return &LegacyStorage{Postgres: postgres, Redis: redis, now: time.Now}, nil
}

func (s *LegacyStorage) RevokeToken(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		return fmt.Errorf("jti is required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	tx, err := s.Postgres.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := upsertJWTBlocklist(ctx, tx, jti, s.now().Add(ttl)); err != nil {
		return err
	}
	if err := s.Redis.SetJWTBlocklist(ctx, jti, ttl); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = s.Redis.DeleteJWTBlocklist(ctx, jti)
		return postgresError("commit jwt blocklist", err)
	}
	return nil
}
