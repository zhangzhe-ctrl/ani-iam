package biz

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCoreLifecycleProjection(t *testing.T) {
	id := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	now := time.Now().UTC().Truncate(time.Microsecond)
	m := CoreProjectionMessage{EventID: id, TenantID: id, Producer: "core.test", Kind: "lifecycle", SourceSequence: 1, OccurredAt: now, RawPayload: []byte(`{}`), LifecycleVersion: 1, Status: "active", Reason: "created", EffectiveAt: now}
	initial, _, err := ProjectCoreLifecycle(CoreTenantProjection{}, m)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("monotonic_freeze_restore_disable", func(t *testing.T) {
		state := initial
		fact := m
		for _, step := range []struct{ status, reason string }{{"frozen", "administrative_freeze"}, {"active", "restored"}, {"disabled", "user_requested_deactivation"}} {
			fact.LifecycleVersion++
			fact.Status, fact.Reason = step.status, step.reason
			var err error
			state, _, err = ProjectCoreLifecycle(state, fact)
			if err != nil || state.Status != step.status || state.Version != fact.LifecycleVersion {
				t.Fatal("complete fact not projected")
			}
		}
		fact.LifecycleVersion++
		fact.Status, fact.Reason = "active", "restored"
		if _, _, err := ProjectCoreLifecycle(state, fact); !errors.Is(err, ErrCoreProjectionConflict) {
			t.Fatal("disabled Tenant revived")
		}
	})
	t.Run("gap_latches_until_snapshot", func(t *testing.T) {
		fact := m
		fact.LifecycleVersion = 3
		fact.Status, fact.Reason = "frozen", "administrative_freeze"
		state, outcome, err := ProjectCoreLifecycle(initial, fact)
		if err != nil || outcome != "version_gap" || !state.RepairRequired || state.Version != 1 || state.RequiredVersion != 3 {
			t.Fatal("gap advanced state")
		}
		fact.LifecycleVersion = 2
		next, outcome, err := ProjectCoreLifecycle(state, fact)
		if err != nil || outcome != "awaiting_snapshot" || next.Version != 1 || next.RequiredVersion != 3 {
			t.Fatal("late event cleared gap")
		}
		fact.LifecycleVersion = 4
		next, _, _ = ProjectCoreLifecycle(next, fact)
		if next.RequiredVersion != 4 {
			t.Fatal("snapshot requirement did not advance")
		}
	})
	t.Run("missing_initial_fact_is_never_active", func(t *testing.T) {
		fact := m
		fact.LifecycleVersion = 4
		state, _, err := ProjectCoreLifecycle(CoreTenantProjection{}, fact)
		if err != nil || state.Version != 0 || state.Status != "" || !state.RepairRequired {
			t.Fatal("missing version one inferred state")
		}
	})
	t.Run("duplicate_and_older_do_not_change_current_fact", func(t *testing.T) {
		same, outcome, err := ProjectCoreLifecycle(initial, m)
		if err != nil || outcome != "same_version" || same != initial {
			t.Fatal("same version changed state")
		}
		state := initial
		state.Version = 2
		state.RequiredVersion = 2
		state.Status = "frozen"
		state.Reason = "administrative_freeze"
		old, outcome, err := ProjectCoreLifecycle(state, m)
		if err != nil || outcome != "older" || old != state {
			t.Fatal("older event overwrote state")
		}
	})
	t.Run("same_version_different_fact_conflicts", func(t *testing.T) {
		fact := m
		fact.Status, fact.Reason = "frozen", "administrative_freeze"
		if _, _, err := ProjectCoreLifecycle(initial, fact); !errors.Is(err, ErrCoreProjectionConflict) {
			t.Fatal("equivocation accepted")
		}
	})
	t.Run("heartbeat_has_no_tenant", func(t *testing.T) {
		fact := CoreProjectionMessage{EventID: id, Producer: "core.test", Kind: "heartbeat", SourceSequence: 2, OccurredAt: now, RawPayload: []byte(`{}`)}
		if err := fact.Validate(); err != nil {
			t.Fatal(err)
		}
		fact.TenantID = id
		if err := fact.Validate(); !errors.Is(err, ErrCoreProjectionInvalid) {
			t.Fatal("Tenant heartbeat accepted")
		}
	})
}
