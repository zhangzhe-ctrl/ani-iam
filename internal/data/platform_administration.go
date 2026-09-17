package data

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type platformAdministrationUOW struct {
	data   *Data
	broker *coreBrokerRepository
}
type platformAdministrationTransaction struct {
	data      *Data
	bootstrap *coreBootstrapWorkTransaction
	broker    *coreBrokerRepository
	q         *sqlcgen.Queries
	protector *OutboxProtector
}

func NewPlatformAdministrationUnitOfWork(d *Data) biz.PlatformAdministrationUnitOfWork {
	return &platformAdministrationUOW{data: d}
}
func NewCoreBrokerPlatformAdministrationUnitOfWork(d *Data, c CoreBrokerConfiguration) (biz.PlatformAdministrationUnitOfWork, error) {
	broker, err := newCoreBrokerRepository(d, c)
	if err != nil {
		return nil, err
	}
	return &platformAdministrationUOW{data: d, broker: broker}, nil
}
func (u *platformAdministrationUOW) WithinPlatformAdministration(ctx context.Context, cap biz.PlatformCapability, fn func(biz.PlatformAdministrationTransaction) error) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	if u == nil || u.data == nil || u.data.pool == nil || fn == nil {
		return biz.ErrPersistenceUnavailable
	}
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
	if u.broker != nil {
		// Keep the same order as receipt/projector and worker transactions.
		if err = q.LockCoreBrokerAuthority(ctx); err != nil {
			return mapPostgresError("lock Platform broker authority", err, nil)
		}
	}
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err = fn(&platformAdministrationTransaction{q: q, protector: u.data.outbox, data: u.data, broker: u.broker}); err != nil {
		return err
	}
	return platformLoginPersistenceError(tx.Commit(ctx))
}
func (t *platformAdministrationTransaction) LookupAuthority(ctx context.Context, cap biz.PlatformCapability, resource string, actions []string) (biz.PlatformAuthorizationState, error) {
	claims, operation, err := cap.CredentialBinding()
	if err != nil {
		return biz.PlatformAuthorizationState{}, err
	}
	if operation == "replayCoreIAMDLQEntry" {
		// Retain the current Human/session rows through commit. Platform role and
		// membership mutations already serialize on the Platform administrator guard.
		_, err = t.q.LockCoreDLQHumanAuthority(ctx, sqlcgen.LockCoreDLQHumanAuthorityParams{PrincipalID: claims.Subject, SessionID: claims.SessionID, GrantID: claims.GrantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.PlatformAuthorizationState{}, biz.ErrPlatformAdministrationDenied
		}
		if err != nil {
			return biz.PlatformAuthorizationState{}, platformLoginPersistenceError(err)
		}
	}
	return lookupPlatformAuthorization(ctx, t.q, claims, resource, actions)
}
func (t *platformAdministrationTransaction) GetMembership(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.PlatformMembership, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformMembership{}, err
	}
	r, err := t.q.GetPlatformMembership(ctx, sqlcgen.GetPlatformMembershipParams{ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformMembership{}, biz.ErrMembershipNotFound
	}
	if err != nil {
		return biz.PlatformMembership{}, platformLoginPersistenceError(err)
	}
	return t.membership(ctx, r)
}
func (t *platformAdministrationTransaction) membership(ctx context.Context, r sqlcgen.PlatformMembership) (biz.PlatformMembership, error) {
	roles, err := t.q.GetPlatformMembershipRoles(ctx, sqlcgen.GetPlatformMembershipRolesParams{MembershipID: r.ID})
	if err != nil {
		return biz.PlatformMembership{}, platformLoginPersistenceError(err)
	}
	return biz.PlatformMembership{ID: r.ID, PrincipalID: r.PrincipalID, Status: biz.MembershipStatus(r.Status), Version: r.Version, RoleIDs: roles, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}, nil
}
func (t *platformAdministrationTransaction) ListMemberships(ctx context.Context, cap biz.PlatformCapability, status biz.MembershipStatus, after uuid.UUID, limit int32) ([]biz.PlatformMembership, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return nil, err
	}
	rows, err := t.q.ListPlatformMemberships(ctx, sqlcgen.ListPlatformMembershipsParams{Status: string(status), AfterID: after, PageLimit: limit})
	if err != nil {
		return nil, platformLoginPersistenceError(err)
	}
	items := make([]biz.PlatformMembership, 0, len(rows))
	for _, row := range rows {
		item, err := t.membership(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
func (t *platformAdministrationTransaction) GetRole(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.PlatformRole, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformRole{}, err
	}
	r, err := t.q.GetPlatformRole(ctx, sqlcgen.GetPlatformRoleParams{ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformRole{}, biz.ErrRoleNotFound
	}
	if err != nil {
		return biz.PlatformRole{}, platformLoginPersistenceError(err)
	}
	return t.role(ctx, r)
}
func (t *platformAdministrationTransaction) role(ctx context.Context, r sqlcgen.PlatformRole) (biz.PlatformRole, error) {
	rows, err := t.q.GetPlatformRolePermissionSet(ctx, sqlcgen.GetPlatformRolePermissionSetParams{RoleID: r.ID})
	if err != nil {
		return biz.PlatformRole{}, platformLoginPersistenceError(err)
	}
	role := biz.PlatformRole{ID: r.ID, Code: r.Code, DisplayName: r.DisplayName, System: r.SystemRole, SystemDefinitionVersion: r.SystemDefinitionVersion, Version: r.Version, Permissions: []biz.Permission{}}
	for _, p := range rows {
		role.Permissions = append(role.Permissions, biz.Permission{Scope: biz.PermissionScope(p.Scope), Resource: p.Resource, Action: p.Action})
	}
	return role, nil
}
func (t *platformAdministrationTransaction) ListRoles(ctx context.Context, cap biz.PlatformCapability, after uuid.UUID, limit int32) ([]biz.PlatformRole, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return nil, err
	}
	rows, err := t.q.ListPlatformRoles(ctx, sqlcgen.ListPlatformRolesParams{AfterID: after, PageLimit: limit})
	if err != nil {
		return nil, platformLoginPersistenceError(err)
	}
	items := make([]biz.PlatformRole, 0, len(rows))
	for _, row := range rows {
		item, err := t.role(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
func (t *platformAdministrationTransaction) GetTenantAccess(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.TenantAccess, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.TenantAccess{}, err
	}
	r, err := t.q.GetPlatformTargetTenantAccess(ctx, sqlcgen.GetPlatformTargetTenantAccessParams{TenantID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAccess{}, biz.ErrTenantAccessNotFound
	}
	if err != nil {
		return biz.TenantAccess{}, platformLoginPersistenceError(err)
	}
	return biz.TenantAccess{Status: biz.TenantAccessStatus(r.Status), Version: r.Version, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}, nil
}
func (t *platformAdministrationTransaction) AppendAudit(ctx context.Context, cap biz.PlatformCapability, a biz.SecurityAuditEvent) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	return appendPlatformAudit(ctx, t.q, a)
}
