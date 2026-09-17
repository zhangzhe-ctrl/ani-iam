package data

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

func (t *invitationAcceptanceTransaction) bootstrap(ctx context.Context, cap biz.InvitationAcceptanceCapability, policy biz.TenantAdminLoginPolicy) (*biz.InvitationBootstrapState, error) {
	c, target, err := t.binding(cap)
	if err != nil {
		return nil, err
	}
	row, err := t.q.GetInvitationBootstrapOperation(ctx, sqlcgen.GetInvitationBootstrapOperationParams{TenantID: target.TenantID, InvitationID: target.InvitationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapPostgresError("read exact Invitation Bootstrap", err, nil)
	}
	s := &biz.InvitationBootstrapState{ID: row.ID, Version: row.Version, Source: row.SourceKind, Status: row.Status, IntendedEmail: row.IntendedEmail, Superseded: row.SupersededBy.Valid}
	role, err := t.q.TenantAdminRecoveryRole(ctx, sqlcgen.TenantAdminRecoveryRoleParams{TenantID: target.TenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, mapPostgresError("read invited Bootstrap builtin", err, nil)
	}
	s.RoleID, s.RoleSystem, s.RoleDefinitionVersion = role.ID, role.SystemRole, role.SystemDefinitionVersion
	permissions, err := t.q.ListTenantAuthorizationRolePermissions(ctx, sqlcgen.ListTenantAuthorizationRolePermissionsParams{TenantID: target.TenantID, RoleID: role.ID})
	if err != nil {
		return nil, mapPostgresError("read invited Bootstrap permission definition", err, nil)
	}
	for _, p := range permissions {
		s.Permissions = append(s.Permissions, biz.Permission{Scope: biz.PermissionScopeTenant, Resource: p.Resource, Action: p.Action})
	}
	login, err := t.q.TenantAdminRecoveryLoginTarget(ctx, sqlcgen.TenantAdminRecoveryLoginTargetParams{PrincipalID: c.Subject, PasswordEnabled: policy.PasswordEnabled, OidcProvider: policy.OIDCProvider, OidcIssuer: policy.OIDCIssuer, Now: requiredTimestamptz(time.Now().UTC())})
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, mapPostgresError("read initial administrator login capability", err, nil)
	}
	s.LoginCapable = login.LoginCapable
	return s, nil
}

func (t *invitationAcceptanceTransaction) CompleteBootstrap(ctx context.Context, cap biz.InvitationAcceptanceCapability, s biz.InvitationBootstrapState, m biz.InvitationAcceptedMembership, a biz.SecurityAuditEvent, now time.Time) error {
	c, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if target.Boundary != biz.AccessBoundaryTenant || m.PrincipalID != c.Subject || a.ActorID != c.Subject || a.Boundary != biz.AuditBoundaryTenant || a.TargetID != s.ID || a.TargetVersion != s.Version+1 || a.Action != "iam.bootstrap.completed" {
		return biz.ErrInvalidPersistenceState
	}
	n, err := t.q.CompleteInvitationBootstrap(ctx, sqlcgen.CompleteInvitationBootstrapParams{TenantID: target.TenantID, ID: s.ID, ExpectedVersion: s.Version, InvitationID: target.InvitationID, MembershipID: requiredPGUUID(m.ID), PrincipalID: requiredPGUUID(c.Subject), Now: requiredTimestamptz(now)})
	if err != nil {
		return mapPostgresError("complete exact Invitation Bootstrap", err, nil)
	}
	if n != 1 {
		return biz.ErrInvitationConflict
	}
	n, err = t.q.ActivateInvitationBootstrapAccess(ctx, sqlcgen.ActivateInvitationBootstrapAccessParams{TenantID: target.TenantID, ExpectedVersion: s.AccessVersion, Now: requiredTimestamptz(now)})
	if err != nil {
		return mapPostgresError("activate accepted Bootstrap Access", err, nil)
	}
	if n != 1 {
		return biz.ErrInvitationConflict
	}
	scope, err := biz.NewTenantScope(target.TenantID)
	if err != nil {
		return err
	}
	return (securityAuditRepository{queries: t.q, tenantID: target.TenantID}).Append(ctx, scope, a)
}
