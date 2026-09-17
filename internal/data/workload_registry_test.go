package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func dataWorkloadRegistryFixture(t *testing.T) *workloadregistry.Registry {
	t.Helper()
	raw, err := os.ReadFile("../../registrations/workload-targets.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	r, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorkloadSourcePolicyCannotOverrideHumanOrOwnerRules(t *testing.T) {
	r := dataWorkloadRegistryFixture(t)
	if _, revision, err := workloadOperationPolicies(r); err != nil || revision != TargetPolicyRevision {
		t.Fatalf("existing policies changed: %v", err)
	}
	doc := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: r.Targets()}
	for i := range doc.Targets {
		if len(doc.Targets[i].Sources) > 0 {
			doc.Targets[i].Sources[0].Action = "delete"
			break
		}
	}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	changed, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := workloadOperationPolicies(changed); err == nil {
		t.Fatal("changed owner permission accepted under an existing operation")
	}
}
