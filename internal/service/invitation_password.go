package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"net/netip"
)

func (s *AuthenticationService) WithInvitationPassword(u *biz.InvitationPasswordUsecase) *AuthenticationService {
	s.invitationPassword = u
	return s
}
func (s *AuthenticationService) AcceptInvitationWithPassword(ctx context.Context, r *iamv1.AcceptInvitationWithPasswordRequest) (*iamv1.AcceptInvitationWithPasswordResponse, error) {
	if r == nil {
		return nil, invalidArgumentStatus("request", "invitation acceptance is required")
	}
	ip, err := netip.ParseAddr(r.GetSourceIp())
	if err != nil {
		return nil, invalidArgumentStatus("source_ip", "trusted source IP is required")
	}
	if s.invitationPassword == nil {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{Dependency: "authentication"})
	}
	result, err := s.invitationPassword.Accept(ctx, biz.InvitationPasswordCommand{Account: r.GetAccount(), Password: r.GetPassword(), InvitationToken: r.GetInvitationToken(), IdempotencyKey: r.GetIdempotencyKey(), SourceIP: ip})
	if errors.Is(err, biz.ErrInvitedAccountInvalid) {
		return nil, invalidArgumentStatus("request", "invalid invitation acceptance request")
	}
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "acceptInvitationWithPassword", CredentialKind: "password", IdempotencyKey: r.GetIdempotencyKey(), Dependency: "authentication"})
	}
	tenant := ""
	if result.Target.TenantID != uuid.Nil {
		tenant = result.Target.TenantID.String()
	}
	return &iamv1.AcceptInvitationWithPasswordResponse{Boundary: string(result.Target.Boundary), TenantId: tenant, InvitationId: result.Target.InvitationID.String(), MembershipId: result.Membership.ID.String(), Version: uint64(result.Membership.Version), AuditEventId: result.AuditEventID.String()}, nil
}
