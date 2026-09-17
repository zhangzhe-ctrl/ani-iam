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

type recordingRoleMutations struct {
	tenantRoleMutations
	command biz.CreateTenantRoleCommand
	calls   int
}

func (f *recordingRoleMutations) Create(_ context.Context, _ biz.TenantScope, c biz.CreateTenantRoleCommand) (biz.TenantRoleMutationResult, error) {
	f.command = c
	f.calls++
	return biz.TenantRoleMutationResult{Role: biz.TenantRole{ID: uuid.Must(uuid.NewV7()), Code: "custom", DisplayName: c.DisplayName, Permissions: c.Permissions, Version: 1}}, nil
}

func TestTenantRoleAdapterUsesAuthenticatedActorAndCanonicalPermissionInput(t *testing.T) {
	tenant := uuid.Must(uuid.NewV7())
	authority := newAdminTestAuthority(tenant)
	mutations := &recordingRoleMutations{}
	s := NewIAMAdministrationService(&recordingTenantAuthorizationReader{}, nil, mutations, nil, nil, NewAdminAuthorization(authority, authority, "revision"))
	r := &iamv1.CreateTenantRoleRequest{Credential: adminTestCredential(), TenantId: tenant.String(), DisplayName: "Reader", Permissions: []string{"instances/read"}, IdempotencyKey: "one"}
	if _, err := s.CreateTenantRole(context.Background(), r); status.Code(err) != codes.PermissionDenied || mutations.calls != 0 {
		t.Fatal("missing direct caller was accepted")
	}
	result, err := s.CreateTenantRole(adminTestCaller("CreateTenantRole"), r)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetRole().GetDisplayName() != "Reader" || mutations.command.Actor.PrincipalID != authority.principal.ID || mutations.command.Permissions[0].Scope != biz.PermissionScopeTenant {
		t.Fatal("actor or DTO conversion differs")
	}
	r.TenantId = uuid.NewString()
	if _, err := s.CreateTenantRole(adminTestCaller("CreateTenantRole"), r); status.Code(err) != codes.PermissionDenied || mutations.calls != 1 {
		t.Fatal("cross-tenant request reached the usecase")
	}
	r.TenantId = tenant.String()
	r.Permissions = []string{"tenant/instances/read"}
	if _, err := s.CreateTenantRole(adminTestCaller("CreateTenantRole"), r); status.Code(err) != codes.InvalidArgument || mutations.calls != 1 {
		t.Fatal("invalid permission syntax reached the usecase")
	}
}
