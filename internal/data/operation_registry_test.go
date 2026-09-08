package data

import (
	"errors"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestTargetOperationRegistryPinsAcceptedANIArtifact(t *testing.T) {
	if TargetPolicyRevision != "sha256:1d5c80b83635e9a152c0edd9e8d1c9b66f5f8962e84cdd4f4701ecc486dd969c" {
		t.Fatalf("TargetPolicyRevision = %q", TargetPolicyRevision)
	}
	if TargetOperationRegistrySHA256 != "742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80" {
		t.Fatalf("TargetOperationRegistrySHA256 = %q", TargetOperationRegistrySHA256)
	}
	registry, err := NewTargetOperationRegistry(TargetPolicyRevision)
	if err != nil {
		t.Fatalf("NewTargetOperationRegistry() error = %v", err)
	}
	if registry.Revision() != TargetPolicyRevision {
		t.Fatalf("policy revision = %q", registry.Revision())
	}
	concrete := registry.(*targetOperationRegistry)
	if len(concrete.policies) != TargetAuthorizedOperationCount {
		t.Fatalf("authorized operation count = %d, want %d", len(concrete.policies), TargetAuthorizedOperationCount)
	}
	policy, ok := registry.Lookup("listInstances")
	if !ok || policy.OperationID != "listInstances" || policy.Resource != "instances" || len(policy.Actions) != 1 || policy.Actions[0] != "read" {
		t.Fatalf("listInstances policy = %#v, present=%t", policy, ok)
	}
	policy, ok = registry.Lookup("getInstance")
	if !ok || policy.OperationID != "getInstance" || policy.Resource != "instances" || len(policy.Actions) != 1 || policy.Actions[0] != "read" {
		t.Fatalf("getInstance policy = %#v, present=%t", policy, ok)
	}
	if len(policy.Obligations) != 1 || policy.Obligations[0].Type != biz.AuthorizationObligationResourceTenantMatch || policy.Obligations[0].Handler != "core.resource_tenant" {
		t.Fatalf("getInstance obligations = %#v", policy.Obligations)
	}
	if _, exists := registry.Lookup("unknownOperation"); exists {
		t.Fatal("unknown operation received a default policy")
	}
	catalog, err := NewTargetPermissionCatalog(TargetPolicyRevision)
	if err != nil {
		t.Fatalf("NewTargetPermissionCatalog() error = %v", err)
	}
	permissions := catalog.Permissions(biz.PermissionScopeTenant)
	if len(permissions) != TargetTenantPermissionCount {
		t.Fatalf("tenant permission count = %d, want %d", len(permissions), TargetTenantPermissionCount)
	}
	if !catalog.Contains(biz.Permission{Scope: biz.PermissionScopeTenant, Resource: "instances", Action: "read"}) {
		t.Fatal("catalog does not contain tenant instances/read")
	}
	if catalog.Contains(biz.Permission{Scope: biz.PermissionScopeTenant, Resource: "instances", Action: "unknown"}) {
		t.Fatal("catalog accepted an unknown permission")
	}
}

func TestTargetOperationRegistryRejectsRevisionDrift(t *testing.T) {
	_, err := NewTargetOperationRegistry("sha256:wrong")
	if !errors.Is(err, biz.ErrAuthorizationPolicyMismatch) {
		t.Fatalf("NewTargetOperationRegistry() error = %v", err)
	}
}

func TestTargetOperationRegistryKeepsSensitiveOperationsOffAPIKeys(t *testing.T) {
	registry, err := NewTargetOperationRegistry(TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	for _, operationID := range []string{"passwordLogin", "requestPasswordAction", "completePasswordAction"} {
		if _, ok := registry.Lookup(operationID); ok {
			t.Fatalf("non-authorization operation %q received a CheckPermission policy", operationID)
		}
	}
	for _, operationID := range []string{
		"createIAMAPIKey",
		"createServicePrincipal",
		"createTenantIAMRole",
		"bindTenantIAMRole",
		"approveRecoveryBootstrap",
		"createPlatformIAMRole",
	} {
		policy, ok := registry.Lookup(operationID)
		if !ok {
			t.Fatalf("sensitive operation %q is missing", operationID)
		}
		if len(policy.CredentialKinds) != 1 || policy.CredentialKinds[0] != biz.CredentialKindAccessToken ||
			len(policy.PrincipalKinds) != 1 || policy.PrincipalKinds[0] != biz.PrincipalTypeHuman {
			t.Fatalf("sensitive operation %q authentication = %#v/%#v", operationID, policy.CredentialKinds, policy.PrincipalKinds)
		}
	}
	policy, ok := registry.Lookup("applyPlatformWorkloadLifecycle")
	if !ok || len(policy.CredentialKinds) != 1 || policy.CredentialKinds[0] != biz.CredentialKindServiceToken ||
		len(policy.PrincipalKinds) != 1 || policy.PrincipalKinds[0] != biz.PrincipalTypeService {
		t.Fatalf("platform-workload authentication = %#v, present=%t", policy, ok)
	}
}
