package service

import (
	"context"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (s *IAMAdminService) ListTenantPermissions(ctx context.Context, r *iamv1.ListTenantPermissionsRequest) (*iamv1.PermissionCatalogResponse, error) {
	_, err := s.authorizeTenantAdmin(ctx, r.GetCredential(), "ListTenantPermissions", "listTenantIAMPermissions", r.GetTenantId(), "")
	if err != nil {
		return nil, err
	}
	page, err := s.catalog.List(biz.PermissionScopeTenant, r.GetPage().GetCursor(), r.GetPage().GetPageSize())
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listTenantIAMPermissions"})
	}
	return catalogDTO(page), nil
}

func catalogDTO(page biz.PermissionCatalogPage) *iamv1.PermissionCatalogResponse {
	values := make([]string, 0, len(page.Permissions))
	for _, p := range page.Permissions {
		values = append(values, p.Resource+"/"+p.Action)
	}
	return &iamv1.PermissionCatalogResponse{Permissions: values, NextCursor: page.NextCursor, PolicyRevision: page.PolicyRevision}
}
