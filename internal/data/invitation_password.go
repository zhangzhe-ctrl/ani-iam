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

type invitationPasswordRepository struct{ data *Data }

func NewInvitationPasswordRepository(d *Data) biz.InvitationPasswordRepository {
	return &invitationPasswordRepository{d}
}
func invitationPasswordState(r sqlcgen.ReadInvitationPasswordRow) biz.InvitationPasswordState {
	s := biz.InvitationPasswordState{PrincipalID: r.PrincipalID, IdentityID: r.IdentityID, PrincipalStatus: biz.PrincipalStatus(r.PrincipalStatus), IdentityActive: r.IdentityActive, NormalizedEmail: r.NormalizedEmail, PasswordHash: r.PasswordHash, CredentialVersion: r.CredentialVersion}
	if r.LockedUntil.Valid {
		s.LockedUntil = r.LockedUntil.Time.UTC()
	}
	return s
}
func (r *invitationPasswordRepository) ReadInvitationPassword(ctx context.Context, email string) (biz.InvitationPasswordState, error) {
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.InvitationPasswordState{}, biz.ErrAuthenticationDependency
	}
	v, err := sqlcgen.New(r.data.pool).ReadInvitationPassword(ctx, sqlcgen.ReadInvitationPasswordParams{Account: email})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.InvitationPasswordState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.InvitationPasswordState{}, mapPostgresError("read independent invitation password", err, nil)
	}
	return invitationPasswordState(v), nil
}
func lockInvitationPassword(ctx context.Context, q *sqlcgen.Queries, email string) (biz.InvitationPasswordState, error) {
	v, err := q.LockInvitationPassword(ctx, sqlcgen.LockInvitationPasswordParams{Account: email})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.InvitationPasswordState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.InvitationPasswordState{}, mapPostgresError("lock independent invitation password", err, nil)
	}
	return invitationPasswordState(sqlcgen.ReadInvitationPasswordRow(v)), nil
}
func (t *invitationAcceptanceTransaction) ResetAcceptancePasswordFailures(ctx context.Context, cap biz.InvitationAcceptanceCapability, version int64, now time.Time) error {
	b, _, err := t.binding(cap)
	if err != nil {
		return err
	}
	if b.Password == nil {
		return biz.ErrInvitationDenied
	}
	_, err = t.q.ResetPasswordLoginFailures(ctx, sqlcgen.ResetPasswordLoginFailuresParams{PrincipalID: b.Subject, ExpectedVersion: version, UpdatedAt: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrInvalidCredential
	}
	return mapPostgresError("reset accepted recipient failures", err, nil)
}
func (r *invitationPasswordRepository) RecordInvitationPasswordFailure(ctx context.Context, observed biz.InvitationPasswordState, a biz.SecurityAuditEvent, increment bool) error {
	if r == nil || r.data == nil || r.data.pool == nil || a.ActorID != uuid.Nil || a.Boundary != biz.AuditBoundaryPrincipal || a.DirectCaller.Identity.PrincipalID == uuid.Nil {
		return biz.ErrAuthenticationDependency
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin invitation authentication denial", err, nil)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return mapPostgresError("lock invitation authentication guard", err, nil)
	}
	if increment && observed.PrincipalID != uuid.Nil {
		current, e := lockInvitationPassword(ctx, q, observed.NormalizedEmail)
		if e != nil && !errors.Is(e, biz.ErrInvalidCredential) {
			return e
		}
		now := time.Now().UTC()
		if e == nil && current.PrincipalID == observed.PrincipalID && current.IdentityID == observed.IdentityID && current.PasswordHash == observed.PasswordHash && current.IdentityActive && current.PrincipalStatus == biz.PrincipalStatusActive && !now.Before(current.LockedUntil) {
			row, e := q.RecordPasswordLoginFailure(ctx, sqlcgen.RecordPasswordLoginFailureParams{PrincipalID: current.PrincipalID, FailedAt: requiredTimestamptz(now), LockUntil: requiredTimestamptz(now.Add(15 * time.Minute))})
			if e != nil {
				return mapPostgresError("record invitation password failure", e, nil)
			}
			a.TargetVersion = row.Version
		}
	}
	err = q.AppendInvitationPasswordDenial(ctx, sqlcgen.AppendInvitationPasswordDenialParams{ID: a.ID, TargetID: a.TargetID, Version: a.TargetVersion, RequestID: a.RequestID, CorrelationID: a.CorrelationID, DecisionID: a.DecisionID, Now: requiredTimestamptz(a.OccurredAt), CallerID: requiredPGUUID(a.DirectCaller.Identity.PrincipalID), BindingID: requiredPGUUID(a.DirectCaller.Identity.BindingID), BindingVersion: optionalPositiveInt64(a.DirectCaller.Identity.BindingVersion), GrantVersion: optionalPositiveInt64(a.DirectCaller.GrantVersion)})
	if err != nil {
		return mapPostgresError("append invitation password denial Audit", err, biz.ErrAuditConflict)
	}
	return mapPostgresError("commit invitation authentication denial", tx.Commit(ctx), nil)
}
