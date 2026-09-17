package service

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workloadCallerVerifier interface {
	VerifyCaller(context.Context, string, biz.WorkloadTarget, string, biz.VerifiedWorkloadPeer, ...string) (biz.VerifiedWorkloadCaller, error)
}

type workloadHTTPCallerVerifier interface {
	VerifyHTTPCaller(context.Context, string, biz.WorkloadTarget, string, string, biz.VerifiedWorkloadPeer, string) (biz.VerifiedWorkloadCaller, error)
}

func (s *AuthorizationService) VerifyWorkloadCaller(ctx context.Context, r *iamv1.VerifyWorkloadCallerRequest) (*iamv1.VerifyWorkloadCallerResponse, error) {
	if r == nil || r.GetObservedPeer() == nil || len(r.ProtoReflect().GetUnknown()) != 0 || len(r.GetObservedPeer().ProtoReflect().GetUnknown()) != 0 {
		return nil, invocationStatus(biz.ErrInvocationInvalid)
	}
	verifier, ok := s.workload.(workloadCallerVerifier)
	if !ok {
		return nil, invocationStatus(biz.ErrPersistenceUnavailable)
	}
	p := r.GetObservedPeer()
	observed := biz.VerifiedWorkloadPeer{Environment: p.GetEnvironment(), TrustDomain: p.GetTrustDomain(), IdentityKind: p.GetIdentityKind(), IdentityValue: p.GetIdentityValue()}
	target := biz.WorkloadTarget{Audience: r.GetAudience(), Operation: r.GetOperationId()}
	var result biz.VerifiedWorkloadCaller
	var err error
	if r.GetHttpMethod() != "" || r.GetHttpPath() != "" {
		httpVerifier, supported := s.workload.(workloadHTTPCallerVerifier)
		if !supported || r.GetRpcMethod() != "" || r.GetHttpMethod() == "" || r.GetHttpPath() == "" {
			return nil, invocationStatus(biz.ErrInvocationInvalid)
		}
		result, err = httpVerifier.VerifyHTTPCaller(ctx, r.GetWorkloadToken(), target, r.GetHttpMethod(), r.GetHttpPath(), observed, r.GetTargetRevision())
	} else {
		if r.GetRpcMethod() == "" {
			return nil, invocationStatus(biz.ErrInvocationInvalid)
		}
		result, err = verifier.VerifyCaller(ctx, r.GetWorkloadToken(), target, r.GetRpcMethod(), observed, r.GetTargetRevision())
	}
	if err != nil {
		return nil, invocationStatus(err)
	}
	c := result.Caller
	i := c.Identity
	return &iamv1.VerifyWorkloadCallerResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: i.PrincipalID.String(), BindingId: i.BindingID.String(), PrincipalVersion: i.PrincipalVersion, BindingVersion: i.BindingVersion, GrantVersion: c.GrantVersion, Peer: &iamv1.WorkloadPeer{Environment: i.Peer.Environment, TrustDomain: i.Peer.TrustDomain, IdentityKind: i.Peer.IdentityKind, IdentityValue: i.Peer.IdentityValue}}, ExpiresAt: timestamppb.New(result.ExpiresAt), Audience: r.GetAudience(), OperationId: r.GetOperationId(), RpcMethod: r.GetRpcMethod(), HttpMethod: r.GetHttpMethod(), HttpPath: r.GetHttpPath(), AuthorityRevision: result.AuthorityRevision}, nil
}
