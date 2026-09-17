package biz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrTenantSnapshotNotReady = errors.New("Tenant lifecycle snapshot is not ready to activate")

type TenantSnapshotBuild struct {
	Cursor                      TenantSnapshotCursor
	GenerationID                uuid.UUID
	State                       string
	NextToken                   string
	LoadedItems, AppliedThrough int64
}

func (TenantSnapshotBuild) String() string     { return "Tenant lifecycle rebuild (cursor redacted)" }
func (b TenantSnapshotBuild) GoString() string { return b.String() }

type TenantSnapshotRecovery struct {
	Needed bool
	Build  TenantSnapshotBuild
}
type TenantSnapshotRecoveryRepository interface {
	NextRecovery(context.Context) (TenantSnapshotRecovery, error)
	Begin(context.Context, TenantSnapshotCursor) (TenantSnapshotBuild, error)
	LoadPage(context.Context, TenantSnapshotCursor, TenantSnapshotPage) (TenantSnapshotBuild, error)
	CatchUp(context.Context, uuid.UUID) (TenantSnapshotBuild, error)
	Activate(context.Context, uuid.UUID) (TenantSnapshotBuild, error)
}

type TenantSnapshotCoordinator struct {
	source     TenantSnapshotSource
	repo       TenantSnapshotRecoveryRepository
	requestKey string
}

func NewTenantSnapshotCoordinator(source TenantSnapshotSource, repo TenantSnapshotRecoveryRepository) (*TenantSnapshotCoordinator, error) {
	if source == nil || repo == nil {
		return nil, ErrTenantLifecycleInvalid
	}
	return &TenantSnapshotCoordinator{source: source, repo: repo}, nil
}

// One bounded remote/page/transaction step; the durable build resumes after a
// process restart. Only a lost Begin before local persistence can leave an
// unused expiring owner cursor. The durable broker consumer predates each cut.
func (c *TenantSnapshotCoordinator) ReconcileNext(ctx context.Context) (bool, error) {
	recovery, err := c.repo.NextRecovery(ctx)
	if err != nil || !recovery.Needed {
		return false, err
	}
	build := recovery.Build
	if build.Cursor.ID == uuid.Nil {
		if c.requestKey == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return false, ErrPersistenceUnavailable
			}
			c.requestKey = id.String()
		}
		cursor, err := c.source.Begin(ctx, c.requestKey, 100)
		if err != nil {
			if errors.Is(err, ErrTenantSnapshotExpired) {
				c.requestKey = ""
			}
			return false, err
		}
		_, err = c.repo.Begin(ctx, cursor)
		if err == nil {
			c.requestKey = ""
		}
		return err == nil, err
	}
	c.requestKey = ""
	switch build.State {
	case "loading":
		page, err := c.source.Page(ctx, build.Cursor, build.NextToken)
		if err != nil {
			return false, err
		}
		if err = page.Validate(build.Cursor); err != nil {
			return false, err
		}
		_, err = c.repo.LoadPage(ctx, build.Cursor, page)
		return err == nil, err
	case "loaded", "catching_up":
		next, err := c.repo.CatchUp(ctx, build.Cursor.ID)
		if err != nil {
			return false, err
		}
		if next.State == "abandoned" {
			return true, nil
		}
		_, err = c.repo.Activate(ctx, build.Cursor.ID)
		if errors.Is(err, ErrTenantSnapshotNotReady) {
			return false, nil
		}
		return err == nil, err
	default:
		return false, ErrTenantLifecycleConflict
	}
}
