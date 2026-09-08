package data

import (
	"fmt"
	"sort"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type targetOperationRegistry struct {
	revision string
	policies map[string]biz.AuthorizationPolicy
}

func NewTargetOperationRegistry(expectedRevision string) (biz.AuthorizationPolicyRegistry, error) {
	if expectedRevision != TargetPolicyRevision {
		return nil, &biz.AuthorizationPolicyMismatchError{
			Expected: TargetPolicyRevision,
			Actual:   expectedRevision,
		}
	}
	return &targetOperationRegistry{
		revision: TargetPolicyRevision,
		policies: generatedTargetPolicies,
	}, nil
}

type targetPermissionCatalog struct {
	permissions map[biz.Permission]struct{}
}

func NewTargetPermissionCatalog(expectedRevision string) (biz.PermissionCatalog, error) {
	registry, err := NewTargetOperationRegistry(expectedRevision)
	if err != nil {
		return nil, err
	}
	permissions := make(map[biz.Permission]struct{})
	for _, policy := range registry.(*targetOperationRegistry).policies {
		for _, action := range policy.Actions {
			permissions[biz.Permission{Scope: policy.Scope, Resource: policy.Resource, Action: action}] = struct{}{}
		}
	}
	return &targetPermissionCatalog{permissions: permissions}, nil
}

func (c *targetPermissionCatalog) Contains(permission biz.Permission) bool {
	_, ok := c.permissions[permission]
	return ok
}

func (c *targetPermissionCatalog) Permissions(scope biz.PermissionScope) []biz.Permission {
	permissions := make([]biz.Permission, 0, len(c.permissions))
	for permission := range c.permissions {
		if permission.Scope == scope {
			permissions = append(permissions, permission)
		}
	}
	sort.Slice(permissions, func(i, j int) bool {
		if permissions[i].Resource != permissions[j].Resource {
			return permissions[i].Resource < permissions[j].Resource
		}
		return permissions[i].Action < permissions[j].Action
	})
	return permissions
}

func (r *targetOperationRegistry) Revision() string {
	return r.revision
}

func (r *targetOperationRegistry) Lookup(operationID string) (biz.AuthorizationPolicy, bool) {
	policy, ok := r.policies[operationID]
	if !ok {
		return biz.AuthorizationPolicy{}, false
	}
	policy.Actions = append([]string(nil), policy.Actions...)
	policy.Obligations = append([]biz.AuthorizationObligation(nil), policy.Obligations...)
	return policy, true
}

func TargetRegistryIdentity() string {
	return fmt.Sprintf("policy=%s registry_sha256=%s core_openapi_sha256=%s services_openapi_sha256=%s", TargetPolicyRevision, TargetOperationRegistrySHA256, TargetCoreOpenAPISHA256, TargetServicesOpenAPISHA256)
}

var (
	_ biz.AuthorizationPolicyRegistry = (*targetOperationRegistry)(nil)
	_ biz.PermissionCatalog           = (*targetPermissionCatalog)(nil)
)
