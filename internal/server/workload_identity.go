package server

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var ErrInvalidGatewayWorkloadIdentity = errors.New("invalid Gateway workload identity")

var gatewayDP2AllowedRPCs = map[string]struct{}{
	"/grpc.health.v1.Health/Check":                         {},
	"/iam.v1.AuthenticationService/PasswordLogin":          {},
	"/iam.v1.AuthenticationService/RequestPasswordAction":  {},
	"/iam.v1.AuthenticationService/CompletePasswordAction": {},
	"/iam.v1.AuthorizationService/CheckPermission":         {},
}

func NewGatewayWorkloadIdentityMiddleware(gatewayDNSName string) (middleware.Middleware, error) {
	gatewayDNSName = strings.TrimSpace(gatewayDNSName)
	if gatewayDNSName == "" || strings.ContainsAny(gatewayDNSName, " /\t\r\n") {
		return nil, ErrInvalidGatewayWorkloadIdentity
	}
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, request any) (any, error) {
			operation, grpcTransport := workloadRPCOperation(ctx)
			if !grpcTransport {
				return next(ctx, request)
			}
			if _, allowed := gatewayDP2AllowedRPCs[operation]; !allowed || !verifiedClientDNSName(ctx, gatewayDNSName) {
				return nil, workloadPermissionDenied(operation)
			}
			return next(ctx, request)
		}
	}, nil
}

func workloadRPCOperation(ctx context.Context) (string, bool) {
	transporter, ok := transport.FromServerContext(ctx)
	if !ok || transporter.Kind() != transport.KindGRPC {
		return "", false
	}
	operation := strings.TrimSpace(transporter.Operation())
	if operation == "" {
		return "unknown", true
	}
	return operation, true
}

func verifiedClientDNSName(ctx context.Context, expected string) bool {
	remote, ok := peer.FromContext(ctx)
	if !ok || remote == nil {
		return false
	}
	tlsInfo, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return false
	}
	for _, chain := range tlsInfo.State.VerifiedChains {
		if len(chain) == 0 || chain[0] == nil {
			continue
		}
		if certificateHasExactDNSName(chain[0], expected) {
			return true
		}
	}
	return false
}

func certificateHasExactDNSName(certificate *x509.Certificate, expected string) bool {
	for _, name := range certificate.DNSNames {
		if name == expected {
			return true
		}
	}
	return false
}

func workloadPermissionDenied(operation string) error {
	grpcStatus := status.New(codes.PermissionDenied, "workload identity is not authorized for IAM RPC")
	withDetails, err := grpcStatus.WithDetails(&errdetails.ErrorInfo{
		Reason: "PERMISSION_DENIED",
		Domain: "iam.ani.internal",
		Metadata: map[string]string{
			"operation_id": operation,
			"decision_id":  "workload-policy",
		},
	})
	if err != nil {
		return grpcStatus.Err()
	}
	return withDetails.Err()
}
