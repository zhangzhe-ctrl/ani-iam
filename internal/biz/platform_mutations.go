package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Platform mutations never manufacture a TenantScope from their target ID.
type PlatformMutationRepository interface {
	FindPlatformMutation(context.Context, PlatformCapability, MutationIdentity) (StoredMutation, bool, error)
	SavePlatformMutation(context.Context, PlatformCapability, StoredMutation) error
	CreateCustomRole(context.Context, PlatformCapability, PlatformRole, time.Time) error
	UpdateCustomRole(context.Context, PlatformCapability, PlatformRole, int64, time.Time) error
	DeleteCustomRole(context.Context, PlatformCapability, uuid.UUID, int64) error
	RoleReferenced(context.Context, PlatformCapability, uuid.UUID) (bool, error)
	UpdateTargetTenantAccess(context.Context, PlatformCapability, uuid.UUID, TenantAccessStatus, int64, time.Time) (TenantAccess, error)
}

type CreatePlatformRoleCommand struct {
	Code, DisplayName, IdempotencyKey string
	Permissions                       []Permission
}
type UpdatePlatformRoleCommand struct {
	RoleID                      uuid.UUID
	DisplayName, IdempotencyKey string
	Permissions                 []Permission
	ExpectedVersion             int64
}
type DeletePlatformRoleCommand struct {
	RoleID          uuid.UUID
	ExpectedVersion int64
	IdempotencyKey  string
}
type UpdatePlatformTenantAccessCommand struct {
	TenantID        uuid.UUID
	Status          TenantAccessStatus
	ExpectedVersion int64
	IdempotencyKey  string
}
type PlatformMutationResult struct {
	Bootstrap          *TenantBootstrapOperation     `json:"bootstrap,omitempty"`
	Recovery           *TenantAdminRecoveryOperation `json:"recovery,omitempty"`
	RestoredMembership *TenantMembershipRecord       `json:"restored_membership,omitempty"`
	Invitation         *PlatformInvitation           `json:"invitation,omitempty"`
	Membership         *PlatformMembership           `json:"membership,omitempty"`
	Role               *PlatformRole                 `json:"role,omitempty"`
	Access             *TenantAccess                 `json:"access,omitempty"`
	TargetID           uuid.UUID                     `json:"target_id"`
	TargetVersion      int64                         `json:"target_version"`
	AuditEventID       uuid.UUID                     `json:"audit_event_id"`
}

func platformMutationIdentity(cap PlatformCapability, operation, key string, fields any) (MutationIdentity, error) {
	claims, op, err := cap.CredentialBinding()
	if err != nil || op != operation {
		return MutationIdentity{}, ErrPlatformAdministrationDenied
	}
	if key == "" || key != strings.TrimSpace(key) || len(key) > 128 {
		return MutationIdentity{}, ErrIdempotencyKeyRequired
	}
	caller := cap.caller.Identity.PrincipalID
	if caller == uuid.Nil {
		return MutationIdentity{}, ErrPlatformAdministrationDenied
	}
	encoded, err := json.Marshal(struct {
		Caller uuid.UUID
		Fields any
	}{caller, fields})
	if err != nil {
		return MutationIdentity{}, ErrInvalidPersistenceState
	}
	return MutationIdentity{ActorID: claims.Subject, CallerID: caller, Operation: operation, Key: key, Intent: sha256.Sum256(encoded)}, nil
}

func (u *PlatformAdministrationUsecase) mutate(ctx context.Context, cap PlatformCapability, operation, key string, fields any, mutation func(PlatformAdministrationTransaction, time.Time) (PlatformMutationResult, error)) (PlatformMutationResult, error) {
	return u.mutateWithPrecondition(ctx, cap, operation, key, fields, nil, mutation)
}

func (u *PlatformAdministrationUsecase) mutateWithPrecondition(ctx context.Context, cap PlatformCapability, operation, key string, fields any, before func(PlatformAdministrationTransaction, time.Time) error, mutation func(PlatformAdministrationTransaction, time.Time) (PlatformMutationResult, error)) (PlatformMutationResult, error) {
	identity, err := platformMutationIdentity(cap, operation, key, fields)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	var result PlatformMutationResult
	err = u.withinAuthorized(ctx, cap, operation, func(tx PlatformAdministrationTransaction, now time.Time) error {
		if before != nil {
			if err := before(tx, now); err != nil {
				return err
			}
			// External authority checks may outlive the original Human token or
			// Session. Recheck current Platform authority before receipt reuse.
			now = u.clock.Now().UTC()
			if !now.Before(cap.claims.ExpiresAt) {
				return ErrInvalidCredential
			}
			policy, _ := u.registry.Lookup(operation)
			state, err := tx.LookupAuthority(ctx, cap, policy.Resource, policy.Actions)
			if err != nil {
				return err
			}
			if platformAuthorizationDenial(state, cap.claims, now) != "" || !state.PermissionAllowed {
				return ErrPlatformAdministrationDenied
			}
		}
		// The UOW already holds the global Platform guard. Authorization comes
		// before receipt lookup, including when the original write succeeded.
		previous, found, err := tx.FindPlatformMutation(ctx, cap, identity)
		if err != nil {
			return err
		}
		if found {
			if previous.Identity.Intent != identity.Intent {
				return ErrIdempotencyConflict
			}
			if !now.Before(previous.ExpiresAt) {
				return ErrIdempotencyExpired
			}
			decoder := json.NewDecoder(bytes.NewReader(previous.Result))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF {
				return ErrInvalidPersistenceState
			}
			if result.AuditEventID.Version() != 7 || result.TargetID == uuid.Nil || result.TargetVersion < 1 {
				return ErrInvalidPersistenceState
			}
			return nil
		}
		result, err = mutation(tx, now)
		if err != nil {
			return err
		}
		id, err := u.ids.NewID()
		if err != nil || id.Version() != 7 {
			return ErrInvalidGeneratedID
		}
		result.AuditEventID = id
		policy, _ := u.registry.Lookup(operation)
		if err = tx.AppendAudit(ctx, cap, newPlatformAdministrationAudit(cap, policy.Resource, id, result.TargetID, result.TargetVersion, now)); err != nil {
			return err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return ErrInvalidPersistenceState
		}
		return tx.SavePlatformMutation(ctx, cap, StoredMutation{Identity: identity, Result: encoded, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func (u *PlatformAdministrationUsecase) rolePermissions(name string, permissions []Permission) ([]Permission, error) {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 || len(permissions) == 0 {
		return nil, ErrRoleInvalid
	}
	if u == nil || u.catalog == nil || u.catalog.catalog == nil {
		return nil, ErrAuthenticationDependency
	}
	result := slices.Clone(permissions)
	for _, p := range result {
		if p.Scope != PermissionScopePlatform || !u.catalog.catalog.Contains(p) {
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

func (u *PlatformAdministrationUsecase) CreateRole(ctx context.Context, cap PlatformCapability, c CreatePlatformRoleCommand) (PlatformMutationResult, error) {
	permissions, err := u.rolePermissions(c.DisplayName, c.Permissions)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	if c.Code != strings.TrimSpace(c.Code) || utf8.RuneCountInString(c.Code) > 128 {
		return PlatformMutationResult{}, ErrRoleInvalid
	}
	c.Permissions = permissions
	return u.mutate(ctx, cap, "createPlatformIAMRole", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		id, err := u.ids.NewID()
		if err != nil || id.Version() != 7 {
			return PlatformMutationResult{}, ErrInvalidGeneratedID
		}
		code := c.Code
		if code == "" {
			code = "custom-" + id.String()
		}
		role := PlatformRole{ID: id, Code: code, DisplayName: c.DisplayName, Version: 1, SystemDefinitionVersion: 1, Permissions: permissions}
		return PlatformMutationResult{Role: &role, TargetID: id, TargetVersion: 1}, tx.CreateCustomRole(ctx, cap, role, now)
	})
}
func (u *PlatformAdministrationUsecase) UpdateRole(ctx context.Context, cap PlatformCapability, c UpdatePlatformRoleCommand) (PlatformMutationResult, error) {
	if c.RoleID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	permissions, err := u.rolePermissions(c.DisplayName, c.Permissions)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	c.Permissions = permissions
	return u.mutate(ctx, cap, "updatePlatformIAMRole", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		role, err := mutablePlatformRole(ctx, tx, cap, c.RoleID, c.ExpectedVersion)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		role.DisplayName, role.Permissions, role.Version = c.DisplayName, permissions, role.Version+1
		return PlatformMutationResult{Role: &role, TargetID: role.ID, TargetVersion: role.Version}, tx.UpdateCustomRole(ctx, cap, role, c.ExpectedVersion, now)
	})
}
func (u *PlatformAdministrationUsecase) DeleteRole(ctx context.Context, cap PlatformCapability, c DeletePlatformRoleCommand) (PlatformMutationResult, error) {
	if c.RoleID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, cap, "deletePlatformIAMRole", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		role, err := mutablePlatformRole(ctx, tx, cap, c.RoleID, c.ExpectedVersion)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if err := u.expirePlatformInvitationsForRole(ctx, tx, cap, c.RoleID, now); err != nil {
			return PlatformMutationResult{}, err
		}
		referenced, err := tx.RoleReferenced(ctx, cap, c.RoleID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if referenced {
			return PlatformMutationResult{}, ErrRoleInUse
		}
		role.Version++
		return PlatformMutationResult{Role: &role, TargetID: role.ID, TargetVersion: role.Version}, tx.DeleteCustomRole(ctx, cap, role.ID, c.ExpectedVersion)
	})
}
func mutablePlatformRole(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, id uuid.UUID, version int64) (PlatformRole, error) {
	role, err := tx.GetRole(ctx, cap, id)
	if err != nil {
		return PlatformRole{}, err
	}
	if role.System {
		return PlatformRole{}, ErrSystemRoleImmutable
	}
	if role.Version != version {
		return PlatformRole{}, ErrVersionConflict
	}
	return role, nil
}
func (u *PlatformAdministrationUsecase) UpdateTenantAccess(ctx context.Context, cap PlatformCapability, c UpdatePlatformTenantAccessCommand) (PlatformMutationResult, error) {
	if c.TenantID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	if c.Status != TenantAccessStatusActive && c.Status != TenantAccessStatusSuspended {
		return PlatformMutationResult{}, ErrMembershipStatusInvalid
	}
	return u.mutate(ctx, cap, "updateTenantAccess", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		current, err := tx.GetTenantAccess(ctx, cap, c.TenantID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if current.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		access, err := tx.UpdateTargetTenantAccess(ctx, cap, c.TenantID, c.Status, c.ExpectedVersion, now)
		return PlatformMutationResult{Access: &access, TargetID: c.TenantID, TargetVersion: access.Version}, err
	})
}
