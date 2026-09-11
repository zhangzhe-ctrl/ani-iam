package grpcworkload

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestWorkloadOnlyCallerRemovesAmbientCredentialsAndNeverRetries(t *testing.T) {
	mints, calls := 0, 0
	interceptor, err := WorkloadOnlyCallerInterceptor(func(ctx context.Context, target WorkloadTarget) (string, error) {
		mints++
		if target.Operation != "notification.submit" {
			t.Fatal("wrong mint target")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("mint unbounded")
		}
		return "only-workload", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "human", "cookie", "refresh", delegationMetadata, "delegated", "trace-id", "safe"))
	err = interceptor(ctx, "/notification.v1.NotificationService/SubmitNotification", nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		calls++
		md, _ := metadata.FromOutgoingContext(ctx)
		if len(md.Get("authorization"))+len(md.Get("cookie"))+len(md.Get(delegationMetadata)) != 0 || len(md.Get(workloadMetadata)) != 1 || md.Get("trace-id")[0] != "safe" {
			t.Fatal("credential boundary failed")
		}
		return status.Error(codes.Unavailable, "response lost")
	})
	if status.Code(err) != codes.Unavailable || mints != 1 || calls != 1 {
		t.Fatal("caller retried or changed error")
	}
	if err = interceptor(ctx, "/unknown", nil, nil, nil, nil); status.Code(err) != codes.PermissionDenied || mints != 1 {
		t.Fatal("unregistered method minted")
	}
}

type workloadVerifyFake struct {
	iamv1.AuthorizationServiceClient
	calls  int
	fail   bool
	mutate func(*iamv1.VerifyWorkloadCallerResponse)
}

func (f *workloadVerifyFake) VerifyWorkloadCaller(_ context.Context, r *iamv1.VerifyWorkloadCallerRequest, _ ...grpc.CallOption) (*iamv1.VerifyWorkloadCallerResponse, error) {
	f.calls++
	if f.fail {
		return nil, status.Error(codes.Unavailable, "IAM failure")
	}
	reply := &iamv1.VerifyWorkloadCallerResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: "iam-workload", BindingId: "binding", PrincipalVersion: 1, BindingVersion: 1, GrantVersion: 1, Peer: r.ObservedPeer}, Audience: r.Audience, OperationId: r.OperationId, RpcMethod: r.RpcMethod, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	if f.mutate != nil {
		f.mutate(reply)
	}
	return reply, nil
}
func TestWorkloadOnlyReceiverRequiresOnlineExactProofAndSanitizesContext(t *testing.T) {
	fake := &workloadVerifyFake{}
	c := &WorkloadOnlyClient{client: &Client{authorization: fake, cfg: ClientConfig{Environment: "wr20", TrustDomain: "wr20.test", Timeout: time.Second}}}
	target, _ := NotificationTarget("/notification.v1.NotificationService/SubmitNotification")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(workloadMetadata, "test-wat", "trace-id", "safe"))
	if _, _, err := c.VerifyCaller(ctx, target); status.Code(err) != codes.Unauthenticated || fake.calls != 0 {
		t.Fatal("TLS missing admitted")
	}
	cert := &x509.Certificate{DNSNames: []string{"ani-iam"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
	clean, caller, err := c.VerifyCaller(ctx, target)
	if err != nil || caller.PrincipalID() != "iam-workload" || fake.calls != 1 {
		t.Fatal("online verification failed")
	}
	md, _ := metadata.FromIncomingContext(clean)
	if len(md.Get(workloadMetadata)) != 0 || md.Get("trace-id")[0] != "safe" {
		t.Fatal("proof leaked")
	}
	for _, mutate := range []func(*iamv1.VerifyWorkloadCallerResponse){func(r *iamv1.VerifyWorkloadCallerResponse) { r.RpcMethod = "other" }, func(r *iamv1.VerifyWorkloadCallerResponse) { r.Caller.Peer = nil }, func(r *iamv1.VerifyWorkloadCallerResponse) {
		r.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
	}, func(r *iamv1.VerifyWorkloadCallerResponse) { r.Caller.GrantVersion = 0 }} {
		fake.mutate = mutate
		if _, _, err = c.VerifyCaller(ctx, target); status.Code(err) != codes.PermissionDenied {
			t.Fatal("mismatched IAM evidence admitted")
		}
	}
	fake.mutate = nil
	fake.fail = true
	if _, _, err = c.VerifyCaller(ctx, target); status.Code(err) != codes.Unavailable {
		t.Fatal("IAM outage admitted")
	}
}
