package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const (
	defaultTenantAdminPageSize int32 = 50
	maximumTenantAdminPageSize int32 = 100
)

type tenantAuthorizationMutations interface {
	UpdateAccess(context.Context, biz.TenantScope, biz.UpdateTenantAccessCommand) (biz.TenantAccessMutationResult, error)
	UpdateMembership(context.Context, biz.TenantScope, biz.UpdateTenantMembershipCommand) (biz.TenantMembershipMutationResult, error)
	BindRole(context.Context, biz.TenantScope, biz.BindTenantRoleCommand) (biz.TenantMembershipMutationResult, error)
	UnbindRole(context.Context, biz.TenantScope, biz.UnbindTenantRoleCommand) (biz.TenantMembershipMutationResult, error)
}

// IAMAdminService implements the DP2-09 tenant-administration slice. A service
// built by NewIAMAdminService retains the frozen Unimplemented behavior until
// the composition-root wiring is explicitly authorized.
type IAMAdminService struct {
	iamv1.UnimplementedIAMAdminServiceServer
	reader    biz.TenantAuthorizationReader
	mutations tenantAuthorizationMutations
}

func NewIAMAdminService() *IAMAdminService {
	return &IAMAdminService{}
}

func NewTenantIAMAdminService(reader biz.TenantAuthorizationReader, mutations tenantAuthorizationMutations) *IAMAdminService {
	return &IAMAdminService{reader: reader, mutations: mutations}
}

func (s *IAMAdminService) GetTenantAccess(ctx context.Context, request *iamv1.GetTenantAccessRequest) (*iamv1.GetTenantAccessResponse, error) {
	if s.reader == nil {
		return nil, status.Error(codes.Unimplemented, "method GetTenantAccess not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	access, err := s.reader.GetAccess(ctx, scope)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getTenantIAMAccess", TenantID: tenantID.String()})
	}
	return &iamv1.GetTenantAccessResponse{TenantAccess: tenantAccessDTO(tenantID, access)}, nil
}

func (s *IAMAdminService) UpdateTenantAccess(ctx context.Context, request *iamv1.UpdateTenantAccessRequest) (*iamv1.UpdateTenantAccessResponse, error) {
	if s.mutations == nil {
		return nil, status.Error(codes.Unimplemented, "method UpdateTenantAccess not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	statusValue, err := tenantAccessStatus(request.GetStatus())
	if err != nil {
		return nil, invalidArgumentStatus("status", "IAM request is invalid")
	}
	expectedVersion, err := mutationVersion(request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.GetIdempotencyKey()) == "" {
		return nil, invalidArgumentStatus("idempotency_key", "IAM request is invalid")
	}
	actor, err := tenantAuthorizationActor(ctx)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "updateTenantIAMAccess", TenantID: tenantID.String()})
	}
	result, err := s.mutations.UpdateAccess(ctx, scope, biz.UpdateTenantAccessCommand{Status: statusValue, ExpectedVersion: expectedVersion, Actor: actor})
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "updateTenantIAMAccess", TenantID: tenantID.String(), IdempotencyKey: strings.TrimSpace(request.GetIdempotencyKey()), DecisionID: actor.DecisionID})
	}
	return &iamv1.UpdateTenantAccessResponse{TenantAccess: tenantAccessDTO(tenantID, result.Access)}, nil
}

func (s *IAMAdminService) GetTenantMembership(ctx context.Context, request *iamv1.GetTenantMembershipRequest) (*iamv1.GetTenantMembershipResponse, error) {
	if s.reader == nil {
		return nil, status.Error(codes.Unimplemented, "method GetTenantMembership not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	membershipID, err := requiredUUID(request.GetMembershipId(), "membership_id")
	if err != nil {
		return nil, err
	}
	record, err := s.reader.GetMembership(ctx, scope, membershipID)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getTenantIAMMembership", TenantID: tenantID.String()})
	}
	return &iamv1.GetTenantMembershipResponse{Membership: membershipRecordDTO(tenantID, record)}, nil
}

func (s *IAMAdminService) ListTenantMemberships(ctx context.Context, request *iamv1.ListTenantMembershipsRequest) (*iamv1.ListTenantMembershipsResponse, error) {
	if s.reader == nil {
		return nil, status.Error(codes.Unimplemented, "method ListTenantMemberships not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	filter, err := optionalMembershipStatus(request.GetStatus())
	if err != nil {
		return nil, invalidArgumentStatus("status", "IAM request is invalid")
	}
	cursor, pageSize, err := tenantAdminPage(request.GetPage())
	if err != nil {
		return nil, err
	}
	page, err := s.reader.ListMemberships(ctx, scope, filter, cursor, pageSize)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listTenantIAMMemberships", TenantID: tenantID.String()})
	}
	items := make([]*iamv1.Membership, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, membershipRecordDTO(tenantID, record))
	}
	return &iamv1.ListTenantMembershipsResponse{Memberships: items, NextCursor: cursorString(page.NextCursor)}, nil
}

func (s *IAMAdminService) UpdateTenantMembership(ctx context.Context, request *iamv1.UpdateTenantMembershipRequest) (*iamv1.UpdateTenantMembershipResponse, error) {
	result, tenantID, err := s.updateMembership(ctx, request.GetTenantId(), request.GetMembershipId(), request.GetStatus(), request.GetExpectedVersion(), request.GetIdempotencyKey(), "updateTenantIAMMembership")
	if err != nil {
		return nil, err
	}
	return &iamv1.UpdateTenantMembershipResponse{Membership: membershipMutationDTO(tenantID, result)}, nil
}

func (s *IAMAdminService) RemoveTenantMembership(ctx context.Context, request *iamv1.RemoveTenantMembershipRequest) (*iamv1.RemoveTenantMembershipResponse, error) {
	result, tenantID, err := s.updateMembership(ctx, request.GetTenantId(), request.GetMembershipId(), iamv1.MembershipStatus_MEMBERSHIP_STATUS_REMOVED, request.GetExpectedVersion(), request.GetIdempotencyKey(), "removeTenantIAMMembership")
	if err != nil {
		return nil, err
	}
	return &iamv1.RemoveTenantMembershipResponse{Membership: membershipMutationDTO(tenantID, result)}, nil
}

func (s *IAMAdminService) updateMembership(ctx context.Context, rawTenantID, rawMembershipID string, rawStatus iamv1.MembershipStatus, rawVersion uint64, idempotencyKey, operationID string) (biz.TenantMembershipMutationResult, uuid.UUID, error) {
	if s.mutations == nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, status.Error(codes.Unimplemented, "tenant membership mutation not implemented")
	}
	scope, tenantID, err := tenantScope(rawTenantID)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	membershipID, err := requiredUUID(rawMembershipID, "membership_id")
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	statusValue, err := membershipStatus(rawStatus)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, invalidArgumentStatus("status", "IAM request is invalid")
	}
	expectedVersion, err := mutationVersion(rawVersion)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, invalidArgumentStatus("idempotency_key", "IAM request is invalid")
	}
	actor, err := tenantAuthorizationActor(ctx)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, mapIAMError(err, errorContext{OperationID: operationID, TenantID: tenantID.String()})
	}
	result, err := s.mutations.UpdateMembership(ctx, scope, biz.UpdateTenantMembershipCommand{MembershipID: membershipID, Status: statusValue, ExpectedVersion: expectedVersion, Actor: actor})
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, mapIAMError(err, errorContext{OperationID: operationID, TenantID: tenantID.String(), IdempotencyKey: strings.TrimSpace(idempotencyKey), DecisionID: actor.DecisionID})
	}
	return result, tenantID, nil
}

func (s *IAMAdminService) GetTenantRole(ctx context.Context, request *iamv1.GetTenantRoleRequest) (*iamv1.GetTenantRoleResponse, error) {
	if s.reader == nil {
		return nil, status.Error(codes.Unimplemented, "method GetTenantRole not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	roleID, err := requiredUUID(request.GetRoleId(), "role_id")
	if err != nil {
		return nil, err
	}
	role, err := s.reader.GetRole(ctx, scope, roleID)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "getTenantIAMRole", TenantID: tenantID.String()})
	}
	return &iamv1.GetTenantRoleResponse{Role: roleDTO(tenantID, role)}, nil
}

func (s *IAMAdminService) ListTenantRoles(ctx context.Context, request *iamv1.ListTenantRolesRequest) (*iamv1.ListTenantRolesResponse, error) {
	if s.reader == nil {
		return nil, status.Error(codes.Unimplemented, "method ListTenantRoles not implemented")
	}
	scope, tenantID, err := tenantScope(request.GetTenantId())
	if err != nil {
		return nil, err
	}
	cursor, pageSize, err := tenantAdminPage(request.GetPage())
	if err != nil {
		return nil, err
	}
	page, err := s.reader.ListRoles(ctx, scope, cursor, pageSize)
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listTenantIAMRoles", TenantID: tenantID.String()})
	}
	items := make([]*iamv1.Role, 0, len(page.Items))
	for _, role := range page.Items {
		items = append(items, roleDTO(tenantID, role))
	}
	return &iamv1.ListTenantRolesResponse{Roles: items, NextCursor: cursorString(page.NextCursor)}, nil
}

func (s *IAMAdminService) BindTenantRole(ctx context.Context, request *iamv1.BindTenantRoleRequest) (*iamv1.BindTenantRoleResponse, error) {
	result, tenantID, err := s.mutateRoleBinding(ctx, request.GetTenantId(), request.GetMembershipId(), request.GetRoleId(), request.GetExpectedMembershipVersion(), request.GetIdempotencyKey(), true)
	if err != nil {
		return nil, err
	}
	return &iamv1.BindTenantRoleResponse{Membership: membershipMutationDTO(tenantID, result)}, nil
}

func (s *IAMAdminService) UnbindTenantRole(ctx context.Context, request *iamv1.UnbindTenantRoleRequest) (*iamv1.UnbindTenantRoleResponse, error) {
	result, tenantID, err := s.mutateRoleBinding(ctx, request.GetTenantId(), request.GetMembershipId(), request.GetRoleId(), request.GetExpectedMembershipVersion(), request.GetIdempotencyKey(), false)
	if err != nil {
		return nil, err
	}
	return &iamv1.UnbindTenantRoleResponse{Membership: membershipMutationDTO(tenantID, result)}, nil
}

func (s *IAMAdminService) mutateRoleBinding(ctx context.Context, rawTenantID, rawMembershipID, rawRoleID string, rawVersion uint64, idempotencyKey string, bind bool) (biz.TenantMembershipMutationResult, uuid.UUID, error) {
	if s.mutations == nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, status.Error(codes.Unimplemented, "tenant role-binding mutation not implemented")
	}
	scope, tenantID, err := tenantScope(rawTenantID)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	membershipID, err := requiredUUID(rawMembershipID, "membership_id")
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	roleID, err := requiredUUID(rawRoleID, "role_id")
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	expectedVersion, err := mutationVersion(rawVersion)
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, invalidArgumentStatus("idempotency_key", "IAM request is invalid")
	}
	actor, err := tenantAuthorizationActor(ctx)
	operationID := "unbindTenantIAMRole"
	if bind {
		operationID = "bindTenantIAMRole"
	}
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, mapIAMError(err, errorContext{OperationID: operationID, TenantID: tenantID.String()})
	}
	command := biz.BindTenantRoleCommand{MembershipID: membershipID, RoleID: roleID, ExpectedMembershipVersion: expectedVersion, Actor: actor}
	var result biz.TenantMembershipMutationResult
	if bind {
		result, err = s.mutations.BindRole(ctx, scope, command)
	} else {
		result, err = s.mutations.UnbindRole(ctx, scope, command)
	}
	if err != nil {
		return biz.TenantMembershipMutationResult{}, uuid.Nil, mapIAMError(err, errorContext{OperationID: operationID, TenantID: tenantID.String(), IdempotencyKey: strings.TrimSpace(idempotencyKey), DecisionID: actor.DecisionID})
	}
	return result, tenantID, nil
}

func tenantScope(raw string) (biz.TenantScope, uuid.UUID, error) {
	tenantID, err := requiredUUID(raw, "tenant_id")
	if err != nil {
		return biz.TenantScope{}, uuid.Nil, err
	}
	scope, err := biz.NewTenantScope(tenantID)
	if err != nil {
		return biz.TenantScope{}, uuid.Nil, invalidArgumentStatus("tenant_id", "IAM request is invalid")
	}
	return scope, tenantID, nil
}

func requiredUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalidArgumentStatus(field, "IAM request is invalid")
	}
	return id, nil
}

func mutationVersion(value uint64) (int64, error) {
	if value == 0 || value > math.MaxInt64 {
		return 0, invalidArgumentStatus("expected_version", "IAM request is invalid")
	}
	return int64(value), nil
}

func tenantAdminPage(page *iamv1.CursorPageRequest) (uuid.UUID, int32, error) {
	pageSize := defaultTenantAdminPageSize
	cursor := uuid.Nil
	if page == nil {
		return cursor, pageSize, nil
	}
	if page.GetPageSize() > uint32(maximumTenantAdminPageSize) {
		return uuid.Nil, 0, invalidArgumentStatus("page.page_size", "IAM request is invalid")
	}
	if page.GetPageSize() > 0 {
		pageSize = int32(page.GetPageSize())
	}
	if strings.TrimSpace(page.GetCursor()) != "" {
		var err error
		cursor, err = requiredUUID(page.GetCursor(), "page.cursor")
		if err != nil {
			return uuid.Nil, 0, err
		}
	}
	return cursor, pageSize, nil
}

func tenantAuthorizationActor(ctx context.Context) (biz.TenantAuthorizationActor, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditActorRequired
	}
	principal, err := exactlyOneMetadataValue(md, "x-ani-principal-id")
	if err != nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditActorRequired
	}
	principalID, err := uuid.Parse(principal)
	if err != nil || principalID == uuid.Nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditActorRequired
	}
	principalType, err := exactlyOneMetadataValue(md, "x-ani-principal-type")
	if err != nil || principalType != "human" {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditActorRequired
	}
	authn, err := exactlyOneMetadataValue(md, "x-ani-authn-method")
	if err != nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditAuthenticationMethodRequired
	}
	method, ok := map[string]biz.AuditAuthenticationMethod{
		"password": biz.AuditAuthenticationMethodPassword, "oidc": biz.AuditAuthenticationMethodOIDC,
		"api_key": biz.AuditAuthenticationMethodAPIKey, "service_token": biz.AuditAuthenticationMethodServiceToken,
	}[authn]
	if !ok {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditAuthenticationMethodRequired
	}
	requestID, err := exactlyOneMetadataValue(md, "x-request-id")
	if err != nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditRequestIDRequired
	}
	correlationID, err := exactlyOneMetadataValue(md, "x-correlation-id")
	if err != nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditCorrelationIDRequired
	}
	decisionID, err := exactlyOneMetadataValue(md, "x-ani-decision-id")
	if err != nil {
		return biz.TenantAuthorizationActor{}, biz.ErrAuditDecisionIDRequired
	}
	return biz.TenantAuthorizationActor{PrincipalID: principalID, AuthenticationMethod: method, RequestID: requestID, CorrelationID: correlationID, DecisionID: decisionID}, nil
}

func exactlyOneMetadataValue(md metadata.MD, key string) (string, error) {
	values := md.Get(key)
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", fmt.Errorf("metadata %s must occur exactly once", key)
	}
	return strings.TrimSpace(values[0]), nil
}

func tenantAccessDTO(tenantID uuid.UUID, access biz.TenantAccess) *iamv1.TenantAccess {
	return &iamv1.TenantAccess{TenantId: tenantID.String(), Status: tenantAccessStatusDTO(access.Status), Version: uint64(access.Version)}
}

func membershipRecordDTO(tenantID uuid.UUID, record biz.TenantMembershipRecord) *iamv1.Membership {
	roleIDs := make([]string, 0, len(record.RoleIDs))
	for _, roleID := range record.RoleIDs {
		roleIDs = append(roleIDs, roleID.String())
	}
	return membershipDTO(tenantID, record.Membership, principalTypeDTO(record.PrincipalType), roleIDs)
}

func membershipMutationDTO(tenantID uuid.UUID, result biz.TenantMembershipMutationResult) *iamv1.Membership {
	roleIDs := make([]string, 0, len(result.RoleIDs))
	for _, roleID := range result.RoleIDs {
		roleIDs = append(roleIDs, roleID.String())
	}
	return membershipDTO(tenantID, result.Membership, principalTypeDTO(result.PrincipalType), roleIDs)
}

func membershipDTO(tenantID uuid.UUID, membership biz.TenantMembership, principalType iamv1.PrincipalType, roleIDs []string) *iamv1.Membership {
	return &iamv1.Membership{
		MembershipId: membership.ID.String(), PrincipalId: membership.PrincipalID.String(), PrincipalType: principalType,
		Boundary: tenantBoundaryDTO(tenantID), Status: membershipStatusDTO(membership.Status), RoleIds: roleIDs, Version: uint64(membership.Version),
	}
}

func roleDTO(tenantID uuid.UUID, role biz.TenantRole) *iamv1.Role {
	permissions := make([]string, 0, len(role.Permissions))
	for _, permission := range role.Permissions {
		permissions = append(permissions, permission.Resource+"/"+permission.Action)
	}
	return &iamv1.Role{
		RoleId: role.ID.String(), Boundary: tenantBoundaryDTO(tenantID), Code: role.Code, DisplayName: role.DisplayName,
		System: role.System, SystemDefinitionVersion: uint64(role.SystemDefinitionVersion), Permissions: permissions, Version: uint64(role.Version),
	}
}

func tenantBoundaryDTO(tenantID uuid.UUID) *iamv1.Boundary {
	return &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenantID.String()}}}
}

func tenantAccessStatus(value iamv1.TenantAccessStatus) (biz.TenantAccessStatus, error) {
	switch value {
	case iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_ACTIVE:
		return biz.TenantAccessStatusActive, nil
	case iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_SUSPENDED:
		return biz.TenantAccessStatusSuspended, nil
	default:
		return "", biz.ErrTenantAccessInactive
	}
}

func tenantAccessStatusDTO(value biz.TenantAccessStatus) iamv1.TenantAccessStatus {
	switch value {
	case biz.TenantAccessStatusBootstrapPending:
		return iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_BOOTSTRAP_PENDING
	case biz.TenantAccessStatusActive:
		return iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_ACTIVE
	case biz.TenantAccessStatusSuspended:
		return iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_SUSPENDED
	default:
		return iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_UNSPECIFIED
	}
}

func membershipStatus(value iamv1.MembershipStatus) (biz.MembershipStatus, error) {
	switch value {
	case iamv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE:
		return biz.MembershipStatusActive, nil
	case iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED:
		return biz.MembershipStatusSuspended, nil
	case iamv1.MembershipStatus_MEMBERSHIP_STATUS_REMOVED:
		return biz.MembershipStatusRemoved, nil
	default:
		return "", biz.ErrMembershipStatusInvalid
	}
}

func optionalMembershipStatus(value iamv1.MembershipStatus) (biz.MembershipStatus, error) {
	if value == iamv1.MembershipStatus_MEMBERSHIP_STATUS_UNSPECIFIED {
		return "", nil
	}
	return membershipStatus(value)
}

func membershipStatusDTO(value biz.MembershipStatus) iamv1.MembershipStatus {
	switch value {
	case biz.MembershipStatusActive:
		return iamv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE
	case biz.MembershipStatusSuspended:
		return iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED
	case biz.MembershipStatusRemoved:
		return iamv1.MembershipStatus_MEMBERSHIP_STATUS_REMOVED
	default:
		return iamv1.MembershipStatus_MEMBERSHIP_STATUS_UNSPECIFIED
	}
}

func principalTypeDTO(value biz.PrincipalType) iamv1.PrincipalType {
	switch value {
	case biz.PrincipalTypeHuman:
		return iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN
	case biz.PrincipalTypeService:
		return iamv1.PrincipalType_PRINCIPAL_TYPE_SERVICE
	default:
		return iamv1.PrincipalType_PRINCIPAL_TYPE_UNSPECIFIED
	}
}

func cursorString(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

var _ iamv1.IAMAdminServiceServer = (*IAMAdminService)(nil)
