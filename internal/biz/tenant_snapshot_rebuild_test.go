package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type tenantSnapshotSourceTest struct {
	cursor  TenantSnapshotCursor
	keys    []string
	fail    bool
	badPage bool
}

func (s *tenantSnapshotSourceTest) Begin(_ context.Context, key string, size int) (TenantSnapshotCursor, error) {
	s.keys = append(s.keys, key)
	if size != 100 {
		return TenantSnapshotCursor{}, ErrTenantLifecycleInvalid
	}
	if s.fail {
		return TenantSnapshotCursor{}, ErrPersistenceUnavailable
	}
	return s.cursor, nil
}
func (s *tenantSnapshotSourceTest) Page(_ context.Context, c TenantSnapshotCursor, token string) (TenantSnapshotPage, error) {
	p := TenantSnapshotPage{SnapshotID: c.ID, Epoch: c.Epoch, Watermark: c.Watermark, RequestToken: token}
	if s.badPage {
		p.Epoch = uuid.Must(uuid.NewV7())
	}
	return p, nil
}

type tenantSnapshotRepoTest struct {
	recovery                 TenantSnapshotRecovery
	pageCalls, activateCalls int
	notReady                 bool
}

func (r *tenantSnapshotRepoTest) NextRecovery(context.Context) (TenantSnapshotRecovery, error) {
	return r.recovery, nil
}
func (r *tenantSnapshotRepoTest) Begin(_ context.Context, c TenantSnapshotCursor) (TenantSnapshotBuild, error) {
	r.recovery.Build = TenantSnapshotBuild{Cursor: c, State: "loading", NextToken: c.FirstToken}
	return r.recovery.Build, nil
}
func (r *tenantSnapshotRepoTest) LoadPage(context.Context, TenantSnapshotCursor, TenantSnapshotPage) (TenantSnapshotBuild, error) {
	r.pageCalls++
	r.recovery.Build.State = "loaded"
	return r.recovery.Build, nil
}
func (r *tenantSnapshotRepoTest) CatchUp(context.Context, uuid.UUID) (TenantSnapshotBuild, error) {
	r.recovery.Build.State = "catching_up"
	return r.recovery.Build, nil
}
func (r *tenantSnapshotRepoTest) Activate(context.Context, uuid.UUID) (TenantSnapshotBuild, error) {
	r.activateCalls++
	if r.notReady {
		return r.recovery.Build, ErrTenantSnapshotNotReady
	}
	r.recovery.Needed = false
	r.recovery.Build.State = "activated"
	return r.recovery.Build, nil
}

func TestTenantSnapshotCoordinatorRetryResumeAndCutFence(t *testing.T) {
	now := time.Now().UTC()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	source := &tenantSnapshotSourceTest{fail: true, cursor: TenantSnapshotCursor{ID: id(), Epoch: id(), ReaderID: id(), PageSize: 100, CapturedAt: now, ExpiresAt: now.Add(time.Minute), FirstToken: strings.Repeat("x", 32)}}
	repository := &tenantSnapshotRepoTest{recovery: TenantSnapshotRecovery{Needed: true}}
	c, err := NewTenantSnapshotCoordinator(source, repository)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = c.ReconcileNext(ctx); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
	source.fail = false
	if worked, err := c.ReconcileNext(ctx); err != nil || !worked {
		t.Fatal(err)
	}
	if len(source.keys) != 2 || source.keys[0] != source.keys[1] {
		t.Fatal("lost Begin response changed idempotency key")
	}
	// Rebuilding the coordinator resumes the exact durable cursor; a response
	// for a different epoch must not even reach the persistence port.
	c, _ = NewTenantSnapshotCoordinator(source, repository)
	source.badPage = true
	if _, err = c.ReconcileNext(ctx); !errors.Is(err, ErrTenantLifecycleInvalid) || repository.pageCalls != 0 {
		t.Fatal("changed source cut reached storage", err)
	}
	source.badPage = false
	if _, err = c.ReconcileNext(ctx); err != nil {
		t.Fatal(err)
	}
	repository.notReady = true
	if worked, err := c.ReconcileNext(ctx); err != nil || worked {
		t.Fatal("gap classified as activation", err)
	}
	repository.notReady = false
	if worked, err := c.ReconcileNext(ctx); err != nil || !worked {
		t.Fatal(err)
	}
	if repository.pageCalls != 1 || repository.activateCalls != 2 || len(source.keys) != 2 {
		t.Fatal("resume repeated Snapshot begin/page")
	}
}
