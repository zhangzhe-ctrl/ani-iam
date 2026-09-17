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

type coreLifecycleProjectionRepository struct {
	data     *Data
	producer string
}

// Producer comes from explicit receiver configuration, never from an incoming
// message. Configuration does not replace the pending transport authority gate.
func NewCoreLifecycleProjectionRepository(d *Data, producer string) (biz.CoreLifecycleProjectionRepository, error) {
	if d == nil || d.pool == nil || !biz.ValidCoreProjectionProducer(producer) {
		return nil, biz.ErrCoreProjectionInvalid
	}
	return &coreLifecycleProjectionRepository{data: d, producer: producer}, nil
}

func (r *coreLifecycleProjectionRepository) InitializeShadow(ctx context.Context) error {
	id, err := uuid.NewV7()
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapPostgresError("begin shadow generation", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	n, err := q.InitializeCoreProjectionPipeline(ctx, sqlcgen.InitializeCoreProjectionPipelineParams{Producer: r.producer, GenerationID: id})
	if err != nil {
		return mapPostgresError("initialize shadow pipeline", err, nil)
	}
	if n == 1 {
		if err = q.InitializeCoreProjectionGeneration(ctx, sqlcgen.InitializeCoreProjectionGenerationParams{Producer: r.producer, ID: id}); err != nil {
			return mapPostgresError("initialize shadow generation", err, nil)
		}
	}
	return mapPostgresError("commit shadow initialization", tx.Commit(ctx), nil)
}

func (r *coreLifecycleProjectionRepository) Apply(ctx context.Context, m biz.CoreProjectionMessage) (biz.CoreProjectionReceipt, error) {
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.CoreProjectionReceipt{}, mapPostgresError("begin Core projection", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	result, err := applyCoreProjection(ctx, sqlcgen.New(tx), r.producer, m)
	if err != nil {
		return biz.CoreProjectionReceipt{}, err
	}
	return result, mapPostgresError("commit Core projection and receipt", tx.Commit(ctx), nil)
}

func applyCoreProjection(ctx context.Context, q *sqlcgen.Queries, producer string, m biz.CoreProjectionMessage) (biz.CoreProjectionReceipt, error) {
	var result biz.CoreProjectionReceipt
	if err := m.Validate(); err != nil {
		return result, err
	}
	if m.Producer != producer {
		return result, biz.ErrCoreProjectionConflict
	}
	pipeline, err := q.LockCoreProjectionPipeline(ctx, sqlcgen.LockCoreProjectionPipelineParams{Producer: producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("lock Core projection pipeline", err, nil)
	}
	result.ContiguousSequence, result.HighestSequence = pipeline.ContiguousSequence, pipeline.HighestSequence
	fingerprint := coreProjectionFingerprint(m)
	rawHash := sha256.Sum256(m.RawPayload)
	previous, err := q.GetCoreIntegrationReceipt(ctx, sqlcgen.GetCoreIntegrationReceiptParams{Producer: producer, EventID: m.EventID})
	if err == nil {
		if previous.SourceSequence != m.SourceSequence || !bytes.Equal(previous.RawPayload, m.RawPayload) || !bytes.Equal(previous.DomainFingerprint, fingerprint[:]) {
			return result, biz.ErrCoreProjectionConflict
		}
		result.Duplicate = true
		result.Outcome = previous.Outcome
		// Exact redelivery is not new pipeline progress and cannot extend freshness.
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read Core source receipt", err, nil)
	}
	result.Outcome = m.Kind
	if m.Kind == "lifecycle" {
		cut, cutErr := q.ReadCoreSnapshotCut(ctx, sqlcgen.ReadCoreSnapshotCutParams{Producer: producer, GenerationID: pipeline.GenerationID})
		if cutErr != nil {
			return result, mapPostgresError("read active Snapshot cut", cutErr, nil)
		}
		result.Outcome, err = projectCoreMessage(ctx, q, producer, pipeline.GenerationID, m, cut)
		if err != nil {
			return result, err
		}
	}

	tenant := pgtype.UUID{}
	if m.TenantID != uuid.Nil {
		tenant = requiredPGUUID(m.TenantID)
	}
	version := pgtype.Int8{}
	status, reason := pgtype.Text{}, pgtype.Text{}
	effective := pgtype.Timestamptz{}
	if m.Kind == "lifecycle" {
		version = pgtype.Int8{Int64: m.LifecycleVersion, Valid: true}
		status = pgtype.Text{String: m.Status, Valid: true}
		reason = pgtype.Text{String: m.Reason, Valid: true}
		effective = requiredTimestamptz(m.EffectiveAt)
	}
	err = q.AppendCoreIntegrationReceipt(ctx, sqlcgen.AppendCoreIntegrationReceiptParams{Producer: producer, EventID: m.EventID, SourceSequence: m.SourceSequence, Kind: m.Kind, TenantID: tenant, RawPayload: m.RawPayload, RawSha256: rawHash[:], DomainFingerprint: fingerprint[:], OccurredAt: requiredTimestamptz(m.OccurredAt), Outcome: result.Outcome, LifecycleVersion: version, LifecycleStatus: status, LifecycleReason: reason, LifecycleEffectiveAt: effective})
	if err != nil {
		return result, mapPostgresError("append immutable Core source receipt", err, biz.ErrCoreProjectionConflict)
	}
	progress, err := q.AdvanceCoreProjectionPipeline(ctx, sqlcgen.AdvanceCoreProjectionPipelineParams{Producer: producer, SourceSequence: m.SourceSequence, OccurredAt: requiredTimestamptz(m.OccurredAt)})
	if err != nil {
		return result, mapPostgresError("advance contiguous Core source progress", err, nil)
	}
	result.ContiguousSequence, result.HighestSequence = progress.ContiguousSequence, progress.HighestSequence
	return result, nil
}

func coreTenantProjection(row sqlcgen.CoreLifecycleProjectionRow) biz.CoreTenantProjection {
	s := biz.CoreTenantProjection{Version: row.LifecycleVersion, RequiredVersion: row.RequiredVersion, Status: row.Status, Reason: row.Reason, RepairRequired: row.RepairRequired, SnapshotBase: row.SnapshotBase}
	if row.EffectiveAt.Valid {
		s.EffectiveAt = row.EffectiveAt.Time
	}
	return s
}

func (r *coreLifecycleProjectionRepository) ReadShadowTenant(ctx context.Context, scope biz.TenantScope) (biz.CoreShadowTenant, error) {
	var result biz.CoreShadowTenant
	tenant, err := scope.TenantID()
	if err != nil {
		return result, err
	}
	row, err := sqlcgen.New(r.data.pool).ReadCoreShadowTenant(ctx, sqlcgen.ReadCoreShadowTenantParams{Producer: r.producer, TenantID: tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("read shadow Tenant and pipeline health", err, nil)
	}
	result.CoreTenantProjection = biz.CoreTenantProjection{Version: row.LifecycleVersion, RequiredVersion: row.RequiredVersion, Status: row.Status, Reason: row.Reason, RepairRequired: row.RepairRequired, SnapshotBase: row.SnapshotBase}
	if row.EffectiveAt.Valid {
		result.EffectiveAt = row.EffectiveAt.Time
	}
	result.GenerationID = row.GenerationID
	result.ContiguousSequence = row.ContiguousSequence
	result.HighestSequence = row.HighestSequence
	result.Fresh = row.Fresh
	return result, nil
}

func coreProjectionFingerprint(m biz.CoreProjectionMessage) [32]byte {
	m.RawPayload = nil
	m.OccurredAt = m.OccurredAt.UTC().Truncate(time.Microsecond)
	m.EffectiveAt = m.EffectiveAt.UTC().Truncate(time.Microsecond)
	encoded, _ := json.Marshal(m)
	return sha256.Sum256(encoded)
}
