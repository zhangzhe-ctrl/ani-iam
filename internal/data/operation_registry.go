package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type targetOperationRegistry struct {
	revision string
	policies map[string]biz.AuthorizationPolicy
}

type ownerTargetPolicy struct {
	Audience, Operation, Method, Path string
	Policy                            biz.AuthorizationPolicy
}

func NewTargetOperationRegistry(expectedRevision string, registries ...*workloadregistry.Registry) (biz.AuthorizationPolicyRegistry, error) {
	var registry *workloadregistry.Registry
	if len(registries) == 1 {
		registry = registries[0]
	}
	policies, revision, err := workloadOperationPolicies(registry)
	if err != nil {
		return nil, err
	}
	if expectedRevision != revision {
		return nil, &biz.AuthorizationPolicyMismatchError{Expected: revision, Actual: expectedRevision}
	}
	return &targetOperationRegistry{revision: revision, policies: policies}, nil
}

// Existing Human policy is immutable. A synchronous declaration may repeat an
// identical source or introduce a new finite Tenant resource permission.
func workloadOperationPolicies(registry *workloadregistry.Registry) (map[string]biz.AuthorizationPolicy, string, error) {
	policies := make(map[string]biz.AuthorizationPolicy, len(generatedTargetPolicies))
	for id, p := range generatedTargetPolicies {
		policies[id] = p
	}
	additions := map[string]biz.AuthorizationPolicy{}
	canonical := func(p biz.AuthorizationPolicy) biz.AuthorizationPolicy {
		p.Actions = slices.Clone(p.Actions)
		sort.Strings(p.Actions)
		p.PrincipalKinds = slices.Clone(p.PrincipalKinds)
		slices.Sort(p.PrincipalKinds)
		p.CredentialKinds = slices.Clone(p.CredentialKinds)
		slices.Sort(p.CredentialKinds)
		return p
	}
	for _, owner := range generatedOwnerTargetPolicies {
		target, exists := registry.Lookup(owner.Audience, owner.Operation)
		if !exists || !target.Enabled {
			continue
		}
		if target.Mechanism != workloadregistry.WorkloadOnly || target.HTTPMethod != owner.Method || target.HTTPPath != owner.Path || target.RPC != "" {
			return nil, "", biz.ErrAuthorizationOperationUnregistered
		}
		p := canonical(owner.Policy)
		if _, collision := policies[p.OperationID]; collision {
			return nil, "", biz.ErrAuthorizationOperationUnregistered
		}
		policies[p.OperationID], additions[p.OperationID] = p, p
	}
	for _, target := range registry.Targets() {
		for _, source := range target.Sources {
			p := biz.AuthorizationPolicy{OperationID: source.Operation, Scope: biz.PermissionScopeTenant, Resource: source.Resource, Actions: []string{source.Action}, Obligations: []biz.AuthorizationObligation{{Type: biz.AuthorizationObligationResourceTenantMatch, Handler: source.OwnerHandler}}}
			for _, kind := range source.PrincipalKinds {
				p.PrincipalKinds = append(p.PrincipalKinds, biz.PrincipalType(kind))
			}
			for _, kind := range source.CredentialKinds {
				p.CredentialKinds = append(p.CredentialKinds, biz.CredentialKind(kind))
			}
			if existing, ok := policies[p.OperationID]; ok {
				if !reflect.DeepEqual(canonical(existing), canonical(p)) {
					return nil, "", biz.ErrAuthorizationOperationUnregistered
				}
			} else {
				policies[p.OperationID] = p
				additions[p.OperationID] = canonical(p)
			}
		}
	}
	revision := TargetPolicyRevision
	if len(additions) > 0 {
		raw, _ := json.Marshal(additions)
		sum := sha256.Sum256(append([]byte(TargetPolicyRevision+"\x00"), raw...))
		revision = "sha256:" + hex.EncodeToString(sum[:])
	}
	return policies, revision, nil
}

type targetPermissionCatalog struct {
	permissions map[biz.Permission]struct{}
}

func NewTargetPermissionCatalog(expectedRevision string, registries ...*workloadregistry.Registry) (biz.PermissionCatalog, error) {
	registry, err := NewTargetOperationRegistry(expectedRevision, registries...)
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
	policy.CredentialKinds = append([]biz.CredentialKind(nil), policy.CredentialKinds...)
	policy.PrincipalKinds = append([]biz.PrincipalType(nil), policy.PrincipalKinds...)
	return policy, true
}

func TargetRegistryIdentity() string {
	return fmt.Sprintf("policy=%s registry_sha256=%s core_openapi_sha256=%s services_openapi_sha256=%s", TargetPolicyRevision, TargetOperationRegistrySHA256, TargetCoreOpenAPISHA256, TargetServicesOpenAPISHA256)
}

var (
	_ biz.AuthorizationPolicyRegistry = (*targetOperationRegistry)(nil)
	_ biz.PermissionCatalog           = (*targetPermissionCatalog)(nil)
)

// WorkloadPolicyRevision changes only when the set of source permissions changes.
func WorkloadPolicyRevision(registry *workloadregistry.Registry) (string, error) {
	_, revision, err := workloadOperationPolicies(registry)
	return revision, err
}
