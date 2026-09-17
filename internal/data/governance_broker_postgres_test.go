package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nkeys"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Real storage/current-authority/worker component. The broker coordinates here
// are a transport fixture; real NATS attribution and formal APIs remain I4.
func runWR33BrokerPostgresComponent(t *testing.T, ctx context.Context, migrator, runtime *pgxpool.Pool) {
	t.Helper()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	cfg := runtime.Config().Copy()
	cfg.ConnConfig.User = "ani_iam_provisioner"
	provisioner, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect provisioner")
	}
	defer provisioner.Close()
	producer, executor := id(), id()
	config := CoreBrokerConfiguration{Environment: "wr33-component", TrustDomain: "wr33-component.test", BrokerName: "wr33-component", Account: "WR33", Stream: "WR33_TENANT", Consumer: "IAM", ConsumerID: id(), ConsumerBindingID: id(), Producer: governancev1.Producer}
	publicKey := func() string {
		key, e := nkeys.CreateUser()
		if e != nil {
			t.Fatal(e)
		}
		defer key.Wipe()
		v, e := key.PublicKey()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	producerKey := publicKey()
	config.ConsumerNKey = publicKey()
	producerBinding := id()
	for _, subject := range []string{governancev1.LifecycleSubject, tenantbootstrap.Subject, governancev1.HeartbeatSubject} {
		route := CoreBrokerRoute{ID: id(), Subject: subject, ProducerBindingID: producerBinding, ProducerNKey: producerKey}
		route.TargetSHA256 = config.RouteRevision(route)
		config.Routes = append(config.Routes, route)
	}
	tx, e := provisioner.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	seed := sqlcgen.New(tx)
	for i, principal := range []uuid.UUID{producer, executor} {
		if e = seed.InsertBootstrapPrincipal(ctx, sqlcgen.InsertBootstrapPrincipalParams{PrincipalID: principal, Now: requiredTimestamptz(time.Now())}); e != nil {
			t.Fatal(e)
		}
		name := []string{"governance-component", "iam-component"}[i]
		if e = seed.InsertBootstrapProfile(ctx, sqlcgen.InsertBootstrapProfileParams{PrincipalID: principal, Name: name, Environment: pgtype.Text{String: config.Environment, Valid: true}, TrustDomain: pgtype.Text{String: config.TrustDomain, Valid: true}, Now: requiredTimestamptz(time.Now())}); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	apply := func(mode string, bindings []CoreBrokerBindingChange, grants []CoreBrokerGrantChange) {
		t.Helper()
		m := CoreBrokerProvisionManifest{Version: 1, ID: id(), Mode: mode, Reason: "WR33 restricted component fixture", ExpiresAt: time.Now().Add(time.Hour), Configuration: config, Bindings: bindings, Grants: grants}
		raw, _ := json.Marshal(m)
		sum := sha256.Sum256(raw)
		if _, e := ApplyCoreBrokerManifest(ctx, NewData(provisioner), raw, hex.EncodeToString(sum[:]), config.Environment, config.TrustDomain, time.Now()); e != nil {
			t.Fatal("apply broker manifest", mode, e)
		}
	}
	apply("register", []CoreBrokerBindingChange{{ID: producerBinding, PrincipalID: producer, NKeyPublic: producerKey, Status: "active"}, {ID: config.ConsumerBindingID, PrincipalID: executor, NKeyPublic: config.ConsumerNKey, Status: "active"}}, nil)
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		t.Fatal(e)
	}
	protector, e := NewOutboxProtector("component", map[string][]byte{"component": key})
	if e != nil {
		t.Fatal(e)
	}
	clear(key)
	d := NewData(runtime, protector)
	repo, e := newCoreBrokerRepository(d, config)
	if e != nil {
		t.Fatal(e)
	}
	epoch, generation, tenant, operation := id(), id(), id(), id()
	q := sqlcgen.New(runtime)
	if e = q.CreateTenantLifecycleGeneration(ctx, sqlcgen.CreateTenantLifecycleGenerationParams{Producer: config.Producer, ID: generation, Epoch: epoch, SnapshotID: id(), CapturedAt: tenantFactTime(time.Now())}); e != nil {
		t.Fatal(e)
	}
	if _, e = runtime.Exec(ctx, `UPDATE tenant_lifecycle_pipelines SET generation_id=$2,epoch=$3,applied_sequence=0,highest_sequence=0,snapshot_required=false,heartbeat_id=NULL,heartbeat_epoch=NULL,heartbeat_observed_at=NULL,heartbeat_received_at=NULL,committed_sequence=0,published_sequence=0 WHERE producer=$1`, config.Producer, generation, epoch); e != nil {
		t.Fatal(e)
	}
	bootstrap := func(tenant, op uuid.UUID, reference int64) biz.TenantBrokerDelivery {
		t.Helper()
		dto := &iamv1.TenantBootstrapRequested{SchemaRevision: tenantbootstrap.Revision, EventId: id().String(), Producer: config.Producer, SourceEpoch: epoch.String(), LifecycleSequence: reference, TenantId: tenant.String(), OperationId: op.String(), OccurredAt: timestamppb.New(time.Now().UTC().Add(-time.Second)), IntendedAdministrator: &iamv1.TenantBootstrapAdministrator{NormalizedEmail: "admin@example.com", Locale: "en-US"}}
		dto.PayloadFingerprint, _ = tenantbootstrap.Fingerprint(dto.TenantId, dto.OperationId, dto.IntendedAdministrator.NormalizedEmail, dto.IntendedAdministrator.Locale)
		raw, e := tenantbootstrap.Marshal(dto)
		if e != nil {
			t.Fatal(e)
		}
		b := biz.CoreBootstrapDelivery{Intent: biz.CoreBootstrapIntent{TenantID: tenant, OperationID: op, NormalizedEmail: "admin@example.com", Locale: "en-US", Fingerprint: dto.PayloadFingerprint}, EventID: uuid.MustParse(dto.EventId), Producer: config.Producer, SourceSequence: reference, OccurredAt: dto.OccurredAt.AsTime(), RawPayload: raw}
		return biz.TenantBrokerDelivery{EventID: b.EventID, Epoch: epoch, TenantID: tenant, Producer: config.Producer, Kind: "bootstrap", Sequence: reference, OccurredAt: b.OccurredAt, RawPayload: raw, Bootstrap: &b}
	}
	message := func(b biz.TenantBrokerDelivery, sequence int64) biz.CoreBrokerMessage {
		subject := map[string]string{"bootstrap": tenantbootstrap.Subject, "lifecycle": governancev1.LifecycleSubject, "heartbeat": governancev1.HeartbeatSubject}[b.Kind]
		return biz.CoreBrokerMessage{DeliveryID: id(), ConsumerID: config.ConsumerID, BrokerName: config.BrokerName, Account: config.Account, Stream: config.Stream, Consumer: config.Consumer, Subject: subject, BrokerSequence: sequence, DeliveryCount: 1, PublishedAt: time.Now(), Payload: b.RawPayload}
	}
	intent := bootstrap(tenant, operation, 1)
	incoming := message(intent, 1)
	if e = repo.Receive(ctx, incoming, intent); !errors.Is(e, biz.ErrCoreBrokerAuthority) {
		t.Fatal("registration granted authority", e)
	}
	var count int
	if e = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1", tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("unauthorized Bootstrap left operation", e)
	}
	grants := []CoreBrokerGrantChange{}
	var executeGrant, heartbeatGrant CoreBrokerGrantChange
	for _, route := range config.Routes {
		for _, action := range []string{"publish", "receive", "execute"} {
			if action == "execute" && route.Subject != tenantbootstrap.Subject {
				continue
			}
			g := CoreBrokerGrantChange{ID: id(), PrincipalID: executor, BindingID: config.ConsumerBindingID, RouteID: route.ID, Action: action, Status: "active"}
			if action == "publish" {
				g.PrincipalID = producer
				g.BindingID = producerBinding
			}
			grants = append(grants, g)
			if action == "execute" {
				executeGrant = g
			}
			if action == "publish" && route.Subject == governancev1.HeartbeatSubject {
				heartbeatGrant = g
			}
		}
	}
	apply("grants", nil, grants)
	if e = repo.Receive(ctx, incoming, intent); e != nil {
		t.Fatal("first authorized Bootstrap receipt", e)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- repo.Receive(ctx, incoming, intent) }()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal("concurrent broker receipt", e)
		}
	}
	if e = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_broker_authority_receipts WHERE consumer_id=$1", config.ConsumerID).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate broker authority", e)
	}
	registry, e := workloadregistry.Load("../../registrations/governance-workload-targets.v1.json", "b64bc41df5f899e17fa2a1a259eca833a41cafaff6082ecd7904e9ef21f7824d")
	if e != nil {
		t.Fatal(e)
	}
	policy, e := WorkloadPolicyRevision(registry)
	if e != nil {
		t.Fatal(e)
	}
	catalog, e := NewTargetPermissionCatalog(policy, registry)
	if e != nil {
		t.Fatal(e)
	}
	uow, e := NewCoreBrokerBootstrapWorkUnitOfWork(d, config)
	if e != nil {
		t.Fatal(e)
	}
	worker, e := biz.NewCoreBootstrapWorker(uow, repo, catalog, NewUUIDv7Generator(), NewSecretGenerator(), NewSystemClock())
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := biz.NewTenantScope(tenant)
	if _, e = worker.Reconcile(ctx, scope, operation); !errors.Is(e, biz.ErrTenantLifecycleStale) {
		t.Fatal("Bootstrap before lifecycle was not deferred", e)
	}
	if e = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1", tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("early Bootstrap created invitation", e)
	}
	event := biz.TenantLifecycleEvent{EventID: id(), Epoch: epoch, TenantID: tenant, Sequence: 1, TenantVersion: 1, Status: "active", Reason: "created", OccurredAt: time.Now().UTC().Add(-time.Second)}
	event.EffectiveAt = event.OccurredAt
	raw, _ := json.Marshal(governancev1.LifecycleEvent{Revision: governancev1.Revision, Producer: governancev1.Producer, EventId: event.EventID.String(), Epoch: epoch.String(), Sequence: 1, TenantId: tenant.String(), TenantVersion: 1, BusinessStatus: governancev1.Active, Reason: event.Reason, OccurredAt: event.OccurredAt, EffectiveAt: event.EffectiveAt})
	life := biz.TenantBrokerDelivery{EventID: event.EventID, Epoch: epoch, TenantID: tenant, Producer: config.Producer, Kind: "lifecycle", Sequence: 1, OccurredAt: event.OccurredAt, RawPayload: raw, Lifecycle: &event}
	if e = repo.Receive(ctx, message(life, 2), life); e != nil {
		t.Fatal(e)
	}
	foreignIntent := bootstrap(id(), id(), 1)
	if e = repo.Receive(ctx, message(foreignIntent, 10), foreignIntent); !errors.Is(e, biz.ErrCoreBootstrapConflict) {
		t.Fatal("Bootstrap referenced another Tenant creation", e)
	}
	if e = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_bootstrap_operations WHERE tenant_id=$1", foreignIntent.TenantID).Scan(&count); e != nil || count != 0 {
		t.Fatal("cross-Tenant reference left intent", e)
	}
	heartbeat := func() biz.TenantBrokerDelivery {
		h := biz.TenantLifecycleHeartbeat{ID: id(), Epoch: epoch, Committed: 1, Published: 1, ObservedAt: time.Now().UTC().Add(-time.Millisecond)}
		raw, _ := json.Marshal(governancev1.Heartbeat{Revision: governancev1.Revision, Producer: governancev1.Producer, HeartbeatId: h.ID.String(), Epoch: epoch.String(), CommittedSequence: 1, PublishedSequence: 1, ObservedAt: h.ObservedAt})
		return biz.TenantBrokerDelivery{EventID: h.ID, Epoch: epoch, Producer: config.Producer, Kind: "heartbeat", OccurredAt: h.ObservedAt, RawPayload: raw, Heartbeat: &h}
	}
	hb := heartbeat()
	if e = repo.Receive(ctx, message(hb, 3), hb); e != nil {
		t.Fatal(e)
	}
	// A real database audit error must roll back invitation, role, Access and result.
	if _, e = migrator.Exec(ctx, `CREATE FUNCTION wr33_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'component injected audit failure' USING ERRCODE='23514'; END $$; CREATE TRIGGER wr33_reject_audit BEFORE INSERT ON iam_audit_events FOR EACH ROW WHEN (NEW.action='iam.bootstrap.invitation.created') EXECUTE FUNCTION wr33_reject_audit();`); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.Reconcile(ctx, scope, operation); e == nil {
		t.Fatal("audit failure was ignored")
	}
	if e = runtime.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_access WHERE tenant_id=$1)+(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1)+(SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1)`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("audit failure left identity effects", e)
	}
	if _, e = migrator.Exec(ctx, `DROP TRIGGER wr33_reject_audit ON iam_audit_events; DROP FUNCTION wr33_reject_audit();`); e != nil {
		t.Fatal(e)
	}
	result, e := worker.Reconcile(ctx, scope, operation)
	if e != nil || result.Status != "waiting_for_principal_verification" {
		t.Fatal("Bootstrap invitation", e)
	}
	repeat, e := worker.Reconcile(ctx, scope, operation)
	if e != nil || repeat != result {
		t.Fatal("Bootstrap duplicate effects", e)
	}
	if e = runtime.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND event_id=$2 AND action='iam.bootstrap.invitation.created'`, tenant, result.AuditID).Scan(&count); e != nil || count != 1 {
		t.Fatal("missing atomic audit", e)
	}
	if e = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1", tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("invitation became Membership", e)
	}
	runWR33SnapshotPostgresComponent(t, ctx, migrator, provisioner, repo, epoch, tenant, registry)
	// Every receive and worker replay rechecks all current exact grants.
	executeGrant.ExpectedVersion = 1
	executeGrant.Status = "revoked"
	apply("grants", nil, []CoreBrokerGrantChange{executeGrant})
	if _, e = worker.Reconcile(ctx, scope, operation); !errors.Is(e, biz.ErrCoreBrokerAuthority) {
		t.Fatal("worker ignored execute revocation", e)
	}
	if e = repo.Receive(ctx, incoming, intent); !errors.Is(e, biz.ErrCoreBrokerAuthority) {
		t.Fatal("duplicate receive ignored execute revocation", e)
	}
	prior, e := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: config.Producer})
	if e != nil {
		t.Fatal(e)
	}
	heartbeatGrant.ExpectedVersion = 1
	heartbeatGrant.Status = "revoked"
	apply("grants", nil, []CoreBrokerGrantChange{heartbeatGrant})
	deniedHeartbeat := heartbeat()
	if e = repo.Receive(ctx, message(deniedHeartbeat, 4), deniedHeartbeat); !errors.Is(e, biz.ErrCoreBrokerAuthority) {
		t.Fatal("heartbeat ignored publish revocation", e)
	}
	after, e := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: config.Producer})
	if e != nil || after.HeartbeatID != prior.HeartbeatID || after.HeartbeatReceivedAt != prior.HeartbeatReceivedAt {
		t.Fatal("denied heartbeat refreshed observation", e)
	}
	if _, e = runtime.Exec(ctx, "UPDATE tenant_broker_authority_receipts SET route_version=2 WHERE consumer_id=$1", config.ConsumerID); e == nil {
		t.Fatal("runtime rewrote immutable authority")
	}
	wrongScope, _ := biz.NewTenantScope(id())
	if _, e = worker.Reconcile(ctx, wrongScope, operation); !errors.Is(e, biz.ErrCoreBootstrapConflict) {
		t.Fatal("cross-Tenant Bootstrap lookup", e)
	}
	runWR33AuthorityPostgresComponent(t, ctx, migrator, provisioner, runtime, registry)
	t.Log("real PG broker current grants, concurrent receipt, Bootstrap-before-Lifecycle deferral, invitation/audit rollback, idempotent worker, no Membership, revocation and denied-heartbeat continuity passed")
}
