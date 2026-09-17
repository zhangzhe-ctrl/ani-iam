package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

type coreBootstrapQueue struct {
	data     *Data
	producer string
}

func NewCoreBootstrapQueue(d *Data, producer string) (biz.CoreBootstrapQueue, error) {
	if d == nil || d.pool == nil || !biz.ValidCoreProjectionProducer(producer) {
		return nil, biz.ErrCoreBootstrapInvalid
	}
	return &coreBootstrapQueue{d, producer}, nil
}
func validCoreBootstrapKind(k string) bool {
	return k == string(biz.CoreBootstrapInitialize) || k == string(biz.CoreBootstrapExpire)
}
func (r *coreBootstrapQueue) withJob(ctx context.Context, tenant, operation uuid.UUID, kind string, fn func(*sqlcgen.Queries, sqlcgen.TenantBootstrapOperation) error) error {
	if tenant.Version() != 7 || operation.Version() != 7 || !validCoreBootstrapKind(kind) {
		return biz.ErrCoreBootstrapInvalid
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapPostgresError("begin Bootstrap schedule", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	if err = q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
		return mapPostgresError("lock Bootstrap schedule Tenant", err, nil)
	}
	op, err := q.GetCoreBootstrapOperation(ctx, sqlcgen.GetCoreBootstrapOperationParams{TenantID: tenant, ID: operation})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrCoreBootstrapConflict
	}
	if err != nil {
		return mapPostgresError("lock Bootstrap schedule operation", err, nil)
	}
	if op.SourceKind != "core" {
		return biz.ErrCoreBootstrapConflict
	}
	if err = fn(q, op); err != nil {
		return err
	}
	return mapPostgresError("commit Bootstrap scheduling transaction", tx.Commit(ctx), nil)
}
func (r *coreBootstrapQueue) key(tenant, op uuid.UUID, kind string, generation int64) sqlcgen.LockCoreBootstrapJobParams {
	return sqlcgen.LockCoreBootstrapJobParams{TenantID: tenant, OperationID: op, Kind: kind, Generation: generation, Producer: r.producer}
}
func coreBootstrapClaim(j sqlcgen.LockCoreBootstrapJobRow) biz.CoreBootstrapJobClaim {
	return biz.CoreBootstrapJobClaim{TenantID: j.TenantID, OperationID: j.OperationID, LeaseID: uuid.UUID(j.LeaseID.Bytes), Kind: biz.CoreBootstrapJobKind(j.Kind), Producer: j.Producer, CycleStartAttempt: j.CycleStartAttempt, RecoveryID: uuid.UUID(j.RecoveryID.Bytes), Attempt: j.AttemptCount, Generation: j.Generation, LeaseUntil: j.LeaseUntil.Time}
}
func (r *coreBootstrapQueue) finishJob(ctx context.Context, q *sqlcgen.Queries, j sqlcgen.LockCoreBootstrapJobRow, state, outcome, code string, available time.Time) error {
	if err := q.AppendCoreBootstrapAttempt(ctx, sqlcgen.AppendCoreBootstrapAttemptParams{TenantID: j.TenantID, OperationID: j.OperationID, Kind: j.Kind, Generation: j.Generation, AttemptNumber: j.AttemptCount, LeaseID: uuid.UUID(j.LeaseID.Bytes), Outcome: outcome, ErrorCode: code, StartedAt: j.LeaseStartedAt, RecoveryID: j.RecoveryID}); err != nil {
		return mapPostgresError("append immutable Bootstrap attempt", err, nil)
	}
	return mapPostgresError("finish Bootstrap schedule", q.FinishCoreBootstrapJob(ctx, sqlcgen.FinishCoreBootstrapJobParams{TenantID: j.TenantID, OperationID: j.OperationID, Kind: j.Kind, Generation: j.Generation, Producer: r.producer, State: state, AvailableAt: requiredTimestamptz(available), ErrorCode: code}), nil)
}

// terminalOrDeferred observes already committed business state. It cannot
// create an Invitation, activate Access, or replace current execution authority.
func coreBootstrapScheduleOutcome(ctx context.Context, q *sqlcgen.Queries, op sqlcgen.TenantBootstrapOperation, kind string, generation int64, now time.Time) (bool, time.Time, error) {
	if op.SupersededBy.Valid || op.Status == "succeeded" || op.Status == "attention_required" {
		return true, now, nil
	}
	if kind == "initialize" {
		return op.Status == "waiting_for_principal_verification", now, nil
	}
	if op.Status != "waiting_for_principal_verification" {
		return false, now, biz.ErrCoreBootstrapConflict
	}
	inv, err := q.ReadCoreBootstrapInvitation(ctx, sqlcgen.ReadCoreBootstrapInvitationParams{TenantID: op.TenantID, OperationID: op.ID})
	if err != nil {
		return false, now, mapPostgresError("read Bootstrap expiration schedule", err, nil)
	}
	return inv.DeliveryGeneration != generation, inv.ExpiresAt.Time, nil
}
func (r *coreBootstrapQueue) Claim(ctx context.Context) (biz.CoreBootstrapJobClaim, bool, error) {
	var result biz.CoreBootstrapJobClaim
	candidates, err := sqlcgen.New(r.data.pool).DiscoverCoreBootstrapJobs(ctx, sqlcgen.DiscoverCoreBootstrapJobsParams{Producer: r.producer})
	if err != nil {
		return result, false, mapPostgresError("discover Bootstrap work", err, nil)
	}
	for _, c := range candidates {
		found := false
		err = r.withJob(ctx, c.TenantID, c.OperationID, c.Kind, func(q *sqlcgen.Queries, op sqlcgen.TenantBootstrapOperation) error {
			key := r.key(c.TenantID, c.OperationID, c.Kind, c.Generation)
			j, err := q.LockCoreBootstrapJob(ctx, key)
			if errors.Is(err, pgx.ErrNoRows) {
				source, e := q.GetCoreBootstrapWorkSource(ctx, sqlcgen.GetCoreBootstrapWorkSourceParams{TenantID: c.TenantID, OperationID: c.OperationID, Producer: r.producer})
				if e != nil {
					return mapPostgresError("read original Bootstrap schedule source", e, biz.ErrCoreBootstrapConflict)
				}
				if e = q.CreateCoreBootstrapJob(ctx, sqlcgen.CreateCoreBootstrapJobParams{TenantID: c.TenantID, OperationID: c.OperationID, Kind: c.Kind, Generation: c.Generation, Producer: r.producer, SourceEventID: source.EventID}); e != nil {
					return mapPostgresError("create durable Bootstrap schedule", e, nil)
				}
				j, err = q.LockCoreBootstrapJob(ctx, key)
			}
			if err != nil {
				return mapPostgresError("lock Bootstrap job", err, nil)
			}
			if j.State == "done" || j.State == "attention_required" {
				return nil
			}
			terminal, available, e := coreBootstrapScheduleOutcome(ctx, q, op, c.Kind, c.Generation, j.ObservedAt.Time)
			if e != nil {
				return e
			}
			if j.State == "claimed" {
				current, e := identityNotificationCurrent(j.LeaseCurrent)
				if e != nil {
					return e
				}
				if current {
					return nil
				}
				if terminal {
					return r.finishJob(ctx, q, j, "done", "recovered_completion", "", j.ObservedAt.Time)
				}
				state := "pending"
				if j.AttemptCount-j.CycleStartAttempt >= 20 {
					state = "attention_required"
				}
				return r.finishJob(ctx, q, j, state, "lease_expired", "lease_expired", j.ObservedAt.Time.Add(biz.CoreBootstrapRetryDelay(j.AttemptCount-j.CycleStartAttempt)))
			}
			// An operation can complete between discovery and this Tenant lock. No new
			// attempt is needed; its dormant pending row is no longer discoverable.
			if terminal || !j.Due || available.After(j.ObservedAt.Time) {
				return nil
			}
			id, e := uuid.NewV7()
			if e != nil {
				return biz.ErrPersistenceUnavailable
			}
			if e = q.ClaimCoreBootstrapJob(ctx, sqlcgen.ClaimCoreBootstrapJobParams{TenantID: c.TenantID, OperationID: c.OperationID, Kind: c.Kind, Generation: c.Generation, Producer: r.producer, LeaseID: requiredPGUUID(id)}); e != nil {
				return mapPostgresError("lease exact Bootstrap work", e, nil)
			}
			j, e = q.LockCoreBootstrapJob(ctx, key)
			if e != nil {
				return mapPostgresError("read leased Bootstrap work", e, nil)
			}
			result = coreBootstrapClaim(j)
			found = true
			return nil
		})
		if err != nil {
			return biz.CoreBootstrapJobClaim{}, false, err
		}
		if found {
			return result, true, nil
		}
	}
	return result, false, nil
}
func (r *coreBootstrapQueue) Finish(ctx context.Context, c biz.CoreBootstrapJobClaim, code string, permanent bool) (bool, error) {
	if c.Producer != r.producer || c.LeaseID.Version() != 7 || c.Attempt < 1 || c.CycleStartAttempt < 0 || c.Attempt-c.CycleStartAttempt < 1 || c.Attempt-c.CycleStartAttempt > 20 || c.Generation < 1 || (permanent && code != "invalid_intent" && code != "intent_conflict" && code != "authority_unavailable") || (!permanent && (code == "invalid_intent" || code == "intent_conflict")) {
		return false, biz.ErrCoreBootstrapInvalid
	}
	switch code {
	case "", "authority_unavailable", "lifecycle_stale", "lifecycle_blocked", "invalid_intent", "intent_conflict", "dependency_unavailable", "execution_failed":
	default:
		return false, biz.ErrCoreBootstrapInvalid
	}
	attention := false
	err := r.withJob(ctx, c.TenantID, c.OperationID, string(c.Kind), func(q *sqlcgen.Queries, op sqlcgen.TenantBootstrapOperation) error {
		j, err := q.LockCoreBootstrapJob(ctx, r.key(c.TenantID, c.OperationID, string(c.Kind), c.Generation))
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrCoreBootstrapLease
		}
		if err != nil {
			return mapPostgresError("lock Bootstrap result lease", err, nil)
		}
		current, err := identityNotificationCurrent(j.LeaseCurrent)
		if j.State != "claimed" || !j.LeaseID.Valid || uuid.UUID(j.LeaseID.Bytes) != c.LeaseID || j.AttemptCount != c.Attempt || j.CycleStartAttempt != c.CycleStartAttempt || uuid.UUID(j.RecoveryID.Bytes) != c.RecoveryID {
			return biz.ErrCoreBootstrapLease
		}
		if err != nil {
			return err
		}
		if !current {
			return biz.ErrCoreBootstrapLease
		}
		terminal, available, err := coreBootstrapScheduleOutcome(ctx, q, op, string(c.Kind), c.Generation, j.ObservedAt.Time)
		if err != nil {
			return err
		}
		if terminal {
			attention = op.Status == "attention_required"
			return r.finishJob(ctx, q, j, "done", "completed", "", j.ObservedAt.Time)
		}
		if code == "" {
			if c.Kind != biz.CoreBootstrapExpire || !available.After(j.ObservedAt.Time) {
				return biz.ErrCoreBootstrapConflict
			}
			return r.finishJob(ctx, q, j, "pending", "deferred", "", available)
		}
		state, outcome := "pending", "retry"
		if permanent || j.AttemptCount-j.CycleStartAttempt >= 20 {
			state = "attention_required"
			outcome = state
			attention = true
		}
		return r.finishJob(ctx, q, j, state, outcome, code, j.ObservedAt.Time.Add(biz.CoreBootstrapRetryDelay(j.AttemptCount-j.CycleStartAttempt)))
	})
	return attention, err
}

func (r *coreBootstrapQueue) Observe(ctx context.Context) (biz.CoreBootstrapQueueObservation, error) {
	v, err := sqlcgen.New(r.data.pool).ObserveCoreBootstrapQueue(ctx, sqlcgen.ObserveCoreBootstrapQueueParams{Producer: r.producer})
	if err != nil {
		return biz.CoreBootstrapQueueObservation{}, mapPostgresError("observe Bootstrap queue attention", err, nil)
	}
	return biz.CoreBootstrapQueueObservation{JobAttention: v.JobAttention, InvitationAttention: v.InvitationAttention}, nil
}
