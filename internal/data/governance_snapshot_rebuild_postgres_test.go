package data

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

// Tests the real durable rebuild component. The Snapshot source is explicit
// owner-fact fixture input; HTTP/TLS is tested separately, real owner awaits I4.
func runWR33SnapshotPostgresComponent(t *testing.T, ctx context.Context, migrator, provisioner *pgxpool.Pool, broker *coreBrokerRepository, epoch, tenant uuid.UUID, registry *workloadregistry.Registry) {
	t.Helper()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	runtime := broker.data.pool
	producer := broker.config.Producer
	q := sqlcgen.New(runtime)
	pq := sqlcgen.New(provisioner)
	if err := InstallWorkloadRegistry(ctx, NewData(migrator), registry, "empty"); err != nil {
		t.Fatal("install Snapshot target registry", err)
	}
	var reader uuid.UUID
	if err := runtime.QueryRow(ctx, "SELECT principal_id FROM core_broker_bindings WHERE id=$1", broker.config.ConsumerBindingID).Scan(&reader); err != nil {
		t.Fatal(err)
	}
	binding := id()
	dns := "iam.wr33-component.test"
	if err := pq.InsertBootstrapBinding(ctx, sqlcgen.InsertBootstrapBindingParams{BindingID: binding, PrincipalID: reader, Environment: broker.config.Environment, TrustDomain: broker.config.TrustDomain, IdentityValue: dns, Now: requiredTimestamptz(time.Now())}); err != nil {
		t.Fatal(err)
	}
	grants := map[string]uuid.UUID{}
	for _, operation := range []string{governancev1.SnapshotBeginOperation, governancev1.SnapshotPageOperation} {
		target, _ := registry.Lookup(governancev1.Audience, operation)
		grant := id()
		grants[operation] = grant
		if err := pq.InsertBootstrapGrant(ctx, sqlcgen.InsertBootstrapGrantParams{GrantID: grant, PrincipalID: reader, Environment: broker.config.Environment, TrustDomain: broker.config.TrustDomain, Audience: governancev1.Audience, Operation: operation, Scope: target.GrantScope, Now: requiredTimestamptz(time.Now())}); err != nil {
			t.Fatal(err)
		}
	}
	sourceConfig := GovernanceSnapshotRecoveryConfiguration{Broker: broker.config, ReaderID: reader, ReaderIdentity: dns, Registry: registry}
	repository, err := NewGovernanceSnapshotRecoveryRepository(broker.data, sourceConfig)
	if err != nil {
		t.Fatal(err)
	}
	cursor := func(sourceEpoch uuid.UUID, watermark, version int64, status string) (biz.TenantSnapshotCursor, biz.TenantSnapshotPage) {
		now := time.Now().UTC().Round(time.Microsecond)
		c := biz.TenantSnapshotCursor{ID: id(), Epoch: sourceEpoch, ReaderID: reader, Watermark: watermark, TotalCount: 1, PageSize: 1, CapturedAt: now, ExpiresAt: now.Add(4 * time.Minute), FirstToken: strings.Repeat("a", 32) + id().String()}
		return c, biz.TenantSnapshotPage{SnapshotID: c.ID, Epoch: c.Epoch, Watermark: c.Watermark, RequestToken: c.FirstToken, Items: []biz.TenantSnapshotFact{{TenantID: tenant, Version: version, Status: status}}}
	}
	receiveLife := func(sourceEpoch uuid.UUID, sequence, version, brokerSequence int64, status string) {
		event := biz.TenantLifecycleEvent{EventID: id(), Epoch: sourceEpoch, TenantID: tenant, Sequence: sequence, TenantVersion: version, Status: status, Reason: "Snapshot incremental fixture", OccurredAt: time.Now().UTC().Add(-time.Second)}
		event.EffectiveAt = event.OccurredAt
		wire := governancev1.LifecycleEvent{Revision: governancev1.Revision, Producer: governancev1.Producer, EventId: event.EventID.String(), Epoch: sourceEpoch.String(), Sequence: sequence, TenantId: tenant.String(), TenantVersion: version, BusinessStatus: governancev1.BusinessStatus(status), Reason: event.Reason, OccurredAt: event.OccurredAt, EffectiveAt: event.EffectiveAt}
		raw, _ := json.Marshal(wire)
		delivery := biz.TenantBrokerDelivery{EventID: event.EventID, Epoch: event.Epoch, TenantID: tenant, Producer: producer, Kind: "lifecycle", Sequence: sequence, OccurredAt: event.OccurredAt, RawPayload: raw, Lifecycle: &event}
		message := biz.CoreBrokerMessage{DeliveryID: id(), ConsumerID: broker.config.ConsumerID, BrokerName: broker.config.BrokerName, Account: broker.config.Account, Stream: broker.config.Stream, Consumer: broker.config.Consumer, Subject: governancev1.LifecycleSubject, BrokerSequence: brokerSequence, DeliveryCount: 1, PublishedAt: time.Now(), Payload: raw}
		if err := broker.Receive(ctx, message, delivery); err != nil {
			t.Fatal("retain Snapshot increment", err)
		}
	}
	c, page := cursor(epoch, 1, 1, "active")
	wrong := c
	wrong.ReaderID = id()
	if _, err = repository.Begin(ctx, wrong); !errors.Is(err, biz.ErrTenantLifecycleInvalid) {
		t.Fatal("Snapshot wrong reader accepted", err)
	}
	begin, err := repository.Begin(ctx, c)
	if err != nil {
		t.Fatal("Snapshot begin", err)
	}
	duplicate, err := repository.Begin(ctx, c)
	if err != nil || duplicate.GenerationID != begin.GenerationID {
		t.Fatal("Snapshot duplicate begin", err)
	}
	mismatched := page
	mismatched.Watermark++
	if _, err = repository.LoadPage(ctx, c, mismatched); err == nil {
		t.Fatal("Snapshot changed cut accepted")
	}
	loaded, err := repository.LoadPage(ctx, c, page)
	if err != nil || loaded.State != "loaded" {
		t.Fatal("Snapshot page", err)
	}
	if _, err = repository.LoadPage(ctx, c, page); err != nil {
		t.Fatal("Snapshot duplicate page", err)
	}
	altered := page
	altered.Items = append([]biz.TenantSnapshotFact(nil), page.Items...)
	altered.Items[0].Status = "frozen"
	if _, err = repository.LoadPage(ctx, c, altered); !errors.Is(err, biz.ErrTenantLifecycleConflict) {
		t.Fatal("Snapshot replay changed bytes", err)
	}
	receiveLife(epoch, 2, 2, 20, "frozen")
	if _, err = repository.Activate(ctx, c.ID); !errors.Is(err, biz.ErrTenantSnapshotNotReady) {
		t.Fatal("Snapshot skipped retained increment", err)
	}
	caught, err := repository.CatchUp(ctx, c.ID)
	if err != nil || caught.AppliedThrough != 2 {
		t.Fatal("Snapshot catch-up", err)
	}
	activated, err := repository.Activate(ctx, c.ID)
	if err != nil || activated.State != "activated" {
		t.Fatal("Snapshot activation", err)
	}
	current, err := q.ReadTenantLifecycleProjection(ctx, sqlcgen.ReadTenantLifecycleProjectionParams{Producer: producer, TenantID: tenant})
	if err != nil || current.TenantVersion != 2 || current.BusinessStatus != "frozen" || current.GenerationID != activated.GenerationID {
		t.Fatal("wrong Snapshot projection", err)
	}
	pipeline, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer})
	if err != nil || pipeline.HeartbeatID.Valid || pipeline.AppliedSequence != 2 {
		t.Fatal("Snapshot reused old freshness", err)
	}
	// A revoked Page grant forbids all local rebuild progress. Restoration is a
	// new version, so the old cut is abandoned, not silently adopted.
	c2, p2 := cursor(epoch, 2, 2, "frozen")
	if _, err = repository.Begin(ctx, c2); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Exec(ctx, "UPDATE workload_grants SET status='revoked',version=version+1,updated_at=clock_timestamp() WHERE id=$1", grants[governancev1.SnapshotPageOperation]); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.LoadPage(ctx, c2, p2); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatal("Snapshot ignored Page grant revocation", err)
	}
	if _, err = migrator.Exec(ctx, "UPDATE workload_grants SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1", grants[governancev1.SnapshotPageOperation]); err != nil {
		t.Fatal(err)
	}
	changed, err := repository.LoadPage(ctx, c2, p2)
	if err != nil || changed.State != "abandoned" {
		t.Fatal("Snapshot reused restored authority", err)
	}
	// Reconstruct a repository to resume persisted progress without process-local state.
	repository, err = NewGovernanceSnapshotRecoveryRepository(broker.data, sourceConfig)
	if err != nil {
		t.Fatal(err)
	}
	newEpoch := id()
	c3, p3 := cursor(newEpoch, 7, 3, "active")
	if _, err = repository.Begin(ctx, c3); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.LoadPage(ctx, c3, p3); err != nil {
		t.Fatal(err)
	}
	receiveLife(newEpoch, 8, 4, 21, "disabled")
	if _, err = repository.CatchUp(ctx, c3.ID); err != nil {
		t.Fatal(err)
	}
	next, err := repository.Activate(ctx, c3.ID)
	if err != nil || next.State != "activated" {
		t.Fatal("new epoch Snapshot failed", err)
	}
	pipeline, err = q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer})
	if err != nil || !pipeline.Epoch.Valid || uuid.UUID(pipeline.Epoch.Bytes) != newEpoch || pipeline.AppliedSequence != 8 || pipeline.HeartbeatID.Valid {
		t.Fatal("new epoch continuity/freshness", err)
	}
	current, err = q.ReadTenantLifecycleProjection(ctx, sqlcgen.ReadTenantLifecycleProjectionParams{Producer: producer, TenantID: tenant})
	if err != nil || current.TenantVersion != 4 || current.BusinessStatus != "disabled" {
		t.Fatal("new epoch increment omitted", err)
	}
	// Snapshot cannot drop an existing Tenant or revive a terminal one.
	invalid, p4 := cursor(newEpoch, 8, 5, "active")
	if _, err = repository.Begin(ctx, invalid); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.LoadPage(ctx, invalid, p4); err != nil {
		t.Fatal(err)
	}
	result, err := repository.Activate(ctx, invalid.ID)
	if err != nil || result.State != "abandoned" {
		t.Fatal("Snapshot revived disabled Tenant", err)
	}
	var identities int
	if err = runtime.QueryRow(ctx, "SELECT count(*) FROM tenant_memberships").Scan(&identities); err != nil || identities != 0 {
		t.Fatal("Snapshot created identities", err)
	}
	if _, err = runtime.Exec(ctx, "UPDATE tenant_snapshot_pages SET item_count=0 WHERE producer=$1", producer); err == nil {
		t.Fatal("Snapshot page receipt mutable")
	}
	t.Log("real PG Snapshot reader/cut, immutable pages, replay, current Begin/Page grant pair, restored-authority fence, atomic generation/catch-up, new epoch, terminal state and no identity effects passed")
}
