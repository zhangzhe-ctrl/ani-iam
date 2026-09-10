package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workloadInvocationUsecase interface {
	VerifyContinuation(context.Context, string, biz.InvocationBinding) (biz.VerifiedInvocation, error)
	IssueWorkloadToken(context.Context, biz.WorkloadTarget) (biz.IssuedWorkloadCredential, error)
	IssueDelegation(context.Context, string, string, biz.InvocationBinding) (biz.IssuedWorkloadCredential, error)
	Verify(context.Context, string, string, biz.InvocationBinding, biz.VerifiedWorkloadPeer) (biz.VerifiedInvocation, error)
}

func NewAuthenticationServiceWithWorkload(auth authenticationUsecase, oidc oidcUsecase, workload workloadInvocationUsecase) *AuthenticationService {
	s := NewAuthenticationService(auth, oidc)
	s.workload = workload
	return s
}
func NewAuthorizationServiceWithWorkload(auth authorizationUsecase, workload workloadInvocationUsecase) *AuthorizationService {
	s := NewAuthorizationService(auth)
	s.workload = workload
	return s
}

func (s *AuthenticationService) IssueWorkloadToken(ctx context.Context, r *iamv1.IssueWorkloadTokenRequest) (*iamv1.IssueWorkloadTokenResponse, error) {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 {
		return nil, invalidArgumentStatus("request", "Workload token target is required")
	}
	if s.workload == nil {
		return nil, invocationStatus(biz.ErrPersistenceUnavailable)
	}
	issued, err := s.workload.IssueWorkloadToken(ctx, biz.WorkloadTarget{Audience: r.GetAudience(), Operation: r.GetOperationId()})
	if err != nil {
		return nil, invocationStatus(err)
	}
	return &iamv1.IssueWorkloadTokenResponse{WorkloadToken: issued.Value, ExpiresAt: timestamppb.New(issued.ExpiresAt), PrincipalId: issued.PrincipalID.String()}, nil
}

func (s *AuthenticationService) IssueDelegation(ctx context.Context, r *iamv1.IssueDelegationRequest) (*iamv1.IssueDelegationResponse, error) {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 {
		return nil, invalidArgumentStatus("request", "delegation request is required")
	}
	binding, err := invocationBindingFromProto(r.GetBinding())
	if err != nil {
		return nil, invocationStatus(err)
	}
	if s.workload == nil {
		return nil, invocationStatus(biz.ErrPersistenceUnavailable)
	}
	raw := ""
	if r.GetSubjectCredential() != nil {
		raw = r.GetSubjectCredential().GetValue()
	}
	issued, err := s.workload.IssueDelegation(ctx, r.GetWorkloadToken(), raw, binding)
	if err != nil {
		return nil, invocationStatus(err)
	}
	return &iamv1.IssueDelegationResponse{Delegation: issued.Value, ExpiresAt: timestamppb.New(issued.ExpiresAt)}, nil
}

func (s *AuthorizationService) VerifyWorkloadInvocation(ctx context.Context, r *iamv1.VerifyWorkloadInvocationRequest) (*iamv1.VerifyWorkloadInvocationResponse, error) {
	if r == nil || r.GetObservedPeer() == nil || len(r.ProtoReflect().GetUnknown()) != 0 {
		return nil, invalidArgumentStatus("request", "receiver evidence is required")
	}
	binding, err := invocationBindingFromProto(r.GetBinding())
	if err != nil {
		return nil, invocationStatus(err)
	}
	if s.workload == nil {
		return nil, invocationStatus(biz.ErrPersistenceUnavailable)
	}
	peer := r.GetObservedPeer()
	if len(peer.ProtoReflect().GetUnknown()) != 0 {
		return nil, invocationStatus(biz.ErrInvocationInvalid)
	}
	result, err := s.workload.Verify(ctx, r.GetWorkloadToken(), r.GetDelegation(), binding, biz.VerifiedWorkloadPeer{Environment: peer.GetEnvironment(), TrustDomain: peer.GetTrustDomain(), IdentityKind: peer.GetIdentityKind(), IdentityValue: peer.GetIdentityValue()})
	if err != nil {
		return nil, invocationStatus(err)
	}
	c := result.Caller
	i := c.Identity
	return &iamv1.VerifyWorkloadInvocationResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: i.PrincipalID.String(), BindingId: i.BindingID.String(), PrincipalVersion: i.PrincipalVersion, BindingVersion: i.BindingVersion, GrantVersion: c.GrantVersion, Peer: &iamv1.WorkloadPeer{Environment: i.Peer.Environment, TrustDomain: i.Peer.TrustDomain, IdentityKind: i.Peer.IdentityKind, IdentityValue: i.Peer.IdentityValue}}, Subject: trustedPrincipalToProto(result.Subject), Binding: invocationBindingToProto(result.Binding), ExpiresAt: timestamppb.New(result.ExpiresAt), Continuation: result.Continuation, ContinuationExpiresAt: timestamppb.New(result.ContinuationExpiresAt)}, nil
}

func invocationBindingFromProto(r *iamv1.InvocationBinding) (biz.InvocationBinding, error) {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 || len(r.GetRequestSha256()) != 32 {
		return biz.InvocationBinding{}, biz.ErrInvocationInvalid
	}
	tenant, err := uuid.Parse(r.GetTenantId())
	if err != nil || tenant.String() != r.GetTenantId() {
		return biz.InvocationBinding{}, biz.ErrInvocationInvalid
	}
	subject, err := uuid.Parse(r.GetSubjectId())
	if err != nil || subject.String() != r.GetSubjectId() {
		return biz.InvocationBinding{}, biz.ErrInvocationInvalid
	}
	b := biz.InvocationBinding{Audience: r.GetAudience(), Operation: r.GetOperationId(), RPCMethod: r.GetRpcMethod(), SourceOperation: r.GetSourceOperationId(), TenantID: tenant, SubjectID: subject, ResourceID: r.GetResourceId(), Mode: r.GetMode(), PolicyRevision: r.GetPolicyRevision()}
	copy(b.RequestSHA256[:], r.GetRequestSha256())
	return b, b.Validate()
}

func invocationBindingToProto(b biz.InvocationBinding) *iamv1.InvocationBinding {
	return &iamv1.InvocationBinding{Audience: b.Audience, OperationId: b.Operation, RpcMethod: b.RPCMethod, SourceOperationId: b.SourceOperation, TenantId: b.TenantID.String(), SubjectId: b.SubjectID.String(), ResourceId: b.ResourceID, Mode: b.Mode, RequestSha256: append([]byte(nil), b.RequestSHA256[:]...), PolicyRevision: b.PolicyRevision}
}

func invocationStatus(err error) error {
	code, reason := codes.Unavailable, "IAM_UNAVAILABLE"
	switch {
	case errors.Is(err, context.Canceled):
		code, reason = codes.Canceled, "REQUEST_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		code, reason = codes.DeadlineExceeded, "IAM_TIMEOUT"
	case errors.Is(err, biz.ErrInvocationInvalid):
		code, reason = codes.InvalidArgument, "INVALID_ARGUMENT"
	case errors.Is(err, biz.ErrInvocationCredentialInvalid), errors.Is(err, biz.ErrWorkloadIdentityInvalid), errors.Is(err, biz.ErrAuthorizationCredentialInvalid), errors.Is(err, biz.ErrAuthorizationCredentialRequired):
		code, reason = codes.Unauthenticated, "CREDENTIAL_INVALID"
	case errors.Is(err, biz.ErrWorkloadPermissionDenied):
		code, reason = codes.PermissionDenied, "PERMISSION_DENIED"
	case errors.Is(err, biz.ErrAuthorizationPolicyMismatch):
		code, reason = codes.FailedPrecondition, "POLICY_REVISION_MISMATCH"
	case errors.Is(err, biz.ErrAuthorizationOperationUnregistered):
		code, reason = codes.PermissionDenied, "OPERATION_UNREGISTERED"
	}
	return newIAMStatus(code, reason, "Workload invocation could not be authorized", nil)
}

func (s *AuthorizationService) VerifySessionContinuation(ctx context.Context, r *iamv1.VerifySessionContinuationRequest) (*iamv1.VerifySessionContinuationResponse, error) {
	if r == nil || len(r.ProtoReflect().GetUnknown()) != 0 {
		return nil, invalidArgumentStatus("request", "continuation request is required")
	}
	binding, err := invocationBindingFromProto(r.GetBinding())
	if err != nil {
		return nil, invocationStatus(err)
	}
	if s.workload == nil {
		return nil, invocationStatus(biz.ErrPersistenceUnavailable)
	}
	result, err := s.workload.VerifyContinuation(ctx, r.GetContinuation(), binding)
	if err != nil {
		return nil, invocationStatus(err)
	}
	c := result.Caller
	i := c.Identity
	return &iamv1.VerifySessionContinuationResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: i.PrincipalID.String(), BindingId: i.BindingID.String(), PrincipalVersion: i.PrincipalVersion, BindingVersion: i.BindingVersion, GrantVersion: c.GrantVersion, Peer: &iamv1.WorkloadPeer{Environment: i.Peer.Environment, TrustDomain: i.Peer.TrustDomain, IdentityKind: i.Peer.IdentityKind, IdentityValue: i.Peer.IdentityValue}}, Subject: trustedPrincipalToProto(result.Subject), Binding: invocationBindingToProto(result.Binding), ExpiresAt: timestamppb.New(result.ExpiresAt)}, nil
}
