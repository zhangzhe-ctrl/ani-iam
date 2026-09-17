package service

import (
	"context"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
)

func tenantBootstrapDTO(v biz.TenantBootstrapOperation) *iamv1.TenantBootstrapOperation {
	r := &iamv1.TenantBootstrapOperation{OperationId: v.ID.String(), TenantId: v.TenantID.String(), Status: v.Status, Version: uint64(v.Version)}
	for _, job := range v.Jobs {
		j := &iamv1.TenantBootstrapJob{Kind: string(job.Kind), Generation: uint64(job.Generation), State: job.State, AttemptCount: uint32(job.AttemptCount), CycleStartAttempt: uint32(job.CycleStartAttempt), LastError: job.LastError, AvailableAt: timestamppb.New(job.AvailableAt)}
		if job.RecoveryID != uuid.Nil {
			j.RecoveryId = job.RecoveryID.String()
		}
		r.Jobs = append(r.Jobs, j)
	}
	if v.Invitation != nil {
		r.Invitation = tenantInvitationDTO(v.TenantID, *v.Invitation)
	}
	return r
}
func (s *IAMAdminService) GetTenantBootstrap(ctx context.Context, r *iamv1.GetTenantBootstrapRequest) (*iamv1.GetTenantBootstrapResponse, error) {
	const op = "getTenantIAMBootstrap"
	cap, err := s.platformCapability(ctx, r.GetCredential(), "GetTenantBootstrap", op, r.GetOperationId())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	v, err := s.platform.GetTenantBootstrap(ctx, cap, id)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String()})
	}
	return &iamv1.GetTenantBootstrapResponse{Operation: tenantBootstrapDTO(v)}, nil
}
func (s *IAMAdminService) ReissueTenantBootstrapInvitation(ctx context.Context, r *iamv1.ReissueTenantBootstrapInvitationRequest) (*iamv1.ReissueTenantBootstrapInvitationResponse, error) {
	const op = "reissueTenantIAMBootstrapInvitation"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "ReissueTenantBootstrapInvitation", op, r.GetOperationId(), r.GetReasonCode())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	v, err := s.platform.ReissueTenantBootstrapInvitation(ctx, cap, biz.ReissueTenantBootstrapCommand{OperationID: id, ExpectedVersion: version, ReasonCode: r.GetReasonCode(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.ReissueTenantBootstrapInvitationResponse{Operation: tenantBootstrapDTO(*v.Bootstrap)}, nil
}

func (s *IAMAdminService) RetryTenantBootstrapJob(ctx context.Context, r *iamv1.RetryTenantBootstrapJobRequest) (*iamv1.RetryTenantBootstrapJobResponse, error) {
	const op = "retryTenantIAMBootstrapJob"
	cap, err := s.platformCapabilityForReason(ctx, r.GetCredential(), "RetryTenantBootstrapJob", op, r.GetOperationId(), r.GetReasonCode())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(r.GetOperationId(), "operation_id")
	if err != nil {
		return nil, err
	}
	version, err := mutationVersion(r.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if r.GetGeneration() < 1 || r.GetGeneration() > math.MaxInt64 {
		return nil, invalidArgumentStatus("generation", "IAM request is invalid")
	}
	if r.GetExpectedAttempt() < 1 || r.GetExpectedAttempt() > math.MaxInt32 {
		return nil, invalidArgumentStatus("expected_attempt", "IAM request is invalid")
	}
	v, err := s.platform.RetryTenantBootstrapJob(ctx, cap, biz.RetryTenantBootstrapJobCommand{OperationID: id, Kind: biz.CoreBootstrapJobKind(r.GetKind()), Generation: int64(r.GetGeneration()), ExpectedAttempt: int32(r.GetExpectedAttempt()), ExpectedVersion: version, ReasonCode: r.GetReasonCode(), IdempotencyKey: r.GetIdempotencyKey()})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: op, ResourceID: id.String(), IdempotencyKey: r.GetIdempotencyKey()})
	}
	return &iamv1.RetryTenantBootstrapJobResponse{Operation: tenantBootstrapDTO(*v.Bootstrap)}, nil
}
