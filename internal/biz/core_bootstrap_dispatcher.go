package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

type CoreBootstrapJobKind string

const (
	CoreBootstrapInitialize CoreBootstrapJobKind = "initialize"
	CoreBootstrapExpire     CoreBootstrapJobKind = "expire"
)

var (
	ErrCoreBootstrapLease     = errors.New("Bootstrap execution lease is no longer current")
	ErrCoreBootstrapRetryable = errors.New("Bootstrap execution scheduled for retry")
	ErrCoreBootstrapAttention = errors.New("Bootstrap execution requires attention")
)

type CoreBootstrapJobClaim struct {
	CycleStartAttempt              int32
	RecoveryID                     uuid.UUID
	TenantID, OperationID, LeaseID uuid.UUID
	Producer                       string
	Kind                           CoreBootstrapJobKind
	Attempt                        int32
	Generation                     int64
	LeaseUntil                     time.Time
}
type CoreBootstrapQueueObservation struct{ JobAttention, InvitationAttention int64 }
type CoreBootstrapQueue interface {
	Observe(context.Context) (CoreBootstrapQueueObservation, error)
	Claim(context.Context) (CoreBootstrapJobClaim, bool, error)
	Finish(context.Context, CoreBootstrapJobClaim, string, bool) (bool, error)
}
type CoreBootstrapExecutor interface {
	Reconcile(context.Context, TenantScope, uuid.UUID) (CoreBootstrapWorkerResult, error)
	ExpireInvitation(context.Context, TenantScope, uuid.UUID, int64) (CoreBootstrapWorkerResult, error)
}
type CoreBootstrapDispatcher struct {
	queue  CoreBootstrapQueue
	worker CoreBootstrapExecutor
}

func NewCoreBootstrapDispatcher(q CoreBootstrapQueue, w CoreBootstrapExecutor) (*CoreBootstrapDispatcher, error) {
	if q == nil || w == nil {
		return nil, ErrCoreBootstrapInvalid
	}
	return &CoreBootstrapDispatcher{q, w}, nil
}

// A stable non-sensitive classification is all the queue retains. Transport
// errors and payloads never become scheduling metadata.
func classifyCoreBootstrapFailure(err error) (string, bool) {
	switch {
	case err == nil:
		return "", false
	case errors.Is(err, ErrCoreBrokerAuthority):
		return "authority_unavailable", true
	case errors.Is(err, ErrCoreBootstrapAuthority):
		return "authority_unavailable", false
	case errors.Is(err, ErrTenantLifecycleStale):
		return "lifecycle_stale", false
	case errors.Is(err, ErrTenantLifecycleBlocked):
		return "lifecycle_blocked", false
	case errors.Is(err, ErrCoreBootstrapInvalid):
		return "invalid_intent", true
	case errors.Is(err, ErrCoreBootstrapConflict):
		return "intent_conflict", true
	case errors.Is(err, ErrPersistenceUnavailable), errors.Is(err, ErrAuthenticationDependency):
		return "dependency_unavailable", false
	default:
		return "execution_failed", false
	}
}
func CoreBootstrapRetryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		return 0
	}
	return min(time.Second<<min(attempt-1, 5), 30*time.Second)
}
func (d *CoreBootstrapDispatcher) DispatchNext(ctx context.Context) (bool, error) {
	c, found, err := d.queue.Claim(ctx)
	if err != nil || !found {
		return false, err
	}
	scope, err := NewTenantScope(c.TenantID)
	if err != nil || c.OperationID.Version() != 7 || c.LeaseID.Version() != 7 || c.Attempt < 1 || c.CycleStartAttempt < 0 || c.Attempt-c.CycleStartAttempt < 1 || c.Attempt-c.CycleStartAttempt > 20 || c.Generation < 1 || !ValidCoreProjectionProducer(c.Producer) {
		return true, ErrCoreBootstrapInvalid
	}
	switch c.Kind {
	case CoreBootstrapInitialize:
		_, err = d.worker.Reconcile(ctx, scope, c.OperationID)
	case CoreBootstrapExpire:
		_, err = d.worker.ExpireInvitation(ctx, scope, c.OperationID, c.Generation)
	default:
		return true, ErrCoreBootstrapInvalid
	}
	code, permanent := classifyCoreBootstrapFailure(err)
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	attention, saveErr := d.queue.Finish(finish, c, code, permanent)
	if errors.Is(saveErr, ErrCoreBootstrapLease) {
		return true, nil
	}
	if saveErr != nil {
		return true, saveErr
	}
	if attention {
		return true, ErrCoreBootstrapAttention
	}
	if err != nil {
		return true, ErrCoreBootstrapRetryable
	}
	return true, nil
}

// Run is registered only by the formal composition root once a real current
// authority adapter is supplied. A wait or a retry never blocks unrelated work.
func (d *CoreBootstrapDispatcher) Run(ctx context.Context, report func(error)) {
	d.RunObserved(ctx, report, nil)
}

// Observation runs even when a restarted queue has no claimable jobs. It
// returns finite counts; transport logging/metrics remain outside the domain.
func (d *CoreBootstrapDispatcher) RunObserved(ctx context.Context, report func(error), observe func(CoreBootstrapQueueObservation, error)) {
	var nextObservation time.Time
	for ctx.Err() == nil {
		if observe != nil && !time.Now().Before(nextObservation) {
			call, cancel := context.WithTimeout(ctx, 2*time.Second)
			value, err := d.queue.Observe(call)
			cancel()
			observe(value, err)
			nextObservation = time.Now().Add(10 * time.Second)
		}
		worked, err := d.DispatchNext(ctx)
		if ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, ErrCoreBootstrapRetryable)) {
			return
		}
		if err != nil && report != nil {
			report(err)
		}
		if worked {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
