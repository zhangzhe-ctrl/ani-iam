package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type principalValidator interface {
	ValidatePrincipal(context.Context, biz.ValidatePrincipalCommand) (biz.ValidatePrincipalResult, error)
}

// AdminAuthorization reuses the current IAM authentication and permission
// usecases. No request metadata is an actor, Tenant or authorization decision.
type AdminAuthorization struct {
	validation     principalValidator
	permissions    authorizationUsecase
	policyRevision string
}

func NewAdminAuthorization(validation principalValidator, permissions authorizationUsecase, revision string) *AdminAuthorization {
	return &AdminAuthorization{validation: validation, permissions: permissions, policyRevision: revision}
}

type tenantAdminContext struct {
	operation string
	tenantID  uuid.UUID
	actor     biz.TenantAuthorizationActor
}
type tenantAdminContextKey struct{}

func (s *IAMAdminService) authorizeTenantAdmin(ctx context.Context, credential *iamv1.BearerCredential, rpc, operation, targetTenant, resource string) (context.Context, error) {
	if s.reader == nil && s.mutations == nil {
		return nil, status.Error(codes.Unimplemented, "IAM administration is unavailable")
	}
	caller, ok := biz.DirectCallerFromContext(ctx)
	if !ok || caller.Target != (biz.WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + rpc}) {
		return nil, permissionDeniedStatus(operation, "not-issued")
	}
	a := s.authorization
	if a == nil || a.validation == nil || a.permissions == nil || a.policyRevision == "" {
		return nil, mapIAMError(biz.ErrAuthenticationDependency, errorContext{OperationID: operation, Dependency: "admin_authorization"})
	}
	raw := strings.TrimSpace(credential.GetValue())
	if raw == "" {
		return nil, mapIAMError(biz.ErrInvalidCredential, errorContext{OperationID: operation})
	}
	requestID, correlationID := principalValidationAuditIdentifiers(ctx)
	validated, err := a.validation.ValidatePrincipal(ctx, biz.ValidatePrincipalCommand{
		RawCredential: raw, OperationID: operation, PolicyRevision: a.policyRevision,
		RequestID: requestID, CorrelationID: correlationID,
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: operation, Dependency: "admin_authentication"})
	}
	// Correlation metadata is optional and never grants authority. Use IAM's
	// generated validation decision when the caller supplies no request IDs.
	if requestID == "" {
		requestID = validated.DecisionID.String()
	}
	if correlationID == "" {
		correlationID = requestID
	}
	tenantID := validated.Principal.TenantID
	if tenantID == uuid.Nil || validated.Principal.Type != biz.PrincipalTypeHuman {
		return nil, permissionDeniedStatus(operation, validated.DecisionID.String())
	}
	if targetTenant != "" {
		id, err := requiredUUID(targetTenant, "tenant_id")
		if err != nil {
			return nil, err
		}
		if id != tenantID {
			return nil, permissionDeniedStatus(operation, validated.DecisionID.String())
		}
	}
	decision, err := a.permissions.CheckPermission(ctx, biz.CheckPermissionCommand{
		RawCredential: raw, OperationID: operation, PolicyRevision: a.policyRevision,
		TargetTenantID: tenantID, TargetResourceID: resource,
		RequestID: requestID, CorrelationID: correlationID,
	})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: operation, TenantID: tenantID.String(), Dependency: "admin_authorization"})
	}
	if !decision.Allowed || decision.Principal.Type != biz.PrincipalTypeHuman ||
		decision.Principal.ID != validated.Principal.ID || decision.Principal.TenantID != tenantID || decision.DecisionID == uuid.Nil {
		return nil, permissionDeniedStatus(operation, decision.DecisionID.String())
	}
	var method biz.AuditAuthenticationMethod
	for _, candidate := range decision.Principal.AuthnMethods {
		if candidate == biz.AuditAuthenticationMethodPassword || candidate == biz.AuditAuthenticationMethodOIDC {
			method = candidate
			break
		}
	}
	if method == "" {
		return nil, permissionDeniedStatus(operation, decision.DecisionID.String())
	}
	actor := biz.TenantAuthorizationActor{PrincipalID: decision.Principal.ID, AuthenticationMethod: method,
		RequestID: requestID, CorrelationID: correlationID, DecisionID: decision.DecisionID.String(), DirectCaller: caller}
	return context.WithValue(ctx, tenantAdminContextKey{}, tenantAdminContext{operation: operation, tenantID: tenantID, actor: actor}), nil
}

func tenantAuthorizationActor(ctx context.Context) (biz.TenantAuthorizationActor, error) {
	value, ok := ctx.Value(tenantAdminContextKey{}).(tenantAdminContext)
	if !ok {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditActorRequired
	}
	return value.actor, nil
}

func trustedTenantScope(ctx context.Context, operation string) (biz.TenantScope, uuid.UUID, error) {
	value, ok := ctx.Value(tenantAdminContextKey{}).(tenantAdminContext)
	if !ok || value.operation != operation || value.tenantID == uuid.Nil {
		return biz.TenantScope{}, uuid.Nil, permissionDeniedStatus(operation, "not-issued")
	}
	scope, err := biz.NewTenantScope(value.tenantID)
	return scope, value.tenantID, err
}
