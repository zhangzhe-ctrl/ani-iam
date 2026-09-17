package data

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type firstAdministratorRepository struct{ data *Data }

func NewFirstAdministratorRepository(d *Data) biz.FirstAdministratorRepository {
	return &firstAdministratorRepository{data: d}
}

func (r *firstAdministratorRepository) Register(ctx context.Context, intent biz.FirstAdministratorIntent) (biz.FirstAdministratorReceipt, error) {
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.FirstAdministratorReceipt{}, biz.ErrPersistenceUnavailable
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollback)
	}()
	if err = requireRestrictedProvisioner(ctx, tx); err != nil {
		if errors.Is(err, biz.ErrWorkloadBootstrapDenied) {
			err = biz.ErrFirstAdministratorDenied
		}
		return biz.FirstAdministratorReceipt{}, err
	}
	q := sqlcgen.New(tx)
	m := intent.Manifest
	if err = q.LockFirstAdministrator(ctx, sqlcgen.LockFirstAdministratorParams{Environment: m.Environment}); err != nil {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	if _, err = q.GetWorkloadBootstrapEnvironmentReceipt(ctx, sqlcgen.GetWorkloadBootstrapEnvironmentReceiptParams{Environment: m.Environment}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorDenied
		}
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	// Completion takes precedence over receipt replay. Provisioning can never
	// restore an administrator, even if the same original manifest is supplied.
	if _, err = q.GetFirstAdministratorCompletion(ctx, sqlcgen.GetFirstAdministratorCompletionParams{Environment: m.Environment}); err == nil {
		return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	previous, err := q.GetFirstAdministratorIntent(ctx, sqlcgen.GetFirstAdministratorIntentParams{IntentID: m.IntentID})
	if err == nil {
		if previous.Environment != m.Environment || !bytes.Equal(previous.IntentSha256, intent.Digest[:]) {
			return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorConflict
		}
		return firstAdministratorReceipt(previous), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	if !m.ExpiresAt.After(intent.Now) {
		return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorExpired
	}
	leaf, err := q.GetFirstAdministratorLeaf(ctx, sqlcgen.GetFirstAdministratorLeafParams{Environment: m.Environment})
	if err == nil {
		if m.Supersedes != leaf.IntentID || leaf.ExpiresAt.Time.After(intent.Now) {
			return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorConflict
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	} else if m.Supersedes != uuid.Nil {
		return biz.FirstAdministratorReceipt{}, biz.ErrFirstAdministratorConflict
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return biz.FirstAdministratorReceipt{}, biz.ErrPersistenceUnavailable
	}
	if err = q.InsertFirstAdministratorIntent(ctx, sqlcgen.InsertFirstAdministratorIntentParams{
		IntentID: m.IntentID, Environment: m.Environment, NormalizedEmail: m.Email, Issuer: m.Issuer, Subject: m.Subject, IntentSha256: intent.Digest[:],
		Supersedes: optionalPGUUID(m.Supersedes), RegisteredAt: requiredTimestamptz(intent.Now), ExpiresAt: requiredTimestamptz(m.ExpiresAt), AuditEventID: auditID,
	}); err != nil {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	digest := hex.EncodeToString(intent.Digest[:])
	if err = q.InsertFirstAdministratorIntentAudit(ctx, sqlcgen.InsertFirstAdministratorIntentAuditParams{EventID: auditID, IntentID: m.IntentID, RequestID: m.IntentID.String(), IntentDigest: digest, Now: requiredTimestamptz(intent.Now)}); err != nil {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return biz.FirstAdministratorReceipt{}, firstAdministratorPersistenceError(err)
	}
	return biz.FirstAdministratorReceipt{IntentID: m.IntentID, AuditEventID: auditID, IntentSHA256: digest, RegisteredAt: intent.Now}, nil
}

func firstAdministratorReceipt(row sqlcgen.FirstAdministratorIntent) biz.FirstAdministratorReceipt {
	return biz.FirstAdministratorReceipt{IntentID: row.IntentID, AuditEventID: row.AuditEventID, IntentSHA256: hex.EncodeToString(row.IntentSha256), RegisteredAt: row.RegisteredAt.Time}
}

func firstAdministratorPersistenceError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return biz.ErrFirstAdministratorConflict
		case "42501":
			return biz.ErrFirstAdministratorDenied
		}
	}
	return biz.ErrPersistenceUnavailable
}
