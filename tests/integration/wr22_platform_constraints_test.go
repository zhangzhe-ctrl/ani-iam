//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR22PlatformPersistenceBoundaries(t *testing.T) {
	env := newPostgresEnvironment(t)
	ctx := context.Background()
	pool := env.runtimePool
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal("isolated Platform persistence setup failed")
		}
	}
	humans := [2]uuid.UUID{mustV7(t), mustV7(t)}
	members := [2]uuid.UUID{mustV7(t), mustV7(t)}
	sessions := [2]uuid.UUID{mustV7(t), mustV7(t)}
	grants := [2]uuid.UUID{mustV7(t), mustV7(t)}
	families := [2]uuid.UUID{mustV7(t), mustV7(t)}
	tokens := [2]uuid.UUID{mustV7(t), mustV7(t)}
	for i := range 2 {
		exec(`INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, humans[i])
		exec(`INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, members[i], humans[i])
		exec(`INSERT INTO sessions(id,principal_id,audience,status,device_name,idle_expires_at,absolute_expires_at,version,created_at,updated_at,reauthenticated_at,authn_methods) VALUES($1,$2,'boss','active','WR22',now()+interval '30 minutes',now()+interval '8 hours',1,now(),now(),now(),ARRAY['password'])`, sessions[i], humans[i])
		exec(`INSERT INTO platform_session_grants(id,session_id,principal_id,membership_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'active',1,now(),now())`, grants[i], sessions[i], humans[i], members[i])
		exec(`INSERT INTO platform_refresh_token_families(id,grant_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, families[i], grants[i])
		digest := sha256.Sum256(tokens[i][:])
		exec(`INSERT INTO platform_refresh_tokens(id,family_id,digest,status,issued_at,expires_at) VALUES($1,$2,$3,'active',now(),now()+interval '8 hours')`, tokens[i], families[i], digest[:])
	}
	t.Run("runtime_schema_and_privilege_gate", func(t *testing.T) {
		if data.ValidateRuntimeFoundation(ctx, data.NewData(pool)) != nil {
			t.Fatal("new exact Platform schema gate failed")
		}
	})
	t.Run("membership_human_only_and_no_revival", func(t *testing.T) {
		seedGatewayWorkload(t, env)
		assertSQLRejected(t, pool, "23514", `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, mustV7(t), fixtureGatewayID)
		exec(`UPDATE platform_memberships SET status='removed',version=version+1 WHERE id=$1`, members[1])
		assertSQLRejected(t, pool, "23514", `UPDATE platform_memberships SET status='active' WHERE id=$1`, members[1])
		assertSQLRejected(t, pool, "23514", `UPDATE platform_memberships SET principal_id=$2 WHERE id=$1`, members[0], humans[1])
	})
	t.Run("grant_preserves_session_membership_owner_and_boss", func(t *testing.T) {
		// Use revoked status to isolate the owner FK from the active-session index.
		assertSQLRejected(t, pool, "23503", `INSERT INTO platform_session_grants(id,session_id,principal_id,membership_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'revoked',1,now(),now())`, mustV7(t), sessions[0], humans[0], members[1])
		console := mustV7(t)
		exec(`INSERT INTO sessions(id,principal_id,audience,status,device_name,idle_expires_at,absolute_expires_at,version,created_at,updated_at,reauthenticated_at,authn_methods) VALUES($1,$2,'console','active','WR22',now()+interval '1 hour',now()+interval '2 hours',1,now(),now(),now(),ARRAY['password'])`, console, humans[0])
		assertSQLRejected(t, pool, "23503", `INSERT INTO platform_session_grants(id,session_id,principal_id,membership_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'active',1,now(),now())`, mustV7(t), console, humans[0], members[0])
		assertSQLRejected(t, pool, "23514", `UPDATE platform_session_grants SET session_id=$2 WHERE id=$1`, grants[0], sessions[1])
	})
	t.Run("refresh_replacement_stays_in_family", func(t *testing.T) {
		assertSQLRejected(t, pool, "23503", `UPDATE platform_refresh_tokens SET status='consumed',consumed_at=now(),replaced_by=$2 WHERE id=$1`, tokens[0], tokens[1])
		assertSQLRejected(t, pool, "23514", `UPDATE platform_refresh_tokens SET family_id=$2 WHERE id=$1`, tokens[0], families[1])
		assertSQLRejected(t, pool, "23505", `INSERT INTO platform_refresh_token_families(id,grant_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, mustV7(t), grants[0])
	})
	t.Run("permissions_are_platform_catalog_relations", func(t *testing.T) {
		role := mustV7(t)
		exec(`INSERT INTO platform_roles(id,code,display_name,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,'wr22-test','WR22',false,1,1,now(),now())`, role)
		exec(`INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.platform-roles','read',now())`, role)
		assertSQLRejected(t, pool, "23514", `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'tenant','iam.roles','read',now())`, role)
		assertSQLRejected(t, pool, "23503", `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','not-catalogued','read',now())`, role)
		assertSQLRejected(t, pool, "42501", `INSERT INTO permission_catalog(scope,resource,action) VALUES('platform','invented','read')`)
	})
}
