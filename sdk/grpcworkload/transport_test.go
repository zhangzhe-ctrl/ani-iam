package grpcworkload

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mintClient struct {
	iamv1.AuthenticationServiceClient
	wat, delegation int
	fail            bool
}

func (m *mintClient) IssueWorkloadToken(context.Context, *iamv1.IssueWorkloadTokenRequest, ...grpc.CallOption) (*iamv1.IssueWorkloadTokenResponse, error) {
	m.wat++
	if m.fail {
		return nil, status.Error(codes.Unavailable, "test dependency failure")
	}
	return &iamv1.IssueWorkloadTokenResponse{WorkloadToken: "test-wat", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}
func (m *mintClient) IssueDelegation(context.Context, *iamv1.IssueDelegationRequest, ...grpc.CallOption) (*iamv1.IssueDelegationResponse, error) {
	m.delegation++
	return &iamv1.IssueDelegationResponse{Delegation: "test-delegation", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}
func testSubject() Subject {
	return Subject{principal: &iamv1.PrincipalContext{PrincipalId: "human", Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: "tenant"}}}}, credential: "test-user-credential", sourceOperation: "createInstanceExecSession", policyRevision: "revision", resourceID: "resource"}
}

func TestCallerNeverRetriesBusinessAndDoesNotForwardUserBearer(t *testing.T) {
	target := bindingTarget()
	mint := &mintClient{}
	c := &Client{authentication: mint, cfg: ClientConfig{PolicyRevision: "revision", Timeout: time.Second}}
	interceptor := c.callerInterceptor(map[string]Target{target.Method: *target})
	req, _ := structpb.NewStruct(map[string]any{"command": "test"})
	calls := 0
	ctx := metadata.NewOutgoingContext(WithSubject(context.Background(), testSubject()), metadata.Pairs("authorization", "test-user-credential", "cookie", "test-cookie", "trace-id", "trace"))
	invoke := func(ctx context.Context, _ string, _ any, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		calls++
		md, _ := metadata.FromOutgoingContext(ctx)
		if len(md.Get("authorization")) != 0 || len(md.Get("cookie")) != 0 || len(md.Get(workloadMetadata)) != 1 || len(md.Get(delegationMetadata)) != 1 || md.Get("trace-id")[0] != "trace" {
			t.Fatal("unsafe/missing outgoing metadata")
		}
		return status.Error(codes.Unavailable, "lost business response")
	}
	err := interceptor(ctx, target.Method, req, nil, nil, invoke)
	if status.Code(err) != codes.Unavailable || calls != 1 || mint.wat != 1 || mint.delegation != 1 {
		t.Fatal("business call was retried or evidence was not obtained once")
	}
	mint.fail = true
	calls = 0
	if err := interceptor(ctx, target.Method, req, nil, nil, invoke); status.Code(err) != codes.Unavailable || calls != 0 {
		t.Fatal("IAM failure reached business invocation")
	}
	mint.fail = false
	changed := testSubject()
	changed.resourceID = "other"
	if err := interceptor(WithSubject(context.Background(), changed), target.Method, req, nil, nil, invoke); status.Code(err) != codes.PermissionDenied || calls != 0 {
		t.Fatal("subject for another resource was accepted")
	}
}

type verifyClient struct {
	iamv1.AuthorizationServiceClient
	calls int
	fail  bool
}

func (v *verifyClient) VerifyWorkloadInvocation(_ context.Context, r *iamv1.VerifyWorkloadInvocationRequest, _ ...grpc.CallOption) (*iamv1.VerifyWorkloadInvocationResponse, error) {
	v.calls++
	if v.fail {
		return nil, status.Error(codes.Unavailable, "test IAM outage")
	}
	return &iamv1.VerifyWorkloadInvocationResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: "gateway", Peer: proto.Clone(r.GetObservedPeer()).(*iamv1.WorkloadPeer)}, Subject: testSubject().Principal(), Binding: proto.Clone(r.GetBinding()).(*iamv1.InvocationBinding), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}

func TestReceiverRequiresTLSAndOnlineIAMAndHidesEvidenceFromHandler(t *testing.T) {
	target := bindingTarget()
	verify := &verifyClient{}
	c := &Client{authorization: verify, cfg: ClientConfig{PolicyRevision: "revision", Environment: "wr19", TrustDomain: "wr19.test", Timeout: time.Second}}
	interceptor, err := c.ReceiverInterceptor([]Target{*target})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := structpb.NewStruct(map[string]any{"command": "test"})
	info := &grpc.UnaryServerInfo{FullMethod: target.Method}
	calls := 0
	handler := func(ctx context.Context, _ any) (any, error) {
		calls++
		verified, ok := VerifiedFromContext(ctx)
		if !ok || verified.Subject().GetPrincipalId() != "human" {
			t.Fatal("missing typed current identity")
		}
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get(workloadMetadata)) != 0 || len(md.Get(delegationMetadata)) != 0 {
			t.Fatal("proof leaked to handler")
		}
		return req, nil
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(workloadMetadata, "test-wat", delegationMetadata, "test-delegation"))
	if _, err := interceptor(ctx, req, info, handler); status.Code(err) != codes.Unauthenticated || verify.calls != 0 || calls != 0 {
		t.Fatal("missing TLS reached IAM or handler")
	}
	cert := &x509.Certificate{DNSNames: []string{"gateway.wr19.test"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
	if _, err := interceptor(ctx, req, info, handler); err != nil || calls != 1 || verify.calls != 1 {
		t.Fatalf("receiver admission: %v", err)
	}
	verify.fail = true
	if _, err := interceptor(ctx, req, info, handler); status.Code(err) != codes.Unavailable || calls != 1 {
		t.Fatal("IAM outage fell through to handler")
	}
	cert.NotAfter = time.Now().Add(-time.Second)
	before := verify.calls
	if _, err := interceptor(ctx, req, info, handler); status.Code(err) != codes.Unauthenticated || verify.calls != before {
		t.Fatal("expired existing TLS connection reached IAM")
	}
}
