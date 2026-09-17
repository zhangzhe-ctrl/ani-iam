package service

import (
	"context"
	"strings"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type tenantRoleMutations interface {
	Create(context.Context, biz.TenantScope, biz.CreateTenantRoleCommand) (biz.TenantRoleMutationResult, error)
	Update(context.Context, biz.TenantScope, biz.UpdateTenantRoleCommand) (biz.TenantRoleMutationResult, error)
	Delete(context.Context, biz.TenantScope, biz.DeleteTenantRoleCommand) (biz.TenantRoleMutationResult, error)
}

func NewIAMAdministrationService(reader biz.TenantAdminReader, mutations tenantAuthorizationMutations, roles tenantRoleMutations, catalog *biz.PermissionCatalogReader, audit *biz.AuditQueryUsecase, authorization ...*AdminAuthorization) *IAMAdminService {
	s := NewTenantIAMAdminService(reader, mutations, authorization...)
	s.roles = roles
	s.catalog = catalog
	s.audit = audit
	return s
}

func (s *IAMAdminService) CreateTenantRole(ctx context.Context, r *iamv1.CreateTenantRoleRequest) (*iamv1.CreateTenantRoleResponse, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "CreateTenantRole", "createTenantIAMRole", r.GetTenantId(), "")
	if err != nil {
		return nil, err
	}
	if s.roles == nil {
		return nil, status.Error(codes.Unimplemented, "tenant role administration is unavailable")
	}
	scope, tenant, err := trustedTenantScope(ctx, "createTenantIAMRole")
	if err != nil {
		return nil, err
	}
	actor, err := tenantAuthorizationActor(ctx)
	if err != nil {
		return nil, err
	}
	permissions, err := tenantRolePermissions(r.GetPermissions())
	if err != nil {
		return nil, err
	}
	result, err := s.roles.Create(ctx, scope, biz.CreateTenantRoleCommand{Code: r.GetCode(), DisplayName: r.GetDisplayName(), Permissions: permissions, IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "createTenantIAMRole", TenantID: tenant.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.CreateTenantRoleResponse{Role: roleDTO(tenant, result.Role)}, nil
}

func (s *IAMAdminService) UpdateTenantRole(ctx context.Context, r *iamv1.UpdateTenantRoleRequest) (*iamv1.UpdateTenantRoleResponse, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "UpdateTenantRole", "updateTenantIAMRole", r.GetTenantId(), r.GetRoleId())
	if err != nil {
		return nil, err
	}
	if s.roles == nil {
		return nil, status.Error(codes.Unimplemented, "tenant role administration is unavailable")
	}
	scope, tenant, err := trustedTenantScope(ctx, "updateTenantIAMRole")
	if err != nil {
		return nil, err
	}
	actor, err := tenantAuthorizationActor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetRoleId(), "role_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	permissions, err := tenantRolePermissions(r.GetPermissions())
	if err != nil {
		return nil, err
	}
	result, err := s.roles.Update(ctx, scope, biz.UpdateTenantRoleCommand{RoleID: id, DisplayName: r.GetDisplayName(), Permissions: permissions, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "updateTenantIAMRole", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.UpdateTenantRoleResponse{Role: roleDTO(tenant, result.Role)}, nil
}

func (s *IAMAdminService) DeleteTenantRole(ctx context.Context, r *iamv1.DeleteTenantRoleRequest) (*iamv1.DeleteTenantRoleResponse, error) {
	ctx, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "DeleteTenantRole", "deleteTenantIAMRole", r.GetTenantId(), r.GetRoleId())
	if err != nil {
		return nil, err
	}
	if s.roles == nil {
		return nil, status.Error(codes.Unimplemented, "tenant role administration is unavailable")
	}
	scope, tenant, err := trustedTenantScope(ctx, "deleteTenantIAMRole")
	if err != nil {
		return nil, err
	}
	actor, err := tenantAuthorizationActor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetRoleId(), "role_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if s.invitations != nil {
		if err := s.invitations.ExpireForRole(ctx, scope, actor, id); err != nil {
			return nil, mapIAMError(err, errorContext{OperationID: "deleteTenantIAMRole", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
		}
	}
	result, err := s.roles.Delete(ctx, scope, biz.DeleteTenantRoleCommand{RoleID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey(), Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "deleteTenantIAMRole", TenantID: tenant.String(), ResourceID: id.String(), DecisionID: actor.DecisionID})
	}
	return &iamv1.DeleteTenantRoleResponse{Result: &iamv1.MutationResult{ResourceId: id.String(), Version: uint64(result.Role.Version)}}, nil
}

func tenantRolePermissions(values []string) ([]biz.Permission, error) {
	result := make([]biz.Permission, 0, len(values))
	for _, raw := range values {
		resource, action, ok := strings.Cut(raw, "/")
		if !ok || resource == "" || action == "" || strings.Contains(action, "/") || strings.TrimSpace(raw) != raw {
			return nil, invalidArgumentStatus("permissions", "IAM request is invalid")
		}
		result = append(result, biz.Permission{Scope: biz.PermissionScopeTenant, Resource: resource, Action: action})
	}
	return result, nil
}
