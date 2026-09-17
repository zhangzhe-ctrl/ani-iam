//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Only the authority policy port is a test double. Principals are provisioned
// through the real provisioner transaction, and all reconciliation/outbox/audit
// operations use the actual restricted PostgreSQL repositories. No broker,
// authenticated receiver, SMTP or formal worker-loop claim is made here.
type wr23BootstrapAuthority struct {
	producer, executor               uuid.UUID
	producerVersion, executorVersion int64
	mutate                           func(*biz.CoreBootstrapExecutionAuthorization)
	deny                             bool
}

func (a *wr23BootstrapAuthority) AuthorizeCoreBootstrap(_ context.Context, s biz.CoreBootstrapSource) (biz.CoreBootstrapExecutionAuthorization, error) {
	if a.deny {
		return biz.CoreBootstrapExecutionAuthorization{}, biz.ErrCoreBootstrapAuthority
	}
	v := biz.CoreBootstrapExecutionAuthorization{Source: s, ProducerPrincipalID: a.producer, ExecutorPrincipalID: a.executor, ProducerVersion: a.producerVersion, ExecutorVersion: a.executorVersion, DecisionID: uuid.Must(uuid.NewV7()), ValidUntil: time.Now().Add(time.Minute)}
	if a.mutate != nil {
		a.mutate(&v)
	}
	return v, nil
}

func TestWR23CoreBootstrapWorker(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	ownerPool := mustPool(t, e.migrationDSN(primaryDB))
	defer ownerPool.Close()
	provisioner := mustPool(t, postgresDSN(provisionerRole, e.provisionerPass, e.host, primaryDB, "wr23-worker-component-provisioner"))
	defer provisioner.Close()
	owner, manifest := newBootstrapManifest(t, strings.Repeat("a", 64))
	second := manifest.Workloads[0]
	second.PrincipalID = mustV7(t)
	second.BindingID = mustV7(t)
	second.Name = "bootstrap-worker"
	second.DNSIdentity = "bootstrap-worker.wr19.test"
	second.Grants = append([]biz.BootstrapWorkloadGrant(nil), second.Grants...)
	for i := range second.Grants {
		second.Grants[i].ID = mustV7(t)
	}
	manifest.Workloads = append(manifest.Workloads, second)
	if _, err := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(provisioner), wr32Registry(t)), data.NewSystemClock(), wr32Registry(t)).Provision(ctx, owner, manifest); err != nil {
		t.Fatal(err)
	}
	producerID, executorID := manifest.Workloads[0].PrincipalID, second.PrincipalID
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("generate own outbox key")
	}
	protector, err := data.NewOutboxProtector("wr23-worker", map[string][]byte{"wr23-worker": key})
	if err != nil {
		t.Fatal(err)
	}
	d := data.NewData(e.runtimePool, protector)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		scope             biz.TenantScope
		tenant, operation uuid.UUID
		producer          string
		projection        biz.CoreLifecycleProjectionRepository
	}
	newFixture := func(t *testing.T, marker bool) fixture {
		t.Helper()
		f := fixture{tenant: mustV7(t), operation: mustV7(t), producer: "core.worker." + mustV7(t).String()}
		f.scope = mustTenantScope(t, f.tenant)
		f.projection, err = data.NewCoreLifecycleProjectionRepository(d, f.producer)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.projection.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		_, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: f.producer, Kind: "lifecycle", SourceSequence: 1, OccurredAt: now, RawPayload: []byte(`{"fixture":"lifecycle"}`), LifecycleVersion: 1, Status: "active", Reason: "created", EffectiveAt: now})
		if err != nil {
			t.Fatal(err)
		}
		i := biz.CoreBootstrapIntent{TenantID: f.tenant, OperationID: f.operation, NormalizedEmail: "invited@example.test", Locale: "en-US"}
		payload, _ := json.Marshal(map[string]string{"locale": i.Locale, "normalized_email": i.NormalizedEmail, "operation_id": i.OperationID.String(), "tenant_id": i.TenantID.String()})
		sum := sha256.Sum256(payload)
		i.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		event := mustV7(t)
		_, err = data.NewCoreBootstrapReceiptRepository(d).Receive(ctx, biz.CoreBootstrapDelivery{Intent: i, EventID: event, Producer: f.producer, SourceSequence: 2, OccurredAt: now, RawPayload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if marker {
			if _, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: event, TenantID: f.tenant, Producer: f.producer, Kind: "bootstrap", SourceSequence: 2, OccurredAt: now, RawPayload: payload}); err != nil {
				t.Fatal(err)
			}
		}
		return f
	}
	newAuthority := func(t *testing.T) *wr23BootstrapAuthority {
		t.Helper()
		a := &wr23BootstrapAuthority{producer: producerID, executor: executorID}
		if err := e.runtimePool.QueryRow(ctx, "SELECT (SELECT version FROM principals WHERE id=$1),(SELECT version FROM principals WHERE id=$2)", producerID, executorID).Scan(&a.producerVersion, &a.executorVersion); err != nil {
			t.Fatal("read component Workload versions")
		}
		return a
	}
	worker := func(t *testing.T, f fixture, store *data.Data, a biz.CoreBootstrapAuthorizer) *biz.CoreBootstrapWorker {
		t.Helper()
		uow, err := data.NewCoreBootstrapWorkUnitOfWork(store, f.producer)
		if err != nil {
			t.Fatal(err)
		}
		w, err := biz.NewCoreBootstrapWorker(uow, a, catalog, data.NewUUIDv7Generator(), data.NewSecretGenerator(), data.NewSystemClock())
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	count := func(t *testing.T, table string, f fixture) int {
		t.Helper()
		var n int
		if err := e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", f.tenant).Scan(&n); err != nil {
			t.Fatal("read exact Tenant worker effects")
		}
		return n
	}
	assertPending := func(t *testing.T, f fixture) {
		t.Helper()
		for _, table := range []string{"tenant_access", "tenant_roles", "tenant_invitations", "tenant_invitation_outbox", "tenant_memberships", "tenant_role_bindings", "core_bootstrap_worker_results", "iam_audit_events"} {
			if count(t, table, f) != 0 {
				t.Fatal("failed worker left partial effects: " + table)
			}
		}
		var status string
		if err := e.runtimePool.QueryRow(ctx, "SELECT status FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2", f.tenant, f.operation).Scan(&status); err != nil || status != "pending" {
			t.Fatal("failed worker consumed original intent")
		}
	}
	t.Run("atomic_invitation_role_access_and_audit_with_existing_outbox", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
			t.Fatal(err)
		}
		f := newFixture(t, true)
		w := worker(t, f, d, newAuthority(t))
		r, err := w.Reconcile(ctx, f.scope, f.operation)
		if err != nil || r.Status != "waiting_for_principal_verification" {
			t.Fatalf("Bootstrap create: %v", err)
		}
		for _, table := range []string{"tenant_access", "tenant_roles", "tenant_invitations", "tenant_invitation_outbox", "core_bootstrap_worker_results", "iam_audit_events"} {
			if count(t, table, f) != 1 {
				t.Fatal("missing atomic effect: " + table)
			}
		}
		for _, table := range []string{"tenant_memberships", "tenant_role_bindings"} {
			if count(t, table, f) != 0 {
				t.Fatal("Invitation created premature authority")
			}
		}
		var activeAccess, humanCount, permissions int
		if err := e.runtimePool.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1 AND status='active'),(SELECT count(*) FROM principals WHERE principal_type='human'),(SELECT count(*) FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2)", f.tenant, r.RoleID).Scan(&activeAccess, &humanCount, &permissions); err != nil || activeAccess != 0 || humanCount != 0 || permissions != len(catalog.Permissions(biz.PermissionScopeTenant)) {
			t.Fatal("bootstrap activation or builtin definition incorrect")
		}
		var auditCorrect bool
		if err := e.runtimePool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM iam_audit_events a JOIN core_bootstrap_worker_results r ON r.tenant_id=a.tenant_id AND r.audit_id=a.event_id WHERE r.tenant_id=$1 AND r.operation_id=$2 AND r.producer_principal_id=$3 AND r.executor_principal_id=$4 AND a.actor_id=$4 AND a.caller_principal_id IS NULL AND a.authentication_method='internal' AND a.action='iam.bootstrap.invitation.created')", f.tenant, f.operation, producerID, executorID).Scan(&auditCorrect); err != nil || !auditCorrect {
			t.Fatal("producer and executor collapsed")
		}
		outbox, err := data.NewIdentityNotificationOutbox(d, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		claim, ok, err := outbox.ClaimIdentityNotification(ctx, biz.IdentityNotificationTenantInvitation, time.Now().UTC(), 5*time.Minute)
		if err != nil || !ok || !claim.PayloadValid || claim.TenantID != f.tenant || claim.SourceID != r.InvitationID || claim.Email != "invited@example.test" || !strings.HasPrefix(claim.Secret, "ani_inv_t."+f.tenant.String()+"."+r.InvitationID.String()+".") {
			t.Fatal("existing outbox cannot consume Bootstrap invitation")
		}
		encoded, _ := json.Marshal(r)
		if strings.Contains(string(encoded), claim.Secret) {
			t.Fatal("worker result exposed Invitation credential")
		}
		// No SMTP claim: cancel this component-only delivery after verifying AEAD.
		if err = outbox.FinishIdentityNotification(ctx, claim, biz.IdentityNotificationCancelled, "", time.Now().UTC(), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		again, err := worker(t, f, d, newAuthority(t)).Reconcile(ctx, f.scope, f.operation)
		if err != nil || again != r {
			t.Fatal("restart changed committed worker result")
		}
	})
	t.Run("concurrent_reconcile_creates_one_invitation", func(t *testing.T) {
		f := newFixture(t, true)
		w := worker(t, f, d, newAuthority(t))
		var wg sync.WaitGroup
		results := make([]biz.CoreBootstrapWorkerResult, 8)
		for i := range results {
			wg.Go(func() {
				var err error
				results[i], err = w.Reconcile(ctx, f.scope, f.operation)
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, r := range results {
			if r != results[0] {
				t.Fatal("concurrent result drift")
			}
		}
		if count(t, "tenant_invitations", f) != 1 || count(t, "iam_audit_events", f) != 1 {
			t.Fatal("duplicate Bootstrap effects")
		}
	})
	t.Run("current_authority_required_and_bound_to_source", func(t *testing.T) {
		for _, mode := range []string{"denied", "expired", "wrong_source", "wrong_version", "same_actor"} {
			t.Run(mode, func(t *testing.T) {
				f := newFixture(t, true)
				a := newAuthority(t)
				switch mode {
				case "denied":
					a.deny = true
				case "expired":
					a.mutate = func(v *biz.CoreBootstrapExecutionAuthorization) { v.ValidUntil = time.Now().Add(-time.Second) }
				case "wrong_source":
					a.mutate = func(v *biz.CoreBootstrapExecutionAuthorization) { v.Source.OperationID = uuid.Must(uuid.NewV7()) }
				case "wrong_version":
					a.producerVersion += 99
				case "same_actor":
					a.executor = a.producer
				}
				if _, err := worker(t, f, d, a).Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
					t.Fatalf("authority %s: %v", mode, err)
				}
				assertPending(t, f)
			})
		}
	})
	t.Run("disabled_workload_cannot_execute_or_replay", func(t *testing.T) {
		f := newFixture(t, true)
		a := newAuthority(t)
		w := worker(t, f, d, a)
		saved, err := w.Reconcile(ctx, f.scope, f.operation)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ownerPool.Exec(ctx, "UPDATE principals SET status='disabled',version=version+1 WHERE id=$1", executorID); err != nil {
			t.Fatal("disable own component Workload")
		}
		defer ownerPool.Exec(ctx, "UPDATE principals SET status='active',version=version+1 WHERE id=$1", executorID)
		if _, err = w.Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
			t.Fatal("committed result bypassed current Workload state")
		}
		if count(t, "iam_audit_events", f) != 1 || saved.InvitationID == uuid.Nil {
			t.Fatal("denied replay changed original result")
		}
	})
	t.Run("requires_receipt_source_marker_and_exact_tenant", func(t *testing.T) {
		f := newFixture(t, false)
		w := worker(t, f, d, newAuthority(t))
		if _, err := w.Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
			t.Fatal("missing source marker accepted")
		}
		assertPending(t, f)
		other := newFixture(t, true)
		if _, err := worker(t, other, d, newAuthority(t)).Reconcile(ctx, other.scope, f.operation); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
			t.Fatal("cross Tenant Bootstrap operation accepted")
		}
		assertPending(t, other)
	})
	t.Run("current_lifecycle_freeze_blocks_until_restored", func(t *testing.T) {
		f := newFixture(t, true)
		w := worker(t, f, d, newAuthority(t))
		now := time.Now().UTC().Truncate(time.Microsecond)
		m := biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: f.producer, Kind: "lifecycle", SourceSequence: 3, OccurredAt: now, RawPayload: []byte(`{"fixture":"freeze"}`), LifecycleVersion: 2, Status: "frozen", Reason: "administrative_freeze", EffectiveAt: now}
		if _, err := f.projection.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrTenantLifecycleBlocked) {
			t.Fatal("frozen Tenant initialized")
		}
		assertPending(t, f)
		m.EventID = mustV7(t)
		m.SourceSequence = 4
		m.LifecycleVersion = 3
		m.Status = "active"
		m.Reason = "restored"
		if _, err := f.projection.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Reconcile(ctx, f.scope, f.operation); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("encryption_failure_rolls_back_every_effect", func(t *testing.T) {
		f := newFixture(t, true)
		if _, err := worker(t, f, data.NewData(e.runtimePool), newAuthority(t)).Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrPersistenceUnavailable) {
			t.Fatalf("missing protector: %v", err)
		}
		assertPending(t, f)
	})
	t.Run("outbox_or_audit_failure_rolls_back_every_effect", func(t *testing.T) {
		for _, table := range []string{"tenant_invitation_outbox", "iam_audit_events"} {
			t.Run(table, func(t *testing.T) {
				f := newFixture(t, true)
				if _, err := ownerPool.Exec(ctx, "REVOKE INSERT ON "+table+" FROM ani_iam_runtime"); err != nil {
					t.Fatal("inject own write failure")
				}
				defer ownerPool.Exec(ctx, "GRANT INSERT ON "+table+" TO ani_iam_runtime")
				if _, err := worker(t, f, d, newAuthority(t)).Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrPersistencePermissionDenied) {
					t.Fatalf("atomic write failure: %v", err)
				}
				assertPending(t, f)
			})
		}
	})
	t.Run("no_implicit_access_adoption", func(t *testing.T) {
		f := newFixture(t, true)
		if _, err := ownerPool.Exec(ctx, "INSERT INTO tenant_access(tenant_id,status,version,created_at,updated_at) VALUES($1,'bootstrap_pending',1,now(),now())", f.tenant); err != nil {
			t.Fatal("inject conflicting own Access")
		}
		if _, err := worker(t, f, d, newAuthority(t)).Reconcile(ctx, f.scope, f.operation); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
			t.Fatal("unrelated Access adopted")
		}
		if count(t, "tenant_invitations", f) != 0 || count(t, "core_bootstrap_worker_results", f) != 0 {
			t.Fatal("conflicting Access received new Bootstrap effects")
		}
	})
	t.Run("worker_requires_authority_adapter", func(t *testing.T) {
		f := newFixture(t, true)
		uow, err := data.NewCoreBootstrapWorkUnitOfWork(d, f.producer)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = biz.NewCoreBootstrapWorker(uow, nil, catalog, data.NewUUIDv7Generator(), data.NewSecretGenerator(), data.NewSystemClock()); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
			t.Fatal("worker accepted missing current authority verifier")
		}
	})
}
