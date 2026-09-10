package server

import (
	"context"
	"sync/atomic"
	"time"
)

type Readiness struct {
	ready        atomic.Bool
	dependencies atomic.Bool
	check        func(context.Context) error
	cancel       context.CancelFunc
	done         chan struct{}
}

func NewReadiness() *Readiness {
	r := &Readiness{}
	r.dependencies.Store(true)
	return r
}

func NewDependencyReadiness(check func(context.Context) error) *Readiness {
	return &Readiness{check: check}
}

func (r *Readiness) Start(context.Context) error {
	if r.check == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			probe, end := context.WithTimeout(ctx, time.Second)
			err := r.check(probe)
			end()
			r.dependencies.Store(err == nil)
			select {
			case <-ctx.Done():
				r.dependencies.Store(false)
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (r *Readiness) Stop(ctx context.Context) error {
	r.Set(false)
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Readiness) Set(ready bool) {
	r.ready.Store(ready)
}

func (r *Readiness) Ready() bool {
	return r.ready.Load() && r.dependencies.Load()
}
