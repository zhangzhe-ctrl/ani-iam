package service

import (
	"context"
	"testing"
	"time"

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

func TestTenantIAMAdminMapsServicePrincipalAndOneTimeAPIKeySecret(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-3000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-3000-7001-9000-000000000002")
	membershipID := uuid.MustParse("0199ca10-3000-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-3000-7001-9000-000000000004")
	keyID := uuid.MustParse("0199ca10-3000-7001-9000-000000000005")
	mutations := &recordingTenantAuthorizationMutations{
		servicePrincipalResult: biz.CreateServicePrincipalResult{
			Principal:  biz.ServicePrincipal{ID: principalID, Name: "Build Bot", NormalizedName: "build bot", Status: biz.PrincipalStatusActive, MembershipID: membershipID, Version: 1},
			Membership: biz.TenantMembershipRecord{Membership: biz.TenantMembership{ID: membershipID, PrincipalID: principalID, Status: biz.MembershipStatusActive, Version: 1}, PrincipalType: biz.PrincipalTypeService, RoleIDs: []uuid.UUID{roleID}},
		},
		apiKeyResult: biz.CreateAPIKeyResult{APIKey: biz.APIKey{
			ID: keyID, PrincipalID: principalID, Status: biz.APIKeyStatusActive,
			DisplayPrefix: "ani_0199ca10", NeverExpires: true, CreatedAt: time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC), Version: 1,
		}, Secret: "ani_0199ca10-secret-once"},
	}
	service := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{servicePrincipal: mutations.servicePrincipalResult.Principal}, mutations)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-ani-principal-id", "0199ca10-3000-7001-9000-000000000099",
		"x-ani-principal-type", "human", "x-ani-authn-method", "password",
		"x-ani-tenant-id", tenantID.String(),
		"x-ani-decision-id", "decision-sp", "x-request-id", "request-sp", "x-correlation-id", "correlation-sp",
	))
	created, err := service.CreateServicePrincipal(ctx, &iamv1.CreateServicePrincipalRequest{
		TenantId: tenantID.String(), Name: " Build Bot ", RoleIds: []string{roleID.String()}, IdempotencyKey: "create-sp-1",
	})
	if err != nil {
		t.Fatalf("CreateServicePrincipal() error = %v", err)
	}
	if mutations.createServicePrincipal.Name != "Build Bot" || mutations.createServicePrincipal.RoleIDs[0] != roleID || created.GetPrincipal().GetPrincipalId() != principalID.String() || created.GetMembership().GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_SERVICE {
		t.Fatalf("service principal mapping = command:%#v response:%#v", mutations.createServicePrincipal, created)
	}
	key, err := service.CreateAPIKey(ctx, &iamv1.CreateAPIKeyRequest{
		PrincipalId: principalID.String(), NeverExpires: true, IdempotencyKey: "create-key-1",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if mutations.createAPIKey.PrincipalID != principalID || key.GetApiKeySecret() != mutations.apiKeyResult.Secret || key.GetApiKey().GetKeyId() != keyID.String() {
		t.Fatalf("API key mapping = command:%#v response:%#v", mutations.createAPIKey, key)
	}
}

func TestTenantIAMAdminServicePrincipalRoutesUseOnlyTrustedTenantScope(t *testing.T) {
	trustedTenantID := uuid.MustParse("0199ca10-3100-7001-9000-000000000001")
	foreignTenantID := uuid.MustParse("0199ca10-3100-7001-9000-000000000002")
	principalID := uuid.MustParse("0199ca10-3100-7001-9000-000000000003")
	reader := &recordingTenantAuthorizationReader{servicePrincipal: biz.ServicePrincipal{ID: principalID, Status: biz.PrincipalStatusActive, Version: 1}}
	mutations := &recordingTenantAuthorizationMutations{}
	service := NewTenantIAMAdminService(reader, mutations)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-ani-principal-id", "0199ca10-3100-7001-9000-000000000099",
		"x-ani-principal-type", "human", "x-ani-authn-method", "password",
		"x-ani-tenant-id", trustedTenantID.String(),
		"x-ani-decision-id", "decision-scope", "x-request-id", "request-scope", "x-correlation-id", "correlation-scope",
	))

	response, err := service.GetServicePrincipal(ctx, &iamv1.GetServicePrincipalRequest{PrincipalId: principalID.String()})
	if err != nil {
		t.Fatalf("GetServicePrincipal() error = %v", err)
	}
	if reader.servicePrincipalCalls != 1 || reader.servicePrincipalScope != trustedTenantID || response.GetPrincipal().GetTenantId() != trustedTenantID.String() {
		t.Fatalf("trusted scope = calls:%d scope:%s response:%#v", reader.servicePrincipalCalls, reader.servicePrincipalScope, response)
	}

	_, err = service.ListServicePrincipals(ctx, &iamv1.ListServicePrincipalsRequest{TenantId: foreignTenantID.String()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ListServicePrincipals(cross-tenant) code = %s, want PermissionDenied", status.Code(err))
	}
	if reader.listPrincipalCalls != 0 {
		t.Fatalf("cross-tenant list reached repository: %d", reader.listPrincipalCalls)
	}

	withoutTrustedTenant := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-ani-principal-id", "0199ca10-3100-7001-9000-000000000099",
		"x-ani-principal-type", "human", "x-ani-authn-method", "password",
		"x-ani-decision-id", "decision-missing", "x-request-id", "request-missing", "x-correlation-id", "correlation-missing",
	))
	_, err = service.GetServicePrincipal(withoutTrustedTenant, &iamv1.GetServicePrincipalRequest{PrincipalId: principalID.String()})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetServicePrincipal(missing trusted tenant) code = %s, want PermissionDenied", status.Code(err))
	}
	if reader.servicePrincipalCalls != 1 {
		t.Fatalf("missing trusted tenant reached repository: %d", reader.servicePrincipalCalls)
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

type recordingTenantAuthorizationReader struct {
	access                biz.TenantAccess
	servicePrincipal      biz.ServicePrincipal
	servicePrincipalPage  biz.ServicePrincipalPage
	apiKeyPage            biz.APIKeyPage
	servicePrincipalCalls int
	servicePrincipalScope uuid.UUID
	listPrincipalCalls    int
	listAPIKeyCalls       int
}

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
func (r *recordingTenantAuthorizationReader) GetServicePrincipal(_ context.Context, scope biz.TenantScope, _ uuid.UUID) (biz.ServicePrincipal, error) {
	r.servicePrincipalCalls++
	r.servicePrincipalScope, _ = scope.TenantID()
	if r.servicePrincipal.ID == uuid.Nil {
		return biz.ServicePrincipal{}, biz.ErrServicePrincipalNotFound
	}
	return r.servicePrincipal, nil
}
func (r *recordingTenantAuthorizationReader) ListServicePrincipals(context.Context, biz.TenantScope, biz.PrincipalStatus, uuid.UUID, int32) (biz.ServicePrincipalPage, error) {
	r.listPrincipalCalls++
	return r.servicePrincipalPage, nil
}
func (r *recordingTenantAuthorizationReader) ListAPIKeys(context.Context, biz.TenantScope, uuid.UUID, uuid.UUID, int32) (biz.APIKeyPage, error) {
	r.listAPIKeyCalls++
	return r.apiKeyPage, nil
}

type recordingTenantAuthorizationMutations struct {
	updateMembership       biz.UpdateTenantMembershipCommand
	membershipResult       biz.TenantMembershipMutationResult
	updateAccessCalls      int
	createServicePrincipal biz.CreateServicePrincipalCommand
	servicePrincipalResult biz.CreateServicePrincipalResult
	createAPIKey           biz.CreateAPIKeyCommand
	apiKeyResult           biz.CreateAPIKeyResult
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
func (m *recordingTenantAuthorizationMutations) CreateServicePrincipal(_ context.Context, _ biz.TenantScope, command biz.CreateServicePrincipalCommand) (biz.CreateServicePrincipalResult, error) {
	m.createServicePrincipal = command
	return m.servicePrincipalResult, nil
}
func (m *recordingTenantAuthorizationMutations) CreateAPIKey(_ context.Context, _ biz.TenantScope, command biz.CreateAPIKeyCommand) (biz.CreateAPIKeyResult, error) {
	m.createAPIKey = command
	return m.apiKeyResult, nil
}
func (*recordingTenantAuthorizationMutations) UpdateServicePrincipal(context.Context, biz.TenantScope, biz.UpdateServicePrincipalCommand) (biz.UpdateServicePrincipalResult, error) {
	return biz.UpdateServicePrincipalResult{}, nil
}
func (*recordingTenantAuthorizationMutations) RevokeAPIKey(context.Context, biz.TenantScope, biz.RevokeAPIKeyCommand) (biz.RevokeAPIKeyResult, error) {
	return biz.RevokeAPIKeyResult{}, nil
}
