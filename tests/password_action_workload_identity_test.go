package tests_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/server"
)

func TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs(t *testing.T) {
	authorize, err := server.NewWorkloadIdentityMiddleware("wr17-18-isolated", "iam.wr17-18.test", biz.NewWorkloadAuthentication(passwordActionIdentityFixture{}), biz.NewWorkloadAuthorization(passwordActionIdentityFixture{}))
	if err != nil {
		t.Fatalf("NewWorkloadIdentityMiddleware() error = %v", err)
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
	certificate := &x509.Certificate{DNSNames: []string{"ani-gateway"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Minute)}
	tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{certificate}}, PeerCertificates: []*x509.Certificate{certificate},
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

// Unit seam only; formal process tests resolve current identities from PostgreSQL.
type passwordActionIdentityFixture struct{}

func (passwordActionIdentityFixture) ResolveWorkloadIdentity(_ context.Context, p biz.VerifiedWorkloadPeer) (biz.WorkloadIdentity, error) {
	return biz.WorkloadIdentity{PrincipalID: uuid.MustParse("01993000-0000-7000-8000-000000000001"), BindingID: uuid.MustParse("01993000-0000-7000-8000-000000000002"), PrincipalVersion: 1, BindingVersion: 1, Peer: p}, nil
}
func (passwordActionIdentityFixture) CheckWorkloadGrant(context.Context, biz.WorkloadIdentity, biz.WorkloadTarget) (int64, error) {
	return 1, nil
}
