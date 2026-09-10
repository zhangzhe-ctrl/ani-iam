package data

import (
	"context"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type workloadCredentialAudit struct{ data *Data }

func NewWorkloadCredentialAudit(data *Data) biz.WorkloadCredentialAudit {
	return &workloadCredentialAudit{data: data}
}

func (r *workloadCredentialAudit) AppendCredentialIssue(ctx context.Context, tenantID uuid.UUID, e biz.SecurityAuditEvent) error {
	caller, ok := biz.DirectCallerFromContext(ctx)
	if !ok || caller != e.DirectCaller || e.ActorID != caller.Identity.PrincipalID || e.ID == uuid.Nil || e.TargetID == uuid.Nil || e.TargetVersion <= 0 || e.AuthenticationMethod != biz.AuditAuthenticationMethodWorkloadToken || e.Result != biz.AuditResultSucceeded {
		return biz.ErrInvalidPersistenceState
	}
	if (tenantID == uuid.Nil && (e.Action != "workload.token.issue" || e.Boundary != biz.AuditBoundaryPrincipal)) || (tenantID != uuid.Nil && (e.Action != "workload.delegation.issue" || e.Boundary != biz.AuditBoundaryTenant)) {
		return biz.ErrInvalidPersistenceState
	}
	// One append is the only state change of minting. The credential is returned
	// only after this durable statement commits; failure never releases it.
	return sqlcgen.New(r.data.pool).AppendWorkloadCredentialSecurityAuditEvent(ctx, sqlcgen.AppendWorkloadCredentialSecurityAuditEventParams{
		TenantID: optionalPGUUID(tenantID), EventID: e.ID, ActorID: requiredPGUUID(e.ActorID), Boundary: string(e.Boundary), Action: string(e.Action), TargetType: string(e.TargetType), TargetID: e.TargetID, TargetVersion: e.TargetVersion, RequestID: e.RequestID, Now: requiredTimestamptz(e.OccurredAt),
		CallerPrincipalID: requiredPGUUID(caller.Identity.PrincipalID), CallerBindingID: requiredPGUUID(caller.Identity.BindingID), CallerBindingVersion: optionalPositiveInt64(caller.Identity.BindingVersion), CallerGrantVersion: optionalPositiveInt64(caller.GrantVersion),
	})
}
