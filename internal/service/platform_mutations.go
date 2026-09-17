package service

import (
	"context"
	"strings"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func platformRolePermissions(values []string) ([]biz.Permission, error) {
	result := make([]biz.Permission, 0, len(values))
	for _, raw := range values {
		resource, action, ok := strings.Cut(raw, "/")
		if !ok || resource == "" || action == "" || strings.Contains(action, "/") || strings.TrimSpace(raw) != raw {
			return nil, invalidArgumentStatus("permissions", "IAM request is invalid")
		}
		result = append(result, biz.Permission{Scope: biz.PermissionScopePlatform, Resource: resource, Action: action})
	}
	return result, nil
}
func (s *IAMAdminService) CreatePlatformRole(ctx context.Context, r *iamv1.CreatePlatformRoleRequest) (*iamv1.CreatePlatformRoleResponse, error) {
	const op = "createPlatformIAMRole"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "CreatePlatformRole", op, "", "PLATFORM_ADMIN_MUTATION")
	if err != nil {
		return nil, err
	}
	permissions, err := platformRolePermissions(r.GetPermissions())
	if err != nil {
		return nil, err
	}
	v, err := s.platform.CreateRole(ctx, cap, biz.CreatePlatformRoleCommand{Code: r.GetCode(), DisplayName: r.GetDisplayName(), Permissions: permissions, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op})
	}
	return &iamv1.CreatePlatformRoleResponse{Role: platformRoleDTO(*v.Role)}, nil
}
func (s *IAMAdminService) UpdatePlatformRole(ctx context.Context, r *iamv1.UpdatePlatformRoleRequest) (*iamv1.UpdatePlatformRoleResponse, error) {
	const op = "updatePlatformIAMRole"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "UpdatePlatformRole", op, r.GetRoleId(), "PLATFORM_ADMIN_MUTATION")
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
	permissions, err := platformRolePermissions(r.GetPermissions())
	if err != nil {
		return nil, err
	}
	v, err := s.platform.UpdateRole(ctx, cap, biz.UpdatePlatformRoleCommand{RoleID: id, DisplayName: r.GetDisplayName(), Permissions: permissions, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.UpdatePlatformRoleResponse{Role: platformRoleDTO(*v.Role)}, nil
}
func (s *IAMAdminService) DeletePlatformRole(ctx context.Context, r *iamv1.DeletePlatformRoleRequest) (*iamv1.DeletePlatformRoleResponse, error) {
	const op = "deletePlatformIAMRole"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "DeletePlatformRole", op, r.GetRoleId(), "PLATFORM_ADMIN_MUTATION")
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
	v, err := s.platform.DeleteRole(ctx, cap, biz.DeletePlatformRoleCommand{RoleID: id, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.DeletePlatformRoleResponse{Result: &iamv1.MutationResult{ResourceId: id.String(), Version: uint64(v.TargetVersion)}}, nil
}
func (s *IAMAdminService) updatePlatformTenantAccess(ctx context.Context, r *iamv1.UpdateTenantAccessRequest) (*iamv1.UpdateTenantAccessResponse, error) {
	const op = "updateTenantAccess"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "UpdateTenantAccess", op, r.GetTenantId(), "PLATFORM_ADMIN_MUTATION")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetTenantId(), "tenant_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	state, err := tenantAccessStatus(r.GetStatus())
	if err != nil {
		return nil, err
	}
	v, err := s.platform.UpdateTenantAccess(ctx, cap, biz.UpdatePlatformTenantAccessCommand{TenantID: id, Status: state, ExpectedVersion: version, IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.UpdateTenantAccessResponse{TenantAccess: tenantAccessDTO(id, *v.Access)}, nil
}
