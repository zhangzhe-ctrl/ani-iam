package biz

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrRoleInvalid         = errors.New("role input is invalid")
	ErrRoleConflict        = errors.New("role code already exists")
	ErrRoleInUse           = errors.New("role is referenced")
	ErrSystemRoleImmutable = errors.New("system role cannot be changed")
)

type TenantRoleTransaction interface {
	TenantRoleAdministrationTransaction
	MutationResultTransaction
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	CreateCustomRole(context.Context, TenantScope, TenantRole) error
	UpdateCustomRole(context.Context, TenantScope, TenantRole, int64) error
	DeleteCustomRole(context.Context, TenantScope, uuid.UUID, int64) error
	RoleReferenced(context.Context, TenantScope, uuid.UUID) (bool, error)
	AppendAudit(context.Context, TenantScope, SecurityAuditEvent) error
}

type TenantRoleUnitOfWork interface {
	WithinTenantRoles(context.Context, TenantScope, func(context.Context, TenantRoleTransaction) error) error
}

type CreateTenantRoleCommand struct {
	Code           string
	DisplayName    string
	Permissions    []Permission
	IdempotencyKey string
	Actor          TenantAuthorizationActor
}

type UpdateTenantRoleCommand struct {
	RoleID          uuid.UUID
	DisplayName     string
	Permissions     []Permission
	ExpectedVersion int64
	IdempotencyKey  string
	Actor           TenantAuthorizationActor
}

type DeleteTenantRoleCommand struct {
	RoleID          uuid.UUID
	ExpectedVersion int64
	IdempotencyKey  string
	Actor           TenantAuthorizationActor
}

type TenantRoleMutationResult struct {
	Role         TenantRole
	AuditEventID uuid.UUID
}

type TenantRoleUsecase struct {
	uow     TenantRoleUnitOfWork
	catalog PermissionCatalog
	ids     IDGenerator
	clock   Clock
}

func NewTenantRoleUsecase(uow TenantRoleUnitOfWork, catalog PermissionCatalog, ids IDGenerator, clock Clock) *TenantRoleUsecase {
	return &TenantRoleUsecase{uow: uow, catalog: catalog, ids: ids, clock: clock}
}

func (u *TenantRoleUsecase) Create(ctx context.Context, scope TenantScope, c CreateTenantRoleCommand) (TenantRoleMutationResult, error) {
	permissions, err := u.validate(c.DisplayName, c.Permissions)
	if err != nil {
		return TenantRoleMutationResult{}, err
	}
	if c.Code != strings.TrimSpace(c.Code) || utf8.RuneCountInString(c.Code) > 128 {
		return TenantRoleMutationResult{}, ErrRoleInvalid
	}
	// An omitted custom code is generated from the role identity, independently
	// of its mutable display name. It never selects a built-in role identity.
	fields := struct {
		Code, Name  string
		Permissions []Permission
	}{c.Code, c.DisplayName, permissions}
	return u.mutate(ctx, scope, c.Actor, "createTenantIAMRole", c.IdempotencyKey, fields, func(ctx context.Context, tx TenantRoleTransaction, now time.Time) (TenantRole, error) {
		id, err := u.newID()
		if err != nil {
			return TenantRole{}, err
		}
		code := c.Code
		if code == "" {
			code = "custom-" + id.String()
		}
		r := TenantRole{ID: id, Code: code, DisplayName: c.DisplayName, Permissions: permissions, SystemDefinitionVersion: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
		return r, tx.CreateCustomRole(ctx, scope, r)
	})
}

func (u *TenantRoleUsecase) Update(ctx context.Context, scope TenantScope, c UpdateTenantRoleCommand) (TenantRoleMutationResult, error) {
	if err := validateTenantMutation(scope, c.RoleID, c.ExpectedVersion, c.Actor); err != nil {
		return TenantRoleMutationResult{}, err
	}
	permissions, err := u.validate(c.DisplayName, c.Permissions)
	if err != nil {
		return TenantRoleMutationResult{}, err
	}
	fields := struct {
		ID          uuid.UUID
		Name        string
		Permissions []Permission
		Version     int64
	}{c.RoleID, c.DisplayName, permissions, c.ExpectedVersion}
	return u.mutate(ctx, scope, c.Actor, "updateTenantIAMRole", c.IdempotencyKey, fields, func(ctx context.Context, tx TenantRoleTransaction, now time.Time) (TenantRole, error) {
		r, err := mutableTenantRole(ctx, tx, scope, c.RoleID, c.ExpectedVersion)
		if err != nil {
			return TenantRole{}, err
		}
		r.DisplayName, r.Permissions, r.Version, r.UpdatedAt = c.DisplayName, permissions, r.Version+1, now
		return r, tx.UpdateCustomRole(ctx, scope, r, c.ExpectedVersion)
	})
}

func (u *TenantRoleUsecase) Delete(ctx context.Context, scope TenantScope, c DeleteTenantRoleCommand) (TenantRoleMutationResult, error) {
	if err := validateTenantMutation(scope, c.RoleID, c.ExpectedVersion, c.Actor); err != nil {
		return TenantRoleMutationResult{}, err
	}
	fields := struct {
		ID      uuid.UUID
		Version int64
	}{c.RoleID, c.ExpectedVersion}
	return u.mutate(ctx, scope, c.Actor, "deleteTenantIAMRole", c.IdempotencyKey, fields, func(ctx context.Context, tx TenantRoleTransaction, now time.Time) (TenantRole, error) {
		r, err := mutableTenantRole(ctx, tx, scope, c.RoleID, c.ExpectedVersion)
		if err != nil {
			return TenantRole{}, err
		}
		referenced, err := tx.RoleReferenced(ctx, scope, c.RoleID)
		if err != nil {
			return TenantRole{}, err
		}
		if referenced {
			return TenantRole{}, ErrRoleInUse
		}
		err = tx.DeleteCustomRole(ctx, scope, c.RoleID, c.ExpectedVersion)
		r.Version++
		r.UpdatedAt = now
		return r, err
	})
}

func mutableTenantRole(ctx context.Context, tx TenantRoleTransaction, scope TenantScope, id uuid.UUID, version int64) (TenantRole, error) {
	r, err := tx.GetRole(ctx, scope, id)
	if err != nil {
		return TenantRole{}, err
	}
	if r.System {
		return TenantRole{}, ErrSystemRoleImmutable
	}
	if r.Version != version {
		return TenantRole{}, ErrVersionConflict
	}
	return r, nil
}

func (u *TenantRoleUsecase) validate(name string, permissions []Permission) ([]Permission, error) {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 || len(permissions) == 0 {
		return nil, ErrRoleInvalid
	}
	if u.catalog == nil {
		return nil, ErrAuthenticationDependency
	}
	result := slices.Clone(permissions)
	for _, p := range result {
		if p.Scope != PermissionScopeTenant {
			return nil, ErrTenantRoleBoundaryInvalid
		}
		if !u.catalog.Contains(p) {
			return nil, ErrPermissionUncatalogued
		}
	}
	slices.SortFunc(result, func(a, b Permission) int {
		if c := strings.Compare(a.Resource, b.Resource); c != 0 {
			return c
		}
		return strings.Compare(a.Action, b.Action)
	})
	return slices.Compact(result), nil
}

func (u *TenantRoleUsecase) mutate(ctx context.Context, scope TenantScope, actor TenantAuthorizationActor, operation, key string, fields any, mutation func(context.Context, TenantRoleTransaction, time.Time) (TenantRole, error)) (TenantRoleMutationResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return TenantRoleMutationResult{}, err
	}
	if u.uow == nil || u.ids == nil || u.clock == nil {
		return TenantRoleMutationResult{}, ErrAuthenticationDependency
	}
	identity, err := mutationIdentity(actor, operation, key, fields)
	if err != nil {
		return TenantRoleMutationResult{}, err
	}
	var result TenantRoleMutationResult
	err = u.uow.WithinTenantRoles(ctx, scope, func(ctx context.Context, tx TenantRoleTransaction) error {
		if err := requireTenantRoleAdministrator(ctx, tx, scope, actor); err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		return executeMutation(ctx, tx, scope, identity, now, &result, func() error {
			role, err := mutation(ctx, tx, now)
			if err != nil {
				return err
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			action := map[string]AuditAction{"createTenantIAMRole": "iam.role.created", "updateTenantIAMRole": "iam.role.updated", "deleteTenantIAMRole": "iam.role.deleted"}[operation]
			if err := tx.AppendAudit(ctx, scope, newTenantAuthorizationAudit(auditID, actor, action, "tenant_role", role.ID, role.Version, now)); err != nil {
				return err
			}
			result = TenantRoleMutationResult{Role: role, AuditEventID: auditID}
			return nil
		})
	})
	return result, err
}

func (u *TenantRoleUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}
