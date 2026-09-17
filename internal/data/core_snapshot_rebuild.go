package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type coreSnapshotRebuildRepository struct {
	data     *Data
	producer string
	broker   *coreBrokerRepository
}

func NewCoreSnapshotRebuildRepository(d *Data, producer string) (biz.CoreSnapshotRebuildRepository, error) {
	if d == nil || d.pool == nil || !biz.ValidCoreProjectionProducer(producer) {
		return nil, biz.ErrCoreProjectionInvalid
	}
	return &coreSnapshotRebuildRepository{data: d, producer: producer}, nil
}

// Formal broker recovery uses the same authority lock order and receipt
// version fences as live processing. The component constructor remains useful
// for isolated projection tests, but is not wired into the formal process.
func NewCoreBrokerSnapshotRecoveryRepository(d *Data, c CoreBrokerConfiguration) (biz.CoreSnapshotRecoveryRepository, error) {
	broker, err := newCoreBrokerRepository(d, c)
	if err != nil {
		return nil, err
	}
	return &coreSnapshotRebuildRepository{data: d, producer: c.Producer, broker: broker}, nil
}
func (r *coreSnapshotRebuildRepository) checkBroker(ctx context.Context, q *sqlcgen.Queries) (coreBrokerAuthority, error) {
	if r.broker == nil {
		return coreBrokerAuthority{}, nil
	}
	route, err := r.broker.config.route(governancev1.LifecycleSubject)
	if err != nil {
		return coreBrokerAuthority{}, err
	}
	return r.broker.current(ctx, q, route)
}
func (r *coreSnapshotRebuildRepository) authorityFingerprint(a coreBrokerAuthority) string {
	if r.broker == nil {
		return ""
	}
	return a.fingerprint()
}
func (r *coreSnapshotRebuildRepository) NextRecovery(ctx context.Context) (biz.CoreSnapshotRecovery, error) {
	var result biz.CoreSnapshotRecovery
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, mapPostgresError("begin Snapshot recovery scan", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	authority, err := r.checkBroker(ctx, q)
	if err != nil {
		return result, err
	}
	pipeline, err := q.LockCoreProjectionPipeline(ctx, sqlcgen.LockCoreProjectionPipelineParams{Producer: r.producer})
	if err != nil {
		return result, mapPostgresError("lock Snapshot recovery pipeline", err, biz.ErrCoreProjectionMissing)
	}
	if err = q.AbandonUnavailableCoreSnapshots(ctx, sqlcgen.AbandonUnavailableCoreSnapshotsParams{Producer: r.producer, GenerationID: pipeline.GenerationID, CurrentAuthoritySha256: r.authorityFingerprint(authority)}); err != nil {
		return result, mapPostgresError("retain abandoned Snapshot cuts", err, nil)
	}
	b, err := q.ReadPendingCoreSnapshot(ctx, sqlcgen.ReadPendingCoreSnapshotParams{Producer: r.producer})
	if err == nil {
		result.Needed = true
		result.Cursor = biz.CoreSnapshotCursor{ID: b.SnapshotID, ConsumerID: b.ConsumerID, Producer: b.Producer, SourceCut: b.SourceCut, BrokerAfter: b.BrokerAfter, PageSize: b.PageSize, ExpiresAt: b.ExpiresAt.Time}
		result.Build = biz.CoreSnapshotBuild{GenerationID: b.GenerationID, State: b.State, LoadedItems: b.LoadedItems, AppliedThrough: b.AppliedThrough, NextToken: b.NextToken}
	} else if errors.Is(err, pgx.ErrNoRows) {
		var needed pgtype.Bool
		needed, err = q.CoreSnapshotRecoveryNeeded(ctx, sqlcgen.CoreSnapshotRecoveryNeededParams{Producer: r.producer, GenerationID: pipeline.GenerationID})
		if err == nil && !needed.Valid {
			return result, biz.ErrCoreProjectionConflict
		}
		result.Needed = needed.Bool
	}
	if err != nil {
		return result, mapPostgresError("read Snapshot recovery state", err, nil)
	}
	return result, mapPostgresError("commit Snapshot recovery scan", tx.Commit(ctx), nil)
}
func snapshotBuild(b sqlcgen.LockCoreSnapshotRebuildRow) biz.CoreSnapshotBuild {
	return biz.CoreSnapshotBuild{GenerationID: b.GenerationID, State: b.State, LoadedItems: b.LoadedItems, AppliedThrough: b.AppliedThrough, NextToken: b.NextToken}
}
func sameSnapshotCursor(c biz.CoreSnapshotCursor, b sqlcgen.LockCoreSnapshotRebuildRow) bool {
	return c.ID == b.SnapshotID && c.Producer == b.Producer && c.ConsumerID == b.ConsumerID && c.SourceCut == b.SourceCut && c.BrokerAfter == b.BrokerAfter && c.PageSize == b.PageSize && c.ExpiresAt.UTC().Truncate(time.Microsecond).Equal(b.ExpiresAt.Time)
}
func (r *coreSnapshotRebuildRepository) Begin(ctx context.Context, c biz.CoreSnapshotCursor) (biz.CoreSnapshotBuild, error) {
	var result biz.CoreSnapshotBuild
	if c.Validate() != nil || c.Producer != r.producer {
		return result, biz.ErrCoreProjectionInvalid
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, mapPostgresError("begin Snapshot rebuild", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	authority, err := r.checkBroker(ctx, q)
	if err != nil {
		return result, err
	}
	pipeline, err := q.LockCoreProjectionPipeline(ctx, sqlcgen.LockCoreProjectionPipelineParams{Producer: r.producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("lock Snapshot pipeline", err, nil)
	}
	key := sqlcgen.LockCoreSnapshotRebuildParams{Producer: r.producer, SnapshotID: c.ID}
	b, err := q.LockCoreSnapshotRebuild(ctx, key)
	if err == nil {
		if !sameSnapshotCursor(c, b) || b.State == "abandoned" {
			return result, biz.ErrCoreProjectionConflict
		}
		return snapshotBuild(b), mapPostgresError("commit Snapshot resume", tx.Commit(ctx), nil)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read Snapshot rebuild", err, nil)
	}
	if r.broker != nil {
		if c.ConsumerID != r.broker.config.ConsumerID {
			return result, biz.ErrCoreBrokerAuthority
		}
		_, pendingErr := q.ReadPendingCoreSnapshot(ctx, sqlcgen.ReadPendingCoreSnapshotParams{Producer: r.producer})
		if pendingErr == nil {
			return result, biz.ErrCoreProjectionConflict
		}
		if !errors.Is(pendingErr, pgx.ErrNoRows) {
			return result, mapPostgresError("check pending Snapshot", pendingErr, nil)
		}
	}
	fresh, err := q.CoreSnapshotCursorUnexpired(ctx, sqlcgen.CoreSnapshotCursorUnexpiredParams{ExpiresAt: requiredTimestamptz(c.ExpiresAt)})
	if err != nil {
		return result, mapPostgresError("check Snapshot expiry", err, nil)
	}
	if !fresh {
		return result, biz.ErrCoreProjectionInvalid
	}
	currentCut, err := q.ReadCoreSnapshotCut(ctx, sqlcgen.ReadCoreSnapshotCutParams{Producer: r.producer, GenerationID: pipeline.GenerationID})
	if err != nil {
		return result, mapPostgresError("read current Snapshot cut", err, nil)
	}
	if c.SourceCut < currentCut {
		return result, biz.ErrCoreProjectionConflict
	}
	id, err := uuid.NewV7()
	if err != nil {
		return result, biz.ErrPersistenceUnavailable
	}
	if err = q.BeginCoreSnapshotGeneration(ctx, sqlcgen.BeginCoreSnapshotGenerationParams{Producer: r.producer, GenerationID: id, SourceCut: c.SourceCut}); err != nil {
		return result, mapPostgresError("create Snapshot generation", err, nil)
	}
	if err = q.BeginCoreSnapshotRebuild(ctx, sqlcgen.BeginCoreSnapshotRebuildParams{Producer: r.producer, SnapshotID: c.ID, GenerationID: id, BaseGenerationID: pipeline.GenerationID, ConsumerID: c.ConsumerID, SourceCut: c.SourceCut, BrokerAfter: c.BrokerAfter, PageSize: c.PageSize, ExpiresAt: requiredTimestamptz(c.ExpiresAt), BrokerAuthoritySha256: r.authorityFingerprint(authority)}); err != nil {
		return result, mapPostgresError("create Snapshot rebuild", err, nil)
	}
	b, err = q.LockCoreSnapshotRebuild(ctx, key)
	if err != nil {
		return result, mapPostgresError("read created Snapshot rebuild", err, nil)
	}
	return snapshotBuild(b), mapPostgresError("commit Snapshot begin", tx.Commit(ctx), nil)
}
func (r *coreSnapshotRebuildRepository) withBuild(ctx context.Context, id uuid.UUID, fn func(*sqlcgen.Queries, sqlcgen.CoreLifecyclePipeline, sqlcgen.LockCoreSnapshotRebuildRow) error) (biz.CoreSnapshotBuild, error) {
	var result biz.CoreSnapshotBuild
	if id == uuid.Nil {
		return result, biz.ErrCoreProjectionInvalid
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, mapPostgresError("begin Snapshot operation", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	authority, err := r.checkBroker(ctx, q)
	if err != nil {
		return result, err
	}
	pipeline, err := q.LockCoreProjectionPipeline(ctx, sqlcgen.LockCoreProjectionPipelineParams{Producer: r.producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("lock Snapshot pipeline", err, nil)
	}
	key := sqlcgen.LockCoreSnapshotRebuildParams{Producer: r.producer, SnapshotID: id}
	b, err := q.LockCoreSnapshotRebuild(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreProjectionMissing
	}
	if err != nil {
		return result, mapPostgresError("lock Snapshot rebuild", err, nil)
	}
	if b.State == "abandoned" || (b.BaseGenerationID != pipeline.GenerationID && b.GenerationID != pipeline.GenerationID) {
		return result, biz.ErrCoreProjectionConflict
	}
	if r.broker != nil && b.State != "activated" && b.BrokerAuthoritySha256 != authority.fingerprint() {
		if err = q.AbandonCoreSnapshotAuthority(ctx, sqlcgen.AbandonCoreSnapshotAuthorityParams{Producer: r.producer, SnapshotID: id}); err != nil {
			return result, mapPostgresError("retain changed Snapshot authority", err, nil)
		}
		b, err = q.LockCoreSnapshotRebuild(ctx, key)
		if err != nil {
			return result, mapPostgresError("read abandoned authority cut", err, nil)
		}
		return snapshotBuild(b), mapPostgresError("commit abandoned authority cut", tx.Commit(ctx), nil)
	}
	if err = fn(q, pipeline, b); err != nil {
		return result, err
	}
	b, err = q.LockCoreSnapshotRebuild(ctx, key)
	if err != nil {
		return result, mapPostgresError("read Snapshot result", err, nil)
	}
	return snapshotBuild(b), mapPostgresError("commit Snapshot operation", tx.Commit(ctx), nil)
}
func (r *coreSnapshotRebuildRepository) LoadPage(ctx context.Context, p biz.CoreSnapshotPage) (biz.CoreSnapshotBuild, error) {
	if p.Validate() != nil || p.Cursor.Producer != r.producer {
		return biz.CoreSnapshotBuild{}, biz.ErrCoreProjectionInvalid
	}
	p.Cursor.ExpiresAt = p.Cursor.ExpiresAt.UTC().Truncate(time.Microsecond)
	encoded, _ := json.Marshal(p)
	fp := sha256.Sum256(encoded)
	return r.withBuild(ctx, p.Cursor.ID, func(q *sqlcgen.Queries, _ sqlcgen.CoreLifecyclePipeline, b sqlcgen.LockCoreSnapshotRebuildRow) error {
		if !sameSnapshotCursor(p.Cursor, b) {
			return biz.ErrCoreProjectionConflict
		}
		previous, err := q.GetCoreSnapshotPageReceipt(ctx, sqlcgen.GetCoreSnapshotPageReceiptParams{Producer: r.producer, SnapshotID: p.Cursor.ID, RequestToken: p.RequestToken})
		if err == nil {
			if !bytes.Equal(previous, fp[:]) {
				return biz.ErrCoreProjectionConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return mapPostgresError("read Snapshot page receipt", err, nil)
		}
		if b.State != "loading" || !b.Unexpired || b.NextToken != p.RequestToken {
			return biz.ErrCoreProjectionConflict
		}
		if len(p.Items) == 0 && (b.LoadedItems != 0 || p.RequestToken != "") {
			return biz.ErrCoreProjectionInvalid
		}
		if p.NextToken != "" {
			_, err = q.GetCoreSnapshotPageReceipt(ctx, sqlcgen.GetCoreSnapshotPageReceiptParams{Producer: r.producer, SnapshotID: p.Cursor.ID, RequestToken: p.NextToken})
			if err == nil {
				return biz.ErrCoreProjectionConflict
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return mapPostgresError("check Snapshot page cycle", err, nil)
			}
		}
		last := b.LastTenantID
		for _, f := range p.Items {
			if last.Valid && f.TenantID.String() <= uuid.UUID(last.Bytes).String() {
				return biz.ErrCoreProjectionConflict
			}
			if err = q.InsertCoreSnapshotFact(ctx, sqlcgen.InsertCoreSnapshotFactParams{Producer: r.producer, GenerationID: b.GenerationID, TenantID: f.TenantID, Version: f.Version, Status: f.Status}); err != nil {
				return mapPostgresError("load Snapshot Tenant fact", err, biz.ErrCoreProjectionConflict)
			}
			last = requiredPGUUID(f.TenantID)
		}
		if err = q.RecordCoreSnapshotPage(ctx, sqlcgen.RecordCoreSnapshotPageParams{Producer: r.producer, SnapshotID: p.Cursor.ID, RequestToken: p.RequestToken, PageFingerprint: fp[:], NextToken: p.NextToken, ItemCount: int32(len(p.Items))}); err != nil {
			return mapPostgresError("record immutable Snapshot page", err, biz.ErrCoreProjectionConflict)
		}
		return mapPostgresError("advance Snapshot page", q.AdvanceCoreSnapshotPage(ctx, sqlcgen.AdvanceCoreSnapshotPageParams{Producer: r.producer, SnapshotID: p.Cursor.ID, NextToken: p.NextToken, LastTenantID: last, ItemCount: int64(len(p.Items))}), nil)
	})
}
func (r *coreSnapshotRebuildRepository) CatchUp(ctx context.Context, id uuid.UUID) (biz.CoreSnapshotBuild, error) {
	return r.withBuild(ctx, id, func(q *sqlcgen.Queries, p sqlcgen.CoreLifecyclePipeline, b sqlcgen.LockCoreSnapshotRebuildRow) error {
		if b.State == "activated" {
			return nil
		}
		if b.State == "loading" {
			return biz.ErrCoreSnapshotNotReady
		}
		through := max(b.SourceCut, p.HighestSequence)
		authority, err := r.checkBroker(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListCoreSnapshotIncrements(ctx, sqlcgen.ListCoreSnapshotIncrementsParams{Producer: r.producer, AfterSequence: b.AppliedThrough, ThroughSequence: through})
		if err != nil {
			return mapPostgresError("read buffered Lifecycle facts", err, nil)
		}
		for _, row := range rows {
			if !row.LifecycleVersion.Valid || !row.LifecycleStatus.Valid || !row.LifecycleReason.Valid || !row.LifecycleEffectiveAt.Valid || !row.TenantID.Valid {
				return biz.ErrCoreProjectionConflict
			}
			m := biz.CoreProjectionMessage{Producer: r.producer, Kind: row.Kind, EventID: row.EventID, TenantID: uuid.UUID(row.TenantID.Bytes), SourceSequence: row.SourceSequence, OccurredAt: row.OccurredAt.Time, RawPayload: row.RawPayload, LifecycleVersion: row.LifecycleVersion.Int64, Status: row.LifecycleStatus.String, Reason: row.LifecycleReason.String, EffectiveAt: row.LifecycleEffectiveAt.Time}
			fp := coreProjectionFingerprint(m)
			if !bytes.Equal(fp[:], row.DomainFingerprint) {
				return biz.ErrCoreProjectionConflict
			}
			if r.broker != nil {
				receipt, err := q.ReadCoreBrokerEventAuthority(ctx, sqlcgen.ReadCoreBrokerEventAuthorityParams{ConsumerID: r.broker.config.ConsumerID, EventID: m.EventID, TenantID: row.TenantID})
				if errors.Is(err, pgx.ErrNoRows) {
					return biz.ErrCoreBrokerAuthority
				}
				if err != nil {
					return mapPostgresError("read Snapshot increment authority", err, nil)
				}
				if receipt.SourceSequence != m.SourceSequence || !bytes.Equal(receipt.RawSha256, row.RawSha256) {
					return biz.ErrCoreBrokerAuthority
				}
				if !authority.matches(receipt) {
					return mapPostgresError("retain fenced Snapshot increment", q.AbandonCoreSnapshotAuthority(ctx, sqlcgen.AbandonCoreSnapshotAuthorityParams{Producer: r.producer, SnapshotID: id}), nil)
				}
			}
			outcome, err := projectCoreMessage(ctx, q, r.producer, b.GenerationID, m, b.SourceCut)
			if err != nil {
				return err
			}
			if outcome == "version_gap" || outcome == "awaiting_snapshot" {
				return biz.ErrCoreProjectionConflict
			}
		}
		if len(rows) == 500 {
			through = rows[len(rows)-1].SourceSequence
		}
		return mapPostgresError("advance Snapshot replay", q.AdvanceCoreSnapshotReplay(ctx, sqlcgen.AdvanceCoreSnapshotReplayParams{Producer: r.producer, SnapshotID: id, AppliedThrough: through}), nil)
	})
}
func (r *coreSnapshotRebuildRepository) ActivateShadow(ctx context.Context, id uuid.UUID) (biz.CoreSnapshotBuild, error) {
	return r.withBuild(ctx, id, func(q *sqlcgen.Queries, p sqlcgen.CoreLifecyclePipeline, b sqlcgen.LockCoreSnapshotRebuildRow) error {
		if b.State == "activated" {
			return nil
		}
		if b.State == "loading" || b.AppliedThrough < p.HighestSequence || p.ContiguousSequence < max(b.SourceCut, p.HighestSequence) {
			return biz.ErrCoreSnapshotNotReady
		}
		covered, err := q.CoreSnapshotCoversCurrent(ctx, sqlcgen.CoreSnapshotCoversCurrentParams{Producer: r.producer, OldGeneration: p.GenerationID, NewGeneration: b.GenerationID})
		if err != nil {
			return mapPostgresError("check Snapshot coverage", err, nil)
		}
		if !covered.Valid || !covered.Bool {
			return biz.ErrCoreProjectionConflict
		}
		if err = q.ActivateCoreSnapshotShadow(ctx, sqlcgen.ActivateCoreSnapshotShadowParams{Producer: r.producer, GenerationID: b.GenerationID, SourceCut: b.SourceCut}); err != nil {
			return mapPostgresError("switch shadow generation", err, nil)
		}
		return mapPostgresError("record Snapshot activation", q.MarkCoreSnapshotActivated(ctx, sqlcgen.MarkCoreSnapshotActivatedParams{Producer: r.producer, SnapshotID: id}), nil)
	})
}

// projectCoreMessage is shared by live receipts and bounded Snapshot catch-up.
// Catch-up applies existing facts; it never writes or replaces their receipts.
func projectCoreMessage(ctx context.Context, q *sqlcgen.Queries, producer string, generation uuid.UUID, m biz.CoreProjectionMessage, cut int64) (string, error) {
	if err := m.Validate(); err != nil || m.Kind != "lifecycle" {
		return "", biz.ErrCoreProjectionInvalid
	}
	row, err := q.GetCoreProjectionRow(ctx, sqlcgen.GetCoreProjectionRowParams{Producer: producer, GenerationID: generation, TenantID: m.TenantID})
	var state biz.CoreTenantProjection
	if err == nil {
		state = coreTenantProjection(row)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", mapPostgresError("read Core Tenant projection", err, nil)
	}
	if m.SourceSequence <= cut {
		if state.Version == 0 || m.LifecycleVersion > state.Version || (m.LifecycleVersion == state.Version && m.Status != state.Status) {
			return "", biz.ErrCoreProjectionConflict
		}
		return "snapshot_covered", nil
	}
	next, outcome, err := biz.ProjectCoreLifecycle(state, m)
	if err != nil {
		return "", err
	}
	if outcome == "applied" || outcome == "version_gap" || outcome == "awaiting_snapshot" {
		at := pgtype.Timestamptz{}
		if !next.EffectiveAt.IsZero() {
			at = requiredTimestamptz(next.EffectiveAt)
		}
		if err = q.SaveCoreProjectionRow(ctx, sqlcgen.SaveCoreProjectionRowParams{Producer: producer, GenerationID: generation, TenantID: m.TenantID, LifecycleVersion: next.Version, RequiredVersion: next.RequiredVersion, Status: next.Status, Reason: next.Reason, EffectiveAt: at, RepairRequired: next.RepairRequired, SnapshotBase: next.SnapshotBase}); err != nil {
			return "", mapPostgresError("save Core Tenant projection", err, nil)
		}
	}
	return outcome, nil
}
