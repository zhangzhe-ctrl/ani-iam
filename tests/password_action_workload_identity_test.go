package tests_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/server"
)

func TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs(t *testing.T) {
	authorize, err := server.NewGatewayWorkloadIdentityMiddleware("ani-gateway")
	if err != nil {
		t.Fatalf("NewGatewayWorkloadIdentityMiddleware() error = %v", err)
	}

	operations := []string{
		iamv1.AuthenticationService_RequestPasswordAction_FullMethodName,
		iamv1.AuthenticationService_CompletePasswordAction_FullMethodName,
	}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			called := false
			handler := authorize(func(context.Context, any) (any, error) {
				called = true
				return "ok", nil
			})

			response, err := handler(passwordActionWorkloadContext(operation), struct{}{})
			if err != nil || !called || response != "ok" {
				t.Fatalf("handler response/error/called = %#v/%v/%t, want ok/nil/true", response, err, called)
			}
		})
	}
}

func passwordActionWorkloadContext(operation string) context.Context {
	certificate := &x509.Certificate{DNSNames: []string{"ani-gateway"}}
	tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{certificate}},
	}}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})
	return transport.NewServerContext(ctx, passwordActionTestTransport{operation: operation})
}

type passwordActionTestTransport struct {
	operation string
}

func (passwordActionTestTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (passwordActionTestTransport) Endpoint() string                { return "grpc://127.0.0.1:19090" }
func (t passwordActionTestTransport) Operation() string             { return t.operation }
func (passwordActionTestTransport) RequestHeader() transport.Header { return nil }
func (passwordActionTestTransport) ReplyHeader() transport.Header   { return nil }
