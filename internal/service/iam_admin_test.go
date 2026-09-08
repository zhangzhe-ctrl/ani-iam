package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestIAMAdminServiceIsRegisteredButOutsideSliceMethodsStayUnimplemented(t *testing.T) {
	service := NewIAMAdminService()
	_, err := service.GetTenantAccess(context.Background(), &iamv1.GetTenantAccessRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetTenantAccess() code = %s, want Unimplemented", status.Code(err))
	}
}

func TestTenantIAMAdminMapsAccessAndMembershipMutation(t *testing.T) {
	tenantID := uuid.MustParse("0199c85c-3000-7001-9000-000000000001")
	membershipID := uuid.MustParse("0199c85c-3000-7001-9000-000000000002")
	reader := &recordingTenantAuthorizationReader{access: biz.TenantAccess{Status: biz.TenantAccessStatusActive, Version: 2}}
	mutations := &recordingTenantAuthorizationMutations{membershipResult: biz.TenantMembershipMutationResult{Membership: biz.TenantMembership{
		ID: membershipID, PrincipalID: uuid.MustParse("0199c85c-3000-7001-9000-000000000003"), Status: biz.MembershipStatusSuspended, Version: 4,
	}, PrincipalType: biz.PrincipalTypeHuman, RoleIDs: []uuid.UUID{uuid.MustParse("0199c85c-3000-7001-9000-000000000005")}}}
	service := NewTenantIAMAdminService(reader, mutations)

	access, err := service.GetTenantAccess(context.Background(), &iamv1.GetTenantAccessRequest{TenantId: tenantID.String()})
	if err != nil {
		t.Fatalf("GetTenantAccess() error = %v", err)
	}
	if access.GetTenantAccess().GetTenantId() != tenantID.String() || access.GetTenantAccess().GetStatus() != iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_ACTIVE || access.GetTenantAccess().GetVersion() != 2 {
		t.Fatalf("access = %#v", access.GetTenantAccess())
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-ani-principal-id", "0199c85c-3000-7001-9000-000000000004",
		"x-ani-principal-type", "human",
		"x-ani-authn-method", "password", "x-ani-decision-id", "decision-1",
		"x-request-id", "request-1", "x-correlation-id", "correlation-1",
	))
	response, err := service.UpdateTenantMembership(ctx, &iamv1.UpdateTenantMembershipRequest{
		TenantId: tenantID.String(), MembershipId: membershipID.String(),
		Status:          iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED,
		ExpectedVersion: 3, IdempotencyKey: "membership-update-1",
	})
	if err != nil {
		t.Fatalf("UpdateTenantMembership() error = %v", err)
	}
	if mutations.updateMembership.Status != biz.MembershipStatusSuspended || mutations.updateMembership.ExpectedVersion != 3 || mutations.updateMembership.Actor.DecisionID != "decision-1" {
		t.Fatalf("mapped command = %#v", mutations.updateMembership)
	}
	if response.GetMembership().GetBoundary().GetTenant().GetTenantId() != tenantID.String() || response.GetMembership().GetStatus() != iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED || response.GetMembership().GetVersion() != 4 {
		t.Fatalf("response membership = %#v", response.GetMembership())
	}
	if response.GetMembership().GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN || len(response.GetMembership().GetRoleIds()) != 1 {
		t.Fatalf("response membership authority = %#v", response.GetMembership())
	}
}

func TestTenantIAMAdminRejectsNonHumanTrustedActor(t *testing.T) {
	mutations := &recordingTenantAuthorizationMutations{}
	service := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{}, mutations)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-ani-principal-id", "0199c85c-3000-7001-9000-000000000014",
		"x-ani-principal-type", "service",
		"x-ani-authn-method", "service_token", "x-ani-decision-id", "decision-2",
		"x-request-id", "request-2", "x-correlation-id", "correlation-2",
	))
	_, err := service.UpdateTenantAccess(ctx, &iamv1.UpdateTenantAccessRequest{
		TenantId:        "0199c85c-3000-7001-9000-000000000011",
		Status:          iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_SUSPENDED,
		ExpectedVersion: 1, IdempotencyKey: "access-update-2",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("UpdateTenantAccess() code = %s, want Unavailable", status.Code(err))
	}
	if mutations.updateAccessCalls != 0 {
		t.Fatalf("UpdateAccess() calls = %d, want 0", mutations.updateAccessCalls)
	}
}

func TestTenantIAMAdminRejectsMissingTrustedActorForMutation(t *testing.T) {
	service := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{}, &recordingTenantAuthorizationMutations{})
	_, err := service.UpdateTenantAccess(context.Background(), &iamv1.UpdateTenantAccessRequest{
		TenantId:        "0199c85c-3000-7001-9000-000000000011",
		Status:          iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_SUSPENDED,
		ExpectedVersion: 1, IdempotencyKey: "access-update-1",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("UpdateTenantAccess() code = %s, want Unavailable", status.Code(err))
	}
}

type recordingTenantAuthorizationReader struct{ access biz.TenantAccess }

func (r *recordingTenantAuthorizationReader) GetAccess(context.Context, biz.TenantScope) (biz.TenantAccess, error) {
	return r.access, nil
}
func (*recordingTenantAuthorizationReader) GetMembership(context.Context, biz.TenantScope, uuid.UUID) (biz.TenantMembershipRecord, error) {
	return biz.TenantMembershipRecord{}, nil
}
func (*recordingTenantAuthorizationReader) ListMemberships(context.Context, biz.TenantScope, biz.MembershipStatus, uuid.UUID, int32) (biz.TenantMembershipPage, error) {
	return biz.TenantMembershipPage{}, nil
}
func (*recordingTenantAuthorizationReader) GetRole(context.Context, biz.TenantScope, uuid.UUID) (biz.TenantRole, error) {
	return biz.TenantRole{}, nil
}
func (*recordingTenantAuthorizationReader) ListRoles(context.Context, biz.TenantScope, uuid.UUID, int32) (biz.TenantRolePage, error) {
	return biz.TenantRolePage{}, nil
}

type recordingTenantAuthorizationMutations struct {
	updateMembership biz.UpdateTenantMembershipCommand
	membershipResult biz.TenantMembershipMutationResult
	updateAccessCalls int
}

func (m *recordingTenantAuthorizationMutations) UpdateAccess(context.Context, biz.TenantScope, biz.UpdateTenantAccessCommand) (biz.TenantAccessMutationResult, error) {
	m.updateAccessCalls++
	return biz.TenantAccessMutationResult{}, nil
}
func (m *recordingTenantAuthorizationMutations) UpdateMembership(_ context.Context, _ biz.TenantScope, command biz.UpdateTenantMembershipCommand) (biz.TenantMembershipMutationResult, error) {
	m.updateMembership = command
	return m.membershipResult, nil
}
func (*recordingTenantAuthorizationMutations) BindRole(context.Context, biz.TenantScope, biz.BindTenantRoleCommand) (biz.TenantMembershipMutationResult, error) {
	return biz.TenantMembershipMutationResult{}, nil
}
func (*recordingTenantAuthorizationMutations) UnbindRole(context.Context, biz.TenantScope, biz.UnbindTenantRoleCommand) (biz.TenantMembershipMutationResult, error) {
	return biz.TenantMembershipMutationResult{}, nil
}
