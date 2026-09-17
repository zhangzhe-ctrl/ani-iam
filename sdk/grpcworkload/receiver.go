package grpcworkload

import (
	"context"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ReceiverInterceptor only admits registered methods after live IAM validation.
// Register health/reflection on a separate administrative listener if needed;
// unknown methods do not gain a bypass on the protected receiver transport.
func (c *Client) ReceiverInterceptor(targets []Target) (grpc.UnaryServerInterceptor, error) {
	return c.receiverInterceptor(targets, nil)
}

func (c *Client) receiverInterceptor(targets []Target, ownerCheck func(context.Context, proto.Message, Verified) error) (grpc.UnaryServerInterceptor, error) {
	for _, t := range targets {
		registration, err := c.registeredTarget(t)
		if err != nil {
			return nil, err
		}
		for _, source := range registration.Sources {
			if source.OwnerCheck == "receiver" && ownerCheck == nil {
				return nil, ErrConfiguration
			}
		}
	}
	index, err := targetIndex(targets)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		target, ok := index[info.FullMethod]
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "target is not registered")
		}
		observed, err := verifiedPeer(ctx, c.cfg.Environment, c.cfg.TrustDomain)
		if err != nil {
			return nil, err
		}
		message, ok := req.(proto.Message)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "protobuf request is required")
		}
		binding, registration, source, err := c.bindTarget(target, message)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "request binding is invalid")
		}
		incoming, _ := metadata.FromIncomingContext(ctx)
		wat, delegation := incoming.Get(workloadMetadata), incoming.Get(delegationMetadata)
		if len(wat) != 1 || len(delegation) != 1 || wat[0] == "" || delegation[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "one Workload credential and delegation are required")
		}
		call, cancel := c.deadline(ctx)
		reply, err := c.authorization.VerifyWorkloadInvocation(call, &iamv1.VerifyWorkloadInvocationRequest{WorkloadToken: wat[0], Delegation: delegation[0], Binding: binding, ObservedPeer: observed})
		cancel()
		if err != nil {
			return nil, err
		}
		if reply.GetCaller().GetPrincipalId() == "" || !proto.Equal(reply.GetCaller().GetPeer(), observed) || !proto.Equal(reply.GetBinding(), binding) || reply.GetSubject().GetPrincipalId() != binding.GetSubjectId() || reply.GetSubject().GetBoundary().GetTenant().GetTenantId() != binding.GetTenantId() || reply.GetExpiresAt() == nil || !time.Now().Before(reply.GetExpiresAt().AsTime()) {
			return nil, status.Error(codes.PermissionDenied, "IAM verification does not match the current request")
		}
		if !registeredPrincipalAllowed(source, reply.GetSubject()) || (!registration.Continuation && reply.GetContinuation() != "") || (registration.Continuation && (reply.GetContinuation() == "" || reply.GetContinuationExpiresAt() == nil)) || (reply.GetSubject().GetPrincipalType() == iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD && reply.GetApiKeyId() == "") {
			return nil, status.Error(codes.PermissionDenied, "registered invocation verification is incomplete")
		}
		verified := Verified{apiKeyID: reply.GetApiKeyId(), continuation: reply.GetContinuation(), continuationExpires: reply.GetContinuationExpiresAt().AsTime(), caller: proto.Clone(reply.GetCaller()).(*iamv1.DirectWorkloadCaller), subject: proto.Clone(reply.GetSubject()).(*iamv1.PrincipalContext), binding: proto.Clone(binding).(*iamv1.InvocationBinding)}
		// Authentication metadata is unavailable to ordinary business handlers.
		sanitized := incoming.Copy()
		for _, name := range []string{"authorization", "cookie", "proxy-authorization"} {
			sanitized.Delete(name)
		}
		sanitized.Delete(workloadMetadata)
		sanitized.Delete(delegationMetadata)
		ctx = metadata.NewIncomingContext(ctx, sanitized)
		if source.OwnerCheck == "receiver" {
			if err := ownerCheck(ctx, message, verified); err != nil {
				return nil, err
			}
		}
		return handler(context.WithValue(ctx, verifiedKey{}, verified), req)
	}, nil
}
