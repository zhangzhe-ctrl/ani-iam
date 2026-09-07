package service

import (
	"context"

	"github.com/google/uuid"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type authorizationUsecase interface {
	CheckPermission(context.Context, biz.CheckPermissionCommand) (biz.AuthorizationDecision, error)
}

type AuthorizationService struct {
	iamv1.UnimplementedAuthorizationServiceServer
	authorization authorizationUsecase
}

func NewAuthorizationService(authorization authorizationUsecase) *AuthorizationService {
	return &AuthorizationService{authorization: authorization}
}

func (s *AuthorizationService) CheckPermission(ctx context.Context, request *iamv1.CheckPermissionRequest) (*iamv1.CheckPermissionResponse, error) {
	if request == nil || request.GetTarget() == nil {
		return nil, invalidArgumentStatus("target", "authorization target is required")
	}
	tenantID, err := uuid.Parse(request.GetTarget().GetTenantId())
	if err != nil || tenantID == uuid.Nil {
		return nil, invalidArgumentStatus("target.tenant_id", "authorization target tenant is invalid")
	}
	credential := ""
	if request.GetCredential() != nil {
		credential = request.GetCredential().GetValue()
	}
	decision, err := s.authorization.CheckPermission(ctx, biz.CheckPermissionCommand{
		RawCredential:  credential,
		OperationID:    request.GetOperationId(),
		PolicyRevision: request.GetPolicyRevision(),
		TargetTenantID: tenantID,
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{
			OperationID:          request.GetOperationId(),
			TenantID:             tenantID.String(),
			CredentialKind:       "bearer",
			Dependency:           "authorization",
			ActualPolicyRevision: request.GetPolicyRevision(),
		})
	}
	return &iamv1.CheckPermissionResponse{Decision: authorizationDecisionToProto(decision)}, nil
}

func authorizationDecisionToProto(decision biz.AuthorizationDecision) *iamv1.AuthorizationDecision {
	return &iamv1.AuthorizationDecision{
		Allowed:        decision.Allowed,
		Reason:         string(decision.Reason),
		DecisionId:     decision.DecisionID.String(),
		Principal:      trustedPrincipalToProto(decision.Principal),
		PolicyRevision: decision.PolicyRevision,
	}
}

func trustedPrincipalToProto(principal biz.TrustedPrincipalContext) *iamv1.PrincipalContext {
	authnMethods := authnMethodsToProto(principal.AuthnMethods)
	return &iamv1.PrincipalContext{
		PrincipalId:     principal.ID.String(),
		PrincipalType:   iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN,
		PrincipalStatus: principalStatusToProto(principal.Status),
		Boundary:        tenantBoundary(principal.TenantID),
		SessionId:       principal.SessionID.String(),
		GrantId:         principal.GrantID.String(),
		AuthnMethods:    authnMethods,
	}
}

func authnMethodsToProto(methods []biz.AuditAuthenticationMethod) []iamv1.AuthnMethod {
	authnMethods := make([]iamv1.AuthnMethod, 0, len(methods))
	for _, method := range methods {
		switch method {
		case biz.AuditAuthenticationMethodPassword:
			authnMethods = append(authnMethods, iamv1.AuthnMethod_AUTHN_METHOD_PASSWORD)
		case biz.AuditAuthenticationMethodOIDC:
			authnMethods = append(authnMethods, iamv1.AuthnMethod_AUTHN_METHOD_OIDC)
		case biz.AuditAuthenticationMethodAPIKey:
			authnMethods = append(authnMethods, iamv1.AuthnMethod_AUTHN_METHOD_API_KEY)
		case biz.AuditAuthenticationMethodServiceToken:
			authnMethods = append(authnMethods, iamv1.AuthnMethod_AUTHN_METHOD_SERVICE_TOKEN)
		}
	}
	return authnMethods
}

var _ iamv1.AuthorizationServiceServer = (*AuthorizationService)(nil)
