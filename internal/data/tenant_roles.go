package data

import (
	"context"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func NewPostgresTenantRoleUnitOfWork(d *Data) biz.TenantRoleUnitOfWork {
	return &postgresTenantAuthorizationUnitOfWork{data: d}
}

func (u *postgresTenantAuthorizationUnitOfWork) WithinTenantRoles(ctx context.Context, scope biz.TenantScope, fn func(context.Context, biz.TenantRoleTransaction) error) error {
	return u.WithinTenantAuthorization(ctx, scope, func(ctx context.Context, tx biz.TenantAuthorizationTransaction) error {
		return fn(ctx, tx.(postgresTenantAuthorizationTransaction))
	})
}

func (tx postgresTenantAuthorizationTransaction) CreateCustomRole(ctx context.Context, scope biz.TenantScope, role biz.TenantRole) error {
	tenant, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	if err := tx.queries.CreateCustomTenantRole(ctx, sqlcgen.CreateCustomTenantRoleParams{TenantID: tenant, ID: role.ID, Code: role.Code, DisplayName: role.DisplayName, CreatedAt: requiredTimestamptz(role.CreatedAt), UpdatedAt: requiredTimestamptz(role.UpdatedAt)}); err != nil {
		return mapPostgresError("create custom tenant role", err, biz.ErrRoleConflict)
	}
	return tx.addCustomRolePermissions(ctx, tenant, role)
}

func (tx postgresTenantAuthorizationTransaction) UpdateCustomRole(ctx context.Context, scope biz.TenantScope, role biz.TenantRole, version int64) error {
	tenant, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	count, err := tx.queries.UpdateCustomTenantRole(ctx, sqlcgen.UpdateCustomTenantRoleParams{TenantID: tenant, ID: role.ID, DisplayName: role.DisplayName, ExpectedVersion: version, UpdatedAt: requiredTimestamptz(role.UpdatedAt)})
	if err != nil {
		return mapPostgresError("update custom tenant role", err, nil)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	if err := tx.queries.ClearCustomTenantRolePermissions(ctx, sqlcgen.ClearCustomTenantRolePermissionsParams{TenantID: tenant, RoleID: role.ID}); err != nil {
		return mapPostgresError("replace custom role permissions", err, nil)
	}
	return tx.addCustomRolePermissions(ctx, tenant, role)
}

func (tx postgresTenantAuthorizationTransaction) DeleteCustomRole(ctx context.Context, scope biz.TenantScope, id uuid.UUID, version int64) error {
	tenant, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	if err := tx.queries.ClearCustomTenantRolePermissions(ctx, sqlcgen.ClearCustomTenantRolePermissionsParams{TenantID: tenant, RoleID: id}); err != nil {
		return mapPostgresError("remove custom role permissions", err, nil)
	}
	count, err := tx.queries.DeleteCustomTenantRole(ctx, sqlcgen.DeleteCustomTenantRoleParams{TenantID: tenant, ID: id, ExpectedVersion: version})
	if err != nil {
		return mapPostgresError("delete custom tenant role", err, nil)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	return nil
}

func (tx postgresTenantAuthorizationTransaction) RoleReferenced(ctx context.Context, scope biz.TenantScope, id uuid.UUID) (bool, error) {
	tenant, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return false, err
	}
	used, err := tx.queries.CustomTenantRoleReferenced(ctx, sqlcgen.CustomTenantRoleReferencedParams{TenantID: tenant, RoleID: id})
	return used, mapPostgresError("check custom role references", err, nil)
}

func (tx postgresTenantAuthorizationTransaction) addCustomRolePermissions(ctx context.Context, tenant uuid.UUID, role biz.TenantRole) error {
	for _, p := range role.Permissions {
		if err := tx.queries.AddCustomTenantRolePermission(ctx, sqlcgen.AddCustomTenantRolePermissionParams{TenantID: tenant, RoleID: role.ID, Resource: p.Resource, Action: p.Action, CreatedAt: requiredTimestamptz(role.UpdatedAt)}); err != nil {
			return mapPostgresError("add custom role permission", err, nil)
		}
	}
	return nil
}
