package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cancellingBootstrapQueue struct {
	CoreBootstrapQueue
	cancel context.CancelFunc
}

func (q cancellingBootstrapQueue) Claim(ctx context.Context) (CoreBootstrapJobClaim, bool, error) {
	q.cancel()
	return CoreBootstrapJobClaim{}, false, ctx.Err()
}
func TestCoreBootstrapDispatcherCancellationIsNormalShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &CoreBootstrapDispatcher{queue: cancellingBootstrapQueue{cancel: cancel}}
	d.Run(ctx, func(err error) { t.Errorf("normal cancellation reported as failure: %v", err) })
	if ctx.Err() == nil {
		t.Fatal("dispatcher did not reach cancellation")
	}
}

func TestCoreBootstrapRetry(t *testing.T) {
	for _, c := range []struct {
		attempt int32
		delay   time.Duration
	}{{1, time.Second}, {2, 2 * time.Second}, {5, 16 * time.Second}, {6, 30 * time.Second}, {20, 30 * time.Second}} {
		if got := CoreBootstrapRetryDelay(c.attempt); got != c.delay {
			t.Fatalf("attempt %d delay %s", c.attempt, got)
		}
	}
}
func TestCoreBootstrapFailureClassification(t *testing.T) {
	for _, c := range []struct {
		name      string
		cause     error
		code      string
		permanent bool
	}{
		{"authority", ErrCoreBootstrapAuthority, "authority_unavailable", false}, {"stale", ErrTenantLifecycleStale, "lifecycle_stale", false}, {"blocked", ErrTenantLifecycleBlocked, "lifecycle_blocked", false}, {"invalid", ErrCoreBootstrapInvalid, "invalid_intent", true}, {"conflict", ErrCoreBootstrapConflict, "intent_conflict", true}, {"storage", ErrPersistenceUnavailable, "dependency_unavailable", false}, {"opaque_transport", errors.New("private transport response must not escape"), "execution_failed", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, p := classifyCoreBootstrapFailure(c.cause)
			if code != c.code || p != c.permanent {
				t.Fatal("unstable failure classification")
			}
		})
	}
}
