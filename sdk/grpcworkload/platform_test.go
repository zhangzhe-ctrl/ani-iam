package grpcworkload

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type platformDecisionClient struct {
	iamv1.AuthorizationServiceClient
	decision *iamv1.AuthorizationDecision
	request  *iamv1.CheckPermissionRequest
	calls    int
}

func (f *platformDecisionClient) CheckPermission(_ context.Context, r *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	f.calls++
	f.request = r
	if r.GetTarget() == nil {
		return nil, status.Error(codes.InvalidArgument, "authorization target is required")
	}
	return &iamv1.CheckPermissionResponse{Decision: f.decision}, nil
}

func TestPlatformHumanAuthorizationHasNoTenantOrDelegatedResource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*iamv1.AuthorizationDecision)
		allowed bool
	}{
		{"current Human", func(*iamv1.AuthorizationDecision) {}, true},
		{"denied", func(d *iamv1.AuthorizationDecision) { d.Allowed = false }, false},
		{"wrong revision", func(d *iamv1.AuthorizationDecision) { d.PolicyRevision = "other" }, false},
		{"Tenant boundary", func(d *iamv1.AuthorizationDecision) {
			d.Principal.Boundary = &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: uuid.NewString()}}}
		}, false},
		{"missing boundary", func(d *iamv1.AuthorizationDecision) { d.Principal.Boundary = nil }, false},
		{"Workload", func(d *iamv1.AuthorizationDecision) {
			d.Principal.PrincipalType = iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD
		}, false},
		{"disabled", func(d *iamv1.AuthorizationDecision) {
			d.Principal.PrincipalStatus = iamv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED
		}, false},
		{"missing Session", func(d *iamv1.AuthorizationDecision) { d.Principal.SessionId = "" }, false},
		{"missing Grant", func(d *iamv1.AuthorizationDecision) { d.Principal.GrantId = "" }, false},
		{"missing decision", func(d *iamv1.AuthorizationDecision) { d.DecisionId = "" }, false},
		{"unexpected obligation", func(d *iamv1.AuthorizationDecision) {
			d.Obligations = []*iamv1.AuthorizationObligation{{Handler: "core.resource_tenant"}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &iamv1.AuthorizationDecision{Allowed: true, DecisionId: uuid.NewString(), PolicyRevision: "revision", Principal: &iamv1.PrincipalContext{PrincipalId: uuid.NewString(), PrincipalType: iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN, PrincipalStatus: iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE, SessionId: uuid.NewString(), GrantId: uuid.NewString(), Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Platform{Platform: &iamv1.PlatformBoundary{}}}}}
			tc.change(d)
			f := &platformDecisionClient{decision: d}
			c := &Client{authorization: f, cfg: ClientConfig{PolicyRevision: "revision", Timeout: time.Second}}
			s, err := c.AuthorizePlatformHuman(context.Background(), AuthorizationRequest{Credential: "unit-credential", SourceOperation: "createTenant"})
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v code=%s", tc.allowed, status.Code(err))
			}
			if f.calls != 1 || f.request.GetTarget() == nil || f.request.GetTarget().GetTenantId() != "" || f.request.GetTarget().GetResourceId() != "" || f.request.GetOperationId() != "createTenant" || f.request.GetPolicyRevision() != "revision" {
				t.Fatal("Platform authorization changed scope or decision count")
			}
			if tc.allowed && (s.Principal().GetBoundary().GetTenant() != nil || s.DecisionID() != d.DecisionId) {
				t.Fatal("Platform decision shape changed")
			}
		})
	}
	for _, r := range []AuthorizationRequest{{Credential: "c", SourceOperation: "createTenant", TenantID: uuid.NewString()}, {Credential: "c", SourceOperation: "createTenant", ResourceID: uuid.NewString()}, {Credential: "c", SourceOperation: "createTenant", CheckResource: func(context.Context, ResourceBoundary) error { return nil }}} {
		f := &platformDecisionClient{}
		c := &Client{authorization: f, cfg: ClientConfig{Timeout: time.Second}}
		if _, e := c.AuthorizePlatformHuman(context.Background(), r); status.Code(e) != codes.InvalidArgument || f.calls != 0 {
			t.Fatal("Tenant/resource intent sent through Platform entry")
		}
	}
}
