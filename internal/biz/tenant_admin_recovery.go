package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

var (
	ErrRecoveryInvalid  = errors.New("recovery request is invalid")
	ErrRecoveryConflict = errors.New("recovery state conflicts with the approved intent")
	ErrRecoveryNotFound = errors.New("recovery operation was not found")
)

type RecoveryStatus string

const (
	RecoveryPendingApproval          RecoveryStatus = "pending_approval"
	RecoveryApproved                 RecoveryStatus = "approved"
	RecoveryExecuted                 RecoveryStatus = "executed"
	RecoveryExpired                  RecoveryStatus = "expired"
	RecoveryRejected                 RecoveryStatus = "rejected"
	recoveryApprovalLifetime                        = time.Hour
	recoveryReauthenticationLifetime                = 15 * time.Minute
)

// This is the durable, non-secret result. Tokens and supplied client clocks are
// deliberately absent from both the operation and its idempotent receipt.
type TenantAdminRecoveryOperation struct {
	ID, TenantID, TargetPrincipalID, RequesterPrincipalID, ApproverPrincipalID uuid.UUID
	ApprovalReference, ReasonCode, PayloadFingerprint                          string
	Status                                                                     RecoveryStatus
	Version                                                                    int64
	CreatedAt, UpdatedAt, ExpiresAt, ApprovedAt, ExecutedAt                    time.Time
	MembershipID                                                               uuid.UUID
}
type RequestTenantAdminRecoveryCommand struct {
	TenantID, TargetPrincipalID                    uuid.UUID
	ReasonCode, PayloadFingerprint, IdempotencyKey string
}
type ApproveTenantAdminRecoveryCommand struct {
	OperationID                                           uuid.UUID
	ApprovalReference, PayloadFingerprint, IdempotencyKey string
}
type ExecuteTenantAdminRecoveryCommand struct {
	OperationID                                                                  uuid.UUID
	ApprovalReference, PayloadFingerprint, ReauthenticationProof, IdempotencyKey string
}

// CanonicalRestoreTenantAdminFingerprint hashes UTF-8 of four lines, with no
// trailing newline: fixed version, canonical Tenant UUID, target Human UUID,
// and an ASCII stable reason code. Each field has one unambiguous position.
func CanonicalRestoreTenantAdminFingerprint(tenant, target uuid.UUID, reason string) (string, error) {
	return canonicalAdministratorRecoveryFingerprint("ani.iam.restore-tenant-admin/v1", tenant, target, reason)
}
func canonicalAdministratorRecoveryFingerprint(domain string, tenant, target uuid.UUID, reason string) (string, error) {
	if tenant == uuid.Nil || target == uuid.Nil || tenant.Version() != 7 || target.Version() != 7 || len(reason) < 1 || len(reason) > 128 || reason != strings.TrimSpace(reason) {
		return "", ErrRecoveryInvalid
	}
	for _, r := range reason {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return "", ErrRecoveryInvalid
		}
	}
	raw := strings.Join([]string{domain, tenant.String(), target.String(), reason}, "\n")
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(raw))), nil
}
func validRecoveryReference(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

type TenantAdminRecoveryRepository interface {
	GetRecovery(context.Context, PlatformCapability, uuid.UUID) (TenantAdminRecoveryOperation, error)
	CreateRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation) error
	ApproveRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation, int64) error
	CompleteRecovery(context.Context, PlatformCapability, TenantAdminRecoveryOperation, int64) error
	ValidateRecoveryTarget(context.Context, PlatformCapability, uuid.UUID, uuid.UUID, TenantAdminLoginPolicy, time.Time) error
	RecoveryReauthentication(context.Context, PlatformCapability, AccessTokenClaims) (time.Time, error)
	AppendRecoveryEffectAudit(context.Context, PlatformCapability, TenantAdminRecoveryOperation, SecurityAuditEvent) error
	RestoreRecoveryAdministrator(context.Context, PlatformCapability, TenantAdminRecoveryOperation, uuid.UUID, uuid.UUID, time.Time) (TenantMembershipRecord, error)
}

func (u *PlatformAdministrationUsecase) WithTenantAdminRecovery(verifier AccessCredentialVerifier, policy TenantAdminLoginPolicy) *PlatformAdministrationUsecase {
	u.recoveryVerifier = verifier
	u.recoveryLoginPolicy = policy
	return u
}
func tenantAdminRecoveryResult(r TenantAdminRecoveryOperation) PlatformMutationResult {
	return PlatformMutationResult{Recovery: &r, TargetID: r.ID, TargetVersion: r.Version}
}
func (u *PlatformAdministrationUsecase) RequestTenantAdminRecovery(ctx context.Context, cap PlatformCapability, c RequestTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
	fingerprint, err := CanonicalRestoreTenantAdminFingerprint(c.TenantID, c.TargetPrincipalID, c.ReasonCode)
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
	return u.mutate(ctx, cap, "requestRestoreTenantAdmin", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		if err := tx.ValidateRecoveryTarget(ctx, cap, c.TenantID, c.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, err
		}
		id, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		r := TenantAdminRecoveryOperation{ID: id, TenantID: c.TenantID, TargetPrincipalID: c.TargetPrincipalID, RequesterPrincipalID: cap.claims.Subject, ReasonCode: c.ReasonCode, PayloadFingerprint: fingerprint, Status: RecoveryPendingApproval, Version: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(recoveryApprovalLifetime)}
		return tenantAdminRecoveryResult(r), tx.CreateRecovery(ctx, cap, r)
	})
}
func (u *PlatformAdministrationUsecase) ApproveTenantAdminRecovery(ctx context.Context, cap PlatformCapability, c ApproveTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
	if c.OperationID == uuid.Nil || !validRecoveryReference(c.ApprovalReference) {
		return PlatformMutationResult{}, ErrRecoveryInvalid
	}
	fields := struct {
		ID                     uuid.UUID
		Reference, Fingerprint string
	}{c.OperationID, c.ApprovalReference, c.PayloadFingerprint}
	return u.mutate(ctx, cap, "approveRestoreTenantAdmin", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		r, err := tx.GetRecovery(ctx, cap, c.OperationID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if r.RequesterPrincipalID == cap.claims.Subject {
			return PlatformMutationResult{}, ErrPlatformAdministrationDenied
		}
		if r.Status != RecoveryPendingApproval || !now.Before(r.ExpiresAt) || r.PayloadFingerprint != c.PayloadFingerprint {
			return PlatformMutationResult{}, ErrRecoveryConflict
		}
		if err = tx.ValidateRecoveryTarget(ctx, cap, r.TenantID, r.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, err
		}
		previous := r.Version
		r.Version++
		r.Status = RecoveryApproved
		r.ApproverPrincipalID = cap.claims.Subject
		r.ApprovalReference = c.ApprovalReference
		r.ApprovedAt = now
		r.UpdatedAt = now
		r.ExpiresAt = now.Add(recoveryApprovalLifetime)
		return tenantAdminRecoveryResult(r), tx.ApproveRecovery(ctx, cap, r, previous)
	})
}
func (u *PlatformAdministrationUsecase) ExecuteTenantAdminRecovery(ctx context.Context, cap PlatformCapability, c ExecuteTenantAdminRecoveryCommand) (PlatformMutationResult, error) {
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
	return u.mutate(ctx, cap, "executeRestoreTenantAdmin", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		if !now.Before(proof.ExpiresAt) {
			return PlatformMutationResult{}, ErrInvalidCredential
		}
		r, err := tx.GetRecovery(ctx, cap, c.OperationID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if r.Status != RecoveryApproved || !now.Before(r.ExpiresAt) || r.ApprovalReference != c.ApprovalReference || r.PayloadFingerprint != c.PayloadFingerprint || r.RequesterPrincipalID == r.ApproverPrincipalID {
			return PlatformMutationResult{}, ErrRecoveryConflict
		}
		if err = tx.ValidateRecoveryTarget(ctx, cap, r.TenantID, r.TargetPrincipalID, u.recoveryLoginPolicy, now); err != nil {
			return PlatformMutationResult{}, err
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
		member, err := tx.RestoreRecoveryAdministrator(ctx, cap, r, memberID, bindingID, now)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		previous := r.Version
		r.Version++
		r.Status = RecoveryExecuted
		r.ExecutedAt = now
		r.UpdatedAt = now
		r.MembershipID = member.Membership.ID
		if err = tx.CompleteRecovery(ctx, cap, r, previous); err != nil {
			return PlatformMutationResult{}, err
		}
		auditID, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		audit := newPlatformAdministrationAudit(cap, "tenant_membership", auditID, member.Membership.ID, member.Membership.Version, now)
		audit.Boundary = AuditBoundaryTenant
		audit.Action = "iam.recovery.tenant-admin.restored"
		audit.Reason = AuditReason(r.ReasonCode)
		// This specialized repository derives Tenant scope only from its locked,
		// approved recovery record when appending the Tenant recovery effect.
		if err = tx.AppendRecoveryEffectAudit(ctx, cap, r, audit); err != nil {
			return PlatformMutationResult{}, err
		}
		result := tenantAdminRecoveryResult(r)
		result.RestoredMembership = &member
		return result, nil
	})
}
