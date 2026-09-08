//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestAuthorizationReaderTreatsMissingAndStaleLifecycleAsUnavailable(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199c85b-2000-7001-9000-000000000301")
	roleID := uuid.MustParse("0199c85b-2000-7001-9000-000000000302")
	now := time.Date(2026, 9, 8, 10, 30, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, nil, nil)

	reader := data.NewPostgresAuthorizationReader(data.NewData(environment.runtimePool))
	lookup := biz.AuthorizationLookup{
		PrincipalID: uuid.MustParse("0199c85b-2000-7001-9000-000000000303"),
		SessionID:   uuid.MustParse("0199c85b-2000-7001-9000-000000000304"),
		GrantID:     uuid.MustParse("0199c85b-2000-7001-9000-000000000305"),
		Resource:    "instances",
		Actions:     []string{"read"},
	}
	scope := mustTenantScope(t, tenantID)
	if _, err := reader.LookupAuthorization(ctx, scope, lookup); !errors.Is(err, biz.ErrTenantLifecycleStale) {
		t.Fatalf("missing lifecycle projection error = %v, want %v", err, biz.ErrTenantLifecycleStale)
	}

	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	staleAt := time.Now().UTC().Add(-time.Minute)
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenant_lifecycle_projections
			(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until)
		VALUES ($1,'active',1,$2,$2,$2)
	`, tenantID, staleAt); err != nil {
		t.Fatalf("seed stale lifecycle projection: %v", err)
	}
	if _, err := reader.LookupAuthorization(ctx, scope, lookup); !errors.Is(err, biz.ErrTenantLifecycleStale) {
		t.Fatalf("stale lifecycle projection error = %v, want %v", err, biz.ErrTenantLifecycleStale)
	}
}

func TestTenantRolePermissionsAreConstrainedToGeneratedTenantCatalog(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199c85b-2000-7001-9000-000000000311")
	roleID := uuid.MustParse("0199c85b-2000-7001-9000-000000000312")
	now := time.Date(2026, 9, 8, 10, 45, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, nil, nil)

	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	var scopeColumns int
	if err := owner.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema='public' AND table_name='tenant_role_permissions' AND column_name='scope'
	`).Scan(&scopeColumns); err != nil {
		t.Fatalf("inspect tenant permission scope column: %v", err)
	}
	if scopeColumns != 1 {
		t.Fatalf("tenant role permission scope columns = %d, want 1", scopeColumns)
	}
	var foreignKeys int
	if err := owner.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid='tenant_role_permissions'::regclass
		  AND contype='f'
		  AND confrelid='permission_catalog'::regclass
	`).Scan(&foreignKeys); err != nil {
		t.Fatalf("inspect permission catalog foreign key: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("permission catalog foreign keys = %d, want 1", foreignKeys)
	}

	_, err := owner.Exec(ctx, `
		INSERT INTO tenant_role_permissions (tenant_id,role_id,scope,resource,action,created_at)
		VALUES ($1,$2,'tenant','instances','invented',now())
	`, tenantID, roleID)
	assertPostgresCode(t, err, "23503", "uncatalogued tenant permission")

	var platformResource, platformAction string
	if err := owner.QueryRow(ctx, `
		SELECT resource, action
		FROM permission_catalog
		WHERE scope='platform'
		ORDER BY resource, action
		LIMIT 1
	`).Scan(&platformResource, &platformAction); err != nil {
		t.Fatalf("select catalogued platform permission: %v", err)
	}
	_, err = owner.Exec(ctx, `
		INSERT INTO tenant_role_permissions (tenant_id,role_id,scope,resource,action,created_at)
		VALUES ($1,$2,'platform',$3,$4,now())
	`, tenantID, roleID, platformResource, platformAction)
	assertPostgresCode(t, err, "23514", "platform permission in tenant role")
}

func assertPostgresCode(t *testing.T, err error, want, context string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != want {
		t.Fatalf("%s error = %v, want PostgreSQL code %s", context, err, want)
	}
}

func TestTenantAuthorizationRestrictedPostgresAndLastAdminConcurrency(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	missingScope := mustTenantScope(t, uuid.MustParse("0199c85b-2000-7001-9000-0000000000ff"))
	_, err := data.NewPostgresAuthorizationReader(data.NewData(environment.runtimePool)).LookupAuthorization(ctx, missingScope, biz.AuthorizationLookup{
		PrincipalID:          uuid.MustParse("0199c85b-2000-7001-9000-0000000000f1"),
		SessionID:            uuid.MustParse("0199c85b-2000-7001-9000-0000000000f2"),
		GrantID:              uuid.MustParse("0199c85b-2000-7001-9000-0000000000f3"),
		ExpectedGrantVersion: 1, Resource: "instances", Actions: []string{"read"},
	})
	if !errors.Is(err, biz.ErrTenantIAMNotReady) {
		t.Fatalf("missing Tenant Access error = %v, want %v", err, biz.ErrTenantIAMNotReady)
	}
	tenantOne := uuid.MustParse("0199c85b-2000-7001-9000-000000000001")
	tenantTwo := uuid.MustParse("0199c85b-2000-7001-9000-000000000002")
	adminOne := uuid.MustParse("0199c85b-2000-7001-9000-000000000011")
	adminTwo := uuid.MustParse("0199c85b-2000-7001-9000-000000000012")
	otherAdmin := uuid.MustParse("0199c85b-2000-7001-9000-000000000013")
	membershipOne := uuid.MustParse("0199c85b-2000-7001-9000-000000000021")
	membershipTwo := uuid.MustParse("0199c85b-2000-7001-9000-000000000022")
	otherMembership := uuid.MustParse("0199c85b-2000-7001-9000-000000000023")
	roleOne := uuid.MustParse("0199c85b-2000-7001-9000-000000000031")
	roleTwo := uuid.MustParse("0199c85b-2000-7001-9000-000000000032")
	now := time.Date(2026, 9, 8, 9, 30, 0, 0, time.UTC)

	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantOne, roleOne, now,
		[]uuid.UUID{adminOne, adminTwo}, []uuid.UUID{membershipOne, membershipTwo})
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantTwo, roleTwo, now,
		[]uuid.UUID{otherAdmin}, []uuid.UUID{otherMembership})

	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	usecase := biz.NewTenantAuthorizationUsecase(
		data.NewPostgresTenantAuthorizationUnitOfWork(data.NewData(environment.runtimePool)),
		catalog, data.NewUUIDv7Generator(), data.NewSystemClock(),
	)
	scopeOne := mustTenantScope(t, tenantOne)
	reader := data.NewPostgresTenantAuthorizationReader(data.NewData(environment.runtimePool))
	record, err := reader.GetMembership(ctx, scopeOne, membershipOne)
	if err != nil {
		t.Fatalf("GetMembership() error = %v", err)
	}
	if record.PrincipalType != biz.PrincipalTypeHuman || len(record.RoleIDs) != 1 || record.RoleIDs[0] != roleOne {
		t.Fatalf("tenant-one membership authority = %#v", record)
	}
	if _, err := reader.GetMembership(ctx, scopeOne, otherMembership); !errors.Is(err, biz.ErrMembershipNotFound) {
		t.Fatalf("cross-tenant GetMembership() error = %v, want %v", err, biz.ErrMembershipNotFound)
	}
	if _, err := reader.GetRole(ctx, scopeOne, roleTwo); !errors.Is(err, biz.ErrRoleNotFound) {
		t.Fatalf("cross-tenant GetRole() error = %v, want %v", err, biz.ErrRoleNotFound)
	}
	if _, err := usecase.BindRole(ctx, scopeOne, biz.BindTenantRoleCommand{
		MembershipID: membershipOne, RoleID: roleTwo, ExpectedMembershipVersion: 1,
		Actor: biz.TenantAuthorizationActor{
			PrincipalID: adminOne, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
			RequestID: "cross-tenant-bind", CorrelationID: "tenant-one", DecisionID: "cross-tenant-bind",
		},
	}); !errors.Is(err, biz.ErrRoleNotFound) {
		t.Fatalf("cross-tenant BindRole() error = %v, want %v", err, biz.ErrRoleNotFound)
	}
	var crossTenantBindings int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND role_id=$2`, tenantOne, roleTwo).Scan(&crossTenantBindings); err != nil {
		t.Fatal(err)
	}
	if crossTenantBindings != 0 {
		t.Fatalf("cross-tenant bindings = %d, want 0", crossTenantBindings)
	}

	start := make(chan struct{})
	errorsByMembership := make(chan error, 2)
	var wait sync.WaitGroup
	for _, membershipID := range []uuid.UUID{membershipOne, membershipTwo} {
		membershipID := membershipID
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, mutationErr := usecase.UpdateMembership(ctx, scopeOne, biz.UpdateTenantMembershipCommand{
				MembershipID: membershipID, Status: biz.MembershipStatusSuspended, ExpectedVersion: 1,
				Actor: biz.TenantAuthorizationActor{
					PrincipalID: adminOne, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
					RequestID: "concurrent-last-admin", CorrelationID: "tenant-one", DecisionID: membershipID.String(),
				},
			})
			errorsByMembership <- mutationErr
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByMembership)
	var succeeded, protected int
	for mutationErr := range errorsByMembership {
		switch {
		case mutationErr == nil:
			succeeded++
		case errors.Is(mutationErr, biz.ErrLastTenantAdministrator):
			protected++
		default:
			t.Fatalf("concurrent mutation error = %v", mutationErr)
		}
	}
	if succeeded != 1 || protected != 1 {
		t.Fatalf("concurrent outcomes = success:%d protected:%d", succeeded, protected)
	}
	var activeOne, activeTwo int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND status='active'`, tenantOne).Scan(&activeOne); err != nil {
		t.Fatal(err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND status='active'`, tenantTwo).Scan(&activeTwo); err != nil {
		t.Fatal(err)
	}
	if activeOne != 1 || activeTwo != 1 {
		t.Fatalf("active memberships tenant one/two = %d/%d", activeOne, activeTwo)
	}
}

func TestTenantAuthorizationAuditFailureRollsBackMembershipMutation(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199c85b-2000-7001-9000-000000000101")
	adminID := uuid.MustParse("0199c85b-2000-7001-9000-000000000102")
	membershipID := uuid.MustParse("0199c85b-2000-7001-9000-000000000103")
	roleID := uuid.MustParse("0199c85b-2000-7001-9000-000000000104")
	now := time.Date(2026, 9, 8, 9, 45, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, []uuid.UUID{adminID}, []uuid.UUID{membershipID})
	catalog, _ := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	usecase := biz.NewTenantAuthorizationUsecase(data.NewPostgresTenantAuthorizationUnitOfWork(data.NewData(environment.runtimePool)), catalog, data.NewUUIDv7Generator(), data.NewSystemClock())

	_, err := usecase.UpdateMembership(ctx, mustTenantScope(t, tenantID), biz.UpdateTenantMembershipCommand{
		MembershipID: membershipID, Status: biz.MembershipStatusActive, ExpectedVersion: 1,
		Actor: biz.TenantAuthorizationActor{
			PrincipalID:          uuid.MustParse("0199c85b-2000-7001-9000-0000000001ff"),
			AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
			RequestID:            "audit-failure", CorrelationID: "rollback", DecisionID: "decision-rollback",
		},
	})
	if err == nil {
		t.Fatal("UpdateMembership() unexpectedly succeeded with a missing audit actor")
	}
	var status string
	var version int64
	if queryErr := environment.runtimePool.QueryRow(ctx, `SELECT status, version FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, tenantID, membershipID).Scan(&status, &version); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "active" || version != 1 {
		t.Fatalf("membership after audit failure = %s/v%d", status, version)
	}
}

func TestTenantAuthorizationRoleBindingAuditsTargetTheBindingVersion(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199c85b-2000-7001-9000-000000000201")
	actorID := uuid.MustParse("0199c85b-2000-7001-9000-000000000202")
	actorMembershipID := uuid.MustParse("0199c85b-2000-7001-9000-000000000203")
	adminRoleID := uuid.MustParse("0199c85b-2000-7001-9000-000000000204")
	targetPrincipalID := uuid.MustParse("0199c85b-2000-7001-9000-000000000205")
	targetMembershipID := uuid.MustParse("0199c85b-2000-7001-9000-000000000206")
	viewerRoleID := uuid.MustParse("0199c85b-2000-7001-9000-000000000207")
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, adminRoleID, now, []uuid.UUID{actorID}, []uuid.UUID{actorMembershipID})
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO principals (id,principal_type,status,version,created_at,updated_at) VALUES ($1,'human','active',1,$2,$2)`, []any{targetPrincipalID, now}},
		{`INSERT INTO tenant_memberships (tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES ($1,$2,$3,'active',1,$4,$4)`, []any{tenantID, targetMembershipID, targetPrincipalID, now}},
		{`INSERT INTO tenant_roles (tenant_id,id,code,system_role,system_definition_version,version,created_at,updated_at) VALUES ($1,$2,'viewer',true,1,1,$3,$3)`, []any{tenantID, viewerRoleID, now}},
	} {
		if _, err := environment.runtimePool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed role-binding boundary: %v", err)
		}
	}
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	usecase := biz.NewTenantAuthorizationUsecase(
		data.NewPostgresTenantAuthorizationUnitOfWork(data.NewData(environment.runtimePool)),
		catalog, data.NewUUIDv7Generator(), data.NewSystemClock(),
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "binding-audit", CorrelationID: "binding-audit", DecisionID: "binding-audit",
	}
	scope := mustTenantScope(t, tenantID)
	bound, err := usecase.BindRole(ctx, scope, biz.BindTenantRoleCommand{
		MembershipID: targetMembershipID, RoleID: viewerRoleID, ExpectedMembershipVersion: 1, Actor: actor,
	})
	if err != nil {
		t.Fatalf("BindRole() error = %v", err)
	}
	if bound.Membership.Version != 2 {
		t.Fatalf("bound membership version = %d, want 2", bound.Membership.Version)
	}
	var bindingID uuid.UUID
	var bindingVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `SELECT target_id, target_version FROM iam_audit_events WHERE event_id=$1`, bound.AuditEventID).Scan(&bindingID, &bindingVersion); err != nil {
		t.Fatal(err)
	}
	if bindingVersion != 1 {
		t.Fatalf("bind audit target = %s/v%d, want binding v1", bindingID, bindingVersion)
	}

	unbound, err := usecase.UnbindRole(ctx, scope, biz.UnbindTenantRoleCommand{
		MembershipID: targetMembershipID, RoleID: viewerRoleID, ExpectedMembershipVersion: 2, Actor: actor,
	})
	if err != nil {
		t.Fatalf("UnbindRole() error = %v", err)
	}
	if unbound.Membership.Version != 3 {
		t.Fatalf("unbound membership version = %d, want 3", unbound.Membership.Version)
	}
	var deletedBindingID uuid.UUID
	var deletedBindingVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `SELECT target_id, target_version FROM iam_audit_events WHERE event_id=$1`, unbound.AuditEventID).Scan(&deletedBindingID, &deletedBindingVersion); err != nil {
		t.Fatal(err)
	}
	if deletedBindingID != bindingID || deletedBindingVersion != 1 {
		t.Fatalf("unbind audit target = %s/v%d, want %s/v1", deletedBindingID, deletedBindingVersion, bindingID)
	}
}

func seedTenantAuthorizationBoundary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, roleID uuid.UUID, now time.Time, principals, memberships []uuid.UUID) {
	t.Helper()
	if len(principals) != len(memberships) {
		t.Fatal("principal and membership fixture lengths differ")
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenant_access (tenant_id,status,version,created_at,updated_at) VALUES ($1,'active',1,$2,$2)`, []any{tenantID, now}},
		{`INSERT INTO tenant_roles (tenant_id,id,code,system_role,system_definition_version,version,created_at,updated_at) VALUES ($1,$2,'tenant-admin',true,1,1,$3,$3)`, []any{tenantID, roleID, now}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed tenant authorization boundary: %v", err)
		}
	}
	for index, principalID := range principals {
		membershipID := memberships[index]
		bindingID, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO principals (id,principal_type,status,version,created_at,updated_at) VALUES ($1,'human','active',1,$2,$2)`, []any{principalID, now}},
			{`INSERT INTO tenant_memberships (tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES ($1,$2,$3,'active',1,$4,$4)`, []any{tenantID, membershipID, principalID, now}},
			{`INSERT INTO tenant_role_bindings (tenant_id,id,membership_id,role_id,version,created_at,updated_at) VALUES ($1,$2,$3,$4,1,$5,$5)`, []any{tenantID, bindingID, membershipID, roleID, now}},
		} {
			if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
				t.Fatalf("seed tenant authorization principal: %v", err)
			}
		}
	}
}
