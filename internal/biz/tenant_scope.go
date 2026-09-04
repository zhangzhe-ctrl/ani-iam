package biz

import (
	"errors"

	"github.com/google/uuid"
)

// ErrTenantScopeRequired reports that a tenant-owned operation has no trusted
// tenant boundary. It is intentionally framework-independent.
var (
	ErrTenantScopeRequired = errors.New("tenant scope is required")
	ErrTenantScopeMismatch = errors.New("tenant scope does not match transaction boundary")
)

// TenantScope is the non-promotable boundary required by every ordinary
// tenant-owned repository operation. Its tenant identifier is private so a
// scope can only be constructed through NewTenantScope.
type TenantScope struct {
	tenantID uuid.UUID
}

// NewTenantScope constructs a trusted tenant boundary. Authorization layers
// must establish the caller's membership and permission before calling it.
func NewTenantScope(tenantID uuid.UUID) (TenantScope, error) {
	if tenantID == uuid.Nil {
		return TenantScope{}, ErrTenantScopeRequired
	}
	return TenantScope{tenantID: tenantID}, nil
}

// TenantID returns the scope's tenant identifier and rejects the Go zero value.
func (s TenantScope) TenantID() (uuid.UUID, error) {
	if s.tenantID == uuid.Nil {
		return uuid.Nil, ErrTenantScopeRequired
	}
	return s.tenantID, nil
}
