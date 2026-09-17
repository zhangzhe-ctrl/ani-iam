package data

import (
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

type identityNotificationOutbox struct {
	data   *Data
	locale string
}

func NewIdentityNotificationOutbox(d *Data, locale string) (biz.IdentityNotificationOutbox, error) {
	if d == nil || d.pool == nil || d.outbox == nil || (locale != "en-US" && locale != "zh-CN") {
		return nil, biz.ErrAuthenticationDependency
	}
	return &identityNotificationOutbox{data: d, locale: locale}, nil
}

type identityNotificationStorageClaim struct {
	id, source, tenant  uuid.UUID
	generation, version int64
	attempts            int32
	created             pgtype.Timestamptz
	key                 pgtype.Text
	ciphertext          []byte
}

func (o *identityNotificationOutbox) ClaimIdentityNotification(ctx context.Context, kind biz.IdentityNotificationKind, now time.Time, lease time.Duration) (biz.IdentityNotificationClaim, bool, error) {
	var c biz.IdentityNotificationClaim
	var row identityNotificationStorageClaim
	if now.IsZero() || lease <= 0 {
		return c, false, biz.ErrInvalidPersistenceState
	}
	q := sqlcgen.New(o.data.pool)
	switch kind {
	case biz.IdentityNotificationTenantInvitation:
		tenants, err := q.DiscoverIdentityNotificationTenants(ctx, sqlcgen.DiscoverIdentityNotificationTenantsParams{Now: requiredTimestamptz(now), LeaseExpiredBefore: requiredTimestamptz(now.Add(-lease))})
		if err != nil {
			return c, false, mapPostgresError("discover invitation delivery boundaries", err, nil)
		}
		found := false
		for _, tenant := range tenants {
			r, e := q.ClaimTenantInvitationNotification(ctx, sqlcgen.ClaimTenantInvitationNotificationParams{TenantID: tenant, Now: requiredTimestamptz(now), LeaseExpiredBefore: requiredTimestamptz(now.Add(-lease))})
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				return c, false, mapPostgresError("claim exact Tenant invitation delivery", e, nil)
			}
			if r.TenantID != tenant {
				return c, false, biz.ErrInvalidPersistenceState
			}
			row = identityNotificationStorageClaim{id: r.ID, source: r.InvitationID, tenant: r.TenantID, generation: r.DeliveryGeneration, version: r.Version, attempts: r.AttemptCount, created: r.CreatedAt, key: r.PayloadKeyVersion, ciphertext: r.PayloadCiphertext}
			found = true
			break
		}
		if !found {
			return c, false, nil
		}
	case biz.IdentityNotificationPlatformInvitation:
		r, e := q.ClaimPlatformInvitationNotification(ctx, sqlcgen.ClaimPlatformInvitationNotificationParams{Now: requiredTimestamptz(now), LeaseExpiredBefore: requiredTimestamptz(now.Add(-lease))})
		if errors.Is(e, pgx.ErrNoRows) {
			return c, false, nil
		}
		if e != nil {
			return c, false, mapPostgresError("claim Platform invitation delivery", e, nil)
		}
		row = identityNotificationStorageClaim{id: r.ID, source: r.InvitationID, generation: r.DeliveryGeneration, version: r.Version, attempts: r.AttemptCount, created: r.CreatedAt, key: r.PayloadKeyVersion, ciphertext: r.PayloadCiphertext}
	case biz.IdentityNotificationEmailVerification:
		r, e := q.ClaimAccountVerificationNotification(ctx, sqlcgen.ClaimAccountVerificationNotificationParams{Now: requiredTimestamptz(now), LeaseExpiredBefore: requiredTimestamptz(now.Add(-lease))})
		if errors.Is(e, pgx.ErrNoRows) {
			return c, false, nil
		}
		if e != nil {
			return c, false, mapPostgresError("claim independent email verification delivery", e, nil)
		}
		row = identityNotificationStorageClaim{id: r.ID, source: r.ChallengeID, generation: 1, version: r.Version, attempts: r.AttemptCount, created: r.CreatedAt, key: r.PayloadKeyVersion, ciphertext: r.PayloadCiphertext}
	default:
		return c, false, biz.ErrInvalidPersistenceState
	}
	c = biz.IdentityNotificationClaim{Kind: kind, TenantID: row.tenant, ID: row.id, SourceID: row.source, Generation: row.generation, Version: row.version, AttemptCount: int(row.attempts), OccurredAt: row.created.Time.UTC(), Locale: o.locale}
	if !row.created.Valid || !row.key.Valid {
		return c, true, nil
	}
	switch kind {
	case biz.IdentityNotificationTenantInvitation:
		parent, err := q.GetTenantInvitation(ctx, sqlcgen.GetTenantInvitationParams{TenantID: row.tenant, ID: row.source})
		if err != nil {
			return c, true, mapPostgresError("read delivery Tenant invitation", err, nil)
		}
		c.ExpiresAt = parent.ExpiresAt.Time.UTC()
		c.Locale = parent.Locale
		payload, err := o.data.outbox.openTenantInvitation(row.key.String, row.tenant, row.source, row.id, row.generation, row.ciphertext)
		if err == nil {
			c.Email, c.Secret = payload.Email, payload.Token
			digest := sha256.Sum256([]byte(payload.Token))
			c.PayloadValid = payload.Email == parent.NormalizedEmail && subtle.ConstantTimeCompare(digest[:], parent.TokenDigest) == 1
		}
	case biz.IdentityNotificationPlatformInvitation:
		parent, err := q.GetPlatformInvitation(ctx, sqlcgen.GetPlatformInvitationParams{ID: row.source})
		if err != nil {
			return c, true, mapPostgresError("read delivery Platform invitation", err, nil)
		}
		c.ExpiresAt = parent.ExpiresAt.Time.UTC()
		c.Locale = parent.Locale
		payload, err := o.data.outbox.openPlatformInvitation(row.key.String, row.source, row.id, row.generation, row.ciphertext)
		if err == nil {
			c.Email, c.Secret = payload.Email, payload.Token
			digest := sha256.Sum256([]byte(payload.Token))
			c.PayloadValid = payload.Email == parent.NormalizedEmail && subtle.ConstantTimeCompare(digest[:], parent.TokenDigest) == 1
		}
	case biz.IdentityNotificationEmailVerification:
		parent, err := q.GetInvitedAccountVerification(ctx, sqlcgen.GetInvitedAccountVerificationParams{ID: row.source})
		if err != nil {
			return c, true, mapPostgresError("read independent code delivery intent", err, nil)
		}
		c.ExpiresAt = parent.ExpiresAt.Time.UTC()
		payload, err := o.data.outbox.openInvitedAccount(row.key.String, row.source, row.id, row.ciphertext)
		if err == nil {
			c.Email, c.Secret = payload.Email, payload.Code
			digest, e := o.data.outbox.verificationDigest(parent.CodeKeyVersion.String, row.source, payload.Email, payload.Code, c.ExpiresAt)
			c.PayloadValid = e == nil && parent.NormalizedEmail.Valid && payload.Email == parent.NormalizedEmail.String && payload.ExpiresAt.Equal(c.ExpiresAt) && subtle.ConstantTimeCompare(digest, parent.CodeDigest) == 1
		}
	}
	return c, true, nil
}
func identityNotificationCurrent(v any) (bool, error) {
	switch b := v.(type) {
	case bool:
		return b, nil
	case pgtype.Bool:
		if b.Valid {
			return b.Bool, nil
		}
	}
	return false, biz.ErrInvalidPersistenceState
}
func (o *identityNotificationOutbox) CurrentIdentityNotification(ctx context.Context, c biz.IdentityNotificationClaim, now time.Time) (bool, error) {
	q := sqlcgen.New(o.data.pool)
	var value any
	var err error
	switch c.Kind {
	case biz.IdentityNotificationTenantInvitation:
		if c.TenantID.Version() != 7 {
			return false, biz.ErrInvalidPersistenceState
		}
		value, err = q.CurrentTenantInvitationNotification(ctx, sqlcgen.CurrentTenantInvitationNotificationParams{TenantID: c.TenantID, ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Generation: c.Generation, Now: requiredTimestamptz(now)})
	case biz.IdentityNotificationPlatformInvitation:
		if c.TenantID != uuid.Nil {
			return false, biz.ErrInvalidPersistenceState
		}
		value, err = q.CurrentPlatformInvitationNotification(ctx, sqlcgen.CurrentPlatformInvitationNotificationParams{ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Generation: c.Generation, Now: requiredTimestamptz(now)})
	case biz.IdentityNotificationEmailVerification:
		if c.TenantID != uuid.Nil || c.Generation != 1 {
			return false, biz.ErrInvalidPersistenceState
		}
		value, err = q.CurrentAccountVerificationNotification(ctx, sqlcgen.CurrentAccountVerificationNotificationParams{ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Now: requiredTimestamptz(now)})
	default:
		return false, biz.ErrInvalidPersistenceState
	}
	if err != nil {
		return false, mapPostgresError("recheck current identity delivery", err, nil)
	}
	return identityNotificationCurrent(value)
}
func (o *identityNotificationOutbox) FinishIdentityNotification(ctx context.Context, c biz.IdentityNotificationClaim, outcome biz.IdentityNotificationOutcome, receipt string, next, now time.Time) error {
	if c.ID.Version() != 7 || c.SourceID.Version() != 7 || c.Version < 2 || now.IsZero() || next.IsZero() {
		return biz.ErrInvalidPersistenceState
	}
	if outcome == biz.IdentityNotificationDelivered {
		id, err := uuid.Parse(receipt)
		if err != nil || id == uuid.Nil || id.String() != receipt {
			return biz.ErrInvalidPersistenceState
		}
	} else if receipt != "" || (outcome != biz.IdentityNotificationRetry && outcome != biz.IdentityNotificationCancelled && outcome != biz.IdentityNotificationAttention) {
		return biz.ErrInvalidPersistenceState
	}
	if outcome == biz.IdentityNotificationRetry && next.Before(now) {
		return biz.ErrInvalidPersistenceState
	}
	q := sqlcgen.New(o.data.pool)
	var n int64
	var err error
	switch c.Kind {
	case biz.IdentityNotificationTenantInvitation:
		if c.TenantID.Version() != 7 {
			return biz.ErrInvalidPersistenceState
		}
		n, err = q.FinishTenantInvitationNotification(ctx, sqlcgen.FinishTenantInvitationNotificationParams{TenantID: c.TenantID, ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Outcome: string(outcome), NotificationID: receipt, NextAttempt: requiredTimestamptz(next), Now: requiredTimestamptz(now)})
	case biz.IdentityNotificationPlatformInvitation:
		if c.TenantID != uuid.Nil {
			return biz.ErrInvalidPersistenceState
		}
		n, err = q.FinishPlatformInvitationNotification(ctx, sqlcgen.FinishPlatformInvitationNotificationParams{ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Outcome: string(outcome), NotificationID: receipt, NextAttempt: requiredTimestamptz(next), Now: requiredTimestamptz(now)})
	case biz.IdentityNotificationEmailVerification:
		if c.TenantID != uuid.Nil || c.Generation != 1 {
			return biz.ErrInvalidPersistenceState
		}
		n, err = q.FinishAccountVerificationNotification(ctx, sqlcgen.FinishAccountVerificationNotificationParams{ID: c.ID, SourceID: c.SourceID, ExpectedVersion: c.Version, Outcome: string(outcome), NotificationID: receipt, NextAttempt: requiredTimestamptz(next), Now: requiredTimestamptz(now)})
	default:
		return biz.ErrInvalidPersistenceState
	}
	if err != nil {
		return mapPostgresError("persist identity delivery transition", err, nil)
	}
	if n != 1 {
		return biz.ErrIdentityNotificationSuperseded
	}
	return nil
}
