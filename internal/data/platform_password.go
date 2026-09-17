package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

type platformPasswordRepository struct{ data *Data }
type platformPasswordTransaction struct{ *platformLoginTransaction }

func NewPlatformPasswordReader(d *Data) biz.PlatformPasswordReader {
	return &platformPasswordRepository{d}
}
func NewPlatformPasswordUnitOfWork(d *Data) biz.PlatformPasswordUnitOfWork {
	return &platformPasswordRepository{d}
}
func (r *platformPasswordRepository) ReadPlatformPassword(ctx context.Context, account string) (biz.PlatformPasswordState, error) {
	v, err := sqlcgen.New(r.data.pool).ReadPlatformPassword(ctx, sqlcgen.ReadPlatformPasswordParams{Account: account})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformPasswordState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.PlatformPasswordState{}, platformLoginPersistenceError(err)
	}
	return platformPasswordState(v), nil
}
func (r *platformPasswordRepository) WithinPlatformPassword(ctx context.Context, fn func(biz.PlatformPasswordTransaction) error) error {
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollback)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err = fn(&platformPasswordTransaction{&platformLoginTransaction{q: q}}); err != nil {
		return err
	}
	return platformLoginPersistenceError(tx.Commit(ctx))
}
func (t *platformPasswordTransaction) LockPassword(ctx context.Context, account string) (biz.PlatformPasswordState, error) {
	v, err := t.q.LockPlatformPassword(ctx, sqlcgen.LockPlatformPasswordParams{Account: account})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformPasswordState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.PlatformPasswordState{}, platformLoginPersistenceError(err)
	}
	return platformPasswordState(sqlcgen.ReadPlatformPasswordRow(v)), nil
}
func (t *platformPasswordTransaction) ResetPasswordFailures(ctx context.Context, id uuid.UUID, version int64, now time.Time) error {
	_, err := t.q.ResetPasswordLoginFailures(ctx, sqlcgen.ResetPasswordLoginFailuresParams{PrincipalID: id, ExpectedVersion: version, UpdatedAt: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrInvalidCredential
	}
	return platformLoginPersistenceError(err)
}
func (t *platformPasswordTransaction) RecordPasswordFailure(ctx context.Context, m biz.LoginFailureMutation) error {
	if m.PrincipalID != uuid.Nil {
		row, err := t.q.RecordPasswordLoginFailure(ctx, sqlcgen.RecordPasswordLoginFailureParams{PrincipalID: m.PrincipalID, FailedAt: requiredTimestamptz(m.FailedAt), LockUntil: requiredTimestamptz(m.LockUntil)})
		if err != nil {
			return platformLoginPersistenceError(err)
		}
		m.Audit.TargetVersion = row.Version
	}
	return appendPlatformAudit(ctx, t.q, m.Audit)
}
func platformPasswordState(v sqlcgen.ReadPlatformPasswordRow) biz.PlatformPasswordState {
	s := biz.PlatformPasswordState{Login: biz.PlatformLoginState{IdentityID: v.IdentityID, IdentityActive: v.IdentityActive, Principal: biz.Principal{ID: v.PrincipalID, Status: biz.PrincipalStatus(v.PrincipalStatus)}, MembershipID: v.MembershipID, MembershipStatus: biz.MembershipStatus(v.MembershipStatus), NormalizedEmail: v.NormalizedEmail}, PasswordHash: v.PasswordHash, CredentialVersion: v.CredentialVersion}
	if v.LockedUntil.Valid {
		s.LockedUntil = v.LockedUntil.Time
	}
	return s
}
