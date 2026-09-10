//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

var fixtureGatewayID = uuid.MustParse("01993000-0000-7000-8000-000000000001")
var fixtureGatewayBindingID = uuid.MustParse("01993000-0000-7000-8000-000000000002")

// Controlled owner fixture only; this is not the WR-19/22 bootstrap protocol.
func seedGatewayWorkload(t *testing.T, environment *postgresEnvironment, operations ...string) {
	t.Helper()
	ctx := context.Background()
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'workload','active',1,now(),now())`, []any{fixtureGatewayID}},
		{`INSERT INTO workload_principals(principal_id,owner_type,environment,trust_domain,name,normalized_name,version,created_at,updated_at) VALUES($1,'platform','wr17-18-isolated','iam.wr17-18.test','gateway','gateway',1,now(),now())`, []any{fixtureGatewayID}},
		{`INSERT INTO workload_identity_bindings(id,principal_id,environment,trust_domain,identity_kind,identity_value,status,version,created_at,updated_at) VALUES($1,$2,'wr17-18-isolated','iam.wr17-18.test','x509_dns','ani-gateway','active',1,now(),now())`, []any{fixtureGatewayBindingID, fixtureGatewayID}},
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed controlled Workload fixture: %v", err)
		}
	}
	for _, op := range operations {
		if _, err := tx.Exec(ctx, `INSERT INTO workload_grants(id,principal_id,environment,trust_domain,audience,operation,scope,status,version,created_at,updated_at) VALUES($1,$2,'wr17-18-isolated','iam.wr17-18.test','ani-iam',$3,'iam_ingress','active',1,now(),now())`, mustV7(t), fixtureGatewayID, op); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func mustV7(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertSQLRejected(t *testing.T, pool *pgxpool.Pool, code, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, query, args...)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err == nil {
		t.Fatal("forbidden SQL committed")
	}
	assertPGCode(t, err, code)
}

func TestWorkloadRuntimeFoundationUsesCurrentOwnerTrustAndEnforcesRelations(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	dataset := data.NewData(environment.runtimePool)
	if err := data.ValidateRuntimeFoundation(ctx, dataset); err != nil {
		t.Fatalf("restricted runtime foundation: %v", err)
	}
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	if err := data.ValidateRuntimeFoundation(ctx, data.NewData(owner)); err == nil {
		t.Fatal("owner role accepted as runtime")
	}
	seedPrincipalsAndTenants(t, ctx, environment.runtimePool)
	operation := iamv1.AuthenticationService_PasswordLogin_FullMethodName
	seedGatewayWorkload(t, environment, operation)
	peer := biz.VerifiedWorkloadPeer{Environment: "wr17-18-isolated", TrustDomain: "iam.wr17-18.test", IdentityKind: "x509_dns", IdentityValue: "ani-gateway"}
	authn := biz.NewWorkloadAuthentication(data.NewWorkloadIdentityReader(dataset))
	authz := biz.NewWorkloadAuthorization(data.NewWorkloadGrantReader(dataset))
	identity, err := authn.Authenticate(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	target := biz.WorkloadTarget{Audience: "ani-iam", Operation: operation}
	caller, err := authz.Authorize(ctx, identity, target)
	if err != nil || caller.Identity.PrincipalID != fixtureGatewayID || caller.GrantVersion != 1 {
		t.Fatalf("current direct caller=%#v/%v", caller, err)
	}
	for _, p := range []biz.VerifiedWorkloadPeer{
		{Environment: "other", TrustDomain: peer.TrustDomain, IdentityKind: peer.IdentityKind, IdentityValue: peer.IdentityValue},
		{Environment: peer.Environment, TrustDomain: "other.test", IdentityKind: peer.IdentityKind, IdentityValue: peer.IdentityValue},
		{Environment: peer.Environment, TrustDomain: peer.TrustDomain, IdentityKind: peer.IdentityKind, IdentityValue: "other-service"},
	} {
		if _, err := authn.Authenticate(ctx, p); !errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
			t.Fatalf("foreign identity accepted: %v", err)
		}
	}
	if _, err := authz.Authorize(ctx, identity, biz.WorkloadTarget{Audience: "ani-iam", Operation: iamv1.IAMAdminService_CreateTenantWorkload_FullMethodName}); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatalf("ungranted target=%v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1 WHERE principal_id=$1`, fixtureGatewayID); err != nil {
		t.Fatal(err)
	}
	if _, err := authz.Authorize(ctx, identity, target); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatalf("revoked grant accepted: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1 WHERE principal_id=$1`, fixtureGatewayID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE workload_identity_bindings SET status='revoked',version=version+1 WHERE id=$1`, fixtureGatewayBindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := authn.Authenticate(ctx, peer); !errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
		t.Fatalf("revoked binding accepted: %v", err)
	}
	if _, err := authz.Authorize(ctx, identity, target); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatalf("old resolved binding accepted: %v", err)
	}

	for _, stmt := range []string{
		`UPDATE workload_identity_bindings SET status='active' WHERE principal_id=$1`,
		`UPDATE workload_grants SET status='active' WHERE principal_id=$1`,
		`UPDATE workload_principals SET version=version+1 WHERE principal_id=$1`,
		`UPDATE principals SET status='disabled' WHERE id=$1`,
	} {
		assertSQLRejected(t, environment.runtimePool, "42501", stmt, fixtureGatewayID)
	}
	assertSQLRejected(t, environment.runtimePool, "23514", `UPDATE principals SET principal_type='workload' WHERE id=$1`, actorID)
	assertSQLRejected(t, environment.runtimePool, "23514", `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'workload','active',1,now(),now())`, mustV7(t))
	assertSQLRejected(t, environment.runtimePool, "23514", `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'service','active',1,now(),now())`, mustV7(t))
	assertSQLRejected(t, environment.runtimePool, "23514", `INSERT INTO workload_principals(principal_id,owner_type,tenant_id,name,normalized_name,version,created_at,updated_at) VALUES($1,'tenant',$2,'bad-human','bad-human',1,now(),now())`, actorID, tenantA)
	assertSQLRejected(t, environment.runtimePool, "23514", `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, tenantA, mustV7(t), fixtureGatewayID)
	for _, table := range []string{"verified_emails", "identities", "password_credentials", "sessions"} {
		assertSQLRejected(t, owner, "23514", "INSERT INTO "+table+"(principal_id) VALUES($1)", fixtureGatewayID)
	}

	roleA, roleB := mustV7(t), mustV7(t)
	seedRoles(t, ctx, environment.runtimePool, roleA, roleB)
	workload := biz.NewTenantWorkloadUsecase(data.NewPostgresTenantWorkloadUnitOfWork(dataset), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})
	scopeA, scopeB := mustTenantScope(t, tenantA), mustTenantScope(t, tenantB)
	actor := biz.TenantAuthorizationActor{PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "relations", CorrelationID: "relations", DecisionID: "relations"}
	created, err := workload.CreateTenantWorkload(ctx, scopeA, biz.CreateTenantWorkloadCommand{Name: "Tenant Bot", RoleIDs: []uuid.UUID{roleA}, IdempotencyKey: "relations-create", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	key, err := workload.CreateAPIKey(ctx, scopeA, biz.CreateAPIKeyCommand{PrincipalID: created.Principal.ID, NeverExpires: true, IdempotencyKey: "relations-key", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLRejected(t, environment.runtimePool, "23514", `UPDATE workload_principals SET tenant_id=$2 WHERE principal_id=$1`, created.Principal.ID, tenantB)
	assertSQLRejected(t, environment.runtimePool, "23514", `UPDATE workload_principals SET name='renamed',normalized_name='renamed' WHERE principal_id=$1`, created.Principal.ID)
	assertSQLRejected(t, environment.runtimePool, "23514", `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, tenantB, mustV7(t), created.Principal.ID)
	if _, err := workload.CreateAPIKey(ctx, scopeB, biz.CreateAPIKeyCommand{PrincipalID: created.Principal.ID, NeverExpires: true, IdempotencyKey: "cross-key", Actor: actor}); err == nil {
		t.Fatal("cross-tenant key creation accepted")
	}
	if _, err := workload.RevokeAPIKey(ctx, scopeA, biz.RevokeAPIKeyCommand{KeyID: key.APIKey.ID, IdempotencyKey: "relations-revoke", Actor: actor}); err != nil {
		t.Fatal(err)
	}
	assertSQLRejected(t, environment.runtimePool, "23514", `UPDATE api_keys SET status='active',revoked_at=NULL WHERE tenant_id=$1 AND key_id=$2`, tenantA, key.APIKey.ID)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	memberships := biz.NewTenantAuthorizationUsecase(data.NewPostgresTenantAuthorizationUnitOfWork(dataset), catalog, data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})
	if _, err := memberships.UpdateMembership(ctx, scopeA, biz.UpdateTenantMembershipCommand{MembershipID: created.Principal.MembershipID, Status: biz.MembershipStatusRemoved, ExpectedVersion: 1, IdempotencyKey: "relations-remove", Actor: actor}); err != nil {
		t.Fatal(err)
	}
	assertSQLRejected(t, environment.runtimePool, "23514", `UPDATE tenant_memberships SET status='active' WHERE tenant_id=$1 AND id=$2`, tenantA, created.Principal.MembershipID)
	enabled, err := workload.UpdateTenantWorkload(ctx, scopeA, biz.UpdateTenantWorkloadCommand{PrincipalID: created.Principal.ID, Status: biz.PrincipalStatusActive, ExpectedVersion: 2, IdempotencyKey: "relations-reenable", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Principal.MembershipID == created.Principal.MembershipID || enabled.Principal.MembershipID == uuid.Nil {
		t.Fatal("reenable reused removed Membership")
	}
	var activeBindings, activeKeys int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND membership_id=$2`, tenantA, enabled.Principal.MembershipID).Scan(&activeBindings); err != nil {
		t.Fatal(err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2 AND status='active'`, tenantA, created.Principal.ID).Scan(&activeKeys); err != nil {
		t.Fatal(err)
	}
	if activeBindings != 0 || activeKeys != 0 {
		t.Fatal("reenable restored old authority or credentials")
	}
}
