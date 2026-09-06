package server

import (
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
)

func TestTargetGRPCServerRegistersOnlyThreeIAMServices(t *testing.T) {
	directory := t.TempDir()
	caCertificate, caKey := newTestCertificateAuthority(t)
	certificateFile, privateKeyFile := writeTestLeafCertificate(t, directory, caCertificate, caKey)
	clientCAFile := directory + "/client-ca.pem"
	writePEMFile(t, clientCAFile, "CERTIFICATE", caCertificate.Raw, 0o600)
	tlsConfig, err := LoadMutualTLSServerConfig(&conf.Server_GRPC_TLS{
		CertificateFile: certificateFile,
		PrivateKeyFile:  privateKeyFile,
		ClientCaFile:    clientCAFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	grpcServer, err := NewTargetGRPCServer(
		&conf.Server_GRPC{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
		tlsConfig,
		&testAuthenticationServer{},
		&testAuthorizationServer{},
		&testIAMAdminServer{},
	)
	if err != nil {
		t.Fatal(err)
	}

	registered := make([]string, 0, 3)
	for serviceName := range grpcServer.GetServiceInfo() {
		if strings.HasPrefix(serviceName, "iam.v1.") {
			registered = append(registered, serviceName)
		}
	}
	slices.Sort(registered)
	want := []string{"iam.v1.AuthenticationService", "iam.v1.AuthorizationService", "iam.v1.IAMAdminService"}
	if !slices.Equal(registered, want) {
		t.Fatalf("registered IAM services = %v, want %v", registered, want)
	}
	if _, legacy := grpcServer.GetServiceInfo()["auth.v1.AuthService"]; legacy {
		t.Fatal("legacy auth.v1.AuthService was registered")
	}
}

type testAuthenticationServer struct {
	iamv1.UnimplementedAuthenticationServiceServer
}
type testAuthorizationServer struct {
	iamv1.UnimplementedAuthorizationServiceServer
}
type testIAMAdminServer struct {
	iamv1.UnimplementedIAMAdminServiceServer
}
