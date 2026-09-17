package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func ownerRegistry(t *testing.T, mutate func(*workloadregistry.Target)) *workloadregistry.Registry {
	t.Helper()
	owner := generatedOwnerTargetPolicies[0]
	target := workloadregistry.Target{Audience: owner.Audience, Operation: owner.Operation, HTTPMethod: owner.Method, HTTPPath: owner.Path, Mechanism: workloadregistry.WorkloadOnly, GrantScope: "owner_call", ReceiverOperation: "owner.receive", Enabled: true}
	if mutate != nil {
		mutate(&target)
	}
	doc := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: []workloadregistry.Target{
		{Audience: owner.Audience, Operation: "owner.receive", Mechanism: workloadregistry.Receiver, GrantScope: "owner_receive", Enabled: true}, target,
	}}
	raw, _ := json.Marshal(doc)
	digest := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestOwnerPoliciesRequireExactEnabledRegisteredTarget(t *testing.T) {
	if len(generatedOwnerTargetPolicies) != 5 {
		t.Fatal("owner business inventory drift")
	}
	for _, owner := range generatedOwnerTargetPolicies {
		if len(owner.Policy.PrincipalKinds) != 1 || owner.Policy.PrincipalKinds[0] != biz.PrincipalTypeHuman || len(owner.Policy.CredentialKinds) != 1 || owner.Policy.CredentialKinds[0] != biz.CredentialKindAccessToken {
			t.Fatal("Human-only source drift")
		}
	}
	registered := ownerRegistry(t, nil)
	revision, err := WorkloadPolicyRevision(registered)
	if err != nil || revision == TargetPolicyRevision {
		t.Fatal("owner policy not versioned", err)
	}
	policies, err := NewTargetOperationRegistry(revision, registered)
	if err != nil {
		t.Fatal(err)
	}
	owner := generatedOwnerTargetPolicies[0]
	actual, exists := policies.Lookup(owner.Policy.OperationID)
	if !exists || actual.Scope != owner.Policy.Scope || actual.Resource != owner.Policy.Resource || actual.Actions[0] != owner.Policy.Actions[0] {
		t.Fatal("owner declaration changed", actual)
	}
	if _, err := NewTargetOperationRegistry(TargetPolicyRevision, registered); err == nil {
		t.Fatal("stale policy revision admitted")
	}
	for _, change := range []func(*workloadregistry.Target){
		func(target *workloadregistry.Target) { target.HTTPPath += "/other" },
		func(target *workloadregistry.Target) {
			if target.HTTPMethod == "GET" {
				target.HTTPMethod = "POST"
			} else {
				target.HTTPMethod = "GET"
			}
		},
	} {
		if _, err := WorkloadPolicyRevision(ownerRegistry(t, change)); err == nil {
			t.Fatal("route drift adopted owner authority")
		}
	}
	disabled := ownerRegistry(t, func(target *workloadregistry.Target) { target.Enabled = false })
	revision, err = WorkloadPolicyRevision(disabled)
	if err != nil || revision != TargetPolicyRevision {
		t.Fatal("disabled target extended policy", err)
	}
}
