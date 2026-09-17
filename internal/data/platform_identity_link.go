package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

type platformIdentityLinkUnitOfWork struct{ data *Data }
type platformIdentityLinkTransaction struct{ q *sqlcgen.Queries }

func NewPlatformIdentityLinkUnitOfWork(d *Data) biz.PlatformIdentityLinkUnitOfWork {
	return &platformIdentityLinkUnitOfWork{d}
}
func (u *platformIdentityLinkUnitOfWork) WithinPlatformIdentityLink(ctx context.Context, fn func(biz.PlatformIdentityLinkTransaction) error) error {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
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
	if err = fn(&platformIdentityLinkTransaction{q}); err != nil {
		return err
	}
	return platformLoginPersistenceError(tx.Commit(ctx))
}
func (t *platformIdentityLinkTransaction) Authentication(ctx context.Context, c biz.AccessTokenClaims) (biz.PlatformAuthorizationState, error) {
	_, err := t.q.LockPlatformIdentityLinkAuthentication(ctx, sqlcgen.LockPlatformIdentityLinkAuthenticationParams{PrincipalID: c.Subject, SessionID: c.SessionID, GrantID: c.GrantID, GrantVersion: c.GrantVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformAuthorizationState{}, biz.ErrOIDCReauthenticationRequired
	}
	if err != nil {
		return biz.PlatformAuthorizationState{}, platformLoginPersistenceError(err)
	}
	return lookupPlatformAuthorization(ctx, t.q, c, "", nil)
}
func (t *platformIdentityLinkTransaction) VerifiedEmailOwner(ctx context.Context, email string) (uuid.UUID, error) {
	id, err := t.q.LookupVerifiedEmailOwner(ctx, sqlcgen.LookupVerifiedEmailOwnerParams{NormalizedEmail: email})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, biz.ErrOIDCEmailConflict
	}
	return id, platformLoginPersistenceError(err)
}
func (t *platformIdentityLinkTransaction) IdentityExists(ctx context.Context, issuer, subject string) (bool, error) {
	_, err := t.q.LookupOIDCIdentityOwner(ctx, sqlcgen.LookupOIDCIdentityOwnerParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, platformLoginPersistenceError(err)
}
func (t *platformIdentityLinkTransaction) CreateIdentity(ctx context.Context, m biz.OIDCIdentityLinkMutation) error {
	err := t.q.CreateOIDCIdentity(ctx, sqlcgen.CreateOIDCIdentityParams{ID: m.IdentityID, PrincipalID: m.PrincipalID, Provider: m.Provider, Issuer: m.Issuer, Subject: m.Subject, CreatedAt: requiredTimestamptz(m.LinkedAt), UpdatedAt: requiredTimestamptz(m.LinkedAt)})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return biz.ErrOIDCIdentityConflict
	}
	return platformLoginPersistenceError(err)
}
func (t *platformIdentityLinkTransaction) AppendAudit(ctx context.Context, a biz.SecurityAuditEvent) error {
	return appendPlatformAudit(ctx, t.q, a)
}
