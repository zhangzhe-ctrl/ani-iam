package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func tenantAdminRecoveryDTO(r biz.TenantAdminRecoveryOperation) *iamv1.RecoveryOperation {
	status := map[biz.RecoveryStatus]iamv1.RecoveryOperationStatus{
		biz.RecoveryPendingApproval: iamv1.RecoveryOperationStatus_RECOVERY_OPERATION_STATUS_PENDING_APPROVAL,
		biz.RecoveryApproved:        iamv1.RecoveryOperationStatus_RECOVERY_OPERATION_STATUS_APPROVED,
		biz.RecoveryExecuted:        iamv1.RecoveryOperationStatus_RECOVERY_OPERATION_STATUS_EXECUTED,
		biz.RecoveryExpired:         iamv1.RecoveryOperationStatus_RECOVERY_OPERATION_STATUS_EXPIRED,
		biz.RecoveryRejected:        iamv1.RecoveryOperationStatus_RECOVERY_OPERATION_STATUS_REJECTED,
	}[r.Status]
	out := &iamv1.RecoveryOperation{OperationId: r.ID.String(), TenantId: r.TenantID.String(), IntendedPrincipalId: r.TargetPrincipalID.String(), RequesterPrincipalId: r.RequesterPrincipalID.String(), Reason: r.ReasonCode, PayloadFingerprint: r.PayloadFingerprint, Status: status, ApprovalExpiresAt: timestamppb.New(r.ExpiresAt), Version: uint64(r.Version), ApprovalReference: r.ApprovalReference}
	if r.ApproverPrincipalID != uuid.Nil {
		out.ApproverPrincipalId = r.ApproverPrincipalID.String()
	}
	return out
}
func (s *IAMAdminService) RequestRestoreTenantAdmin(ctx context.Context, r *iamv1.RequestRestoreTenantAdminRequest) (*iamv1.RequestRestoreTenantAdminResponse, error) {
	const op = "requestRestoreTenantAdmin"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "RequestRestoreTenantAdmin", op, r.GetTenantId(), "RESTORE_TENANT_ADMIN_REQUEST")
	if err != nil {
		return nil, err
	}
	tenant, err := requiredUUID(r.GetTenantId(), "tenant_id")
	if err != nil {
		return nil, err
	}
	target, err := requiredUUID(r.GetIntendedPrincipalId(), "intended_principal_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.RequestTenantAdminRecovery(ctx, cap, biz.RequestTenantAdminRecoveryCommand{TenantID: tenant, TargetPrincipalID: target, ReasonCode: r.GetReason(), PayloadFingerprint: r.GetPayloadFingerprint(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, TenantID: tenant.String(), ResourceID: tenant.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.RequestRestoreTenantAdminResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery)}, nil
}
func (s *IAMAdminService) ApproveRestoreTenantAdmin(ctx context.Context, r *iamv1.ApproveRestoreTenantAdminRequest) (*iamv1.ApproveRestoreTenantAdminResponse, error) {
	const op = "approveRestoreTenantAdmin"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ApproveRestoreTenantAdmin", op, r.GetOperationId(), "RESTORE_TENANT_ADMIN_APPROVE")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.ApproveTenantAdminRecovery(ctx, cap, biz.ApproveTenantAdminRecoveryCommand{OperationID: id, ApprovalReference: r.GetApprovalReference(), PayloadFingerprint: r.GetPayloadFingerprint(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.ApproveRestoreTenantAdminResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery)}, nil
}
func (s *IAMAdminService) ExecuteRestoreTenantAdmin(ctx context.Context, r *iamv1.ExecuteRestoreTenantAdminRequest) (*iamv1.ExecuteRestoreTenantAdminResponse, error) {
	const op = "executeRestoreTenantAdmin"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ExecuteRestoreTenantAdmin", op, r.GetOperationId(), "RESTORE_TENANT_ADMIN_EXECUTE")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.ExecuteTenantAdminRecovery(ctx, cap, biz.ExecuteTenantAdminRecoveryCommand{OperationID: id, ApprovalReference: r.GetApprovalReference(), PayloadFingerprint: r.GetPayloadFingerprint(), ReauthenticationProof: r.GetReauthenticationProof(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.ExecuteRestoreTenantAdminResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery), Membership: membershipRecordDTO(result.Recovery.TenantID, *result.RestoredMembership)}, nil
}
