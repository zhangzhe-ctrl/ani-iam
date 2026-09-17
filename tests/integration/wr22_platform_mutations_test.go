//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"sync"
	"testing"
)

func TestWR22FormalPlatformRoleAndAccessMutations(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	browser := b.browser(t)
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("state differs")
	}
	status, body, _ := b.callback(t, browser, code, state)
	if status != 303 {
		t.Fatal("BOSS login prerequisite")
	}
	status, body, _ = b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS refresh prerequisite")
	}
	token, _ := body["access_token"].(string)
	call := func(t *testing.T, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		status, body, _ := b.request(t, browser, method, path, token, payload, headers)
		if status != want {
			t.Fatalf("%s %s status=%d reason=%v want=%d", method, path, status, body["code"], want)
		}
		return body
	}
	const roles = "/iam/platform/roles"
	read := []string{"iam.platform-memberships/read"}
	create := map[string]any{"name": "WR22 readers", "permissions": read}
	key := uuid.NewString()
	role := ""
	var builtin string
	if b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin' AND system_role`).Scan(&builtin) != nil {
		t.Fatal("first admin role missing")
	}
	t.Run("concurrent_formal_create_has_one_role_and_receipt", func(t *testing.T) {
		var wg sync.WaitGroup
		out := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v := call(t, "POST", roles, key, create, 201)
				id, _ := v["role_id"].(string)
				out <- id
			}()
		}
		wg.Wait()
		close(out)
		for id := range out {
			if id == "" {
				t.Fatal("role identity absent")
			}
			if role == "" {
				role = id
			}
			if role != id {
				t.Fatal("concurrent receipt differs")
			}
		}
		var n, a, p int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_roles WHERE id=$1),(SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action='iam.platform.createPlatformIAMRole' AND caller_principal_id IS NOT NULL AND caller_binding_id IS NOT NULL AND caller_binding_version>0 AND caller_grant_version>0),(SELECT count(*) FROM platform_role_permissions WHERE role_id=$1)`, role).Scan(&n, &a, &p) != nil || n != 1 || a != 1 || p != 1 {
			t.Fatal("role/permission/audit not atomic")
		}
	})
	if t.Failed() || role == "" {
		return
	}
	t.Run("exact_intent_catalog_and_system_guards", func(t *testing.T) {
		call(t, "POST", roles, key, map[string]any{"name": "changed intent", "permissions": read}, 409)
		call(t, "POST", roles, uuid.NewString(), map[string]any{"name": "wrong scope", "permissions": []string{"instances/read"}}, 400)
		call(t, "POST", roles, uuid.NewString(), map[string]any{"name": "invented", "permissions": []string{"invented/read"}}, 400)
		call(t, "PATCH", roles+"/"+builtin, uuid.NewString(), map[string]any{"name": "alter builtin", "permissions": read, "expected_version": 1}, 403)
		call(t, "DELETE", roles+"/"+builtin+"?expected_version=1", uuid.NewString(), nil, 403)
		call(t, "PATCH", roles+"/"+b.adminRoles[0].String(), uuid.NewString(), map[string]any{"name": "foreign role", "permissions": read, "expected_version": 1}, 404)
	})
	t.Run("concurrent_CAS_and_Permission_join_replacement", func(t *testing.T) {
		out := make(chan int, 2)
		for i := 0; i < 2; i++ {
			go func() {
				code := 0
				defer func() { out <- code }()
				status, _, _ := b.request(t, browser, "PATCH", roles+"/"+role, token, map[string]any{"name": "WR22 access readers", "permissions": []string{"iam.tenant-access/read"}, "expected_version": 1}, map[string]string{"Idempotency-Key": uuid.NewString()})
				code = status
			}()
		}
		counts := map[int]int{}
		counts[<-out]++
		counts[<-out]++
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatal("Platform version CAS differs")
		}
		v := call(t, "GET", roles+"/"+role, "", nil, 200)
		p, _ := v["permissions"].([]any)
		if v["version"] != float64(2) || len(p) != 1 || p[0] != "iam.tenant-access/read" {
			t.Fatal("role Permission replacement differs")
		}
	})
	t.Run("confirmed_Audit_fault_rolls_back_role_and_receipt_then_retry", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_platform_mutation_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_platform_mutation_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_platform_mutation_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN ('iam.platform.updatePlatformIAMRole','iam.platform.updateTenantAccess') THEN PERFORM nextval('wr22_platform_mutation_fault_hits'); RAISE EXCEPTION 'isolated Platform mutation Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_platform_mutation_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_platform_mutation_fault()`)
		if err != nil {
			t.Fatal("own mutation Audit fault setup")
		}
		key := uuid.NewString()
		payload := map[string]any{"name": "WR22 recovered role", "permissions": read, "expected_version": 2}
		call(t, "PATCH", roles+"/"+role, key, payload, 503)
		accessKey := uuid.NewString()
		call(t, "PATCH", "/iam/tenants/"+referenceTenants[0].String()+"/access", accessKey, map[string]any{"status": "suspended", "expected_version": 1}, 503)
		var accessVersion, accessReceipts int64
		var accessState string
		if b.owner.QueryRow(ctx, `SELECT a.version,a.status,(SELECT count(*) FROM platform_mutation_results WHERE idempotency_key=$2) FROM tenant_access a WHERE tenant_id=$1`, referenceTenants[0], accessKey).Scan(&accessVersion, &accessState, &accessReceipts) != nil || accessVersion != 1 || accessState != "active" || accessReceipts != 0 {
			t.Fatal("TenantAccess audit failure left state or receipt")
		}

		var hit bool
		var version, receipts int64
		if b.owner.QueryRow(ctx, `SELECT (SELECT is_called FROM wr22_platform_mutation_fault_hits),(SELECT version FROM platform_roles WHERE id=$1),(SELECT count(*) FROM platform_mutation_results WHERE idempotency_key=$2)`, role, key).Scan(&hit, &version, &receipts) != nil || !hit || version != 2 || receipts != 0 {
			t.Fatal("confirmed Audit failure left state or receipt")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_platform_mutation_fault ON iam_audit_events; DROP FUNCTION wr22_platform_mutation_fault(); DROP SEQUENCE wr22_platform_mutation_fault_hits`); err != nil {
			t.Fatal("own mutation Audit restoration")
		}
		call(t, "PATCH", roles+"/"+role, key, payload, 200)
		call(t, "PATCH", roles+"/"+role, key, payload, 200)
	})
	if t.Failed() {
		return
	}
	t.Run("current_authority_precedes_replay_and_24h_window", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND resource='iam.platform-roles' AND action='create'`, builtin); err != nil {
			t.Fatal("own authority fault")
		}
		call(t, "POST", roles, key, create, 403)
		if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.platform-roles','create',now())`, builtin); err != nil {
			t.Fatal("own authority restoration")
		}
		call(t, "POST", roles, key, create, 201)
		if _, err := b.owner.Exec(ctx, `UPDATE platform_mutation_results SET created_at=created_at-interval '25 hours',expires_at=expires_at-interval '25 hours' WHERE operation='createPlatformIAMRole' AND idempotency_key=$1`, key); err != nil {
			t.Fatal("own synthetic receipt clock")
		}
		call(t, "POST", roles, key, create, 409)
	})
	t.Run("TenantAccess_CAS_and_restore_does_not_mutate_Core_projection", func(t *testing.T) {
		path := "/iam/tenants/" + referenceTenants[0].String() + "/access"
		v := call(t, "GET", path, "", nil, 200)
		version := v["version"].(float64)
		var before, after string
		if b.owner.QueryRow(ctx, `SELECT row_to_json(p)::text FROM tenant_lifecycle_projections p WHERE tenant_id=$1`, referenceTenants[0]).Scan(&before) != nil {
			t.Fatal("own lifecycle prerequisite")
		}
		key := uuid.NewString()
		payload := map[string]any{"status": "suspended", "expected_version": version}
		v = call(t, "PATCH", path, key, payload, 200)
		if v["status"] != "suspended" || v["version"] != version+1 {
			t.Fatal("TenantAccess CAS differs")
		}
		call(t, "PATCH", path, key, payload, 200)
		call(t, "PATCH", path, uuid.NewString(), payload, 409)
		call(t, "PATCH", path, uuid.NewString(), map[string]any{"status": "active", "expected_version": version + 1}, 200)
		call(t, "PATCH", "/iam/tenants/"+mustV7(t).String()+"/access", uuid.NewString(), payload, 404)
		if b.owner.QueryRow(ctx, `SELECT row_to_json(p)::text FROM tenant_lifecycle_projections p WHERE tenant_id=$1`, referenceTenants[0]).Scan(&after) != nil || before != after {
			t.Fatal("IAM access changed Core projection")
		}
	})

	t.Run("binding_reference_rejects_role_deletion", func(t *testing.T) {
		// Only the reference relation is a prerequisite fixture; deletion is
		// executed through the real management endpoint. Binding API follows.
		var membership string
		if b.owner.QueryRow(ctx, `SELECT id FROM platform_memberships WHERE status='active'`).Scan(&membership) != nil {
			t.Fatal("own membership prerequisite")
		}
		binding := mustV7(t)
		if _, err := b.owner.Exec(ctx, `INSERT INTO platform_role_bindings(id,membership_id,role_id,version,created_at,updated_at) VALUES($1,$2,$3,1,now(),now())`, binding, membership, role); err != nil {
			t.Fatal("own role reference fixture")
		}
		call(t, "DELETE", roles+"/"+role+"?expected_version=3", uuid.NewString(), nil, 409)
		if _, err := b.owner.Exec(ctx, `DELETE FROM platform_role_bindings WHERE id=$1`, binding); err != nil {
			t.Fatal("own role reference restoration")
		}
	})
	t.Run("formal_delete_receipt_then_missing_resource", func(t *testing.T) {
		key := uuid.NewString()
		path := roles + "/" + role + "?expected_version=3"
		call(t, "DELETE", path, key, nil, 204)
		call(t, "DELETE", path, key, nil, 204)
		call(t, "GET", roles+"/"+role, "", nil, 404)
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A11", "formal_Platform_role_TenantAccess_mutations": "pass", "Invitation_reference": "not_verified", "Platform_Membership_mutations": "not_verified"})
	}
}
