package data

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type tenantAuditReader struct{ data *Data }

func NewPostgresTenantAuditReader(d *Data) biz.TenantAuditReader { return &tenantAuditReader{data: d} }

func (r *tenantAuditReader) Get(ctx context.Context, scope biz.TenantScope, id uuid.UUID) (biz.AuditRecord, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return biz.AuditRecord{}, err
	}
	v, err := sqlcgen.New(r.data.pool).GetTenantAuditEvent(ctx, sqlcgen.GetTenantAuditEventParams{TenantID: requiredPGUUID(tenant), EventID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.AuditRecord{}, biz.ErrAuditEventNotFound
	}
	if err != nil {
		return biz.AuditRecord{}, mapPostgresError("get tenant audit event", err, nil)
	}
	return auditRecord(v), nil
}

func (r *tenantAuditReader) List(ctx context.Context, scope biz.TenantScope, q biz.AuditQuery) ([]biz.AuditRecord, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 101 {
		return nil, biz.ErrAuditQueryInvalid
	}
	rows, err := sqlcgen.New(r.data.pool).ListTenantAuditEvents(ctx, sqlcgen.ListTenantAuditEventsParams{TenantID: requiredPGUUID(tenant), Action: q.Action, Result: string(q.Result), BeforeID: q.Before, PageLimit: q.Limit})
	if err != nil {
		return nil, mapPostgresError("list tenant audit events", err, nil)
	}
	result := make([]biz.AuditRecord, 0, len(rows))
	for _, v := range rows {
		result = append(result, auditRecord(sqlcgen.GetTenantAuditEventRow(v)))
	}
	return result, nil
}

func auditRecord(v sqlcgen.GetTenantAuditEventRow) biz.AuditRecord {
	actor := uuid.Nil
	if v.ActorID.Valid {
		actor = uuid.UUID(v.ActorID.Bytes)
	}
	caller := biz.DirectCaller{}
	if v.CallerPrincipalID.Valid && v.CallerBindingID.Valid && v.CallerBindingVersion.Valid && v.CallerGrantVersion.Valid {
		caller.Identity.PrincipalID = uuid.UUID(v.CallerPrincipalID.Bytes)
		caller.Identity.BindingID = uuid.UUID(v.CallerBindingID.Bytes)
		caller.Identity.BindingVersion = v.CallerBindingVersion.Int64
		caller.GrantVersion = v.CallerGrantVersion.Int64
	}
	return biz.AuditRecord{ActorType: v.ActorType, SecurityAuditEvent: biz.SecurityAuditEvent{DirectCaller: caller, ID: v.EventID, ActorID: actor, AuthenticationMethod: biz.AuditAuthenticationMethod(v.AuthenticationMethod), Boundary: biz.AuditBoundary(v.Boundary), Action: biz.AuditAction(v.Action), TargetType: biz.AuditTargetType(v.TargetType), TargetID: v.TargetID, TargetVersion: v.TargetVersion, Result: biz.AuditResult(v.Result), Reason: biz.AuditReason(v.Reason), RequestID: v.RequestID, CorrelationID: v.CorrelationID, DecisionID: v.DecisionID, SourceService: biz.AuditSourceService(v.SourceService), OccurredAt: v.OccurredAt.Time, RecordedAt: v.RecordedAt.Time}}
}
