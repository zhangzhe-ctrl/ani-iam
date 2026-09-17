package biz

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

var ErrPermissionCatalogPage = errors.New("permission catalog page is invalid")

type PermissionCatalogPage struct {
	Permissions    []Permission
	NextCursor     string
	PolicyRevision string
}

type PermissionCatalogReader struct {
	catalog  PermissionCatalog
	revision string
}

func NewPermissionCatalogReader(catalog PermissionCatalog, revision string) *PermissionCatalogReader {
	return &PermissionCatalogReader{catalog: catalog, revision: revision}
}

type permissionCatalogCursor struct {
	Scope    PermissionScope `json:"scope"`
	Revision string          `json:"revision"`
	After    string          `json:"after"`
}

// List returns only the generated catalog at one exact authorization boundary.
// A cursor from a different boundary or policy snapshot is never reusable.
func (r *PermissionCatalogReader) List(scope PermissionScope, cursor string, limit uint32) (PermissionCatalogPage, error) {
	if r == nil || r.catalog == nil || r.revision == "" {
		return PermissionCatalogPage{}, ErrAuthenticationDependency
	}
	if scope != PermissionScopeTenant && scope != PermissionScopePlatform || limit > 100 || len(cursor) > 2048 {
		return PermissionCatalogPage{}, ErrPermissionCatalogPage
	}
	if limit == 0 {
		limit = 50
	}
	values := r.catalog.Permissions(scope)
	slices.SortFunc(values, func(a, b Permission) int { return strings.Compare(a.Resource+"/"+a.Action, b.Resource+"/"+b.Action) })
	start := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return PermissionCatalogPage{}, ErrPermissionCatalogPage
		}
		var c permissionCatalogCursor
		if json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.Revision != r.revision || c.After == "" {
			return PermissionCatalogPage{}, ErrPermissionCatalogPage
		}
		position := slices.IndexFunc(values, func(p Permission) bool { return p.Resource+"/"+p.Action == c.After })
		if position < 0 {
			return PermissionCatalogPage{}, ErrPermissionCatalogPage
		}
		start = position + 1
	}
	end := min(start+int(limit), len(values))
	result := PermissionCatalogPage{Permissions: slices.Clone(values[start:end]), PolicyRevision: r.revision}
	if end < len(values) {
		last := values[end-1]
		raw, _ := json.Marshal(permissionCatalogCursor{Scope: scope, Revision: r.revision, After: last.Resource + "/" + last.Action})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return result, nil
}
