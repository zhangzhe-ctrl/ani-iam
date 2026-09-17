package biz

import (
	"errors"
	"testing"
)

type catalogPageFixture struct{}

func (catalogPageFixture) Contains(Permission) bool { return true }
func (catalogPageFixture) Permissions(scope PermissionScope) []Permission {
	return []Permission{{Scope: scope, Resource: "z", Action: "read"}, {Scope: scope, Resource: "a", Action: "read"}}
}

func TestPermissionCatalogCursorIsBoundaryAndRevisionBound(t *testing.T) {
	r := NewPermissionCatalogReader(catalogPageFixture{}, "fixed-revision")
	first, err := r.List(PermissionScopeTenant, "", 1)
	if err != nil || len(first.Permissions) != 1 || first.Permissions[0].Resource != "a" || first.NextCursor == "" {
		t.Fatal("first page invalid")
	}
	second, err := r.List(PermissionScopeTenant, first.NextCursor, 1)
	if err != nil || len(second.Permissions) != 1 || second.Permissions[0].Resource != "z" || second.NextCursor != "" {
		t.Fatal("second page invalid")
	}
	for _, c := range []struct {
		reader *PermissionCatalogReader
		scope  PermissionScope
		cursor string
		limit  uint32
	}{{r, PermissionScopePlatform, first.NextCursor, 1}, {NewPermissionCatalogReader(catalogPageFixture{}, "other"), PermissionScopeTenant, first.NextCursor, 1}, {r, PermissionScopeTenant, "broken", 1}, {r, PermissionScopeTenant, "", 101}, {r, "", "", 1}} {
		if _, err := c.reader.List(c.scope, c.cursor, c.limit); !errors.Is(err, ErrPermissionCatalogPage) {
			t.Fatal("invalid catalog page accepted")
		}
	}
}
