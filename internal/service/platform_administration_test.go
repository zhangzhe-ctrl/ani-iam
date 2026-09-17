package service

import (
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestPlatformTargetTenantAccessMissingUsesResourceNotFound(t *testing.T) {
	for _, tc := range []struct {
		operation string
		want      codes.Code
	}{{"getTenantAccess", codes.NotFound}, {"updateTenantAccess", codes.NotFound}, {"passwordLogin", codes.Unavailable}} {
		if got := status.Code(mapIAMError(biz.ErrTenantAccessNotFound, errorContext{OperationID: tc.operation})); got != tc.want {
			t.Errorf("%s: code=%s want=%s", tc.operation, got, tc.want)
		}
	}
}
