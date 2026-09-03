package service

import (
	"context"

	authv1 "github.com/zhangzhe-ctrl/ani-iam/internal/compat/authv1"
	commonv1 "github.com/zhangzhe-ctrl/ani-iam/internal/compat/authv1/commonv1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	legacyErrorDomain          = "ani.auth.v1"
	legacyNotImplementedReason = "CP0_COMPAT_NOT_IMPLEMENTED"
)

// LegacyAuthService exposes the frozen transport contract without pretending
// that CP0-1 has implemented the legacy business or persistence semantics.
type LegacyAuthService struct {
	authv1.UnimplementedAuthServiceServer
}

func NewLegacyAuthService() *LegacyAuthService {
	return &LegacyAuthService{}
}

func (s *LegacyAuthService) Login(ctx context.Context, _ *authv1.LoginRequest) (*authv1.TokenPair, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_Login_FullMethodName)
}

func (s *LegacyAuthService) PlatformPasswordLogin(ctx context.Context, _ *authv1.PlatformPasswordLoginRequest) (*authv1.TokenPair, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_PlatformPasswordLogin_FullMethodName)
}

func (s *LegacyAuthService) BeginOIDCLogin(ctx context.Context, _ *authv1.BeginOIDCLoginRequest) (*authv1.BeginOIDCLoginResponse, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_BeginOIDCLogin_FullMethodName)
}

func (s *LegacyAuthService) CompleteOIDCLogin(ctx context.Context, _ *authv1.CompleteOIDCLoginRequest) (*authv1.TokenPair, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_CompleteOIDCLogin_FullMethodName)
}

func (s *LegacyAuthService) RefreshToken(ctx context.Context, _ *authv1.RefreshTokenRequest) (*authv1.AccessToken, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_RefreshToken_FullMethodName)
}

func (s *LegacyAuthService) RevokeToken(ctx context.Context, _ *authv1.RevokeTokenRequest) (*emptypb.Empty, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_RevokeToken_FullMethodName)
}

func (s *LegacyAuthService) ValidateToken(ctx context.Context, _ *authv1.ValidateTokenRequest) (*commonv1.TenantContext, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_ValidateToken_FullMethodName)
}

func (s *LegacyAuthService) ValidatePrincipal(ctx context.Context, _ *authv1.ValidatePrincipalRequest) (*authv1.PrincipalContext, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_ValidatePrincipal_FullMethodName)
}

func (s *LegacyAuthService) IssueServiceToken(ctx context.Context, _ *authv1.IssueServiceTokenRequest) (*authv1.AccessToken, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_IssueServiceToken_FullMethodName)
}

func (s *LegacyAuthService) CheckPermission(ctx context.Context, _ *authv1.CheckPermissionRequest) (*authv1.CheckPermissionResponse, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_CheckPermission_FullMethodName)
}

func (s *LegacyAuthService) CheckPermissionV2(ctx context.Context, _ *authv1.AuthorizationRequest) (*authv1.AuthorizationDecision, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_CheckPermissionV2_FullMethodName)
}

func (s *LegacyAuthService) CreateAPIKey(ctx context.Context, _ *authv1.CreateAPIKeyRequest) (*authv1.CreateAPIKeyResponse, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_CreateAPIKey_FullMethodName)
}

func (s *LegacyAuthService) ListAPIKeys(ctx context.Context, _ *authv1.ListAPIKeysRequest) (*authv1.ListAPIKeysResponse, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_ListAPIKeys_FullMethodName)
}

func (s *LegacyAuthService) RevokeAPIKey(ctx context.Context, _ *authv1.RevokeAPIKeyRequest) (*emptypb.Empty, error) {
	return nil, legacyNotImplemented(ctx, authv1.AuthService_RevokeAPIKey_FullMethodName)
}

func legacyNotImplemented(ctx context.Context, fullMethod string) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	st := status.New(codes.Unimplemented, "legacy Auth behavior is not implemented in CP0-1")
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: legacyNotImplementedReason,
		Domain: legacyErrorDomain,
		Metadata: map[string]string{
			"method": fullMethod,
		},
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}
