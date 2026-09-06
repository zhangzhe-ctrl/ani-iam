package data

import (
	"errors"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestTargetOperationRegistryPinsDP202ListInstances(t *testing.T) {
	registry, err := NewTargetOperationRegistry(TargetPolicyRevision)
	if err != nil {
		t.Fatalf("NewTargetOperationRegistry() error = %v", err)
	}
	if registry.Revision() != "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa" {
		t.Fatalf("policy revision = %q", registry.Revision())
	}
	policy, ok := registry.Lookup("listInstances")
	if !ok || policy.OperationID != "listInstances" || policy.Resource != "instances" || len(policy.Actions) != 1 || policy.Actions[0] != "read" {
		t.Fatalf("listInstances policy = %#v, present=%t", policy, ok)
	}
	if _, exists := registry.Lookup("unknownOperation"); exists {
		t.Fatal("unknown operation received a default policy")
	}
}

func TestTargetOperationRegistryRejectsRevisionDrift(t *testing.T) {
	_, err := NewTargetOperationRegistry("sha256:wrong")
	if !errors.Is(err, biz.ErrAuthorizationPolicyMismatch) {
		t.Fatalf("NewTargetOperationRegistry() error = %v", err)
	}
}
