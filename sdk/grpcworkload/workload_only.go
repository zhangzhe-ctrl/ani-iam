package grpcworkload

import (
	"context"
	"crypto/tls"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"time"
)

const NotificationAudience = "ani-notification-service"

type WorkloadTarget struct{ Audience, Operation, RPCMethod string }

func NotificationTarget(method string) (WorkloadTarget, error) {
	operation := ""
	switch method {
	case "/notification.v1.NotificationService/SubmitNotification":
		operation = "notification.submit"
	case "/notification.v1.NotificationService/GetSubmissionStatus":
		operation = "notification.get_own"
	default:
		return WorkloadTarget{}, status.Error(codes.PermissionDenied, "Workload method is not registered")
	}
	return WorkloadTarget{Audience: NotificationAudience, Operation: operation, RPCMethod: method}, nil
}
func (t WorkloadTarget) validate() error {
	expected, err := NotificationTarget(t.RPCMethod)
	if err != nil || expected != t {
		return ErrConfiguration
	}
	return nil
}

// WorkloadCaller carries no Human, Tenant or credential and has no public
// constructor. It is produced only by online receiver verification.
type WorkloadCaller struct{ principalID string }

func (c WorkloadCaller) PrincipalID() string { return c.principalID }

type WorkloadOnlyClient struct{ client *Client }

func NewWorkloadOnlyClient(cfg ClientConfig) (*WorkloadOnlyClient, error) {
	c, err := newClient(cfg, false)
	if err != nil {
		return nil, err
	}
	return &WorkloadOnlyClient{client: c}, nil
}
func (c *WorkloadOnlyClient) Close() error { return c.client.Close() }

// Check probes the receiver's current mTLS identity, health and verification
// Grants. The empty credential must reach the verifier and be rejected with the
// exact credential error. This negative probe never authorizes a business call.
func (c *WorkloadOnlyClient) Check(ctx context.Context) error {
	call, cancel := c.client.deadline(ctx)
	defer cancel()
	reply, err := healthv1.NewHealthClient(c.client.conn).Check(call, &healthv1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if reply.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		return status.Error(codes.Unavailable, "IAM is not ready")
	}
	target, _ := NotificationTarget("/notification.v1.NotificationService/SubmitNotification")
	_, err = c.client.authorization.VerifyWorkloadCaller(call, &iamv1.VerifyWorkloadCallerRequest{
		Audience: target.Audience, OperationId: target.Operation, RpcMethod: target.RPCMethod,
		ObservedPeer: &iamv1.WorkloadPeer{Environment: c.client.cfg.Environment, TrustDomain: c.client.cfg.TrustDomain},
	})
	if status.Code(err) == codes.Unauthenticated {
		for _, detail := range status.Convert(err).Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "iam.ani.internal" && info.Reason == "CREDENTIAL_INVALID" {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}
	return status.Error(codes.Unavailable, "IAM verification probe did not reject its empty credential")
}
func (c *WorkloadOnlyClient) VerifyCaller(ctx context.Context, target WorkloadTarget) (context.Context, WorkloadCaller, error) {
	if err := target.validate(); err != nil {
		return ctx, WorkloadCaller{}, err
	}
	observed, err := verifiedPeer(ctx, c.client.cfg.Environment, c.client.cfg.TrustDomain)
	if err != nil {
		return ctx, WorkloadCaller{}, err
	}
	md, _ := metadata.FromIncomingContext(ctx)
	wat := md.Get(workloadMetadata)
	if len(wat) != 1 || wat[0] == "" || len(md.Get(delegationMetadata)) != 0 || len(md.Get("authorization")) != 0 || len(md.Get("cookie")) != 0 {
		return ctx, WorkloadCaller{}, status.Error(codes.Unauthenticated, "one Workload-only credential is required")
	}
	call, cancel := c.client.deadline(ctx)
	defer cancel()
	reply, err := c.client.authorization.VerifyWorkloadCaller(call, &iamv1.VerifyWorkloadCallerRequest{WorkloadToken: wat[0], Audience: target.Audience, OperationId: target.Operation, RpcMethod: target.RPCMethod, ObservedPeer: observed})
	if err != nil {
		return ctx, WorkloadCaller{}, err
	}
	caller := reply.GetCaller()
	if caller.GetPrincipalId() == "" || caller.GetBindingId() == "" || caller.GetPrincipalVersion() <= 0 || caller.GetBindingVersion() <= 0 || caller.GetGrantVersion() <= 0 || !proto.Equal(caller.GetPeer(), observed) || reply.GetAudience() != target.Audience || reply.GetOperationId() != target.Operation || reply.GetRpcMethod() != target.RPCMethod || reply.GetExpiresAt() == nil || reply.GetExpiresAt().CheckValid() != nil || !time.Now().Before(reply.GetExpiresAt().AsTime()) {
		return ctx, WorkloadCaller{}, status.Error(codes.PermissionDenied, "IAM verification did not match the call")
	}
	sanitized := md.Copy()
	for _, key := range []string{workloadMetadata, delegationMetadata, "authorization", "proxy-authorization", "cookie"} {
		sanitized.Delete(key)
	}
	return metadata.NewIncomingContext(ctx, sanitized), WorkloadCaller{principalID: caller.GetPrincipalId()}, nil
}

// WorkloadTokenSource supplies one currently authorized short-lived credential.
// IAM's own dispatcher supplies its local issuer without recursively dialing IAM.
type WorkloadTokenSource func(context.Context, WorkloadTarget) (string, error)

func WorkloadOnlyCallerInterceptor(source WorkloadTokenSource) (grpc.UnaryClientInterceptor, error) {
	if source == nil {
		return nil, ErrConfiguration
	}
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		target, err := NotificationTarget(method)
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		token, err := source(call, target)
		if err != nil {
			return err
		}
		if token == "" {
			return status.Error(codes.Unauthenticated, "Workload credential is required")
		}
		md, _ := metadata.FromOutgoingContext(call)
		md = md.Copy()
		for _, key := range []string{"authorization", "cookie", "proxy-authorization", delegationMetadata, workloadMetadata} {
			md.Delete(key)
		}
		md.Set(workloadMetadata, token)
		return invoke(metadata.NewOutgoingContext(call, md), method, req, reply, cc, opts...)
	}, nil
}
func (f TLSFiles) ServerTLSConfig() (*tls.Config, error) { return f.config("", true) }
