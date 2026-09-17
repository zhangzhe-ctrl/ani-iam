//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Cursor and consumer IDs are component fixtures, not authenticated REST or
// NATS evidence. Actual restricted PostgreSQL and real elapsed expiry are used.
func TestWR23CoreSnapshotRebuild(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	d := data.NewData(e.runtimePool)
	admin := mustPool(t, e.migrationDSN(primaryDB))
	defer admin.Close()
	type fixture struct {
		producer string
		live     biz.CoreLifecycleProjectionRepository
		rebuild  biz.CoreSnapshotRebuildRepository
	}
	fixtureFor := func(t *testing.T) fixture {
		t.Helper()
		p := "core.snapshot." + mustV7(t).String()
		l, err := data.NewCoreLifecycleProjectionRepository(d, p)
		if err != nil {
			t.Fatal(err)
		}
		if err = l.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		r, err := data.NewCoreSnapshotRebuildRepository(d, p)
		if err != nil {
			t.Fatal(err)
		}
		return fixture{p, l, r}
	}
	message := func(f fixture, seq int64, id uuid.UUID, version int64, status string) biz.CoreProjectionMessage {
		m := biz.CoreProjectionMessage{Producer: f.producer, SourceSequence: seq, EventID: mustV7(t), TenantID: id, OccurredAt: time.Now().UTC().Truncate(time.Microsecond), RawPayload: []byte(`{"fixture":"snapshot-component"}`), Kind: "heartbeat"}
		if id != uuid.Nil {
			m.Kind = "lifecycle"
			m.LifecycleVersion = version
			m.Status = status
			m.EffectiveAt = m.OccurredAt
			switch status {
			case "active":
				m.Reason = "restored"
				if version == 1 {
					m.Reason = "created"
				}
			case "frozen":
				m.Reason = "administrative_freeze"
			case "disabled":
				m.Reason = "user_requested_deactivation"
			}
		}
		return m
	}
	apply := func(t *testing.T, f fixture, m biz.CoreProjectionMessage) {
		t.Helper()
		if _, err := f.live.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	read := func(t *testing.T, f fixture, id uuid.UUID) biz.CoreShadowTenant {
		t.Helper()
		r, err := f.live.ReadShadowTenant(ctx, mustTenantScope(t, id))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	cursor := func(f fixture, cut int64, size int32) biz.CoreSnapshotCursor {
		return biz.CoreSnapshotCursor{ID: uuid.New(), ConsumerID: mustV7(t), Producer: f.producer, SourceCut: cut, BrokerAfter: cut, PageSize: size, ExpiresAt: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	}
	begin := func(t *testing.T, f fixture, c biz.CoreSnapshotCursor) biz.CoreSnapshotBuild {
		t.Helper()
		r, err := f.rebuild.Begin(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	load := func(t *testing.T, f fixture, p biz.CoreSnapshotPage) biz.CoreSnapshotBuild {
		t.Helper()
		r, err := f.rebuild.LoadPage(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	activate := func(t *testing.T, f fixture, id uuid.UUID) biz.CoreSnapshotBuild {
		t.Helper()
		r, err := f.rebuild.ActivateShadow(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	t.Run("schema_and_restartable_pages_then_buffered_switch", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
			t.Fatal(err)
		}
		f := fixtureFor(t)
		ids := []uuid.UUID{mustV7(t), mustV7(t), mustV7(t)}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		for i, id := range ids {
			apply(t, f, message(f, int64(i+1), id, 1, "active"))
		}
		old := read(t, f, ids[0])
		c := cursor(f, 3, 2)
		b := begin(t, f, c)
		token := uuid.NewString()
		p := biz.CoreSnapshotPage{Cursor: c, NextToken: token, Items: []biz.CoreSnapshotFact{{TenantID: ids[0], Version: 1, Status: "active"}, {TenantID: ids[1], Version: 1, Status: "active"}}}
		if v := load(t, f, p); v.State != "loading" || v.LoadedItems != 2 {
			t.Fatal("page not durable")
		}
		f.rebuild, _ = data.NewCoreSnapshotRebuildRepository(d, f.producer)
		if again := begin(t, f, c); again.GenerationID != b.GenerationID || again.NextToken != token {
			t.Fatal("restart lost cursor")
		}
		load(t, f, p)
		bad := p
		bad.Items = append([]biz.CoreSnapshotFact(nil), p.Items...)
		bad.Items[0].Version = 2
		if _, err := f.rebuild.LoadPage(ctx, bad); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("changed page accepted")
		}
		if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
			t.Fatal("partial Snapshot activated")
		}
		p2 := biz.CoreSnapshotPage{Cursor: c, RequestToken: token, Items: []biz.CoreSnapshotFact{{TenantID: ids[2], Version: 1, Status: "active"}}}
		load(t, f, p2)
		apply(t, f, message(f, 4, ids[0], 2, "frozen"))
		apply(t, f, message(f, 5, ids[0], 3, "active"))
		if read(t, f, ids[0]).GenerationID != old.GenerationID {
			t.Fatal("staging leaked to active generation")
		}
		if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
			t.Fatal("buffer skipped")
		}
		if _, err := f.rebuild.CatchUp(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		activate(t, f, c.ID)
		v := read(t, f, ids[0])
		if v.GenerationID != b.GenerationID || v.Version != 3 || v.SnapshotBase || !v.Fresh {
			t.Fatal("buffered switch lost full latest fact")
		}
		v = read(t, f, ids[1])
		if !v.SnapshotBase || v.Reason != "" || !v.EffectiveAt.IsZero() {
			t.Fatal("invented Snapshot reason/time")
		}
		activate(t, f, c.ID)
	})
	t.Run("version_gap_repaired_without_fabricated_fields", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		apply(t, f, message(f, 1, id, 1, "active"))
		apply(t, f, message(f, 2, id, 3, "frozen"))
		if !read(t, f, id).RepairRequired {
			t.Fatal("missing gap")
		}
		c := cursor(f, 2, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 3, Status: "frozen"}}})
		activate(t, f, c.ID)
		v := read(t, f, id)
		if v.RepairRequired || !v.Fresh || v.Version != 3 || !v.SnapshotBase {
			t.Fatal("Snapshot failed to repair Tenant gap")
		}
		apply(t, f, message(f, 3, id, 3, "frozen"))
		if v = read(t, f, id); v.SnapshotBase || v.Reason != "administrative_freeze" || v.EffectiveAt.IsZero() {
			t.Fatal("matching complete event did not supply unknown metadata")
		}
		if _, err := f.live.Apply(ctx, message(f, 4, id, 3, "active")); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("same version changed status")
		}
	})
	t.Run("snapshot_cut_cannot_activate_over_unknown_source_holes", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		first := message(f, 1, id, 1, "active")
		apply(t, f, first)
		before := read(t, f, id)
		apply(t, f, message(f, 3, uuid.Nil, 0, ""))
		c := cursor(f, 3, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}})
		if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
			t.Fatalf("activation over unknown source: %v", err)
		}
		v := read(t, f, id)
		if v.Fresh || v.GenerationID != before.GenerationID || v.ContiguousSequence != 1 || v.HighestSequence != 3 {
			t.Fatal("Snapshot skipped unknown Bootstrap/source history")
		}
		apply(t, f, first)
		if read(t, f, id).Fresh {
			t.Fatal("old duplicate revived missing source")
		}
		apply(t, f, message(f, 2, uuid.Nil, 0, ""))
		activate(t, f, c.ID)
		if v = read(t, f, id); !v.Fresh || !v.SnapshotBase || v.ContiguousSequence != 3 {
			t.Fatal("real missing receipt did not permit complete cut")
		}
	})
	t.Run("cold_snapshot_cannot_fabricate_unreceived_source_history", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		c := cursor(f, 2, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}})
		if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
			t.Fatalf("cold activation over missing receipts: %v", err)
		}
		if _, err := f.live.ReadShadowTenant(ctx, mustTenantScope(t, id)); !errors.Is(err, biz.ErrCoreProjectionMissing) {
			t.Fatal("unreceived history became current")
		}
		apply(t, f, message(f, 1, id, 1, "active"))
		apply(t, f, message(f, 2, uuid.Nil, 0, ""))
		activate(t, f, c.ID)
		if v := read(t, f, id); !v.Fresh || !v.SnapshotBase || v.ContiguousSequence != 2 {
			t.Fatal("received complete history failed to activate")
		}
	})
	t.Run("missing_source_must_arrive_before_cut_coverage_is_trusted", func(t *testing.T) {
		for _, kind := range []string{"omitted_tenant", "newer_version", "exact_state"} {
			t.Run(kind, func(t *testing.T) {
				f := fixtureFor(t)
				id := mustV7(t)
				apply(t, f, message(f, 1, id, 1, "active"))
				before := read(t, f, id)
				apply(t, f, message(f, 3, uuid.Nil, 0, ""))
				c := cursor(f, 3, 1)
				begin(t, f, c)
				load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 2, Status: "frozen"}}})
				if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
					t.Fatalf("incomplete cut accepted: %v", err)
				}
				m := message(f, 2, id, 2, "frozen")
				switch kind {
				case "omitted_tenant":
					m = message(f, 2, mustV7(t), 1, "active")
				case "newer_version":
					m = message(f, 2, id, 3, "disabled")
				}
				apply(t, f, m)
				_, err := f.rebuild.ActivateShadow(ctx, c.ID)
				v := read(t, f, id)
				if kind == "exact_state" {
					if err != nil || v.Version != 2 || !v.SnapshotBase || !v.Fresh {
						t.Fatalf("complete covered state rejected: %v", err)
					}
				} else if !errors.Is(err, biz.ErrCoreProjectionConflict) || v.GenerationID != before.GenerationID {
					t.Fatalf("cut hid received Tenant/version: %v", err)
				}
			})
		}
	})
	t.Run("coverage_prevents_regression_omission_and_contradiction", func(t *testing.T) {
		for _, kind := range []string{"omit", "older", "same_version_status", "disabled_revival"} {
			t.Run(kind, func(t *testing.T) {
				f := fixtureFor(t)
				id := mustV7(t)
				apply(t, f, message(f, 1, id, 1, "active"))
				status := "frozen"
				if kind == "disabled_revival" {
					status = "disabled"
				}
				apply(t, f, message(f, 2, id, 2, status))
				old := read(t, f, id)
				c := cursor(f, 2, 1)
				begin(t, f, c)
				items := []biz.CoreSnapshotFact{{TenantID: id, Version: 2, Status: status}}
				switch kind {
				case "omit":
					items = nil
				case "older":
					items[0].Version = 1
					items[0].Status = "active"
				case "same_version_status":
					items[0].Status = "active"
				case "disabled_revival":
					items[0].Version = 3
					items[0].Status = "active"
				}
				load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: items})
				if _, err := f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreProjectionConflict) {
					t.Fatal("invalid replacement activated")
				}
				if read(t, f, id).GenerationID != old.GenerationID {
					t.Fatal("failed switch changed pointer")
				}
			})
		}
	})
	t.Run("page_order_cursor_binding_and_stale_build_fencing", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		apply(t, f, message(f, 1, id, 1, "active"))
		a, b := cursor(f, 1, 1), cursor(f, 1, 1)
		begin(t, f, a)
		begin(t, f, b)
		bad := biz.CoreSnapshotPage{Cursor: a, RequestToken: uuid.NewString(), Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}}
		if _, err := f.rebuild.LoadPage(ctx, bad); err == nil {
			t.Fatal("skipped first page")
		}
		bad.RequestToken = ""
		bad.Cursor.ConsumerID = mustV7(t)
		if _, err := f.rebuild.LoadPage(ctx, bad); err == nil {
			t.Fatal("changed durable consumer binding")
		}
		p := biz.CoreSnapshotPage{Cursor: a, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}}
		load(t, f, p)
		p.Cursor = b
		load(t, f, p)
		activate(t, f, a.ID)
		if _, err := f.rebuild.ActivateShadow(ctx, b.ID); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("stale generation replaced newer rebuild")
		}
	})
	t.Run("real_cursor_expiry_blocks_unfinished_pages", func(t *testing.T) {
		f := fixtureFor(t)
		c := cursor(f, 0, 1)
		c.ExpiresAt = time.Now().Add(-time.Second)
		if _, err := f.rebuild.Begin(ctx, c); err == nil {
			t.Fatal("expired cursor began")
		}
		c.ID = uuid.New()
		c.ExpiresAt = time.Now().Add(250 * time.Millisecond).UTC().Truncate(time.Microsecond)
		begin(t, f, c)
		timer := time.NewTimer(time.Until(c.ExpiresAt.Add(25 * time.Millisecond)))
		<-timer.C
		if _, err := f.rebuild.LoadPage(ctx, biz.CoreSnapshotPage{Cursor: c}); err == nil {
			t.Fatal("expired cursor accepted new page")
		}
	})
	t.Run("page_receipt_failure_rolls_back_facts", func(t *testing.T) {
		f := fixtureFor(t)
		c := cursor(f, 0, 1)
		b := begin(t, f, c)
		id := mustV7(t)
		if _, err := admin.Exec(ctx, "REVOKE INSERT ON core_lifecycle_rebuild_pages FROM ani_iam_runtime"); err != nil {
			t.Fatal(err)
		}
		_, err := f.rebuild.LoadPage(ctx, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}})
		_, restore := admin.Exec(ctx, "GRANT INSERT ON core_lifecycle_rebuild_pages TO ani_iam_runtime")
		if restore != nil {
			t.Fatal(restore)
		}
		if err == nil {
			t.Fatal("page receipt fault not propagated")
		}
		var n int
		if err = e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM core_lifecycle_projection_rows WHERE producer=$1 AND generation_id=$2", f.producer, b.GenerationID).Scan(&n); err != nil || n != 0 {
			t.Fatal("failed page left facts")
		}
	})
	t.Run("activation_record_failure_rolls_back_generation_pointer", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		apply(t, f, message(f, 1, id, 1, "active"))
		old := read(t, f, id)
		c := cursor(f, 1, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}})
		// Fail after the pipeline pointer UPDATE, when recording activation.
		if _, err := admin.Exec(ctx, `CREATE FUNCTION wr23_snapshot_activation_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated Snapshot activation record fault'; END $$;
CREATE TRIGGER wr23_snapshot_activation_fault BEFORE UPDATE ON core_lifecycle_rebuilds FOR EACH ROW WHEN (NEW.state='activated') EXECUTE FUNCTION wr23_snapshot_activation_fault()`); err != nil {
			t.Fatal(err)
		}
		_, err := f.rebuild.ActivateShadow(ctx, c.ID)
		_, restore := admin.Exec(ctx, `DROP TRIGGER wr23_snapshot_activation_fault ON core_lifecycle_rebuilds; DROP FUNCTION wr23_snapshot_activation_fault()`)

		if restore != nil {
			t.Fatal(restore)
		}
		if err == nil {
			t.Fatal("activation fault not propagated")
		}
		if read(t, f, id).GenerationID != old.GenerationID {
			t.Fatal("failed activation changed pointer")
		}
		activate(t, f, c.ID)
	})
	t.Run("bounded_replay_does_not_skip_last_increment", func(t *testing.T) {
		f := fixtureFor(t)
		id := mustV7(t)
		apply(t, f, message(f, 1, id, 1, "active"))
		c := cursor(f, 1, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c, Items: []biz.CoreSnapshotFact{{TenantID: id, Version: 1, Status: "active"}}})
		for seq := int64(2); seq <= 502; seq++ {
			status := "active"
			if seq%2 == 0 {
				status = "frozen"
			}
			apply(t, f, message(f, seq, id, seq, status))
		}
		r, err := f.rebuild.CatchUp(ctx, c.ID)
		if err != nil || r.AppliedThrough != 501 {
			t.Fatal("replay did not respect 500 fact boundary", err)
		}
		if _, err = f.rebuild.ActivateShadow(ctx, c.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
			t.Fatal("remaining increment skipped")
		}
		if _, err = f.rebuild.CatchUp(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		activate(t, f, c.ID)
		if read(t, f, id).Version != 502 {
			t.Fatal("last increment missing")
		}
	})
	t.Run("concurrent_new_tenants_survive_snapshot_switch", func(t *testing.T) {
		f := fixtureFor(t)
		c := cursor(f, 0, 1)
		begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c})
		messages := make([]biz.CoreProjectionMessage, 12)
		for i := range messages {
			messages[i] = message(f, int64(i+1), mustV7(t), 1, "active")
		}
		var wg sync.WaitGroup
		for _, m := range messages {
			wg.Go(func() {
				if _, err := f.live.Apply(ctx, m); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if _, err := f.rebuild.CatchUp(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		activate(t, f, c.ID)
		for _, m := range messages {
			if v := read(t, f, m.TenantID); !v.Fresh || v.Version != 1 {
				t.Fatal("concurrent Tenant lost")
			}
		}
	})
	t.Run("immutable_generation_and_page_history_no_identity_writes", func(t *testing.T) {
		f := fixtureFor(t)
		c := cursor(f, 0, 1)
		b := begin(t, f, c)
		load(t, f, biz.CoreSnapshotPage{Cursor: c})
		activate(t, f, c.ID)
		for _, table := range []string{"core_lifecycle_generations", "core_lifecycle_rebuild_pages"} {
			if _, err := admin.Exec(ctx, "DELETE FROM "+table+" WHERE producer=$1", f.producer); err == nil {
				t.Fatal("immutable history erased")
			}
		}
		if _, err := admin.Exec(ctx, "UPDATE core_lifecycle_rebuilds SET source_cut=source_cut+1,applied_through=applied_through+1 WHERE producer=$1 AND generation_id=$2", f.producer, b.GenerationID); err == nil {
			t.Fatal("cursor identity rewritten")
		}
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM tenant_lifecycle_projections").Scan(&n); err == nil {
			t.Fatal("runtime can still read the retired lifecycle projection")
		} else {
			assertPGCode(t, err, "42501")
		}
		if err := admin.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenant_lifecycle_projections)+(SELECT count(*) FROM tenant_access)+(SELECT count(*) FROM principals)").Scan(&n); err != nil || n != 0 {
			t.Fatal("rebuild wrote retired storage or created identity authority")
		}
	})
}
