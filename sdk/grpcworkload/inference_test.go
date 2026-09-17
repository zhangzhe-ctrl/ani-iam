package grpcworkload

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
	"time"
)

type inferenceDecisionClient struct {
	iamv1.AuthorizationServiceClient
	decision *iamv1.AuthorizationDecision
	calls    int
	err      error
}

func (f *inferenceDecisionClient) CheckPermission(context.Context, *iamv1.CheckPermissionRequest, ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	f.calls++
	return &iamv1.CheckPermissionResponse{Decision: f.decision}, f.err
}
func inferenceDecision() *iamv1.AuthorizationDecision {
	return &iamv1.AuthorizationDecision{Allowed: true, DecisionId: "decision", PolicyRevision: "revision", Principal: &iamv1.PrincipalContext{PrincipalId: "subject", PrincipalType: iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD, AuthnMethods: []iamv1.AuthnMethod{iamv1.AuthnMethod_AUTHN_METHOD_API_KEY}, Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: "tenant"}}}}, Obligations: []*iamv1.AuthorizationObligation{{Type: iamv1.AuthorizationObligationType_AUTHORIZATION_OBLIGATION_TYPE_RESOURCE_TENANT_MATCH, Handler: "inference.resource_tenant", ResourceId: "resource", ExpectedTenantId: "tenant"}}}
}
func TestInferenceAuthorizationRequiresExactOwnerObligationAndWorkloadKey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*iamv1.AuthorizationDecision)
		code   codes.Code
	}{
		{"valid", func(*iamv1.AuthorizationDecision) {}, codes.OK},
		{"missing obligation", func(d *iamv1.AuthorizationDecision) { d.Obligations = nil }, codes.PermissionDenied},
		{"wrong owner", func(d *iamv1.AuthorizationDecision) { d.Obligations[0].Handler = "core.resource_tenant" }, codes.PermissionDenied},
		{"wrong resource", func(d *iamv1.AuthorizationDecision) { d.Obligations[0].ResourceId = "other" }, codes.PermissionDenied},
		{"wrong tenant", func(d *iamv1.AuthorizationDecision) { d.Obligations[0].ExpectedTenantId = "other" }, codes.PermissionDenied},
		{"human", func(d *iamv1.AuthorizationDecision) {
			d.Principal.PrincipalType = iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN
		}, codes.PermissionDenied},
		{"denied", func(d *iamv1.AuthorizationDecision) { d.Allowed = false }, codes.PermissionDenied},
		{"revision", func(d *iamv1.AuthorizationDecision) { d.PolicyRevision = "old" }, codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &inferenceDecisionClient{decision: inferenceDecision()}
			tc.change(f.decision)
			c := &Client{authorization: f, cfg: ClientConfig{Registry: registryFixture(t), PolicyRevision: "revision", Timeout: time.Second}}
			r := AuthorizationRequest{Credential: "unit-test-key", TenantID: "tenant", ResourceID: "resource", SourceOperation: InferenceSourceOperation}
			subject, err := c.AuthorizeInferenceAccess(context.Background(), r)
			if status.Code(err) != tc.code {
				t.Fatalf("code=%s", status.Code(err))
			}
			if err == nil && subject.receiverTarget.RPCMethod != InferenceMethod {
				t.Fatal("deferred subject lost target")
			}
			// The general entry point still requires the source owner's immediate check.
			if tc.code == codes.OK {
				if _, err := c.Authorize(context.Background(), r); status.Code(err) != codes.PermissionDenied {
					t.Fatal("generic authorization skipped owner")
				}
			}
		})
	}
}
func TestInferenceReceiverCannotBeRegisteredWithoutOwner(t *testing.T) {
	c := &Client{cfg: ClientConfig{Registry: registryFixture(t)}}
	target := Target{Audience: InferenceAudience, Operation: InferenceOperation, Method: InferenceMethod, Describe: func(proto.Message) (RequestScope, error) { return RequestScope{}, nil }}
	if _, err := c.ReceiverInterceptor([]Target{target}); err == nil {
		t.Fatal("generic receiver admitted deferred owner target")
	}
	if _, err := c.InferenceReceiverInterceptor(target, nil); err == nil {
		t.Fatal("missing owner accepted")
	}
	target.Audience = "wrong"
	if _, err := c.InferenceReceiverInterceptor(target, func(context.Context, proto.Message, Verified) error { return nil }); err == nil {
		t.Fatal("wrong audience accepted")
	}
}
func TestDeferredInferenceSubjectCannotInvokeSession(t *testing.T) {
	c := &Client{cfg: ClientConfig{Registry: registryFixture(t), PolicyRevision: "revision"}}
	target := Target{Audience: "ani-session-gateway", Operation: "session.create", Method: "/ani.session.v1.SessionService/CreateSession", Describe: func(proto.Message) (RequestScope, error) {
		return RequestScope{TenantID: "tenant", SubjectID: "subject", ResourceID: "resource", SourceOperation: InferenceSourceOperation, Mode: "chat_completions"}, nil
	}}
	subject := Subject{principal: inferenceDecision().Principal, credential: "unit-test-key", sourceOperation: InferenceSourceOperation, policyRevision: "revision", resourceID: "resource", receiverTarget: WorkloadTarget{Audience: InferenceAudience, Operation: InferenceOperation, RPCMethod: InferenceMethod}}
	called := false
	err := c.callerInterceptor(map[string]Target{target.Method: target})(WithSubject(context.Background(), subject), target.Method, &structpb.Struct{}, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		called = true
		return nil
	})
	if status.Code(err) != codes.PermissionDenied || called {
		t.Fatal("deferred subject escaped fixed receiver")
	}
}
