package service

import (
	"context"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *IAMAdminService) GetAuditEvent(ctx context.Context, r *iamv1.GetAuditEventRequest) (*iamv1.GetAuditEventResponse, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "GetAuditEvent", "getIAMSecurityAuditEvent", r.GetTenantId(), r.GetEventId())
	if err != nil {
		return nil, err
	}
	scope, tenant, err := trustedTenantScope(ctx, "getIAMSecurityAuditEvent")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetEventId(), "event_id")
	if err != nil {
		return nil, err
	}
	event, err := s.audit.Get(ctx, scope, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getIAMSecurityAuditEvent", TenantID: tenant.String(), ResourceID: id.String()})
	}
	return &iamv1.GetAuditEventResponse{Event: tenantAuditDTO(tenant, event)}, nil
}

func (s *IAMAdminService) ListAuditEvents(ctx context.Context, r *iamv1.ListAuditEventsRequest) (*iamv1.ListAuditEventsResponse, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "ListAuditEvents", "listIAMSecurityAuditEvents", r.GetTenantId(), "")
	if err != nil {
		return nil, err
	}
	scope, tenant, err := trustedTenantScope(ctx, "listIAMSecurityAuditEvents")
	if err != nil {
		return nil, err
	}
	page, err := s.audit.List(ctx, scope, r.GetAction(), biz.AuditResult(r.GetResult()), r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listIAMSecurityAuditEvents", TenantID: tenant.String()})
	}
	result := &iamv1.ListAuditEventsResponse{Events: make([]*iamv1.AuditEvent, 0, len(page.Events)), NextCursor: page.NextCursor}
	for _, e := range page.Events {
		result.Events = append(result.Events, tenantAuditDTO(tenant, e))
	}
	return result, nil
}

func tenantAuditDTO(tenant uuid.UUID, r biz.AuditRecord) *iamv1.AuditEvent {
	actor := ""
	if r.ActorID != uuid.Nil {
		actor = r.ActorID.String()
	}
	return &iamv1.AuditEvent{EventId: r.ID.String(), OccurredAt: timestamppb.New(r.OccurredAt), RecordedAt: timestamppb.New(r.RecordedAt), ActorPrincipalId: actor, ActorType: r.ActorType, AuthnMethods: authnMethodsToProto([]biz.AuditAuthenticationMethod{r.AuthenticationMethod}), Boundary: &iamv1.AuditBoundary{Boundary: &iamv1.AuditBoundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenant.String()}}}, Action: string(r.Action), TargetType: string(r.TargetType), TargetId: r.TargetID.String(), TargetVersion: uint64(r.TargetVersion), Result: string(r.Result), Reason: string(r.Reason), RequestId: r.RequestID, CorrelationId: r.CorrelationID, DecisionId: r.DecisionID, SourceService: string(r.SourceService), AuthenticationMethod: string(r.AuthenticationMethod), CallerPrincipalId: auditOptionalUUID(r.DirectCaller.Identity.PrincipalID), CallerBindingId: auditOptionalUUID(r.DirectCaller.Identity.BindingID), CallerBindingVersion: uint64(r.DirectCaller.Identity.BindingVersion), CallerGrantVersion: uint64(r.DirectCaller.GrantVersion)}
}

func auditOptionalUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
