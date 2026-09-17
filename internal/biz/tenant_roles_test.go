package biz

import (
	"errors"
	"reflect"
	"testing"
)

type roleTestCatalog struct{}

func (roleTestCatalog) Contains(p Permission) bool {
	return p.Scope == PermissionScopeTenant && p.Resource == "instances" && (p.Action == "read" || p.Action == "create")
}
func (roleTestCatalog) Permissions(PermissionScope) []Permission { return nil }

func TestCustomRolePermissionSetIsCataloguedAndCanonical(t *testing.T) {
	u := NewTenantRoleUsecase(nil, roleTestCatalog{}, nil, nil)
	read := Permission{Scope: PermissionScopeTenant, Resource: "instances", Action: "read"}
	create := read
	create.Action = "create"
	input := []Permission{read, create, read}
	got, err := u.validate("Readers", input)
	if err != nil || !reflect.DeepEqual(got, []Permission{create, read}) || input[0] != read {
		t.Fatal("permissions must be canonical without mutating caller input")
	}
	bad := read
	bad.Action = "unknown"
	if _, err := u.validate("Readers", []Permission{bad}); !errors.Is(err, ErrPermissionUncatalogued) {
		t.Fatal("unknown permission accepted")
	}
	bad = read
	bad.Scope = PermissionScopePlatform
	if _, err := u.validate("Readers", []Permission{bad}); !errors.Is(err, ErrTenantRoleBoundaryInvalid) {
		t.Fatal("platform permission accepted")
	}
	if _, err := u.validate("Readers", nil); !errors.Is(err, ErrRoleInvalid) {
		t.Fatal("empty permissions accepted")
	}
}
