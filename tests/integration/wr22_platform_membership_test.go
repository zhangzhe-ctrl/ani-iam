//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"strconv"
	"sync"
	"testing"
)

func TestWR22FormalPlatformMembership(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	browser := b.browser(t)
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("state differs")
	}
	status, _, _ := b.callback(t, browser, code, state)
	if status != 303 {
		t.Fatal("formal first admin login failed")
	}
	status, body, _ := b.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
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
	var first, adminRole string
	if b.owner.QueryRow(ctx, `SELECT m.id,r.id FROM platform_memberships m JOIN platform_role_bindings b ON b.membership_id=m.id JOIN platform_roles r ON r.id=b.role_id WHERE r.code='platform-admin' AND r.system_role`).Scan(&first, &adminRole) != nil {
		t.Fatal("formal first admin graph missing")
	}
	const members = "/iam/platform/members/"
	t.Run("first_real_login_capable_admin_is_protected", func(t *testing.T) {
		call(t, "PATCH", members+first, uuid.NewString(), map[string]any{"status": "suspended", "expected_version": 1}, 403)
		call(t, "DELETE", members+first+"?expected_version=1", uuid.NewString(), nil, 403)
		call(t, "DELETE", members+first+"/role-bindings/"+adminRole+"?expected_membership_version=1", uuid.NewString(), nil, 403)
	})
	// An additional target Membership is an explicit SQL prerequisite. It uses
	// this run's Console Human and cannot be counted as a BOSS login identity.
	other := mustV7(t)
	var human uuid.UUID
	if b.owner.QueryRow(ctx, `SELECT p.id FROM principals p JOIN tenant_memberships m ON m.principal_id=p.id WHERE p.principal_type='human' AND p.status='active' LIMIT 1`).Scan(&human) != nil {
		t.Fatal("own Console Human prerequisite")
	}
	if _, err := b.owner.Exec(ctx, `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, other, human); err != nil {
		t.Fatal("own target Membership prerequisite")
	}
	roleBody := call(t, "POST", "/iam/platform/roles", uuid.NewString(), map[string]any{"name": "WR22 member read role", "permissions": []string{"iam.platform-memberships/read"}}, 201)
	role, _ := roleBody["role_id"].(string)
	path := members + other.String()
	t.Run("concurrent_binding_one_effect_and_exact_role_boundary", func(t *testing.T) {
		key := uuid.NewString()
		payload := map[string]any{"role_id": role, "expected_membership_version": 1}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); call(t, "POST", path+"/role-bindings", key, payload, 204) }()
		}
		wg.Wait()
		v := call(t, "GET", path, "", nil, 200)
		ids, _ := v["role_ids"].([]any)
		if v["version"] != float64(2) || len(ids) != 1 || ids[0] != role {
			t.Fatal("one Binding/Version result differs")
		}
		call(t, "POST", path+"/role-bindings", uuid.NewString(), payload, 409)
		call(t, "POST", path+"/role-bindings", uuid.NewString(), map[string]any{"role_id": b.adminRoles[0].String(), "expected_membership_version": 2}, 404)
	})
	if t.Failed() {
		return
	}
	t.Run("Human_without_trusted_BOSS_login_does_not_satisfy_last_admin", func(t *testing.T) {
		// A15 enables BOSS passwords. This negative fixture disables only this
		// run's password Identity, leaving no usable configured BOSS method.
		if _, err := b.owner.Exec(ctx, `UPDATE identities SET status='disabled' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
			t.Fatal("own password Identity fault")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `UPDATE identities SET status='active' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
				t.Error("own password Identity restoration")
			}
		}()
		call(t, "POST", path+"/role-bindings", uuid.NewString(), map[string]any{"role_id": adminRole, "expected_membership_version": 2}, 204)
		call(t, "PATCH", members+first, uuid.NewString(), map[string]any{"status": "suspended", "expected_version": 1}, 403)
		// A syntactically active OIDC Identity with an untrusted issuer is also
		// not sufficient. This is a deliberate own-test policy fault fixture.
		identity := mustV7(t)
		if _, err := b.owner.Exec(ctx, `INSERT INTO identities(id,principal_id,provider,issuer,subject,status,version,created_at,updated_at) VALUES($1,$2,'dex','https://untrusted.wr22.test','wr22-guard-fixture','active',1,now(),now())`, identity, human); err != nil {
			t.Fatal("own untrusted Identity prerequisite")
		}
		call(t, "DELETE", members+first+"?expected_version=1", uuid.NewString(), nil, 403)
		call(t, "DELETE", path+"/role-bindings/"+adminRole+"?expected_membership_version=3", uuid.NewString(), nil, 204)
	})
	t.Run("Membership_CAS_suspend_restore_and_Audit_rollback", func(t *testing.T) {
		out := make(chan int, 2)
		for i := 0; i < 2; i++ {
			go func() {
				code := 0
				defer func() { out <- code }()
				status, _, _ := b.request(t, browser, "PATCH", path, token, map[string]any{"status": "suspended", "expected_version": 4}, map[string]string{"Idempotency-Key": uuid.NewString()})
				code = status
			}()
		}
		counts := map[int]int{}
		counts[<-out]++
		counts[<-out]++
		if counts[200] != 1 || counts[409] != 1 {
			t.Fatal("Membership CAS differs")
		}
		call(t, "POST", path+"/role-bindings", uuid.NewString(), map[string]any{"role_id": adminRole, "expected_membership_version": 5}, 400)
		call(t, "PATCH", path, uuid.NewString(), map[string]any{"status": "active", "expected_version": 5}, 200)
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_member_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_member_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_member_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.updatePlatformIAMMember' THEN PERFORM nextval('wr22_member_fault_hits'); RAISE EXCEPTION 'isolated member Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_member_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_member_fault()`)
		if err != nil {
			t.Fatal("own member Audit fault setup")
		}
		key := uuid.NewString()
		call(t, "PATCH", path, key, map[string]any{"status": "suspended", "expected_version": 6}, 503)
		var hit bool
		var version, receipts int64
		if b.owner.QueryRow(ctx, `SELECT (SELECT is_called FROM wr22_member_fault_hits),(SELECT version FROM platform_memberships WHERE id=$1),(SELECT count(*) FROM platform_mutation_results WHERE idempotency_key=$2)`, other, key).Scan(&hit, &version, &receipts) != nil || !hit || version != 6 || receipts != 0 {
			t.Fatal("confirmed member Audit rollback failed")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_member_fault ON iam_audit_events; DROP FUNCTION wr22_member_fault(); DROP SEQUENCE wr22_member_fault_hits`); err != nil {
			t.Fatal("own member Audit restoration")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("terminal_removal_replays_and_references_can_be_unbound", func(t *testing.T) {
		key := uuid.NewString()
		call(t, "DELETE", path+"?expected_version=6", key, nil, 204)
		call(t, "DELETE", path+"?expected_version=6", key, nil, 204)
		v := call(t, "GET", path, "", nil, 200)
		if v["status"] != "removed" || v["version"] != float64(7) {
			t.Fatal("removed Membership differs")
		}
		call(t, "PATCH", path, uuid.NewString(), map[string]any{"status": "active", "expected_version": 7}, 400)
		key = uuid.NewString()
		unbind := path + "/role-bindings/" + role + "?expected_membership_version=" + strconv.Itoa(7)
		call(t, "DELETE", unbind, key, nil, 204)
		call(t, "DELETE", unbind, key, nil, 204)
		call(t, "DELETE", "/iam/platform/roles/"+role+"?expected_version=1", uuid.NewString(), nil, 204)
		v = call(t, "GET", path, "", nil, 200)
		if v["status"] != "removed" || v["version"] != float64(8) {
			t.Fatal("unbind revived removed Membership")
		}
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A12", "formal_Platform_Membership_and_Binding": "pass", "first_real_admin_guard": "pass", "additional_Human": "SQL prerequisite from this run's Console fixture; not real Invitation signup", "two_real_login_capable_admin_race": "not_verified"})
	}
}
