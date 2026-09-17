package biz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrLastPlatformAdministrator = errors.New("last login-capable Platform administrator cannot be removed")

// Login policy comes only from the composition root's enabled authentication
// implementation. Request fields and IdP reachability never establish it.
type PlatformAdminLoginPolicy struct {
	OIDCProvider, OIDCIssuer string
	PasswordEnabled          bool
}
type PlatformAdministratorCandidate struct {
	MembershipID uuid.UUID
	LoginCapable bool
}
type PlatformRoleBinding struct {
	ID, MembershipID, RoleID uuid.UUID
	Version                  int64
}
type PlatformMembershipRepository interface {
	AdministratorCandidates(context.Context, PlatformCapability, PlatformAdminLoginPolicy) ([]PlatformAdministratorCandidate, error)
	UpdateMembership(context.Context, PlatformCapability, uuid.UUID, MembershipStatus, int64, time.Time) (PlatformMembership, error)
	BindMembershipRole(context.Context, PlatformCapability, PlatformRoleBinding, int64, time.Time) (PlatformMembership, error)
	UnbindMembershipRole(context.Context, PlatformCapability, uuid.UUID, uuid.UUID, int64, time.Time) (PlatformMembership, PlatformRoleBinding, error)
}
type UpdatePlatformMembershipCommand struct {
	MembershipID    uuid.UUID
	Status          MembershipStatus
	ExpectedVersion int64
	IdempotencyKey  string
}
type BindPlatformRoleCommand struct {
	MembershipID, RoleID      uuid.UUID
	ExpectedMembershipVersion int64
	IdempotencyKey            string
}

func (u *PlatformAdministrationUsecase) WithLoginPolicy(policy PlatformAdminLoginPolicy) *PlatformAdministrationUsecase {
	u.loginPolicy = policy
	return u
}

func (u *PlatformAdministrationUsecase) protectPlatformAdministrator(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, member uuid.UUID) error {

	candidates, err := tx.AdministratorCandidates(ctx, cap, u.loginPolicy)
	if err != nil {
		return err
	}
	count := 0
	target := false
	for _, candidate := range candidates {
		if candidate.LoginCapable {
			count++
			if candidate.MembershipID == member {
				target = true
			}
		}
	}
	if target && count <= 1 {
		return ErrLastPlatformAdministrator
	}
	return nil

}
func (u *PlatformAdministrationUsecase) UpdateMembership(ctx context.Context, cap PlatformCapability, c UpdatePlatformMembershipCommand) (PlatformMutationResult, error) {
	if c.MembershipID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	if c.Status != MembershipStatusActive && c.Status != MembershipStatusSuspended && c.Status != MembershipStatusRemoved {
		return PlatformMutationResult{}, ErrMembershipStatusInvalid
	}
	op := "updatePlatformIAMMember"
	if c.Status == MembershipStatusRemoved {
		op = "removePlatformIAMMember"
	}
	return u.mutate(ctx, cap, op, c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		m, err := tx.GetMembership(ctx, cap, c.MembershipID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if m.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		if m.Status == MembershipStatusRemoved {
			return PlatformMutationResult{}, ErrMembershipStatusInvalid
		}
		if m.Status == MembershipStatusActive && c.Status != MembershipStatusActive {
			if err = u.protectPlatformAdministrator(ctx, tx, cap, m.ID); err != nil {
				return PlatformMutationResult{}, err
			}
		}
		m, err = tx.UpdateMembership(ctx, cap, m.ID, c.Status, c.ExpectedVersion, now)
		return PlatformMutationResult{Membership: &m, TargetID: m.ID, TargetVersion: m.Version}, err
	})
}
func (u *PlatformAdministrationUsecase) BindRole(ctx context.Context, cap PlatformCapability, c BindPlatformRoleCommand) (PlatformMutationResult, error) {
	if c.MembershipID == uuid.Nil || c.RoleID == uuid.Nil || c.ExpectedMembershipVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, cap, "bindPlatformIAMRole", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		m, err := mutablePlatformMembership(ctx, tx, cap, c.MembershipID, c.ExpectedMembershipVersion)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if _, err = tx.GetRole(ctx, cap, c.RoleID); err != nil {
			return PlatformMutationResult{}, err
		}
		id, err := u.ids.NewID()
		if err != nil || id.Version() != 7 {
			return PlatformMutationResult{}, ErrInvalidGeneratedID
		}
		binding := PlatformRoleBinding{ID: id, MembershipID: m.ID, RoleID: c.RoleID, Version: 1}
		m, err = tx.BindMembershipRole(ctx, cap, binding, c.ExpectedMembershipVersion, now)
		return PlatformMutationResult{Membership: &m, TargetID: id, TargetVersion: 1}, err
	})
}
func (u *PlatformAdministrationUsecase) UnbindRole(ctx context.Context, cap PlatformCapability, c BindPlatformRoleCommand) (PlatformMutationResult, error) {
	if c.MembershipID == uuid.Nil || c.RoleID == uuid.Nil || c.ExpectedMembershipVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, cap, "unbindPlatformIAMRole", c.IdempotencyKey, c, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		m, err := tx.GetMembership(ctx, cap, c.MembershipID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if m.Version != c.ExpectedMembershipVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		role, err := tx.GetRole(ctx, cap, c.RoleID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if role.System && role.Code == "platform-admin" {
			if err = u.protectPlatformAdministrator(ctx, tx, cap, m.ID); err != nil {
				return PlatformMutationResult{}, err
			}
		}
		m, binding, err := tx.UnbindMembershipRole(ctx, cap, m.ID, role.ID, c.ExpectedMembershipVersion, now)
		return PlatformMutationResult{Membership: &m, TargetID: binding.ID, TargetVersion: binding.Version + 1}, err
	})
}
func mutablePlatformMembership(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, id uuid.UUID, version int64) (PlatformMembership, error) {
	m, err := tx.GetMembership(ctx, cap, id)
	if err != nil {
		return PlatformMembership{}, err
	}
	if m.Status != MembershipStatusActive {
		return PlatformMembership{}, ErrMembershipStatusInvalid
	}
	if m.Version != version {
		return PlatformMembership{}, ErrVersionConflict
	}
	return m, nil
}
