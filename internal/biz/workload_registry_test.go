package biz

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func workloadRegistryFixture(t *testing.T) *workloadregistry.Registry {
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

func TestRegisteredInvocationRejectsUnknownTargetAndSourceMixing(t *testing.T) {
	r := workloadRegistryFixture(t)
	base := InvocationBinding{Audience: SessionInvocationAudience, Operation: SessionInvocationOperation, RPCMethod: SessionInvocationRPC, SourceOperation: "createInstanceExecSession", TenantID: uuid.New(), SubjectID: uuid.New(), ResourceID: "resource", Mode: "exec", PolicyRevision: "revision", RequestSHA256: [32]byte{1}}
	base.TargetRevision = r.Revision(base.Audience, base.Operation)
	if _, _, err := ValidateRegisteredInvocation(r, base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*InvocationBinding){
		func(b *InvocationBinding) { b.Audience = "unknown" },
		func(b *InvocationBinding) { b.TargetRevision = "" },
		func(b *InvocationBinding) { b.TargetRevision = "wrong" },
		func(b *InvocationBinding) { b.RPCMethod = InferenceInvocationRPC },
		func(b *InvocationBinding) { b.SourceOperation = InferenceSourceOperation },
		func(b *InvocationBinding) { b.Mode = "vm_console" },
		func(b *InvocationBinding) { b.RequestSHA256 = [32]byte{} },
	} {
		b := base
		change(&b)
		if _, _, err := ValidateRegisteredInvocation(r, b); err == nil {
			t.Fatal("mixed binding accepted")
		}
	}
}
