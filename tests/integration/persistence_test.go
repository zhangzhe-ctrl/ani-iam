//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

const (
	postgresImage   = "postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c"
	migrationRole   = "ani_iam_migrator"
	provisionerRole = "ani_iam_provisioner"
	runtimeRole     = "ani_iam_runtime"
	primaryDB       = "ani_iam_wr18_a"
	replayDB        = "ani_iam_wr18_b"
)

var (
	tenantA = uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	tenantB = uuid.MustParse("0198f062-b76d-7892-b978-baa342c53020")
	actorID = uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
)

func TestNoRLSPersistenceFoundation(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()

	scopeA := mustTenantScope(t, tenantA)
	scopeB := mustTenantScope(t, tenantB)
	uow := data.NewPostgresUnitOfWork(data.NewData(environment.runtimePool))

	t.Run("empty database migration replay", func(t *testing.T) {
		assertTargetTables(t, ctx, environment.runtimePool)

		replayPool := mustPool(t, environment.runtimeDSN(replayDB, "dp2-04-replay-check"))
		defer replayPool.Close()
		assertTargetTables(t, ctx, replayPool)
	})

	t.Run("migration and runtime roles stay separated", func(t *testing.T) {
		var (
			currentUser     string
			superuser       bool
			bypassRLS       bool
			createRole      bool
			createDB        bool
			temporary       bool
			ownsTables      int
			migrationSuper  bool
			migrationBypass bool
			migrationOwns   int
		)
		err := environment.runtimePool.QueryRow(ctx, `
			SELECT current_user, rolsuper, rolbypassrls, rolcreaterole, rolcreatedb
			FROM pg_roles
			WHERE rolname = current_user
		`).Scan(&currentUser, &superuser, &bypassRLS, &createRole, &createDB)
		if err != nil {
			t.Fatalf("query runtime role attributes: %v", err)
		}
		if currentUser != runtimeRole || superuser || bypassRLS || createRole || createDB {
			t.Fatalf("runtime attributes = user:%s super:%t bypassrls:%t createrole:%t createdb:%t", currentUser, superuser, bypassRLS, createRole, createDB)
		}
		err = environment.runtimePool.QueryRow(ctx, `
				SELECT has_database_privilege(current_user, current_database(), 'TEMPORARY')
			`).Scan(&temporary)
		if err != nil {
			t.Fatalf("query runtime TEMPORARY privilege: %v", err)
		}
		if temporary {
			t.Fatal("runtime role unexpectedly has database TEMPORARY privilege")
		}

		err = environment.runtimePool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_class AS c
			JOIN pg_namespace AS n ON n.oid = c.relnamespace
			JOIN pg_roles AS r ON r.oid = c.relowner
			WHERE n.nspname = 'public' AND c.relkind = 'r' AND r.rolname = current_user
		`).Scan(&ownsTables)
		if err != nil {
			t.Fatalf("query runtime-owned tables: %v", err)
		}
		if ownsTables != 0 {
			t.Fatalf("runtime owns %d target tables, want 0", ownsTables)
		}
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT rolsuper, rolbypassrls
			FROM pg_roles
			WHERE rolname = 'ani_iam_migrator'
		`).Scan(&migrationSuper, &migrationBypass)
		if err != nil {
			t.Fatalf("query migration role attributes: %v", err)
		}
		if migrationSuper || migrationBypass {
			t.Fatalf("migration role attributes = super:%t bypassrls:%t, want false/false", migrationSuper, migrationBypass)
		}
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_class AS c
			JOIN pg_namespace AS n ON n.oid = c.relnamespace
			JOIN pg_roles AS r ON r.oid = c.relowner
			WHERE n.nspname = 'public'
			  AND c.relkind = 'r'
			  AND c.relname IN (
				'principals', 'tenant_access', 'tenant_memberships', 'tenant_roles',
				'tenant_role_bindings', 'iam_audit_events', 'password_action_requests',
				'password_actions', 'notification_outbox', 'password_action_completions',
				'permission_catalog'
			  )
			  AND r.rolname = 'ani_iam_migrator'
		`).Scan(&migrationOwns)
		if err != nil {
			t.Fatalf("query migration-owned tables: %v", err)
		}
		if migrationOwns != 11 {
			t.Fatalf("migration role owns %d target tables, want 11", migrationOwns)
		}

		rows, err := environment.runtimePool.Query(ctx, `
			SELECT table_name, privilege_type
			FROM information_schema.role_table_grants
			WHERE grantee = current_user AND table_schema = 'public'
		`)
		if err != nil {
			t.Fatalf("query runtime table grants: %v", err)
		}
		grants := make(map[string]struct{})
		for rows.Next() {
			var tableName, privilege string
			if err := rows.Scan(&tableName, &privilege); err != nil {
				rows.Close()
				t.Fatalf("scan runtime table grant: %v", err)
			}
			grants[tableName+"/"+privilege] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate runtime table grants: %v", err)
		}
		rows.Close()
		expectedGrants := make(map[string]struct{})
		for _, tableName := range []string{"principals", "tenant_access", "tenant_memberships", "tenant_roles", "tenant_role_bindings"} {
			for _, privilege := range []string{"INSERT", "SELECT", "UPDATE"} {
				expectedGrants[tableName+"/"+privilege] = struct{}{}
			}
		}
		for _, privilege := range []string{"INSERT", "SELECT"} {
			expectedGrants["iam_audit_events/"+privilege] = struct{}{}
		}
		expectedGrants["tenant_mutation_results/SELECT"] = struct{}{}
		expectedGrants["tenant_mutation_results/INSERT"] = struct{}{}
		expectedGrants["tenant_role_bindings/DELETE"] = struct{}{}
		for _, tableName := range []string{"verified_emails", "tenant_lifecycle_projections", "tenant_role_permissions", "workload_identity_bindings", "workload_grants", "iam_schema_revision"} {
			expectedGrants[tableName+"/SELECT"] = struct{}{}
		}
		for _, tableName := range []string{
			"identities", "password_credentials", "sessions", "session_grants",
			"refresh_token_families", "refresh_tokens", "password_action_requests",
			"password_actions", "notification_outbox", "password_action_completions",
			"workload_principals", "api_keys",
		} {
			for _, privilege := range []string{"INSERT", "SELECT", "UPDATE"} {
				expectedGrants[tableName+"/"+privilege] = struct{}{}
			}
		}
		if len(grants) != len(expectedGrants) {
			t.Fatalf("runtime grant count = %d, want %d: %#v", len(grants), len(expectedGrants), grants)
		}
		for expectedGrant := range expectedGrants {
			if _, present := grants[expectedGrant]; !present {
				t.Fatalf("runtime missing target grant %s: %#v", expectedGrant, grants)
			}
		}

		if _, err := environment.runtimePool.Exec(ctx, `CREATE TABLE runtime_must_not_create (id bigint)`); err == nil {
			t.Fatal("runtime role unexpectedly executed DDL")
		} else {
			assertPGCode(t, err, "42501")
		}
		if _, err := environment.runtimePool.Exec(ctx, `CREATE TEMP TABLE runtime_must_not_create_temp (id bigint)`); err == nil {
			t.Fatal("runtime role unexpectedly created a temporary table")
		} else {
			assertPGCode(t, err, "42501")
		}
		if _, err := environment.runtimePool.Exec(ctx, `SET ROLE ani_iam_migrator`); err == nil {
			t.Fatal("runtime role unexpectedly assumed the migration role")
		} else {
			assertPGCode(t, err, "42501")
		}
		if _, err := environment.runtimePool.Exec(ctx, `UPDATE iam_audit_events SET reason = reason`); err == nil {
			t.Fatal("runtime role unexpectedly updated append-only audit rows")
		} else {
			assertPGCode(t, err, "42501")
		}
		if _, err := environment.runtimePool.Exec(ctx, `DELETE FROM iam_audit_events`); err == nil {
			t.Fatal("runtime role unexpectedly deleted append-only audit rows")
		} else {
			assertPGCode(t, err, "42501")
		}
	})

	t.Run("schema has no RLS and tenant-owned relations keep non-null tenant boundaries", func(t *testing.T) {
		var rlsTables int
		err := environment.runtimePool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_class AS c
			JOIN pg_namespace AS n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relname IN ('tenant_access', 'tenant_memberships', 'tenant_roles', 'tenant_role_bindings', 'iam_audit_events')
			  AND (c.relrowsecurity OR c.relforcerowsecurity)
		`).Scan(&rlsTables)
		if err != nil {
			t.Fatalf("query RLS flags: %v", err)
		}
		if rlsTables != 0 {
			t.Fatalf("found %d RLS-enabled target tables, want 0", rlsTables)
		}

		var nullableTenantColumns int
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT count(*)
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name IN ('tenant_access', 'tenant_memberships', 'tenant_roles', 'tenant_role_bindings')
			  AND column_name = 'tenant_id'
			  AND is_nullable <> 'NO'
		`).Scan(&nullableTenantColumns)
		if err != nil {
			t.Fatalf("query tenant_id nullability: %v", err)
		}
		if nullableTenantColumns != 0 {
			t.Fatalf("found %d nullable tenant_id columns, want 0", nullableTenantColumns)
		}

		var speculativeRoleStatusColumns int
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT count(*)
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name IN ('tenant_roles', 'tenant_role_bindings')
			  AND column_name = 'status'
		`).Scan(&speculativeRoleStatusColumns)
		if err != nil {
			t.Fatalf("query role lifecycle columns: %v", err)
		}
		if speculativeRoleStatusColumns != 0 {
			t.Fatalf("found %d role lifecycle columns without a frozen domain contract, want 0", speculativeRoleStatusColumns)
		}

		if _, err := environment.runtimePool.Exec(ctx, `
			INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at)
			VALUES ('00000000-0000-0000-0000-000000000000', 'active', 1, now(), now())
		`); err == nil {
			t.Fatal("all-zero Tenant ID unexpectedly passed a database constraint")
		} else {
			assertPGCode(t, err, "23514")
		}
	})

	t.Run("security audit schema requires complete identities", func(t *testing.T) {
		var requiredColumns int
		err := environment.runtimePool.QueryRow(ctx, `
				SELECT count(*)
				FROM information_schema.columns
				WHERE table_schema = 'public'
				  AND table_name = 'iam_audit_events'
				  AND is_nullable = 'NO'
				  AND column_name IN (
					'tenant_id', 'event_id', 'actor_id', 'authentication_method',
					'boundary', 'action', 'target_type', 'target_id', 'target_version',
					'result', 'reason', 'request_id', 'correlation_id', 'decision_id',
					'source_service', 'occurred_at', 'recorded_at'
				  )
			`).Scan(&requiredColumns)
		if err != nil {
			t.Fatalf("query required audit columns: %v", err)
		}
		if requiredColumns != 15 {
			t.Fatalf("required non-null audit columns = %d, want 15", requiredColumns)
		}

		var identityConstraints int
		err = environment.runtimePool.QueryRow(ctx, `
				SELECT count(*)
				FROM pg_constraint AS constraint_definition
				JOIN pg_class AS target_table ON target_table.oid = constraint_definition.conrelid
				JOIN pg_namespace AS target_schema ON target_schema.oid = target_table.relnamespace
				WHERE target_schema.nspname = 'public'
				  AND target_table.relname = 'iam_audit_events'
				  AND constraint_definition.conname IN (
					'iam_audit_events_authentication_method_allowed',
					'iam_audit_events_boundary_scope',
					'iam_audit_events_actor_scope',
					'iam_audit_events_request_required',
					'iam_audit_events_correlation_required',
					'iam_audit_events_decision_required',
					'iam_audit_events_source_required'
				  )
			`).Scan(&identityConstraints)
		if err != nil {
			t.Fatalf("query audit identity constraints: %v", err)
		}
		if identityConstraints != 7 {
			t.Fatalf("audit identity constraints = %d, want 7", identityConstraints)
		}
	})

	seedPrincipalsAndTenants(t, ctx, environment.runtimePool)
	roleA := uuid.MustParse("0198f062-b76d-7001-9000-000000000001")
	roleB := uuid.MustParse("0198f062-b76d-7001-9000-000000000002")
	seedRoles(t, ctx, environment.runtimePool, roleA, roleB)

	var committedMembership biz.TenantMembership
	t.Run("runtime DML and mutation plus audit commit atomically", func(t *testing.T) {
		membershipID := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01")
		auditID := uuid.MustParse("0198f062-b76d-7653-8d33-55e7ddf4094b")
		usecase := biz.NewMembershipUsecase(
			uow,
			&fixedIDGenerator{ids: []uuid.UUID{membershipID, auditID}},
			fixedClock{now: time.Date(2026, 9, 4, 4, 10, 0, 0, time.UTC)},
		)
		result, err := usecase.Create(ctx, scopeA, biz.CreateMembershipCommand{
			PrincipalID:          actorID,
			ActorID:              actorID,
			AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
			RequestID:            "req-dp2-04-commit",
			CorrelationID:        "corr-dp2-04-commit",
			DecisionID:           "decision-dp2-04-commit",
			Reason:               biz.AuditReasonTenantBootstrap,
		})
		if err != nil {
			t.Fatalf("Create() through runtime role: %v", err)
		}
		committedMembership = result.Membership

		var auditCount int
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT count(*) FROM iam_audit_events
			WHERE tenant_id = $1 AND event_id = $2 AND target_id = $3
		`, tenantA, auditID, membershipID).Scan(&auditCount)
		if err != nil {
			t.Fatalf("query committed audit: %v", err)
		}
		if auditCount != 1 {
			t.Fatalf("committed audit count = %d, want 1", auditCount)
		}
	})

	t.Run("Tenant B cannot read or mutate Tenant A", func(t *testing.T) {
		var got biz.TenantMembership
		err := uow.WithinTenant(ctx, scopeA, func(txContext context.Context, tx biz.TenantTransaction) error {
			var getErr error
			got, getErr = tx.Memberships().Get(txContext, scopeA, committedMembership.ID)
			return getErr
		})
		if err != nil {
			t.Fatalf("Tenant A Get() error = %v", err)
		}
		if got.ID != committedMembership.ID {
			t.Fatalf("Tenant A Get() ID = %s, want %s", got.ID, committedMembership.ID)
		}

		err = uow.WithinTenant(ctx, scopeB, func(txContext context.Context, tx biz.TenantTransaction) error {
			_, getErr := tx.Memberships().Get(txContext, scopeB, committedMembership.ID)
			return getErr
		})
		if !errors.Is(err, biz.ErrMembershipNotFound) {
			t.Fatalf("Tenant B Get() error = %v, want %v", err, biz.ErrMembershipNotFound)
		}

		err = uow.WithinTenant(ctx, scopeB, func(txContext context.Context, tx biz.TenantTransaction) error {
			_, updateErr := tx.Memberships().UpdateStatus(
				txContext,
				scopeB,
				committedMembership.ID,
				biz.MembershipStatusSuspended,
				committedMembership.Version,
				time.Now().UTC(),
			)
			return updateErr
		})
		if !errors.Is(err, biz.ErrVersionConflict) {
			t.Fatalf("Tenant B UpdateStatus() error = %v, want %v", err, biz.ErrVersionConflict)
		}
	})

	t.Run("transaction rejects a different TenantScope", func(t *testing.T) {
		err := uow.WithinTenant(ctx, scopeA, func(txContext context.Context, tx biz.TenantTransaction) error {
			_, getErr := tx.Memberships().Get(txContext, scopeB, committedMembership.ID)
			return getErr
		})
		if !errors.Is(err, biz.ErrTenantScopeMismatch) {
			t.Fatalf("scope switch error = %v, want %v", err, biz.ErrTenantScopeMismatch)
		}
	})

	t.Run("composite foreign keys reject cross-Tenant relations", func(t *testing.T) {
		bindingID := uuid.MustParse("0198f062-b76d-7002-9000-000000000001")
		_, err := environment.runtimePool.Exec(ctx, `
				INSERT INTO tenant_role_bindings (
					tenant_id, id, membership_id, role_id, version,
					created_at, updated_at
				) VALUES ($1, $2, $3, $4, 1, now(), now())
			`, tenantA, bindingID, committedMembership.ID, roleB)
		if err == nil {
			t.Fatal("cross-Tenant role binding unexpectedly committed")
		}
		assertPGCode(t, err, "23503")
		assertBindingAbsent(t, ctx, environment.runtimePool, tenantA, bindingID)
	})

	t.Run("commit failure is returned and leaves no partial transaction", func(t *testing.T) {
		auditID := uuid.MustParse("0198f062-b76d-7653-8d33-55e7ddf4094d")
		missingActorID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc09999")
		err := uow.WithinTenant(ctx, scopeA, func(txContext context.Context, tx biz.TenantTransaction) error {
			return tx.AuditEvents().Append(txContext, scopeA, biz.SecurityAuditEvent{
				ID:                   auditID,
				ActorID:              missingActorID,
				AuthenticationMethod: biz.AuditAuthenticationMethodInternal,
				Boundary:             biz.AuditBoundaryTenant,
				Action:               biz.AuditActionMembershipCreated,
				TargetType:           biz.AuditTargetTypeTenantMembership,
				TargetID:             actorID,
				TargetVersion:        1,
				Result:               biz.AuditResultSucceeded,
				Reason:               biz.AuditReasonTenantBootstrap,
				RequestID:            "req-dp2-04-commit-failure",
				CorrelationID:        "corr-dp2-04-commit-failure",
				DecisionID:           "decision-dp2-04-commit-failure",
				SourceService:        biz.AuditSourceServiceIAM,
				OccurredAt:           time.Now().UTC(),
				RecordedAt:           time.Now().UTC(),
			})
		})
		if err == nil {
			t.Fatal("deferred foreign-key failure unexpectedly committed")
		}
		if !errors.Is(err, biz.ErrInvalidPersistenceState) {
			t.Fatalf("commit error = %v, want %v", err, biz.ErrInvalidPersistenceState)
		}
		var leakedDriverError *pgconn.PgError
		if errors.As(err, &leakedDriverError) {
			t.Fatalf("commit error leaked pgx/pgconn type: %v", leakedDriverError)
		}
		assertAuditAbsent(t, ctx, environment.runtimePool, tenantA, auditID)
	})

	t.Run("required audit failure rolls mutation back", func(t *testing.T) {
		membershipID := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f02")
		duplicateAuditID := uuid.MustParse("0198f062-b76d-7653-8d33-55e7ddf4094c")
		_, err := environment.runtimePool.Exec(ctx, `
			INSERT INTO iam_audit_events (
				tenant_id, event_id, actor_id, authentication_method, boundary,
				action, target_type, target_id, target_version, result, reason,
				request_id, correlation_id, decision_id, source_service,
				occurred_at, recorded_at
			) VALUES ($1, $2, $3, 'internal', 'tenant', 'iam.fixture.created',
				'fixture', $4, 1, 'succeeded', 'FIXTURE', 'req-fixture',
				'corr-fixture', 'decision-fixture', 'iam-service', now(), now())
		`, tenantA, duplicateAuditID, actorID, actorID)
		if err != nil {
			t.Fatalf("seed duplicate audit identity: %v", err)
		}

		usecase := biz.NewMembershipUsecase(
			uow,
			&fixedIDGenerator{ids: []uuid.UUID{membershipID, duplicateAuditID}},
			fixedClock{now: time.Date(2026, 9, 4, 4, 20, 0, 0, time.UTC)},
		)
		_, err = usecase.Create(ctx, scopeA, biz.CreateMembershipCommand{
			PrincipalID:          actorID,
			ActorID:              actorID,
			AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
			RequestID:            "req-dp2-04-rollback",
			CorrelationID:        "corr-dp2-04-rollback",
			DecisionID:           "decision-dp2-04-rollback",
			Reason:               biz.AuditReasonTenantBootstrap,
		})
		if err == nil {
			t.Fatal("Create() unexpectedly succeeded when required audit insert failed")
		}

		assertMembershipAbsent(t, ctx, environment.runtimePool, tenantA, membershipID)
	})

	t.Run("optimistic version permits one concurrent mutation", func(t *testing.T) {
		statuses := []biz.MembershipStatus{biz.MembershipStatusSuspended, biz.MembershipStatusRemoved}
		errorsByAttempt := make(chan error, len(statuses))
		start := make(chan struct{})
		var ready sync.WaitGroup
		ready.Add(len(statuses))
		for _, status := range statuses {
			status := status
			go func() {
				ready.Done()
				<-start
				errorsByAttempt <- uow.WithinTenant(ctx, scopeA, func(txContext context.Context, tx biz.TenantTransaction) error {
					_, err := tx.Memberships().UpdateStatus(
						txContext,
						scopeA,
						committedMembership.ID,
						status,
						committedMembership.Version,
						time.Now().UTC(),
					)
					return err
				})
			}()
		}
		ready.Wait()
		close(start)

		var successes, conflicts int
		for range statuses {
			err := <-errorsByAttempt
			switch {
			case err == nil:
				successes++
			case errors.Is(err, biz.ErrVersionConflict):
				conflicts++
			default:
				t.Fatalf("concurrent UpdateStatus() error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent results = %d success/%d conflict, want 1/1", successes, conflicts)
		}
	})

	t.Run("tenant predicate mutation exposes the negative control", func(t *testing.T) {
		var leakedTenant uuid.UUID
		err := environment.runtimePool.QueryRow(ctx, `
			SELECT tenant_id
			FROM tenant_memberships
			WHERE $1::uuid IS NOT NULL AND id = $2
		`, tenantB, committedMembership.ID).Scan(&leakedTenant)
		if err != nil {
			t.Fatalf("execute deliberate query mutant: %v", err)
		}
		if leakedTenant != tenantA {
			t.Fatalf("query mutant returned Tenant %s, want leaked Tenant A %s", leakedTenant, tenantA)
		}
	})
}

type postgresEnvironment struct {
	runtimePool     *pgxpool.Pool
	host            string
	migrationPass   string
	provisionerPass string
	runtimePass     string
}

func newPostgresEnvironment(t *testing.T) *postgresEnvironment {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	atlasBinary := os.Getenv("DP2_ATLAS_BIN")
	if atlasBinary == "" {
		t.Fatal("DP2_ATLAS_BIN must name the pinned Atlas Community binary")
	}
	if _, err := os.Stat(atlasBinary); err != nil {
		t.Fatalf("stat DP2_ATLAS_BIN: %v", err)
	}
	repositoryRoot := findRepositoryRoot(t)

	superPassword := randomPassword(t)
	container, err := postgres.Run(
		ctx,
		postgresImage,
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword(superPassword),
		postgres.BasicWaitStrategies(),
		isolatedContainer(t, "postgres", "5432/tcp"),
	)
	if err != nil {
		t.Fatalf("start pinned PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		terminateContext, terminateCancel := context.WithTimeout(context.Background(), time.Minute)
		defer terminateCancel()
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(terminateContext)); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("PostgreSQL superuser connection string: %v", err)
	}
	parsed, err := url.Parse(connectionString)
	if err != nil {
		t.Fatalf("parse PostgreSQL connection string: %v", err)
	}

	migrationPassword := randomPassword(t)
	provisionerPassword := randomPassword(t)
	runtimePassword := randomPassword(t)
	recordFixtureSecrets(t, map[string]string{"provisioner-db": provisionerPassword, "runtime-db": runtimePassword, "migration-db": migrationPassword})
	super, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect isolated PostgreSQL bootstrap superuser: %v", err)
	}
	defer super.Close(ctx)

	bootstrap := []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS", migrationRole, migrationPassword),
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS", runtimeRole, runtimePassword),
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS", provisionerRole, provisionerPassword),
		fmt.Sprintf("CREATE DATABASE %s OWNER %s", primaryDB, migrationRole),
		fmt.Sprintf("CREATE DATABASE %s OWNER %s", replayDB, migrationRole),
	}
	for _, statement := range bootstrap {
		if _, err := super.Exec(ctx, statement); err != nil {
			t.Fatalf("bootstrap isolated role/database: %v", err)
		}
	}

	environment := &postgresEnvironment{
		host:            normalizeProcessE2ELoopbackAddress(t, parsed.Host),
		migrationPass:   migrationPassword,
		provisionerPass: provisionerPassword,
		runtimePass:     runtimePassword,
	}
	for _, database := range []string{primaryDB, replayDB} {
		applyAtlasMigrations(t, ctx, atlasBinary, repositoryRoot, environment.migrationDSN(database))
	}

	environment.runtimePool = mustPool(t, environment.runtimeDSN(primaryDB, "ani-iam-dp2-04-integration"))
	t.Cleanup(environment.runtimePool.Close)
	return environment
}

func (e *postgresEnvironment) migrationDSN(database string) string {
	return postgresDSN(migrationRole, e.migrationPass, e.host, database, "ani-iam-dp2-04-atlas")
}

func (e *postgresEnvironment) runtimeDSN(database, applicationName string) string {
	return postgresDSN(runtimeRole, e.runtimePass, e.host, database, applicationName)
}

func postgresDSN(username, password, host, database, applicationName string) string {
	connection := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   host,
		Path:   "/" + database,
	}
	query := connection.Query()
	query.Set("sslmode", "disable")
	query.Set("application_name", applicationName)
	connection.RawQuery = query.Encode()
	return connection.String()
}

func applyAtlasMigrations(t *testing.T, ctx context.Context, atlasBinary, repositoryRoot, databaseURL string) {
	t.Helper()
	command := exec.CommandContext(
		ctx,
		atlasBinary,
		"migrate", "apply",
		"--dir", "file://"+filepath.Join(repositoryRoot, "migrations"),
		"--url", databaseURL,
	)
	command.Env = append(os.Environ(), "ATLAS_NO_UPDATE_NOTIFIER=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Atlas empty database replay failed: %v\n%s", err, output)
	}
}

func seedPrincipalsAndTenants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO principals (id, principal_type, status, version, created_at, updated_at)
		VALUES ($1, 'human', 'active', 1, now(), now())
	`, actorID)
	if err != nil {
		t.Fatalf("seed Principal with runtime role: %v", err)
	}
	for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
		_, err := pool.Exec(ctx, `
			INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at)
			VALUES ($1, 'active', 1, now(), now())
		`, tenantID)
		if err != nil {
			t.Fatalf("seed Tenant Access %s with runtime role: %v", tenantID, err)
		}
	}
}

func seedRoles(t *testing.T, ctx context.Context, pool *pgxpool.Pool, roleA, roleB uuid.UUID) {
	t.Helper()
	fixtures := []struct {
		tenantID uuid.UUID
		roleID   uuid.UUID
		code     string
	}{
		{tenantID: tenantA, roleID: roleA, code: "tenant-a-role"},
		{tenantID: tenantB, roleID: roleB, code: "tenant-b-role"},
	}
	for _, fixture := range fixtures {
		_, err := pool.Exec(ctx, `
			INSERT INTO tenant_roles (
				tenant_id, id, code, system_role, system_definition_version,
				version, created_at, updated_at
			) VALUES ($1, $2, $3, false, 1, 1, now(), now())
		`, fixture.tenantID, fixture.roleID, fixture.code)
		if err != nil {
			t.Fatalf("seed role %s with runtime role: %v", fixture.roleID, err)
		}
	}
}

func assertTargetTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN (
			'principals', 'tenant_access', 'tenant_memberships', 'tenant_roles',
			'tenant_role_bindings', 'iam_audit_events'
		  )
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query migrated tables: %v", err)
	}
	if count != 6 {
		t.Fatalf("migrated target table count = %d, want 6", count)
	}
}

func assertBindingAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, bindingID uuid.UUID) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM tenant_role_bindings WHERE tenant_id = $1 AND id = $2
	`, tenantID, bindingID).Scan(&count)
	if err != nil {
		t.Fatalf("query role binding after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("role binding count after rollback = %d, want 0", count)
	}
}

func assertMembershipAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, membershipID uuid.UUID) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM tenant_memberships WHERE tenant_id = $1 AND id = $2
	`, tenantID, membershipID).Scan(&count)
	if err != nil {
		t.Fatalf("query membership after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("membership count after rollback = %d, want 0", count)
	}
}

func assertAuditAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, auditID uuid.UUID) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM iam_audit_events WHERE tenant_id = $1 AND event_id = $2
	`, tenantID, auditID).Scan(&count)
	if err != nil {
		t.Fatalf("query audit event after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("audit event count after rollback = %d, want 0", count)
	}
}

func assertPGCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		t.Fatalf("error = %v, want PostgreSQL code %s", err, code)
	}
	if pgError.Code != code {
		t.Fatalf("PostgreSQL code = %s, want %s (error: %v)", pgError.Code, code, err)
	}
}

func mustPool(t *testing.T, connectionString string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), connectionString)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping PostgreSQL pool: %v", err)
	}
	return pool
}

func mustTenantScope(t *testing.T, tenantID uuid.UUID) biz.TenantScope {
	t.Helper()
	scope, err := biz.NewTenantScope(tenantID)
	if err != nil {
		t.Fatalf("NewTenantScope(%s): %v", tenantID, err)
	}
	return scope
}

func randomPassword(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate isolated PostgreSQL password: %v", err)
	}
	return hex.EncodeToString(buffer)
}

func findRepositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get integration test working directory: %v", err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot, "go.mod")); err != nil {
		t.Fatalf("verify repository root: %v", err)
	}
	return repositoryRoot
}

type fixedIDGenerator struct {
	ids []uuid.UUID
}

func (g *fixedIDGenerator) NewID() (uuid.UUID, error) {
	if len(g.ids) == 0 {
		return uuid.Nil, errors.New("no fixed ID available")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}
