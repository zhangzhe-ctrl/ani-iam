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
	admin := NewIAMAdminService()
	_, err := admin.GetTenantAccess(context.Background(), &iamv1.GetTenantAccessRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Platform access must be unavailable: %v", err)
	}
}

func TestTenantIAMAdminMapsTenantWorkloadAndOneTimeAPIKeySecret(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-3000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-3000-7001-9000-000000000002")
	membershipID := uuid.MustParse("0199ca10-3000-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-3000-7001-9000-000000000004")
	keyID := uuid.MustParse("0199ca10-3000-7001-9000-000000000005")
	mutations := &recordingTenantAuthorizationMutations{
		tenantWorkloadResult: biz.CreateTenantWorkloadResult{
			Principal:  biz.TenantWorkload{ID: principalID, Name: "build bot", NormalizedName: "build bot", Status: biz.PrincipalStatusActive, MembershipID: membershipID, Version: 1},
			Membership: biz.TenantMembershipRecord{Membership: biz.TenantMembership{ID: membershipID, PrincipalID: principalID, Status: biz.MembershipStatusActive, Version: 1}, PrincipalType: biz.PrincipalTypeWorkload, RoleIDs: []uuid.UUID{roleID}},
		},
		apiKeyResult: biz.CreateAPIKeyResult{APIKey: biz.APIKey{ID: keyID, PrincipalID: principalID, Status: biz.APIKeyStatusActive, DisplayPrefix: "ani_0199ca10", NeverExpires: true, CreatedAt: time.Now(), Version: 1}, Secret: "once-secret"},
	}
	auth := newAdminTestAuthority(tenantID)
	admin := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{tenantWorkload: mutations.tenantWorkloadResult.Principal}, mutations, NewAdminAuthorization(auth, auth, "revision"))
	ctx := adminTestCaller("CreateTenantWorkload")
	// Forged forwarded identity must not replace the IAM-authenticated actor.
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-ani-principal-id", principalID.String(), "x-ani-tenant-id", uuid.NewString(), "x-ani-decision-id", "forged"))
	created, err := admin.CreateTenantWorkload(ctx, &iamv1.CreateTenantWorkloadRequest{Credential: adminTestCredential(), TenantId: tenantID.String(), Name: " Build Bot ", RoleIds: []string{roleID.String()}, IdempotencyKey: "create"})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetPrincipal().GetPrincipalId() != principalID.String() || created.GetMembership().GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD || mutations.createTenantWorkload.Actor.PrincipalID != auth.principal.ID || mutations.createTenantWorkload.IdempotencyKey != "create" {
		t.Fatal("creation mapping or trusted actor mismatch")
	}
	key, err := admin.CreateAPIKey(adminTestCaller("CreateAPIKey"), &iamv1.CreateAPIKeyRequest{Credential: adminTestCredential(), PrincipalId: principalID.String(), NeverExpires: true, IdempotencyKey: "key"})
	if err != nil || key.GetApiKeySecret() != "once-secret" || key.GetReplayed() {
		t.Fatalf("initial secret contract: %v", err)
	}
	mutations.apiKeyResult.Secret = ""
	mutations.apiKeyResult.Replayed = true
	replay, err := admin.CreateAPIKey(adminTestCaller("CreateAPIKey"), &iamv1.CreateAPIKeyRequest{Credential: adminTestCredential(), PrincipalId: principalID.String(), NeverExpires: true, IdempotencyKey: "key"})
	if err != nil || replay.GetApiKeySecret() != "" || !replay.GetReplayed() {
		t.Fatalf("replay secret contract: %v", err)
	}
}

func TestTenantIAMAdminTenantWorkloadRoutesUseOnlyVerifiedCredentialScope(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-3100-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-3100-7001-9000-000000000003")
	reader := &recordingTenantAuthorizationReader{tenantWorkload: biz.TenantWorkload{ID: principalID, Status: biz.PrincipalStatusActive, Version: 1}}
	authority := newAdminTestAuthority(tenantID)
	admin := NewTenantIAMAdminService(reader, &recordingTenantAuthorizationMutations{}, NewAdminAuthorization(authority, authority, "revision"))
	response, err := admin.GetTenantWorkload(adminTestCaller("GetTenantWorkload"), &iamv1.GetTenantWorkloadRequest{Credential: adminTestCredential(), PrincipalId: principalID.String()})
	if err != nil || reader.tenantWorkloadScope != tenantID || response.GetPrincipal().GetTenantId() != tenantID.String() {
		t.Fatalf("verified scope: %v", err)
	}
	_, err = admin.ListTenantWorkloads(adminTestCaller("ListTenantWorkloads"), &iamv1.ListTenantWorkloadsRequest{Credential: adminTestCredential(), TenantId: uuid.NewString()})
	if status.Code(err) != codes.PermissionDenied || reader.listPrincipalCalls != 0 {
		t.Fatalf("foreign Tenant reached reader: %v", err)
	}
	forged := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-ani-principal-type", "human", "x-ani-tenant-id", tenantID.String()))
	_, err = admin.GetTenantWorkload(forged, &iamv1.GetTenantWorkloadRequest{Credential: adminTestCredential(), PrincipalId: principalID.String()})
	if status.Code(err) != codes.PermissionDenied || reader.tenantWorkloadCalls != 1 {
		t.Fatalf("headers substituted for caller: %v", err)
	}
	_, err = admin.GetTenantWorkload(adminTestCaller("GetTenantWorkload"), &iamv1.GetTenantWorkloadRequest{PrincipalId: principalID.String()})
	if status.Code(err) != codes.Unauthenticated || reader.tenantWorkloadCalls != 1 {
		t.Fatalf("missing subject credential accepted: %v", err)
	}
}

func TestTenantIAMAdminMapsMembershipMutationWithIAMDecision(t *testing.T) {
	tenantID := uuid.MustParse("0199c85c-3000-7001-9000-000000000001")
	membershipID := uuid.MustParse("0199c85c-3000-7001-9000-000000000002")
	authority := newAdminTestAuthority(tenantID)
	mutations := &recordingTenantAuthorizationMutations{membershipResult: biz.TenantMembershipMutationResult{Membership: biz.TenantMembership{ID: membershipID, PrincipalID: authority.principal.ID, Status: biz.MembershipStatusSuspended, Version: 4}, PrincipalType: biz.PrincipalTypeHuman}}
	admin := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{}, mutations, NewAdminAuthorization(authority, authority, "revision"))
	response, err := admin.UpdateTenantMembership(adminTestCaller("UpdateTenantMembership"), &iamv1.UpdateTenantMembershipRequest{Credential: adminTestCredential(), TenantId: tenantID.String(), MembershipId: membershipID.String(), Status: iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED, ExpectedVersion: 3, IdempotencyKey: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetMembership().GetVersion() != 4 || mutations.updateMembership.Actor.DecisionID != authority.decision.String() || mutations.updateMembership.IdempotencyKey != "update" || authority.operation != "updateTenantIAMMember" {
		t.Fatal("membership mapping mismatch")
	}
}

func TestPlatformTenantAccessRemainsUnavailableWithOrWithoutActor(t *testing.T) {
	mutations := &recordingTenantAuthorizationMutations{}
	admin := NewTenantIAMAdminService(&recordingTenantAuthorizationReader{}, mutations)
	for _, ctx := range []context.Context{context.Background(), adminTestCaller("UpdateTenantAccess")} {
		_, err := admin.UpdateTenantAccess(ctx, &iamv1.UpdateTenantAccessRequest{Credential: adminTestCredential(), TenantId: uuid.NewString(), Status: iamv1.TenantAccessStatus_TENANT_ACCESS_STATUS_SUSPENDED, ExpectedVersion: 1, IdempotencyKey: "deny"})
		if status.Code(err) != codes.Unimplemented || mutations.updateAccessCalls != 0 {
			t.Fatalf("Platform method accepted: %v", err)
		}
	}
}

type recordingTenantAuthorizationReader struct {
	access              biz.TenantAccess
	tenantWorkload      biz.TenantWorkload
	tenantWorkloadPage  biz.TenantWorkloadPage
	apiKeyPage          biz.APIKeyPage
	tenantWorkloadCalls int
	tenantWorkloadScope uuid.UUID
	listPrincipalCalls  int
	listAPIKeyCalls     int
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
func (r *recordingTenantAuthorizationReader) GetTenantWorkload(_ context.Context, scope biz.TenantScope, _ uuid.UUID) (biz.TenantWorkload, error) {
	r.tenantWorkloadCalls++
	r.tenantWorkloadScope, _ = scope.TenantID()
	if r.tenantWorkload.ID == uuid.Nil {
		return biz.TenantWorkload{}, biz.ErrTenantWorkloadNotFound
	}
	return r.tenantWorkload, nil
}
func (r *recordingTenantAuthorizationReader) ListTenantWorkloads(context.Context, biz.TenantScope, biz.PrincipalStatus, uuid.UUID, int32) (biz.TenantWorkloadPage, error) {
	r.listPrincipalCalls++
	return r.tenantWorkloadPage, nil
}
func (r *recordingTenantAuthorizationReader) ListAPIKeys(context.Context, biz.TenantScope, uuid.UUID, uuid.UUID, int32) (biz.APIKeyPage, error) {
	r.listAPIKeyCalls++
	return r.apiKeyPage, nil
}

type recordingTenantAuthorizationMutations struct {
	updateMembership     biz.UpdateTenantMembershipCommand
	membershipResult     biz.TenantMembershipMutationResult
	updateAccessCalls    int
	createTenantWorkload biz.CreateTenantWorkloadCommand
	tenantWorkloadResult biz.CreateTenantWorkloadResult
	createAPIKey         biz.CreateAPIKeyCommand
	apiKeyResult         biz.CreateAPIKeyResult
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
func (m *recordingTenantAuthorizationMutations) CreateTenantWorkload(_ context.Context, _ biz.TenantScope, command biz.CreateTenantWorkloadCommand) (biz.CreateTenantWorkloadResult, error) {
	m.createTenantWorkload = command
	return m.tenantWorkloadResult, nil
}
func (m *recordingTenantAuthorizationMutations) CreateAPIKey(_ context.Context, _ biz.TenantScope, command biz.CreateAPIKeyCommand) (biz.CreateAPIKeyResult, error) {
	m.createAPIKey = command
	return m.apiKeyResult, nil
}
func (*recordingTenantAuthorizationMutations) UpdateTenantWorkload(context.Context, biz.TenantScope, biz.UpdateTenantWorkloadCommand) (biz.UpdateTenantWorkloadResult, error) {
	return biz.UpdateTenantWorkloadResult{}, nil
}
func (*recordingTenantAuthorizationMutations) RevokeAPIKey(context.Context, biz.TenantScope, biz.RevokeAPIKeyCommand) (biz.RevokeAPIKeyResult, error) {
	return biz.RevokeAPIKeyResult{}, nil
}
