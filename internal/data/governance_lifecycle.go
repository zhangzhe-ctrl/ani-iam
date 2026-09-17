package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

// applyTenantIntegration is an internal transaction step. The broker receiver
// must hold the current authority lock and check the exact route/producer before
// calling it, then append authority evidence before committing and acknowledging.
// Snapshot catch-up uses the same projector, never a fabricated delivery.
func applyTenantIntegration(ctx context.Context, q *sqlcgen.Queries, producer string, d biz.TenantBrokerDelivery) (biz.TenantBrokerReceipt, error) {
	var result biz.TenantBrokerReceipt
	if d.Validate() != nil || d.Producer != producer {
		return result, biz.ErrTenantLifecycleInvalid
	}
	row, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("lock Tenant lifecycle pipeline", err, nil)
	}
	pipeline := tenantLifecyclePipeline(row)
	result.Applied, result.Highest = pipeline.Applied, pipeline.Highest
	fingerprint := tenantIntegrationFingerprint(d)
	rawHash := sha256.Sum256(d.RawPayload)
	prior, err := q.ReadTenantIntegrationReceipt(ctx, sqlcgen.ReadTenantIntegrationReceiptParams{Producer: producer, EventID: d.EventID})
	if err == nil {
		if prior.Epoch != d.Epoch || prior.Kind != d.Kind || prior.SourceSequence != d.Sequence || !bytes.Equal(prior.RawPayload, d.RawPayload) || !bytes.Equal(prior.DomainSha256, fingerprint[:]) {
			return result, biz.ErrTenantLifecycleConflict
		}
		result.Duplicate, result.Outcome = true, prior.Outcome
		// A redelivery is still subject to the caller's current Grant gate, but
		// it cannot refresh heartbeat or mutate an already retained fact.
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read Tenant immutable receipt", err, nil)
	}
	record := sqlcgen.AppendTenantIntegrationReceiptParams{Producer: producer, EventID: d.EventID, Epoch: d.Epoch, Kind: d.Kind, SourceSequence: d.Sequence, RawPayload: d.RawPayload, RawSha256: rawHash[:], DomainSha256: fingerprint[:], OccurredAt: tenantFactTime(d.OccurredAt)}
	if d.TenantID != uuid.Nil {
		record.TenantID = requiredPGUUID(d.TenantID)
	}
	switch d.Kind {
	case "lifecycle":
		fact := biz.TenantLifecycleFact{}
		if row.GenerationID.Valid {
			existing, readErr := q.ReadTenantLifecycleFact(ctx, sqlcgen.ReadTenantLifecycleFactParams{Producer: producer, GenerationID: row.GenerationID.Bytes, TenantID: d.TenantID})
			if readErr == nil {
				fact = tenantLifecycleFact(existing)
			} else if !errors.Is(readErr, pgx.ErrNoRows) {
				return result, mapPostgresError("read Tenant lifecycle fact", readErr, nil)
			}
		}
		update, projectErr := biz.ProjectTenantLifecycle(pipeline, fact, *d.Lifecycle)
		if projectErr != nil {
			return result, projectErr
		}
		pipeline, result.Outcome = update.Pipeline, update.Outcome
		if result.Outcome == "applied" {
			if !row.GenerationID.Valid {
				return result, biz.ErrTenantLifecycleInvalid
			}
			if err := q.WriteTenantLifecycleFact(ctx, sqlcgen.WriteTenantLifecycleFactParams{Producer: producer, GenerationID: row.GenerationID.Bytes, TenantID: d.TenantID, TenantVersion: update.Fact.Version, BusinessStatus: update.Fact.Status, Reason: update.Fact.Reason, EffectiveAt: tenantFactTime(update.Fact.EffectiveAt), SnapshotBase: false}); err != nil {
				return result, mapPostgresError("project Tenant lifecycle fact", err, biz.ErrTenantLifecycleConflict)
			}
		}
		record.TenantVersion = pgtype.Int8{Int64: d.Lifecycle.TenantVersion, Valid: true}
		record.BusinessStatus = pgtype.Text{String: d.Lifecycle.Status, Valid: true}
		record.Reason = pgtype.Text{String: d.Lifecycle.Reason, Valid: true}
		record.EffectiveAt = tenantFactTime(d.Lifecycle.EffectiveAt)
	case "heartbeat":
		clock, clockErr := q.ReadTenantLifecycleClock(ctx)
		if clockErr != nil {
			return result, mapPostgresError("read heartbeat receiver time", clockErr, nil)
		}
		pipeline, result.Outcome, err = biz.ObserveTenantHeartbeat(pipeline, *d.Heartbeat, clock.Time)
		if err != nil {
			return result, err
		}
		record.CommittedSequence = pgtype.Int8{Int64: d.Heartbeat.Committed, Valid: true}
		record.PublishedSequence = pgtype.Int8{Int64: d.Heartbeat.Published, Valid: true}
	case "bootstrap":
		// The existing Bootstrap receipt and operation must already be retained
		// in this UoW. The SQL binding trigger checks tenant, event, intent and
		// original bytes. No Lifecycle slot or freshness is advanced here.
		record.BootstrapOperationID = requiredPGUUID(d.Bootstrap.Intent.OperationID)
		result.Outcome = "bootstrap"
	}
	record.Outcome = result.Outcome
	if err := q.AppendTenantIntegrationReceipt(ctx, record); err != nil {
		return result, mapPostgresError("append Tenant integration receipt", err, biz.ErrTenantLifecycleConflict)
	}
	if err := writeTenantLifecyclePipeline(ctx, q, producer, pipeline); err != nil {
		return result, err
	}
	result.Applied, result.Highest = pipeline.Applied, pipeline.Highest
	return result, nil
}

func tenantLifecyclePipeline(row sqlcgen.TenantLifecyclePipeline) biz.TenantLifecyclePipeline {
	result := biz.TenantLifecyclePipeline{Applied: row.AppliedSequence, Highest: row.HighestSequence, SnapshotRequired: row.SnapshotRequired}
	if row.Epoch.Valid {
		result.Epoch = row.Epoch.Bytes
	}
	if row.HeartbeatID.Valid {
		result.Heartbeat = biz.TenantLifecycleHeartbeat{ID: row.HeartbeatID.Bytes, Epoch: row.HeartbeatEpoch.Bytes, Committed: row.CommittedSequence, Published: row.PublishedSequence, ObservedAt: row.HeartbeatObservedAt.Time}
		result.HeartbeatReceivedAt = row.HeartbeatReceivedAt.Time
	}
	return result
}

func tenantLifecycleFact(row sqlcgen.TenantLifecycleFact) biz.TenantLifecycleFact {
	result := biz.TenantLifecycleFact{Version: row.TenantVersion, Status: row.BusinessStatus, Reason: row.Reason, SnapshotBase: row.SnapshotBase}
	if row.EffectiveAt.Valid {
		result.EffectiveAt = row.EffectiveAt.Time
	}
	return result
}

func writeTenantLifecyclePipeline(ctx context.Context, q *sqlcgen.Queries, producer string, p biz.TenantLifecyclePipeline) error {
	update := sqlcgen.AdvanceTenantLifecyclePipelineParams{Producer: producer, AppliedSequence: p.Applied, HighestSequence: p.Highest, SnapshotRequired: p.SnapshotRequired, CommittedSequence: p.Heartbeat.Committed, PublishedSequence: p.Heartbeat.Published}
	if p.Heartbeat.ID != uuid.Nil {
		update.HeartbeatID = requiredPGUUID(p.Heartbeat.ID)
		update.HeartbeatEpoch = requiredPGUUID(p.Heartbeat.Epoch)
		update.HeartbeatObservedAt = tenantFactTime(p.Heartbeat.ObservedAt)
		update.HeartbeatReceivedAt = tenantFactTime(p.HeartbeatReceivedAt)
	}
	return mapPostgresError("advance Tenant lifecycle continuity", q.AdvanceTenantLifecyclePipeline(ctx, update), nil)
}

func tenantFactTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	// PostgreSQL parses RFC3339Nano at microsecond precision by rounding. The
	// original nanosecond bytes remain immutable in the receipt.
	return requiredTimestamptz(t.UTC().Round(time.Microsecond))
}

func tenantIntegrationFingerprint(d biz.TenantBrokerDelivery) [32]byte {
	d.RawPayload = nil
	if d.Bootstrap != nil {
		copy := *d.Bootstrap
		copy.RawPayload = nil
		d.Bootstrap = &copy
	}
	raw, _ := json.Marshal(d)
	return sha256.Sum256(raw)
}
