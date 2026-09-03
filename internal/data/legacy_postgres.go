package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LegacyPostgresConfig struct {
	DSN          string
	ExpectedRole string
}

type LegacyRoleInfo struct {
	Name       string
	CanLogin   bool
	Superuser  bool
	BypassRLS  bool
	CreateRole bool
	CreateDB   bool
}

// LegacyPostgres preserves the old app.current_tenant_id transaction boundary.
// It refuses owner, superuser, or BYPASSRLS connections before exposing a pool.
type LegacyPostgres struct {
	pool *pgxpool.Pool
	role LegacyRoleInfo
}

func OpenLegacyPostgres(ctx context.Context, cfg LegacyPostgresConfig) (*LegacyPostgres, error) {
	if strings.TrimSpace(cfg.DSN) == "" || strings.TrimSpace(cfg.ExpectedRole) == "" {
		return nil, fmt.Errorf("postgres DSN and expected runtime role are required")
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolConfig.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, unavailable("postgres", "open", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			pool.Close()
		}
	}()
	if err := pool.Ping(ctx); err != nil {
		return nil, unavailable("postgres", "ping", err)
	}

	var role LegacyRoleInfo
	err = pool.QueryRow(ctx, `
		SELECT rolname, rolcanlogin, rolsuper, rolbypassrls, rolcreaterole, rolcreatedb
		FROM pg_roles
		WHERE rolname = current_user
	`).Scan(&role.Name, &role.CanLogin, &role.Superuser, &role.BypassRLS, &role.CreateRole, &role.CreateDB)
	if err != nil {
		return nil, postgresError("inspect runtime role", err)
	}
	if role.Name != cfg.ExpectedRole || !role.CanLogin || role.Superuser || role.BypassRLS || role.CreateRole || role.CreateDB {
		return nil, fmt.Errorf("%w: role=%q login=%t superuser=%t bypassrls=%t createrole=%t createdb=%t",
			ErrUnsafeRuntimeRole, role.Name, role.CanLogin, role.Superuser, role.BypassRLS, role.CreateRole, role.CreateDB)
	}

	closeOnError = false
	return &LegacyPostgres{pool: pool, role: role}, nil
}

func (p *LegacyPostgres) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func (p *LegacyPostgres) RoleInfo() LegacyRoleInfo {
	if p == nil {
		return LegacyRoleInfo{}
	}
	return p.role
}

// WithTenantTx sets the old RLS GUC locally for exactly one transaction.
func (p *LegacyPostgres) WithTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant ID is required")
	}
	return p.withTx(ctx, func(tx pgx.Tx) error {
		var normalized string
		if err := tx.QueryRow(ctx, `SELECT set_config('app.current_tenant_id', $1::uuid::text, true)`, tenantID).Scan(&normalized); err != nil {
			return postgresError("set tenant", err)
		}
		if normalized != tenantID {
			return fmt.Errorf("postgres tenant context mismatch")
		}
		return fn(tx)
	})
}

// WithPlatformTx intentionally leaves app.current_tenant_id unset. It is only
// for old platform rows whose frozen policy explicitly permits tenant_id NULL.
func (p *LegacyPostgres) WithPlatformTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return p.withTx(ctx, fn)
}

func (p *LegacyPostgres) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	if p == nil || p.pool == nil {
		return unavailable("postgres", "begin transaction", errors.New("pool is nil"))
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return postgresError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return postgresError("commit transaction", err)
	}
	return nil
}

func (p *LegacyPostgres) begin(ctx context.Context) (pgx.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, unavailable("postgres", "begin transaction", errors.New("pool is nil"))
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, postgresError("begin transaction", err)
	}
	return tx, nil
}

func upsertJWTBlocklist(ctx context.Context, tx pgx.Tx, jti string, expiresAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO jwt_blocklist (jti, expires_at)
		VALUES ($1, $2)
		ON CONFLICT (jti) DO UPDATE
		SET expires_at = GREATEST(jwt_blocklist.expires_at, EXCLUDED.expires_at),
		    revoked_at = NOW()
	`, jti, expiresAt)
	if err != nil {
		return postgresError("persist jwt blocklist", err)
	}
	return nil
}

func (p *LegacyPostgres) IsJWTRevoked(ctx context.Context, jti string) (bool, error) {
	if p == nil || p.pool == nil {
		return false, unavailable("postgres", "read jwt blocklist", errors.New("pool is nil"))
	}
	var exists bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM jwt_blocklist WHERE jti=$1 AND expires_at > NOW()
	)`, jti).Scan(&exists)
	if err != nil {
		return false, postgresError("read jwt blocklist", err)
	}
	return exists, nil
}

func postgresError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "57P01" || pgErr.Code == "57P02" || pgErr.Code == "57P03" {
			return unavailable("postgres", operation, err)
		}
		return err
	}
	return unavailable("postgres", operation, err)
}
