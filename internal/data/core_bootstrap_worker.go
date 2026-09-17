package data

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type coreBootstrapWorkUnitOfWork struct {
	broker   *coreBrokerRepository
	data     *Data
	producer string
}
type coreBootstrapWorkTransaction struct {
	platformRecovery bool
	broker           *coreBrokerRepository
	q                *sqlcgen.Queries
	data             *Data
	producer         string
	tenant           uuid.UUID
	sourceEpoch      uuid.UUID
	work             *biz.CoreBootstrapWork
	authority        *biz.CoreBootstrapExecutionAuthorization
	auditID          uuid.UUID
	auditAction      biz.AuditAction
}

func NewCoreBootstrapWorkUnitOfWork(d *Data, producer string) (biz.CoreBootstrapWorkUnitOfWork, error) {
	if d == nil || d.pool == nil || !biz.ValidCoreProjectionProducer(producer) {
		return nil, biz.ErrCoreBootstrapInvalid
	}
	return &coreBootstrapWorkUnitOfWork{data: d, producer: producer}, nil
}

func NewCoreBrokerBootstrapWorkUnitOfWork(d *Data, c CoreBrokerConfiguration) (biz.CoreBootstrapWorkUnitOfWork, error) {
	broker, err := newCoreBrokerRepository(d, c)
	if err != nil {
		return nil, err
	}
	return &coreBootstrapWorkUnitOfWork{data: d, producer: c.Producer, broker: broker}, nil
}

func (u *coreBootstrapWorkUnitOfWork) WithinCoreBootstrapWork(ctx context.Context, scope biz.TenantScope, fn func(context.Context, biz.CoreBootstrapWorkTransaction) error) error {
	tenant, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin Bootstrap reconciliation", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	if u.broker != nil {
		if err = q.LockCoreBrokerAuthority(ctx); err != nil {
			return mapPostgresError("lock worker current broker authority", err, nil)
		}
	}
	// Freeze the pipeline/generation while evaluating this local mutation, then
	// take the existing Tenant administration/recovery guard in that order.
	if _, err = q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: u.producer}); errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrCoreProjectionMissing
	} else if err != nil {
		return mapPostgresError("lock Bootstrap lifecycle", err, nil)
	}
	if err = q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
		return mapPostgresError("lock Bootstrap Tenant", err, nil)
	}
	t := &coreBootstrapWorkTransaction{q: q, data: u.data, producer: u.producer, tenant: tenant, broker: u.broker}
	if u.broker != nil {
		ctx = context.WithValue(ctx, coreBrokerTransactionKey{}, coreBrokerTransaction{data: u.data, consumer: u.broker.config.ConsumerID, q: q, work: t})
	}
	if err = fn(ctx, t); err != nil {
		return err
	}
	return mapPostgresError("commit Bootstrap invitation and audit", tx.Commit(ctx), nil)
}

func (t *coreBootstrapWorkTransaction) LoadCoreBootstrapWork(ctx context.Context, scope biz.TenantScope, id uuid.UUID) (biz.CoreBootstrapWork, error) {
	var result biz.CoreBootstrapWork
	if _, err := tenantIDForScope(t.tenant, scope); err != nil {
		return result, err
	}
	op, err := t.q.GetCoreBootstrapOperation(ctx, sqlcgen.GetCoreBootstrapOperationParams{TenantID: t.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreBootstrapConflict
	}
	if err != nil {
		return result, mapPostgresError("load Bootstrap operation", err, nil)
	}
	if op.SourceKind != "core" {
		return result, biz.ErrCoreBootstrapConflict
	}
	source, err := t.q.GetCoreBootstrapWorkSource(ctx, sqlcgen.GetCoreBootstrapWorkSourceParams{TenantID: t.tenant, OperationID: id, Producer: t.producer})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreBootstrapConflict
	}
	if err != nil {
		return result, mapPostgresError("load durable original Bootstrap source", err, nil)
	}
	var payload map[string]string
	if json.Unmarshal(op.Payload, &payload) != nil || len(payload) != 4 || payload["tenant_id"] != t.tenant.String() || payload["operation_id"] != id.String() || payload["normalized_email"] != op.IntendedEmail || source.PayloadFingerprint != op.PayloadFingerprint {
		return result, biz.ErrCoreBootstrapInvalid
	}
	if err = checkBootstrapCreatingFact(ctx, t.q, t.producer, uuid.UUID(source.SourceEpoch.Bytes), t.tenant, source.SourceSequence); err != nil {
		return result, err
	}
	t.sourceEpoch = uuid.UUID(source.SourceEpoch.Bytes)
	result.Source = biz.CoreBootstrapSource{TenantID: t.tenant, OperationID: id, EventID: source.EventID, Producer: t.producer, Fingerprint: op.PayloadFingerprint, SourceSequence: source.SourceSequence}
	result.Intent = biz.CoreBootstrapIntent{TenantID: t.tenant, OperationID: id, NormalizedEmail: op.IntendedEmail, Locale: payload["locale"], Fingerprint: op.PayloadFingerprint}
	result.Version, result.Status, result.Superseded = op.Version, op.Status, op.SupersededBy.Valid
	lifecycle, err := t.q.ReadCoreShadowTenant(ctx, sqlcgen.ReadCoreShadowTenantParams{Producer: t.producer, TenantID: t.tenant})
	if err == nil {
		result.Lifecycle = biz.CoreShadowTenant{CoreTenantProjection: biz.CoreTenantProjection{Status: lifecycle.Status, Version: lifecycle.LifecycleVersion, RepairRequired: lifecycle.RepairRequired}, Fresh: lifecycle.Fresh && source.SourceEpoch.Valid && source.SourceEpoch == lifecycle.Epoch && lifecycle.AppliedSequence >= source.SourceSequence, GenerationID: lifecycle.GenerationID}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read Bootstrap current lifecycle", err, nil)
	}
	access, err := t.q.GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: t.tenant})
	result.AccessExists = err == nil
	if err == nil {
		result.AccessStatus = biz.TenantAccessStatus(access.Status)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("check Bootstrap Access absence", err, nil)
	}
	saved, err := t.q.GetCoreBootstrapWorkerResult(ctx, sqlcgen.GetCoreBootstrapWorkerResultParams{TenantID: t.tenant, OperationID: id})
	if err == nil {
		result.Result = &biz.CoreBootstrapWorkerResult{OperationID: id, InvitationID: saved.InvitationID, RoleID: saved.RoleID, DeliveryID: saved.DeliveryID, AuditID: saved.AuditID, Status: op.Status}
		invitations := tenantInvitationTransaction{postgresTenantAuthorizationTransaction: postgresTenantAuthorizationTransaction{queries: t.q, tenantID: t.tenant}, protector: t.data.outbox}
		invitation, readErr := invitations.GetInvitation(ctx, scope, saved.InvitationID)
		if readErr != nil {
			return result, readErr
		}
		result.Invitation = &invitation
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read durable Bootstrap worker result", err, nil)
	}
	jobs, err := t.q.ListCurrentCoreBootstrapJobs(ctx, sqlcgen.ListCurrentCoreBootstrapJobsParams{TenantID: t.tenant, OperationID: id, Producer: t.producer})
	if err != nil {
		return result, mapPostgresError("read current Bootstrap jobs", err, nil)
	}
	for _, job := range jobs {
		result.Jobs = append(result.Jobs, toCoreBootstrapJob(job))
	}
	t.work = &result
	return result, nil
}

func (t *coreBootstrapWorkTransaction) RecheckBootstrapExecution(ctx context.Context, scope biz.TenantScope, a biz.CoreBootstrapExecutionAuthorization) error {
	if _, err := tenantIDForScope(t.tenant, scope); err != nil {
		return err
	}
	if t.work == nil || a.Source != t.work.Source || a.ProducerPrincipalID == a.ExecutorPrincipalID {
		return biz.ErrCoreBootstrapAuthority
	}
	if t.broker != nil {
		current, err := t.currentBrokerAuthority(ctx, a.Source)
		if err != nil {
			return err
		}
		if a.ProducerPrincipalID != current.producer.PrincipalID || a.ExecutorPrincipalID != current.receiver.PrincipalID || a.ProducerVersion != current.producer.PrincipalVersion || a.ExecutorVersion != current.receiver.PrincipalVersion || !time.Now().Before(a.ValidUntil) {
			return biz.ErrCoreBootstrapAuthority
		}
		t.authority = &a
		return nil
	}
	principals := []struct {
		id      uuid.UUID
		version int64
	}{{a.ProducerPrincipalID, a.ProducerVersion}, {a.ExecutorPrincipalID, a.ExecutorVersion}}
	slices.SortFunc(principals, func(a, b struct {
		id      uuid.UUID
		version int64
	}) int {
		return strings.Compare(a.id.String(), b.id.String())
	})
	for _, p := range principals {
		if _, err := t.q.LockCoreBootstrapWorkerPrincipal(ctx, sqlcgen.LockCoreBootstrapWorkerPrincipalParams{PrincipalID: p.id, ExpectedVersion: p.version, ValidUntil: requiredTimestamptz(a.ValidUntil)}); errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrCoreBootstrapAuthority
		} else if err != nil {
			return mapPostgresError("recheck Bootstrap Workload identity", err, nil)
		}
	}
	t.authority = &a
	return nil
}

func (t *coreBootstrapWorkTransaction) CreateCoreBootstrapInvitation(ctx context.Context, scope biz.TenantScope, work biz.CoreBootstrapWork, draft biz.CoreBootstrapInvitationDraft, a biz.CoreBootstrapExecutionAuthorization) error {
	if _, err := tenantIDForScope(t.tenant, scope); err != nil {
		return err
	}
	if t.work == nil || t.authority == nil || *t.authority != a || work.Source != t.work.Source || t.work.AccessExists || t.work.Result != nil {
		return biz.ErrCoreBootstrapConflict
	}
	if err := t.checkBootstrapLifecycle(ctx); err != nil {
		return err
	}
	i := draft.Invitation.Invitation
	if i.Version != 1 || i.DeliveryGeneration != 1 || i.Status != biz.InvitationPending || sha256.Sum256([]byte(draft.Token)) != draft.Invitation.TokenDigest || i.CreatedBy != a.ExecutorPrincipalID || len(i.RoleIDs) != 1 || i.RoleIDs[0] != draft.RoleID || i.NormalizedEmail != work.Intent.NormalizedEmail || i.Locale != work.Intent.Locale {
		return biz.ErrCoreBootstrapConflict
	}
	if err := t.q.CreateBootstrapPendingAccess(ctx, sqlcgen.CreateBootstrapPendingAccessParams{TenantID: t.tenant, Now: requiredTimestamptz(i.CreatedAt)}); err != nil {
		return mapPostgresError("create pending Bootstrap Access", err, nil)
	}
	if err := t.q.CreateBootstrapAdministratorRole(ctx, sqlcgen.CreateBootstrapAdministratorRoleParams{TenantID: t.tenant, ID: draft.RoleID, Now: requiredTimestamptz(i.CreatedAt)}); err != nil {
		return mapPostgresError("create Bootstrap administrator role", err, nil)
	}
	for _, p := range draft.Permissions {
		if p.Scope != biz.PermissionScopeTenant {
			return biz.ErrCoreBootstrapConflict
		}
		if err := t.q.AddCustomTenantRolePermission(ctx, sqlcgen.AddCustomTenantRolePermissionParams{TenantID: t.tenant, RoleID: draft.RoleID, Resource: p.Resource, Action: p.Action, CreatedAt: requiredTimestamptz(i.CreatedAt)}); err != nil {
			return mapPostgresError("populate Bootstrap administrator definition", err, nil)
		}
	}
	err := t.q.CreateCoreBootstrapTenantInvitation(ctx, sqlcgen.CreateCoreBootstrapTenantInvitationParams{TenantID: t.tenant, ID: i.ID, NormalizedEmail: i.NormalizedEmail, RoleIds: i.RoleIDs, Locale: i.Locale, TokenDigest: draft.Invitation.TokenDigest[:], ExpiresAt: requiredTimestamptz(i.ExpiresAt), CreatedBy: i.CreatedBy, Now: requiredTimestamptz(i.CreatedAt), OperationID: requiredPGUUID(work.Source.OperationID)})
	if err != nil {
		return mapPostgresError("create exact Bootstrap invitation", err, biz.ErrCoreBootstrapConflict)
	}
	invitations := tenantInvitationTransaction{postgresTenantAuthorizationTransaction: postgresTenantAuthorizationTransaction{queries: t.q, tenantID: t.tenant}, protector: t.data.outbox}
	if err = invitations.addInvitationRoles(ctx, t.tenant, i); err != nil {
		return err
	}
	if err = invitations.addInvitationDelivery(ctx, t.tenant, i, draft.DeliveryID, draft.Token); err != nil {
		return err
	}
	n, err := t.q.MarkCoreBootstrapWaiting(ctx, sqlcgen.MarkCoreBootstrapWaitingParams{TenantID: t.tenant, OperationID: work.Source.OperationID, ExpectedVersion: work.Version, Now: requiredTimestamptz(i.CreatedAt)})
	if err != nil {
		return mapPostgresError("wait for invited administrator acceptance", err, nil)
	}
	if n != 1 {
		return biz.ErrCoreBootstrapConflict
	}
	err = t.q.SaveCoreBootstrapWorkerResult(ctx, sqlcgen.SaveCoreBootstrapWorkerResultParams{TenantID: t.tenant, OperationID: work.Source.OperationID, SourceEventID: work.Source.EventID, ProducerPrincipalID: a.ProducerPrincipalID, ExecutorPrincipalID: a.ExecutorPrincipalID, ProducerVersion: a.ProducerVersion, ExecutorVersion: a.ExecutorVersion, DecisionID: a.DecisionID, RoleID: draft.RoleID, InvitationID: i.ID, DeliveryID: draft.DeliveryID, AuditID: draft.AuditID, Now: requiredTimestamptz(i.CreatedAt)})
	if err != nil {
		return mapPostgresError("retain immutable Bootstrap worker result", err, nil)
	}
	t.auditID = draft.AuditID
	t.auditAction = "iam.bootstrap.invitation.created"
	return nil
}

func (t *coreBootstrapWorkTransaction) AppendBootstrapWorkerAudit(ctx context.Context, scope biz.TenantScope, e biz.SecurityAuditEvent) error {
	if t.work == nil || t.authority == nil || e.ID != t.auditID || e.ActorID != t.authority.ExecutorPrincipalID || e.TargetID != t.work.Source.OperationID || e.TargetVersion != t.work.Version+1 || e.Action != t.auditAction || e.RequestID != t.work.Source.EventID.String() || e.DecisionID != t.authority.DecisionID.String() || e.DirectCaller != (biz.DirectCaller{}) {
		return biz.ErrInvalidPersistenceState
	}
	return (securityAuditRepository{queries: t.q, tenantID: t.tenant}).Append(ctx, scope, e)
}

func (t *coreBootstrapWorkTransaction) ExpireCoreBootstrapInvitation(ctx context.Context, scope biz.TenantScope, work biz.CoreBootstrapWork, a biz.CoreBootstrapExecutionAuthorization, audit uuid.UUID, now time.Time) error {
	if _, err := tenantIDForScope(t.tenant, scope); err != nil {
		return err
	}
	if t.work == nil || t.authority == nil || *t.authority != a || work.Source != t.work.Source || work.Result == nil || work.Invitation == nil || work.Result.InvitationID != work.Invitation.Invitation.ID || work.Status != "waiting_for_principal_verification" || audit.Version() != 7 {
		return biz.ErrCoreBootstrapConflict
	}
	inv := work.Invitation.Invitation
	invitations := tenantInvitationTransaction{postgresTenantAuthorizationTransaction: postgresTenantAuthorizationTransaction{queries: t.q, tenantID: t.tenant}, protector: t.data.outbox}
	if inv.Status == biz.InvitationPending {
		if err := invitations.EndInvitation(ctx, scope, inv.ID, inv.Version, biz.InvitationExpired, now); err != nil {
			return err
		}
	} else if inv.Status != biz.InvitationExpired {
		return biz.ErrCoreBootstrapConflict
	}
	n, err := t.q.MarkCoreBootstrapInvitationAttention(ctx, sqlcgen.MarkCoreBootstrapInvitationAttentionParams{TenantID: t.tenant, OperationID: work.Source.OperationID, ExpectedVersion: work.Version, InvitationID: inv.ID, Now: requiredTimestamptz(now)})
	if err != nil {
		return mapPostgresError("mark expired Bootstrap invitation attention", err, nil)
	}
	if n != 1 {
		return biz.ErrCoreBootstrapConflict
	}
	t.auditID = audit
	t.auditAction = "iam.bootstrap.invitation.expired"
	return nil
}

// The creating fact must be covered in the same epoch. Bootstrap may arrive
// first on its independently retried subject, including while an older epoch
// still has a fresh active fact for this Tenant.
func (t *coreBootstrapWorkTransaction) checkBootstrapLifecycle(ctx context.Context) error {
	if t.work == nil || t.sourceEpoch == uuid.Nil {
		return biz.ErrTenantLifecycleStale
	}
	v, err := t.q.ReadCoreShadowTenant(ctx, sqlcgen.ReadCoreShadowTenantParams{Producer: t.producer, TenantID: t.tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrTenantLifecycleStale
	}
	if err != nil {
		return mapPostgresError("recheck Bootstrap referenced lifecycle", err, nil)
	}
	if !v.Fresh || !v.Epoch.Valid || uuid.UUID(v.Epoch.Bytes) != t.sourceEpoch || v.AppliedSequence < t.work.Source.SourceSequence {
		return biz.ErrTenantLifecycleStale
	}
	if v.Status != "active" {
		return biz.ErrTenantLifecycleBlocked
	}
	return nil
}

// If the creating event is retained, its exact epoch/sequence must identify
// this Tenant's creation. An absent historical event can only be covered by
// the separately verified Snapshot/freshness guard; it grants no identity.
func checkBootstrapCreatingFact(ctx context.Context, q *sqlcgen.Queries, producer string, epoch, tenant uuid.UUID, sequence int64) error {
	event, err := q.ReadTenantLifecyclePosition(ctx, sqlcgen.ReadTenantLifecyclePositionParams{Producer: producer, Epoch: epoch, SourceSequence: sequence})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return mapPostgresError("read Bootstrap creating fact", err, nil)
	}
	if !event.TenantID.Valid || uuid.UUID(event.TenantID.Bytes) != tenant || event.TenantVersion.Int64 != 1 || event.BusinessStatus.String != "active" {
		return biz.ErrCoreBootstrapConflict
	}
	return nil
}
