package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
)

func (s *IAMAdminService) WithTenantInvitations(u *biz.TenantInvitationUsecase) *IAMAdminService {
	s.invitations = u
	return s
}
func (s *IAMAdminService) tenantInvitationContext(ctx context.Context, credential *iamv1.BearerCredential, method, operation, tenant, resource string) (context.Context, biz.TenantScope, uuid.UUID, biz.TenantAuthorizationActor, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, credential, method, operation, tenant, resource)
	if err != nil {
		return ctx, biz.TenantScope{}, uuid.Nil, biz.TenantAuthorizationActor{}, err
	}
	if s.invitations == nil {
		return ctx, biz.TenantScope{}, uuid.Nil, biz.TenantAuthorizationActor{}, status.Error(codes.Unimplemented, "tenant invitation administration is unavailable")
	}
	scope, id, err := trustedTenantScope(ctx, operation)
	if err != nil {
		return ctx, scope, id, biz.TenantAuthorizationActor{}, err
	}
	actor, err := tenantAuthorizationActor(ctx)
	return ctx, scope, id, actor, err
}
func (s *IAMAdminService) CreateTenantInvitation(ctx context.Context, r *iamv1.CreateTenantInvitationRequest) (*iamv1.CreateTenantInvitationResponse, error) {
	ctx, scope, tenant, actor, err := s.tenantInvitationContext(ctx, r.GetCredential(), "CreateTenantInvitation", "createTenantIAMInvitation", r.GetTenantId(), "")
	if err != nil {
		return nil, err
	}
	roles := make([]uuid.UUID, 0, len(r.GetRoleIds()))
	for _, raw := range r.GetRoleIds() {
		id, e := requiredUUID(raw, "role_ids")
		if e != nil {
			return nil, e
		}
		roles = append(roles, id)
	}
	result, err := s.invitations.Create(ctx, scope, biz.CreateTenantInvitationCommand{Email: r.GetNormalizedEmail(), RoleIDs: roles, Locale: r.GetLocale(), IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "createTenantIAMInvitation", TenantID: tenant.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.CreateTenantInvitationResponse{Invitation: tenantInvitationDTO(tenant, result.Invitation)}, nil
}
func (s *IAMAdminService) GetTenantInvitation(ctx context.Context, r *iamv1.GetTenantInvitationRequest) (*iamv1.GetTenantInvitationResponse, error) {
	ctx, scope, tenant, actor, err := s.tenantInvitationContext(ctx, r.GetCredential(), "GetTenantInvitation", "getTenantIAMInvitation", r.GetTenantId(), r.GetInvitationId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.invitations.Get(ctx, scope, actor, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getTenantIAMInvitation", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.GetTenantInvitationResponse{Invitation: tenantInvitationDTO(tenant, result)}, nil
}
func (s *IAMAdminService) ListTenantInvitations(ctx context.Context, r *iamv1.ListTenantInvitationsRequest) (*iamv1.ListTenantInvitationsResponse, error) {
	ctx, scope, tenant, actor, err := s.tenantInvitationContext(ctx, r.GetCredential(), "ListTenantInvitations", "listTenantIAMInvitations", r.GetTenantId(), "")
	if err != nil {
		return nil, err
	}
	filter, ok := invitationStatusFromDTO(r.GetStatus())
	if !ok {
		return nil, invalidArgumentStatus("status", "IAM request is invalid")
	}
	result, err := s.invitations.List(ctx, scope, actor, filter, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listTenantIAMInvitations", TenantID: tenant.String(), DecisionID: actor.DecisionID})
	}
	out := &iamv1.ListTenantInvitationsResponse{Invitations: []*iamv1.Invitation{}, NextCursor: result.NextCursor}
	for _, v := range result.Items {
		out.Invitations = append(out.Invitations, tenantInvitationDTO(tenant, v))
	}
	return out, nil
}
func (s *IAMAdminService) ResendTenantInvitation(ctx context.Context, r *iamv1.ResendTenantInvitationRequest) (*iamv1.ResendTenantInvitationResponse, error) {
	ctx, scope, tenant, actor, err := s.tenantInvitationContext(ctx, r.GetCredential(), "ResendTenantInvitation", "resendTenantIAMInvitation", r.GetTenantId(), r.GetInvitationId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	result, err := s.invitations.Resend(ctx, scope, biz.ChangeTenantInvitationCommand{ID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "resendTenantIAMInvitation", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.ResendTenantInvitationResponse{Invitation: tenantInvitationDTO(tenant, result.Invitation)}, nil
}
func (s *IAMAdminService) CancelTenantInvitation(ctx context.Context, r *iamv1.CancelTenantInvitationRequest) (*iamv1.CancelTenantInvitationResponse, error) {
	ctx, scope, tenant, actor, err := s.tenantInvitationContext(ctx, r.GetCredential(), "CancelTenantInvitation", "cancelTenantIAMInvitation", r.GetTenantId(), r.GetInvitationId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	result, err := s.invitations.Cancel(ctx, scope, biz.ChangeTenantInvitationCommand{ID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "cancelTenantIAMInvitation", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.CancelTenantInvitationResponse{Invitation: tenantInvitationDTO(tenant, result.Invitation)}, nil
}
func invitationStatusFromDTO(value iamv1.InvitationStatus) (biz.InvitationStatus, bool) {
	values := map[iamv1.InvitationStatus]biz.InvitationStatus{iamv1.InvitationStatus_INVITATION_STATUS_UNSPECIFIED: "", iamv1.InvitationStatus_INVITATION_STATUS_PENDING: biz.InvitationPending, iamv1.InvitationStatus_INVITATION_STATUS_ACCEPTED: biz.InvitationAccepted, iamv1.InvitationStatus_INVITATION_STATUS_CANCELLED: biz.InvitationCancelled, iamv1.InvitationStatus_INVITATION_STATUS_EXPIRED: biz.InvitationExpired}
	v, ok := values[value]
	return v, ok
}
func tenantInvitationDTO(tenant uuid.UUID, v biz.TenantInvitation) *iamv1.Invitation {
	result := invitationMetadataDTO(v)
	result.Boundary = &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenant.String()}}}
	return result
}
func invitationMetadataDTO(v biz.TenantInvitation) *iamv1.Invitation {
	roles := make([]string, 0, len(v.RoleIDs))
	for _, id := range v.RoleIDs {
		roles = append(roles, id.String())
	}
	state := map[biz.InvitationStatus]iamv1.InvitationStatus{biz.InvitationPending: iamv1.InvitationStatus_INVITATION_STATUS_PENDING, biz.InvitationAccepted: iamv1.InvitationStatus_INVITATION_STATUS_ACCEPTED, biz.InvitationCancelled: iamv1.InvitationStatus_INVITATION_STATUS_CANCELLED, biz.InvitationExpired: iamv1.InvitationStatus_INVITATION_STATUS_EXPIRED}[v.Status]
	local, domain, _ := strings.Cut(v.NormalizedEmail, "@")
	hint := "***@" + domain
	if runes := []rune(local); len(runes) > 1 {
		hint = string(runes[0]) + hint
	}
	return &iamv1.Invitation{InvitationId: v.ID.String(), NormalizedEmailHint: hint, RoleIds: roles, Status: state, ExpiresAt: timestamppb.New(v.ExpiresAt), Version: uint64(v.Version), DeliveryGeneration: uint64(v.DeliveryGeneration), DeliveryStatus: v.DeliveryStatus, DeliveryAttemptCount: uint32(v.DeliveryAttemptCount)}
}
