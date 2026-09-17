package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (s *IAMAdminService) GetPlatformAuditEvent(ctx context.Context, r *iamv1.GetPlatformAuditEventRequest) (*iamv1.GetPlatformAuditEventResponse, error) {
	const op = "getPlatformIAMSecurityAuditEvent"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "GetPlatformAuditEvent", op, r.GetEventId(), "PLATFORM_AUDIT_QUERY")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetEventId(), "event_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetAuditEvent(ctx, cap, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	event, err := platformAuditDTO(v)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op})
	}
	return &iamv1.GetPlatformAuditEventResponse{Event: event}, nil
}
func (s *IAMAdminService) ListPlatformAuditEvents(ctx context.Context, r *iamv1.ListPlatformAuditEventsRequest) (*iamv1.ListPlatformAuditEventsResponse, error) {
	const op = "listPlatformIAMSecurityAuditEvents"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ListPlatformAuditEvents", op, r.GetTenantId(), "PLATFORM_AUDIT_QUERY")
	if err != nil {
		return nil, err
	}
	tenant := uuid.Nil
	if r.GetTenantId() != "" {
		tenant, err = requiredUUID(r.GetTenantId(), "tenant_id")
		if err != nil {
			return nil, err
		}
	}
	v, err := s.platform.ListAuditEvents(ctx, cap, tenant, r.GetAction(), biz.AuditResult(r.GetResult()), r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op})
	}
	page := &iamv1.ListPlatformAuditEventsResponse{Events: make([]*iamv1.AuditEvent, 0, len(v.Events)), NextCursor: v.NextCursor}
	for _, record := range v.Events {
		event, err := platformAuditDTO(record)
		if err != nil {
			return nil, mapIAMError(err, errorContext{OperationID: op})
		}
		page.Events = append(page.Events, event)
	}
	return page, nil
}
func platformAuditDTO(v biz.PlatformAuditRecord) (*iamv1.AuditEvent, error) {
	event := tenantAuditDTO(v.TenantID, v.Record)
	switch v.Record.Boundary {
	case biz.AuditBoundaryTenant:
		if v.TenantID == uuid.Nil {
			return nil, biz.ErrInvalidPersistenceState
		}
	case biz.AuditBoundaryPlatform:
		if v.TenantID != uuid.Nil {
			return nil, biz.ErrInvalidPersistenceState
		}
		event.Boundary = &iamv1.AuditBoundary{Boundary: &iamv1.AuditBoundary_Platform{Platform: &iamv1.PlatformBoundary{}}}
	case biz.AuditBoundaryPrincipal:
		if v.TenantID != uuid.Nil {
			return nil, biz.ErrInvalidPersistenceState
		}
		event.Boundary = &iamv1.AuditBoundary{Boundary: &iamv1.AuditBoundary_Principal{Principal: &iamv1.PrincipalAuditBoundary{}}}
	default:
		return nil, biz.ErrInvalidPersistenceState
	}
	return event, nil
}
