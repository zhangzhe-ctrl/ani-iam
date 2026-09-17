package biz

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type CoreBootstrapJob struct {
	Kind                            CoreBootstrapJobKind
	Generation                      int64
	State                           string
	AttemptCount, CycleStartAttempt int32
	LastError                       string
	AvailableAt                     time.Time
	RecoveryID                      uuid.UUID
}
type RetryTenantBootstrapJobCommand struct {
	OperationID                 uuid.UUID
	Kind                        CoreBootstrapJobKind
	Generation, ExpectedVersion int64
	ExpectedAttempt             int32
	ReasonCode, IdempotencyKey  string
}
type CoreBootstrapJobRecovery struct {
	Job        CoreBootstrapJob
	RecoveryID uuid.UUID
	ReasonCode string
}

func (u *PlatformAdministrationUsecase) RetryTenantBootstrapJob(ctx context.Context, cap PlatformCapability, c RetryTenantBootstrapJobCommand) (PlatformMutationResult, error) {
	if c.OperationID.Version() != 7 || c.ExpectedVersion < 1 || c.ExpectedAttempt < 1 || c.Generation < 1 || (c.Kind != CoreBootstrapInitialize && c.Kind != CoreBootstrapExpire) || (c.Kind == CoreBootstrapInitialize && c.Generation != 1) || !validBootstrapReissueReason(c.ReasonCode) || cap.reason != c.ReasonCode {
		return PlatformMutationResult{}, ErrCoreBootstrapInvalid
	}
	if u == nil || !ValidCoreProjectionProducer(u.bootstrapProducer) || u.bootstrapAuthority == nil {
		return PlatformMutationResult{}, ErrCoreBootstrapAuthority
	}
	var work CoreBootstrapWork
	var authority CoreBootstrapExecutionAuthorization
	before := func(tx PlatformAdministrationTransaction, _ time.Time) error {
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
	return u.mutateWithPrecondition(ctx, cap, "retryTenantIAMBootstrapJob", c.IdempotencyKey, c, before, func(tx PlatformAdministrationTransaction, _ time.Time) (PlatformMutationResult, error) {
		if work.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		if !work.Lifecycle.Fresh {
			return PlatformMutationResult{}, ErrTenantLifecycleStale
		}
		if work.Lifecycle.Status != "active" {
			return PlatformMutationResult{}, ErrTenantLifecycleBlocked
		}
		index := -1
		for i, job := range work.Jobs {
			if job.Kind == c.Kind && job.Generation == c.Generation {
				index = i
				break
			}
		}
		if index < 0 {
			return PlatformMutationResult{}, ErrCoreBootstrapNotFound
		}
		job := work.Jobs[index]
		if job.State != "attention_required" || job.AttemptCount != c.ExpectedAttempt {
			return PlatformMutationResult{}, ErrCoreBootstrapConflict
		}
		if c.Kind == CoreBootstrapInitialize {
			if work.Status != "pending" || work.Result != nil || work.Invitation != nil {
				return PlatformMutationResult{}, ErrCoreBootstrapConflict
			}
		} else if work.Status != "waiting_for_principal_verification" || work.Invitation == nil || work.Invitation.Invitation.DeliveryGeneration != c.Generation || (work.Invitation.Invitation.Status != InvitationPending && work.Invitation.Invitation.Status != InvitationExpired) {
			return PlatformMutationResult{}, ErrCoreBootstrapConflict
		}
		recovery, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		next, err := tx.RetryPlatformBootstrapJob(ctx, cap, work, CoreBootstrapJobRecovery{Job: job, RecoveryID: recovery, ReasonCode: c.ReasonCode}, authority)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		// Technical recovery may precede TenantAccess creation. The platform
		// mutation wrapper commits its audit and receipt with this recovery.
		if err = tx.RecheckPlatformBootstrapExecution(ctx, cap, authority); err != nil {
			return PlatformMutationResult{}, err
		}
		view := bootstrapAdministrationView(work)
		view.Jobs[index] = next
		return PlatformMutationResult{Bootstrap: &view, TargetID: c.OperationID, TargetVersion: work.Version}, nil
	})
}
