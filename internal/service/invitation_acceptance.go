package service

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (s *IAMAdminService) WithInvitationAcceptance(u *biz.InvitationAcceptanceUsecase) *IAMAdminService {
	s.invitationAcceptance = u
	return s
}
func acceptedInvitationDTO(r biz.InvitationAcceptanceResult) *iamv1.Membership {
	roles := make([]string, len(r.Membership.RoleIDs))
	for i, id := range r.Membership.RoleIDs {
		roles[i] = id.String()
	}
	var boundary *iamv1.Boundary
	if r.Target.Boundary == biz.AccessBoundaryPlatform {
		boundary = platformBoundaryDTO()
	} else {
		boundary = tenantBoundaryDTO(r.Target.TenantID)
	}
	return &iamv1.Membership{MembershipId: r.Membership.ID.String(), PrincipalId: r.Membership.PrincipalID.String(), PrincipalType: iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN, Boundary: boundary, Status: membershipStatusDTO(r.Membership.Status), RoleIds: roles, Version: uint64(r.Membership.Version)}
}
func (s *IAMAdminService) AcceptTenantInvitation(ctx context.Context, r *iamv1.AcceptTenantInvitationRequest) (*iamv1.AcceptTenantInvitationResponse, error) {
	const op = "acceptTenantIAMInvitation"
	if s.invitationAcceptance == nil {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{OperationID: op})
	}
	tenant, err := requiredUUID(r.GetTenantId(), "tenant_id")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	request, correlation := principalValidationAuditIdentifiers(ctx)
	v, err := s.invitationAcceptance.Accept(ctx, biz.InvitationAcceptanceCommand{Target: biz.InvitationAcceptanceTarget{Boundary: biz.AccessBoundaryTenant, TenantID: tenant, InvitationID: id}, Credential: r.GetCredential().GetValue(), InvitationToken: r.GetInvitationToken(), IdempotencyKey: r.GetIdempotencyKey(), RequestID: request, CorrelationID: correlation})
	if err != nil {
		context := errorContext{OperationID: op, TenantID: tenant.String(), ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()}
		if source, ok := biz.TenantIDFromAuthenticationError(err); ok {
			context.TenantID = source.String()
		}
		return nil, mapIAMError(err, context)
	}
	return &iamv1.AcceptTenantInvitationResponse{Membership: acceptedInvitationDTO(v)}, nil
}
func (s *IAMAdminService) AcceptPlatformInvitation(ctx context.Context, r *iamv1.AcceptPlatformInvitationRequest) (*iamv1.AcceptPlatformInvitationResponse, error) {
	const op = "acceptPlatformIAMInvitation"
	if s.invitationAcceptance == nil {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{OperationID: op})
	}
	id, err := requiredUUID(r.GetInvitationId(), "invitation_id")
	if err != nil {
		return nil, err
	}
	request, correlation := principalValidationAuditIdentifiers(ctx)
	v, err := s.invitationAcceptance.Accept(ctx, biz.InvitationAcceptanceCommand{Target: biz.InvitationAcceptanceTarget{Boundary: biz.AccessBoundaryPlatform, InvitationID: id}, Credential: r.GetCredential().GetValue(), InvitationToken: r.GetInvitationToken(), IdempotencyKey: r.GetIdempotencyKey(), RequestID: request, CorrelationID: correlation})
	if err != nil {
		context := errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()}
		if source, ok := biz.TenantIDFromAuthenticationError(err); ok {
			context.TenantID = source.String()
		}
		return nil, mapIAMError(err, context)
	}
	return &iamv1.AcceptPlatformInvitationResponse{Membership: acceptedInvitationDTO(v)}, nil
}
