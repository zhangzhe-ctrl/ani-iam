package data

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (tx postgresTenantAuthorizationTransaction) LockMutation(ctx context.Context, scope biz.TenantScope, identity biz.MutationIdentity) error {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	// Length-separated values avoid ambiguity. Advisory hash collisions only
	// serialize independent keys; full identity/intent is checked in the table.
	lock := strings.Join([]string{tenantID.String(), identity.ActorID.String(), identity.Operation, hex.EncodeToString([]byte(identity.Key))}, ":")
	return mapPostgresError("lock mutation result", tx.queries.LockTenantMutationResult(ctx, sqlcgen.LockTenantMutationResultParams{LockKey: lock}), nil)
}

func (tx postgresTenantAuthorizationTransaction) FindMutation(ctx context.Context, scope biz.TenantScope, identity biz.MutationIdentity) (biz.StoredMutation, bool, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.StoredMutation{}, false, err
	}
	row, err := tx.queries.FindTenantMutationResult(ctx, sqlcgen.FindTenantMutationResultParams{
		TenantID: tenantID, ActorID: identity.ActorID, Operation: identity.Operation, IdempotencyKey: identity.Key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.StoredMutation{}, false, nil
	}
	if err != nil {
		return biz.StoredMutation{}, false, mapPostgresError("read mutation result", err, nil)
	}
	if len(row.IntentDigest) != 32 || !row.CreatedAt.Valid || !row.ExpiresAt.Valid {
		return biz.StoredMutation{}, false, biz.ErrInvalidPersistenceState
	}
	copy(identity.Intent[:], row.IntentDigest)
	identity.CallerID = uuid.UUID(row.CallerPrincipalID.Bytes)
	return biz.StoredMutation{Identity: identity, Result: row.Result, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time}, true, nil
}

func (tx postgresTenantAuthorizationTransaction) SaveMutation(ctx context.Context, scope biz.TenantScope, result biz.StoredMutation) error {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	return mapPostgresError("save mutation result", tx.queries.SaveTenantMutationResult(ctx, sqlcgen.SaveTenantMutationResultParams{
		TenantID: tenantID, ActorID: result.Identity.ActorID, Operation: result.Identity.Operation, IdempotencyKey: result.Identity.Key,
		IntentDigest: result.Identity.Intent[:], CallerPrincipalID: optionalPGUUID(result.Identity.CallerID), Result: result.Result,
		CreatedAt: requiredTimestamptz(result.CreatedAt), ExpiresAt: requiredTimestamptz(result.ExpiresAt),
	}), nil)
}
