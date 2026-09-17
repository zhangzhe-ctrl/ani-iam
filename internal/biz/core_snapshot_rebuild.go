package biz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCoreSnapshotNotReady = errors.New("Core Snapshot rebuild is not caught up")
var ErrCoreSnapshotExpired = errors.New("Core Snapshot cursor expired")

// CoreSnapshotSource is the authenticated, versioned owner read boundary.
// Transport retries preserve the request key and cursor; an expired cut must
// be abandoned explicitly rather than silently mixing two snapshots.
type CoreSnapshotSource interface {
	Begin(context.Context, string, int32) (CoreSnapshotCursor, error)
	Page(context.Context, CoreSnapshotCursor, string) (CoreSnapshotPage, error)
}

// This is decoded Core data, not authentication evidence. The receiver must
// bind it to the authenticated REST response and a live durable subscription.
type CoreSnapshotCursor struct {
	ID, ConsumerID         uuid.UUID
	Producer               string
	SourceCut, BrokerAfter int64
	PageSize               int32
	ExpiresAt              time.Time
}

func (c CoreSnapshotCursor) Validate() error {
	if c.ID == uuid.Nil || c.ConsumerID.Version() != 7 || !ValidCoreProjectionProducer(c.Producer) || c.SourceCut < 0 || c.BrokerAfter < 0 || c.PageSize < 1 || c.PageSize > 500 || c.ExpiresAt.IsZero() {
		return ErrCoreProjectionInvalid
	}
	return nil
}

type CoreSnapshotFact struct {
	TenantID uuid.UUID
	Version  int64
	Status   string
}
type CoreSnapshotPage struct {
	Cursor                  CoreSnapshotCursor
	RequestToken, NextToken string
	Items                   []CoreSnapshotFact
}

func (p CoreSnapshotPage) Validate() error {
	if p.Cursor.Validate() != nil || len(p.Items) > int(p.Cursor.PageSize) {
		return ErrCoreProjectionInvalid
	}
	for _, s := range []string{p.RequestToken, p.NextToken} {
		if s != "" {
			id, err := uuid.Parse(s)
			if err != nil || id == uuid.Nil || id.String() != s {
				return ErrCoreProjectionInvalid
			}
		}
	}
	if p.NextToken != "" && (p.NextToken == p.RequestToken || len(p.Items) != int(p.Cursor.PageSize)) {
		return ErrCoreProjectionInvalid
	}
	previous := ""
	for _, f := range p.Items {
		if f.TenantID.Version() != 7 || f.Version < 1 || (f.Status != "active" && f.Status != "frozen" && f.Status != "disabled") || f.TenantID.String() <= previous {
			return ErrCoreProjectionInvalid
		}
		previous = f.TenantID.String()
	}
	return nil
}

type CoreSnapshotBuild struct {
	GenerationID                uuid.UUID
	State                       string
	LoadedItems, AppliedThrough int64
	NextToken                   string
}

// This port switches shadow generations only. It cannot enable enforcement or
// declare missing Bootstrap history covered by a Lifecycle snapshot.
type CoreSnapshotRebuildRepository interface {
	Begin(context.Context, CoreSnapshotCursor) (CoreSnapshotBuild, error)
	LoadPage(context.Context, CoreSnapshotPage) (CoreSnapshotBuild, error)
	CatchUp(context.Context, uuid.UUID) (CoreSnapshotBuild, error)
	ActivateShadow(context.Context, uuid.UUID) (CoreSnapshotBuild, error)
}

// Recovery is a durable cursor and page position, not an in-memory replay
// queue. An expired loading cut is retained as abandoned evidence by the repo.
type CoreSnapshotRecovery struct {
	Cursor CoreSnapshotCursor
	Build  CoreSnapshotBuild
	Needed bool
}
type CoreSnapshotRecoveryRepository interface {
	CoreSnapshotRebuildRepository
	NextRecovery(context.Context) (CoreSnapshotRecovery, error)
}

type CoreSnapshotCoordinator struct {
	source     CoreSnapshotSource
	repo       CoreSnapshotRecoveryRepository
	requestKey string
}

func NewCoreSnapshotCoordinator(source CoreSnapshotSource, repo CoreSnapshotRecoveryRepository) (*CoreSnapshotCoordinator, error) {
	if source == nil || repo == nil {
		return nil, ErrCoreProjectionInvalid
	}
	return &CoreSnapshotCoordinator{source: source, repo: repo}, nil
}

// ReconcileNext performs one bounded step. Only one worker calls it; process
// restarts recover from the database. A lost owner Begin response retries its
// request key, while a process crash before persisting a cursor can only leave
// an unused, expiring owner snapshot: the durable broker subscription predates
// every cut and continues receiving throughout the rebuild.
func (c *CoreSnapshotCoordinator) ReconcileNext(ctx context.Context) (bool, error) {
	r, err := c.repo.NextRecovery(ctx)
	if err != nil || !r.Needed {
		return false, err
	}
	if r.Cursor.ID == uuid.Nil {
		if c.requestKey == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return false, ErrPersistenceUnavailable
			}
			c.requestKey = id.String()
		}
		cursor, err := c.source.Begin(ctx, c.requestKey, 200)
		if err != nil {
			if errors.Is(err, ErrCoreSnapshotExpired) {
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
	switch r.Build.State {
	case "loading":
		page, err := c.source.Page(ctx, r.Cursor, r.Build.NextToken)
		if err != nil {
			return false, err
		}
		_, err = c.repo.LoadPage(ctx, page)
		return err == nil, err
	case "loaded", "catching_up":
		build, err := c.repo.CatchUp(ctx, r.Cursor.ID)
		if err != nil {
			return false, err
		}
		if build.State == "abandoned" {
			return true, nil
		}
		_, err = c.repo.ActivateShadow(ctx, r.Cursor.ID)
		if errors.Is(err, ErrCoreSnapshotNotReady) {
			return false, nil
		}
		return err == nil, err
	default:
		return false, ErrCoreProjectionConflict
	}
}
