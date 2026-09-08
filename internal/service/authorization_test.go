package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestCheckPermissionMapsOneFrozenDecision(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	decisionID := uuid.MustParse("0198f062-b76d-7101-9000-000000000007")
	usecase := &recordingAuthorizationUsecase{decision: biz.AuthorizationDecision{
		Allowed:    true,
		Reason:     biz.AuthorizationReasonAllowed,
		DecisionID: decisionID,
		Principal: biz.TrustedPrincipalContext{
			ID:           principalID,
			Status:       biz.PrincipalStatusActive,
			TenantID:     tenantID,
			SessionID:    uuid.MustParse("0198f062-b76d-7101-9000-000000000001"),
			GrantID:      uuid.MustParse("0198f062-b76d-7101-9000-000000000002"),
			AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
		},
		PolicyRevision: testIntegrationPolicyRevision,
		Obligations: []biz.AuthorizationObligation{{
			Type:             biz.AuthorizationObligationResourceTenantMatch,
			Handler:          "core.resource_tenant",
			ResourceID:       "instance-1",
			ExpectedTenantID: tenantID,
		}},
	}}
	service := NewAuthorizationService(usecase)

	response, err := service.CheckPermission(context.Background(), &iamv1.CheckPermissionRequest{
		Credential:     &iamv1.BearerCredential{Value: "signed-access-token"},
		OperationId:    "listInstances",
		PolicyRevision: testIntegrationPolicyRevision,
		Target:         &iamv1.AuthorizationTarget{TenantId: tenantID.String()},
	})
	if err != nil {
		t.Fatalf("CheckPermission() error = %v", err)
	}
	if usecase.command.RawCredential != "signed-access-token" || usecase.command.OperationID != "listInstances" || usecase.command.TargetTenantID != tenantID {
		t.Fatalf("mapped command = %#v", usecase.command)
	}
	decision := response.GetDecision()
	if !decision.GetAllowed() || decision.GetReason() != "ALLOWED" || decision.GetDecisionId() != decisionID.String() || decision.GetPolicyRevision() != testIntegrationPolicyRevision {
		t.Fatalf("mapped decision = %#v", decision)
	}
	if decision.GetPrincipal().GetPrincipalId() != principalID.String() || decision.GetPrincipal().GetBoundary().GetTenant().GetTenantId() != tenantID.String() {
		t.Fatalf("mapped principal = %#v", decision.GetPrincipal())
	}
	if len(decision.GetObligations()) != 1 || decision.GetObligations()[0].GetType() != iamv1.AuthorizationObligationType_AUTHORIZATION_OBLIGATION_TYPE_RESOURCE_TENANT_MATCH || decision.GetObligations()[0].GetHandler() != "core.resource_tenant" || decision.GetObligations()[0].GetResourceId() != "instance-1" || decision.GetObligations()[0].GetExpectedTenantId() != tenantID.String() {
		t.Fatalf("mapped obligations = %#v", decision.GetObligations())
	}
}

func TestCheckPermissionMapsUnregisteredOperationToStableErrorInfo(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	service := NewAuthorizationService(&recordingAuthorizationUsecase{err: biz.ErrAuthorizationOperationUnregistered})
	_, err := service.CheckPermission(context.Background(), &iamv1.CheckPermissionRequest{
		Credential:     &iamv1.BearerCredential{Value: "signed-access-token"},
		OperationId:    "unknownOperation",
		PolicyRevision: testIntegrationPolicyRevision,
		Target:         &iamv1.AuthorizationTarget{TenantId: tenantID.String()},
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.Unavailable || len(grpcStatus.Details()) != 1 {
		t.Fatalf("unregistered-operation status = %s details=%#v", grpcStatus.Code(), grpcStatus.Details())
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != "AUTHZ_OPERATION_UNREGISTERED" || info.GetDomain() != "iam.ani.internal" || info.GetMetadata()["operation_id"] != "unknownOperation" || info.GetMetadata()["policy_revision"] != testIntegrationPolicyRevision {
		t.Fatalf("unregistered-operation ErrorInfo = %#v", info)
	}
}

func TestCheckPermissionMapsDependencyAndTimeoutFailures(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	tests := []struct {
		name       string
		domainErr  error
		wantCode   codes.Code
		wantReason string
		metadata   map[string]string
	}{
		{
			name:       "tenant IAM is not ready",
			domainErr:  biz.ErrTenantIAMNotReady,
			wantCode:   codes.Unavailable,
			wantReason: "TENANT_IAM_NOT_READY",
			metadata:   map[string]string{"tenant_id": tenantID.String()},
		},
		{
			name:       "dependency unavailable",
			domainErr:  errors.Join(biz.ErrAuthorizationDependency, errors.New("postgres unavailable")),
			wantCode:   codes.Unavailable,
			wantReason: "IAM_UNAVAILABLE",
			metadata:   map[string]string{"dependency": "authorization"},
		},
		{
			name:       "tenant lifecycle projection stale",
			domainErr:  errors.Join(biz.ErrAuthorizationDependency, biz.ErrTenantLifecycleStale),
			wantCode:   codes.Unavailable,
			wantReason: "TENANT_LIFECYCLE_STALE",
			metadata:   map[string]string{"tenant_id": tenantID.String()},
		},
		{
			name:       "deadline exceeded",
			domainErr:  context.DeadlineExceeded,
			wantCode:   codes.DeadlineExceeded,
			wantReason: "IAM_TIMEOUT",
			metadata:   map[string]string{"operation_id": "listInstances"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthorizationService(&recordingAuthorizationUsecase{err: test.domainErr})
			_, err := service.CheckPermission(context.Background(), &iamv1.CheckPermissionRequest{
				Credential:     &iamv1.BearerCredential{Value: "signed-access-token"},
				OperationId:    "listInstances",
				PolicyRevision: testIntegrationPolicyRevision,
				Target:         &iamv1.AuthorizationTarget{TenantId: tenantID.String()},
			})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode || len(grpcStatus.Details()) != 1 {
				t.Fatalf("status = %s details=%#v", grpcStatus.Code(), grpcStatus.Details())
			}
			info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
			if !ok || info.GetReason() != test.wantReason || info.GetDomain() != "iam.ani.internal" {
				t.Fatalf("ErrorInfo = %#v", info)
			}
			for key, value := range test.metadata {
				if info.GetMetadata()[key] != value {
					t.Fatalf("metadata[%q] = %q, want %q", key, info.GetMetadata()[key], value)
				}
			}
		})
	}
}

func TestCheckPermissionMapsPolicyMismatchToStableErrorInfo(t *testing.T) {
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	usecase := biz.NewAuthorizationUsecase(
		servicePolicyRegistry{},
		unusedServiceCredentialVerifier{},
		unusedServiceAuthorizationReader{},
		unusedServiceIDGenerator{},
		serviceClock{},
	)
	service := NewAuthorizationService(usecase)

	_, err := service.CheckPermission(context.Background(), &iamv1.CheckPermissionRequest{
		Credential:     &iamv1.BearerCredential{Value: "signed-access-token"},
		OperationId:    "listInstances",
		PolicyRevision: "sha256:wrong",
		Target:         &iamv1.AuthorizationTarget{TenantId: tenantID.String()},
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.Unavailable || len(grpcStatus.Details()) != 1 {
		t.Fatalf("status = %v details=%#v", grpcStatus.Code(), grpcStatus.Details())
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != "AUTHZ_POLICY_MISMATCH" || info.GetDomain() != "iam.ani.internal" {
		t.Fatalf("ErrorInfo = %#v", info)
	}
	if info.GetMetadata()["expected_policy_revision"] != testIntegrationPolicyRevision || info.GetMetadata()["actual_policy_revision"] != "sha256:wrong" {
		t.Fatalf("policy metadata = %#v", info.GetMetadata())
	}
}

func TestCheckPermissionValidationUsesFrozenErrorInfo(t *testing.T) {
	tests := []struct {
		name      string
		request   *iamv1.CheckPermissionRequest
		wantField string
	}{
		{name: "missing request", wantField: "target"},
		{name: "missing target", request: &iamv1.CheckPermissionRequest{}, wantField: "target"},
		{
			name:      "invalid target tenant",
			request:   &iamv1.CheckPermissionRequest{Target: &iamv1.AuthorizationTarget{TenantId: "not-a-uuid"}},
			wantField: "target.tenant_id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewAuthorizationService(&recordingAuthorizationUsecase{})
			_, err := service.CheckPermission(context.Background(), test.request)
			assertFrozenErrorInfo(t, err, codes.InvalidArgument, "INVALID_ARGUMENT", map[string]string{"field": test.wantField})
		})
	}
}

const testIntegrationPolicyRevision = "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa"

type recordingAuthorizationUsecase struct {
	decision biz.AuthorizationDecision
	err      error
	command  biz.CheckPermissionCommand
}

func (u *recordingAuthorizationUsecase) CheckPermission(_ context.Context, command biz.CheckPermissionCommand) (biz.AuthorizationDecision, error) {
	u.command = command
	return u.decision, u.err
}

type servicePolicyRegistry struct{}

func (servicePolicyRegistry) Revision() string { return testIntegrationPolicyRevision }
func (servicePolicyRegistry) Lookup(string) (biz.AuthorizationPolicy, bool) {
	panic("policy lookup must not run on revision mismatch")
}

type unusedServiceCredentialVerifier struct{}

func (unusedServiceCredentialVerifier) Verify(context.Context, string) (biz.AccessTokenClaims, error) {
	panic("credential verifier must not run on revision mismatch")
}

type unusedServiceAuthorizationReader struct{}

func (unusedServiceAuthorizationReader) LookupAuthorization(context.Context, biz.TenantScope, biz.AuthorizationLookup) (biz.AuthorizationState, error) {
	panic("authorization reader must not run on revision mismatch")
}

type unusedServiceIDGenerator struct{}

func (unusedServiceIDGenerator) NewID() (uuid.UUID, error) {
	panic("ID generator must not run on revision mismatch")
}

type serviceClock struct{}

func (serviceClock) Now() time.Time { return time.Time{} }
