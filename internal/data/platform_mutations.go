package data

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (t *platformAdministrationTransaction) FindPlatformMutation(ctx context.Context, cap biz.PlatformCapability, id biz.MutationIdentity) (biz.StoredMutation, bool, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.StoredMutation{}, false, err
	}
	r, err := t.q.FindPlatformMutation(ctx, sqlcgen.FindPlatformMutationParams{ActorID: id.ActorID, Operation: id.Operation, IdempotencyKey: id.Key})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.StoredMutation{}, false, nil
	}
	if err != nil {
		return biz.StoredMutation{}, false, platformLoginPersistenceError(err)
	}
	if len(r.RequestHash) != 32 {
		return biz.StoredMutation{}, false, biz.ErrInvalidPersistenceState
	}
	copy(id.Intent[:], r.RequestHash)
	return biz.StoredMutation{Identity: id, Result: r.Result, CreatedAt: r.CreatedAt.Time, ExpiresAt: r.ExpiresAt.Time}, true, nil
}
func (t *platformAdministrationTransaction) SavePlatformMutation(ctx context.Context, cap biz.PlatformCapability, r biz.StoredMutation) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	return platformLoginPersistenceError(t.q.SavePlatformMutation(ctx, sqlcgen.SavePlatformMutationParams{ActorID: r.Identity.ActorID, Operation: r.Identity.Operation, IdempotencyKey: r.Identity.Key, RequestHash: r.Identity.Intent[:], Result: r.Result, CreatedAt: requiredTimestamptz(r.CreatedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)}))
}
func (t *platformAdministrationTransaction) CreateCustomRole(ctx context.Context, cap biz.PlatformCapability, role biz.PlatformRole, now time.Time) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	err := t.q.CreateCustomPlatformRole(ctx, sqlcgen.CreateCustomPlatformRoleParams{ID: role.ID, Code: role.Code, DisplayName: role.DisplayName, Now: requiredTimestamptz(now)})
	if err != nil {
		var p *pgconn.PgError
		if errors.As(err, &p) && p.Code == "23505" {
			return biz.ErrRoleConflict
		}
		return platformLoginPersistenceError(err)
	}
	return t.addPlatformRolePermissions(ctx, role, now)
}
func (t *platformAdministrationTransaction) UpdateCustomRole(ctx context.Context, cap biz.PlatformCapability, role biz.PlatformRole, version int64, now time.Time) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	n, err := t.q.UpdateCustomPlatformRole(ctx, sqlcgen.UpdateCustomPlatformRoleParams{ID: role.ID, DisplayName: role.DisplayName, ExpectedVersion: version, Now: requiredTimestamptz(now)})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	if n != 1 {
		return biz.ErrVersionConflict
	}
	if err = t.q.DeleteCustomPlatformRolePermissions(ctx, sqlcgen.DeleteCustomPlatformRolePermissionsParams{RoleID: role.ID}); err != nil {
		return platformLoginPersistenceError(err)
	}
	return t.addPlatformRolePermissions(ctx, role, now)
}
func (t *platformAdministrationTransaction) DeleteCustomRole(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID, version int64) error {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return err
	}
	if err := t.q.DeleteCustomPlatformRolePermissions(ctx, sqlcgen.DeleteCustomPlatformRolePermissionsParams{RoleID: id}); err != nil {
		return platformLoginPersistenceError(err)
	}
	n, err := t.q.DeleteCustomPlatformRole(ctx, sqlcgen.DeleteCustomPlatformRoleParams{ID: id, ExpectedVersion: version})
	if err != nil {
		var p *pgconn.PgError
		if errors.As(err, &p) && p.Code == "23503" {
			return biz.ErrRoleInUse
		}
		return platformLoginPersistenceError(err)
	}
	if n != 1 {
		return biz.ErrVersionConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) addPlatformRolePermissions(ctx context.Context, role biz.PlatformRole, now time.Time) error {
	for _, p := range role.Permissions {
		if err := t.q.InsertPlatformRolePermission(ctx, sqlcgen.InsertPlatformRolePermissionParams{RoleID: role.ID, Resource: p.Resource, Action: p.Action, Now: requiredTimestamptz(now)}); err != nil {
			return platformLoginPersistenceError(err)
		}
	}
	return nil
}
func (t *platformAdministrationTransaction) RoleReferenced(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (bool, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return false, err
	}
	v, err := t.q.PlatformRoleReferenced(ctx, sqlcgen.PlatformRoleReferencedParams{RoleID: id})
	return v, platformLoginPersistenceError(err)
}
func (t *platformAdministrationTransaction) UpdateTargetTenantAccess(ctx context.Context, cap biz.PlatformCapability, tenant uuid.UUID, status biz.TenantAccessStatus, version int64, now time.Time) (biz.TenantAccess, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.TenantAccess{}, err
	}
	r, err := t.q.UpdatePlatformTargetTenantAccess(ctx, sqlcgen.UpdatePlatformTargetTenantAccessParams{TenantID: tenant, Status: string(status), ExpectedVersion: version, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAccess{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantAccess{}, platformLoginPersistenceError(err)
	}
	return biz.TenantAccess{Status: biz.TenantAccessStatus(r.Status), Version: r.Version, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}, nil
}
