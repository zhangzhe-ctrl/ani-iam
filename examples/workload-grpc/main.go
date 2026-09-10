// The example exercises the public adapter and an explicit owner inventory.
// Its receiver performs no resource mutation and creates no terminal session.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	sessionv1 "github.com/zhangzhe-ctrl/ani-session-gateway/api/gen/session/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type config struct {
	IAM              grpcworkload.ClientConfig `json:"iam"`
	TargetAddress    string                    `json:"target_address"`
	TargetServerName string                    `json:"target_server_name"`
	ListenAddress    string                    `json:"listen_address"`
	ServerTLS        grpcworkload.TLSFiles     `json:"server_tls"`
	InventoryFile    string                    `json:"inventory_file"`
	CredentialFile   string                    `json:"credential_file"`
	RequestFile      string                    `json:"request_file"`
}

type ownedResource struct {
	TenantID     string `json:"tenant_id"`
	InstanceID   string `json:"instance_id"`
	WorkloadName string `json:"workload_name"`
	Ready        bool   `json:"ready"`
}

type inventory []ownedResource

func (i inventory) check(_ context.Context, boundary grpcworkload.ResourceBoundary) error {
	for _, r := range i {
		if r.InstanceID == boundary.ResourceID && r.TenantID == boundary.TenantID && r.Ready {
			return nil
		}
	}
	return errors.New("owner resource boundary denied")
}

func target() grpcworkload.Target {
	return grpcworkload.Target{Method: sessionv1.SessionService_CreateSession_FullMethodName, Audience: "ani-session-gateway", Operation: "session.create", Describe: func(message proto.Message) (grpcworkload.RequestScope, error) {
		r, ok := message.(*sessionv1.CreateSessionRequest)
		if !ok || r.GetPrincipal() == nil || r.GetTarget() == nil {
			return grpcworkload.RequestScope{}, errors.New("owner DTO required")
		}
		mode, source := "", ""
		switch r.GetMode().(type) {
		case *sessionv1.CreateSessionRequest_Exec:
			mode, source = "exec", "createInstanceExecSession"
		case *sessionv1.CreateSessionRequest_VmConsole:
			mode, source = "vm_console", "createInstanceConsoleSession"
		default:
			return grpcworkload.RequestScope{}, errors.New("owner mode required")
		}
		return grpcworkload.RequestScope{TenantID: r.Principal.TenantId, SubjectID: r.Principal.SubjectId, ResourceID: r.Target.InstanceId, Mode: mode, SourceOperation: source}, nil
	}}
}

type receiver struct {
	sessionv1.UnimplementedSessionServiceServer
	resources inventory
}

func (s *receiver) CreateSession(ctx context.Context, r *sessionv1.CreateSessionRequest) (*sessionv1.CreateSessionResponse, error) {
	v, ok := grpcworkload.VerifiedFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "verified context required")
	}
	b := v.Binding()
	if err := s.resources.check(ctx, grpcworkload.ResourceBoundary{TenantID: b.TenantId, ResourceID: b.ResourceId}); err != nil {
		return nil, status.Error(codes.PermissionDenied, "owner resource denied")
	}
	for _, owned := range s.resources {
		if owned.TenantID == b.TenantId && owned.InstanceID == b.ResourceId && owned.WorkloadName == r.GetTarget().GetWorkloadName() {
			// A harmless example acknowledgement, explicitly not a session ticket.
			return &sessionv1.CreateSessionResponse{SessionId: "example-only-no-session"}, nil
		}
	}
	return nil, status.Error(codes.PermissionDenied, "owner workload denied")
}

func readJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("one JSON value required")
	}
	return nil
}

func run(mode, path string) error {
	var cfg config
	if err := readJSON(path, &cfg); err != nil {
		return errors.New("invalid example configuration")
	}
	var resources inventory
	if err := readJSON(cfg.InventoryFile, &resources); err != nil {
		return errors.New("invalid owner inventory")
	}
	iam, err := grpcworkload.NewClient(cfg.IAM)
	if err != nil {
		return err
	}
	defer iam.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if mode == "receiver" {
		host, _, err := net.SplitHostPort(cfg.ListenAddress)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("example listener must use loopback")
		}
		transport, err := cfg.ServerTLS.ServerCredentials()
		if err != nil {
			return err
		}
		interceptor, err := iam.ReceiverInterceptor([]grpcworkload.Target{target()})
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", cfg.ListenAddress)
		if err != nil {
			return err
		}
		defer listener.Close()
		server := grpc.NewServer(grpc.Creds(transport), grpc.UnaryInterceptor(interceptor))
		sessionv1.RegisterSessionServiceServer(server, &receiver{resources: resources})
		go func() { <-ctx.Done(); server.Stop() }()
		fmt.Println("example receiver ready; no real Session backend")
		return server.Serve(listener)
	}
	if mode != "caller" {
		return errors.New("mode must be caller or receiver")
	}
	info, err := os.Lstat(cfg.CredentialFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 32768 {
		return errors.New("private credential file required")
	}
	raw, err := os.ReadFile(cfg.CredentialFile)
	if err != nil {
		return errors.New("credential file unavailable")
	}
	requestRaw, err := os.ReadFile(cfg.RequestFile)
	if err != nil || len(requestRaw) > 1<<20 {
		return errors.New("request file unavailable")
	}
	r := new(sessionv1.CreateSessionRequest)
	if err := protojson.Unmarshal(requestRaw, r); err != nil {
		return errors.New("invalid owner request")
	}
	scope, err := target().Describe(r)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	subject, err := iam.Authorize(call, grpcworkload.AuthorizationRequest{Credential: strings.TrimSpace(string(raw)), SourceOperation: scope.SourceOperation, TenantID: scope.TenantID, ResourceID: scope.ResourceID, CheckResource: resources.check})
	if err != nil {
		return err
	}
	conn, err := iam.DialCaller(grpcworkload.CallerConfig{Address: cfg.TargetAddress, ServerName: cfg.TargetServerName, Targets: []grpcworkload.Target{target()}})
	if err != nil {
		return err
	}
	defer conn.Close()
	result, err := sessionv1.NewSessionServiceClient(conn).CreateSession(grpcworkload.WithSubject(call, subject), r)
	if err != nil {
		return err
	}
	if result.GetSessionId() == "" {
		return errors.New("receiver returned no result")
	}
	fmt.Println("authorized call passed; credential, request and ticket values omitted")
	return nil
}

func main() {
	mode := flag.String("mode", "", "caller or receiver")
	path := flag.String("config", "", "local JSON configuration path")
	flag.Parse()
	if err := run(*mode, *path); err != nil {
		fmt.Fprintln(os.Stderr, "example failed:", status.Code(err))
		os.Exit(1)
	}
}
