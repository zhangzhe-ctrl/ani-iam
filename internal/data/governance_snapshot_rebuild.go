package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type GovernanceSnapshotRecoveryConfiguration struct {
	Broker   CoreBrokerConfiguration
	ReaderID uuid.UUID
	// ReaderIdentity is derived from the same validated TLS certificate loaded
	// by the Snapshot HTTP client; a config string alone is not peer evidence.
	ReaderIdentity string
	Registry       *workloadregistry.Registry
}
type governanceSnapshotRepository struct {
	data     *Data
	broker   *coreBrokerRepository
	reader   uuid.UUID
	peer     biz.VerifiedWorkloadPeer
	registry *workloadregistry.Registry
}

func NewGovernanceSnapshotRecoveryRepository(d *Data, c GovernanceSnapshotRecoveryConfiguration) (biz.TenantSnapshotRecoveryRepository, error) {
	broker, err := newCoreBrokerRepository(d, c.Broker)
	if err != nil || c.ReaderID.Version() != 7 || c.ReaderIdentity == "" || c.Registry == nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	for path, operation := range map[string]string{governancev1.SnapshotBeginPath: governancev1.SnapshotBeginOperation, governancev1.SnapshotPagePath: governancev1.SnapshotPageOperation} {
		target, ok := c.Registry.Lookup(governancev1.Audience, operation)
		if !ok || !target.Enabled || target.Mechanism != workloadregistry.WorkloadOnly || target.HTTPMethod != "POST" || target.HTTPPath != path {
			return nil, biz.ErrWorkloadPermissionDenied
		}
	}
	return &governanceSnapshotRepository{data: d, broker: broker, reader: c.ReaderID, peer: biz.VerifiedWorkloadPeer{Environment: c.Broker.Environment, TrustDomain: c.Broker.TrustDomain, IdentityKind: "x509_dns", IdentityValue: c.ReaderIdentity}, registry: c.Registry}, nil
}

type tenantSnapshotAuthority struct {
	broker      coreBrokerAuthority
	binding     sqlcgen.ResolveWorkloadIdentityRow
	fingerprint string
}

func (r *governanceSnapshotRepository) current(ctx context.Context, q *sqlcgen.Queries) (tenantSnapshotAuthority, error) {
	var result tenantSnapshotAuthority
	route, err := r.broker.config.route(governancev1.LifecycleSubject)
	if err != nil {
		return result, err
	}
	result.broker, err = r.broker.current(ctx, q, route)
	if err != nil {
		return result, err
	}
	result.binding, err = q.ResolveWorkloadIdentity(ctx, sqlcgen.ResolveWorkloadIdentityParams{Environment: r.peer.Environment, TrustDomain: r.peer.TrustDomain, IdentityKind: r.peer.IdentityKind, IdentityValue: r.peer.IdentityValue})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrWorkloadPermissionDenied
	}
	if err != nil {
		return result, mapPostgresError("read Snapshot current reader", err, nil)
	}
	if result.binding.PrincipalID != r.reader || r.reader != result.broker.receiver.PrincipalID {
		return result, biz.ErrWorkloadPermissionDenied
	}
	versions := []int64{}
	revisions := []string{}
	for _, operation := range []string{governancev1.SnapshotBeginOperation, governancev1.SnapshotPageOperation} {
		revision := r.registry.Revision(governancev1.Audience, operation)
		digest, _ := hex.DecodeString(revision)
		version, err := q.CheckWorkloadGrant(ctx, sqlcgen.CheckWorkloadGrantParams{PrincipalID: r.reader, BindingID: result.binding.BindingID, PrincipalVersion: result.binding.PrincipalVersion, BindingVersion: result.binding.BindingVersion, Environment: r.peer.Environment, TrustDomain: r.peer.TrustDomain, Audience: governancev1.Audience, Operation: operation, TargetSha256: digest})
		if errors.Is(err, pgx.ErrNoRows) {
			return result, biz.ErrWorkloadPermissionDenied
		}
		if err != nil {
			return result, mapPostgresError("check Snapshot current target pair", err, nil)
		}
		versions = append(versions, version)
		revisions = append(revisions, revision)
	}
	raw, _ := json.Marshal(struct {
		Broker    string
		Reader    sqlcgen.ResolveWorkloadIdentityRow
		Versions  []int64
		Revisions []string
	}{result.broker.fingerprint(), result.binding, versions, revisions})
	digest := sha256.Sum256(raw)
	result.fingerprint = hex.EncodeToString(digest[:])
	return result, nil
}
func tenantSnapshotBuild(row sqlcgen.TenantSnapshotRebuild) biz.TenantSnapshotBuild {
	return biz.TenantSnapshotBuild{Cursor: biz.TenantSnapshotCursor{ID: row.SnapshotID, Epoch: row.Epoch, ReaderID: row.ReaderID, Watermark: row.Watermark, TotalCount: row.TotalCount, PageSize: int(row.PageSize), CapturedAt: row.CapturedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(), FirstToken: row.FirstToken}, GenerationID: row.GenerationID, State: row.State, NextToken: row.NextToken, LoadedItems: row.LoadedItems, AppliedThrough: row.AppliedThrough}
}
func sameTenantSnapshotCursor(c biz.TenantSnapshotCursor, row sqlcgen.TenantSnapshotRebuild) bool {
	c.CapturedAt = c.CapturedAt.UTC().Round(time.Microsecond)
	c.ExpiresAt = c.ExpiresAt.UTC().Round(time.Microsecond)
	return c == tenantSnapshotBuild(row).Cursor
}
func (r *governanceSnapshotRepository) abandon(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.TenantSnapshotRebuild, reason string) error {
	return mapPostgresError("retain abandoned Tenant Snapshot", q.AbandonTenantSnapshot(ctx, sqlcgen.AbandonTenantSnapshotParams{Producer: row.Producer, SnapshotID: row.SnapshotID, Reason: reason}), nil)
}
func (r *governanceSnapshotRepository) unavailable(p sqlcgen.TenantLifecyclePipeline, b sqlcgen.TenantSnapshotRebuild, a tenantSnapshotAuthority, now time.Time) string {
	if b.BaseGenerationID != p.GenerationID {
		return "superseded_generation"
	}
	if b.AuthoritySha256 != a.fingerprint {
		return "authority_changed"
	}
	if !now.Before(b.ExpiresAt.Time) {
		return "cursor_expired"
	}
	return ""
}
func (r *governanceSnapshotRepository) NextRecovery(ctx context.Context) (biz.TenantSnapshotRecovery, error) {
	var result biz.TenantSnapshotRecovery
	tx, err := r.data.pool.Begin(ctx)
	if err != nil {
		return result, mapPostgresError("begin Tenant Snapshot scan", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	a, err := r.current(ctx, q)
	if err != nil {
		return result, err
	}
	if err = q.InitializeTenantLifecyclePipeline(ctx, sqlcgen.InitializeTenantLifecyclePipelineParams{Producer: r.broker.config.Producer}); err != nil {
		return result, mapPostgresError("initialize Snapshot pipeline", err, nil)
	}
	p, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: r.broker.config.Producer})
	if err != nil {
		return result, mapPostgresError("lock Snapshot scan", err, nil)
	}
	row, err := q.ReadPendingTenantSnapshot(ctx, sqlcgen.ReadPendingTenantSnapshotParams{Producer: r.broker.config.Producer})
	if err == nil {
		clock, e := q.ReadTenantLifecycleClock(ctx)
		if e != nil {
			return result, e
		}
		if reason := r.unavailable(p, row, a, clock.Time); reason != "" {
			if err = r.abandon(ctx, q, row, reason); err != nil {
				return result, err
			}
		} else {
			result.Needed = true
			result.Build = tenantSnapshotBuild(row)
			return result, mapPostgresError("commit Snapshot resume scan", tx.Commit(ctx), nil)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read pending Tenant Snapshot", err, nil)
	}
	result.Needed = !p.GenerationID.Valid || p.SnapshotRequired || p.HighestSequence > p.AppliedSequence
	return result, mapPostgresError("commit Tenant Snapshot scan", tx.Commit(ctx), nil)
}
func (r *governanceSnapshotRepository) Begin(ctx context.Context, c biz.TenantSnapshotCursor) (biz.TenantSnapshotBuild, error) {
	var result biz.TenantSnapshotBuild
	if c.Validate() != nil || c.ReaderID != r.reader {
		return result, biz.ErrTenantLifecycleInvalid
	}
	tx, err := r.data.pool.Begin(ctx)
	if err != nil {
		return result, mapPostgresError("begin Tenant Snapshot", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	a, err := r.current(ctx, q)
	if err != nil {
		return result, err
	}
	p, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: r.broker.config.Producer})
	if err != nil {
		return result, mapPostgresError("lock Snapshot base", err, biz.ErrCoreProjectionMissing)
	}
	clock, err := q.ReadTenantLifecycleClock(ctx)
	if err != nil {
		return result, err
	}
	if !clock.Time.Before(c.ExpiresAt) || c.CapturedAt.After(clock.Time) {
		return result, biz.ErrTenantSnapshotExpired
	}
	row, err := q.LockTenantSnapshot(ctx, sqlcgen.LockTenantSnapshotParams{Producer: r.broker.config.Producer, SnapshotID: c.ID})
	if err == nil {
		if !sameTenantSnapshotCursor(c, row) || row.AuthoritySha256 != a.fingerprint || row.State == "abandoned" {
			return result, biz.ErrTenantLifecycleConflict
		}
		return tenantSnapshotBuild(row), mapPostgresError("commit duplicate Snapshot begin", tx.Commit(ctx), nil)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read Snapshot begin", err, nil)
	}
	if p.Epoch.Valid && uuid.UUID(p.Epoch.Bytes) == c.Epoch && c.Watermark < p.AppliedSequence {
		return result, biz.ErrTenantLifecycleConflict
	}
	generation, err := uuid.NewV7()
	if err != nil {
		return result, biz.ErrPersistenceUnavailable
	}
	if err = q.CreateTenantLifecycleGeneration(ctx, sqlcgen.CreateTenantLifecycleGenerationParams{Producer: r.broker.config.Producer, ID: generation, Epoch: c.Epoch, SnapshotID: c.ID, SnapshotWatermark: c.Watermark, CapturedAt: tenantFactTime(c.CapturedAt)}); err != nil {
		return result, mapPostgresError("create Snapshot shadow generation", err, nil)
	}
	if err = q.BeginTenantSnapshot(ctx, sqlcgen.BeginTenantSnapshotParams{Producer: r.broker.config.Producer, SnapshotID: c.ID, GenerationID: generation, BaseGenerationID: p.GenerationID, Epoch: c.Epoch, ReaderID: r.reader, ReaderBindingID: a.binding.BindingID, Watermark: c.Watermark, TotalCount: c.TotalCount, PageSize: int32(c.PageSize), CapturedAt: tenantFactTime(c.CapturedAt), ExpiresAt: tenantFactTime(c.ExpiresAt), FirstToken: c.FirstToken, AuthoritySha256: a.fingerprint}); err != nil {
		return result, mapPostgresError("persist Tenant Snapshot cut", err, biz.ErrTenantLifecycleConflict)
	}
	row, err = q.LockTenantSnapshot(ctx, sqlcgen.LockTenantSnapshotParams{Producer: r.broker.config.Producer, SnapshotID: c.ID})
	if err != nil {
		return result, err
	}
	return tenantSnapshotBuild(row), mapPostgresError("commit Tenant Snapshot begin", tx.Commit(ctx), nil)
}
func (r *governanceSnapshotRepository) withBuild(ctx context.Context, id uuid.UUID, fn func(*sqlcgen.Queries, sqlcgen.TenantLifecyclePipeline, sqlcgen.TenantSnapshotRebuild, tenantSnapshotAuthority) error) (biz.TenantSnapshotBuild, error) {
	var result biz.TenantSnapshotBuild
	if id.Version() != 7 {
		return result, biz.ErrTenantLifecycleInvalid
	}
	tx, err := r.data.pool.Begin(ctx)
	if err != nil {
		return result, mapPostgresError("begin Snapshot progress", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	a, err := r.current(ctx, q)
	if err != nil {
		return result, err
	}
	p, err := q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: r.broker.config.Producer})
	if err != nil {
		return result, mapPostgresError("lock Snapshot progress", err, nil)
	}
	key := sqlcgen.LockTenantSnapshotParams{Producer: r.broker.config.Producer, SnapshotID: id}
	row, err := q.LockTenantSnapshot(ctx, key)
	if err != nil {
		return result, mapPostgresError("load durable Snapshot cut", err, biz.ErrCoreProjectionMissing)
	}
	if row.State == "abandoned" {
		return tenantSnapshotBuild(row), nil
	}
	clock, err := q.ReadTenantLifecycleClock(ctx)
	if err != nil {
		return result, err
	}
	if row.State != "activated" {
		if reason := r.unavailable(p, row, a, clock.Time); reason != "" {
			if err = r.abandon(ctx, q, row, reason); err != nil {
				return result, err
			}
			row.State = "abandoned"
			return tenantSnapshotBuild(row), mapPostgresError("commit abandoned Snapshot", tx.Commit(ctx), nil)
		}
	}
	if err = fn(q, p, row, a); err != nil {
		return result, err
	}
	row, err = q.LockTenantSnapshot(ctx, key)
	if err != nil {
		return result, err
	}
	return tenantSnapshotBuild(row), mapPostgresError("commit Snapshot progress", tx.Commit(ctx), nil)
}
func (r *governanceSnapshotRepository) LoadPage(ctx context.Context, c biz.TenantSnapshotCursor, page biz.TenantSnapshotPage) (biz.TenantSnapshotBuild, error) {
	if page.Validate(c) != nil || c.ReaderID != r.reader {
		return biz.TenantSnapshotBuild{}, biz.ErrTenantLifecycleInvalid
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 65536 {
		return biz.TenantSnapshotBuild{}, biz.ErrTenantLifecycleInvalid
	}
	digest := sha256.Sum256(raw)
	return r.withBuild(ctx, c.ID, func(q *sqlcgen.Queries, _ sqlcgen.TenantLifecyclePipeline, b sqlcgen.TenantSnapshotRebuild, _ tenantSnapshotAuthority) error {
		if !sameTenantSnapshotCursor(c, b) {
			return biz.ErrTenantLifecycleConflict
		}
		prior, err := q.ReadTenantSnapshotPage(ctx, sqlcgen.ReadTenantSnapshotPageParams{Producer: b.Producer, SnapshotID: b.SnapshotID, RequestToken: page.RequestToken})
		if err == nil {
			if !bytes.Equal(prior.PageSha256, digest[:]) || !bytes.Equal(prior.PagePayload, raw) {
				return biz.ErrTenantLifecycleConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return mapPostgresError("read Snapshot page receipt", err, nil)
		}
		if b.State != "loading" || page.RequestToken != b.NextToken || b.LoadedItems+int64(len(page.Items)) > b.TotalCount {
			return biz.ErrTenantLifecycleConflict
		}
		if page.NextToken == "" && b.LoadedItems+int64(len(page.Items)) != b.TotalCount {
			return biz.ErrTenantLifecycleConflict
		}
		if page.NextToken != "" {
			if b.LoadedItems+int64(len(page.Items)) >= b.TotalCount {
				return biz.ErrTenantLifecycleConflict
			}
			_, e := q.ReadTenantSnapshotPage(ctx, sqlcgen.ReadTenantSnapshotPageParams{Producer: b.Producer, SnapshotID: b.SnapshotID, RequestToken: page.NextToken})
			if e == nil {
				return biz.ErrTenantLifecycleConflict
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return mapPostgresError("check Snapshot cursor cycle", e, nil)
			}
		}
		last := b.LastTenantID
		for _, fact := range page.Items {
			if last.Valid && fact.TenantID.String() <= uuid.UUID(last.Bytes).String() {
				return biz.ErrTenantLifecycleConflict
			}
			if err = q.InsertTenantSnapshotFact(ctx, sqlcgen.InsertTenantSnapshotFactParams{Producer: b.Producer, GenerationID: b.GenerationID, TenantID: fact.TenantID, TenantVersion: fact.Version, BusinessStatus: fact.Status}); err != nil {
				return mapPostgresError("retain tenant-scoped Snapshot fact", err, biz.ErrTenantLifecycleConflict)
			}
			last = requiredPGUUID(fact.TenantID)
		}
		if err = q.AppendTenantSnapshotPage(ctx, sqlcgen.AppendTenantSnapshotPageParams{Producer: b.Producer, SnapshotID: b.SnapshotID, RequestToken: page.RequestToken, PageSha256: digest[:], PagePayload: raw, NextToken: page.NextToken, ItemCount: int32(len(page.Items))}); err != nil {
			return mapPostgresError("append immutable Snapshot page", err, nil)
		}
		return mapPostgresError("advance Snapshot page", q.AdvanceTenantSnapshotPage(ctx, sqlcgen.AdvanceTenantSnapshotPageParams{Producer: b.Producer, SnapshotID: b.SnapshotID, NextToken: page.NextToken, LastTenantID: last, ItemCount: int64(len(page.Items))}), nil)
	})
}
func (r *governanceSnapshotRepository) CatchUp(ctx context.Context, id uuid.UUID) (biz.TenantSnapshotBuild, error) {
	return r.withBuild(ctx, id, func(q *sqlcgen.Queries, _ sqlcgen.TenantLifecyclePipeline, b sqlcgen.TenantSnapshotRebuild, a tenantSnapshotAuthority) error {
		if b.State == "activated" {
			return nil
		}
		if b.State != "loaded" && b.State != "catching_up" {
			return biz.ErrTenantSnapshotNotReady
		}
		increments, err := q.ListTenantSnapshotIncrements(ctx, sqlcgen.ListTenantSnapshotIncrementsParams{Producer: b.Producer, Epoch: b.Epoch, AfterSequence: b.AppliedThrough})
		if err != nil {
			return mapPostgresError("read retained Snapshot increments", err, nil)
		}
		p, err := biz.ActivateTenantLifecycleSnapshot(b.Epoch, b.AppliedThrough, b.AppliedThrough)
		if err != nil {
			return err
		}
		for _, receipt := range increments {
			prior, e := q.ReadCoreBrokerEventAuthority(ctx, sqlcgen.ReadCoreBrokerEventAuthorityParams{ConsumerID: r.broker.config.ConsumerID, EventID: receipt.EventID, TenantID: receipt.TenantID})
			if errors.Is(e, pgx.ErrNoRows) {
				return biz.ErrCoreBrokerAuthority
			}
			if e != nil {
				return mapPostgresError("read Snapshot replay authority", e, nil)
			}
			if !a.broker.matches(prior) || prior.Epoch != b.Epoch || prior.SourceSequence != receipt.SourceSequence || !bytes.Equal(prior.RawSha256, receipt.RawSha256) {
				return r.abandon(ctx, q, b, "authority_changed")
			}
			if receipt.SourceSequence != p.Applied+1 {
				return r.abandon(ctx, q, b, "increment_gap")
			}
			fact := biz.TenantLifecycleFact{}
			row, e := q.ReadTenantLifecycleFact(ctx, sqlcgen.ReadTenantLifecycleFactParams{Producer: b.Producer, GenerationID: b.GenerationID, TenantID: receipt.TenantID.Bytes})
			if e == nil {
				fact = tenantLifecycleFact(row)
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return mapPostgresError("read Snapshot Tenant increment base", e, nil)
			}
			event := biz.TenantLifecycleEvent{EventID: receipt.EventID, Epoch: receipt.Epoch, TenantID: receipt.TenantID.Bytes, Sequence: receipt.SourceSequence, TenantVersion: receipt.TenantVersion.Int64, Status: receipt.BusinessStatus.String, Reason: receipt.Reason.String, OccurredAt: receipt.OccurredAt.Time, EffectiveAt: receipt.EffectiveAt.Time}
			update, e := biz.ProjectTenantLifecycle(p, fact, event)
			if e != nil || update.Outcome != "applied" {
				return r.abandon(ctx, q, b, "conflicting_fact")
			}
			p = update.Pipeline
			if e = q.WriteTenantLifecycleFact(ctx, sqlcgen.WriteTenantLifecycleFactParams{Producer: b.Producer, GenerationID: b.GenerationID, TenantID: event.TenantID, TenantVersion: update.Fact.Version, BusinessStatus: update.Fact.Status, Reason: update.Fact.Reason, EffectiveAt: tenantFactTime(update.Fact.EffectiveAt)}); e != nil {
				return mapPostgresError("apply exact Snapshot increment", e, nil)
			}
		}
		return mapPostgresError("advance Snapshot replay", q.AdvanceTenantSnapshotReplay(ctx, sqlcgen.AdvanceTenantSnapshotReplayParams{Producer: b.Producer, SnapshotID: b.SnapshotID, AppliedThrough: p.Applied}), nil)
	})
}
func (r *governanceSnapshotRepository) Activate(ctx context.Context, id uuid.UUID) (biz.TenantSnapshotBuild, error) {
	return r.withBuild(ctx, id, func(q *sqlcgen.Queries, p sqlcgen.TenantLifecyclePipeline, b sqlcgen.TenantSnapshotRebuild, _ tenantSnapshotAuthority) error {
		if b.State == "activated" {
			return nil
		}
		if b.State != "catching_up" && b.State != "loaded" {
			return biz.ErrTenantSnapshotNotReady
		}
		highest, err := q.TenantSnapshotHighest(ctx, sqlcgen.TenantSnapshotHighestParams{Producer: b.Producer, Epoch: b.Epoch})
		if err != nil {
			return mapPostgresError("read Snapshot continuity cut", err, nil)
		}
		if b.AppliedThrough < highest || (p.Epoch.Valid && uuid.UUID(p.Epoch.Bytes) == b.Epoch && b.AppliedThrough < p.AppliedSequence) {
			return biz.ErrTenantSnapshotNotReady
		}
		covered, err := q.TenantSnapshotCoversCurrent(ctx, sqlcgen.TenantSnapshotCoversCurrentParams{Producer: b.Producer, NewGeneration: b.GenerationID, OldGeneration: p.GenerationID})
		if err != nil {
			return mapPostgresError("verify Snapshot covers current facts", err, nil)
		}
		if !covered.Valid || !covered.Bool {
			return r.abandon(ctx, q, b, "conflicting_fact")
		}
		if err = q.ActivateTenantSnapshot(ctx, sqlcgen.ActivateTenantSnapshotParams{Producer: b.Producer, GenerationID: requiredPGUUID(b.GenerationID), Epoch: requiredPGUUID(b.Epoch), AppliedSequence: b.AppliedThrough}); err != nil {
			return mapPostgresError("atomically activate Tenant lifecycle generation", err, nil)
		}
		return mapPostgresError("retain activated Snapshot", q.MarkTenantSnapshotActivated(ctx, sqlcgen.MarkTenantSnapshotActivatedParams{Producer: b.Producer, SnapshotID: b.SnapshotID}), nil)
	})
}
