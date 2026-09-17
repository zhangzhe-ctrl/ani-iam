package biz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrTenantSnapshotExpired = errors.New("Tenant lifecycle snapshot expired")

type TenantSnapshotSource interface {
	Begin(context.Context, string, int) (TenantSnapshotCursor, error)
	Page(context.Context, TenantSnapshotCursor, string) (TenantSnapshotPage, error)
}

// The source cut is (Epoch, Watermark), not a broker stream sequence. Snapshot
// carries no identity intent and cannot create Access or an administrator.
type TenantSnapshotCursor struct {
	ID, Epoch, ReaderID   uuid.UUID
	Watermark, TotalCount int64
	PageSize              int
	CapturedAt, ExpiresAt time.Time
	FirstToken            string
}

func (TenantSnapshotCursor) String() string     { return "Tenant lifecycle snapshot (cursor redacted)" }
func (c TenantSnapshotCursor) GoString() string { return c.String() }
func (c TenantSnapshotCursor) Validate() error {
	if !lifecycleID(c.ID) || !lifecycleID(c.Epoch) || !lifecycleID(c.ReaderID) || c.Watermark < 0 || c.TotalCount < 0 || c.PageSize < 1 || c.PageSize > 100 ||
		c.CapturedAt.IsZero() || !c.ExpiresAt.After(c.CapturedAt) || c.ExpiresAt.Sub(c.CapturedAt) > 5*time.Minute || !ValidTenantSnapshotToken(c.FirstToken) {
		return ErrTenantLifecycleInvalid
	}
	return nil
}

func ValidTenantSnapshotToken(token string) bool {
	if len(token) < 32 || len(token) > 256 {
		return false
	}
	for _, ch := range token {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

type TenantSnapshotFact struct {
	TenantID uuid.UUID
	Version  int64
	Status   string
}
type TenantSnapshotPage struct {
	SnapshotID, Epoch       uuid.UUID
	Watermark               int64
	RequestToken, NextToken string
	Items                   []TenantSnapshotFact
}

func (TenantSnapshotPage) String() string     { return "Tenant lifecycle snapshot page (cursor redacted)" }
func (p TenantSnapshotPage) GoString() string { return p.String() }
func (p TenantSnapshotPage) Validate(c TenantSnapshotCursor) error {
	if c.Validate() != nil || p.SnapshotID != c.ID || p.Epoch != c.Epoch || p.Watermark != c.Watermark || !ValidTenantSnapshotToken(p.RequestToken) || len(p.Items) > c.PageSize || int64(len(p.Items)) > c.TotalCount {
		return ErrTenantLifecycleInvalid
	}
	if p.NextToken != "" && (!ValidTenantSnapshotToken(p.NextToken) || p.NextToken == p.RequestToken || len(p.Items) != c.PageSize) {
		return ErrTenantLifecycleInvalid
	}
	previous := ""
	for _, fact := range p.Items {
		if !lifecycleID(fact.TenantID) || fact.Version < 1 || !lifecycleStatus(fact.Status) || fact.TenantID.String() <= previous {
			return ErrTenantLifecycleInvalid
		}
		previous = fact.TenantID.String()
	}
	return nil
}
