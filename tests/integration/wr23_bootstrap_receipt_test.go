//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// This gate exercises the real restricted PostgreSQL repository. Inputs are
// domain fixtures; broker attribution, consumer ACK and worker activation are
// explicitly outside this component's evidence grade.
func TestWR23CoreBootstrapReceipt(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	d := data.NewData(e.runtimePool)
	repo := data.NewCoreBootstrapReceiptRepository(d)
	var seq int64
	fixture := func() biz.CoreBootstrapDelivery {
		i := biz.CoreBootstrapIntent{TenantID: mustV7(t), OperationID: mustV7(t), NormalizedEmail: "invited@example.test", Locale: "en-US"}
		preimage, _ := json.Marshal(map[string]string{"locale": i.Locale, "normalized_email": i.NormalizedEmail, "operation_id": i.OperationID.String(), "tenant_id": i.TenantID.String()})
		sum := sha256.Sum256(preimage)
		i.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		return biz.CoreBootstrapDelivery{Intent: i, EventID: mustV7(t), Producer: "core.wr23.test", SourceSequence: atomic.AddInt64(&seq, 1), OccurredAt: time.Now().UTC().Truncate(time.Microsecond), RawPayload: preimage}
	}
	count := func(t *testing.T, table string, tenant uuid.UUID) int {
		t.Helper()
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&n); err != nil {
			t.Fatal("read isolated receipt count")
		}
		return n
	}
	assertEmpty := func(t *testing.T, f biz.CoreBootstrapDelivery) {
		t.Helper()
		for _, table := range []string{"tenant_bootstrap_operations", "core_bootstrap_receipts"} {
			if count(t, table, f.Intent.TenantID) != 0 {
				t.Fatal("failed transaction left partial state")
			}
		}
	}
	assertNoAuthority := func(t *testing.T, f biz.CoreBootstrapDelivery) {
		t.Helper()
		for _, table := range []string{"tenant_access", "tenant_memberships", "tenant_roles", "tenant_role_bindings", "tenant_invitations"} {
			if count(t, table, f.Intent.TenantID) != 0 {
				t.Fatal("receipt created premature authority")
			}
		}
	}
	t.Run("restricted_schema_and_fresh_database", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM principals").Scan(&n); err != nil || n != 0 {
			t.Fatal("unexpected Principal seed")
		}
	})
	t.Run("atomic_intent_receipt_and_exact_retry", func(t *testing.T) {
		f := fixture()
		first, err := repo.Receive(ctx, f)
		if err != nil || !first.OperationCreated || first.DuplicateEvent || first.ReceivedAt.IsZero() {
			t.Fatalf("receipt: %v", err)
		}
		// Reconstruct the repository to prove the result is not held in memory.
		second, err := data.NewCoreBootstrapReceiptRepository(data.NewData(e.runtimePool)).Receive(ctx, f)
		if err != nil || second.OperationCreated || !second.DuplicateEvent || !second.ReceivedAt.Equal(first.ReceivedAt) {
			t.Fatalf("stable retry: %v", err)
		}
		if count(t, "tenant_bootstrap_operations", f.Intent.TenantID) != 1 || count(t, "core_bootstrap_receipts", f.Intent.TenantID) != 1 {
			t.Fatal("duplicate durable state")
		}
		assertNoAuthority(t, f)
	})
	t.Run("concurrent_duplicate_delivery", func(t *testing.T) {
		f := fixture()
		var wg sync.WaitGroup
		var created, duplicates atomic.Int64
		for range 8 {
			wg.Go(func() {
				v, err := repo.Receive(ctx, f)
				if err != nil {
					t.Error(err)
					return
				}
				if v.OperationCreated {
					created.Add(1)
				}
				if v.DuplicateEvent {
					duplicates.Add(1)
				}
			})
		}
		wg.Wait()
		if created.Load() != 1 || duplicates.Load() != 7 {
			t.Fatal("concurrent replay produced multiple operations")
		}
	})
	t.Run("same_operation_new_event_does_not_reinitialize", func(t *testing.T) {
		f := fixture()
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
		f.EventID = mustV7(t)
		f.SourceSequence = atomic.AddInt64(&seq, 1)
		v, err := repo.Receive(ctx, f)
		if err != nil || v.OperationCreated || v.DuplicateEvent {
			t.Fatalf("operation retry: %v", err)
		}
		if count(t, "tenant_bootstrap_operations", f.Intent.TenantID) != 1 || count(t, "core_bootstrap_receipts", f.Intent.TenantID) != 2 {
			t.Fatal("operation identity lost")
		}
	})
	t.Run("changed_event_bytes_and_position_conflict", func(t *testing.T) {
		f := fixture()
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
		for name, change := range map[string]func(*biz.CoreBootstrapDelivery){"payload": func(d *biz.CoreBootstrapDelivery) { d.RawPayload = []byte(`{"changed":true}`) }, "sequence": func(d *biz.CoreBootstrapDelivery) { d.SourceSequence++ }, "producer": func(d *biz.CoreBootstrapDelivery) { d.Producer = "other" }, "time": func(d *biz.CoreBootstrapDelivery) { d.OccurredAt = d.OccurredAt.Add(time.Second) }} {
			t.Run(name, func(t *testing.T) {
				bad := f
				change(&bad)
				if _, err := repo.Receive(ctx, bad); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
					t.Fatalf("changed receipt accepted: %v", err)
				}
			})
		}
	})
	t.Run("cross_tenant_event_and_operation_ids_rejected", func(t *testing.T) {
		f := fixture()
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"event", "operation", "source"} {
			t.Run(kind, func(t *testing.T) {
				bad := fixture()
				switch kind {
				case "event":
					bad.EventID = f.EventID
				case "source":
					bad.SourceSequence = f.SourceSequence
				case "operation":
					bad.Intent.OperationID = f.Intent.OperationID
					raw, _ := json.Marshal(map[string]string{"locale": bad.Intent.Locale, "normalized_email": bad.Intent.NormalizedEmail, "operation_id": bad.Intent.OperationID.String(), "tenant_id": bad.Intent.TenantID.String()})
					sum := sha256.Sum256(raw)
					bad.Intent.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
					bad.RawPayload = raw
				}
				if _, err := repo.Receive(ctx, bad); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
					t.Fatalf("cross Tenant %s: %v", kind, err)
				}
				assertEmpty(t, bad)
			})
		}
	})
	t.Run("second_current_operation_conflicts", func(t *testing.T) {
		f := fixture()
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
		bad := fixture()
		bad.Intent.TenantID = f.Intent.TenantID
		raw, _ := json.Marshal(map[string]string{"locale": bad.Intent.Locale, "normalized_email": bad.Intent.NormalizedEmail, "operation_id": bad.Intent.OperationID.String(), "tenant_id": bad.Intent.TenantID.String()})
		sum := sha256.Sum256(raw)
		bad.Intent.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		bad.RawPayload = raw
		if _, err := repo.Receive(ctx, bad); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
			t.Fatal("second current intent accepted")
		}
		if count(t, "core_bootstrap_receipts", f.Intent.TenantID) != 1 {
			t.Fatal("conflicting intent was acknowledged")
		}
	})
	t.Run("receipt_insert_failure_rolls_back_operation", func(t *testing.T) {
		owner := mustPool(t, e.migrationDSN(primaryDB))
		defer owner.Close()
		if _, err := owner.Exec(ctx, "REVOKE INSERT ON core_bootstrap_receipts FROM ani_iam_runtime"); err != nil {
			t.Fatal("fault injection failed")
		}
		defer func() {
			if _, err := owner.Exec(ctx, "GRANT INSERT ON core_bootstrap_receipts TO ani_iam_runtime"); err != nil {
				t.Error("restore receipt privilege failed")
			}
		}()
		f := fixture()
		if _, err := repo.Receive(ctx, f); !errors.Is(err, biz.ErrPersistencePermissionDenied) {
			t.Fatalf("injected receipt failure: %v", err)
		}
		assertEmpty(t, f)
		if err := data.ValidateRuntimeFoundation(ctx, d); err == nil {
			t.Fatal("startup accepted missing receipt privilege")
		}
		if _, err := owner.Exec(ctx, "GRANT INSERT ON core_bootstrap_receipts TO ani_iam_runtime"); err != nil {
			t.Fatal("restore failed")
		}
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("receipt_is_immutable_even_with_accidental_update_grant", func(t *testing.T) {
		f := fixture()
		if _, err := repo.Receive(ctx, f); err != nil {
			t.Fatal(err)
		}
		if _, err := e.runtimePool.Exec(ctx, "DELETE FROM core_bootstrap_receipts WHERE tenant_id=$1 AND event_id=$2", f.Intent.TenantID, f.EventID); err == nil {
			t.Fatal("runtime deleted receipt")
		}
		owner := mustPool(t, e.migrationDSN(primaryDB))
		defer owner.Close()
		if _, err := owner.Exec(ctx, "GRANT UPDATE ON core_bootstrap_receipts TO ani_iam_runtime"); err != nil {
			t.Fatal("own grant injection failed")
		}
		defer owner.Exec(ctx, "REVOKE UPDATE ON core_bootstrap_receipts FROM ani_iam_runtime")
		if err := data.ValidateRuntimeFoundation(ctx, d); err == nil {
			t.Fatal("startup accepted mutable receipts")
		}
		if _, err := e.runtimePool.Exec(ctx, "UPDATE core_bootstrap_receipts SET source_sequence=999 WHERE tenant_id=$1 AND event_id=$2", f.Intent.TenantID, f.EventID); err == nil {
			t.Fatal("guard allowed receipt rewrite")
		}
	})
}
