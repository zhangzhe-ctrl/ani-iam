package biz

import (
	"github.com/google/uuid"
	"testing"
)

func TestInferenceBindingAcceptsOnlyFrozenChatTarget(t *testing.T) {
	b := InvocationBinding{Audience: InferenceInvocationAudience, Operation: InferenceInvocationOperation, RPCMethod: InferenceInvocationRPC, SourceOperation: InferenceSourceOperation, TenantID: uuid.New(), SubjectID: uuid.New(), ResourceID: uuid.NewString(), Mode: "chat_completions", RequestSHA256: [32]byte{1}, PolicyRevision: "revision"}
	b.TargetRevision = workloadRegistryFixture(t).Revision(b.Audience, b.Operation)
	if _, _, err := ValidateRegisteredInvocation(workloadRegistryFixture(t), b); err != nil {
		t.Fatal("frozen Inference binding rejected")
	}
	for _, change := range []func(*InvocationBinding){func(b *InvocationBinding) { b.Audience = SessionInvocationAudience }, func(b *InvocationBinding) { b.Operation = "inference.*" }, func(b *InvocationBinding) { b.SourceOperation = "invokeInferenceResponses" }, func(b *InvocationBinding) { b.RPCMethod = "/other.Service/CheckInferenceAccess" }, func(b *InvocationBinding) { b.Mode = "embeddings" }, func(b *InvocationBinding) { b.SubjectID = uuid.Nil }, func(b *InvocationBinding) { b.TenantID = uuid.Nil }, func(b *InvocationBinding) { b.ResourceID = "" }, func(b *InvocationBinding) { b.RequestSHA256 = [32]byte{} }} {
		copy := b
		change(&copy)
		if _, _, err := ValidateRegisteredInvocation(workloadRegistryFixture(t), copy); err == nil {
			t.Fatal("unfrozen binding accepted")
		}
	}
}
