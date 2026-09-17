//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Domain fixtures exercise the actual PostgreSQL projection transaction. This
// test does not claim broker attribution, formal consumer ACK, shadow exit or
// authorization activation. The time-window case uses real elapsed time.
func TestWR23CoreLifecycleProjection(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	d := data.NewData(e.runtimePool)
	type fixture struct {
		producer string
		repo     biz.CoreLifecycleProjectionRepository
	}
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		producer := "core.wr23." + mustV7(t).String()
		repo, err := data.NewCoreLifecycleProjectionRepository(d, producer)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		return fixture{producer, repo}
	}
	message := func(f fixture, seq int64, tenant uuid.UUID, version int64, status string) biz.CoreProjectionMessage {
		m := biz.CoreProjectionMessage{Producer: f.producer, SourceSequence: seq, EventID: mustV7(t), TenantID: tenant, OccurredAt: time.Now().UTC().Truncate(time.Microsecond), RawPayload: []byte(`{"fixture":"domain-only"}`)}
		if tenant == uuid.Nil {
			m.Kind = "heartbeat"
			return m
		}
		m.Kind = "lifecycle"
		m.LifecycleVersion = version
		m.Status = status
		m.EffectiveAt = m.OccurredAt
		switch status {
		case "active":
			m.Reason = "created"
			if version > 1 {
				m.Reason = "restored"
			}
		case "frozen":
			m.Reason = "administrative_freeze"
		case "disabled":
			m.Reason = "user_requested_deactivation"
		}
		return m
	}
	apply := func(t *testing.T, f fixture, m biz.CoreProjectionMessage) biz.CoreProjectionReceipt {
		t.Helper()
		r, err := f.repo.Apply(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	read := func(t *testing.T, f fixture, tenant uuid.UUID) biz.CoreShadowTenant {
		t.Helper()
		v, err := f.repo.ReadShadowTenant(ctx, mustTenantScope(t, tenant))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("schema_and_explicit_pipeline_initialization", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
			t.Fatal(err)
		}
		f := newFixture(t)
		tenant := mustV7(t)
		apply(t, f, message(f, 1, tenant, 1, "active"))
		before := read(t, f, tenant)
		if err := f.repo.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		after := read(t, f, tenant)
		if before.GenerationID != after.GenerationID {
			t.Fatal("restart replaced generation")
		}
		wrong := message(f, 2, uuid.Nil, 0, "")
		wrong.Producer = "not-configured"
		if _, err := f.repo.Apply(ctx, wrong); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("payload selected producer")
		}
		missing, _ := data.NewCoreLifecycleProjectionRepository(d, "uninitialized")
		wrong.Producer = "uninitialized"
		if _, err := missing.Apply(ctx, wrong); !errors.Is(err, biz.ErrCoreProjectionMissing) {
			t.Fatal("message implicitly initialized pipeline")
		}
	})
	t.Run("source_holes_cannot_be_hidden_by_heartbeat", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		apply(t, f, message(f, 1, tenant, 1, "active"))
		for _, seq := range []int64{3, 5, 4} {
			r := apply(t, f, message(f, seq, uuid.Nil, 0, ""))
			if r.ContiguousSequence != 1 {
				t.Fatal("source gap skipped")
			}
		}
		if v := read(t, f, tenant); v.Fresh || v.HighestSequence != 5 {
			t.Fatal("heartbeat hid missing source two")
		}
		r := apply(t, f, message(f, 2, uuid.Nil, 0, ""))
		if r.ContiguousSequence != 5 || !read(t, f, tenant).Fresh {
			t.Fatal("durably filled source interval not recovered")
		}
	})
	t.Run("version_gap_affects_only_its_tenant_and_requires_snapshot", func(t *testing.T) {
		f := newFixture(t)
		a, b := mustV7(t), mustV7(t)
		apply(t, f, message(f, 1, a, 1, "active"))
		apply(t, f, message(f, 2, b, 1, "active"))
		r := apply(t, f, message(f, 3, a, 3, "frozen"))
		if r.Outcome != "version_gap" {
			t.Fatal("out of order fact applied")
		}
		if v := read(t, f, a); v.Fresh || !v.RepairRequired || v.Version != 1 || v.RequiredVersion != 3 {
			t.Fatal("bad version gap state")
		}
		if !read(t, f, b).Fresh {
			t.Fatal("known Tenant gap blocked unrelated Tenant")
		}
		apply(t, f, message(f, 4, a, 2, "frozen"))
		if v := read(t, f, a); v.Fresh || v.Version != 1 || !v.RepairRequired {
			t.Fatal("late version replaced Snapshot repair")
		}
		unknown := mustV7(t)
		apply(t, f, message(f, 5, unknown, 3, "frozen"))
		if v := read(t, f, unknown); v.Fresh || v.Status != "" || v.Version != 0 {
			t.Fatal("missing initial state inferred active Tenant")
		}
	})
	t.Run("exact_replay_restart_and_equivocation", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		m := message(f, 1, tenant, 1, "active")
		apply(t, f, m)
		restarted, err := data.NewCoreLifecycleProjectionRepository(data.NewData(e.runtimePool), f.producer)
		if err != nil {
			t.Fatal(err)
		}
		r, err := restarted.Apply(ctx, m)
		if err != nil || !r.Duplicate || r.ContiguousSequence != 1 {
			t.Fatal("restart lost stable receipt")
		}
		bad := m
		bad.RawPayload = []byte(`{"changed":true}`)
		if _, err := f.repo.Apply(ctx, bad); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("event bytes were overwritten")
		}
		bad = m
		bad.TenantID = mustV7(t)
		if _, err := f.repo.Apply(ctx, bad); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("event crossed Tenant")
		}
		bad = message(f, 2, tenant, 1, "frozen")
		if _, err := f.repo.Apply(ctx, bad); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("same version changed fact")
		}
		if v := read(t, f, tenant); v.Version != 1 || v.Status != "active" || v.HighestSequence != 1 {
			t.Fatal("failed transaction advanced progress")
		}
	})
	t.Run("source_conflict_rolls_back_new_tenant_projection", func(t *testing.T) {
		f := newFixture(t)
		a, b := mustV7(t), mustV7(t)
		apply(t, f, message(f, 1, a, 1, "active"))
		if _, err := f.repo.Apply(ctx, message(f, 1, b, 1, "active")); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("source sequence reused")
		}
		if _, err := f.repo.ReadShadowTenant(ctx, mustTenantScope(t, b)); !errors.Is(err, biz.ErrCoreProjectionMissing) {
			t.Fatal("failed receipt left partial projection")
		}
	})
	t.Run("concurrent_tenants_and_out_of_order_source", func(t *testing.T) {
		f := newFixture(t)
		messages := make([]biz.CoreProjectionMessage, 12)
		for i := range messages {
			messages[i] = message(f, int64(i+1), mustV7(t), 1, "active")
		}
		var wg sync.WaitGroup
		for _, m := range messages {
			wg.Go(func() {
				if _, err := f.repo.Apply(ctx, m); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, m := range messages {
			if v := read(t, f, m.TenantID); !v.Fresh || v.ContiguousSequence != 12 || v.HighestSequence != 12 {
				t.Fatal("concurrent delivery left unexplained gap")
			}
		}
	})
	t.Run("bootstrap_progress_requires_durable_exact_operation", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		apply(t, f, message(f, 1, tenant, 1, "active"))
		i := biz.CoreBootstrapIntent{TenantID: tenant, OperationID: mustV7(t), NormalizedEmail: "invited@example.test", Locale: "en-US"}
		payload, _ := json.Marshal(map[string]string{"locale": i.Locale, "normalized_email": i.NormalizedEmail, "operation_id": i.OperationID.String(), "tenant_id": i.TenantID.String()})
		sum := sha256.Sum256(payload)
		i.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		m := message(f, 2, uuid.Nil, 0, "")
		m.Kind = "bootstrap"
		m.TenantID = tenant
		m.RawPayload = payload
		if _, err := f.repo.Apply(ctx, m); !errors.Is(err, biz.ErrInvalidPersistenceState) {
			t.Fatal("bootstrap source advanced without durable intent")
		}
		if v := read(t, f, tenant); v.ContiguousSequence != 1 || v.HighestSequence != 1 {
			t.Fatal("failed bootstrap marker advanced pipeline")
		}
		_, err := data.NewCoreBootstrapReceiptRepository(d).Receive(ctx, biz.CoreBootstrapDelivery{Intent: i, EventID: m.EventID, Producer: f.producer, SourceSequence: 2, OccurredAt: m.OccurredAt, RawPayload: payload})
		if err != nil {
			t.Fatal(err)
		}
		apply(t, f, m)
		if v := read(t, f, tenant); v.ContiguousSequence != 2 || !v.Fresh {
			t.Fatal("durable bootstrap not counted in source progress")
		}
	})
	t.Run("backlog_and_future_clock_do_not_extend_freshness", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		m := message(f, 1, tenant, 1, "active")
		m.OccurredAt = m.OccurredAt.Add(-time.Minute)
		apply(t, f, m)
		if read(t, f, tenant).Fresh {
			t.Fatal("old queued event masqueraded as fresh progress")
		}
		hb := message(f, 2, uuid.Nil, 0, "")
		hb.OccurredAt = hb.OccurredAt.Add(time.Hour)
		apply(t, f, hb)
		var future bool
		if err := e.runtimePool.QueryRow(ctx, "SELECT progress_at>clock_timestamp() FROM core_lifecycle_pipelines WHERE producer=$1", f.producer).Scan(&future); err != nil || future {
			t.Fatal("producer clock extended health beyond receipt time")
		}
	})
	t.Run("immutable_receipts_and_no_current_authorization_writer", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		m := message(f, 1, tenant, 1, "active")
		apply(t, f, m)
		if _, err := e.runtimePool.Exec(ctx, "DELETE FROM core_integration_receipts WHERE producer=$1 AND event_id=$2", f.producer, m.EventID); err == nil {
			t.Fatal("runtime erased source evidence")
		}
		owner := mustPool(t, e.migrationDSN(primaryDB))
		defer owner.Close()
		if _, err := owner.Exec(ctx, "UPDATE core_integration_receipts SET outcome='older' WHERE producer=$1 AND event_id=$2", f.producer, m.EventID); err == nil {
			t.Fatal("receipt guard allowed mutation")
		}
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM tenant_lifecycle_projections").Scan(&n); err == nil {
			t.Fatal("runtime can still read the retired lifecycle projection")
		} else {
			assertPGCode(t, err, "42501")
		}
		if err := owner.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenant_lifecycle_projections)+(SELECT count(*) FROM tenant_access)+(SELECT count(*) FROM principals)").Scan(&n); err != nil || n != 0 {
			t.Fatal("projection wrote retired storage or created identity authority")
		}
	})
	t.Run("real_heartbeat_window_and_stale_after_silence", func(t *testing.T) {
		f := newFixture(t)
		tenant := mustV7(t)
		first := message(f, 1, tenant, 1, "active")
		apply(t, f, first)
		start := time.Now()
		var last biz.CoreProjectionMessage
		for seq := int64(2); seq <= 4; seq++ {
			timer := time.NewTimer(time.Until(start.Add(time.Duration(seq-1) * 10 * time.Second)))
			<-timer.C
			last = message(f, seq, uuid.Nil, 0, "")
			apply(t, f, last)
		}
		timer := time.NewTimer(time.Until(start.Add(32 * time.Second)))
		<-timer.C
		v := read(t, f, tenant)
		if !v.Fresh || v.Version != 1 || !v.EffectiveAt.Equal(first.EffectiveAt) || time.Since(start) < 32*time.Second {
			t.Fatal("unchanged Tenant expired despite heartbeat progress")
		}
		// Wait until 32 seconds after the final actual heartbeat receipt. This
		// deadline is observed in wall time; no injected Clock or row freshness.
		timer = time.NewTimer(time.Until(last.OccurredAt.Add(32 * time.Second)))
		<-timer.C
		if read(t, f, tenant).Fresh {
			t.Fatal("pipeline remained fresh after real silence")
		}
		r := apply(t, f, last)
		if !r.Duplicate || read(t, f, tenant).Fresh {
			t.Fatal("duplicate heartbeat revived expired pipeline")
		}
		apply(t, f, message(f, 5, uuid.Nil, 0, ""))
		if !read(t, f, tenant).Fresh {
			t.Fatal("new heartbeat did not resume source progress")
		}
	})
}
