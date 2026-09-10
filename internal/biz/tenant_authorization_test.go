package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTenantAuthorizationUpdateMembershipRejectsLastActiveHumanAdmin(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1000-7001-9000-000000000001")
	membershipID := uuid.MustParse("0199c85a-1000-7001-9000-000000000002")
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.membership = TenantMembership{ID: membershipID, PrincipalID: uuid.MustParse("0199c85a-1000-7001-9000-000000000003"), Status: MembershipStatusActive, Version: 4}
	tx.activeHumanAdmin = true
	tx.activeHumanAdminCount = 1
	usecase := NewTenantAuthorizationUsecase(&fakeTenantAuthorizationUnitOfWork{tx: tx}, allowAllTenantPermissions{}, &fixedIDs{}, fixedAuthClock{}, allowingAPIKeyCreationLimiter{})

	_, err := usecase.UpdateMembership(context.Background(), scope, UpdateTenantMembershipCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, Status: MembershipStatusSuspended, ExpectedVersion: 4,
		Actor: validTenantAuthorizationActor(),
	})
	if !errors.Is(err, ErrLastTenantAdministrator) {
		t.Fatalf("UpdateMembership() error = %v, want %v", err, ErrLastTenantAdministrator)
	}
	if tx.guardLocks != 1 || tx.membershipUpdates != 0 || len(tx.audits) != 0 {
		t.Fatalf("guard/update/audit = %d/%d/%d", tx.guardLocks, tx.membershipUpdates, len(tx.audits))
	}
}

func TestTenantAuthorizationUnbindRejectsLastActiveHumanAdmin(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1000-7001-9000-000000000011")
	membershipID := uuid.MustParse("0199c85a-1000-7001-9000-000000000012")
	roleID := uuid.MustParse("0199c85a-1000-7001-9000-000000000013")
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.role = TenantRole{ID: roleID, Code: TenantAdminRoleCode, System: true, SystemDefinitionVersion: 1, Version: 1}
	tx.activeHumanAdmin = true
	tx.activeHumanAdminCount = 1
	usecase := NewTenantAuthorizationUsecase(&fakeTenantAuthorizationUnitOfWork{tx: tx}, allowAllTenantPermissions{}, &fixedIDs{}, fixedAuthClock{}, allowingAPIKeyCreationLimiter{})

	_, err := usecase.UnbindRole(context.Background(), scope, UnbindTenantRoleCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, RoleID: roleID, ExpectedMembershipVersion: 3,
		Actor: validTenantAuthorizationActor(),
	})
	if !errors.Is(err, ErrLastTenantAdministrator) {
		t.Fatalf("UnbindRole() error = %v, want %v", err, ErrLastTenantAdministrator)
	}
	if tx.guardLocks != 1 || tx.unbinds != 0 || len(tx.audits) != 0 {
		t.Fatalf("guard/unbind/audit = %d/%d/%d", tx.guardLocks, tx.unbinds, len(tx.audits))
	}
}

func TestTenantAuthorizationBindRoleRejectsUncataloguedPermission(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1000-7001-9000-000000000021")
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.role = TenantRole{
		ID: uuid.MustParse("0199c85a-1000-7001-9000-000000000022"), Code: "invalid-role", Version: 1,
		Permissions: []Permission{{Scope: PermissionScopeTenant, Resource: "instances", Action: "invented"}},
	}
	usecase := NewTenantAuthorizationUsecase(&fakeTenantAuthorizationUnitOfWork{tx: tx}, denyAllPermissions{}, &fixedIDs{}, fixedAuthClock{}, allowingAPIKeyCreationLimiter{})

	_, err := usecase.BindRole(context.Background(), scope, BindTenantRoleCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: uuid.MustParse("0199c85a-1000-7001-9000-000000000023"), RoleID: tx.role.ID,
		ExpectedMembershipVersion: 1, Actor: validTenantAuthorizationActor(),
	})
	if !errors.Is(err, ErrPermissionUncatalogued) {
		t.Fatalf("BindRole() error = %v, want %v", err, ErrPermissionUncatalogued)
	}
	if tx.binds != 0 || len(tx.audits) != 0 {
		t.Fatalf("bind/audit = %d/%d", tx.binds, len(tx.audits))
	}
}

func TestTenantAuthorizationRoleBindingAuditsUseBindingVersion(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1000-7001-9000-000000000024")
	membershipID := uuid.MustParse("0199c85a-1000-7001-9000-000000000025")
	roleID := uuid.MustParse("0199c85a-1000-7001-9000-000000000026")
	bindingID := uuid.MustParse("0199c85a-1000-7001-9000-000000000027")
	bindAuditID := uuid.MustParse("0199c85a-1000-7001-9000-000000000028")
	unbindAuditID := uuid.MustParse("0199c85a-1000-7001-9000-000000000029")
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.membership = TenantMembership{ID: membershipID, Version: 8}
	tx.role = TenantRole{ID: roleID, Code: "viewer", Version: 1}
	tx.bindingID = bindingID
	usecase := NewTenantAuthorizationUsecase(
		&fakeTenantAuthorizationUnitOfWork{tx: tx}, allowAllTenantPermissions{},
		&fixedIDs{values: []uuid.UUID{bindingID, bindAuditID, unbindAuditID}}, fixedAuthClock{}, allowingAPIKeyCreationLimiter{})

	if _, err := usecase.BindRole(context.Background(), scope, BindTenantRoleCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, RoleID: roleID, ExpectedMembershipVersion: 7,
		Actor: validTenantAuthorizationActor(),
	}); err != nil {
		t.Fatalf("BindRole() error = %v", err)
	}
	tx.membership.Version = 9
	if _, err := usecase.UnbindRole(context.Background(), scope, UnbindTenantRoleCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, RoleID: roleID, ExpectedMembershipVersion: 8,
		Actor: validTenantAuthorizationActor(),
	}); err != nil {
		t.Fatalf("UnbindRole() error = %v", err)
	}

	if len(tx.audits) != 2 {
		t.Fatalf("audit count = %d, want 2", len(tx.audits))
	}
	for index, audit := range tx.audits {
		if audit.TargetID != bindingID || audit.TargetVersion != 1 {
			t.Fatalf("audit[%d] target = %s/v%d, want %s/v1", index, audit.TargetID, audit.TargetVersion, bindingID)
		}
	}
}

func TestTenantAuthorizationUpdateMembershipCommitsMutationAndAudit(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1000-7001-9000-000000000031")
	membershipID := uuid.MustParse("0199c85a-1000-7001-9000-000000000032")
	auditID := uuid.MustParse("0199c85a-1000-7001-9000-000000000033")
	now := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.membership = TenantMembership{ID: membershipID, PrincipalID: uuid.MustParse("0199c85a-1000-7001-9000-000000000034"), Status: MembershipStatusActive, Version: 7}
	usecase := NewTenantAuthorizationUsecase(
		&fakeTenantAuthorizationUnitOfWork{tx: tx}, allowAllTenantPermissions{},
		&fixedIDs{values: []uuid.UUID{auditID}}, fixedAuthClock{now: now}, allowingAPIKeyCreationLimiter{})

	result, err := usecase.UpdateMembership(context.Background(), scope, UpdateTenantMembershipCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, Status: MembershipStatusSuspended, ExpectedVersion: 7,
		Actor: validTenantAuthorizationActor(),
	})
	if err != nil {
		t.Fatalf("UpdateMembership() error = %v", err)
	}
	if result.Membership.Status != MembershipStatusSuspended || result.Membership.Version != 8 || result.AuditEventID != auditID {
		t.Fatalf("result = %#v", result)
	}
	if tx.membershipUpdates != 1 || len(tx.audits) != 1 || tx.audits[0].Action != AuditActionMembershipUpdated || tx.audits[0].TargetVersion != 8 {
		t.Fatalf("mutation/audit = %d/%#v", tx.membershipUpdates, tx.audits)
	}
}

func TestTenantAuthorizationRemoveServiceMembershipDisablesPrincipalAndRevokesKeysAtomically(t *testing.T) {
	tenantID := uuid.MustParse("0199c85a-1100-7001-9000-000000000001")
	membershipID := uuid.MustParse("0199c85a-1100-7001-9000-000000000002")
	principalID := uuid.MustParse("0199c85a-1100-7001-9000-000000000003")
	membershipAuditID := uuid.MustParse("0199c85a-1100-7001-9000-000000000004")
	principalAuditID := uuid.MustParse("0199c85a-1100-7001-9000-000000000005")
	scope, _ := NewTenantScope(tenantID)
	tx := newFakeTenantAuthorizationTransaction()
	tx.membership = TenantMembership{ID: membershipID, PrincipalID: principalID, Status: MembershipStatusActive, Version: 2}
	tx.principalType = PrincipalTypeWorkload
	usecase := NewTenantAuthorizationUsecase(
		&fakeTenantAuthorizationUnitOfWork{tx: tx}, allowAllTenantPermissions{},
		&fixedIDs{values: []uuid.UUID{membershipAuditID, principalAuditID}}, fixedAuthClock{}, allowingAPIKeyCreationLimiter{})

	_, err := usecase.UpdateMembership(context.Background(), scope, UpdateTenantMembershipCommand{IdempotencyKey: uuid.NewString(),
		MembershipID: membershipID, Status: MembershipStatusRemoved, ExpectedVersion: 2,
		Actor: validTenantAuthorizationActor(),
	})
	if err != nil {
		t.Fatalf("UpdateMembership() error = %v", err)
	}
	if tx.tenantWorkloadDisables != 1 || tx.revokedAPIKeys != 3 {
		t.Fatalf("tenant workload disables/revoked keys = %d/%d", tx.tenantWorkloadDisables, tx.revokedAPIKeys)
	}
	if len(tx.audits) != 2 || tx.audits[1].Action != AuditActionTenantWorkloadDisabled || tx.audits[1].TargetID != principalID {
		t.Fatalf("audits = %#v", tx.audits)
	}
}

func validTenantAuthorizationActor() TenantAuthorizationActor {
	return TenantAuthorizationActor{
		PrincipalID:          uuid.MustParse("0199c85a-1000-7001-9000-000000000099"),
		AuthenticationMethod: AuditAuthenticationMethodPassword,
		RequestID:            "request-1", CorrelationID: "correlation-1", DecisionID: "decision-1",
	}
}

type fakeTenantAuthorizationUnitOfWork struct {
	tx *fakeTenantAuthorizationTransaction
}

func (u *fakeTenantAuthorizationUnitOfWork) WithinTenantAuthorization(ctx context.Context, _ TenantScope, fn func(context.Context, TenantAuthorizationTransaction) error) error {
	return fn(ctx, u.tx)
}

func (u *fakeTenantAuthorizationUnitOfWork) WithinTenantWorkload(ctx context.Context, _ TenantScope, fn func(context.Context, TenantWorkloadTransaction) error) error {
	return fn(ctx, &recordingTenantWorkloadTransaction{})
}

type fakeTenantAuthorizationTransaction struct {
	memoryMutationResults
	access                 TenantAccess
	membership             TenantMembership
	role                   TenantRole
	activeHumanAdmin       bool
	activeHumanAdminCount  int
	guardLocks             int
	membershipUpdates      int
	binds                  int
	unbinds                int
	bindingID              uuid.UUID
	audits                 []SecurityAuditEvent
	principalType          PrincipalType
	tenantWorkloadDisables int
	revokedAPIKeys         int64
}

func newFakeTenantAuthorizationTransaction() *fakeTenantAuthorizationTransaction {
	return &fakeTenantAuthorizationTransaction{}
}
func (t *fakeTenantAuthorizationTransaction) LockAdministrationGuard(context.Context, TenantScope) error {
	t.guardLocks++
	return nil
}
func (t *fakeTenantAuthorizationTransaction) GetAccess(context.Context, TenantScope) (TenantAccess, error) {
	return t.access, nil
}
func (t *fakeTenantAuthorizationTransaction) UpdateAccessStatus(_ context.Context, _ TenantScope, status TenantAccessStatus, _ int64, updatedAt time.Time) (TenantAccess, error) {
	t.access.Status = status
	t.access.Version++
	t.access.UpdatedAt = updatedAt
	return t.access, nil
}
func (t *fakeTenantAuthorizationTransaction) GetMembership(context.Context, TenantScope, uuid.UUID) (TenantMembership, error) {
	return t.membership, nil
}
func (t *fakeTenantAuthorizationTransaction) GetMembershipRecord(context.Context, TenantScope, uuid.UUID) (TenantMembershipRecord, error) {
	principalType := t.principalType
	if principalType == "" {
		principalType = PrincipalTypeHuman
	}
	return TenantMembershipRecord{Membership: t.membership, PrincipalType: principalType}, nil
}
func (t *fakeTenantAuthorizationTransaction) UpdateMembershipStatus(_ context.Context, _ TenantScope, _ uuid.UUID, status MembershipStatus, _ int64, updatedAt time.Time) (TenantMembership, error) {
	t.membershipUpdates++
	t.membership.Status = status
	t.membership.Version++
	t.membership.UpdatedAt = updatedAt
	return t.membership, nil
}
func (t *fakeTenantAuthorizationTransaction) GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error) {
	return t.role, nil
}
func (t *fakeTenantAuthorizationTransaction) ActiveHumanAdministratorCount(context.Context, TenantScope) (int64, error) {
	return int64(t.activeHumanAdminCount), nil
}
func (t *fakeTenantAuthorizationTransaction) IsActiveHumanAdministrator(context.Context, TenantScope, uuid.UUID) (bool, error) {
	return t.activeHumanAdmin, nil
}
func (t *fakeTenantAuthorizationTransaction) BindRole(_ context.Context, _ TenantScope, binding TenantRoleBinding, _ int64, _ time.Time) (TenantMembership, TenantRoleBinding, error) {
	t.binds++
	return t.membership, binding, nil
}

func (t *fakeTenantAuthorizationTransaction) UnbindRole(_ context.Context, _ TenantScope, membershipID uuid.UUID, roleID uuid.UUID, _ int64, _ time.Time) (TenantMembership, TenantRoleBinding, error) {
	t.unbinds++
	if t.bindingID == uuid.Nil {
		t.bindingID = uuid.MustParse("0199c85a-1000-7001-9000-0000000000aa")
	}
	return t.membership, TenantRoleBinding{ID: t.bindingID, MembershipID: membershipID, RoleID: roleID, Version: 1}, nil
}
func (t *fakeTenantAuthorizationTransaction) AppendAudit(_ context.Context, _ TenantScope, event SecurityAuditEvent) error {
	t.audits = append(t.audits, event)
	return nil
}

func (t *fakeTenantAuthorizationTransaction) DisableTenantWorkloadForMembership(_ context.Context, _ TenantScope, _ uuid.UUID, updatedAt time.Time) (TenantWorkload, int64, error) {
	t.tenantWorkloadDisables++
	t.revokedAPIKeys = 3
	return TenantWorkload{ID: t.membership.PrincipalID, Status: PrincipalStatusDisabled, Version: 2, UpdatedAt: updatedAt}, t.revokedAPIKeys, nil
}

type allowAllTenantPermissions struct{}

func (allowAllTenantPermissions) Contains(Permission) bool                 { return true }
func (allowAllTenantPermissions) Permissions(PermissionScope) []Permission { return nil }

type denyAllPermissions struct{}

func (denyAllPermissions) Contains(Permission) bool                 { return false }
func (denyAllPermissions) Permissions(PermissionScope) []Permission { return nil }
