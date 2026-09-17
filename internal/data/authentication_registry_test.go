package data

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func TestAuthenticationReaderUsesDeploymentPolicyRegistry(t *testing.T) {
	path := filepath.Join("..", "..", "registrations", "governance-workload-targets.v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	targets, err := workloadregistry.Load(path, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := WorkloadPolicyRevision(targets)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewTargetOperationRegistry(revision, targets)
	if err != nil {
		t.Fatal(err)
	}
	if revision == TargetPolicyRevision {
		t.Fatal("fixture must add owner policies")
	}
	reader := NewPostgresPasswordLoginReader(NewData(nil), registry)
	if reader.Revision() != registry.Revision() {
		t.Fatalf("authentication reader policy mismatch: got %s want %s", reader.Revision(), registry.Revision())
	}
	for _, operation := range []string{"GetTenant", "getTenantIAMMember", "CreateTenant"} {
		expected, ok := registry.Lookup(operation)
		if !ok {
			t.Fatalf("registered fixture operation %s missing", operation)
		}
		got, ok := reader.Lookup(operation)
		if !ok || got.OperationID != expected.OperationID || got.Resource != expected.Resource || got.Scope != expected.Scope {
			t.Fatalf("reader policy differs for %s: %#v", operation, got)
		}
	}
}

func TestAuthenticationReaderWithoutRegistryFailsClosed(t *testing.T) {
	reader := NewPostgresPasswordLoginReader(NewData(nil), nil)
	if reader.Revision() != "" {
		t.Fatal("missing registry silently selected a policy")
	}
	if _, ok := reader.Lookup("getTenantIAMMember"); ok {
		t.Fatal("missing registry silently authorized a registered operation")
	}
}
