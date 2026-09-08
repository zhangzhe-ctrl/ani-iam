package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestGatewayWorkloadIdentityMiddlewareAuthorizesConfiguredDNSNamePerRPC(t *testing.T) {
	authorize, err := NewGatewayWorkloadIdentityMiddleware("ani-gateway")
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
			wantCode:  codes.PermissionDenied,
		},
		{
			name:      "gateway can get tenant access",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/GetTenantAccess",
			wantCode:  codes.OK,
			called:    true,
		},
		{
			name:      "gateway can update tenant access",
			dnsNames:  []string{"ani-gateway"},
			operation: "/iam.v1.IAMAdminService/UpdateTenantAccess",
			wantCode:  codes.OK,
			called:    true,
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
	authorize, err := NewGatewayWorkloadIdentityMiddleware("ani-gateway")
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
	certificate := &x509.Certificate{DNSNames: dnsNames}
	tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{certificate}},
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
