package cp0_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	kratos "github.com/go-kratos/kratos/v3"
	authv1 "github.com/zhangzhe-ctrl/ani-iam/internal/compat/authv1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	serverpkg "github.com/zhangzhe-ctrl/ani-iam/internal/server"
	servicepkg "github.com/zhangzhe-ctrl/ani-iam/internal/service"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

const frozenAuthProtoSHA256 = "aabcc72b10bd2b89591eaf706b4cf2659b98a8b5b4e3dbf92b3e387938bc33ec"
const frozenAuthDescriptorSHA256 = "39db0d6c1a937221afd0b91a6903c5fe1515abff37b41b26069cb57b0ee44248"

var frozenAuthMethods = []string{
	"Login",
	"PlatformPasswordLogin",
	"BeginOIDCLogin",
	"CompleteOIDCLogin",
	"RefreshToken",
	"RevokeToken",
	"ValidateToken",
	"ValidatePrincipal",
	"IssueServiceToken",
	"CheckPermission",
	"CheckPermissionV2",
	"CreateAPIKey",
	"ListAPIKeys",
	"RevokeAPIKey",
}

func TestFrozenLegacyAuthDescriptorAndSource(t *testing.T) {
	serviceDescriptor := authv1.File_auth_v1_auth_service_proto.Services().ByName("AuthService")
	if serviceDescriptor == nil {
		t.Fatal("auth.v1.AuthService descriptor is missing")
	}
	if got := string(serviceDescriptor.FullName()); got != "auth.v1.AuthService" {
		t.Fatalf("service full name = %q", got)
	}

	gotMethods := make([]string, 0, serviceDescriptor.Methods().Len())
	for i := 0; i < serviceDescriptor.Methods().Len(); i++ {
		gotMethods = append(gotMethods, string(serviceDescriptor.Methods().Get(i).Name()))
	}
	if !reflect.DeepEqual(gotMethods, frozenAuthMethods) {
		t.Fatalf("AuthService methods = %v, want %v", gotMethods, frozenAuthMethods)
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve contract test path")
	}
	protoPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "internal", "compat", "authv1", "proto", "auth", "v1", "auth_service.proto"))
	contents, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatalf("read frozen Auth source: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != frozenAuthProtoSHA256 {
		t.Fatalf("frozen Auth source SHA-256 = %s, want %s", got, frozenAuthProtoSHA256)
	}

	descriptor := protodesc.ToFileDescriptorProto(authv1.File_auth_v1_auth_service_proto)
	descriptor.SourceCodeInfo = nil
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal Auth descriptor: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != frozenAuthDescriptorSHA256 {
		t.Fatalf("frozen Auth descriptor SHA-256 = %s, want %s", got, frozenAuthDescriptorSHA256)
	}
}

func TestAllFrozenLegacyAuthRPCsAreRegisteredAndFailExplicitly(t *testing.T) {
	conn := startLegacyAuthServer(t, servicepkg.NewLegacyAuthService(), time.Second)
	client := authv1.NewAuthServiceClient(conn)
	calls := []struct {
		method string
		call   func(context.Context) error
	}{
		{authv1.AuthService_Login_FullMethodName, func(ctx context.Context) error { _, err := client.Login(ctx, &authv1.LoginRequest{}); return err }},
		{authv1.AuthService_PlatformPasswordLogin_FullMethodName, func(ctx context.Context) error {
			_, err := client.PlatformPasswordLogin(ctx, &authv1.PlatformPasswordLoginRequest{})
			return err
		}},
		{authv1.AuthService_BeginOIDCLogin_FullMethodName, func(ctx context.Context) error {
			_, err := client.BeginOIDCLogin(ctx, &authv1.BeginOIDCLoginRequest{})
			return err
		}},
		{authv1.AuthService_CompleteOIDCLogin_FullMethodName, func(ctx context.Context) error {
			_, err := client.CompleteOIDCLogin(ctx, &authv1.CompleteOIDCLoginRequest{})
			return err
		}},
		{authv1.AuthService_RefreshToken_FullMethodName, func(ctx context.Context) error {
			_, err := client.RefreshToken(ctx, &authv1.RefreshTokenRequest{})
			return err
		}},
		{authv1.AuthService_RevokeToken_FullMethodName, func(ctx context.Context) error {
			_, err := client.RevokeToken(ctx, &authv1.RevokeTokenRequest{})
			return err
		}},
		{authv1.AuthService_ValidateToken_FullMethodName, func(ctx context.Context) error {
			_, err := client.ValidateToken(ctx, &authv1.ValidateTokenRequest{})
			return err
		}},
		{authv1.AuthService_ValidatePrincipal_FullMethodName, func(ctx context.Context) error {
			_, err := client.ValidatePrincipal(ctx, &authv1.ValidatePrincipalRequest{})
			return err
		}},
		{authv1.AuthService_IssueServiceToken_FullMethodName, func(ctx context.Context) error {
			_, err := client.IssueServiceToken(ctx, &authv1.IssueServiceTokenRequest{})
			return err
		}},
		{authv1.AuthService_CheckPermission_FullMethodName, func(ctx context.Context) error {
			_, err := client.CheckPermission(ctx, &authv1.CheckPermissionRequest{})
			return err
		}},
		{authv1.AuthService_CheckPermissionV2_FullMethodName, func(ctx context.Context) error {
			_, err := client.CheckPermissionV2(ctx, &authv1.AuthorizationRequest{})
			return err
		}},
		{authv1.AuthService_CreateAPIKey_FullMethodName, func(ctx context.Context) error {
			_, err := client.CreateAPIKey(ctx, &authv1.CreateAPIKeyRequest{})
			return err
		}},
		{authv1.AuthService_ListAPIKeys_FullMethodName, func(ctx context.Context) error {
			_, err := client.ListAPIKeys(ctx, &authv1.ListAPIKeysRequest{})
			return err
		}},
		{authv1.AuthService_RevokeAPIKey_FullMethodName, func(ctx context.Context) error {
			_, err := client.RevokeAPIKey(ctx, &authv1.RevokeAPIKeyRequest{})
			return err
		}},
	}
	if len(calls) != len(frozenAuthMethods) {
		t.Fatalf("call inventory has %d methods, want %d", len(calls), len(frozenAuthMethods))
	}

	for _, test := range calls {
		t.Run(filepath.Base(test.method), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := test.call(ctx)
			st := status.Convert(err)
			if st.Code() != codes.Unimplemented {
				t.Fatalf("code = %s, message = %q", st.Code(), st.Message())
			}
			if st.Message() != "legacy Auth behavior is not implemented in CP0-1" {
				t.Fatalf("message = %q", st.Message())
			}
			assertLegacyErrorInfo(t, st, test.method)
		})
	}
}

func TestLegacyAuthUnknownMethodFailsClosed(t *testing.T) {
	conn := startLegacyAuthServer(t, servicepkg.NewLegacyAuthService(), time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := conn.Invoke(ctx, "/auth.v1.AuthService/Unknown", &emptypb.Empty{}, &emptypb.Empty{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("unknown method code = %s, want %s", got, codes.Unimplemented)
	}
}

func TestLegacyAuthPropagatesCancellationAndDeadline(t *testing.T) {
	blocking := &blockingAuthService{entered: make(chan struct{}, 3)}
	conn := startLegacyAuthServer(t, blocking, 150*time.Millisecond)
	client := authv1.NewAuthServiceClient(conn)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan error, 1)
	go func() {
		_, err := client.Login(cancelCtx, &authv1.LoginRequest{})
		cancelResult <- err
	}()
	waitForHandlerEntry(t, blocking.entered)
	cancel()
	if got := status.Code(<-cancelResult); got != codes.Canceled {
		t.Fatalf("canceled call code = %s, want %s", got, codes.Canceled)
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer deadlineCancel()
	deadlineResult := make(chan error, 1)
	go func() {
		_, err := client.Login(deadlineCtx, &authv1.LoginRequest{})
		deadlineResult <- err
	}()
	waitForHandlerEntry(t, blocking.entered)
	if got := status.Code(<-deadlineResult); got != codes.DeadlineExceeded {
		t.Fatalf("deadline call code = %s, want %s", got, codes.DeadlineExceeded)
	}

	serverDeadlineResult := make(chan error, 1)
	go func() {
		_, err := client.Login(context.Background(), &authv1.LoginRequest{})
		serverDeadlineResult <- err
	}()
	waitForHandlerEntry(t, blocking.entered)
	select {
	case err := <-serverDeadlineResult:
		if got := status.Code(err); got != codes.DeadlineExceeded {
			t.Fatalf("server deadline call code = %s, want %s", got, codes.DeadlineExceeded)
		}
	case <-time.After(time.Second):
		t.Fatal("configured server deadline did not terminate the call")
	}
}

type blockingAuthService struct {
	authv1.UnimplementedAuthServiceServer
	entered chan struct{}
}

func (s *blockingAuthService) Login(ctx context.Context, _ *authv1.LoginRequest) (*authv1.TokenPair, error) {
	s.entered <- struct{}{}
	<-ctx.Done()
	return nil, status.FromContextError(ctx.Err()).Err()
}

func assertLegacyErrorInfo(t *testing.T, st *status.Status, method string) {
	t.Helper()
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if info.Reason != "CP0_COMPAT_NOT_IMPLEMENTED" || info.Domain != "ani.auth.v1" || info.Metadata["method"] != method {
			t.Fatalf("ErrorInfo = %+v", info)
		}
		return
	}
	t.Fatal("missing google.rpc.ErrorInfo detail")
}

func startLegacyAuthServer(t *testing.T, authService authv1.AuthServiceServer, timeout time.Duration) *grpc.ClientConn {
	t.Helper()
	grpcServer := serverpkg.NewGRPCServerWithLegacyAuth(
		&conf.Server_GRPC{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(timeout)},
		authService,
	)
	endpoint, err := grpcServer.Endpoint()
	if err != nil {
		t.Fatalf("gRPC Endpoint() error = %v", err)
	}
	app := kratos.New(kratos.Server(grpcServer), kratos.StopTimeout(time.Second))
	runResult := make(chan error, 1)
	go func() { runResult <- app.Run() }()
	t.Cleanup(func() {
		if err := app.Stop(); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
		if err := <-runResult; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	})

	conn, err := grpc.NewClient(endpoint.Host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitForGRPCServing(t, conn)
	return conn
}

func waitForGRPCServing(t *testing.T, conn *grpc.ClientConn) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		cancel()
		if err == nil && response.Status == grpc_health_v1.HealthCheckResponse_SERVING {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("gRPC server did not become ready")
}

func waitForHandlerEntry(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("RPC did not reach the compatibility handler")
	}
}
