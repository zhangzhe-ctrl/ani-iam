//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"strings"
	"testing"
)

func changedWR32Registry(t *testing.T, source *workloadregistry.Registry, change func(*workloadregistry.Document)) *workloadregistry.Registry {
	t.Helper()
	doc := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: source.Targets()}
	change(&doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	r, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWR32RegistryRestrictedStorageAndLiveTargetVersion(t *testing.T) {
	env := newPostgresEnvironment(t)
	ctx := context.Background()
	r := wr32Registry(t)
	owner := mustPool(t, env.migrationDSN(primaryDB))
	defer owner.Close()
	provisioner := mustPool(t, postgresDSN(provisionerRole, env.provisionerPass, env.host, primaryDB, "wr32-registry-denial"))
	defer provisioner.Close()
	dataset := data.NewData(env.runtimePool)
	installer := data.NewData(owner)
	if err := data.ValidateRuntimeFoundation(ctx, dataset); err != nil {
		t.Fatal(err)
	}
	if err := data.ValidateWorkloadRegistry(ctx, dataset, r); err != nil {
		t.Fatal(err)
	}
	var principals, grants, installs int
	if err := owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM principals),(SELECT count(*) FROM workload_grants),(SELECT count(*) FROM workload_registry_installations)`).Scan(&principals, &grants, &installs); err != nil {
		t.Fatal(err)
	}
	if principals != 0 || grants != 0 || installs != 1 {
		t.Fatal("registry installation created authority or lost its audit record")
	}
	for _, unauthorized := range []*data.Data{dataset, data.NewData(provisioner)} {
		if err := data.InstallWorkloadRegistry(ctx, unauthorized, r, r.Digest()); !errors.Is(err, biz.ErrWorkloadBootstrapDenied) {
			t.Fatalf("unauthorized installer: %v", err)
		}
	}
	for _, query := range []string{`UPDATE workload_target_registrations SET enabled=false`, `DELETE FROM workload_registry_installations`, `INSERT INTO workload_registry_installations(id,installed_sha256,installed_by,installed_at) VALUES(gen_random_uuid(),decode(repeat('00',32),'hex'),'ani_iam_runtime',now())`} {
		assertSQLRejected(t, env.runtimePool, "42501", query)
	}
	if err := data.InstallWorkloadRegistry(ctx, installer, r, r.Digest()); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM workload_registry_installations`).Scan(&installs); err != nil || installs != 1 {
		t.Fatal("idempotent install appended a duplicate")
	}
	op := "/iam.v1.AuthenticationService/PasswordLogin"
	seedGatewayWorkload(t, env, op)
	peer := biz.VerifiedWorkloadPeer{Environment: "wr17-18-isolated", TrustDomain: "iam.wr17-18.test", IdentityKind: "x509_dns", IdentityValue: "ani-gateway"}
	identity, err := biz.NewWorkloadAuthentication(data.NewWorkloadIdentityReader(dataset)).Authenticate(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	target := biz.WorkloadTarget{Audience: "ani-iam", Operation: op}
	auth := biz.NewWorkloadAuthorization(data.NewWorkloadGrantReader(dataset, r), r)
	if _, err = auth.Authorize(ctx, identity, target); err != nil {
		t.Fatal(err)
	}
	disabled := changedWR32Registry(t, r, func(doc *workloadregistry.Document) {
		for i := range doc.Targets {
			if doc.Targets[i].Audience == target.Audience && doc.Targets[i].Operation == target.Operation {
				doc.Targets[i].Enabled = false
			}
		}
	})
	if err = data.InstallWorkloadRegistry(ctx, installer, disabled, strings.Repeat("a", 64)); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
		t.Fatal("wrong predecessor accepted")
	}
	if err = data.ValidateWorkloadRegistry(ctx, dataset, r); err != nil {
		t.Fatal("failed install changed registry")
	}
	if _, err = owner.Exec(ctx, `CREATE FUNCTION wr32_install_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'WR32 audit failure'; END $$; CREATE TRIGGER wr32_install_fault BEFORE INSERT ON workload_registry_installations FOR EACH ROW EXECUTE FUNCTION wr32_install_fault()`); err != nil {
		t.Fatal(err)
	}
	if err = data.InstallWorkloadRegistry(ctx, installer, disabled, r.Digest()); !errors.Is(err, biz.ErrPersistenceUnavailable) {
		t.Fatal("audit failure committed registration")
	}
	if err = data.ValidateWorkloadRegistry(ctx, dataset, r); err != nil {
		t.Fatal("audit failure left partial registry")
	}
	if _, err = owner.Exec(ctx, `DROP TRIGGER wr32_install_fault ON workload_registry_installations; DROP FUNCTION wr32_install_fault()`); err != nil {
		t.Fatal(err)
	}
	if err = data.InstallWorkloadRegistry(ctx, installer, disabled, r.Digest()); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Authorize(ctx, identity, target); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatal("stale process admitted disabled target")
	}
	if err = data.ValidateWorkloadRegistry(ctx, dataset, r); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
		t.Fatal("stale startup registry accepted")
	}
	if err = data.InstallWorkloadRegistry(ctx, installer, r, disabled.Digest()); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Authorize(ctx, identity, target); err != nil {
		t.Fatal("reviewed rollback did not restore current authority")
	}
	assertSQLRejected(t, owner, "23503", `UPDATE workload_grants SET audience='unregistered-owner' WHERE principal_id=$1`, fixtureGatewayID)
	assertSQLRejected(t, owner, "23503", `UPDATE workload_grants SET scope='wrong_scope' WHERE principal_id=$1`, fixtureGatewayID)
	omitted := changedWR32Registry(t, r, func(doc *workloadregistry.Document) {
		for i, x := range doc.Targets {
			if x.Audience == target.Audience && x.Operation == target.Operation {
				doc.Targets = append(doc.Targets[:i], doc.Targets[i+1:]...)
				break
			}
		}
	})
	if err = data.InstallWorkloadRegistry(ctx, installer, omitted, r.Digest()); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
		t.Fatal("existing granted target silently removed")
	}
	if err = data.ValidateWorkloadRegistry(ctx, dataset, r); err != nil {
		t.Fatal(err)
	}
}

// An upgrade must preserve populated historical authority, fail closed before
// reviewed installation, and retain identity, status and version afterwards.
func TestWR32MigrationPreservesExistingWorkloadAuthority(t *testing.T) {
	ctx := context.Background()
	var principal, binding, grant string
	var before string
	newPostgresEnvironment(t, func(t *testing.T, pool *pgxpool.Pool, phase string) {
		if phase == "before" {
			principal, binding, grant = mustV7(t).String(), mustV7(t).String(), mustV7(t).String()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'workload','active',3,now(),now())`, []any{principal}},
				{`INSERT INTO workload_principals(principal_id,owner_type,environment,trust_domain,name,normalized_name,version,created_at,updated_at) VALUES($1,'platform','wr32-upgrade','wr32.test','upgrade-caller','upgrade-caller',2,now(),now())`, []any{principal}},
				{`INSERT INTO workload_identity_bindings(id,principal_id,environment,trust_domain,identity_kind,identity_value,status,version,created_at,updated_at) VALUES($1,$2,'wr32-upgrade','wr32.test','x509_dns','upgrade.wr32.test','active',4,now(),now())`, []any{binding, principal}},
				{`INSERT INTO workload_grants(id,principal_id,environment,trust_domain,audience,operation,scope,status,version,created_at,updated_at) VALUES($1,$2,'wr32-upgrade','wr32.test','ani-session-gateway','session.create','delegated_session','active',7,now(),now())`, []any{grant, principal}},
			} {
				if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('principal',(SELECT to_jsonb(p) FROM principals p WHERE id=$1),'workload',(SELECT to_jsonb(w) FROM workload_principals w WHERE principal_id=$1),'binding',(SELECT to_jsonb(b) FROM workload_identity_bindings b WHERE id=$2),'grant',(SELECT to_jsonb(g) FROM workload_grants g WHERE id=$3))::text`, principal, binding, grant).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		if phase == "before" {
			before = snapshot
			return
		}
		if snapshot != before {
			t.Fatal("migration or registry installation changed existing authority")
		}
		var enabled bool
		var digest, revision string
		if err := pool.QueryRow(ctx, `SELECT enabled,encode(bundle_sha256,'hex'),encode(target_sha256,'hex') FROM workload_target_registrations WHERE audience='ani-session-gateway' AND operation='session.create'`).Scan(&enabled, &digest, &revision); err != nil {
			t.Fatal(err)
		}
		if phase == "migrated" {
			if enabled || digest != strings.Repeat("0", 64) || revision != strings.Repeat("0", 64) {
				t.Fatal("unreviewed migrated target was enabled")
			}
		} else if !enabled || digest != wr32Registry(t).Digest() || revision != wr32Registry(t).Revision("ani-session-gateway", "session.create") {
			t.Fatal("reviewed install did not replace the exact placeholder")
		}
	})
}
