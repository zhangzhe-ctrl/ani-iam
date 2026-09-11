package data

import (
	"context"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (r *postgresPasswordLoginReader) ListOwnedSessions(ctx context.Context, principal, after uuid.UUID, limit int) ([]biz.Session, error) {
	rows, err := sqlcgen.New(r.data.pool).ListOwnedHumanSessions(ctx, sqlcgen.ListOwnedHumanSessionsParams{PrincipalID: principal, AfterID: after, RowLimit: int32(limit)})
	if err != nil {
		return nil, mapPostgresError("list owned human sessions", err, nil)
	}
	result := make([]biz.Session, 0, len(rows))
	for _, s := range rows {
		methods := make([]biz.AuditAuthenticationMethod, 0, len(s.AuthnMethods))
		for _, m := range s.AuthnMethods {
			methods = append(methods, biz.AuditAuthenticationMethod(m))
		}
		result = append(result, biz.Session{ID: s.ID, PrincipalID: s.PrincipalID, Audience: biz.Audience(s.Audience), Status: biz.SessionStatus(s.Status), Version: s.Version, AuthnMethods: methods, DeviceName: s.DeviceName, IdleExpiresAt: s.IdleExpiresAt.Time, AbsoluteExpiry: s.AbsoluteExpiresAt.Time, ReauthenticatedAt: s.ReauthenticatedAt.Time, CreatedAt: s.CreatedAt.Time, UpdatedAt: s.UpdatedAt.Time})
	}
	return result, nil
}
func (r *postgresPasswordLoginReader) ListOwnedSessionGrants(ctx context.Context, scope biz.TenantScope, principal, session uuid.UUID) ([]biz.SessionGrant, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return nil, err
	}
	rows, err := sqlcgen.New(r.data.pool).ListOwnedSessionGrants(ctx, sqlcgen.ListOwnedSessionGrantsParams{TenantID: tenant, PrincipalID: principal, SessionID: session})
	if err != nil {
		return nil, mapPostgresError("list owned session grants", err, nil)
	}
	result := make([]biz.SessionGrant, 0, len(rows))
	for _, g := range rows {
		result = append(result, biz.SessionGrant{ID: g.ID, SessionID: g.SessionID, MembershipID: g.MembershipID, Status: biz.GrantStatus(g.Status), Version: g.Version, CreatedAt: g.CreatedAt.Time, UpdatedAt: g.UpdatedAt.Time})
	}
	return result, nil
}

var _ biz.SessionListReader = (*postgresPasswordLoginReader)(nil)
