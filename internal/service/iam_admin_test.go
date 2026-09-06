package service

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
)

func TestIAMAdminServiceIsRegisteredButOutsideSliceMethodsStayUnimplemented(t *testing.T) {
	service := NewIAMAdminService()
	_, err := service.GetTenantAccess(context.Background(), &iamv1.GetTenantAccessRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetTenantAccess() code = %s, want Unimplemented", status.Code(err))
	}
}
