package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCoreBootstrapNotFound = errors.New("Core Bootstrap operation was not found")

// Platform administration addresses a stored operation, never an arbitrary
// TenantScope. Its repository derives Tenant scope from that locked record.
type CoreBootstrapAdministrationRepository interface {
	AuthorizePlatformBootstrapExecution(context.Context, PlatformCapability, CoreBootstrapSource, CoreBootstrapAuthorizer) (CoreBootstrapExecutionAuthorization, error)
	RetryPlatformBootstrapJob(context.Context, PlatformCapability, CoreBootstrapWork, CoreBootstrapJobRecovery, CoreBootstrapExecutionAuthorization) (CoreBootstrapJob, error)
	LoadPlatformBootstrap(context.Context, PlatformCapability, uuid.UUID, string) (CoreBootstrapWork, error)
	RecheckPlatformBootstrapExecution(context.Context, PlatformCapability, CoreBootstrapExecutionAuthorization) error
	ReissuePlatformBootstrapInvitation(context.Context, PlatformCapability, CoreBootstrapWork, CoreBootstrapInvitationDraft, CoreBootstrapExecutionAuthorization) error
	AppendPlatformBootstrapEffectAudit(context.Context, PlatformCapability, SecurityAuditEvent) error
}

type TenantBootstrapOperation struct {
	Jobs         []CoreBootstrapJob
	ID, TenantID uuid.UUID
	Status       string
	Version      int64
	Invitation   *TenantInvitation
}
type ReissueTenantBootstrapCommand struct {
	OperationID                uuid.UUID
	ExpectedVersion            int64
	ReasonCode, IdempotencyKey string
}

func (u *PlatformAdministrationUsecase) WithCoreBootstrapAdministration(producer string, authority CoreBootstrapAuthorizer) *PlatformAdministrationUsecase {
	u.bootstrapProducer, u.bootstrapAuthority = producer, authority
	return u
}

func bootstrapAdministrationView(w CoreBootstrapWork) TenantBootstrapOperation {
	v := TenantBootstrapOperation{Jobs: append([]CoreBootstrapJob{}, w.Jobs...), ID: w.Source.OperationID, TenantID: w.Source.TenantID, Status: w.Status, Version: w.Version}
	if w.Invitation != nil {
		i := w.Invitation.Invitation
		i.RoleIDs = append([]uuid.UUID(nil), i.RoleIDs...)
		v.Invitation = &i
	}
	return v
}

func (u *PlatformAdministrationUsecase) GetTenantBootstrap(ctx context.Context, cap PlatformCapability, id uuid.UUID) (TenantBootstrapOperation, error) {
	var result TenantBootstrapOperation
	if id.Version() != 7 {
		return result, ErrCoreBootstrapInvalid
	}
	if u == nil || !ValidCoreProjectionProducer(u.bootstrapProducer) {
		return result, ErrCoreBootstrapAuthority
	}
	version := int64(1)
	err := u.within(ctx, cap, "getTenantIAMBootstrap", id, &version, func(tx PlatformAdministrationTransaction) error {
		w, err := tx.LoadPlatformBootstrap(ctx, cap, id, u.bootstrapProducer)
		if err != nil {
			return err
		}
		result = bootstrapAdministrationView(w)
		version = w.Version
		return nil
	})
	return result, err
}

func (u *PlatformAdministrationUsecase) ReissueTenantBootstrapInvitation(ctx context.Context, cap PlatformCapability, c ReissueTenantBootstrapCommand) (PlatformMutationResult, error) {
	if c.OperationID.Version() != 7 || c.ExpectedVersion < 1 || !validBootstrapReissueReason(c.ReasonCode) || cap.reason != c.ReasonCode {
		return PlatformMutationResult{}, ErrCoreBootstrapInvalid
	}
	if u == nil || !ValidCoreProjectionProducer(u.bootstrapProducer) || u.bootstrapAuthority == nil || u.invitationSecrets == nil {
		return PlatformMutationResult{}, ErrCoreBootstrapAuthority
	}
	var work CoreBootstrapWork
	var authority CoreBootstrapExecutionAuthorization
	precondition := func(tx PlatformAdministrationTransaction, _ time.Time) error {
		var err error
		work, err = tx.LoadPlatformBootstrap(ctx, cap, c.OperationID, u.bootstrapProducer)
		if err != nil {
			return err
		}
		if _, err = work.Intent.CanonicalPayload(); err != nil {
			return err
		}
		if work.Superseded {
			return ErrCoreBootstrapConflict
		}
		authority, err = tx.AuthorizePlatformBootstrapExecution(ctx, cap, work.Source, u.bootstrapAuthority)
		if err != nil {
			return err
		}
		if !validCoreBootstrapExecution(authority, work.Source, u.clock.Now()) {
			return ErrCoreBootstrapAuthority
		}
		return tx.RecheckPlatformBootstrapExecution(ctx, cap, authority)
	}
	return u.mutateWithPrecondition(ctx, cap, "reissueTenantIAMBootstrapInvitation", c.IdempotencyKey, c, precondition, func(tx PlatformAdministrationTransaction, _ time.Time) (PlatformMutationResult, error) {
		if work.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		if work.Result == nil || work.Invitation == nil || !work.AccessExists || work.AccessStatus != TenantAccessStatusBootstrapPending || (work.Status != "waiting_for_principal_verification" && work.Status != "attention_required") {
			return PlatformMutationResult{}, ErrCoreBootstrapConflict
		}
		if !work.Lifecycle.Fresh {
			return PlatformMutationResult{}, ErrTenantLifecycleStale
		}
		if work.Lifecycle.Status != "active" {
			return PlatformMutationResult{}, ErrTenantLifecycleBlocked
		}
		inv := work.Invitation.Invitation
		if inv.Status != InvitationPending && inv.Status != InvitationExpired {
			return PlatformMutationResult{}, ErrInvitationConflict
		}
		if inv.ID != work.Result.InvitationID || inv.NormalizedEmail != work.Intent.NormalizedEmail || inv.Locale != work.Intent.Locale || len(inv.RoleIDs) != 1 || inv.RoleIDs[0] != work.Result.RoleID {
			return PlatformMutationResult{}, ErrCoreBootstrapConflict
		}
		delivery, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		audit, err := u.newAdministrationID()
		if err != nil || audit == delivery {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		// The source is returned by the capability-scoped, locked repository;
		// this scope is used only to encode the already-bound Invitation token.
		scope, err := NewTenantScope(work.Source.TenantID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		token, err := (&TenantInvitationUsecase{secrets: u.invitationSecrets}).token(scope, inv.ID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		digest := sha256.Sum256([]byte(token))
		if digest == work.Invitation.TokenDigest {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		now := u.clock.Now().UTC()
		inv.Status = InvitationPending
		inv.Version++
		inv.DeliveryGeneration++
		inv.ExpiresAt = now.Add(invitationLifetime)
		inv.UpdatedAt = now
		inv.DeliveryStatus = "pending"
		inv.DeliveryAttemptCount = 0
		if u.catalog == nil || u.catalog.catalog == nil {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		draft := CoreBootstrapInvitationDraft{RoleID: work.Result.RoleID, DeliveryID: delivery, AuditID: audit, Invitation: TenantInvitationState{Invitation: inv, TokenDigest: digest}, Token: token, Permissions: u.catalog.catalog.Permissions(PermissionScopeTenant)}
		if err = tx.ReissuePlatformBootstrapInvitation(ctx, cap, work, draft, authority); err != nil {
			return PlatformMutationResult{}, err
		}
		event := newPlatformAdministrationAudit(cap, "invitation", audit, inv.ID, inv.Version, now)
		event.Boundary = AuditBoundaryTenant
		event.Action = "iam.bootstrap.invitation.reissued"
		if err = tx.AppendPlatformBootstrapEffectAudit(ctx, cap, event); err != nil {
			return PlatformMutationResult{}, err
		}
		if err = tx.RecheckPlatformBootstrapExecution(ctx, cap, authority); err != nil {
			return PlatformMutationResult{}, err
		}
		view := bootstrapAdministrationView(work)
		view.Status = "waiting_for_principal_verification"
		view.Version++
		view.Invitation = &inv
		return PlatformMutationResult{Bootstrap: &view, TargetID: c.OperationID, TargetVersion: view.Version}, nil
	})
}

func validBootstrapReissueReason(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}
