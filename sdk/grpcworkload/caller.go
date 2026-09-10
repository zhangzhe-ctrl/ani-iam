package grpcworkload

import (
	"context"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const workloadMetadata = "ani-workload-token"
const delegationMetadata = "ani-delegation"

type CallerConfig struct {
	Address, ServerName string
	TLS                 TLSFiles
	Targets             []Target
}

// DialCaller owns mTLS and authorization attachment. Each protected call mints
// fresh short evidence under current authority; the business RPC is never retried.
func (c *Client) DialCaller(cfg CallerConfig) (*grpc.ClientConn, error) {
	targets, err := targetIndex(cfg.Targets)
	if err != nil {
		return nil, err
	}
	if cfg.TLS == (TLSFiles{}) {
		cfg.TLS = c.cfg.TLS
	}
	tlsConfig, err := cfg.TLS.config(cfg.ServerName, false)
	if err != nil || cfg.Address == "" {
		return nil, ErrConfiguration
	}
	return grpc.NewClient(cfg.Address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDisableRetry(), grpc.WithUnaryInterceptor(c.callerInterceptor(targets)))
}

func (c *Client) callerInterceptor(targets map[string]Target) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
		target, ok := targets[method]
		if !ok {
			return status.Error(codes.PermissionDenied, "target is not registered")
		}
		subject, ok := SubjectFromContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "IAM-authorized subject is required")
		}
		message, ok := req.(proto.Message)
		if !ok {
			return status.Error(codes.InvalidArgument, "protobuf request is required")
		}
		binding, err := target.bind(message, c.cfg.PolicyRevision)
		if err != nil {
			return status.Error(codes.InvalidArgument, "request binding is invalid")
		}
		if subject.policyRevision != binding.GetPolicyRevision() || subject.sourceOperation != binding.GetSourceOperationId() || subject.resourceID != binding.GetResourceId() || subject.principal.GetPrincipalId() != binding.GetSubjectId() || subject.principal.GetBoundary().GetTenant().GetTenantId() != binding.GetTenantId() {
			return status.Error(codes.PermissionDenied, "request does not match the authorized subject and target")
		}
		call, cancel := c.deadline(ctx)
		wat, err := c.authentication.IssueWorkloadToken(call, &iamv1.IssueWorkloadTokenRequest{Audience: target.Audience, OperationId: target.Operation})
		cancel()
		if err != nil {
			return err
		}
		if wat.GetWorkloadToken() == "" || wat.GetExpiresAt() == nil || !time.Now().Before(wat.GetExpiresAt().AsTime()) {
			return status.Error(codes.Unauthenticated, "IAM returned no current Workload credential")
		}
		call, cancel = c.deadline(ctx)
		delegation, err := c.authentication.IssueDelegation(call, &iamv1.IssueDelegationRequest{SubjectCredential: &iamv1.BearerCredential{Value: subject.credential}, WorkloadToken: wat.GetWorkloadToken(), Binding: binding})
		cancel()
		if err != nil {
			return err
		}
		if delegation.GetDelegation() == "" || delegation.GetExpiresAt() == nil || !time.Now().Before(delegation.GetExpiresAt().AsTime()) {
			return status.Error(codes.Unauthenticated, "IAM returned no current delegation")
		}
		outgoing, _ := metadata.FromOutgoingContext(ctx)
		outgoing = outgoing.Copy()
		for _, name := range []string{"authorization", "cookie", "proxy-authorization"} {
			outgoing.Delete(name)
		}
		outgoing.Set(workloadMetadata, wat.GetWorkloadToken())
		outgoing.Set(delegationMetadata, delegation.GetDelegation())
		// The actual request passed to gRPC is the one bound above. No business
		// retry, user bearer propagation, caller identity header or token parsing.
		return invoker(metadata.NewOutgoingContext(ctx, outgoing), method, req, reply, cc, options...)
	}
}
