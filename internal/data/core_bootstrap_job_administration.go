package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func toCoreBootstrapJob(v sqlcgen.ListCurrentCoreBootstrapJobsRow) biz.CoreBootstrapJob {
	return biz.CoreBootstrapJob{Kind: biz.CoreBootstrapJobKind(v.Kind), Generation: v.Generation, State: v.State, AttemptCount: v.AttemptCount, CycleStartAttempt: v.CycleStartAttempt, LastError: v.LastError, AvailableAt: v.AvailableAt.Time, RecoveryID: uuid.UUID(v.RecoveryID.Bytes)}
}
func (t *platformAdministrationTransaction) RetryPlatformBootstrapJob(ctx context.Context, cap biz.PlatformCapability, w biz.CoreBootstrapWork, d biz.CoreBootstrapJobRecovery, a biz.CoreBootstrapExecutionAuthorization) (biz.CoreBootstrapJob, error) {
	var empty biz.CoreBootstrapJob
	if err := recoveryCapability(cap, "retryTenantIAMBootstrapJob"); err != nil {
		return empty, err
	}
	b := t.bootstrap
	if b == nil || b.work == nil || b.authority == nil || *b.authority != a || w.Source != b.work.Source || w.Version != b.work.Version || d.RecoveryID.Version() != 7 {
		return empty, biz.ErrCoreBootstrapConflict
	}
	claims, _, err := cap.CredentialBinding()
	if err != nil {
		return empty, err
	}
	job, err := t.q.LockCoreBootstrapJob(ctx, sqlcgen.LockCoreBootstrapJobParams{TenantID: b.tenant, OperationID: w.Source.OperationID, Kind: string(d.Job.Kind), Generation: d.Job.Generation, Producer: b.producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, biz.ErrCoreBootstrapNotFound
	}
	if err != nil {
		return empty, mapPostgresError("lock original Bootstrap recovery job", err, nil)
	}
	if job.State != "attention_required" || job.AttemptCount != d.Job.AttemptCount || job.CycleStartAttempt != d.Job.CycleStartAttempt || uuid.UUID(job.RecoveryID.Bytes) != d.Job.RecoveryID || job.SourceEventID != w.Source.EventID {
		return empty, biz.ErrCoreBootstrapConflict
	}
	if err := b.checkBootstrapLifecycle(ctx); err != nil {
		return empty, err
	}
	if err = t.q.AppendCoreBootstrapJobRecovery(ctx, sqlcgen.AppendCoreBootstrapJobRecoveryParams{TenantID: b.tenant, ID: d.RecoveryID, OperationID: w.Source.OperationID, Kind: job.Kind, Generation: job.Generation, SourceEventID: job.SourceEventID, Producer: b.producer, PayloadFingerprint: w.Source.Fingerprint, PreviousAttempt: job.AttemptCount, PreviousRecoveryID: job.RecoveryID, ActorID: claims.Subject, ReasonCode: d.ReasonCode}); err != nil {
		return empty, mapPostgresError("append immutable Bootstrap job recovery", err, nil)
	}
	if err = t.appendBootstrapBrokerApproval(ctx, cap, d.Job.Kind, d.Job.Generation, d.RecoveryID, false, d.ReasonCode); err != nil {
		return empty, err
	}
	n, err := t.q.RetryCoreBootstrapJob(ctx, sqlcgen.RetryCoreBootstrapJobParams{TenantID: b.tenant, OperationID: w.Source.OperationID, Kind: job.Kind, Generation: job.Generation, Producer: b.producer, ExpectedAttempt: job.AttemptCount, RecoveryID: requiredPGUUID(d.RecoveryID)})
	if err != nil {
		return empty, mapPostgresError("resume original Bootstrap job", err, nil)
	}
	if n != 1 {
		return empty, biz.ErrCoreBootstrapConflict
	}
	rows, err := t.q.ListCurrentCoreBootstrapJobs(ctx, sqlcgen.ListCurrentCoreBootstrapJobsParams{TenantID: b.tenant, OperationID: w.Source.OperationID, Producer: b.producer})
	if err != nil {
		return empty, mapPostgresError("read recovered Bootstrap job", err, nil)
	}
	for _, row := range rows {
		if row.Kind == job.Kind && row.Generation == job.Generation {
			return toCoreBootstrapJob(row), nil
		}
	}
	return empty, biz.ErrCoreBootstrapConflict
}
