package service

import (
	"context"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *IAMAdminService) WithPlatformAdministration(u *biz.PlatformAdministrationUsecase, authorization *biz.PlatformAuthorizationUsecase) *IAMAdminService {
	s.platform = u
	s.platformAuthorization = authorization
	return s
}
func (s *IAMAdminService) platformCapability(ctx context.Context, credential *iamv1.BearerCredential, rpc, operation, target string) (biz.PlatformCapability, error) {
	return s.platformCapabilityForReason(ctx, credential, rpc, operation, target, "PLATFORM_ADMIN_READ")
}
func (s *IAMAdminService) platformCapabilityForReason(ctx context.Context, credential *iamv1.BearerCredential, rpc, operation, target, reason string) (biz.PlatformCapability, error) {
	if s.platform == nil || s.platformAuthorization == nil {
		return biz.PlatformCapability{}, status.Error(codes.Unimplemented, "Platform administration is unavailable")
	}
	if s.authorization == nil {
		return biz.PlatformCapability{}, mapIAMError(biz.ErrAuthenticationDependency, errorContext{OperationID: operation})
	}
	request, correlation := principalValidationAuditIdentifiers(ctx)
	c, err := s.platformAuthorization.AuthorizeAdministration(ctx, biz.CheckPermissionCommand{RawCredential: credential.GetValue(), OperationID: operation, PolicyRevision: s.authorization.policyRevision, TargetResourceID: target, RequestID: request, CorrelationID: correlation}, rpc, reason)
	if err != nil {
		return biz.PlatformCapability{}, mapIAMError(err, errorContext{OperationID: operation, Dependency: "platform_administration"})
	}
	return c, nil
}
func (s *IAMAdminService) GetPlatformMembership(ctx context.Context, r *iamv1.GetPlatformMembershipRequest) (*iamv1.GetPlatformMembershipResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "GetPlatformMembership", "getPlatformIAMMember", r.GetMembershipId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetMembershipId(), "membership_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetMembership(ctx, c, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getPlatformIAMMember"})
	}
	return &iamv1.GetPlatformMembershipResponse{Membership: platformMembershipDTO(v)}, nil
}
func (s *IAMAdminService) ListPlatformMemberships(ctx context.Context, r *iamv1.ListPlatformMembershipsRequest) (*iamv1.ListPlatformMembershipsResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "ListPlatformMemberships", "listPlatformIAMMembers", "")
	if err != nil {
		return nil, err
	}
	filter, err := optionalMembershipStatus(r.GetStatus())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listPlatformIAMMembers"})
	}
	v, err := s.platform.ListMemberships(ctx, c, filter, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listPlatformIAMMembers"})
	}
	out := &iamv1.ListPlatformMembershipsResponse{NextCursor: v.NextCursor, Memberships: []*iamv1.Membership{}}
	for _, m := range v.Items {
		out.Memberships = append(out.Memberships, platformMembershipDTO(m))
	}
	return out, nil
}
func (s *IAMAdminService) GetPlatformRole(ctx context.Context, r *iamv1.GetPlatformRoleRequest) (*iamv1.GetPlatformRoleResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "GetPlatformRole", "getPlatformIAMRole", r.GetRoleId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetRoleId(), "role_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetRole(ctx, c, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getPlatformIAMRole"})
	}
	return &iamv1.GetPlatformRoleResponse{Role: platformRoleDTO(v)}, nil
}
func (s *IAMAdminService) ListPlatformRoles(ctx context.Context, r *iamv1.ListPlatformRolesRequest) (*iamv1.ListPlatformRolesResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "ListPlatformRoles", "listPlatformIAMRoles", "")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.ListRoles(ctx, c, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listPlatformIAMRoles"})
	}
	out := &iamv1.ListPlatformRolesResponse{NextCursor: v.NextCursor, Roles: []*iamv1.Role{}}
	for _, role := range v.Items {
		out.Roles = append(out.Roles, platformRoleDTO(role))
	}
	return out, nil
}
func (s *IAMAdminService) ListPlatformPermissions(ctx context.Context, r *iamv1.ListPlatformPermissionsRequest) (*iamv1.PermissionCatalogResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "ListPlatformPermissions", "listPlatformIAMPermissions", "")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.ListPermissions(ctx, c, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listPlatformIAMPermissions"})
	}
	return catalogDTO(v), nil
}
func (s *IAMAdminService) getPlatformTargetTenantAccess(ctx context.Context, r *iamv1.GetTenantAccessRequest) (*iamv1.GetTenantAccessResponse, error) {
	c, err := s.platformCapability(ctx, r.GetCredential(), "GetTenantAccess", "getTenantAccess", r.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetTenantId(), "tenant_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetTenantAccess(ctx, c, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getTenantAccess"})
	}
	return &iamv1.GetTenantAccessResponse{TenantAccess: tenantAccessDTO(id, v)}, nil
}
func platformBoundaryDTO() *iamv1.Boundary {
	return &iamv1.Boundary{Boundary: &iamv1.Boundary_Platform{Platform: &iamv1.PlatformBoundary{}}}
}
func platformMembershipDTO(v biz.PlatformMembership) *iamv1.Membership {
	roles := make([]string, 0, len(v.RoleIDs))
	for _, id := range v.RoleIDs {
		roles = append(roles, id.String())
	}
	return &iamv1.Membership{MembershipId: v.ID.String(), PrincipalId: v.PrincipalID.String(), PrincipalType: iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN, Boundary: platformBoundaryDTO(), Status: membershipStatusDTO(v.Status), RoleIds: roles, Version: uint64(v.Version)}
}
func platformRoleDTO(v biz.PlatformRole) *iamv1.Role {
	permissions := make([]string, 0, len(v.Permissions))
	for _, p := range v.Permissions {
		permissions = append(permissions, p.Resource+"/"+p.Action)
	}
	return &iamv1.Role{RoleId: v.ID.String(), Boundary: platformBoundaryDTO(), Code: v.Code, DisplayName: v.DisplayName, System: v.System, SystemDefinitionVersion: uint64(v.SystemDefinitionVersion), Permissions: permissions, Version: uint64(v.Version)}
}
