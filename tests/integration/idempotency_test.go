//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWorkloadMutationDurableIdempotencyAndAtomicFailures(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	seedPrincipalsAndTenants(t, ctx, environment.runtimePool)
	roleA, roleB := mustV7(t), mustV7(t)
	seedRoles(t, ctx, environment.runtimePool, roleA, roleB)
	dataset := data.NewData(environment.runtimePool)
	usecase := biz.NewTenantWorkloadUsecase(data.NewPostgresTenantWorkloadUnitOfWork(dataset), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})
	scope := mustTenantScope(t, tenantA)
	actor := biz.TenantAuthorizationActor{PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "idempotency", CorrelationID: "idempotency", DecisionID: "idempotency"}
	command := biz.CreateTenantWorkloadCommand{Name: "Build Worker", RoleIDs: []uuid.UUID{roleA}, IdempotencyKey: "create-once", Actor: actor}
	created, err := usecase.CreateTenantWorkload(ctx, scope, command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := usecase.CreateTenantWorkload(ctx, scope, command)
	if err != nil || replay.Principal.ID != created.Principal.ID || replay.AuditEventID != created.AuditEventID {
		t.Fatalf("durable create replay failed: %v", err)
	}
	conflicting := command
	conflicting.Name = "Different Worker"
	if _, err := usecase.CreateTenantWorkload(ctx, scope, conflicting); !errors.Is(err, biz.ErrIdempotencyConflict) {
		t.Fatalf("conflicting request=%v", err)
	}
	var count int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM workload_principals WHERE tenant_id=$1`, tenantA).Scan(&count); err != nil || count != 1 {
		t.Fatalf("create-once rows=%d/%v", count, err)
	}

	type result struct {
		created biz.CreateAPIKeyResult
		err     error
	}
	results := make(chan result, 2)
	keyCommand := biz.CreateAPIKeyCommand{PrincipalID: created.Principal.ID, NeverExpires: true, IdempotencyKey: "key-once", Actor: actor}
	for i := 0; i < 2; i++ {
		go func() { r, e := usecase.CreateAPIKey(ctx, scope, keyCommand); results <- result{r, e} }()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent key creation: %v / %v", first.err, second.err)
	}
	if first.created.Replayed {
		first, second = second, first
	}
	if first.created.Secret == "" || first.created.Replayed || second.created.Secret != "" || !second.created.Replayed || first.created.APIKey.ID != second.created.APIKey.ID || first.created.AuditEventID != second.created.AuditEventID {
		t.Fatal("concurrent replay duplicated or re-revealed credential")
	}
	var stored []byte
	if err := environment.runtimePool.QueryRow(ctx, `SELECT result FROM tenant_mutation_results WHERE tenant_id=$1 AND actor_id=$2 AND operation='createIAMAPIKey' AND idempotency_key='key-once'`, tenantA, actorID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(first.created.Secret)) || bytes.Contains(stored, []byte("Digest")) || bytes.Contains(stored, []byte("Secret")) {
		t.Fatal("durable result contains credential material")
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2`, tenantA, created.Principal.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent key rows=%d/%v", count, err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.api-key.created'`, tenantA).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent key audits=%d/%v", count, err)
	}

	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	for _, table := range []string{"iam_audit_events", "tenant_mutation_results"} {
		t.Run("rollback when "+table+" insert fails", func(t *testing.T) {
			if _, err := owner.Exec(ctx, "REVOKE INSERT ON "+table+" FROM ani_iam_runtime"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := owner.Exec(ctx, "GRANT INSERT ON "+table+" TO ani_iam_runtime"); err != nil {
					t.Error(err)
				}
			}()
			failed := command
			failed.Name = "rollback-" + table
			failed.IdempotencyKey = "rollback-" + table
			if _, err := usecase.CreateTenantWorkload(ctx, scope, failed); !errors.Is(err, biz.ErrPersistencePermissionDenied) {
				t.Fatalf("injected insert failure=%v", err)
			}
			if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM workload_principals WHERE tenant_id=$1 AND name=$2`, tenantA, failed.Name).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed transaction leaked Workload=%d/%v", count, err)
			}
			if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM tenant_mutation_results WHERE tenant_id=$1 AND idempotency_key=$2`, tenantA, failed.IdempotencyKey).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed transaction leaked result=%d/%v", count, err)
			}
			if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.tenant-workload.created'`, tenantA).Scan(&count); err != nil || count != 1 {
				t.Fatalf("failed transaction leaked audit=%d/%v", count, err)
			}
		})
	}
	// A fresh process/usecase reads the same durable result; no process cache is required.
	usecase = biz.NewTenantWorkloadUsecase(data.NewPostgresTenantWorkloadUnitOfWork(dataset), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})
	replayedKey, err := usecase.CreateAPIKey(ctx, scope, keyCommand)
	if err != nil || !replayedKey.Replayed || replayedKey.Secret != "" || replayedKey.APIKey.ID != first.created.APIKey.ID {
		t.Fatalf("new usecase replay=%v", err)
	}
	expirationBase := time.Now().UTC().Truncate(time.Second)
	if _, err := owner.Exec(ctx, `UPDATE tenant_mutation_results SET created_at=$3,expires_at=$4 WHERE tenant_id=$1 AND idempotency_key=$2`, tenantA, "key-once", expirationBase.Add(-25*time.Hour), expirationBase.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.CreateAPIKey(ctx, scope, keyCommand); !errors.Is(err, biz.ErrIdempotencyExpired) {
		t.Fatalf("expired key recreated or replayed: %v", err)
	}
}
