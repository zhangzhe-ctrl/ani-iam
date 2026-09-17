package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

type invitedAccountUnitOfWork struct{ data *Data }

func NewInvitedAccountUnitOfWork(d *Data) biz.InvitedAccountUnitOfWork {
	return &invitedAccountUnitOfWork{data: d}
}

var errInvitedAccountDeniedCommit = errors.New("commit independently audited verification denial")

func (u *invitedAccountUnitOfWork) within(ctx context.Context, email string, fn func(*sqlcgen.Queries, []uuid.UUID) error) error {
	if u == nil || u.data == nil || u.data.pool == nil || u.data.outbox == nil {
		return biz.ErrAuthenticationDependency
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin invited account", err, nil)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return mapPostgresError("lock global Human creation", err, nil)
	}
	tenants, err := q.InvitedAccountTenantBoundaries(ctx, sqlcgen.InvitedAccountTenantBoundariesParams{Email: email})
	if err != nil {
		return mapPostgresError("read invited account boundaries", err, nil)
	}
	// Global first, then sorted exact Tenant guards. Only these locked boundaries
	// can establish final eligibility; a newly appearing unlocked invitation is
	// deliberately left for a later request.
	for _, id := range tenants {
		if err = q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: id}); err != nil {
			return mapPostgresError("lock invited account Tenant", err, nil)
		}
	}
	err = fn(q, tenants)
	if err != nil && !errors.Is(err, errInvitedAccountDeniedCommit) {
		return err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return mapPostgresError("commit invited account", commitErr, nil)
	}
	if errors.Is(err, errInvitedAccountDeniedCommit) {
		return biz.ErrInvalidCredential
	}
	return nil
}
func appendInvitedAccountAudit(ctx context.Context, q *sqlcgen.Queries, a biz.SecurityAuditEvent) error {
	if a.Boundary != biz.AuditBoundaryPrincipal || a.TargetType != "invited_account_verification" || a.DirectCaller.Identity.PrincipalID == uuid.Nil {
		return biz.ErrInvalidPersistenceState
	}
	return mapPostgresError("append independent account verification Audit", q.AppendInvitedAccountAudit(ctx, sqlcgen.AppendInvitedAccountAuditParams{ID: a.ID, ActorID: optionalPGUUID(a.ActorID), AuthenticationMethod: string(a.AuthenticationMethod), Action: string(a.Action), ChallengeID: a.TargetID, Version: a.TargetVersion, Result: string(a.Result), Reason: string(a.Reason), RequestID: a.RequestID, CorrelationID: a.CorrelationID, DecisionID: a.DecisionID, Now: requiredTimestamptz(a.OccurredAt), CallerID: requiredPGUUID(a.DirectCaller.Identity.PrincipalID), BindingID: requiredPGUUID(a.DirectCaller.Identity.BindingID), BindingVersion: optionalPositiveInt64(a.DirectCaller.Identity.BindingVersion), GrantVersion: optionalPositiveInt64(a.DirectCaller.GrantVersion)}), biz.ErrAuditConflict)
}
func (u *invitedAccountUnitOfWork) RequestInvitedAccountVerification(ctx context.Context, m biz.InvitedAccountVerificationMutation) (biz.InvitedAccountVerificationResult, error) {
	var result biz.InvitedAccountVerificationResult
	err := u.within(ctx, m.NormalizedEmail, func(q *sqlcgen.Queries, tenants []uuid.UUID) error {
		old, err := q.FindInvitedAccountRequest(ctx, sqlcgen.FindInvitedAccountRequestParams{Key: m.IdempotencyKey})
		if err == nil {
			if !bytes.Equal(old.AccountDigest, m.AccountDigest[:]) || old.CallerPrincipalID != m.Caller.Identity.PrincipalID {
				return biz.ErrIdempotencyConflict
			}
			if !old.ExpiresAt.Valid {
				return biz.ErrInvalidPersistenceState
			}
			result = biz.InvitedAccountVerificationResult{ChallengeID: old.ID, ExpiresAt: old.ExpiresAt.Time.UTC()}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return mapPostgresError("read verification request receipt", err, nil)
		}
		eligible, err := q.InvitedAccountEligibility(ctx, sqlcgen.InvitedAccountEligibilityParams{Email: m.NormalizedEmail, TenantIds: tenants})
		if err != nil {
			return mapPostgresError("check verification eligibility", err, nil)
		}
		if !eligible.Valid {
			return biz.ErrInvalidPersistenceState
		}
		if err = q.SupersedeInvitedAccountVerifications(ctx, sqlcgen.SupersedeInvitedAccountVerificationsParams{AccountDigest: m.AccountDigest[:], Now: requiredTimestamptz(m.CreatedAt)}); err != nil {
			return mapPostgresError("supersede old verification", err, nil)
		}
		if err = q.CancelInactiveInvitedAccountDeliveries(ctx, sqlcgen.CancelInactiveInvitedAccountDeliveriesParams{AccountDigest: m.AccountDigest[:], Now: requiredTimestamptz(m.CreatedAt)}); err != nil {
			return mapPostgresError("cancel old verification delivery", err, nil)
		}
		var email, key pgtype.Text
		var digest, encrypted []byte
		if eligible.Bool {
			version, ciphertext, e := u.data.outbox.sealInvitedAccount(m.ChallengeID, m.DeliveryID, invitedAccountDeliveryPayload{Email: m.NormalizedEmail, Code: m.Code, ExpiresAt: m.ExpiresAt})
			if e != nil {
				return biz.ErrAuthenticationDependency
			}
			digest, e = u.data.outbox.verificationDigest(version, m.ChallengeID, m.NormalizedEmail, m.Code, m.ExpiresAt)
			if e != nil {
				return biz.ErrAuthenticationDependency
			}
			email = pgtype.Text{String: m.NormalizedEmail, Valid: true}
			key = pgtype.Text{String: version, Valid: true}
			encrypted = ciphertext
		}
		if err = q.CreateInvitedAccountVerification(ctx, sqlcgen.CreateInvitedAccountVerificationParams{ID: m.ChallengeID, AccountDigest: m.AccountDigest[:], Email: email, KeyVersion: key, CodeDigest: digest, CallerPrincipalID: m.Caller.Identity.PrincipalID, Key: m.IdempotencyKey, Now: requiredTimestamptz(m.CreatedAt), ExpiresAt: requiredTimestamptz(m.ExpiresAt)}); err != nil {
			return mapPostgresError("create independent verification intent", err, nil)
		}
		if eligible.Bool {
			if err = q.CreateInvitedAccountDelivery(ctx, sqlcgen.CreateInvitedAccountDeliveryParams{ID: m.DeliveryID, ChallengeID: m.ChallengeID, KeyVersion: key, Ciphertext: encrypted, Now: requiredTimestamptz(m.CreatedAt)}); err != nil {
				return mapPostgresError("create verification delivery", err, nil)
			}
		}
		if err = appendInvitedAccountAudit(ctx, q, m.Audit); err != nil {
			return err
		}
		result = biz.InvitedAccountVerificationResult{ChallengeID: m.ChallengeID, ExpiresAt: m.ExpiresAt}
		return nil
	})
	return result, err
}
func (u *invitedAccountUnitOfWork) CompleteInvitedAccount(ctx context.Context, m biz.InvitedAccountCompletionMutation) error {
	return u.within(ctx, m.NormalizedEmail, func(q *sqlcgen.Queries, tenants []uuid.UUID) error {
		row, err := q.GetInvitedAccountVerification(ctx, sqlcgen.GetInvitedAccountVerificationParams{ID: m.ChallengeID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return mapPostgresError("read verification challenge", err, nil)
		}
		found := err == nil
		now := time.Now().UTC()
		denied := func(increment bool) error {
			audit := m.DenialAudit
			audit.OccurredAt = now
			audit.RecordedAt = now
			if found {
				audit.TargetVersion = row.Version
			}
			if found && row.Status == "pending" {
				var n int64
				var e error
				if !now.Before(row.ExpiresAt.Time) {
					n, e = q.ExpireInvitedAccountVerification(ctx, sqlcgen.ExpireInvitedAccountVerificationParams{ID: row.ID, ExpectedVersion: row.Version, Now: requiredTimestamptz(now)})
				} else if increment {
					n, e = q.FailInvitedAccountVerification(ctx, sqlcgen.FailInvitedAccountVerificationParams{ID: row.ID, ExpectedVersion: row.Version, Now: requiredTimestamptz(now)})
				}
				if e != nil {
					return mapPostgresError("record code verification failure", e, nil)
				}
				if n == 1 {
					audit.TargetVersion++
				}
				if e = q.CancelInactiveInvitedAccountDeliveries(ctx, sqlcgen.CancelInactiveInvitedAccountDeliveriesParams{AccountDigest: row.AccountDigest, Now: requiredTimestamptz(now)}); e != nil {
					return mapPostgresError("cancel exhausted verification delivery", e, nil)
				}
			}
			if e := appendInvitedAccountAudit(ctx, q, audit); e != nil {
				return e
			}
			return errInvitedAccountDeniedCommit
		}
		if !found {
			return denied(false)
		}
		if !row.ExpiresAt.Valid {
			return biz.ErrInvalidPersistenceState
		}
		sum := sha256.Sum256([]byte(m.NormalizedEmail))
		if !bytes.Equal(row.AccountDigest, sum[:]) || row.CallerPrincipalID != m.Caller.Identity.PrincipalID || !row.NormalizedEmail.Valid || row.NormalizedEmail.String != m.NormalizedEmail || !row.CodeKeyVersion.Valid || !now.Before(row.ExpiresAt.Time) || row.FailedAttempts >= biz.InvitedAccountVerificationAttempts {
			return denied(true)
		}
		code, err := u.data.outbox.verificationDigest(row.CodeKeyVersion.String, row.ID, m.NormalizedEmail, m.Code, row.ExpiresAt.Time)
		if err != nil {
			return biz.ErrAuthenticationDependency
		}
		if subtle.ConstantTimeCompare(code, row.CodeDigest) != 1 {
			return denied(true)
		}
		intent, err := u.data.outbox.completionDigest(row.CodeKeyVersion.String, row.ID, m.Intent)
		if err != nil {
			return biz.ErrAuthenticationDependency
		}
		if row.Status == "consumed" {
			if !row.CompletionKey.Valid || row.CompletionKey.String != m.IdempotencyKey {
				return denied(false)
			}
			if subtle.ConstantTimeCompare(intent, row.CompletionIntent) != 1 {
				return biz.ErrIdempotencyConflict
			}
			return nil
		}
		if row.Status != "pending" {
			return denied(false)
		}
		eligible, err := q.InvitedAccountEligibility(ctx, sqlcgen.InvitedAccountEligibilityParams{Email: m.NormalizedEmail, TenantIds: tenants})
		if err != nil {
			return mapPostgresError("recheck signup eligibility", err, nil)
		}
		if !eligible.Valid {
			return biz.ErrInvalidPersistenceState
		}
		if !eligible.Bool {
			return denied(true)
		}
		if err = q.CreateInvitedHuman(ctx, sqlcgen.CreateInvitedHumanParams{ID: m.PrincipalID, Now: requiredTimestamptz(now)}); err != nil {
			return mapPostgresError("create independently verified Human", err, nil)
		}
		if err = q.CreateInvitedVerifiedEmail(ctx, sqlcgen.CreateInvitedVerifiedEmailParams{PrincipalID: m.PrincipalID, Email: m.NormalizedEmail, Now: requiredTimestamptz(now)}); err != nil {
			return mapPostgresError("verify invited Human email", err, nil)
		}
		if err = q.CreatePasswordIdentity(ctx, sqlcgen.CreatePasswordIdentityParams{ID: m.IdentityID, PrincipalID: m.PrincipalID, Subject: m.NormalizedEmail, CreatedAt: requiredTimestamptz(now)}); err != nil {
			return mapPostgresError("create invited password Identity", err, nil)
		}
		if _, err = q.CreatePasswordCredential(ctx, sqlcgen.CreatePasswordCredentialParams{PrincipalID: m.PrincipalID, IdentityID: m.IdentityID, PasswordHash: m.PasswordHash, CreatedAt: requiredTimestamptz(now)}); err != nil {
			return mapPostgresError("create invited password credential", err, nil)
		}
		n, err := q.ConsumeInvitedAccountVerification(ctx, sqlcgen.ConsumeInvitedAccountVerificationParams{ID: row.ID, ExpectedVersion: row.Version, PrincipalID: requiredPGUUID(m.PrincipalID), Key: pgtype.Text{String: m.IdempotencyKey, Valid: true}, Intent: intent, Now: requiredTimestamptz(now)})
		if err != nil {
			return mapPostgresError("consume independent email verification", err, biz.ErrIdempotencyConflict)
		}
		if n != 1 {
			return biz.ErrInvalidCredential
		}
		if err = q.CancelInactiveInvitedAccountDeliveries(ctx, sqlcgen.CancelInactiveInvitedAccountDeliveriesParams{AccountDigest: row.AccountDigest, Now: requiredTimestamptz(now)}); err != nil {
			return mapPostgresError("cancel consumed code delivery", err, nil)
		}
		audit := m.Audit
		audit.TargetVersion = row.Version + 1
		audit.OccurredAt = now
		audit.RecordedAt = now
		return appendInvitedAccountAudit(ctx, q, audit)
	})
}
