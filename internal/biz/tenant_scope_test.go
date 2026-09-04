package biz_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestNewTenantScope(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	scope, err := biz.NewTenantScope(tenantID)
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}

	got, err := scope.TenantID()
	if err != nil {
		t.Fatalf("TenantID() error = %v", err)
	}
	if got != tenantID {
		t.Fatalf("TenantID() = %s, want %s", got, tenantID)
	}
}

func TestTenantScopeRejectsMissingTenant(t *testing.T) {
	t.Parallel()

	if _, err := biz.NewTenantScope(uuid.Nil); !errors.Is(err, biz.ErrTenantScopeRequired) {
		t.Fatalf("NewTenantScope(uuid.Nil) error = %v, want %v", err, biz.ErrTenantScopeRequired)
	}

	var zero biz.TenantScope
	if _, err := zero.TenantID(); !errors.Is(err, biz.ErrTenantScopeRequired) {
		t.Fatalf("zero TenantScope.TenantID() error = %v, want %v", err, biz.ErrTenantScopeRequired)
	}
}
