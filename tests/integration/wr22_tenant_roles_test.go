//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR22TenantRoleTransactions(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Now().UTC()
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	tenant, other, admin, otherAdmin, member, otherMember, builtin, otherBuiltin := mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenant, builtin, now, []uuid.UUID{admin}, []uuid.UUID{member})
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, other, otherBuiltin, now, []uuid.UUID{otherAdmin}, []uuid.UUID{otherMember})
	scope, otherScope := mustTenantScope(t, tenant), mustTenantScope(t, other)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	d := data.NewData(environment.runtimePool)
	u := biz.NewTenantRoleUsecase(data.NewPostgresTenantRoleUnitOfWork(d), catalog, data.NewUUIDv7Generator(), data.NewSystemClock())
	reader := data.NewPostgresTenantAuthorizationReader(d)
	bindings := biz.NewTenantAuthorizationUsecase(data.NewPostgresTenantAuthorizationUnitOfWork(d), catalog, data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})
	actor := biz.TenantAuthorizationActor{PrincipalID: admin, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "wr22-roles", CorrelationID: "wr22-roles", DecisionID: "wr22-roles"}
	otherActor := actor
	otherActor.PrincipalID = otherAdmin
	read := biz.Permission{Scope: biz.PermissionScopeTenant, Resource: "instances", Action: "read"}
	create := read
	create.Action = "create"
	command := biz.CreateTenantRoleCommand{DisplayName: "Readers", Permissions: []biz.Permission{read, read}, IdempotencyKey: uuid.NewString(), Actor: actor}
	var role biz.TenantRole
	t.Run("concurrent_create_commits_one_role_and_audit", func(t *testing.T) {
		type outcome struct {
			result biz.TenantRoleMutationResult
			err    error
		}
		out := make(chan outcome, 4)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r, e := u.Create(ctx, scope, command); out <- outcome{r, e} }()
		}
		wg.Wait()
		close(out)
		var audit uuid.UUID
		for result := range out {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if role.ID == uuid.Nil {
				role = result.result.Role
				audit = result.result.AuditEventID
			}
			if result.result.Role.ID != role.ID || result.result.AuditEventID != audit {
				t.Fatal("replay changed identity")
			}
		}
		var roles, audits, permissions int
		if err := environment.runtimePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_roles WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.role.created'),(SELECT count(*) FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2)`, tenant, role.ID).Scan(&roles, &audits, &permissions); err != nil {
			t.Fatal(err)
		}
		if roles != 1 || audits != 1 || permissions != 1 || role.DisplayName != "Readers" || role.System {
			t.Fatal("role/permission/audit atomic result differs")
		}
	})
	if role.ID == uuid.Nil {
		t.Fatal("role creation prerequisite failed")
	}
	t.Run("scope_catalog_system_role_and_current_admin_guard", func(t *testing.T) {
		c := command
		c.IdempotencyKey = uuid.NewString()
		c.Permissions = []biz.Permission{{Scope: biz.PermissionScopePlatform, Resource: "iam.platform-roles", Action: "create"}}
		if _, e := u.Create(ctx, scope, c); !errors.Is(e, biz.ErrTenantRoleBoundaryInvalid) {
			t.Fatalf("platform permission: %v", e)
		}
		c = command
		c.IdempotencyKey = uuid.NewString()
		c.Permissions = []biz.Permission{{Scope: biz.PermissionScopeTenant, Resource: "invented", Action: "read"}}
		if _, e := u.Create(ctx, scope, c); !errors.Is(e, biz.ErrPermissionUncatalogued) {
			t.Fatalf("invented permission: %v", e)
		}
		c = command
		c.Actor = otherActor
		if _, e := u.Create(ctx, scope, c); !errors.Is(e, biz.ErrTenantAdministratorRequired) {
			t.Fatalf("foreign actor: %v", e)
		}
		if _, e := u.Update(ctx, otherScope, biz.UpdateTenantRoleCommand{RoleID: role.ID, DisplayName: "Wrong", Permissions: []biz.Permission{read}, ExpectedVersion: 1, IdempotencyKey: uuid.NewString(), Actor: otherActor}); !errors.Is(e, biz.ErrRoleNotFound) {
			t.Fatalf("foreign role: %v", e)
		}
		if _, e := u.Delete(ctx, scope, biz.DeleteTenantRoleCommand{RoleID: builtin, ExpectedVersion: 1, IdempotencyKey: uuid.NewString(), Actor: actor}); !errors.Is(e, biz.ErrSystemRoleImmutable) {
			t.Fatalf("system role deletion: %v", e)
		}
		c = command
		c.DisplayName = "Different"
		if _, e := u.Create(ctx, scope, c); !errors.Is(e, biz.ErrIdempotencyConflict) {
			t.Fatalf("intent conflict: %v", e)
		}
	})
	t.Run("role_update_cas_and_permission_replacement", func(t *testing.T) {
		errorsCh := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				_, e := u.Update(ctx, scope, biz.UpdateTenantRoleCommand{RoleID: role.ID, DisplayName: "Creators", Permissions: []biz.Permission{create}, ExpectedVersion: 1, IdempotencyKey: uuid.NewString(), Actor: actor})
				errorsCh <- e
			}()
		}
		succeeded, conflicts := 0, 0
		for i := 0; i < 2; i++ {
			e := <-errorsCh
			if e == nil {
				succeeded++
			} else if errors.Is(e, biz.ErrVersionConflict) {
				conflicts++
			} else {
				t.Fatal(e)
			}
		}
		if succeeded != 1 || conflicts != 1 {
			t.Fatal("version compare-and-swap did not serialize writers")
		}
		r, e := reader.GetRole(ctx, scope, role.ID)
		if e != nil {
			t.Fatal(e)
		}
		if r.Version != 2 || r.Code != role.Code || r.DisplayName != "Creators" || len(r.Permissions) != 1 || r.Permissions[0] != create {
			t.Fatal("role replacement differs")
		}
		role = r
	})
	t.Run("audit_failure_rolls_back_role_permissions_and_ledger", func(t *testing.T) {
		if _, e := owner.Exec(ctx, `REVOKE INSERT ON iam_audit_events FROM ani_iam_runtime`); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := owner.Exec(ctx, `GRANT INSERT ON iam_audit_events TO ani_iam_runtime`); e != nil {
				t.Fatal(e)
			}
		}()
		key := uuid.NewString()
		_, e := u.Update(ctx, scope, biz.UpdateTenantRoleCommand{RoleID: role.ID, DisplayName: "Must rollback", Permissions: []biz.Permission{read}, ExpectedVersion: 2, IdempotencyKey: key, Actor: actor})
		if e == nil {
			t.Fatal("audit failure accepted")
		}
		r, e := reader.GetRole(ctx, scope, role.ID)
		if e != nil {
			t.Fatal(e)
		}
		var results int
		if e := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_mutation_results WHERE tenant_id=$1 AND actor_id=$2 AND operation='updateTenantIAMRole' AND idempotency_key=$3`, tenant, admin, key).Scan(&results); e != nil {
			t.Fatal(e)
		}
		if r.Version != 2 || r.DisplayName != "Creators" || len(r.Permissions) != 1 || r.Permissions[0] != create || results != 0 {
			t.Fatal("audit failure left partial state")
		}
	})
	t.Run("referenced_role_cannot_be_deleted_then_idempotent_removal", func(t *testing.T) {
		bound, e := bindings.BindRole(ctx, scope, biz.BindTenantRoleCommand{MembershipID: member, RoleID: role.ID, ExpectedMembershipVersion: 1, IdempotencyKey: uuid.NewString(), Actor: actor})
		if e != nil {
			t.Fatal(e)
		}
		c := biz.DeleteTenantRoleCommand{RoleID: role.ID, ExpectedVersion: 2, IdempotencyKey: uuid.NewString(), Actor: actor}
		if _, e := u.Delete(ctx, scope, c); !errors.Is(e, biz.ErrRoleInUse) {
			t.Fatalf("referenced role deletion: %v", e)
		}
		if _, e := bindings.UnbindRole(ctx, scope, biz.UnbindTenantRoleCommand{MembershipID: member, RoleID: role.ID, ExpectedMembershipVersion: bound.Membership.Version, IdempotencyKey: uuid.NewString(), Actor: actor}); e != nil {
			t.Fatal(e)
		}
		first, e := u.Delete(ctx, scope, c)
		if e != nil {
			t.Fatal(e)
		}
		second, e := u.Delete(ctx, scope, c)
		if e != nil {
			t.Fatal(e)
		}
		if first.AuditEventID != second.AuditEventID || second.Role.Version != 3 {
			t.Fatal("delete replay differs")
		}
		if _, e := reader.GetRole(ctx, scope, role.ID); !errors.Is(e, biz.ErrRoleNotFound) {
			t.Fatalf("deleted role remains: %v", e)
		}
		var audit int
		if e := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.role.deleted'`, tenant, role.ID).Scan(&audit); e != nil {
			t.Fatal(e)
		}
		if audit != 1 {
			t.Fatal("delete audit missing or duplicated")
		}
	})
}
