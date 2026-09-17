//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"strings"
	"testing"
	"time"
)

type wr23ScheduledExecutor struct {
	worker  *biz.CoreBootstrapWorker
	failure error
}

func (w *wr23ScheduledExecutor) Reconcile(ctx context.Context, s biz.TenantScope, id uuid.UUID) (biz.CoreBootstrapWorkerResult, error) {
	if w.failure != nil {
		return biz.CoreBootstrapWorkerResult{}, w.failure
	}
	return w.worker.Reconcile(ctx, s, id)
}
func (w *wr23ScheduledExecutor) ExpireInvitation(ctx context.Context, s biz.TenantScope, id uuid.UUID, generation int64) (biz.CoreBootstrapWorkerResult, error) {
	if w.failure != nil {
		return biz.CoreBootstrapWorkerResult{}, w.failure
	}
	return w.worker.ExpireInvitation(ctx, s, id, generation)
}

// Real PG/provisioner/outbox and dispatcher. Authority is the same explicit
// component double as the worker test. No NATS, formal loop or SMTP claim.
func TestWR23CoreBootstrapDispatcher(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	admin := mustPool(t, e.migrationDSN(primaryDB))
	defer admin.Close()
	provisioner := mustPool(t, postgresDSN(provisionerRole, e.provisionerPass, e.host, primaryDB, "wr23-dispatch-provisioner"))
	defer provisioner.Close()
	owner, manifest := newBootstrapManifest(t, strings.Repeat("a", 64))
	second := manifest.Workloads[0]
	second.PrincipalID = mustV7(t)
	second.BindingID = mustV7(t)
	second.Name = "bootstrap-dispatcher"
	second.DNSIdentity = "bootstrap-dispatcher.wr19.test"
	second.Grants = append([]biz.BootstrapWorkloadGrant(nil), second.Grants...)
	for i := range second.Grants {
		second.Grants[i].ID = mustV7(t)
	}
	manifest.Workloads = append(manifest.Workloads, second)
	if _, err := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(provisioner), wr32Registry(t)), data.NewSystemClock(), wr32Registry(t)).Provision(ctx, owner, manifest); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("generate isolated key")
	}
	protector, err := data.NewOutboxProtector("wr23-schedule", map[string][]byte{"wr23-schedule": key})
	if err != nil {
		t.Fatal(err)
	}
	d := data.NewData(e.runtimePool, protector)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		tenant, operation uuid.UUID
		scope             biz.TenantScope
		producer          string
		projection        biz.CoreLifecycleProjectionRepository
		queue             biz.CoreBootstrapQueue
		worker            *biz.CoreBootstrapWorker
		authority         *wr23BootstrapAuthority
	}
	newFixture := func(t *testing.T, producer string, start int64) fixture {
		t.Helper()
		if producer == "" {
			producer = "core.dispatch." + mustV7(t).String()
		}
		f := fixture{tenant: mustV7(t), operation: mustV7(t), producer: producer}
		f.scope = mustTenantScope(t, f.tenant)
		f.projection, err = data.NewCoreLifecycleProjectionRepository(d, producer)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.projection.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: producer, Kind: "lifecycle", SourceSequence: start + 1, OccurredAt: now, RawPayload: []byte(`{"fixture":"dispatcher"}`), LifecycleVersion: 1, Status: "active", Reason: "created", EffectiveAt: now}); err != nil {
			t.Fatal(err)
		}
		intent := biz.CoreBootstrapIntent{TenantID: f.tenant, OperationID: f.operation, NormalizedEmail: "invited@example.test", Locale: "en-US"}
		raw, _ := json.Marshal(map[string]string{"tenant_id": f.tenant.String(), "operation_id": f.operation.String(), "normalized_email": intent.NormalizedEmail, "locale": intent.Locale})
		sum := sha256.Sum256(raw)
		intent.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		event := mustV7(t)
		if _, err = data.NewCoreBootstrapReceiptRepository(d).Receive(ctx, biz.CoreBootstrapDelivery{Intent: intent, EventID: event, Producer: producer, SourceSequence: start + 2, OccurredAt: now, RawPayload: raw}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: event, TenantID: f.tenant, Producer: producer, Kind: "bootstrap", SourceSequence: start + 2, OccurredAt: now, RawPayload: raw}); err != nil {
			t.Fatal(err)
		}
		f.authority = &wr23BootstrapAuthority{producer: manifest.Workloads[0].PrincipalID, executor: second.PrincipalID, producerVersion: 1, executorVersion: 1}
		uow, _ := data.NewCoreBootstrapWorkUnitOfWork(d, producer)
		f.worker, err = biz.NewCoreBootstrapWorker(uow, f.authority, catalog, data.NewUUIDv7Generator(), data.NewSecretGenerator(), data.NewSystemClock())
		if err != nil {
			t.Fatal(err)
		}
		f.queue, err = data.NewCoreBootstrapQueue(d, producer)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	dispatcher := func(t *testing.T, f fixture, executor biz.CoreBootstrapExecutor) *biz.CoreBootstrapDispatcher {
		t.Helper()
		v, err := biz.NewCoreBootstrapDispatcher(f.queue, executor)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	claim := func(t *testing.T, f fixture) biz.CoreBootstrapJobClaim {
		t.Helper()
		c, found, err := f.queue.Claim(ctx)
		if err != nil || !found {
			t.Fatal("expected durable work", err)
		}
		return c
	}
	none := func(t *testing.T, f fixture) {
		t.Helper()
		_, found, err := f.queue.Claim(ctx)
		if err != nil || found {
			t.Fatal("unexpected active retry", err)
		}
	}
	status := func(t *testing.T, f fixture) string {
		t.Helper()
		var s string
		if err := e.runtimePool.QueryRow(ctx, "SELECT status FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2", f.tenant, f.operation).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	count := func(t *testing.T, f fixture, table string) int {
		t.Helper()
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", f.tenant).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	expireOwn := func(t *testing.T, f fixture) {
		t.Helper()
		if _, err := admin.Exec(ctx, "UPDATE tenant_invitations SET expires_at=created_at+interval '1 microsecond',version=version+1 WHERE tenant_id=$1 AND bootstrap_operation_id=$2", f.tenant, f.operation); err != nil {
			t.Fatal(err)
		}
	}
	runInit := func(t *testing.T, f fixture) {
		t.Helper()
		worked, err := dispatcher(t, f, f.worker).DispatchNext(ctx)
		if err != nil || !worked || status(t, f) != "waiting_for_principal_verification" {
			t.Fatal("initialization dispatch failed", err)
		}
	}
	t.Run("runtime_schema_run_loop_and_stable_waiting", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
			t.Fatal(err)
		}
		f := newFixture(t, "", 0)
		loop := dispatcher(t, f, f.worker)
		run, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		reported := make(chan error, 8)
		go func() { defer close(done); loop.Run(run, func(err error) { reported <- err }) }()
		deadline := time.Now().Add(5 * time.Second)
		for status(t, f) != "waiting_for_principal_verification" && time.Now().Before(deadline) {
			timer := time.NewTimer(10 * time.Millisecond)
			<-timer.C
		}
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("dispatcher failed to stop")
		}
		select {
		case err := <-reported:
			t.Fatal(err)
		default:
		}
		if status(t, f) != "waiting_for_principal_verification" || count(t, f, "tenant_invitations") != 1 {
			t.Fatal("loop did not reconcile")
		}
		none(t, f)
		if count(t, f, "core_bootstrap_attempts") != 1 {
			t.Fatal("waiting invitation actively retried")
		}
	})
	t.Run("retry_delay_allows_unrelated_tenant_to_progress", func(t *testing.T) {
		a := newFixture(t, "", 0)
		b := newFixture(t, a.producer, 2)
		failing := &wr23ScheduledExecutor{worker: a.worker, failure: biz.ErrPersistenceUnavailable}
		loop := dispatcher(t, a, failing)
		start := time.Now()
		worked, err := loop.DispatchNext(ctx)
		if !worked || !errors.Is(err, biz.ErrCoreBootstrapRetryable) {
			t.Fatal("failure did not schedule retry", err)
		}
		runInit(t, b)
		none(t, a)
		var at time.Time
		if err = e.runtimePool.QueryRow(ctx, "SELECT available_at FROM core_bootstrap_jobs WHERE tenant_id=$1 AND operation_id=$2 AND kind='initialize'", a.tenant, a.operation).Scan(&at); err != nil {
			t.Fatal(err)
		}
		timer := time.NewTimer(time.Until(at.Add(15 * time.Millisecond)))
		<-timer.C
		if time.Since(start) < time.Second {
			t.Fatal("first backoff was not real time")
		}
		failing.failure = nil
		if worked, err = loop.DispatchNext(ctx); !worked || err != nil {
			t.Fatal("retry failed", err)
		}
		if count(t, a, "tenant_invitations") != 1 || count(t, a, "core_bootstrap_attempts") != 2 {
			t.Fatal("retry duplicated effect or lost history")
		}
	})
	t.Run("real_expired_leases_recover_commit_and_fence_new_attempt", func(t *testing.T) {
		completed := newFixture(t, "", 0)
		pending := newFixture(t, "", 0)
		a, b := claim(t, completed), claim(t, pending)
		if _, err := completed.worker.Reconcile(ctx, completed.scope, completed.operation); err != nil {
			t.Fatal(err)
		}
		none(t, pending)
		forged := b
		forged.LeaseID = mustV7(t)
		if _, err := pending.queue.Finish(ctx, forged, "dependency_unavailable", false); !errors.Is(err, biz.ErrCoreBootstrapLease) {
			t.Fatal("foreign lease finished")
		}
		deadline := a.LeaseUntil
		if b.LeaseUntil.After(deadline) {
			deadline = b.LeaseUntil
		}
		start := time.Now()
		timer := time.NewTimer(time.Until(deadline.Add(50 * time.Millisecond)))
		<-timer.C
		if time.Since(start) < 29*time.Second {
			t.Fatal("lease was not allowed to expire in real time")
		}
		completed.queue, _ = data.NewCoreBootstrapQueue(d, completed.producer)
		none(t, completed)
		none(t, pending)
		if count(t, completed, "tenant_invitations") != 1 || count(t, completed, "core_bootstrap_attempts") != 1 {
			t.Fatal("commit-before-finish recovery duplicated effect")
		}
		var outcome string
		if err := e.runtimePool.QueryRow(ctx, "SELECT outcome FROM core_bootstrap_attempts WHERE tenant_id=$1", completed.tenant).Scan(&outcome); err != nil || outcome != "recovered_completion" {
			t.Fatal("committed result was not recovered")
		}
		if _, err := completed.queue.Finish(ctx, a, "", false); !errors.Is(err, biz.ErrCoreBootstrapLease) {
			t.Fatal("old completed lease remained usable")
		}
		timer = time.NewTimer(1100 * time.Millisecond)
		<-timer.C
		c := claim(t, pending)
		if c.Attempt != 2 || c.LeaseID == b.LeaseID {
			t.Fatal("reclaim did not fence old lease")
		}
		if _, err := pending.queue.Finish(ctx, b, "dependency_unavailable", false); !errors.Is(err, biz.ErrCoreBootstrapLease) {
			t.Fatal("old attempt finished current lease")
		}
		now := time.Now()
		if _, err := pending.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), Producer: pending.producer, Kind: "heartbeat", SourceSequence: 3, OccurredAt: now, RawPayload: []byte(`{"fixture":"lease-heartbeat"}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := pending.worker.Reconcile(ctx, pending.scope, pending.operation); err != nil {
			t.Fatal(err)
		}
		if _, err := pending.queue.Finish(ctx, c, "", false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("twenty_attempt_boundary_retains_intent_and_attention", func(t *testing.T) {
		f := newFixture(t, "", 0)
		loop := dispatcher(t, f, &wr23ScheduledExecutor{failure: errors.New("private transport diagnostic must not be persisted")})
		for attempt := 1; attempt <= 20; attempt++ {
			if attempt > 1 {
				if _, err := admin.Exec(ctx, "UPDATE core_bootstrap_jobs SET available_at=clock_timestamp() WHERE tenant_id=$1 AND operation_id=$2 AND kind='initialize' AND state='pending'", f.tenant, f.operation); err != nil {
					t.Fatal(err)
				}
			}
			worked, err := loop.DispatchNext(ctx)
			expected := biz.ErrCoreBootstrapRetryable
			if attempt == 20 {
				expected = biz.ErrCoreBootstrapAttention
			}
			if !worked || !errors.Is(err, expected) {
				t.Fatal("attempt boundary", attempt, err)
			}
		}
		none(t, f)
		if count(t, f, "core_bootstrap_attempts") != 20 || status(t, f) != "pending" || count(t, f, "tenant_access") != 0 {
			t.Fatal("exhaustion erased or executed original intent")
		}
		var state, code string
		var attempts int
		if err := e.runtimePool.QueryRow(ctx, "SELECT state,last_error,attempt_count FROM core_bootstrap_jobs WHERE tenant_id=$1", f.tenant).Scan(&state, &code, &attempts); err != nil || state != "attention_required" || code != "execution_failed" || attempts != 20 {
			t.Fatal("unsafe or missing attention metadata")
		}
	})
	t.Run("permanent_conflict_stops_without_repeated_effects", func(t *testing.T) {
		f := newFixture(t, "", 0)
		worked, err := dispatcher(t, f, &wr23ScheduledExecutor{failure: biz.ErrCoreBootstrapConflict}).DispatchNext(ctx)
		if !worked || !errors.Is(err, biz.ErrCoreBootstrapAttention) {
			t.Fatal("conflict did not require attention")
		}
		none(t, f)
		if count(t, f, "core_bootstrap_attempts") != 1 {
			t.Fatal("permanent conflict retried")
		}
	})
	t.Run("expiry_atomically_ends_invitation_and_audits_original_operation", func(t *testing.T) {
		f := newFixture(t, "", 0)
		runInit(t, f)
		expireOwn(t, f)
		worked, err := dispatcher(t, f, f.worker).DispatchNext(ctx)
		if !worked || !errors.Is(err, biz.ErrCoreBootstrapAttention) {
			t.Fatal("expiry did not require operator attention", err)
		}
		none(t, f)
		if status(t, f) != "attention_required" || count(t, f, "tenant_memberships") != 0 {
			t.Fatal("expired invitation activated identity")
		}
		var expired, cancelled bool
		var audits int
		if err = e.runtimePool.QueryRow(ctx, "SELECT status='expired' FROM tenant_invitations WHERE tenant_id=$1", f.tenant).Scan(&expired); err != nil || !expired {
			t.Fatal("invitation did not expire")
		}
		if err = e.runtimePool.QueryRow(ctx, "SELECT status='cancelled' AND payload_ciphertext IS NULL AND payload_key_version IS NULL FROM tenant_invitation_outbox WHERE tenant_id=$1", f.tenant).Scan(&cancelled); err != nil || !cancelled {
			t.Fatal("obsolete delivery remained sendable")
		}
		if err = e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.bootstrap.invitation.expired' AND target_version=3", f.tenant, f.operation).Scan(&audits); err != nil || audits != 1 {
			t.Fatal("expiry audit not atomically linked")
		}
		before, err := f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1)
		if err != nil {
			t.Fatal(err)
		}
		after, err := f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1)
		if err != nil || before != after {
			t.Fatal("expiry retry changed result")
		}
		if count(t, f, "iam_audit_events") != 2 {
			t.Fatal("expiry retry duplicated audit")
		}
	})
	t.Run("expiry_audit_failure_rolls_back_all_business_effects", func(t *testing.T) {
		f := newFixture(t, "", 0)
		runInit(t, f)
		expireOwn(t, f)
		if _, err := admin.Exec(ctx, "REVOKE INSERT ON iam_audit_events FROM ani_iam_runtime"); err != nil {
			t.Fatal(err)
		}
		_, err := f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1)
		_, restore := admin.Exec(ctx, "GRANT INSERT ON iam_audit_events TO ani_iam_runtime")
		if restore != nil {
			t.Fatal(restore)
		}
		if err == nil {
			t.Fatal("audit fault not propagated")
		}
		var pending bool
		if err = e.runtimePool.QueryRow(ctx, "SELECT status='pending' FROM tenant_invitations WHERE tenant_id=$1", f.tenant).Scan(&pending); err != nil || !pending || status(t, f) != "waiting_for_principal_verification" {
			t.Fatal("audit failure left partial expiry")
		}
		if count(t, f, "tenant_invitation_roles") != 1 {
			t.Fatal("rollback lost active invitation role reference")
		}
		if _, err = f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("expiry_requires_current_authority_and_preserves_frozen_tenant", func(t *testing.T) {
		f := newFixture(t, "", 0)
		runInit(t, f)
		expireOwn(t, f)
		f.authority.deny = true
		if _, err := f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
			t.Fatal("expiry ignored authority")
		}
		if status(t, f) != "waiting_for_principal_verification" {
			t.Fatal("denied expiry changed intent")
		}
		f.authority.deny = false
		now := time.Now()
		if _, err := f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: f.producer, Kind: "lifecycle", SourceSequence: 3, OccurredAt: now, RawPayload: []byte(`{"fixture":"frozen"}`), LifecycleVersion: 2, Status: "frozen", Reason: "administrative_freeze", EffectiveAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.worker.ExpireInvitation(ctx, f.scope, f.operation, 1); err != nil {
			t.Fatal(err)
		}
		v, err := f.projection.ReadShadowTenant(ctx, f.scope)
		if err != nil || v.Status != "frozen" || status(t, f) != "attention_required" {
			t.Fatal("expiry changed authoritative lifecycle")
		}
	})
	t.Run("attempt_append_failure_preserves_claim_and_cross_tenant_fencing", func(t *testing.T) {
		f := newFixture(t, "", 0)
		other := newFixture(t, "", 0)
		c := claim(t, f)
		bad := c
		bad.TenantID = other.tenant
		if _, err := f.queue.Finish(ctx, bad, "dependency_unavailable", false); err == nil {
			t.Fatal("attempt crossed Tenant")
		}
		if _, err := admin.Exec(ctx, "REVOKE INSERT ON core_bootstrap_attempts FROM ani_iam_runtime"); err != nil {
			t.Fatal(err)
		}
		_, err := f.queue.Finish(ctx, c, "dependency_unavailable", false)
		_, restore := admin.Exec(ctx, "GRANT INSERT ON core_bootstrap_attempts TO ani_iam_runtime")
		if restore != nil {
			t.Fatal(restore)
		}
		if err == nil {
			t.Fatal("attempt history fault ignored")
		}
		var state string
		if err = e.runtimePool.QueryRow(ctx, "SELECT state FROM core_bootstrap_jobs WHERE tenant_id=$1", f.tenant).Scan(&state); err != nil || state != "claimed" || count(t, f, "core_bootstrap_attempts") != 0 {
			t.Fatal("failed attempt append advanced schedule")
		}
		if _, err = f.queue.Finish(ctx, c, "dependency_unavailable", false); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, "UPDATE core_bootstrap_attempts SET error_code='' WHERE tenant_id=$1", f.tenant); err == nil {
			t.Fatal("attempt was mutable")
		}
		if _, err = admin.Exec(ctx, "UPDATE core_bootstrap_jobs SET producer='different' WHERE tenant_id=$1", f.tenant); err == nil {
			t.Fatal("job source was mutable")
		}
	})
}
