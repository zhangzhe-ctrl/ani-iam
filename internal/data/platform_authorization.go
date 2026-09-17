package data

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type platformAuthorizationReader struct{ data *Data }

func NewPlatformAuthorizationReader(d *Data) biz.PlatformAuthorizationReader {
	return &platformAuthorizationReader{data: d}
}
func (r *platformAuthorizationReader) LookupPlatformAuthorization(ctx context.Context, c biz.AccessTokenClaims, resource string, actions []string) (biz.PlatformAuthorizationState, error) {
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.PlatformAuthorizationState{}, biz.ErrPersistenceUnavailable
	}
	return lookupPlatformAuthorization(ctx, sqlcgen.New(r.data.pool), c, resource, actions)
}

func lookupPlatformAuthorization(ctx context.Context, q *sqlcgen.Queries, c biz.AccessTokenClaims, resource string, actions []string) (biz.PlatformAuthorizationState, error) {
	s, err := q.LookupPlatformAuthorization(ctx, sqlcgen.LookupPlatformAuthorizationParams{PrincipalID: c.Subject, SessionID: c.SessionID, GrantID: c.GrantID, Resource: resource, Actions: actions})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformAuthorizationState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.PlatformAuthorizationState{}, platformLoginPersistenceError(err)
	}
	return biz.PlatformAuthorizationState{PrincipalStatus: biz.PrincipalStatus(s.PrincipalStatus), MembershipID: s.MembershipID, MembershipStatus: biz.MembershipStatus(s.MembershipStatus), SessionStatus: biz.SessionStatus(s.SessionStatus), IdleExpiresAt: s.IdleExpiresAt.Time, AbsoluteExpiresAt: s.AbsoluteExpiresAt.Time, ReauthenticatedAt: s.ReauthenticatedAt.Time, GrantStatus: biz.GrantStatus(s.GrantStatus), GrantVersion: s.GrantVersion, PermissionAllowed: s.PermissionAllowed}, nil
}
func (r *platformAuthorizationReader) RecordPlatformAuthorization(ctx context.Context, a biz.SecurityAuditEvent) error {
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.ErrPersistenceUnavailable
	}
	return appendPlatformAudit(ctx, sqlcgen.New(r.data.pool), a)
}
