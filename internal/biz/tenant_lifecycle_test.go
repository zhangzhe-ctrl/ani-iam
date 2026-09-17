package biz

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func lifecycleUUID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
func lifecycleEvent(epoch uuid.UUID, sequence, version int64, status string) TenantLifecycleEvent {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	return TenantLifecycleEvent{EventID: lifecycleUUID(), Epoch: epoch, TenantID: lifecycleUUID(), Sequence: sequence, TenantVersion: version, Status: status, Reason: "owner supplied reason", OccurredAt: now, EffectiveAt: now}
}

func TestTenantLifecycleSequenceAndTenantVersionAreIndependent(t *testing.T) {
	epoch := lifecycleUUID()
	p, err := ActivateTenantLifecycleSnapshot(epoch, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	previous := TenantLifecycleFact{Version: 2, Status: "active", SnapshotBase: true}
	update, err := ProjectTenantLifecycle(p, previous, lifecycleEvent(epoch, 9, 3, "frozen"))
	if err != nil || update.Outcome != "applied" || update.Pipeline.Applied != 9 || update.Fact.Version != 3 || update.Fact.SnapshotBase {
		t.Fatal(update, err)
	}
	// A different tenant's initial version occupies the next global slot.
	update, err = ProjectTenantLifecycle(update.Pipeline, TenantLifecycleFact{}, lifecycleEvent(epoch, 10, 1, "active"))
	if err != nil || update.Outcome != "applied" || update.Pipeline.Applied != 10 {
		t.Fatal(update, err)
	}
	for _, tc := range []struct {
		name         string
		seq, version int64
		outcome      string
	}{
		{"source hole", 12, 2, "sequence_gap"}, {"tenant hole", 11, 3, "tenant_version_gap"},
	} {
		result, err := ProjectTenantLifecycle(update.Pipeline, update.Fact, lifecycleEvent(epoch, tc.seq, tc.version, "frozen"))
		if err != nil || result.Outcome != tc.outcome || !result.Pipeline.SnapshotRequired || result.Pipeline.Applied != 10 || result.Fact != update.Fact {
			t.Fatal(tc.name, result, err)
		}
		result, err = ProjectTenantLifecycle(result.Pipeline, result.Fact, lifecycleEvent(epoch, 11, 2, "frozen"))
		if err != nil || result.Outcome != "awaiting_snapshot" || result.Pipeline.Applied != 10 {
			t.Fatal("late event repaired gap without snapshot", result, err)
		}
	}
}

func TestTenantLifecycleEpochAndSnapshotCatchup(t *testing.T) {
	epoch := lifecycleUUID()
	e := lifecycleEvent(epoch, 1, 1, "active")
	result, err := ProjectTenantLifecycle(TenantLifecyclePipeline{}, TenantLifecycleFact{}, e)
	if err != nil || result.Outcome != "epoch_requires_snapshot" || !result.Pipeline.SnapshotRequired || result.Pipeline.Epoch != uuid.Nil {
		t.Fatal(result, err)
	}
	p, err := ActivateTenantLifecycleSnapshot(epoch, 10, 12)
	if err != nil || p.Applied != 10 || p.Highest != 12 || p.Fresh(e.OccurredAt) {
		t.Fatal(p, err)
	}
	result, err = ProjectTenantLifecycle(p, TenantLifecycleFact{Version: 1, Status: "active", SnapshotBase: true}, lifecycleEvent(epoch, 11, 2, "frozen"))
	if err != nil || result.Outcome != "applied" || result.Pipeline.Highest != 12 {
		t.Fatal(result, err)
	}
	result, err = ProjectTenantLifecycle(result.Pipeline, result.Fact, lifecycleEvent(epoch, 12, 3, "active"))
	if err != nil || result.Pipeline.Applied != 12 || result.Pipeline.Fresh(e.OccurredAt) {
		t.Fatal(result, err)
	}
	covered, err := ProjectTenantLifecycle(result.Pipeline, result.Fact, lifecycleEvent(epoch, 9, 1, "active"))
	if err != nil || covered.Outcome != "covered" || covered.Fact != result.Fact {
		t.Fatal(covered, err)
	}
	changed, err := ProjectTenantLifecycle(result.Pipeline, result.Fact, lifecycleEvent(lifecycleUUID(), 1, 1, "active"))
	if err != nil || !changed.Pipeline.SnapshotRequired || changed.Pipeline.Epoch != epoch {
		t.Fatal("new epoch trusted as continuity", changed, err)
	}
}

func TestTenantLifecycleHeartbeatFreshnessUsesBothClocksAndWatermark(t *testing.T) {
	epoch := lifecycleUUID()
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	p, _ := ActivateTenantLifecycleSnapshot(epoch, 3, 3)
	h := TenantLifecycleHeartbeat{ID: lifecycleUUID(), Epoch: epoch, Committed: 3, Published: 3, ObservedAt: now}
	p, outcome, err := ObserveTenantHeartbeat(p, h, now.Add(time.Second))
	if err != nil || outcome != "observed" || !p.Fresh(now.Add(29*time.Second)) || p.Fresh(now.Add(30*time.Second)) || p.Applied != 3 {
		t.Fatal(p, outcome, err)
	}
	replay, outcome, err := ObserveTenantHeartbeat(p, h, now.Add(29*time.Second))
	if err != nil || outcome != "older_heartbeat" || replay.HeartbeatReceivedAt != p.HeartbeatReceivedAt || replay.Fresh(now.Add(30*time.Second)) {
		t.Fatal("replay renewed freshness", replay, err)
	}
	newer := h
	newer.ID, newer.ObservedAt, newer.Committed = lifecycleUUID(), now.Add(10*time.Second), 4
	waiting, _, err := ObserveTenantHeartbeat(p, newer, newer.ObservedAt)
	if err != nil || waiting.Applied != 3 || waiting.Highest != 4 || waiting.Fresh(newer.ObservedAt) {
		t.Fatal("heartbeat filled unapplied slot", waiting, err)
	}
	update, err := ProjectTenantLifecycle(waiting, TenantLifecycleFact{}, lifecycleEvent(epoch, 4, 1, "active"))
	if err != nil || !update.Pipeline.Fresh(newer.ObservedAt) {
		t.Fatal(update, err)
	}
	// A source timestamp newer than the receiver time cannot establish freshness.
	if _, _, err = ObserveTenantHeartbeat(p, newer, now); !errors.Is(err, ErrTenantLifecycleInvalid) {
		t.Fatal(err)
	}
	old := newer
	old.ID, old.ObservedAt, old.Committed = lifecycleUUID(), now.Add(20*time.Second), 2
	old.Published = 2
	if _, _, err = ObserveTenantHeartbeat(waiting, old, old.ObservedAt); !errors.Is(err, ErrTenantLifecycleConflict) {
		t.Fatal("regression accepted", err)
	}
	other := h
	other.ID, other.Epoch = lifecycleUUID(), lifecycleUUID()
	mismatch, _, err := ObserveTenantHeartbeat(p, other, now)
	if err != nil || !mismatch.SnapshotRequired || mismatch.Fresh(now.Add(time.Second)) {
		t.Fatal(mismatch, err)
	}
	empty, _ := ActivateTenantLifecycleSnapshot(epoch, 0, 0)
	h.ID, h.Committed, h.Published = lifecycleUUID(), 0, 0
	empty, _, err = ObserveTenantHeartbeat(empty, h, now)
	if err != nil || !empty.Fresh(now) {
		t.Fatal("empty authoritative stream cannot become fresh", empty, err)
	}
}

func TestTenantLifecycleRejectsImpossibleTransitions(t *testing.T) {
	epoch := lifecycleUUID()
	p, _ := ActivateTenantLifecycleSnapshot(epoch, 5, 5)
	for _, tc := range []struct {
		from, to     string
		before, next int64
	}{
		{"disabled", "active", 3, 4}, {"disabled", "disabled", 3, 4}, {"active", "active", 1, 2}, {"", "frozen", 0, 1}, {"active", "frozen", 3, 3},
	} {
		_, err := ProjectTenantLifecycle(p, TenantLifecycleFact{Version: tc.before, Status: tc.from}, lifecycleEvent(epoch, 6, tc.next, tc.to))
		if !errors.Is(err, ErrTenantLifecycleConflict) {
			t.Fatal(tc, err)
		}
	}
	e := lifecycleEvent(epoch, 6, 2, "frozen")
	e.EffectiveAt = e.OccurredAt.Add(time.Second)
	if e.Validate() == nil {
		t.Fatal("scheduled lifecycle invented")
	}
}
