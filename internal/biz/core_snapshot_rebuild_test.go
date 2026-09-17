package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestCoreSnapshotPage(t *testing.T) {
	consumer, _ := uuid.NewV7()
	tenant, _ := uuid.NewV7()
	good := CoreSnapshotPage{Cursor: CoreSnapshotCursor{ID: uuid.New(), ConsumerID: consumer, Producer: "core.example", SourceCut: 3, BrokerAfter: 2, PageSize: 1, ExpiresAt: time.Now().Add(time.Minute)}, Items: []CoreSnapshotFact{{TenantID: tenant, Version: 4, Status: "frozen"}}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*CoreSnapshotPage){
		"duplicate_tenant":    func(p *CoreSnapshotPage) { p.Cursor.PageSize = 2; p.Items = append(p.Items, p.Items[0]) },
		"unknown_status":      func(p *CoreSnapshotPage) { p.Items[0].Status = "paused" },
		"zero_version":        func(p *CoreSnapshotPage) { p.Items[0].Version = 0 },
		"token_cycle":         func(p *CoreSnapshotPage) { p.RequestToken = uuid.NewString(); p.NextToken = p.RequestToken },
		"short_nonfinal_page": func(p *CoreSnapshotPage) { p.Cursor.PageSize = 2; p.NextToken = uuid.NewString() },
		"invalid_consumer":    func(p *CoreSnapshotPage) { p.Cursor.ConsumerID = uuid.Nil },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			p := good
			p.Items = append([]CoreSnapshotFact(nil), good.Items...)
			edit(&p)
			if p.Validate() == nil {
				t.Fatal("accepted invalid page")
			}
		})
	}
}

type snapshotRecoveryFixture struct {
	CoreSnapshotRebuildRepository
	plan              CoreSnapshotRecovery
	beginErr, pageErr error
	beginKeys         []string
	pageTokens        []string
	storedPages       int
	activated         bool
}

func (f *snapshotRecoveryFixture) NextRecovery(context.Context) (CoreSnapshotRecovery, error) {
	return f.plan, nil
}
func (f *snapshotRecoveryFixture) Begin(_ context.Context, c CoreSnapshotCursor) (CoreSnapshotBuild, error) {
	f.plan.Cursor = c
	f.plan.Build.State = "loading"
	return f.plan.Build, nil
}
func (f *snapshotRecoveryFixture) LoadPage(_ context.Context, p CoreSnapshotPage) (CoreSnapshotBuild, error) {
	if f.pageErr != nil {
		return CoreSnapshotBuild{}, f.pageErr
	}
	f.storedPages++
	f.plan.Build.NextToken = p.NextToken
	f.plan.Build.State = "loaded"
	return f.plan.Build, nil
}
func (f *snapshotRecoveryFixture) CatchUp(context.Context, uuid.UUID) (CoreSnapshotBuild, error) {
	f.plan.Build.State = "catching_up"
	return f.plan.Build, nil
}
func (f *snapshotRecoveryFixture) ActivateShadow(context.Context, uuid.UUID) (CoreSnapshotBuild, error) {
	f.activated = true
	f.plan.Needed = false
	return f.plan.Build, nil
}

type snapshotSourceFixture struct {
	repo   *snapshotRecoveryFixture
	cursor CoreSnapshotCursor
}

func (f snapshotSourceFixture) Begin(_ context.Context, key string, _ int32) (CoreSnapshotCursor, error) {
	f.repo.beginKeys = append(f.repo.beginKeys, key)
	return f.cursor, f.repo.beginErr
}
func (f snapshotSourceFixture) Page(_ context.Context, c CoreSnapshotCursor, token string) (CoreSnapshotPage, error) {
	f.repo.pageTokens = append(f.repo.pageTokens, token)
	return CoreSnapshotPage{Cursor: c, RequestToken: token}, nil
}
func TestCoreSnapshotCoordinatorResumesDurableProgress(t *testing.T) {
	ctx := context.Background()
	repo := &snapshotRecoveryFixture{plan: CoreSnapshotRecovery{Needed: true}, beginErr: context.DeadlineExceeded}
	source := snapshotSourceFixture{repo: repo, cursor: CoreSnapshotCursor{ID: uuid.New()}}
	c, err := NewCoreSnapshotCoordinator(source, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReconcileNext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	repo.beginErr = nil
	if _, err = c.ReconcileNext(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repo.beginKeys) != 2 || repo.beginKeys[0] == "" || repo.beginKeys[0] != repo.beginKeys[1] {
		t.Fatal("lost Begin idempotency key")
	}
	// A process restart must use the persisted cursor and requested page token.
	repo.plan.Build.NextToken = uuid.NewString()
	repo.pageErr = ErrPersistenceUnavailable
	c, err = NewCoreSnapshotCoordinator(source, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ReconcileNext(ctx); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal(err)
	}
	repo.pageErr = nil
	if _, err = c.ReconcileNext(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repo.beginKeys) != 2 || len(repo.pageTokens) != 2 || repo.pageTokens[0] != repo.pageTokens[1] || repo.storedPages != 1 || repo.activated {
		t.Fatal("failed page advanced or recreated cut")
	}
	if _, err = c.ReconcileNext(ctx); err != nil || !repo.activated {
		t.Fatal("did not catch up before activation", err)
	}
}
