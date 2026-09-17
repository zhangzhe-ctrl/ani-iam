//go:build integration && !governance

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nkeys"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

// This real restricted-PG gate proves storage/authority semantics. Its transport
// coordinates are component inputs; actual broker attribution is a separate gate.
func TestWR23ResumeBrokerPostgres(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	d := data.NewData(e.runtimePool)
	if err := data.ValidateRuntimeFoundation(ctx, d); err != nil {
		t.Fatal(err)
	}
	admin := mustPool(t, e.migrationDSN(primaryDB))
	defer admin.Close()
	provisioner := mustPool(t, postgresDSN(provisionerRole, e.provisionerPass, e.host, primaryDB, "wr23-broker-provisioner"))
	defer provisioner.Close()
	owner, workloads := newBootstrapManifest(t, strings.Repeat("a", 64))
	// These broker identities receive no unrelated synchronous RPC permission.
	workloads.Workloads[0].Grants = nil
	second := workloads.Workloads[0]
	second.PrincipalID = mustV7(t)
	second.BindingID = mustV7(t)
	second.Name = "iam-consumer"
	second.DNSIdentity = "iam-consumer.wr19.test"
	second.Grants = append([]biz.BootstrapWorkloadGrant(nil), second.Grants...)
	for i := range second.Grants {
		second.Grants[i].ID = mustV7(t)
	}
	workloads.Workloads = append(workloads.Workloads, second)
	registry := wr32Registry(t)
	if _, err := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(provisioner), registry), data.NewSystemClock(), registry).Provision(ctx, owner, workloads); err != nil {
		t.Fatal(err)
	}
	public := func() string {
		key, err := nkeys.CreateUser()
		if err != nil {
			t.Fatal(err)
		}
		defer key.Wipe()
		v, err := key.PublicKey()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	producerKey, consumerKey := public(), public()
	producerBinding, consumerBinding := mustV7(t), mustV7(t)
	cfg := data.CoreBrokerConfiguration{Environment: owner.Environment, TrustDomain: owner.TrustDomain, BrokerName: "wr23", Account: "WR23", Stream: "CORE", Consumer: "IAM", ConsumerID: mustV7(t), ConsumerBindingID: consumerBinding, ConsumerNKey: consumerKey, Producer: "core.wr23.test"}
	for _, subject := range []string{"ani.integration.tenant.lifecycle.v1", "ani.integration.tenant.iam-bootstrap.v1", "ani.integration.tenant.lifecycle-heartbeat.v1"} {
		r := data.CoreBrokerRoute{ID: mustV7(t), Subject: subject, ProducerBindingID: producerBinding, ProducerNKey: producerKey}
		r.TargetSHA256 = cfg.RouteRevision(r)
		cfg.Routes = append(cfg.Routes, r)
	}
	newManifest := func(mode string) data.CoreBrokerProvisionManifest {
		return data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Mode: mode, Reason: "WR23 restricted database component acceptance", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: cfg}
	}
	apply := func(m data.CoreBrokerProvisionManifest) (data.CoreBrokerProvisionReceipt, error) {
		raw, _ := json.Marshal(m)
		sum := sha256.Sum256(raw)
		return data.ApplyCoreBrokerManifest(ctx, data.NewData(provisioner), raw, hex.EncodeToString(sum[:]), owner.Environment, owner.TrustDomain, time.Now().UTC())
	}
	registration := newManifest("register")
	registration.Bindings = []data.CoreBrokerBindingChange{{ID: producerBinding, PrincipalID: workloads.Workloads[0].PrincipalID, NKeyPublic: producerKey, Status: "active"}, {ID: consumerBinding, PrincipalID: second.PrincipalID, NKeyPublic: consumerKey, Status: "active"}}
	first, err := apply(registration)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := apply(registration)
	if err != nil || repeat != first {
		t.Fatalf("registration retry %v", err)
	}
	drift := registration
	drift.Reason = "different reviewed bytes under the same identifier"
	if _, err = apply(drift); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
		t.Fatalf("manifest identity drift: %v", err)
	}
	repo, err := data.NewCoreBrokerRepository(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Check(ctx); !errors.Is(err, biz.ErrCoreBrokerAuthority) {
		t.Fatalf("registration authorized itself: %v", err)
	}
	authorization := newManifest("grants")
	for _, r := range cfg.Routes {
		authorization.Grants = append(authorization.Grants, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: workloads.Workloads[0].PrincipalID, BindingID: producerBinding, RouteID: r.ID, Action: "publish", Status: "active"}, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: second.PrincipalID, BindingID: consumerBinding, RouteID: r.ID, Action: "receive", Status: "active"})
		if strings.Contains(r.Subject, "iam-bootstrap") {
			authorization.Grants = append(authorization.Grants, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: second.PrincipalID, BindingID: consumerBinding, RouteID: r.ID, Action: "execute", Status: "active"})
		}
	}
	if _, err = apply(authorization); err != nil {
		t.Fatal(err)
	}
	if err = repo.Check(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(newManifest("bindings"))
	sum := sha256.Sum256(raw)
	if _, err = data.ApplyCoreBrokerManifest(ctx, d, raw, hex.EncodeToString(sum[:]), owner.Environment, owner.TrustDomain, time.Now()); err == nil {
		t.Fatal("runtime self authorization")
	}
	projection, err := data.NewCoreLifecycleProjectionRepository(d, cfg.Producer)
	if err != nil {
		t.Fatal(err)
	}
	if err = projection.InitializeShadow(ctx); err != nil {
		t.Fatal(err)
	}
	heartbeat := func(source, position int64) biz.CoreBrokerMessage {
		event := mustV7(t)
		at := time.Now().UTC().Truncate(time.Microsecond)
		raw, _ := json.Marshal(map[string]any{"heartbeat_id": event.String(), "schema_major": 1, "producer": cfg.Producer, "occurred_at": at, "source_sequence": fmt.Sprint(source)})
		return biz.CoreBrokerMessage{DeliveryID: mustV7(t), ConsumerID: cfg.ConsumerID, BrokerName: cfg.BrokerName, Account: cfg.Account, Stream: cfg.Stream, Consumer: cfg.Consumer, Subject: cfg.Routes[2].Subject, BrokerSequence: position, DeliveryCount: 1, PublishedAt: at, Payload: raw}
	}
	decoder := service.NewCoreBrokerDecoder()
	receive := func(m biz.CoreBrokerMessage) error {
		decoded, err := decoder.Decode(m)
		if err != nil {
			return err
		}
		return repo.Receive(ctx, m, decoded)
	}
	m := heartbeat(1, 1)
	if err = receive(m); err != nil {
		t.Fatal(err)
	}
	if err = receive(m); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = e.runtimePool.QueryRow(ctx, "SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1", cfg.ConsumerID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate receipt")
	}
	changed := m
	changed.Payload = append([]byte(nil), m.Payload...)
	changed.Payload = append(changed.Payload, ' ')
	if err = receive(changed); err == nil {
		t.Fatal("changed immutable payload")
	}
	wrong := heartbeat(2, 2)
	wrong.Account = "OTHER"
	if err = receive(wrong); !errors.Is(err, biz.ErrCoreBrokerAuthority) {
		t.Fatal("account self attribution")
	}
	// A revoker cannot commit while a business transaction holds the shared
	// current-authority lock. It becomes visible immediately after that commit.
	held, err := e.runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = held.Exec(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended('ani-iam:core-broker-authority',0))"); err != nil {
		t.Fatal(err)
	}
	revoke := newManifest("grants")
	grant := authorization.Grants[len(authorization.Grants)-2]
	grant.ExpectedVersion = 1
	grant.Status = "revoked"
	revoke.Grants = []data.CoreBrokerGrantChange{grant}
	result := make(chan error, 1)
	go func() { _, err := apply(revoke); result <- err }()
	select {
	case err = <-result:
		t.Fatalf("revoke passed active transaction: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err = held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoke stayed blocked")
	}
	denied := heartbeat(2, 2)
	if err = receive(denied); !errors.Is(err, biz.ErrCoreBrokerAuthority) {
		t.Fatalf("revocation: %v", err)
	}
	if err = repo.Quarantine(ctx, denied, "authority_denied"); err != nil {
		t.Fatal(err)
	}
	restore := newManifest("grants")
	grant.ExpectedVersion = 2
	grant.Status = "active"
	restore.Grants = []data.CoreBrokerGrantChange{grant}
	if _, err = apply(restore); err != nil {
		t.Fatal(err)
	}
	if err = receive(denied); !errors.Is(err, biz.ErrCoreBrokerQuarantined) {
		t.Fatalf("credential restoration replayed denied request: %v", err)
	}
	old := m
	old.BrokerSequence = 3
	if err = receive(old); !errors.Is(err, biz.ErrCoreBrokerAuthority) {
		t.Fatalf("old authority version: %v", err)
	}
	fresh := heartbeat(2, 4)
	if err = receive(fresh); err != nil {
		t.Fatalf("fresh authorized fact: %v", err)
	}
	// Poison retention may fail, but cannot be treated as durable success.
	if _, err = admin.Exec(ctx, "REVOKE INSERT ON core_broker_dlq FROM ani_iam_runtime"); err != nil {
		t.Fatal(err)
	}
	poison := heartbeat(3, 5)
	poison.Payload = []byte("broken")
	if err = repo.Quarantine(ctx, poison, "invalid_event"); err == nil {
		t.Fatal("DLQ persistence failure ignored")
	}
	if _, err = admin.Exec(ctx, "GRANT INSERT ON core_broker_dlq TO ani_iam_runtime"); err != nil {
		t.Fatal(err)
	}
	if err = repo.Quarantine(ctx, poison, "invalid_event"); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"UPDATE core_broker_dlq SET raw_payload='changed'", "DELETE FROM core_broker_authority_receipts", "UPDATE core_broker_bindings SET status='revoked',version=version+1", "INSERT INTO core_broker_administration_receipts(id,manifest_sha256,manifest,mode,reason,provisioner_role) VALUES(gen_random_uuid(),decode(repeat('aa',32),'hex'),'{}','register','forbidden runtime write','ani_iam_provisioner')"} {
		if _, err = e.runtimePool.Exec(ctx, statement); err == nil {
			t.Fatal("runtime exceeded bounded privileges")
		}
	}
	if _, err = admin.Exec(ctx, "UPDATE core_broker_administration_receipts SET reason='rewritten audit'"); err == nil {
		t.Fatal("immutable audit changed")
	}
	if err = data.ValidateRuntimeFoundation(ctx, d); err != nil {
		t.Fatal(err)
	}
	// Both bootstrap and identity readers share the same current generation.
	tenantA, tenantB := mustV7(t), mustV7(t)
	lifecycle := func(tenant string, version, source, position int64, status, reason string) biz.CoreBrokerMessage {
		m := heartbeat(source, position)
		m.Subject = cfg.Routes[0].Subject
		at := time.Now().UTC().Truncate(time.Microsecond)
		m.Payload, _ = json.Marshal(map[string]any{"envelope": map[string]any{"event_id": mustV7(t).String(), "schema_major": 1, "producer": cfg.Producer, "tenant_id": tenant, "aggregate_version": fmt.Sprint(version), "source_sequence": fmt.Sprint(source), "occurred_at": at}, "status": status, "reason": reason, "effective_at": at})
		return m
	}
	for _, m := range []biz.CoreBrokerMessage{lifecycle(tenantA.String(), 1, 3, 6, "TENANT_LIFECYCLE_STATUS_ACTIVE", "TENANT_LIFECYCLE_REASON_CREATED"), lifecycle(tenantB.String(), 1, 4, 7, "TENANT_LIFECYCLE_STATUS_ACTIVE", "TENANT_LIFECYCLE_REASON_CREATED")} {
		if err = receive(m); err != nil {
			t.Fatal(err)
		}
	}
	current, err := e.runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Rollback(ctx)
	if _, err = current.Exec(ctx, "SELECT set_config('ani_iam.core_producer',$1,true)", cfg.Producer); err != nil {
		t.Fatal(err)
	}
	var status string
	var version int64
	var freshView bool
	if err = current.QueryRow(ctx, "SELECT status,version,fresh_until>clock_timestamp() FROM current_tenant_lifecycle WHERE tenant_id=$1", tenantA).Scan(&status, &version, &freshView); err != nil || status != "active" || version != 1 || !freshView {
		t.Fatalf("current generation %s %d %t %v", status, version, freshView, err)
	}
	if err = receive(lifecycle(tenantA.String(), 3, 5, 8, "TENANT_LIFECYCLE_STATUS_FROZEN", "TENANT_LIFECYCLE_REASON_ADMINISTRATIVE_FREEZE")); err != nil {
		t.Fatal(err)
	}
	if err = current.QueryRow(ctx, "SELECT fresh_until>clock_timestamp() FROM current_tenant_lifecycle WHERE tenant_id=$1", tenantA).Scan(&freshView); err != nil || freshView {
		t.Fatal("Tenant gap reported fresh")
	}
	if err = current.QueryRow(ctx, "SELECT fresh_until>clock_timestamp() FROM current_tenant_lifecycle WHERE tenant_id=$1", tenantB).Scan(&freshView); err != nil || !freshView {
		t.Fatal("Tenant gap contaminated another Tenant")
	}
	if _, err = current.Exec(ctx, "SELECT set_config('ani_iam.core_producer','unconfigured-source',true)"); err != nil {
		t.Fatal(err)
	}
	if err = current.QueryRow(ctx, "SELECT count(*) FROM current_tenant_lifecycle WHERE tenant_id=$1", tenantA).Scan(&count); err != nil || count != 0 {
		t.Fatal("current producer fallback")
	}
	if _, err = e.runtimePool.Exec(ctx, "SELECT * FROM tenant_lifecycle_projections"); err == nil {
		t.Fatal("historical projection remained readable")
	}
	// These are explicit owner-data component inputs. The formal REST and
	// simultaneous owner-write scenario must separately prove their authority.
	recovery, err := data.NewCoreBrokerSnapshotRecoveryRepository(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := recovery.NextRecovery(ctx)
	if err != nil || !plan.Needed || plan.Cursor.ID != uuid.Nil {
		t.Fatal("initial recovery plan", err)
	}
	cursor := biz.CoreSnapshotCursor{ID: mustV7(t), ConsumerID: cfg.ConsumerID, Producer: cfg.Producer, SourceCut: 5, BrokerAfter: 8, PageSize: 1, ExpiresAt: time.Now().UTC().Add(200 * time.Millisecond).Truncate(time.Microsecond)}
	if _, err = recovery.Begin(ctx, cursor); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if plan, err = recovery.NextRecovery(ctx); err != nil || !plan.Needed || plan.Cursor.ID != uuid.Nil {
		t.Fatal("expired cut was not abandoned", err)
	}
	var abandoned string
	if err = e.runtimePool.QueryRow(ctx, "SELECT abandoned_reason FROM core_lifecycle_rebuilds WHERE snapshot_id=$1", cursor.ID).Scan(&abandoned); err != nil || abandoned != "cursor_expired" {
		t.Fatal("expired cut audit", err)
	}
	if _, err = e.runtimePool.Exec(ctx, "UPDATE core_lifecycle_rebuilds SET state='loading',abandoned_reason='' WHERE snapshot_id=$1", cursor.ID); err == nil {
		t.Fatal("abandoned cut rewritten")
	}
	cursor.ID = mustV7(t)
	cursor.ExpiresAt = time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if _, err = recovery.Begin(ctx, cursor); err != nil {
		t.Fatal(err)
	}
	page := biz.CoreSnapshotPage{Cursor: cursor, NextToken: tenantA.String(), Items: []biz.CoreSnapshotFact{{TenantID: tenantA, Version: 3, Status: "frozen"}}}
	if _, err = recovery.LoadPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	recovery, err = data.NewCoreBrokerSnapshotRecoveryRepository(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plan, err = recovery.NextRecovery(ctx); err != nil || plan.Cursor.ID != cursor.ID || !plan.Cursor.ExpiresAt.Equal(cursor.ExpiresAt) || plan.Build.NextToken != page.NextToken || plan.Build.LoadedItems != 1 {
		t.Fatal("restart lost cursor/page progress", err)
	}
	if err = receive(lifecycle(tenantB.String(), 2, 6, 9, "TENANT_LIFECYCLE_STATUS_FROZEN", "TENANT_LIFECYCLE_REASON_ADMINISTRATIVE_FREEZE")); err != nil {
		t.Fatal(err)
	}
	page.RequestToken = page.NextToken
	page.NextToken = ""
	page.Items = []biz.CoreSnapshotFact{{TenantID: tenantB, Version: 1, Status: "active"}}
	if _, err = recovery.LoadPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if _, err = recovery.ActivateShadow(ctx, cursor.ID); !errors.Is(err, biz.ErrCoreSnapshotNotReady) {
		t.Fatal("activated before buffered increment", err)
	}
	if _, err = recovery.CatchUp(ctx, cursor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = recovery.ActivateShadow(ctx, cursor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = current.Exec(ctx, "SELECT set_config('ani_iam.core_producer',$1,true)", cfg.Producer); err != nil {
		t.Fatal(err)
	}
	if err = current.QueryRow(ctx, "SELECT status,version,fresh_until>clock_timestamp() FROM current_tenant_lifecycle WHERE tenant_id=$1", tenantB).Scan(&status, &version, &freshView); err != nil || status != "frozen" || version != 2 || !freshView {
		t.Fatal("atomic activation lost concurrent increment", err)
	}
	if plan, err = recovery.NextRecovery(ctx); err != nil || plan.Needed {
		t.Fatal("repaired generation remained pending", err)
	}
	// A Broker version change fences both a loaded cut and a cut whose
	// increments were applied before revocation but are not yet activated.
	lifecycleGrant := authorization.Grants[0]
	lifecycleGrant.ExpectedVersion = 1
	for index, catchBeforeRevoke := range []bool{false, true} {
		cut := int64(6 + index)
		version := int64(3 + index)
		status, nextStatus, wireStatus, reason := "frozen", "active", "TENANT_LIFECYCLE_STATUS_ACTIVE", "TENANT_LIFECYCLE_REASON_RESTORED"
		if index == 1 {
			status, nextStatus, wireStatus, reason = "active", "frozen", "TENANT_LIFECYCLE_STATUS_FROZEN", "TENANT_LIFECYCLE_REASON_ADMINISTRATIVE_FREEZE"
		}
		cutCursor := biz.CoreSnapshotCursor{ID: mustV7(t), ConsumerID: cfg.ConsumerID, Producer: cfg.Producer, SourceCut: cut, BrokerAfter: cut + 3, PageSize: 2, ExpiresAt: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
		if _, err = recovery.Begin(ctx, cutCursor); err != nil {
			t.Fatal("begin authority-fence cut", err)
		}
		if _, err = e.runtimePool.Exec(ctx, "UPDATE core_lifecycle_rebuilds SET broker_authority_sha256=repeat('b',64) WHERE producer=$1 AND snapshot_id=$2", cfg.Producer, cutCursor.ID); err == nil {
			t.Fatal("runtime rewrote pending cut authority fingerprint")
		}
		cutPage := biz.CoreSnapshotPage{Cursor: cutCursor, Items: []biz.CoreSnapshotFact{{TenantID: tenantA, Version: version, Status: status}, {TenantID: tenantB, Version: 2, Status: "frozen"}}}
		if _, err = recovery.LoadPage(ctx, cutPage); err != nil {
			t.Fatal("load authority-fence cut", err)
		}
		if err = receive(lifecycle(tenantA.String(), version+1, cut+1, cut+4, wireStatus, reason)); err != nil {
			t.Fatal("authorized cut increment", err)
		}
		if catchBeforeRevoke {
			if _, err = recovery.CatchUp(ctx, cutCursor.ID); err != nil {
				t.Fatal("initial current-authority catch-up", err)
			}
		}
		change := newManifest("grants")
		lifecycleGrant.Status = "revoked"
		change.Grants = []data.CoreBrokerGrantChange{lifecycleGrant}
		if _, err = apply(change); err != nil {
			t.Fatal("revoke lifecycle authority", err)
		}
		lifecycleGrant.ExpectedVersion++
		if _, err = recovery.CatchUp(ctx, cutCursor.ID); !errors.Is(err, biz.ErrCoreBrokerAuthority) {
			t.Fatal("revoked pending cut permitted replay", err)
		}
		change = newManifest("grants")
		lifecycleGrant.Status = "active"
		change.Grants = []data.CoreBrokerGrantChange{lifecycleGrant}
		if _, err = apply(change); err != nil {
			t.Fatal("restore lifecycle authority", err)
		}
		lifecycleGrant.ExpectedVersion++
		if plan, err = recovery.NextRecovery(ctx); err != nil || !plan.Needed || plan.Cursor.ID != uuid.Nil {
			t.Fatal("changed authority did not retain and abandon old cut for a new Snapshot", err)
		}
		if err = e.runtimePool.QueryRow(ctx, "SELECT abandoned_reason FROM core_lifecycle_rebuilds WHERE producer=$1 AND snapshot_id=$2", cfg.Producer, cutCursor.ID).Scan(&abandoned); err != nil || abandoned != "authority_changed" {
			t.Fatal("missing immutable authority-fence reason", err)
		}
		if _, err = recovery.ActivateShadow(ctx, cutCursor.ID); !errors.Is(err, biz.ErrCoreProjectionConflict) {
			t.Fatal("old cut activated after restored authority", err)
		}
		cutCursor.ID = mustV7(t)
		cutCursor.SourceCut++
		cutCursor.BrokerAfter++
		cutPage.Cursor = cutCursor
		cutPage.Items[0].Version++
		cutPage.Items[0].Status = nextStatus
		if _, err = recovery.Begin(ctx, cutCursor); err != nil {
			t.Fatal("new authoritative cut", err)
		}
		if _, err = recovery.LoadPage(ctx, cutPage); err != nil {
			t.Fatal("new cut pages", err)
		}
		if _, err = recovery.CatchUp(ctx, cutCursor.ID); err != nil {
			t.Fatal("new cut catch-up", err)
		}
		if _, err = recovery.ActivateShadow(ctx, cutCursor.ID); err != nil {
			t.Fatal("new cut activation", err)
		}
	}
	t.Log("pass: restricted PG, separate registration/Grants, immutable receipt/audit, exact current authority, serialized revoke, denied replay fence, failed DLQ persistence")
}

const wr23ResumeNATSImage = "docker.io/library/nats@sha256:065e8355c20a5575b3c77224be1855e8103fd148b68fba05130b9b8ddfa40ccc"

// Actual NATS transport/ACL evidence. This gate intentionally does not substitute
// for the formal Core outbox/HTTP owner or for the separate PG business commit.
func TestWR23ResumeNATSTransport(t *testing.T) {
	run, goal := isolatedRun(t)
	if goal != "wr23" {
		t.Fatal("WR23 isolated run required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	private := filepath.Join(run, "private", "broker-"+mustV7(t).String())
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	ca, caKey, caFile := writeProcessE2ECertificateAuthority(t, private)
	certificate, keyFile := writeProcessE2ELeafCertificate(t, private, "broker", "broker.wr23.test", x509.ExtKeyUsageServerAuth, ca, caKey)
	keys := map[string]nkeys.KeyPair{}
	public := map[string]string{}
	seeds := map[string]string{}
	for _, role := range []string{"operator", "producer", "consumer", "other"} {
		key, err := nkeys.CreateUser()
		if err != nil {
			t.Fatal(err)
		}
		defer key.Wipe()
		keys[role] = key
		public[role], err = key.PublicKey()
		if err != nil {
			t.Fatal(err)
		}
		seed, err := key.Seed()
		if err != nil {
			t.Fatal(err)
		}
		// Seed aliases the live signing key; only clear our exported copy.
		seed = append([]byte(nil), seed...)
		file := filepath.Join(private, role+".seed")
		if err = os.WriteFile(file, seed, 0600); err != nil {
			t.Fatal(err)
		}
		clear(seed)
		seeds[role] = file
	}
	inbox := "_WR23." + filepath.Base(run)
	manifest := map[string]any{"version": 1, "run_id": filepath.Base(run), "broker_name": "wr23-broker", "account": "WR23", "stream": "CORE", "consumer": "IAM", "inbox_prefix": inbox, "public_keys": public}
	raw, _ := json.Marshal(manifest)
	manifestFile := filepath.Join(private, "environment.json")
	if os.WriteFile(manifestFile, raw, 0600) != nil {
		t.Fatal("manifest write")
	}
	sum := sha256.Sum256(raw)
	render := func(state, tag string) string {
		file := filepath.Join(private, tag+".conf")
		cmd := exec.CommandContext(ctx, "python3", filepath.Join(findRepositoryRoot(t), "tools/wr23-resume/broker-config.py"), "--manifest", manifestFile, "--approved-sha256", hex.EncodeToString(sum[:]), "--state", state, "--output", file, "--proof", filepath.Join(run, tag+"-results.json"))
		if output, err := cmd.CombinedOutput(); err != nil {
			_ = os.WriteFile(filepath.Join(private, tag+".log"), output, 0600)
			t.Fatal("render finite ACL")
		}
		return file
	}
	initial := render("active", "broker-active")
	c, err := testcontainers.Run(ctx, wr23ResumeNATSImage, testcontainers.WithCmd("-c", "/run/wr23/nats.conf"), testcontainers.WithFiles(
		testcontainers.ContainerFile{HostFilePath: initial, ContainerFilePath: "/run/wr23/nats.conf", FileMode: 0600},
		testcontainers.ContainerFile{HostFilePath: certificate, ContainerFilePath: "/run/wr23/server.crt", FileMode: 0644},
		testcontainers.ContainerFile{HostFilePath: keyFile, ContainerFilePath: "/run/wr23/server.key", FileMode: 0600},
		testcontainers.ContainerFile{HostFilePath: caFile, ContainerFilePath: "/run/wr23/ca.crt", FileMode: 0644}), testcontainers.WithExposedPorts("4222/tcp", "8222/tcp"), isolatedContainer(t, "nats", "4222/tcp", "8222/tcp"), testcontainers.WithWaitStrategy(wait.ForHTTP("/varz").WithPort("8222/tcp").WithStartupTimeout(30*time.Second)))
	if c != nil {
		t.Cleanup(func() {
			if err := testcontainers.TerminateContainer(c); err != nil {
				t.Error("terminate own NATS", err)
			}
		})
	}
	if err != nil {
		t.Fatal("start pinned NATS", err)
	}
	port, err := c.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatal(err)
	}
	monitorPort, err := c.MappedPort(ctx, "8222/tcp")
	if err != nil {
		t.Fatal(err)
	}
	address := "tls://127.0.0.1:" + port.Port()
	monitor := "http://127.0.0.1:" + monitorPort.Port()
	rootPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		t.Fatal("own TLS root")
	}
	connect := func(role string) (*nats.Conn, chan error, error) {
		failures := make(chan error, 16)
		nc, err := nats.Connect(address, nats.Nkey(public[role], keys[role].Sign), nats.Secure(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: "broker.wr23.test", RootCAs: roots}), nats.TLSHandshakeFirst(), nats.IgnoreDiscoveredServers(), nats.CustomInboxPrefix(inbox+"."+role), nats.Timeout(2*time.Second), nats.ReconnectWait(100*time.Millisecond), nats.MaxReconnects(3), nats.ReconnectBufSize(0), nats.PermissionErrOnSubscribe(true), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			select {
			case failures <- err:
			default:
			}
		}))
		if err == nil {
			t.Cleanup(nc.Close)
		}
		return nc, failures, err
	}
	operator, _, err := connect("operator")
	if err != nil {
		t.Fatal("operator NKey TLS", err)
	}
	if operator.ConnectedServerName() != "wr23-broker" {
		t.Fatal("broker identity")
	}
	version, err := exec.CommandContext(ctx, "docker", "exec", c.GetContainerID(), "nats-server", "--version").Output()
	if err != nil || !strings.Contains(string(version), "v2.14.6") {
		t.Fatal("actual pinned NATS binary version")
	}
	js, err := operator.JetStream(nats.MaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	subjects := []string{"ani.integration.tenant.lifecycle.v1", "ani.integration.tenant.iam-bootstrap.v1", "ani.integration.tenant.lifecycle-heartbeat.v1"}
	if _, err = js.AddStream(&nats.StreamConfig{Name: "CORE", Subjects: subjects, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, Replicas: 1, DenyDelete: true, DenyPurge: true, Discard: nats.DiscardNew, MaxMsgSize: 65536, MaxBytes: 128 << 20, MaxAge: 48 * time.Hour}); err != nil {
		t.Fatal("owner stream install", err)
	}
	if _, err = js.AddConsumer("CORE", &nats.ConsumerConfig{Name: "IAM", Durable: "IAM", DeliverPolicy: nats.DeliverAllPolicy, AckPolicy: nats.AckExplicitPolicy, AckWait: 30 * time.Second, MaxDeliver: -1, FilterSubjects: subjects, ReplayPolicy: nats.ReplayInstantPolicy, MaxAckPending: 1, MaxWaiting: 4, MaxRequestBatch: 1, MaxRequestExpires: 2 * time.Second, Replicas: 1}); err != nil {
		t.Fatal("owner consumer install", err)
	}
	producer, producerFailures, err := connect("producer")
	if err != nil {
		t.Fatal("producer independent NKey", err)
	}
	publish, err := producer.JetStream(nats.MaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	producerBinding := mustV7(t)
	config := data.CoreBrokerConfiguration{Environment: "wr23-broker", TrustDomain: "wr23.test", BrokerName: "wr23-broker", Account: "WR23", Stream: "CORE", Consumer: "IAM", ConsumerID: mustV7(t), ConsumerBindingID: mustV7(t), ConsumerNKey: public["consumer"], Producer: "core.wr23.test"}
	for _, subject := range subjects {
		r := data.CoreBrokerRoute{ID: mustV7(t), Subject: subject, ProducerBindingID: producerBinding, ProducerNKey: public["producer"]}
		r.TargetSHA256 = config.RouteRevision(r)
		config.Routes = append(config.Routes, r)
	}
	natsConfig := data.CoreNATSConfiguration{URL: address, ServerName: "broker.wr23.test", InboxPrefix: inbox + ".consumer", SeedFile: seeds["consumer"], CAFile: caFile, Authority: config}
	consumer, err := data.NewCoreNATSConsumer(ctx, natsConfig)
	if err != nil {
		t.Fatal("actual durable consumer", err)
	}
	defer consumer.Close()
	payload := []byte(`{"transport":"actual independently authenticated publisher"}`)
	ack, err := publish.Publish(subjects[2], payload, nats.ExpectStream("CORE"), nats.MsgId(mustV7(t).String()))
	if err != nil || ack.Stream != "CORE" {
		t.Fatal("publish ACK", err)
	}
	message, err := consumer.Next(ctx)
	if err != nil || !bytes.Equal(message.Payload, payload) || message.BrokerSequence != int64(ack.Sequence) || message.Account != "WR23" || message.Stream != "CORE" || message.Consumer != "IAM" || message.ConsumerID != config.ConsumerID || message.PublishedAt.IsZero() {
		t.Fatal("trusted broker metadata", err)
	}
	if err = consumer.Ack(ctx, mustV7(t)); err == nil {
		t.Fatal("ACK accepted an unknown delivery")
	}
	if err = consumer.Retry(ctx, message.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err = consumer.Close(); err != nil {
		t.Fatal(err)
	}
	consumer, err = data.NewCoreNATSConsumer(ctx, natsConfig)
	if err != nil {
		t.Fatal("consumer reconnect", err)
	}
	defer consumer.Close()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		message, err = consumer.Next(ctx)
		if err == nil {
			break
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if err != nil || message.DeliveryCount < 2 || message.BrokerSequence != int64(ack.Sequence) {
		t.Fatal("durable redelivery after restart", err)
	}
	if err = consumer.Ack(ctx, message.DeliveryID); err != nil {
		t.Fatal("durable ACK/reply ACL", err)
	}
	info, err := js.ConsumerInfo("CORE", "IAM")
	if err != nil || info.AckFloor.Stream != ack.Sequence {
		t.Fatal("server did not commit ACK", err)
	}
	denyPublish := func(nc *nats.Conn, failures chan error, subject string) {
		for len(failures) > 0 {
			<-failures
		}
		if err := nc.Publish(subject, []byte("denial-probe")); err != nil {
			return
		}
		_ = nc.FlushTimeout(time.Second)
		select {
		case err := <-failures:
			if !errors.Is(err, nats.ErrPermissionViolation) {
				t.Fatalf("unexpected broker refusal %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("broker did not reject unauthorized publish")
		}
	}
	denyPublish(producer, producerFailures, "$JS.API.STREAM.UPDATE.CORE")
	denyPublish(producer, producerFailures, inbox+".consumer.inject")
	denyPublish(producer, producerFailures, "ani.integration.other.v1")
	ordinary, ordinaryFailures, err := connect("consumer")
	if err != nil {
		t.Fatal(err)
	}
	denyPublish(ordinary, ordinaryFailures, subjects[0])
	denyPublish(ordinary, ordinaryFailures, "$JS.ACK.CORE.OTHER.1.1.1.1.0")
	if sub, err := producer.SubscribeSync(inbox + ".consumer.>"); err == nil {
		_ = producer.FlushTimeout(time.Second)
		select {
		case failure := <-producerFailures:
			if !errors.Is(failure, nats.ErrPermissionViolation) {
				t.Fatal(failure)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("other consumer subscription accepted")
		}
		_ = sub.Unsubscribe()
	}
	reload := func(candidate, tag string, wantSuccess bool) {
		raw, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		resultPath := filepath.Join(run, tag+"-results.json")
		command := exec.CommandContext(ctx, "bash", filepath.Join(findRepositoryRoot(t), "tools/wr23-resume/broker-reload.sh"), "--run", run, "--container", c.GetContainerID(), "--candidate", candidate, "--approved-sha256", hex.EncodeToString(sum[:]), "--monitor", monitor, "--result", resultPath)
		output, err := command.CombinedOutput()
		_ = os.WriteFile(filepath.Join(private, tag+".log"), output, 0600)
		if (err == nil) != wantSuccess {
			t.Fatal("controlled reload unexpected result; inspect private log")
		}
		raw, err = os.ReadFile(resultPath)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Applied  bool `json:"configuration_applied"`
			Verified bool `json:"permission_revocation_verified"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Applied != wantSuccess || result.Verified {
			t.Fatal("reload claimed permission proof")
		}
	}
	deniedConfig := render("producer_denied", "broker-producer-denied")
	reload(deniedConfig, "broker-producer-reload", true)
	denyPublish(producer, producerFailures, subjects[0])
	newProducer, newFailures, err := connect("producer")
	if err != nil {
		t.Fatal("denied ACL should retain independent identity authentication", err)
	}
	denyPublish(newProducer, newFailures, subjects[0])
	before, err := js.StreamInfo("CORE")
	if err != nil || before.State.LastSeq != ack.Sequence {
		t.Fatal("unauthorized publish changed stream", err)
	}
	invalid := filepath.Join(private, "invalid.conf")
	if os.WriteFile(invalid, []byte("invalid {"), 0600) != nil {
		t.Fatal("fault configuration")
	}
	reload(invalid, "broker-invalid-reload", false)
	denyPublish(newProducer, newFailures, subjects[0])
	consumerDenied := render("consumer_denied", "broker-consumer-denied")
	reload(consumerDenied, "broker-consumer-reload", true)
	denyPublish(ordinary, ordinaryFailures, "$JS.API.CONSUMER.MSG.NEXT.CORE.IAM")
	newConsumer, newConsumerFailures, err := connect("consumer")
	if err != nil {
		t.Fatal(err)
	}
	denyPublish(newConsumer, newConsumerFailures, "$JS.API.CONSUMER.MSG.NEXT.CORE.IAM")
	removed := render("producer_removed", "broker-producer-removed")
	reload(removed, "broker-producer-removed-reload", true)
	deadline = time.Now().Add(5 * time.Second)
	for producer.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if producer.IsConnected() {
		t.Fatal("removed producer old connection remained authenticated")
	}
	if nc, _, err := connect("producer"); err == nil {
		nc.Close()
		t.Fatal("removed producer new connection authenticated")
	}
	consumerRemoved := render("consumer_removed", "broker-consumer-removed")
	reload(consumerRemoved, "broker-consumer-removed-reload", true)
	deadline = time.Now().Add(5 * time.Second)
	for (ordinary.IsConnected() || newConsumer.IsConnected()) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ordinary.IsConnected() || newConsumer.IsConnected() {
		t.Fatal("removed consumer old connections remained authenticated")
	}
	if nc, _, err := connect("consumer"); err == nil {
		nc.Close()
		t.Fatal("removed consumer new connection authenticated")
	}
	result := map[string]any{"result": "pass", "grade": "real NATS transport and environment ACL only; formal owner chain remains separate", "binary_version": strings.TrimSpace(string(version)), "image": wr23ResumeNATSImage, "consumer_redelivery": message.DeliveryCount, "producer_old_new_publish_denied": true, "consumer_old_new_pull_denied": true, "other_inbox_injection_denied": true, "stream_configuration_denied": true, "reload_failure_kept_denial": true, "removed_producer_old_new_authentication_denied": true, "removed_consumer_old_new_authentication_denied": true}
	raw, _ = json.MarshalIndent(result, "", "  ")
	if os.WriteFile(filepath.Join(run, "broker-transport-results.json"), append(raw, '\n'), 0600) != nil {
		t.Fatal("evidence write")
	}
}
