package service

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (s *IAMAdminService) UpdatePlatformMembership(ctx context.Context, r *iamv1.UpdatePlatformMembershipRequest) (*iamv1.UpdatePlatformMembershipResponse, error) {
	state, err := membershipStatus(r.GetStatus())
	if err != nil {
		return nil, err
	}
	if state == biz.MembershipStatusRemoved {
		return nil, invalidArgumentStatus("status", "use the member removal operation")
	}
	v, err := s.platformMemberTransition(ctx, r.GetCredential(), r.GetMembershipId(), r.GetExpectedVersion(), r.GetIdempotencyKey(), state, "UpdatePlatformMembership", "updatePlatformIAMMember")
	if err != nil {
		return nil, err
	}
	return &iamv1.UpdatePlatformMembershipResponse{Membership: platformMembershipDTO(v)}, nil
}
func (s *IAMAdminService) RemovePlatformMembership(ctx context.Context, r *iamv1.RemovePlatformMembershipRequest) (*iamv1.RemovePlatformMembershipResponse, error) {
	v, err := s.platformMemberTransition(ctx, r.GetCredential(), r.GetMembershipId(), r.GetExpectedVersion(), r.GetIdempotencyKey(), biz.MembershipStatusRemoved, "RemovePlatformMembership", "removePlatformIAMMember")
	if err != nil {
		return nil, err
	}
	return &iamv1.RemovePlatformMembershipResponse{Membership: platformMembershipDTO(v)}, nil
}
func (s *IAMAdminService) platformMemberTransition(ctx context.Context, credential *iamv1.BearerCredential, member string, version uint64, key string, state biz.MembershipStatus, rpc, op string) (biz.PlatformMembership, error) {
	cap, err := s.platformCapabilityForReason(ctx, credential, rpc, op, member, "PLATFORM_ADMIN_MUTATION")
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	id, err := requiredUUID(member, "membership_id")
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	expected, err := mutationVersion(version)
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	v, err := s.platform.UpdateMembership(ctx, cap, biz.UpdatePlatformMembershipCommand{MembershipID: id, Status: state, ExpectedVersion: expected, IdempotencyKey: key})
	if err != nil {
		return biz.PlatformMembership{}, mapIAMError(err, errorContext{OperationID: op, ResourceID: member})
	}
	return *v.Membership, nil
}
func (s *IAMAdminService) BindPlatformRole(ctx context.Context, r *iamv1.BindPlatformRoleRequest) (*iamv1.BindPlatformRoleResponse, error) {
	v, err := s.platformMemberBinding(ctx, r.GetCredential(), r.GetMembershipId(), r.GetRoleId(), r.GetExpectedMembershipVersion(), r.GetIdempotencyKey(), true)
	if err != nil {
		return nil, err
	}
	return &iamv1.BindPlatformRoleResponse{Membership: platformMembershipDTO(v)}, nil
}
func (s *IAMAdminService) UnbindPlatformRole(ctx context.Context, r *iamv1.UnbindPlatformRoleRequest) (*iamv1.UnbindPlatformRoleResponse, error) {
	v, err := s.platformMemberBinding(ctx, r.GetCredential(), r.GetMembershipId(), r.GetRoleId(), r.GetExpectedMembershipVersion(), r.GetIdempotencyKey(), false)
	if err != nil {
		return nil, err
	}
	return &iamv1.UnbindPlatformRoleResponse{Membership: platformMembershipDTO(v)}, nil
}
func (s *IAMAdminService) platformMemberBinding(ctx context.Context, credential *iamv1.BearerCredential, member, role string, version uint64, key string, bind bool) (biz.PlatformMembership, error) {
	rpc, op := "BindPlatformRole", "bindPlatformIAMRole"
	if !bind {
		rpc, op = "UnbindPlatformRole", "unbindPlatformIAMRole"
	}
	cap, err := s.platformCapabilityForReason(ctx, credential, rpc, op, member, "PLATFORM_ADMIN_MUTATION")
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	id, err := requiredUUID(member, "membership_id")
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	roleID, err := requiredUUID(role, "role_id")
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	expected, err := mutationVersion(version)
	if err != nil {
		return biz.PlatformMembership{}, err
	}
	c := biz.BindPlatformRoleCommand{MembershipID: id, RoleID: roleID, ExpectedMembershipVersion: expected, IdempotencyKey: key}
	var v biz.PlatformMutationResult
	if bind {
		v, err = s.platform.BindRole(ctx, cap, c)
	} else {
		v, err = s.platform.UnbindRole(ctx, cap, c)
	}
	if err != nil {
		return biz.PlatformMembership{}, mapIAMError(err, errorContext{OperationID: op, ResourceID: member})
	}
	return *v.Membership, nil
}
