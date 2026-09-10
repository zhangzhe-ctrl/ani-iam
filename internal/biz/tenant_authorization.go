package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTenantAccessNotFound      = errors.New("tenant access not found")
	ErrRoleNotFound              = errors.New("tenant role not found")
	ErrRoleBindingNotFound       = errors.New("tenant role binding not found")
	ErrRoleBindingConflict       = errors.New("tenant role binding conflicts with existing state")
	ErrPermissionUncatalogued    = errors.New("permission is not present in the accepted catalog")
	ErrLastTenantAdministrator   = errors.New("last active human tenant administrator cannot be removed")
	ErrMembershipStatusInvalid   = errors.New("membership status is invalid")
	ErrExpectedVersionRequired   = errors.New("expected version is required")
	ErrTenantRoleBoundaryInvalid = errors.New("tenant role contains a non-tenant permission")
)

const TenantAdminRoleCode = "tenant-admin"

const (
	AuditActionMembershipUpdated   AuditAction = "iam.membership.updated"
	AuditActionRoleBound           AuditAction = "iam.role-binding.created"
	AuditActionRoleUnbound         AuditAction = "iam.role-binding.deleted"
	AuditActionTenantAccessUpdated AuditAction = "iam.tenant-access.updated"
)

const (
	AuditTargetTypeTenantRoleBinding AuditTargetType = "tenant_role_binding"
	AuditTargetTypeTenantAccess      AuditTargetType = "tenant_access"
)

const AuditReasonTenantAdminMutation AuditReason = "TENANT_ADMIN_MUTATION"

type TenantAccess struct {
	Status    TenantAccessStatus
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TenantRole struct {
	ID                      uuid.UUID
	Code                    string
	DisplayName             string
	System                  bool
	SystemDefinitionVersion int64
	Permissions             []Permission
	Version                 int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type TenantRoleBinding struct {
	ID           uuid.UUID
	MembershipID uuid.UUID
	RoleID       uuid.UUID
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PrincipalType string

const (
	PrincipalTypeHuman    PrincipalType = "human"
	PrincipalTypeWorkload PrincipalType = "workload"
)

type TenantMembershipRecord struct {
	Membership    TenantMembership
	PrincipalType PrincipalType
	RoleIDs       []uuid.UUID
}

type TenantMembershipPage struct {
	Items      []TenantMembershipRecord
	NextCursor uuid.UUID
}

type TenantRolePage struct {
	Items      []TenantRole
	NextCursor uuid.UUID
}

type TenantAuthorizationReader interface {
	GetAccess(context.Context, TenantScope) (TenantAccess, error)
	GetMembership(context.Context, TenantScope, uuid.UUID) (TenantMembershipRecord, error)
	ListMemberships(context.Context, TenantScope, MembershipStatus, uuid.UUID, int32) (TenantMembershipPage, error)
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	ListRoles(context.Context, TenantScope, uuid.UUID, int32) (TenantRolePage, error)
}

type TenantAuthorizationActor struct {
	DirectCaller         DirectCaller
	PrincipalID          uuid.UUID
	AuthenticationMethod AuditAuthenticationMethod
	RequestID            string
	CorrelationID        string
	DecisionID           string
}

type TenantAuthorizationTransaction interface {
	MutationResultTransaction
	LockAdministrationGuard(context.Context, TenantScope) error
	GetAccess(context.Context, TenantScope) (TenantAccess, error)
	UpdateAccessStatus(context.Context, TenantScope, TenantAccessStatus, int64, time.Time) (TenantAccess, error)
	GetMembership(context.Context, TenantScope, uuid.UUID) (TenantMembership, error)
	GetMembershipRecord(context.Context, TenantScope, uuid.UUID) (TenantMembershipRecord, error)
	UpdateMembershipStatus(context.Context, TenantScope, uuid.UUID, MembershipStatus, int64, time.Time) (TenantMembership, error)
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	ActiveHumanAdministratorCount(context.Context, TenantScope) (int64, error)
	IsActiveHumanAdministrator(context.Context, TenantScope, uuid.UUID) (bool, error)
	BindRole(context.Context, TenantScope, TenantRoleBinding, int64, time.Time) (TenantMembership, TenantRoleBinding, error)
	UnbindRole(context.Context, TenantScope, uuid.UUID, uuid.UUID, int64, time.Time) (TenantMembership, TenantRoleBinding, error)
	DisableTenantWorkloadForMembership(context.Context, TenantScope, uuid.UUID, time.Time) (TenantWorkload, int64, error)
	AppendAudit(context.Context, TenantScope, SecurityAuditEvent) error
}

type TenantAuthorizationUnitOfWork interface {
	WithinTenantAuthorization(context.Context, TenantScope, func(context.Context, TenantAuthorizationTransaction) error) error
	TenantWorkloadUnitOfWork
}

type UpdateTenantMembershipCommand struct {
	IdempotencyKey  string
	MembershipID    uuid.UUID
	Status          MembershipStatus
	ExpectedVersion int64
	Actor           TenantAuthorizationActor
}

type UpdateTenantAccessCommand struct {
	IdempotencyKey  string
	Status          TenantAccessStatus
	ExpectedVersion int64
	Actor           TenantAuthorizationActor
}

type TenantAccessMutationResult struct {
	Access       TenantAccess
	AuditEventID uuid.UUID
}

type BindTenantRoleCommand struct {
	IdempotencyKey            string
	MembershipID              uuid.UUID
	RoleID                    uuid.UUID
	ExpectedMembershipVersion int64
	Actor                     TenantAuthorizationActor
}

type UnbindTenantRoleCommand = BindTenantRoleCommand

type TenantMembershipMutationResult struct {
	Membership    TenantMembership
	PrincipalType PrincipalType
	RoleIDs       []uuid.UUID
	AuditEventID  uuid.UUID
}

type TenantAuthorizationUsecase struct {
	uow             TenantAuthorizationUnitOfWork
	catalog         PermissionCatalog
	ids             IDGenerator
	clock           Clock
	tenantWorkloads *TenantWorkloadUsecase
}

func NewTenantAuthorizationUsecase(
	uow TenantAuthorizationUnitOfWork,
	catalog PermissionCatalog,
	ids IDGenerator,
	clock Clock,
	limiter APIKeyCreationLimiter,
) *TenantAuthorizationUsecase {
	return &TenantAuthorizationUsecase{
		uow: uow, catalog: catalog, ids: ids, clock: clock,
		tenantWorkloads: NewTenantWorkloadUsecase(uow, ids, clock, limiter),
	}
}

func (u *TenantAuthorizationUsecase) CreateTenantWorkload(ctx context.Context, scope TenantScope, command CreateTenantWorkloadCommand) (CreateTenantWorkloadResult, error) {
	if u.tenantWorkloads == nil {
		return CreateTenantWorkloadResult{}, ErrAuthenticationDependency
	}
	return u.tenantWorkloads.CreateTenantWorkload(ctx, scope, command)
}

func (u *TenantAuthorizationUsecase) CreateAPIKey(ctx context.Context, scope TenantScope, command CreateAPIKeyCommand) (CreateAPIKeyResult, error) {
	if u.tenantWorkloads == nil {
		return CreateAPIKeyResult{}, ErrAuthenticationDependency
	}
	return u.tenantWorkloads.CreateAPIKey(ctx, scope, command)
}

func (u *TenantAuthorizationUsecase) UpdateTenantWorkload(ctx context.Context, scope TenantScope, command UpdateTenantWorkloadCommand) (UpdateTenantWorkloadResult, error) {
	if u.tenantWorkloads == nil {
		return UpdateTenantWorkloadResult{}, ErrAuthenticationDependency
	}
	return u.tenantWorkloads.UpdateTenantWorkload(ctx, scope, command)
}

func (u *TenantAuthorizationUsecase) RevokeAPIKey(ctx context.Context, scope TenantScope, command RevokeAPIKeyCommand) (RevokeAPIKeyResult, error) {
	if u.tenantWorkloads == nil {
		return RevokeAPIKeyResult{}, ErrAuthenticationDependency
	}
	return u.tenantWorkloads.RevokeAPIKey(ctx, scope, command)
}

func (u *TenantAuthorizationUsecase) UpdateAccess(ctx context.Context, scope TenantScope, command UpdateTenantAccessCommand) (TenantAccessMutationResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return TenantAccessMutationResult{}, err
	}
	if command.ExpectedVersion <= 0 {
		return TenantAccessMutationResult{}, ErrExpectedVersionRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return TenantAccessMutationResult{}, err
	}
	if command.Status != TenantAccessStatusActive && command.Status != TenantAccessStatusSuspended {
		return TenantAccessMutationResult{}, ErrTenantAccessInactive
	}
	identity, err := mutationIdentity(command.Actor, "updateTenantIAMAccess", command.IdempotencyKey, struct {
		Status  TenantAccessStatus
		Version int64
	}{command.Status, command.ExpectedVersion})
	if err != nil {
		return TenantAccessMutationResult{}, err
	}
	now := u.clock.Now().UTC()
	var result TenantAccessMutationResult
	err = u.uow.WithinTenantAuthorization(ctx, scope, func(txContext context.Context, tx TenantAuthorizationTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			current, err := tx.GetAccess(txContext, scope)
			if err != nil {
				return err
			}
			if current.Version != command.ExpectedVersion {
				return ErrVersionConflict
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			updated, err := tx.UpdateAccessStatus(txContext, scope, command.Status, command.ExpectedVersion, now)
			if err != nil {
				return err
			}
			tenantID, _ := scope.TenantID()
			audit := newTenantAuthorizationAudit(auditID, command.Actor, AuditActionTenantAccessUpdated, AuditTargetTypeTenantAccess, tenantID, updated.Version, now)
			if err := tx.AppendAudit(txContext, scope, audit); err != nil {
				return err
			}
			result = TenantAccessMutationResult{Access: updated, AuditEventID: auditID}
			return nil
		})
	})
	return result, err
}

func (u *TenantAuthorizationUsecase) UpdateMembership(ctx context.Context, scope TenantScope, command UpdateTenantMembershipCommand) (TenantMembershipMutationResult, error) {
	if err := validateTenantMutation(scope, command.MembershipID, command.ExpectedVersion, command.Actor); err != nil {
		return TenantMembershipMutationResult{}, err
	}
	if command.Status != MembershipStatusActive && command.Status != MembershipStatusSuspended && command.Status != MembershipStatusRemoved {
		return TenantMembershipMutationResult{}, ErrMembershipStatusInvalid
	}
	operation := "updateTenantIAMMember"
	if command.Status == MembershipStatusRemoved {
		operation = "removeTenantIAMMember"
	}
	identity, err := mutationIdentity(command.Actor, operation, command.IdempotencyKey, struct {
		ID      uuid.UUID
		Status  MembershipStatus
		Version int64
	}{command.MembershipID, command.Status, command.ExpectedVersion})
	if err != nil {
		return TenantMembershipMutationResult{}, err
	}
	now := u.clock.Now().UTC()
	var result TenantMembershipMutationResult
	err = u.uow.WithinTenantAuthorization(ctx, scope, func(txContext context.Context, tx TenantAuthorizationTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			currentRecord, err := tx.GetMembershipRecord(txContext, scope, command.MembershipID)
			if err != nil {
				return err
			}
			current := currentRecord.Membership
			if current.Version != command.ExpectedVersion {
				return ErrVersionConflict
			}
			if current.Status == MembershipStatusRemoved && command.Status != MembershipStatusRemoved {
				return ErrMembershipStatusInvalid
			}
			if current.Status == MembershipStatusActive && command.Status != MembershipStatusActive {
				if err := u.protectLastAdministrator(txContext, tx, scope, command.MembershipID); err != nil {
					return err
				}
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			updated, err := tx.UpdateMembershipStatus(txContext, scope, command.MembershipID, command.Status, command.ExpectedVersion, now)
			if err != nil {
				return err
			}
			audit := newTenantAuthorizationAudit(auditID, command.Actor, AuditActionMembershipUpdated, AuditTargetTypeTenantMembership, updated.ID, updated.Version, now)
			if err := tx.AppendAudit(txContext, scope, audit); err != nil {
				return err
			}
			if command.Status == MembershipStatusRemoved && currentRecord.PrincipalType == PrincipalTypeWorkload {
				principal, _, err := tx.DisableTenantWorkloadForMembership(txContext, scope, updated.ID, now)
				if err != nil {
					return err
				}
				disableAuditID, err := u.newID()
				if err != nil {
					return err
				}
				disableAudit := newTenantAuthorizationAudit(
					disableAuditID, command.Actor, AuditActionTenantWorkloadDisabled,
					AuditTargetTypeTenantWorkload, principal.ID, principal.Version, now,
				)
				if err := tx.AppendAudit(txContext, scope, disableAudit); err != nil {
					return err
				}
			}
			record, err := tx.GetMembershipRecord(txContext, scope, updated.ID)
			if err != nil {
				return err
			}
			result = newTenantMembershipMutationResult(record, auditID)
			return nil
		})
	})
	return result, err
}

func (u *TenantAuthorizationUsecase) BindRole(ctx context.Context, scope TenantScope, command BindTenantRoleCommand) (TenantMembershipMutationResult, error) {
	if err := validateTenantMutation(scope, command.MembershipID, command.ExpectedMembershipVersion, command.Actor); err != nil {
		return TenantMembershipMutationResult{}, err
	}
	if command.RoleID == uuid.Nil {
		return TenantMembershipMutationResult{}, ErrRoleNotFound
	}
	identity, err := mutationIdentity(command.Actor, "bindTenantIAMRole", command.IdempotencyKey, struct {
		Membership, Role uuid.UUID
		Version          int64
	}{command.MembershipID, command.RoleID, command.ExpectedMembershipVersion})
	if err != nil {
		return TenantMembershipMutationResult{}, err
	}
	now := u.clock.Now().UTC()
	var result TenantMembershipMutationResult
	err = u.uow.WithinTenantAuthorization(ctx, scope, func(txContext context.Context, tx TenantAuthorizationTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			role, err := tx.GetRole(txContext, scope, command.RoleID)
			if err != nil {
				return err
			}
			if err := u.validateRolePermissions(role); err != nil {
				return err
			}
			bindingID, err := u.newID()
			if err != nil {
				return err
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			updated, binding, err := tx.BindRole(txContext, scope, TenantRoleBinding{
				ID: bindingID, MembershipID: command.MembershipID, RoleID: command.RoleID,
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}, command.ExpectedMembershipVersion, now)
			if err != nil {
				return err
			}
			audit := newTenantAuthorizationAudit(auditID, command.Actor, AuditActionRoleBound, AuditTargetTypeTenantRoleBinding, binding.ID, binding.Version, now)
			if err := tx.AppendAudit(txContext, scope, audit); err != nil {
				return err
			}
			record, err := tx.GetMembershipRecord(txContext, scope, updated.ID)
			if err != nil {
				return err
			}
			result = newTenantMembershipMutationResult(record, auditID)
			return nil
		})
	})
	return result, err
}

func (u *TenantAuthorizationUsecase) UnbindRole(ctx context.Context, scope TenantScope, command UnbindTenantRoleCommand) (TenantMembershipMutationResult, error) {
	if err := validateTenantMutation(scope, command.MembershipID, command.ExpectedMembershipVersion, command.Actor); err != nil {
		return TenantMembershipMutationResult{}, err
	}
	if command.RoleID == uuid.Nil {
		return TenantMembershipMutationResult{}, ErrRoleNotFound
	}
	identity, err := mutationIdentity(command.Actor, "unbindTenantIAMRole", command.IdempotencyKey, struct {
		Membership, Role uuid.UUID
		Version          int64
	}{command.MembershipID, command.RoleID, command.ExpectedMembershipVersion})
	if err != nil {
		return TenantMembershipMutationResult{}, err
	}
	now := u.clock.Now().UTC()
	var result TenantMembershipMutationResult
	err = u.uow.WithinTenantAuthorization(ctx, scope, func(txContext context.Context, tx TenantAuthorizationTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			role, err := tx.GetRole(txContext, scope, command.RoleID)
			if err != nil {
				return err
			}
			if role.System && role.Code == TenantAdminRoleCode {
				if err := u.protectLastAdministrator(txContext, tx, scope, command.MembershipID); err != nil {
					return err
				}
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			updated, binding, err := tx.UnbindRole(txContext, scope, command.MembershipID, command.RoleID, command.ExpectedMembershipVersion, now)
			if err != nil {
				return err
			}
			audit := newTenantAuthorizationAudit(auditID, command.Actor, AuditActionRoleUnbound, AuditTargetTypeTenantRoleBinding, binding.ID, binding.Version, now)
			if err := tx.AppendAudit(txContext, scope, audit); err != nil {
				return err
			}
			record, err := tx.GetMembershipRecord(txContext, scope, updated.ID)
			if err != nil {
				return err
			}
			result = newTenantMembershipMutationResult(record, auditID)
			return nil
		})
	})
	return result, err
}

func newTenantMembershipMutationResult(record TenantMembershipRecord, auditID uuid.UUID) TenantMembershipMutationResult {
	return TenantMembershipMutationResult{
		Membership: record.Membership, PrincipalType: record.PrincipalType,
		RoleIDs: append([]uuid.UUID(nil), record.RoleIDs...), AuditEventID: auditID,
	}
}

func (u *TenantAuthorizationUsecase) protectLastAdministrator(ctx context.Context, tx TenantAuthorizationTransaction, scope TenantScope, membershipID uuid.UUID) error {
	if err := tx.LockAdministrationGuard(ctx, scope); err != nil {
		return err
	}
	isAdministrator, err := tx.IsActiveHumanAdministrator(ctx, scope, membershipID)
	if err != nil || !isAdministrator {
		return err
	}
	count, err := tx.ActiveHumanAdministratorCount(ctx, scope)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastTenantAdministrator
	}
	return nil
}

func (u *TenantAuthorizationUsecase) validateRolePermissions(role TenantRole) error {
	for _, permission := range role.Permissions {
		if permission.Scope != PermissionScopeTenant {
			return ErrTenantRoleBoundaryInvalid
		}
		if !u.catalog.Contains(permission) {
			return fmt.Errorf("%w: %s/%s", ErrPermissionUncatalogued, permission.Resource, permission.Action)
		}
	}
	return nil
}

func (u *TenantAuthorizationUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}

func validateTenantMutation(scope TenantScope, targetID uuid.UUID, expectedVersion int64, actor TenantAuthorizationActor) error {
	if _, err := scope.TenantID(); err != nil {
		return err
	}
	if targetID == uuid.Nil {
		return ErrMembershipNotFound
	}
	if expectedVersion <= 0 {
		return ErrExpectedVersionRequired
	}
	return validateTenantActor(actor)
}

func validateTenantActor(actor TenantAuthorizationActor) error {
	if actor.PrincipalID == uuid.Nil {
		return ErrAuditActorRequired
	}
	if actor.AuthenticationMethod == "" {
		return ErrAuditAuthenticationMethodRequired
	}
	if strings.TrimSpace(actor.RequestID) == "" {
		return ErrAuditRequestIDRequired
	}
	if strings.TrimSpace(actor.CorrelationID) == "" {
		return ErrAuditCorrelationIDRequired
	}
	if strings.TrimSpace(actor.DecisionID) == "" {
		return ErrAuditDecisionIDRequired
	}
	return nil
}

func newTenantAuthorizationAudit(id uuid.UUID, actor TenantAuthorizationActor, action AuditAction, targetType AuditTargetType, targetID uuid.UUID, targetVersion int64, now time.Time) SecurityAuditEvent {
	return SecurityAuditEvent{
		ID: id, ActorID: actor.PrincipalID, AuthenticationMethod: actor.AuthenticationMethod, DirectCaller: actor.DirectCaller,
		Boundary: AuditBoundaryTenant, Action: action, TargetType: targetType, TargetID: targetID,
		TargetVersion: targetVersion, Result: AuditResultSucceeded, Reason: AuditReasonTenantAdminMutation,
		RequestID: actor.RequestID, CorrelationID: actor.CorrelationID, DecisionID: actor.DecisionID,
		SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}
