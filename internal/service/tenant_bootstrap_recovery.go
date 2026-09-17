package service

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (s *IAMAdminService) RequestRecoveryBootstrap(ctx context.Context, r *iamv1.RequestRecoveryBootstrapRequest) (*iamv1.RequestRecoveryBootstrapResponse, error) {
	const op = "requestRecoveryBootstrap"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "RequestRecoveryBootstrap", op, r.GetTenantId(), "RECOVERY_BOOTSTRAP_REQUEST")
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
	result, err := s.platform.RequestTenantBootstrapRecovery(ctx, cap, biz.RequestTenantAdminRecoveryCommand{TenantID: tenant, TargetPrincipalID: target, ReasonCode: r.GetReason(), PayloadFingerprint: r.GetPayloadFingerprint(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, TenantID: tenant.String(), ResourceID: tenant.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.RequestRecoveryBootstrapResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery)}, nil
}
func (s *IAMAdminService) ApproveRecoveryBootstrap(ctx context.Context, r *iamv1.ApproveRecoveryBootstrapRequest) (*iamv1.ApproveRecoveryBootstrapResponse, error) {
	const op = "approveRecoveryBootstrap"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ApproveRecoveryBootstrap", op, r.GetOperationId(), "RECOVERY_BOOTSTRAP_APPROVE")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.ApproveTenantBootstrapRecovery(ctx, cap, biz.ApproveTenantAdminRecoveryCommand{OperationID: id, ApprovalReference: r.GetApprovalReference(), PayloadFingerprint: r.GetPayloadFingerprint(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		context := errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()}
		if tenant, ok := biz.TenantIDFromAuthenticationError(err); ok {
			context.TenantID = tenant.String()
		}
		return nil, mapIAMError(err, context)
	}
	return &iamv1.ApproveRecoveryBootstrapResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery)}, nil
}
func (s *IAMAdminService) ExecuteRecoveryBootstrap(ctx context.Context, r *iamv1.ExecuteRecoveryBootstrapRequest) (*iamv1.ExecuteRecoveryBootstrapResponse, error) {
	const op = "executeRecoveryBootstrap"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ExecuteRecoveryBootstrap", op, r.GetOperationId(), "RECOVERY_BOOTSTRAP_EXECUTE")
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	result, err := s.platform.ExecuteTenantBootstrapRecovery(ctx, cap, biz.ExecuteTenantAdminRecoveryCommand{OperationID: id, ApprovalReference: r.GetApprovalReference(), PayloadFingerprint: r.GetPayloadFingerprint(), ReauthenticationProof: r.GetReauthenticationProof(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		context := errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()}
		if tenant, ok := biz.TenantIDFromAuthenticationError(err); ok {
			context.TenantID = tenant.String()
		}
		return nil, mapIAMError(err, context)
	}
	return &iamv1.ExecuteRecoveryBootstrapResponse{Operation: tenantAdminRecoveryDTO(*result.Recovery), TenantAccess: tenantAccessDTO(result.Recovery.TenantID, *result.Access)}, nil
}
