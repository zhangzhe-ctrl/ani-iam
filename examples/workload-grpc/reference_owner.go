package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	referencev1 "github.com/zhangzhe-ctrl/ani-iam/examples/workload-grpc/api/reference/v1"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const referenceAudience = "wr32-inventory-owner"
const referenceSource = "readWR32InventoryResource"

func referenceTarget() grpcworkload.Target {
	return grpcworkload.Target{Audience: referenceAudience, Operation: "reference.read", Method: referencev1.InventoryService_ReadResource_FullMethodName, Describe: func(message proto.Message) (grpcworkload.RequestScope, error) {
		r, ok := message.(*referencev1.ReadResourceRequest)
		if !ok || r.GetRequestId() == "" || len(r.GetRequestId()) > 128 || r.GetResourceId() == "" || len(r.GetResourceId()) > 128 {
			return grpcworkload.RequestScope{}, grpcworkload.ErrBinding
		}
		return grpcworkload.RequestScope{TenantID: r.GetTenantId(), SubjectID: r.GetSubjectId(), ResourceID: r.GetResourceId(), SourceOperation: referenceSource, Mode: "read"}, nil
	}}
}

type referenceResource struct {
	TenantID   string `json:"tenant_id"`
	ResourceID string `json:"resource_id"`
	Ready      bool   `json:"ready"`
}
type referenceOwner struct {
	referencev1.UnimplementedInventoryServiceServer
	resources []referenceResource
}
type referenceCallerKey struct{}

func (o *referenceOwner) check(_ context.Context, _ proto.Message, v grpcworkload.Verified) error {
	b := v.Binding()
	for _, r := range o.resources {
		if r.TenantID == b.GetTenantId() && r.ResourceID == b.GetResourceId() && r.Ready {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "reference owner resource denied")
}
func (o *referenceOwner) ReadStatus(ctx context.Context, _ *referencev1.ReadStatusRequest) (*referencev1.ReadStatusResponse, error) {
	if caller, ok := ctx.Value(referenceCallerKey{}).(grpcworkload.WorkloadCaller); !ok || caller.PrincipalID() == "" {
		return nil, status.Error(codes.Unauthenticated, "verified caller required")
	}
	return &referencev1.ReadStatusResponse{Status: "reference-owner-ready"}, nil
}
func (o *referenceOwner) ReadResource(ctx context.Context, r *referencev1.ReadResourceRequest) (*referencev1.ReadResourceResponse, error) {
	v, ok := grpcworkload.VerifiedFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "verified subject required")
	}
	if err := o.check(ctx, r, v); err != nil {
		return nil, err
	}
	return &referencev1.ReadResourceResponse{ResourceId: r.GetResourceId(), Status: "reference-resource-ready"}, nil
}

func runReference(mode string, cfg config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	delegated, err := grpcworkload.NewClient(cfg.IAM)
	if err != nil {
		return err
	}
	defer delegated.Close()
	workload, err := grpcworkload.NewWorkloadOnlyClient(cfg.IAM)
	if err != nil {
		return err
	}
	defer workload.Close()
	statusTarget, err := grpcworkload.RegisteredWorkloadTarget(cfg.IAM.Registry, referencev1.InventoryService_ReadStatus_FullMethodName)
	if err != nil {
		return err
	}
	switch mode {
	case "reference-receiver":
		owner := new(referenceOwner)
		if err := readJSON(cfg.InventoryFile, &owner.resources); err != nil {
			return errors.New("reference inventory unavailable")
		}
		receiver, err := delegated.ReceiverInterceptorWithOwnerCheck([]grpcworkload.Target{referenceTarget()}, owner.check)
		if err != nil {
			return err
		}
		host, _, err := net.SplitHostPort(cfg.ListenAddress)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("reference listener must use loopback")
		}
		transport, err := cfg.ServerTLS.ServerCredentials()
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", cfg.ListenAddress)
		if err != nil {
			return err
		}
		defer listener.Close()
		intercept := func(call context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if info.FullMethod == statusTarget.RPCMethod {
				clean, caller, err := workload.VerifyCaller(call, statusTarget)
				if err != nil {
					return nil, err
				}
				return handler(context.WithValue(clean, referenceCallerKey{}, caller), request)
			}
			return receiver(call, request, info, handler)
		}
		server := grpc.NewServer(grpc.Creds(transport), grpc.UnaryInterceptor(intercept))
		referencev1.RegisterInventoryServiceServer(server, owner)
		go func() { <-ctx.Done(); server.GracefulStop() }()
		fmt.Println("reference receiver ready")
		if err = server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	case "reference-workload-caller":
		creds, err := cfg.IAM.TLS.ClientCredentials(cfg.TargetServerName)
		if err != nil {
			return err
		}
		intercept, err := grpcworkload.WorkloadOnlyCallerInterceptor(workload.TokenSource, cfg.IAM.Registry)
		if err != nil {
			return err
		}
		conn, err := grpc.NewClient(cfg.TargetAddress, grpc.WithTransportCredentials(creds), grpc.WithDisableRetry(), grpc.WithUnaryInterceptor(intercept))
		if err != nil {
			return err
		}
		defer conn.Close()
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		reply, err := referencev1.NewInventoryServiceClient(conn).ReadStatus(call, &referencev1.ReadStatusRequest{})
		if err != nil {
			return err
		}
		if reply.GetStatus() != "reference-owner-ready" {
			return errors.New("unexpected owner result")
		}
		fmt.Println("reference workload-only resource result: pass")
		return nil
	case "reference-caller":
		info, err := os.Lstat(cfg.CredentialFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
			return errors.New("private subject credential file required")
		}
		raw, err := os.ReadFile(cfg.CredentialFile)
		if err != nil {
			return errors.New("subject credential unavailable")
		}
		request, err := os.ReadFile(cfg.RequestFile)
		if err != nil || len(request) > 1<<20 {
			return errors.New("reference request unavailable")
		}
		dto := new(referencev1.ReadResourceRequest)
		if err = protojson.Unmarshal(request, dto); err != nil {
			return errors.New("invalid reference request")
		}
		target := referenceTarget()
		scope, err := target.Describe(dto)
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		subject, err := delegated.AuthorizeForReceiver(call, grpcworkload.AuthorizationRequest{Credential: strings.TrimSpace(string(raw)), SourceOperation: scope.SourceOperation, TenantID: scope.TenantID, ResourceID: scope.ResourceID}, grpcworkload.WorkloadTarget{Audience: target.Audience, Operation: target.Operation, RPCMethod: target.Method})
		if err != nil {
			return err
		}
		conn, err := delegated.DialCaller(grpcworkload.CallerConfig{Address: cfg.TargetAddress, ServerName: cfg.TargetServerName, Targets: []grpcworkload.Target{target}})
		if err != nil {
			return err
		}
		defer conn.Close()
		reply, err := referencev1.NewInventoryServiceClient(conn).ReadResource(grpcworkload.WithSubject(call, subject), dto)
		if err != nil {
			return err
		}
		if reply.GetResourceId() != dto.GetResourceId() || reply.GetStatus() != "reference-resource-ready" {
			return errors.New("unexpected owner resource result")
		}
		fmt.Println("reference delegated resource result: pass")
		return nil
	default:
		return errors.New("unknown reference mode")
	}
}
