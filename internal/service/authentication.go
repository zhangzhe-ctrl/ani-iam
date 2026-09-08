package service

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type authenticationUsecase interface {
	PasswordLogin(context.Context, biz.PasswordLoginCommand) (biz.PasswordLoginResult, error)
	ValidatePrincipal(context.Context, biz.ValidatePrincipalCommand) (biz.ValidatePrincipalResult, error)
	RequestPasswordAction(context.Context, biz.RequestPasswordActionCommand) (biz.RequestPasswordActionResult, error)
	CompletePasswordAction(context.Context, biz.CompletePasswordActionCommand) (biz.CompletePasswordActionResult, error)
	RefreshSession(context.Context, biz.RefreshSessionCommand) (biz.RefreshSessionResult, error)
	LogoutSession(context.Context, biz.LogoutSessionCommand) (biz.LogoutSessionResult, error)
	SwitchTenant(context.Context, biz.SwitchTenantCommand) (biz.SwitchTenantResult, error)
}

type oidcUsecase interface {
	BeginLogin(context.Context, biz.BeginOIDCLoginCommand) (biz.BeginOIDCLoginResult, error)
	CompleteLogin(context.Context, biz.CompleteOIDCLoginCommand) (biz.LoginResult, error)
	BeginIdentityLink(context.Context, biz.BeginOIDCIdentityLinkCommand) (biz.BeginOIDCIdentityLinkResult, error)
	CompleteIdentityLink(context.Context, biz.CompleteOIDCIdentityLinkCommand) (biz.OIDCIdentityLinkResult, error)
}

func (s *AuthenticationService) CompleteOIDCIdentityLink(ctx context.Context, request *iamv1.CompleteOIDCIdentityLinkRequest) (*iamv1.CompleteOIDCIdentityLinkResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "OIDC identity-link completion request is required")
	}
	if s.oidc == nil {
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{"dependency": "oidc"})
	}
	credential := ""
	if request.GetCredential() != nil {
		credential = request.GetCredential().GetValue()
	}
	result, err := s.oidc.CompleteIdentityLink(ctx, biz.CompleteOIDCIdentityLinkCommand{
		RawCredential: credential, Code: request.GetCode(), State: request.GetState(), RedirectURI: request.GetRedirectUri(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "completeOIDCIdentityLink", CredentialKind: "oidc", Dependency: "oidc",
		})
	}
	return &iamv1.CompleteOIDCIdentityLinkResponse{
		IdentityId: result.IdentityID.String(), PrincipalId: result.PrincipalID.String(),
	}, nil
}

func (s *AuthenticationService) BeginOIDCIdentityLink(ctx context.Context, request *iamv1.BeginOIDCIdentityLinkRequest) (*iamv1.BeginOIDCIdentityLinkResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "OIDC identity-link request is required")
	}
	if s.oidc == nil {
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{"dependency": "oidc"})
	}
	credential := ""
	if request.GetCredential() != nil {
		credential = request.GetCredential().GetValue()
	}
	result, err := s.oidc.BeginIdentityLink(ctx, biz.BeginOIDCIdentityLinkCommand{
		RawCredential: credential, Provider: request.GetProvider(), RedirectURI: request.GetRedirectUri(), IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "beginOIDCIdentityLink", IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			CredentialKind: "bearer", Dependency: "oidc",
		})
	}
	return &iamv1.BeginOIDCIdentityLinkResponse{
		AuthorizationUrl: result.AuthorizationURL, State: result.State, ExpiresAt: timestamppb.New(result.ExpiresAt),
	}, nil
}

type AuthenticationService struct {
	iamv1.UnimplementedAuthenticationServiceServer
	authentication authenticationUsecase
	oidc           oidcUsecase
}

func NewAuthenticationService(authentication authenticationUsecase, oidc ...oidcUsecase) *AuthenticationService {
	service := &AuthenticationService{authentication: authentication}
	if len(oidc) == 1 {
		service.oidc = oidc[0]
	}
	return service
}

func (s *AuthenticationService) ValidatePrincipal(ctx context.Context, request *iamv1.ValidatePrincipalRequest) (*iamv1.ValidatePrincipalResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "principal validation request is required")
	}
	credential := ""
	if request.GetCredential() != nil {
		credential = request.GetCredential().GetValue()
	}
	credentialKind := "bearer"
	if strings.HasPrefix(strings.TrimSpace(credential), "ani_") {
		credentialKind = "api_key"
	}
	requestID, correlationID := principalValidationAuditIdentifiers(ctx)
	result, err := s.authentication.ValidatePrincipal(ctx, biz.ValidatePrincipalCommand{
		RawCredential: credential, OperationID: request.GetOperationId(), PolicyRevision: request.GetPolicyRevision(),
		RequestID: requestID, CorrelationID: correlationID,
	})
	if err != nil {
		tenantID := ""
		if boundTenantID, ok := biz.TenantIDFromAuthenticationError(err); ok {
			tenantID = boundTenantID.String()
		}
		return nil, mapIAMError(err, errorContext{
			OperationID: request.GetOperationId(), CredentialKind: credentialKind,
			Dependency: "authentication", ActualPolicyRevision: request.GetPolicyRevision(), TenantID: tenantID,
		})
	}
	return &iamv1.ValidatePrincipalResponse{
		Principal: trustedPrincipalToProto(result.Principal), DecisionId: result.DecisionID.String(), PolicyRevision: result.PolicyRevision,
	}, nil
}

func principalValidationAuditIdentifiers(ctx context.Context) (string, string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	requestID, _ := exactlyOneMetadataValue(md, "x-request-id")
	correlationID, _ := exactlyOneMetadataValue(md, "x-correlation-id")
	return requestID, correlationID
}

func (s *AuthenticationService) PasswordLogin(ctx context.Context, request *iamv1.PasswordLoginRequest) (*iamv1.PasswordLoginResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "password login request is required")
	}
	audience, err := audienceFromProto(request.GetAudience())
	if err != nil {
		return nil, err
	}
	if audience == biz.AudienceBoss {
		if request.GetBoundary() == nil || request.GetBoundary().GetPlatform() == nil {
			return nil, invalidArgumentStatus("boundary", "BOSS audience requires a platform boundary")
		}
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "BOSS authentication is unavailable", map[string]string{
			"dependency": "platform_authentication",
		})
	}
	tenantID, err := tenantIDFromBoundary(request.GetBoundary())
	if err != nil {
		return nil, err
	}
	sourceIP, err := netip.ParseAddr(strings.TrimSpace(request.GetSourceIp()))
	if err != nil {
		return nil, invalidArgumentStatus("source_ip", "source IP is required and must be a valid IP address")
	}
	result, err := s.authentication.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        request.GetAccount(),
		Password:       request.GetPassword(),
		Audience:       audience,
		TenantID:       tenantID,
		SourceIP:       sourceIP.Unmap(),
		DeviceName:     request.GetDeviceName(),
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID:    "passwordLogin",
			TenantID:       tenantID.String(),
			CredentialKind: "password",
			Dependency:     "authentication",
		})
	}
	return loginResponse(result, tenantID), nil
}

func (s *AuthenticationService) RefreshSession(ctx context.Context, request *iamv1.RefreshSessionRequest) (*iamv1.RefreshSessionResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "refresh request is required")
	}
	result, err := s.authentication.RefreshSession(ctx, biz.RefreshSessionCommand{
		RefreshToken: request.GetRefreshToken(), CSRFToken: request.GetCsrfToken(),
		Origin: request.GetOrigin(), IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "refreshSession", IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			CredentialKind: "refresh_token", Dependency: "authentication",
		})
	}
	return &iamv1.RefreshSessionResponse{Login: loginResponse(result, result.TenantID)}, nil
}

func (s *AuthenticationService) LogoutSession(ctx context.Context, request *iamv1.LogoutSessionRequest) (*iamv1.LogoutSessionResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "logout request is required")
	}
	_, err := s.authentication.LogoutSession(ctx, biz.LogoutSessionCommand{
		RefreshToken: request.GetRefreshToken(), CSRFToken: request.GetCsrfToken(),
		Origin: request.GetOrigin(), IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "logoutSession", IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			CredentialKind: "refresh_token", Dependency: "authentication",
		})
	}
	// The public Gateway maps this response to 204. Keeping the internal result
	// empty also avoids distinguishing unknown from already-revoked credentials.
	return &iamv1.LogoutSessionResponse{Result: &iamv1.MutationResult{}}, nil
}

func (s *AuthenticationService) SwitchTenant(ctx context.Context, request *iamv1.SwitchTenantRequest) (*iamv1.SwitchTenantResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "tenant-switch request is required")
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(request.GetTenantId()))
	if err != nil || tenantID == uuid.Nil {
		return nil, invalidArgumentStatus("tenant_id", "target tenant is invalid")
	}
	credential := ""
	if request.GetCredential() != nil {
		credential = request.GetCredential().GetValue()
	}
	result, err := s.authentication.SwitchTenant(ctx, biz.SwitchTenantCommand{
		RawCredential: credential, TargetTenantID: tenantID, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "switchTenant", IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			TenantID: tenantID.String(), CredentialKind: "bearer", Dependency: "authentication",
		})
	}
	return &iamv1.SwitchTenantResponse{Login: loginResponse(result, tenantID)}, nil
}

func (s *AuthenticationService) CompleteOIDCLogin(ctx context.Context, request *iamv1.CompleteOIDCLoginRequest) (*iamv1.CompleteOIDCLoginResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "OIDC login completion request is required")
	}
	if s.oidc == nil {
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{"dependency": "oidc"})
	}
	result, err := s.oidc.CompleteLogin(ctx, biz.CompleteOIDCLoginCommand{
		Code: request.GetCode(), State: request.GetState(), RedirectURI: request.GetRedirectUri(), DeviceName: request.GetDeviceName(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "completeOIDCLogin", CredentialKind: "oidc", Dependency: "oidc"})
	}
	if result.TenantID == uuid.Nil {
		return nil, mapIAMError(biz.ErrOIDCDependency, errorContext{OperationID: "completeOIDCLogin", Dependency: "oidc"})
	}
	return &iamv1.CompleteOIDCLoginResponse{
		Login: loginResponse(result, result.TenantID),
	}, nil
}

func (s *AuthenticationService) BeginOIDCLogin(ctx context.Context, request *iamv1.BeginOIDCLoginRequest) (*iamv1.BeginOIDCLoginResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "OIDC login request is required")
	}
	if s.oidc == nil {
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{"dependency": "oidc"})
	}
	audience, err := audienceFromProto(request.GetAudience())
	if err != nil {
		return nil, err
	}
	tenantID, err := tenantIDFromBoundary(request.GetBoundary())
	if err != nil {
		return nil, err
	}
	result, err := s.oidc.BeginLogin(ctx, biz.BeginOIDCLoginCommand{
		Audience: audience, TenantID: tenantID, RedirectURI: request.GetRedirectUri(), IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID: "beginOIDCLogin", IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			TenantID: tenantID.String(), CredentialKind: "oidc", Dependency: "oidc",
		})
	}
	return &iamv1.BeginOIDCLoginResponse{
		AuthorizationUrl: result.AuthorizationURL, State: result.State, ExpiresAt: timestamppb.New(result.ExpiresAt),
	}, nil
}

func (s *AuthenticationService) RequestPasswordAction(ctx context.Context, request *iamv1.RequestPasswordActionRequest) (*iamv1.RequestPasswordActionResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "password action request is required")
	}
	audience, err := audienceFromProto(request.GetAudience())
	if err != nil {
		return nil, err
	}
	result, err := s.authentication.RequestPasswordAction(ctx, biz.RequestPasswordActionCommand{
		Account:        request.GetAccount(),
		Audience:       audience,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID:    "requestPasswordAction",
			IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			CredentialKind: "password_action",
			Dependency:     "authentication",
		})
	}
	return &iamv1.RequestPasswordActionResponse{
		OperationId: result.OperationID.String(),
		ExpiresAt:   timestamppb.New(result.ExpiresAt),
	}, nil
}

func (s *AuthenticationService) CompletePasswordAction(ctx context.Context, request *iamv1.CompletePasswordActionRequest) (*iamv1.CompletePasswordActionResponse, error) {
	if request == nil {
		return nil, invalidArgumentStatus("request", "password action completion request is required")
	}
	result, err := s.authentication.CompletePasswordAction(ctx, biz.CompletePasswordActionCommand{
		ActionToken:    request.GetActionToken(),
		NewPassword:    request.GetNewPassword(),
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID:    "completePasswordAction",
			IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()),
			CredentialKind: "password_action",
			Dependency:     "authentication",
		})
	}
	return &iamv1.CompletePasswordActionResponse{Result: &iamv1.MutationResult{
		ResourceId: result.PrincipalID.String(),
		Version:    uint64(result.CredentialVersion),
	}}, nil
}

func loginResponse(result biz.LoginResult, tenantID uuid.UUID) *iamv1.PasswordLoginResponse {
	boundary := tenantBoundary(tenantID)
	authnMethods := authnMethodsToProto(result.Session.AuthnMethods)
	grant := &iamv1.SessionGrantSummary{
		GrantId:  result.Grant.ID.String(),
		Boundary: boundary,
		Version:  uint64(result.Grant.Version),
		Status:   grantStatusToProto(result.Grant.Status),
	}
	issuedAt := result.Session.UpdatedAt
	if issuedAt.IsZero() {
		issuedAt = result.Session.CreatedAt
	}
	expiresIn := result.AccessTokenExpiresAt.Sub(issuedAt)
	if expiresIn < 0 {
		expiresIn = 0
	}
	return &iamv1.PasswordLoginResponse{
		AccessToken:      result.AccessToken,
		ExpiresInSeconds: uint32(expiresIn / time.Second),
		Principal: &iamv1.PrincipalContext{
			PrincipalId:     result.Principal.ID.String(),
			PrincipalType:   iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN,
			PrincipalStatus: principalStatusToProto(result.Principal.Status),
			Boundary:        boundary,
			SessionId:       result.Session.ID.String(),
			GrantId:         result.Grant.ID.String(),
			AuthnMethods:    authnMethods,
		},
		Session: &iamv1.SessionSummary{
			SessionId:         result.Session.ID.String(),
			Status:            sessionStatusToProto(result.Session.Status),
			Grants:            []*iamv1.SessionGrantSummary{grant},
			AuthnMethods:      authnMethods,
			DeviceName:        result.Session.DeviceName,
			CreatedAt:         timestamppb.New(result.Session.CreatedAt),
			IdleExpiresAt:     timestamppb.New(result.Session.IdleExpiresAt),
			AbsoluteExpiresAt: timestamppb.New(result.Session.AbsoluteExpiry),
		},
		Grant:            grant,
		RefreshToken:     result.RefreshToken,
		RefreshExpiresAt: timestamppb.New(result.Session.AbsoluteExpiry),
	}
}

func tenantIDFromBoundary(boundary *iamv1.Boundary) (uuid.UUID, error) {
	if boundary == nil || boundary.GetTenant() == nil {
		return uuid.Nil, invalidArgumentStatus("boundary", "tenant boundary is required")
	}
	tenantID, err := uuid.Parse(boundary.GetTenant().GetTenantId())
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, invalidArgumentStatus("boundary", "tenant boundary is invalid")
	}
	return tenantID, nil
}

func tenantBoundary(tenantID uuid.UUID) *iamv1.Boundary {
	return &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenantID.String()}}}
}

func audienceFromProto(audience iamv1.Audience) (biz.Audience, error) {
	switch audience {
	case iamv1.Audience_AUDIENCE_CONSOLE:
		return biz.AudienceConsole, nil
	case iamv1.Audience_AUDIENCE_BOSS:
		return biz.AudienceBoss, nil
	default:
		return "", invalidArgumentStatus("audience", "audience is invalid")
	}
}

func principalStatusToProto(value biz.PrincipalStatus) iamv1.PrincipalStatus {
	if value == biz.PrincipalStatusActive {
		return iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE
	}
	return iamv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED
}

func sessionStatusToProto(value biz.SessionStatus) iamv1.SessionStatus {
	if value == biz.SessionStatusActive {
		return iamv1.SessionStatus_SESSION_STATUS_ACTIVE
	}
	return iamv1.SessionStatus_SESSION_STATUS_REVOKED
}

func grantStatusToProto(value biz.GrantStatus) iamv1.GrantStatus {
	if value == biz.GrantStatusActive {
		return iamv1.GrantStatus_GRANT_STATUS_ACTIVE
	}
	return iamv1.GrantStatus_GRANT_STATUS_REVOKED
}

type errorContext struct {
	OperationID            string
	IdempotencyKey         string
	TenantID               string
	CredentialKind         string
	Dependency             string
	DecisionID             string
	ExpectedPolicyRevision string
	ActualPolicyRevision   string
	ResourceType           string
	ResourceID             string
}

func mapIAMError(err error, details errorContext) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return newIAMStatus(codes.DeadlineExceeded, "IAM_TIMEOUT", "IAM operation timed out", map[string]string{
			"operation_id": details.OperationID,
		})
	}
	if errors.Is(err, biz.ErrAuthenticationRateLimited) {
		limitScope := "password_account"
		retryAfterSeconds := int64(1)
		var rateLimit *biz.AuthenticationRateLimitError
		if errors.As(err, &rateLimit) {
			if value := strings.TrimSpace(rateLimit.LimitScope); value != "" {
				limitScope = value
			}
			if seconds := int64(rateLimit.RetryAfter / time.Second); seconds > 0 {
				retryAfterSeconds = seconds
			}
		}
		return newIAMStatus(codes.ResourceExhausted, "AUTH_RATE_LIMITED", "authentication rate limit exceeded", map[string]string{
			"limit_scope":         limitScope,
			"retry_after_seconds": strconv.FormatInt(retryAfterSeconds, 10),
		})
	}
	if errors.Is(err, biz.ErrIdempotencyConflict) {
		return newIAMStatus(codes.AlreadyExists, "IDEMPOTENCY_CONFLICT", "idempotency key conflicts with another request", map[string]string{
			"operation_id":    details.OperationID,
			"idempotency_key": details.IdempotencyKey,
		})
	}
	if field := invalidArgumentField(err); field != "" {
		return invalidArgumentStatus(field, "IAM request is invalid")
	}
	if errors.Is(err, biz.ErrInvalidCredential) || errors.Is(err, biz.ErrPasswordActionInvalid) || errors.Is(err, biz.ErrPasswordActionTokenRequired) || errors.Is(err, biz.ErrAuthorizationCredentialInvalid) || errors.Is(err, biz.ErrAuthorizationCredentialRequired) || errors.Is(err, biz.ErrOIDCStateInvalid) || errors.Is(err, biz.ErrOIDCEmailUnverified) {
		credentialKind := details.CredentialKind
		if credentialKind == "" {
			credentialKind = "bearer"
		}
		return newIAMStatus(codes.Unauthenticated, "CREDENTIAL_INVALID", "credential is invalid", map[string]string{
			"credential_kind": credentialKind,
		})
	}
	if errors.Is(err, biz.ErrOIDCReauthenticationRequired) || errors.Is(err, biz.ErrOIDCIdentityConflict) || errors.Is(err, biz.ErrOIDCEmailConflict) || errors.Is(err, biz.ErrAuthenticationCredentialKindDenied) {
		return newIAMStatus(codes.PermissionDenied, "PERMISSION_DENIED", "access is denied", map[string]string{
			"operation_id": details.OperationID,
			"decision_id":  "not-issued",
		})
	}
	if errors.Is(err, biz.ErrServicePrincipalDisabled) {
		decisionID := details.DecisionID
		if decisionID == "" {
			decisionID = "not-issued"
		}
		return newIAMStatus(codes.PermissionDenied, "PERMISSION_DENIED", "access is denied", map[string]string{
			"operation_id": details.OperationID,
			"decision_id":  decisionID,
		})
	}
	if errors.Is(err, biz.ErrAuthorizationOperationUnregistered) {
		return newIAMStatus(codes.Unavailable, "AUTHZ_OPERATION_UNREGISTERED", "authorization operation is not registered", map[string]string{
			"operation_id":    details.OperationID,
			"policy_revision": details.ActualPolicyRevision,
		})
	}
	if errors.Is(err, biz.ErrTenantIAMNotReady) {
		return newIAMStatus(codes.Unavailable, "TENANT_IAM_NOT_READY", "tenant IAM access is not ready", map[string]string{
			"tenant_id": details.TenantID,
		})
	}
	if errors.Is(err, biz.ErrTenantAccessNotFound) {
		return newIAMStatus(codes.Unavailable, "TENANT_IAM_NOT_READY", "tenant IAM access is not ready", map[string]string{
			"tenant_id": details.TenantID,
		})
	}
	if errors.Is(err, biz.ErrServicePrincipalNotFound) {
		return newIAMStatus(codes.NotFound, "NOT_FOUND", "IAM resource was not found", map[string]string{
			"resource_type": "service_principal",
			"resource_id":   details.ResourceID,
		})
	}
	if errors.Is(err, biz.ErrAPIKeyNotFound) {
		return newIAMStatus(codes.NotFound, "NOT_FOUND", "IAM resource was not found", map[string]string{
			"resource_type": "api_key",
			"resource_id":   details.ResourceID,
		})
	}
	if errors.Is(err, biz.ErrMembershipNotFound) || errors.Is(err, biz.ErrRoleNotFound) || errors.Is(err, biz.ErrRoleBindingNotFound) {
		return newIAMStatus(codes.NotFound, "NOT_FOUND", "IAM resource was not found", map[string]string{
			"operation_id": details.OperationID,
		})
	}
	if errors.Is(err, biz.ErrVersionConflict) || errors.Is(err, biz.ErrRoleBindingConflict) || errors.Is(err, biz.ErrServicePrincipalConflict) || errors.Is(err, biz.ErrAPIKeyConflict) {
		return newIAMStatus(codes.Aborted, "VERSION_CONFLICT", "IAM resource version conflicts with current state", map[string]string{
			"resource_id":      details.ResourceID,
			"expected_version": "not_available",
			"actual_version":   "not_available",
		})
	}
	if errors.Is(err, biz.ErrLastTenantAdministrator) {
		return newIAMStatus(codes.PermissionDenied, "PERMISSION_DENIED", "access is denied", map[string]string{
			"operation_id": details.OperationID,
			"decision_id":  details.DecisionID,
		})
	}
	var policyMismatch *biz.AuthorizationPolicyMismatchError
	if errors.As(err, &policyMismatch) {
		return newIAMStatus(codes.Unavailable, "AUTHZ_POLICY_MISMATCH", "authorization policy revision mismatch", map[string]string{
			"expected_policy_revision": policyMismatch.Expected,
			"actual_policy_revision":   policyMismatch.Actual,
		})
	}
	if errors.Is(err, biz.ErrTenantLifecycleStale) {
		return newIAMStatus(codes.Unavailable, "TENANT_LIFECYCLE_STALE", "tenant lifecycle projection is stale", map[string]string{
			"tenant_id":        details.TenantID,
			"expected_version": "not_available",
			"observed_version": "not_available",
		})
	}
	if errors.Is(err, biz.ErrPrincipalInactive) || errors.Is(err, biz.ErrMembershipInactive) || errors.Is(err, biz.ErrTenantAccessInactive) || errors.Is(err, biz.ErrTenantLifecycleBlocked) {
		decisionID := details.DecisionID
		if decisionID == "" {
			decisionID = "not-issued"
		}
		return newIAMStatus(codes.PermissionDenied, "PERMISSION_DENIED", "access is denied", map[string]string{
			"operation_id": details.OperationID,
			"decision_id":  decisionID,
		})
	}
	if errors.Is(err, biz.ErrAuthenticationDependency) || errors.Is(err, biz.ErrAuthorizationDependency) || errors.Is(err, biz.ErrOIDCDependency) || errors.Is(err, biz.ErrPersistenceUnavailable) {
		dependency := details.Dependency
		if dependency == "" {
			dependency = "iam"
		}
		return newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{
			"dependency": dependency,
		})
	}
	dependency := details.Dependency
	if dependency == "" {
		dependency = "iam"
	}
	return newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "IAM dependency is unavailable", map[string]string{
		"dependency": dependency,
	})
}

func invalidArgumentField(err error) string {
	switch {
	case errors.Is(err, biz.ErrAccountRequired):
		return "account"
	case errors.Is(err, biz.ErrPasswordRequired):
		return "password"
	case errors.Is(err, biz.ErrAudienceRequired):
		return "audience"
	case errors.Is(err, biz.ErrSourceIPRequired):
		return "source_ip"
	case errors.Is(err, biz.ErrRefreshTokenRequired):
		return "refresh_token"
	case errors.Is(err, biz.ErrCSRFTokenRequired):
		return "csrf_token"
	case errors.Is(err, biz.ErrOriginRequired):
		return "origin"
	case errors.Is(err, biz.ErrOIDCRedirectInvalid):
		return "redirect_uri"
	case errors.Is(err, biz.ErrOIDCConfigurationInvalid):
		return "provider"
	case errors.Is(err, biz.ErrNewPasswordRequired):
		return "new_password"
	case errors.Is(err, biz.ErrIdempotencyKeyRequired):
		return "idempotency_key"
	case errors.Is(err, biz.ErrTenantScopeRequired):
		return "boundary"
	case errors.Is(err, biz.ErrAuthorizationOperationRequired):
		return "operation_id"
	case errors.Is(err, biz.ErrAuthorizationPolicyRevisionRequired):
		return "policy_revision"
	case errors.Is(err, biz.ErrAuthorizationTargetRequired):
		return "target.resource_id"
	case errors.Is(err, biz.ErrServicePrincipalNameRequired):
		return "name"
	case errors.Is(err, biz.ErrServicePrincipalRolesRequired):
		return "role_ids"
	case errors.Is(err, biz.ErrAPIKeyExpiryInvalid):
		return "expires_at"
	default:
		return ""
	}
}

func invalidArgumentStatus(field, message string) error {
	return newIAMStatus(codes.InvalidArgument, "INVALID_ARGUMENT", message, map[string]string{
		"field": field,
	})
}

func newIAMStatus(code codes.Code, reason, message string, metadata map[string]string) error {
	grpcStatus := status.New(code, message)
	withDetails, err := grpcStatus.WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   "iam.ani.internal",
		Metadata: metadata,
	})
	if err != nil {
		return grpcStatus.Err()
	}
	return withDetails.Err()
}

var _ iamv1.AuthenticationServiceServer = (*AuthenticationService)(nil)
