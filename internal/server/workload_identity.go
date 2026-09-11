package server

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var ErrInvalidGatewayWorkloadIdentity = errors.New("invalid Gateway workload identity")

var workloadRuntimeRPCs = map[string]struct{}{
	"/iam.v1.AuthenticationService/ListSessions":             {},
	biz.VerifyWorkloadCallerRPC:                              {},
	biz.VerifySessionContinuationRPC:                         {},
	"/iam.v1.AuthenticationService/IssueWorkloadToken":       {},
	"/iam.v1.AuthenticationService/IssueDelegation":          {},
	"/iam.v1.AuthorizationService/VerifyWorkloadInvocation":  {},
	"/grpc.health.v1.Health/Check":                           {},
	"/iam.v1.AuthenticationService/PasswordLogin":            {},
	"/iam.v1.AuthenticationService/RequestPasswordAction":    {},
	"/iam.v1.AuthenticationService/CompletePasswordAction":   {},
	"/iam.v1.AuthenticationService/BeginOIDCLogin":           {},
	"/iam.v1.AuthenticationService/CompleteOIDCLogin":        {},
	"/iam.v1.AuthenticationService/BeginOIDCIdentityLink":    {},
	"/iam.v1.AuthenticationService/CompleteOIDCIdentityLink": {},
	"/iam.v1.AuthorizationService/CheckPermission":           {},
	"/iam.v1.AuthenticationService/RefreshSession":           {},
	"/iam.v1.AuthenticationService/LogoutSession":            {},
	"/iam.v1.AuthenticationService/SwitchTenant":             {},
	"/iam.v1.AuthenticationService/ValidatePrincipal":        {},
	"/iam.v1.IAMAdminService/CreateTenantWorkload":           {},
	"/iam.v1.IAMAdminService/GetTenantWorkload":              {},
	"/iam.v1.IAMAdminService/ListTenantWorkloads":            {},
	"/iam.v1.IAMAdminService/UpdateTenantWorkload":           {},
	"/iam.v1.IAMAdminService/CreateAPIKey":                   {},
	"/iam.v1.IAMAdminService/ListAPIKeys":                    {},
	"/iam.v1.IAMAdminService/RevokeAPIKey":                   {},
	"/iam.v1.IAMAdminService/GetTenantMembership":            {},
	"/iam.v1.IAMAdminService/ListTenantMemberships":          {},
	"/iam.v1.IAMAdminService/UpdateTenantMembership":         {},
	"/iam.v1.IAMAdminService/RemoveTenantMembership":         {},
	"/iam.v1.IAMAdminService/GetTenantRole":                  {},
	"/iam.v1.IAMAdminService/ListTenantRoles":                {},
	"/iam.v1.IAMAdminService/BindTenantRole":                 {},
	"/iam.v1.IAMAdminService/UnbindTenantRole":               {},
}

func NewWorkloadIdentityMiddleware(environment, trustDomain string, authentication *biz.WorkloadAuthentication, authorization *biz.WorkloadAuthorization) (middleware.Middleware, error) {
	if environment == "" || trustDomain == "" || authentication == nil || authorization == nil ||
		strings.ContainsAny(environment+trustDomain, " /\t\r\n") {
		return nil, ErrInvalidGatewayWorkloadIdentity
	}
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, request any) (any, error) {
			operation, grpcTransport := workloadRPCOperation(ctx)
			if !grpcTransport {
				return next(ctx, request)
			}
			if _, allowed := workloadRuntimeRPCs[operation]; !allowed {
				return nil, workloadPermissionDenied(operation)
			}
			identityValue, valid := verifiedWorkloadDNS(ctx)
			if !valid {
				return nil, workloadIdentityInvalid(operation)
			}
			identity, err := authentication.Authenticate(ctx, biz.VerifiedWorkloadPeer{
				Environment: environment, TrustDomain: trustDomain, IdentityKind: "x509_dns", IdentityValue: identityValue,
			})
			if err != nil {
				return nil, workloadError(operation, err)
			}
			caller, err := authorization.Authorize(ctx, identity, biz.WorkloadTarget{Audience: "ani-iam", Operation: operation})
			if err != nil {
				return nil, workloadError(operation, err)
			}
			return next(biz.WithDirectCaller(ctx, caller), request)
		}
	}, nil
}

func verifiedWorkloadDNS(ctx context.Context) (string, bool) {
	remote, ok := peer.FromContext(ctx)
	if !ok || remote == nil {
		return "", false
	}
	info, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return "", false
	}
	leaf := info.State.PeerCertificates[0]
	if len(leaf.DNSNames) != 1 || len(leaf.IPAddresses) != 0 || len(leaf.URIs) != 0 || len(leaf.EmailAddresses) != 0 {
		return "", false
	}
	clientUsage := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientUsage = true
		}
	}
	if !clientUsage {
		return "", false
	}
	now := time.Now()
	for _, chain := range info.State.VerifiedChains {
		if len(chain) == 0 || !chain[0].Equal(leaf) {
			continue
		}
		current := true
		for _, cert := range chain {
			if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
				current = false
				break
			}
		}
		if current {
			return leaf.DNSNames[0], true
		}
	}
	return "", false
}

func workloadIdentityInvalid(operation string) error {
	return workloadStatus(codes.Unauthenticated, "CREDENTIAL_INVALID", operation)
}

func workloadError(operation string, err error) error {
	if errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
		return workloadIdentityInvalid(operation)
	}
	if errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		return workloadPermissionDenied(operation)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return workloadStatus(codes.DeadlineExceeded, "IAM_TIMEOUT", operation)
	}
	return workloadStatus(codes.Unavailable, "IAM_UNAVAILABLE", operation)
}

func workloadStatus(code codes.Code, reason, operation string) error {
	s := status.New(code, "Workload invocation was rejected")
	withDetails, err := s.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "iam.ani.internal", Metadata: map[string]string{"operation_id": operation, "decision_id": "workload-policy"}})
	if err != nil {
		return s.Err()
	}
	return withDetails.Err()
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
