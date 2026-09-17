package data

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type platformSessionUnitOfWork struct{ data *Data }
type platformSessionTransaction struct{ q *sqlcgen.Queries }

func NewPlatformSessionUnitOfWork(d *Data) biz.PlatformSessionUnitOfWork {
	return &platformSessionUnitOfWork{data: d}
}

func (u *platformSessionUnitOfWork) WithinPlatformSession(ctx context.Context, digest [sha256.Size]byte, fn func(biz.PlatformSessionTransaction, biz.PlatformSessionState, bool) error) error {
	if u == nil || u.data == nil || u.data.pool == nil || fn == nil {
		return biz.ErrPersistenceUnavailable
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return platformLoginPersistenceError(err)
	}
	r, err := q.LockPlatformRefreshSession(ctx, sqlcgen.LockPlatformRefreshSessionParams{Digest: digest[:]})
	found := !errors.Is(err, pgx.ErrNoRows)
	if err != nil && found {
		return platformLoginPersistenceError(err)
	}
	var s biz.PlatformSessionState
	if found {
		p, m, session, g, f, token := r.Principal, r.PlatformMembership, r.Session, r.PlatformSessionGrant, r.PlatformRefreshTokenFamily, r.PlatformRefreshToken
		if len(token.Digest) != sha256.Size {
			return biz.ErrInvalidPersistenceState
		}
		s.Principal = biz.Principal{ID: p.ID, Status: biz.PrincipalStatus(p.Status)}
		s.MembershipID = m.ID
		s.MembershipStatus = biz.MembershipStatus(m.Status)
		methods := make([]biz.AuditAuthenticationMethod, len(session.AuthnMethods))
		for i, a := range session.AuthnMethods {
			methods[i] = biz.AuditAuthenticationMethod(a)
		}
		s.Session = biz.Session{ID: session.ID, PrincipalID: session.PrincipalID, Audience: biz.Audience(session.Audience), Status: biz.SessionStatus(session.Status), Version: session.Version, AuthnMethods: methods, DeviceName: session.DeviceName, IdleExpiresAt: session.IdleExpiresAt.Time, AbsoluteExpiry: session.AbsoluteExpiresAt.Time, ReauthenticatedAt: session.ReauthenticatedAt.Time, CreatedAt: session.CreatedAt.Time, UpdatedAt: session.UpdatedAt.Time}
		s.Grant = biz.SessionGrant{ID: g.ID, SessionID: g.SessionID, MembershipID: g.MembershipID, Status: biz.GrantStatus(g.Status), Version: g.Version, CreatedAt: g.CreatedAt.Time, UpdatedAt: g.UpdatedAt.Time}
		s.Family = biz.RefreshTokenFamily{ID: f.ID, GrantID: f.GrantID, Status: biz.GrantStatus(f.Status), Version: f.Version, CreatedAt: f.CreatedAt.Time, UpdatedAt: f.UpdatedAt.Time}
		s.Token = biz.RefreshToken{ID: token.ID, FamilyID: token.FamilyID, Status: biz.RefreshTokenStatus(token.Status), IssuedAt: token.IssuedAt.Time, ExpiresAt: token.ExpiresAt.Time, ConsumedAt: token.ConsumedAt.Time}
		copy(s.Token.Digest[:], token.Digest)
	}
	if err = fn(&platformSessionTransaction{q: q}, s, found); err != nil {
		return err
	}
	return platformLoginPersistenceError(tx.Commit(ctx))
}

func (t *platformSessionTransaction) Rotate(ctx context.Context, s biz.PlatformSessionState, r biz.RefreshToken, idle, now time.Time) (int64, error) {
	count, err := t.q.ConsumePlatformRefreshToken(ctx, sqlcgen.ConsumePlatformRefreshTokenParams{ID: s.Token.ID, FamilyID: s.Family.ID, Now: requiredTimestamptz(now), ReplacementID: optionalPGUUID(r.ID)})
	if err != nil {
		return 0, platformLoginPersistenceError(err)
	}
	if count != 1 {
		return 0, biz.ErrInvalidPersistenceState
	}
	if err = t.q.CreatePlatformRefreshToken(ctx, sqlcgen.CreatePlatformRefreshTokenParams{ID: r.ID, FamilyID: r.FamilyID, Digest: r.Digest[:], IssuedAt: requiredTimestamptz(r.IssuedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)}); err != nil {
		return 0, platformLoginPersistenceError(err)
	}
	version, err := t.q.TouchPlatformSession(ctx, sqlcgen.TouchPlatformSessionParams{ID: s.Session.ID, PrincipalID: s.Principal.ID, IdleExpiresAt: requiredTimestamptz(idle), Now: requiredTimestamptz(now)})
	return version, platformLoginPersistenceError(err)
}
func (t *platformSessionTransaction) RevokeFamily(ctx context.Context, s biz.PlatformSessionState, now time.Time) error {
	count, err := t.q.RevokePlatformRefreshFamily(ctx, sqlcgen.RevokePlatformRefreshFamilyParams{ID: s.Family.ID, GrantID: s.Grant.ID, Now: requiredTimestamptz(now)})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	if count != 1 {
		return biz.ErrInvalidPersistenceState
	}
	if err = t.q.RevokePlatformFamilyTokens(ctx, sqlcgen.RevokePlatformFamilyTokensParams{FamilyID: s.Family.ID}); err != nil {
		return platformLoginPersistenceError(err)
	}
	count, err = t.q.InvalidatePlatformGrantVersion(ctx, sqlcgen.InvalidatePlatformGrantVersionParams{ID: s.Grant.ID, SessionID: s.Session.ID, Now: requiredTimestamptz(now)})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	if count != 1 {
		return biz.ErrInvalidPersistenceState
	}
	return nil
}
func (t *platformSessionTransaction) Logout(ctx context.Context, s biz.PlatformSessionState, now time.Time) error {
	count, err := t.q.RevokePlatformSession(ctx, sqlcgen.RevokePlatformSessionParams{ID: s.Session.ID, PrincipalID: s.Principal.ID, Now: requiredTimestamptz(now)})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	if count != 1 {
		return biz.ErrInvalidPersistenceState
	}
	if err = t.q.RevokePlatformSessionGrants(ctx, sqlcgen.RevokePlatformSessionGrantsParams{SessionID: s.Session.ID, PrincipalID: s.Principal.ID, Now: requiredTimestamptz(now)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err = t.q.RevokePlatformSessionFamilies(ctx, sqlcgen.RevokePlatformSessionFamiliesParams{SessionID: s.Session.ID, PrincipalID: s.Principal.ID, Now: requiredTimestamptz(now)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	return platformLoginPersistenceError(t.q.RevokePlatformSessionTokens(ctx, sqlcgen.RevokePlatformSessionTokensParams{SessionID: s.Session.ID, PrincipalID: s.Principal.ID}))
}
func (t *platformSessionTransaction) AppendAudit(ctx context.Context, a biz.SecurityAuditEvent) error {
	return appendPlatformAudit(ctx, t.q, a)
}
