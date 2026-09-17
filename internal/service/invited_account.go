package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/netip"
)

func (s *AuthenticationService) WithInvitedAccount(u *biz.InvitedAccountUsecase) *AuthenticationService {
	s.invitedAccount = u
	return s
}
func (s *AuthenticationService) RequestInvitedAccountVerification(ctx context.Context, r *iamv1.RequestInvitedAccountVerificationRequest) (*iamv1.RequestInvitedAccountVerificationResponse, error) {
	if r == nil {
		return nil, invalidArgumentStatus("request", "verification request is required")
	}
	ip, err := netip.ParseAddr(r.GetSourceIp())
	if err != nil {
		return nil, invalidArgumentStatus("source_ip", "trusted source IP is required")
	}
	if s.invitedAccount == nil {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{Dependency: "authentication"})
	}
	result, err := s.invitedAccount.RequestVerification(ctx, biz.InvitedAccountVerificationRequest{Account: r.GetAccount(), IdempotencyKey: r.GetIdempotencyKey(), SourceIP: ip})
	if errors.Is(err, biz.ErrInvitedAccountInvalid) {
		return nil, invalidArgumentStatus("request", "invalid account verification request")
	}
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "requestInvitedAccountVerification", CredentialKind: "email_verification", IdempotencyKey: r.GetIdempotencyKey(), Dependency: "authentication"})
	}
	return &iamv1.RequestInvitedAccountVerificationResponse{ChallengeId: result.ChallengeID.String(), ExpiresAt: timestamppb.New(result.ExpiresAt)}, nil
}
func (s *AuthenticationService) CompleteInvitedAccount(ctx context.Context, r *iamv1.CompleteInvitedAccountRequest) (*iamv1.CompleteInvitedAccountResponse, error) {
	if r == nil {
		return nil, invalidArgumentStatus("request", "account completion request is required")
	}
	id, err := uuid.Parse(r.GetChallengeId())
	if err != nil || id.Version() != 7 {
		return nil, invalidArgumentStatus("challenge_id", "valid verification challenge is required")
	}
	ip, err := netip.ParseAddr(r.GetSourceIp())
	if err != nil {
		return nil, invalidArgumentStatus("source_ip", "trusted source IP is required")
	}
	if s.invitedAccount == nil {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{Dependency: "authentication"})
	}
	err = s.invitedAccount.Complete(ctx, biz.InvitedAccountCompletionRequest{ChallengeID: id, Account: r.GetAccount(), VerificationCode: r.GetVerificationCode(), NewPassword: r.GetNewPassword(), IdempotencyKey: r.GetIdempotencyKey(), SourceIP: ip})
	if errors.Is(err, biz.ErrInvitedAccountInvalid) {
		return nil, invalidArgumentStatus("request", "invalid account completion request")
	}
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "completeInvitedAccount", CredentialKind: "email_verification", IdempotencyKey: r.GetIdempotencyKey(), Dependency: "authentication"})
	}
	return &iamv1.CompleteInvitedAccountResponse{}, nil
}
