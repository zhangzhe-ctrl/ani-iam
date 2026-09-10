package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestGatewayWorkloadIdentityMiddlewareAuthorizesConfiguredDNSNamePerRPC(t *testing.T) {
	authorize, err := NewWorkloadIdentityMiddleware("test", "iam.test", biz.NewWorkloadAuthentication(workloadTestReader{}), biz.NewWorkloadAuthorization(workloadTestReader{}))
	if err != nil {
		t.Fatalf("NewGatewayWorkloadIdentityMiddleware() error = %v", err)
	}

	tests := []struct {
		name      string
		dnsNames  []string
		operation string
		wantCode  codes.Code
		called    bool
	}{
		{
			name:      "configured gateway can login",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.AuthenticationService/PasswordLogin",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "configured gateway can begin OIDC login",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.AuthenticationService/BeginOIDCLogin",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "configured gateway can complete OIDC login",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.AuthenticationService/CompleteOIDCLogin",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "configured gateway can begin OIDC identity link",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.AuthenticationService/BeginOIDCIdentityLink",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "configured gateway can complete OIDC identity link",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.AuthenticationService/CompleteOIDCIdentityLink",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "same CA different identity is denied",
			dnsNames:  []string{"other-workload"},
			operation: "/iam.v1.AuthenticationService/PasswordLogin",
			wantCode:  codes.Unauthenticated,
		},
		{
			name:      "gateway can get tenant access",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/GetTenantAccess",
			wantCode:  codes.PermissionDenied,
			called:    false,
		},
		{
			name:      "gateway can update tenant access",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/UpdateTenantAccess",
			wantCode:  codes.PermissionDenied,
			called:    false,
		},
		{
			name:      "gateway can get tenant membership",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/GetTenantMembership",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can list tenant memberships",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/ListTenantMemberships",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can update tenant membership",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/UpdateTenantMembership",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can remove tenant membership",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/RemoveTenantMembership",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can get tenant role",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/GetTenantRole",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can list tenant roles",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/ListTenantRoles",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can bind tenant role",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/BindTenantRole",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can unbind tenant role",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/UnbindTenantRole",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway cannot create a custom tenant role in DP2-09",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/CreateTenantRole",
			wantCode:  codes.PermissionDenied,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := authorize(func(context.Context, any) (any, error) {
				called = true
				return "ok", nil
			})
			ctx := workloadRPCContext(test.operation, test.dnsNames)
			_, err := handler(ctx, struct{}{})
			if status.Code(err) != test.wantCode || called != test.called {
				t.Fatalf("middleware status/called = %s/%t, want %s/%t", status.Code(err), called, test.wantCode, test.called)
			}
		})
	}
}

func TestGatewayWorkloadIdentityMiddlewareDoesNotInterceptAdminHTTP(t *testing.T) {
	authorize, err := NewWorkloadIdentityMiddleware("test", "iam.test", biz.NewWorkloadAuthentication(workloadTestReader{}), biz.NewWorkloadAuthorization(workloadTestReader{}))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := authorize(func(context.Context, any) (any, error) {
		called = true
		return "ready", nil
	})
	ctx := transport.NewServerContext(context.Background(), workloadTestTransport{
		kind:      transport.KindHTTP,
		operation: "/readyz",
	})
	response, err := handler(ctx, struct{}{})
	if err != nil || !called || response != "ready" {
		t.Fatalf("admin HTTP middleware response/error/called = %#v/%v/%t", response, err, called)
	}
}

func workloadRPCContext(operation string, dnsNames []string) context.Context {
	certificate := &x509.Certificate{NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: dnsNames, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{certificate}}, PeerCertificates: []*x509.Certificate{certificate},
	}}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})
	return transport.NewServerContext(ctx, workloadTestTransport{kind: transport.KindGRPC, operation: operation})
}

type workloadTestTransport struct {
	kind      transport.Kind
	operation string
}

func (t workloadTestTransport) Kind() transport.Kind            { return t.kind }
func (t workloadTestTransport) Endpoint() string                { return "grpc://127.0.0.1:19090" }
func (t workloadTestTransport) Operation() string               { return t.operation }
func (t workloadTestTransport) RequestHeader() transport.Header { return nil }
func (t workloadTestTransport) ReplyHeader() transport.Header   { return nil }

type workloadTestReader struct{}

func (workloadTestReader) ResolveWorkloadIdentity(_ context.Context, peer biz.VerifiedWorkloadPeer) (biz.WorkloadIdentity, error) {
	if peer.IdentityValue != "ani-gateway" {
		return biz.WorkloadIdentity{}, biz.ErrWorkloadIdentityInvalid
	}
	return biz.WorkloadIdentity{PrincipalID: uuid.MustParse("01993000-0000-7000-8000-000000000001"), BindingID: uuid.MustParse("01993000-0000-7000-8000-000000000002"), PrincipalVersion: 1, BindingVersion: 1, Peer: peer}, nil
}
func (workloadTestReader) CheckWorkloadGrant(context.Context, biz.WorkloadIdentity, biz.WorkloadTarget) (int64, error) {
	return 1, nil
}

func TestExpiredCertificateIsRejectedOnAnExistingIAMConnection(t *testing.T) {
	ctx := workloadRPCContext("/iam.v1.AuthenticationService/PasswordLogin", []string{"ani-gateway"})
	remote, _ := peer.FromContext(ctx)
	info := remote.AuthInfo.(credentials.TLSInfo)
	info.State.PeerCertificates[0].NotAfter = time.Now().Add(-time.Second)
	if _, ok := verifiedWorkloadDNS(ctx); ok {
		t.Fatal("expired certificate on an existing TLS connection was accepted")
	}
}
