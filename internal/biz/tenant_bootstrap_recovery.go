package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

func CanonicalRecoveryBootstrapFingerprint(tenant, target uuid.UUID, reason string) (string, error) {
	return canonicalAdministratorRecoveryFingerprint("ani.iam.recovery-bootstrap/v1", tenant, target, reason)
}

type BootstrapCancelledInvitation struct {
	ID      uuid.UUID
	Version int64
}
type BootstrapRecoveryEffects struct {
	Membership           TenantMembershipRecord
	Access               TenantAccess
	CancelledInvitations []BootstrapCancelledInvitation
}
type TenantBootstrapRecoveryRepository interface {
	GetBootstrapRecovery(context.Context, PlatformCapability, uuid.UUID) (TenantAdminRecoveryOperation, error)
	CreateBootstrapRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation) error
	ApproveBootstrapRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation, int64) error
	CompleteBootstrapRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation, int64) error
	ValidateBootstrapRecoveryTarget(context.Context, PlatformCapability, uuid.UUID, uuid.UUID, TenantAdminLoginPolicy, time.Time) error
	ApplyBootstrapRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation, uuid.UUID, uuid.UUID, uuid.UUID, []Permission, time.Time) (BootstrapRecoveryEffects, error)
	AppendBootstrapRecoveryEffectAudit(context.Context, PlatformCapability, TenantAdminRecoveryOperation, SecurityAuditEvent) error
}

func (u *PlatformAdministrationUsecase) RequestTenantBootstrapRecovery(ctx context.Context, cap PlatformCapability, c RequestTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
	fingerprint, err := CanonicalRecoveryBootstrapFingerprint(c.TenantID, c.TargetPrincipalID, c.ReasonCode)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	if fingerprint != c.PayloadFingerprint {
		return PlatformMutationResult{}, ErrRecoveryConflict
	}
	fields := struct {
		Tenant, Target      uuid.UUID
		Reason, Fingerprint string
	}{c.TenantID, c.TargetPrincipalID, c.ReasonCode, fingerprint}
	return u.mutate(ctx, cap, "requestRecoveryBootstrap", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		if err := tx.ValidateBootstrapRecoveryTarget(ctx, cap, c.TenantID, c.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, tenantBoundAuthenticationError(c.TenantID, err)
		}
		id, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		r := TenantAdminRecoveryOperation{ID: id, TenantID: c.TenantID, TargetPrincipalID: c.TargetPrincipalID, RequesterPrincipalID: cap.claims.Subject, ReasonCode: c.ReasonCode, PayloadFingerprint: fingerprint, Status: RecoveryPendingApproval, Version: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(recoveryApprovalLifetime)}
		return tenantAdminRecoveryResult(r), tx.CreateBootstrapRecovery(ctx, cap, r)
	})
}
func (u *PlatformAdministrationUsecase) ApproveTenantBootstrapRecovery(ctx context.Context, cap PlatformCapability, c ApproveTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
	if c.OperationID == uuid.Nil || !validRecoveryReference(c.ApprovalReference) {
		return PlatformMutationResult{}, ErrRecoveryInvalid
	}
	fields := struct {
		ID                     uuid.UUID
		Reference, Fingerprint string
	}{c.OperationID, c.ApprovalReference, c.PayloadFingerprint}
	return u.mutate(ctx, cap, "approveRecoveryBootstrap", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		r, err := tx.GetBootstrapRecovery(ctx, cap, c.OperationID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if r.RequesterPrincipalID == cap.claims.Subject {
			return PlatformMutationResult{}, ErrPlatformAdministrationDenied
		}
		if r.Status != RecoveryPendingApproval || !now.Before(r.ExpiresAt) || r.PayloadFingerprint != c.PayloadFingerprint {
			return PlatformMutationResult{}, ErrRecoveryConflict
		}
		if err = tx.ValidateBootstrapRecoveryTarget(ctx, cap, r.TenantID, r.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, tenantBoundAuthenticationError(r.TenantID, err)
		}
		previous := r.Version
		r.Version++
		r.Status = RecoveryApproved
		r.ApproverPrincipalID = cap.claims.Subject
		r.ApprovalReference = c.ApprovalReference
		r.ApprovedAt = now
		r.UpdatedAt = now
		r.ExpiresAt = now.Add(recoveryApprovalLifetime)
		return tenantAdminRecoveryResult(r), tx.ApproveBootstrapRecovery(ctx, cap, r, previous)
	})
}
func (u *PlatformAdministrationUsecase) ExecuteTenantBootstrapRecovery(ctx context.Context, cap PlatformCapability, c ExecuteTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
	if c.OperationID == uuid.Nil || !validRecoveryReference(c.ApprovalReference) || len(c.ReauthenticationProof) == 0 || len(c.ReauthenticationProof) > 2048 {
		return PlatformMutationResult{}, ErrRecoveryInvalid
	}
	if u == nil || u.recoveryVerifier == nil {
		return PlatformMutationResult{}, ErrAuthenticationDependency
	}
	proof, err := u.recoveryVerifier.Verify(ctx, c.ReauthenticationProof)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return PlatformMutationResult{}, err
		}
		return PlatformMutationResult{}, ErrInvalidCredential
	}
	if proof.Subject != cap.claims.Subject || proof.Boundary != AccessBoundaryPlatform || proof.Audience != AudienceBoss || proof.TenantID != uuid.Nil || !containsHumanAuthenticationMethod(proof.AuthnMethods) {
		return PlatformMutationResult{}, ErrPlatformAdministrationDenied
	}
	fields := struct {
		ID                     uuid.UUID
		Reference, Fingerprint string
	}{c.OperationID, c.ApprovalReference, c.PayloadFingerprint}
	return u.mutate(ctx, cap, "executeRecoveryBootstrap", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		if !now.Before(proof.ExpiresAt) {
			return PlatformMutationResult{}, ErrInvalidCredential
		}
		r, err := tx.GetBootstrapRecovery(ctx, cap, c.OperationID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if r.Status != RecoveryApproved || !now.Before(r.ExpiresAt) || r.ApprovalReference != c.ApprovalReference || r.PayloadFingerprint != c.PayloadFingerprint || r.RequesterPrincipalID == r.ApproverPrincipalID {
			return PlatformMutationResult{}, ErrRecoveryConflict
		}
		if err = tx.ValidateBootstrapRecoveryTarget(ctx, cap, r.TenantID, r.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, tenantBoundAuthenticationError(r.TenantID, err)
		}
		// Tenant/Principal locks may have waited. Recheck actual authentication
		// and approval time after those locks, before producing any effect.
		now = u.clock.Now().UTC()
		if !now.Before(proof.ExpiresAt) {
			return PlatformMutationResult{}, ErrInvalidCredential
		}
		if !now.Before(r.ExpiresAt) {
			return PlatformMutationResult{}, ErrRecoveryConflict
		}
		authenticatedAt, err := tx.RecoveryReauthentication(ctx, cap, proof)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if authenticatedAt.IsZero() || authenticatedAt.After(now) || now.Sub(authenticatedAt) > recoveryReauthenticationLifetime {
			return PlatformMutationResult{}, ErrOIDCReauthenticationRequired
		}
		memberID, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		bindingID, err := u.newAdministrationID()
		if err != nil || bindingID == memberID {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		roleID, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if u.catalog == nil || u.catalog.catalog == nil {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		effects, err := tx.ApplyBootstrapRecovery(ctx, cap, r, roleID, memberID, bindingID, u.catalog.catalog.Permissions(PermissionScopeTenant), now)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		member := effects.Membership
		previous := r.Version
		r.Version++
		r.Status = RecoveryExecuted
		r.ExecutedAt = now
		r.UpdatedAt = now
		r.MembershipID = member.Membership.ID
		if err = tx.CompleteBootstrapRecovery(ctx, cap, r, previous); err != nil {
			return PlatformMutationResult{}, err
		}
		auditID, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		audit := newPlatformAdministrationAudit(cap, "tenant_membership", auditID, member.Membership.ID, member.Membership.Version, now)
		audit.Boundary = AuditBoundaryTenant
		audit.Action = "iam.recovery.bootstrap.completed"
		audit.Reason = AuditReason(r.ReasonCode)
		// This specialized repository derives Tenant scope only from its locked,
		// approved recovery record when appending the Tenant recovery effect.
		if err = tx.AppendBootstrapRecoveryEffectAudit(ctx, cap, r, audit); err != nil {
			return PlatformMutationResult{}, err
		}
		for _, invitation := range effects.CancelledInvitations {
			id, err := u.newAdministrationID()
			if err != nil {
				return PlatformMutationResult{}, err
			}
			audit := newPlatformAdministrationAudit(cap, "invitation", id, invitation.ID, invitation.Version, now)
			audit.Boundary = AuditBoundaryTenant
			audit.Action = "iam.invitation.cancelled"
			audit.Reason = "BOOTSTRAP_IDENTITY_REPLACED"
			if err = tx.AppendBootstrapRecoveryEffectAudit(ctx, cap, r, audit); err != nil {
				return PlatformMutationResult{}, err
			}
		}
		result := tenantAdminRecoveryResult(r)
		result.RestoredMembership = &member
		result.Access = &effects.Access
		return result, nil
	})
}
