package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type adminTestAuthority struct {
	principal biz.TrustedPrincipalContext
	decision  uuid.UUID
	operation string
	allowed   bool
	err       error
}

func newAdminTestAuthority(tenant uuid.UUID) *adminTestAuthority {
	return &adminTestAuthority{allowed: true, decision: uuid.MustParse("01993000-0000-7000-8000-000000000093"), principal: biz.TrustedPrincipalContext{
		ID: uuid.MustParse("01993000-0000-7000-8000-000000000094"), Type: biz.PrincipalTypeHuman, Status: biz.PrincipalStatusActive,
		TenantID: tenant, AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodPassword},
	}}
}
func (a *adminTestAuthority) ValidatePrincipal(_ context.Context, command biz.ValidatePrincipalCommand) (biz.ValidatePrincipalResult, error) {
	if command.RawCredential != "test-credential" {
		return biz.ValidatePrincipalResult{}, biz.ErrInvalidCredential
	}
	if a.err != nil {
		return biz.ValidatePrincipalResult{}, a.err
	}
	return biz.ValidatePrincipalResult{Principal: a.principal, DecisionID: a.decision, PolicyRevision: "revision"}, nil
}
func (a *adminTestAuthority) CheckPermission(_ context.Context, command biz.CheckPermissionCommand) (biz.AuthorizationDecision, error) {
	a.operation = command.OperationID
	if a.err != nil {
		return biz.AuthorizationDecision{}, a.err
	}
	return biz.AuthorizationDecision{Principal: a.principal, DecisionID: a.decision, Allowed: a.allowed, PolicyRevision: "revision"}, nil
}
func adminTestCredential() *iamv1.BearerCredential {
	return &iamv1.BearerCredential{Value: "test-credential"}
}
func adminTestCaller(rpc string) context.Context {
	return biz.WithDirectCaller(context.Background(), biz.DirectCaller{Identity: biz.WorkloadIdentity{
		PrincipalID: uuid.MustParse("01993000-0000-7000-8000-000000000001"), BindingID: uuid.MustParse("01993000-0000-7000-8000-000000000002"), PrincipalVersion: 1, BindingVersion: 1,
	}, Target: biz.WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + rpc}, GrantVersion: 1})
}

func TestAdminAuthorizationRechecksCurrentPermissionAndRejectsOtherCallerTarget(t *testing.T) {
	tenant := uuid.MustParse("01993000-0000-7000-8000-000000000098")
	authority := newAdminTestAuthority(tenant)
	reader := &recordingTenantAuthorizationReader{tenantWorkload: biz.TenantWorkload{ID: uuid.MustParse("01993000-0000-7000-8000-000000000099")}}
	admin := NewTenantIAMAdminService(reader, &recordingTenantAuthorizationMutations{}, NewAdminAuthorization(authority, authority, "revision"))
	request := &iamv1.GetTenantWorkloadRequest{Credential: adminTestCredential(), PrincipalId: reader.tenantWorkload.ID.String()}
	if _, err := admin.GetTenantWorkload(adminTestCaller("GetTenantWorkload"), request); err != nil {
		t.Fatal(err)
	}
	authority.allowed = false
	if _, err := admin.GetTenantWorkload(adminTestCaller("GetTenantWorkload"), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked permission: %v", err)
	}
	authority.allowed = true
	if _, err := admin.GetTenantWorkload(adminTestCaller("CreateAPIKey"), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong direct target: %v", err)
	}
	authority.principal.Type = biz.PrincipalTypeWorkload
	if _, err := admin.GetTenantWorkload(adminTestCaller("GetTenantWorkload"), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-Human admin: %v", err)
	}
	if reader.tenantWorkloadCalls != 1 {
		t.Fatal("rejected call reached the business reader")
	}
}
