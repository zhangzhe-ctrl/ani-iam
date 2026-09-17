package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func platformInvitationDTO(v biz.PlatformInvitation) *iamv1.Invitation {
	result := invitationMetadataDTO(biz.TenantInvitation(v))
	result.Boundary = &iamv1.Boundary{Boundary: &iamv1.Boundary_Platform{Platform: &iamv1.PlatformBoundary{}}}
	return result
}
func (s *IAMAdminService) CreatePlatformInvitation(ctx context.Context, r *iamv1.CreatePlatformInvitationRequest) (*iamv1.CreatePlatformInvitationResponse, error) {
	const op = "createPlatformIAMInvitation"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "CreatePlatformInvitation", op, "", "PLATFORM_INVITATION_ADMINISTRATION")
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
	result, err := s.platform.CreateInvitation(ctx, cap, biz.CreatePlatformInvitationCommand{Email: r.GetNormalizedEmail(), RoleIDs: roles, Locale: r.GetLocale(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op})
	}
	return &iamv1.CreatePlatformInvitationResponse{Invitation: platformInvitationDTO(*result.Invitation)}, nil
}
func (s *IAMAdminService) GetPlatformInvitation(ctx context.Context, r *iamv1.GetPlatformInvitationRequest) (*iamv1.GetPlatformInvitationResponse, error) {
	const op = "getPlatformIAMInvitation"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "GetPlatformInvitation", op, r.GetInvitationId(), "PLATFORM_INVITATION_ADMINISTRATION")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.GetInvitation(ctx, cap, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.GetPlatformInvitationResponse{Invitation: platformInvitationDTO(result)}, nil
}
func (s *IAMAdminService) ListPlatformInvitations(ctx context.Context, r *iamv1.ListPlatformInvitationsRequest) (*iamv1.ListPlatformInvitationsResponse, error) {
	const op = "listPlatformIAMInvitations"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ListPlatformInvitations", op, "", "PLATFORM_INVITATION_ADMINISTRATION")
	if err != nil {
		return nil, err
	}
	filter, ok := invitationStatusFromDTO(r.GetStatus())
	if !ok {
		return nil, invalidArgumentStatus("status", "IAM request is invalid")
	}
	result, err := s.platform.ListInvitations(ctx, cap, filter, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op})
	}
	out := &iamv1.ListPlatformInvitationsResponse{Invitations: []*iamv1.Invitation{}, NextCursor: result.NextCursor}
	for _, v := range result.Items {
		out.Invitations = append(out.Invitations, platformInvitationDTO(v))
	}
	return out, nil
}
func (s *IAMAdminService) ResendPlatformInvitation(ctx context.Context, r *iamv1.ResendPlatformInvitationRequest) (*iamv1.ResendPlatformInvitationResponse, error) {
	const op = "resendPlatformIAMInvitation"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ResendPlatformInvitation", op, r.GetInvitationId(), "PLATFORM_INVITATION_ADMINISTRATION")
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
	result, err := s.platform.ResendInvitation(ctx, cap, biz.ChangePlatformInvitationCommand{ID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.ResendPlatformInvitationResponse{Invitation: platformInvitationDTO(*result.Invitation)}, nil
}
func (s *IAMAdminService) CancelPlatformInvitation(ctx context.Context, r *iamv1.CancelPlatformInvitationRequest) (*iamv1.CancelPlatformInvitationResponse, error) {
	const op = "cancelPlatformIAMInvitation"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "CancelPlatformInvitation", op, r.GetInvitationId(), "PLATFORM_INVITATION_ADMINISTRATION")
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
	result, err := s.platform.CancelInvitation(ctx, cap, biz.ChangePlatformInvitationCommand{ID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.CancelPlatformInvitationResponse{Invitation: platformInvitationDTO(*result.Invitation)}, nil
}
