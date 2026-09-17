package data

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

// This suite exercises the real PostgreSQL transaction component. It does not
// replace the required broker/current-authority, REST rebuild or runtime gates.
func TestWR33LifecyclePostgresComponent(t *testing.T) {
	secretFile := os.Getenv("WR33_POSTGRES_DSN_FILE")
	if secretFile == "" {
		t.Skip("requires task-owned Fedora PostgreSQL fixture")
	}
	raw, err := os.ReadFile(secretFile)
	if err != nil {
		t.Fatal("read task PostgreSQL connection file")
	}
	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal("parse task PostgreSQL connection file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect task PostgreSQL")
	}
	defer root.Close()
	var version string
	if err := root.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Log("PostgreSQL", version)
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(passwordBytes)
	// These roles live only inside this task's isolated private PG container.
	for _, role := range []string{"ani_iam_migrator", "ani_iam_runtime", "ani_iam_provisioner"} {
		var exists bool
		if err := root.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		action := "CREATE ROLE"
		if exists {
			action = "ALTER ROLE"
		}
		if _, err := root.Exec(ctx, action+" "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '"+password+"'"); err != nil {
			t.Fatal("initialize task roles")
		}
	}
	database := "wr33_lifecycle_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()+" OWNER ani_iam_migrator"); err != nil {
		t.Fatal(err)
	}
	t.Log("retained task database", database)
	migrationConfig := cfg.Copy()
	migrationConfig.ConnConfig.Database = database
	migrationConfig.ConnConfig.User = "ani_iam_migrator"
	migrationConfig.ConnConfig.Password = password
	migrator, err := pgxpool.NewWithConfig(ctx, migrationConfig)
	if err != nil {
		t.Fatal("connect migration role")
	}
	defer migrator.Close()
	migrations, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrator.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(path), err)
		}
	}
	runtimeConfig := migrationConfig.Copy()
	runtimeConfig.ConnConfig.User = "ani_iam_runtime"
	runtimeConfig.MaxConns = 4
	runtimeConfig.ConnConfig.RuntimeParams["ani_iam.tenant_producer"] = governancev1.Producer
	runtime, err := pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal("connect runtime role")
	}
	defer runtime.Close()
	var current string
	var privileged bool
	if err := runtime.QueryRow(ctx, "SELECT current_user,(SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole FROM pg_roles WHERE rolname=current_user)").Scan(&current, &privileged); err != nil || privileged || current != "ani_iam_runtime" {
		t.Fatal("runtime is privileged", err)
	}
	if err := ValidateRuntimeFoundation(ctx, NewData(runtime)); err != nil {
		t.Fatal("runtime schema guards", err)
	}
	q := sqlcgen.New(runtime)
	producer := governancev1.Producer
	if err := q.InitializeTenantLifecyclePipeline(ctx, sqlcgen.InitializeTenantLifecyclePipelineParams{Producer: producer}); err != nil {
		t.Fatal(err)
	}
	epoch, gen := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if err := q.CreateTenantLifecycleGeneration(ctx, sqlcgen.CreateTenantLifecycleGenerationParams{Producer: producer, ID: gen, Epoch: epoch, SnapshotID: uuid.Must(uuid.NewV7()), SnapshotWatermark: 0, CapturedAt: tenantFactTime(time.Now())}); err != nil {
		t.Fatal(err)
	}
	// An empty authoritative Snapshot is fixture setup, not a claim that REST
	// authority/rebuild has passed. All tested receipts go through product code.
	if _, err := runtime.Exec(ctx, "UPDATE tenant_lifecycle_pipelines SET generation_id=$2,epoch=$3,snapshot_required=false WHERE producer=$1", producer, gen, epoch); err != nil {
		t.Fatal(err)
	}
	makeEvent := func(tenant uuid.UUID, sequence, version int64, state string) biz.TenantBrokerDelivery {
		event := biz.TenantLifecycleEvent{EventID: uuid.Must(uuid.NewV7()), Epoch: epoch, TenantID: tenant, Sequence: sequence, TenantVersion: version, Status: state, Reason: "owner operation", OccurredAt: time.Now().UTC().Add(-time.Second)}
		event.EffectiveAt = event.OccurredAt
		wire := governancev1.LifecycleEvent{Revision: governancev1.Revision, Producer: governancev1.Producer, EventId: event.EventID.String(), Epoch: epoch.String(), TenantId: tenant.String(), Sequence: sequence, TenantVersion: version, BusinessStatus: governancev1.BusinessStatus(state), Reason: event.Reason, OccurredAt: event.OccurredAt, EffectiveAt: event.EffectiveAt}
		raw, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		return biz.TenantBrokerDelivery{EventID: event.EventID, Epoch: epoch, TenantID: tenant, Producer: producer, Kind: "lifecycle", Sequence: sequence, OccurredAt: event.OccurredAt, RawPayload: raw, Lifecycle: &event}
	}
	receive := func(d biz.TenantBrokerDelivery, rollback bool) (biz.TenantBrokerReceipt, error) {
		tx, err := runtime.Begin(ctx)
		if err != nil {
			return biz.TenantBrokerReceipt{}, err
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		result, err := applyTenantIntegration(ctx, sqlcgen.New(tx), producer, d)
		if err != nil {
			return result, err
		}
		if rollback {
			return result, nil
		}
		return result, tx.Commit(ctx)
	}
	tenantA, tenantB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	first := makeEvent(tenantA, 1, 1, "active")
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	receipts := make(chan biz.TenantBrokerReceipt, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); receipt, err := receive(first, false); failures <- err; receipts <- receipt }()
	}
	wg.Wait()
	close(failures)
	close(receipts)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent receive", err)
		}
	}
	original := 0
	for receipt := range receipts {
		if !receipt.Duplicate {
			original++
		}
	}
	if original != 1 {
		t.Fatal("duplicate effects", original)
	}
	// A changed decoded domain with identical wire bytes must not reuse a receipt.
	altered := first
	life := *first.Lifecycle
	life.Status = "frozen"
	altered.Lifecycle = &life
	if _, err := receive(altered, false); !errors.Is(err, biz.ErrTenantLifecycleConflict) {
		t.Fatal("domain/wire mismatch reused receipt", err)
	}
	rolled := makeEvent(tenantA, 2, 2, "frozen")
	if _, err := receive(rolled, true); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ReadTenantIntegrationReceipt(ctx, sqlcgen.ReadTenantIntegrationReceiptParams{Producer: producer, EventID: rolled.EventID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("rollback left receipt", err)
	}
	fact, err := q.ReadTenantLifecycleFact(ctx, sqlcgen.ReadTenantLifecycleFactParams{Producer: producer, GenerationID: gen, TenantID: tenantA})
	if err != nil || fact.TenantVersion != 1 || fact.BusinessStatus != "active" {
		t.Fatal("rollback left projection", err)
	}
	if _, err := receive(rolled, false); err != nil {
		t.Fatal(err)
	}
	if _, err := receive(makeEvent(tenantB, 3, 1, "active"), false); err != nil {
		t.Fatal(err)
	}
	// A new event ID cannot occupy an already committed epoch/sequence slot.
	if _, err := receive(makeEvent(tenantB, 2, 1, "active"), false); !errors.Is(err, biz.ErrTenantLifecycleConflict) {
		t.Fatal("source position conflict admitted", err)
	}
	crossed := makeEvent(tenantB, 4, 2, "frozen")
	crossed.TenantID = tenantA
	crossed.Lifecycle.TenantID = tenantA
	crossed.Lifecycle.TenantVersion = 3
	crossed.Lifecycle.Status = "active"
	if _, err := receive(crossed, false); err == nil {
		t.Fatal("cross-Tenant raw binding accepted")
	}
	fact, err = q.ReadTenantLifecycleFact(ctx, sqlcgen.ReadTenantLifecycleFactParams{Producer: producer, GenerationID: gen, TenantID: tenantB})
	if err != nil || fact.TenantVersion != 1 {
		t.Fatal("failed cross-Tenant transaction changed fact", err)
	}
	if _, err := q.ReadTenantLifecycleProjection(ctx, sqlcgen.ReadTenantLifecycleProjectionParams{Producer: producer, TenantID: uuid.Must(uuid.NewV7())}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("Tenant-scoped query leaked a fact", err)
	}
	heartbeat := biz.TenantLifecycleHeartbeat{ID: uuid.Must(uuid.NewV7()), Epoch: epoch, Committed: 3, Published: 3, ObservedAt: time.Now().UTC().Add(-time.Second)}
	wire := governancev1.Heartbeat{Revision: governancev1.Revision, Producer: governancev1.Producer, HeartbeatId: heartbeat.ID.String(), Epoch: epoch.String(), CommittedSequence: 3, PublishedSequence: 3, ObservedAt: heartbeat.ObservedAt}
	raw, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	h := biz.TenantBrokerDelivery{EventID: heartbeat.ID, Epoch: epoch, Producer: producer, Kind: "heartbeat", OccurredAt: heartbeat.ObservedAt, RawPayload: raw, Heartbeat: &heartbeat}
	if _, err := receive(h, false); err != nil {
		t.Fatal(err)
	}
	var fresh bool
	if err := runtime.QueryRow(ctx, "SELECT fresh_until>statement_timestamp() FROM tenant_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2", producer, tenantA).Scan(&fresh); err != nil || !fresh {
		t.Fatal("current heartbeat failed freshness", err)
	}
	before, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receive(h, false); err != nil {
		t.Fatal(err)
	}
	after, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer})
	if err != nil || after.AppliedSequence != 3 || !after.HeartbeatReceivedAt.Time.Equal(before.HeartbeatReceivedAt.Time) {
		t.Fatal("replay refreshed clock or advanced sequence", err)
	}
	// Close/reopen the real runtime pool to prove the state is not process-local.
	runtime.Close()
	runtime, err = pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal("reconnect runtime")
	}
	defer runtime.Close()
	q = sqlcgen.New(runtime)
	if _, err := q.ReadTenantIntegrationReceipt(ctx, sqlcgen.ReadTenantIntegrationReceiptParams{Producer: producer, EventID: first.EventID}); err != nil {
		t.Fatal("receipt lost after reconnect", err)
	}
	gap, err := receive(makeEvent(tenantA, 5, 3, "active"), false)
	if err != nil || gap.Outcome != "sequence_gap" || gap.Applied != 3 || gap.Highest != 5 {
		t.Fatal("gap applied future fact", gap, err)
	}
	late, err := receive(makeEvent(tenantB, 4, 2, "frozen"), false)
	if err != nil || late.Outcome != "awaiting_snapshot" || late.Applied != 3 {
		t.Fatal("late delta repaired without snapshot", late, err)
	}
	if err := runtime.QueryRow(ctx, "SELECT fresh_until>statement_timestamp() FROM tenant_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2", producer, tenantA).Scan(&fresh); err != nil || fresh {
		t.Fatal("gap stayed fresh", err)
	}
	if _, err := runtime.Exec(ctx, "UPDATE tenant_integration_receipts SET outcome='covered' WHERE producer=$1", producer); err == nil {
		t.Fatal("runtime changed immutable evidence")
	}
	if _, err := runtime.Exec(ctx, "DELETE FROM tenant_lifecycle_generations WHERE producer=$1", producer); err == nil {
		t.Fatal("runtime deleted generations")
	}
	var identities int
	if err := runtime.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenant_memberships)+(SELECT count(*) FROM principals)").Scan(&identities); err != nil || identities != 0 {
		t.Fatal("Lifecycle created identity", err)
	}
	runWR33BrokerPostgresComponent(t, ctx, migrator, runtime)
	t.Log("real PG concurrent idempotency, raw/domain binding, rollback, tenant isolation, heartbeat continuity, gap fail-closed and reconnect passed")
}
