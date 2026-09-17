package biz

import (
	"context"
	"github.com/google/uuid"
	"time"
)

// InvitationBootstrapState is the exact retained initial operation, not an
// inferred administrator or a request to create a missing Tenant Access.
type InvitationBootstrapState struct {
	ID                            uuid.UUID
	Version, AccessVersion        int64
	Source, Status, IntendedEmail string
	Superseded                    bool
	RoleID                        uuid.UUID
	RoleSystem                    bool
	RoleDefinitionVersion         int64
	Permissions                   []Permission
	LoginCapable                  bool
}

func (u *InvitationAcceptanceUsecase) WithBootstrap(policy TenantAdminLoginPolicy, catalog PermissionCatalog) *InvitationAcceptanceUsecase {
	u.bootstrapLoginPolicy, u.bootstrapCatalog = policy, catalog
	return u
}

func validateInvitationBootstrap(s *InvitationBootstrapState, email string, roles []uuid.UUID, catalog PermissionCatalog) error {
	if s == nil {
		return nil
	}
	if catalog == nil {
		return ErrAuthenticationDependency
	}
	if s.ID.Version() != 7 || s.Version < 1 || s.AccessVersion < 1 || s.Source != "core" || s.Status != "waiting_for_principal_verification" || s.Superseded || s.IntendedEmail != email || !s.LoginCapable || !s.RoleSystem || s.RoleDefinitionVersion != 1 || len(roles) != 1 || roles[0] != s.RoleID {
		return ErrInvitationConflict
	}
	expected := catalog.Permissions(PermissionScopeTenant)
	if len(expected) == 0 || len(expected) != len(s.Permissions) {
		return ErrInvitationConflict
	}
	seen := map[Permission]bool{}
	for _, p := range s.Permissions {
		if p.Scope != PermissionScopeTenant || !catalog.Contains(p) || seen[p] {
			return ErrInvitationConflict
		}
		seen[p] = true
	}
	for _, p := range expected {
		if !seen[p] {
			return ErrInvitationConflict
		}
	}
	return nil
}

func (u *InvitationAcceptanceUsecase) completeBootstrap(ctx context.Context, tx InvitationAcceptanceTransaction, cap InvitationAcceptanceCapability, s *InvitationBootstrapState, m InvitationAcceptedMembership, accepted SecurityAuditEvent, now time.Time) error {
	if s == nil {
		return nil
	}
	id, err := u.newID()
	if err != nil {
		return err
	}
	audit := accepted
	audit.ID = id
	audit.Action = "iam.bootstrap.completed"
	audit.TargetType = "tenant_bootstrap"
	audit.TargetID = s.ID
	audit.TargetVersion = s.Version + 1
	audit.Reason = "BOOTSTRAP_INVITATION_ACCEPTED"
	audit.DecisionID = id.String()
	return tx.CompleteBootstrap(ctx, cap, *s, m, audit, now)
}
