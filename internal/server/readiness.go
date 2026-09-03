package server

import "sync/atomic"

type Readiness struct {
	ready atomic.Bool
}

func NewReadiness() *Readiness {
	return &Readiness{}
}

func (r *Readiness) Set(ready bool) {
	r.ready.Store(ready)
}

func (r *Readiness) Ready() bool {
	return r.ready.Load()
}
