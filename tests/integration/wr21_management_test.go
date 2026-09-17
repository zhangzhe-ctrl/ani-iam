//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWR21StageAFormalManagement(t *testing.T) {
	e := newWR21Environment(t)
	ctx := context.Background()
	client, token, _, _ := e.login(t, 0)
	other, otherToken, _, _ := e.login(t, 1)
	tenant := referenceTenants[0]
	var principal, keyID, secret string
	createIntent := uuid.NewString()
	keyIntent := uuid.NewString()
	request := func(t *testing.T, c *http.Client, bearer, method, path string, body any, key string, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		code, result, response := e.request(t, c, method, path, bearer, body, headers)
		if code != want {
			t.Fatalf("%s %s status=%d reason=%s want=%d", method, path, code, result["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("management response is cacheable")
		}
		return result
	}
	sub := func(name string, fn func(*testing.T)) {
		if !t.Run(name, fn) {
			t.FailNow()
		}
	}
	createBody := map[string]any{"tenant_id": tenant.String(), "name": "WR21 Bot", "role_ids": []string{e.invokeRoles[0].String()}}
	sub("create_atomic_workload_membership_binding_audit_without_key", func(t *testing.T) {
		result := request(t, client, token, "POST", "/auth/tenant-workloads", createBody, createIntent, 201)
		principal, _ = result["principal_id"].(string)
		if principal == "" || result["name"] != "wr21 bot" || result["tenant_id"] != tenant.String() {
			t.Fatal("Workload response contract differs")
		}
		var memberships, bindings, audits, keys int
		err := e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status<>'removed'),(SELECT count(*) FROM tenant_role_bindings b JOIN tenant_memberships m ON m.tenant_id=b.tenant_id AND m.id=b.membership_id WHERE m.tenant_id=$1 AND m.principal_id=$2),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.tenant-workload.created'),(SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2)`, tenant, principal).Scan(&memberships, &bindings, &audits, &keys)
		if err != nil {
			t.Fatal("atomic state read failed")
		}
		if memberships != 1 || bindings != 1 || audits != 1 || keys != 0 {
			t.Fatalf("atomic counts=%d/%d/%d/%d", memberships, bindings, audits, keys)
		}
		replay := request(t, client, token, "POST", "/auth/tenant-workloads", createBody, createIntent, 201)
		if replay["principal_id"] != principal {
			t.Fatal("Workload replay created a second principal")
		}
	})
	sub("read_list_cross_tenant_and_immutable_fields", func(t *testing.T) {
		request(t, client, token, "GET", "/auth/tenant-workloads/"+principal, nil, "", 200)
		list := request(t, client, token, "GET", "/auth/tenant-workloads?tenant_id="+tenant.String(), nil, "", 200)
		if len(list["items"].([]any)) != 1 {
			t.Fatal("unexpected Workload list")
		}
		request(t, other, otherToken, "GET", "/auth/tenant-workloads/"+principal, nil, "", 404)
		request(t, other, otherToken, "GET", "/auth/tenant-workloads?tenant_id="+tenant.String(), nil, "", 403)
		request(t, client, "", "GET", "/auth/tenant-workloads/"+principal, nil, "", 401)
		request(t, client, token, "PATCH", "/auth/tenant-workloads/"+principal, map[string]any{"name": "renamed", "status": "active", "expected_version": 1}, "", 400)
		request(t, client, token, "POST", "/auth/tenant-workloads", map[string]any{"tenant_id": tenant.String(), "name": "foreign-role", "role_ids": []string{e.invokeRoles[1].String()}}, "", 404)
	})
	sub("explicit_key_once_digest_only_and_safe_replay", func(t *testing.T) {
		body := map[string]any{"principal_id": principal, "never_expires": true}
		result := request(t, client, token, "POST", "/auth/api-keys", body, keyIntent, 201)
		secret, _ = result["secret"].(string)
		keyID, _ = result["api_key"].(map[string]any)["key_id"].(string)
		if secret == "" || keyID == "" || result["replayed"] != false {
			t.Fatal("initial Key result incomplete")
		}
		var digest []byte
		var leaked int
		err := e.owner.QueryRow(ctx, `SELECT secret_digest,(SELECT count(*) FROM tenant_mutation_results WHERE tenant_id=$1 AND encode(result,'escape') LIKE '%'||$3||'%')+(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND row_to_json(iam_audit_events)::text LIKE '%'||$3||'%') FROM api_keys WHERE tenant_id=$1 AND key_id=$2`, tenant, keyID, secret).Scan(&digest, &leaked)
		if err != nil {
			t.Fatal("key persistence read failed")
		}
		want := sha256.Sum256([]byte(secret))
		if string(digest) != string(want[:]) || leaked != 0 {
			t.Fatal("secret persistence contract failed")
		}
		replay := request(t, client, token, "POST", "/auth/api-keys", body, keyIntent, 201)
		if replay["secret"] != "" || replay["replayed"] != true || replay["api_key"].(map[string]any)["key_id"] != keyID {
			t.Fatal("Key replay secret or identity differs")
		}
		list := request(t, client, token, "GET", "/auth/api-keys?principal_id="+principal, nil, "", 200)
		if len(list["items"].([]any)) != 1 {
			t.Fatal("Key replay multiplied keys")
		}
		request(t, client, secret, "GET", "/auth/tenant-workloads?tenant_id="+tenant.String(), nil, "", 403)
		request(t, client, token, "POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true, "expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)}, "", 400)
		request(t, client, token, "POST", "/auth/api-keys", map[string]any{"principal_id": e.humans[0].String(), "never_expires": true}, "", 404)
	})
	sub("idempotency_conflict_expiry_and_current_actor_recheck", func(t *testing.T) {
		request(t, client, token, "POST", "/auth/api-keys", map[string]any{"principal_id": principal, "expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)}, keyIntent, 409)
		_, err := e.owner.Exec(ctx, `UPDATE tenant_mutation_results SET created_at=now()-interval '25 hours',expires_at=now()-interval '1 hour' WHERE tenant_id=$1 AND actor_id=$2 AND operation='createIAMAPIKey' AND idempotency_key=$3`, tenant, e.humans[0], keyIntent)
		if err != nil {
			t.Fatal("isolated ledger expiry injection failed")
		}
		request(t, client, token, "POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true}, keyIntent, 409)
		_, err = e.owner.Exec(ctx, `DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='iam.tenant-workloads' AND action='create'`, tenant, e.adminRoles[0])
		if err != nil {
			t.Fatal("isolated permission fault failed")
		}
		request(t, client, token, "POST", "/auth/tenant-workloads", createBody, createIntent, 403)
		_, err = e.owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','iam.tenant-workloads','create',now())`, tenant, e.adminRoles[0])
		if err != nil {
			t.Fatal("isolated permission recovery failed")
		}
	})
	sub("concurrent_key_creation_returns_one_secret", func(t *testing.T) {
		type outcome struct {
			r   *iamv1.CreateAPIKeyResponse
			err error
		}
		out := make(chan outcome, 4)
		intent := uuid.NewString()
		for i := 0; i < 4; i++ {
			go func() {
				call, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				r, err := e.iam.admin.CreateAPIKey(call, &iamv1.CreateAPIKeyRequest{Credential: &iamv1.BearerCredential{Value: token}, PrincipalId: principal, NeverExpires: true, IdempotencyKey: intent})
				out <- outcome{r, err}
			}()
		}
		id := ""
		secretCount := 0
		for i := 0; i < 4; i++ {
			o := <-out
			if o.err != nil {
				t.Fatal("concurrent formal Key creation failed")
			}
			if id == "" {
				id = o.r.GetApiKey().GetKeyId()
			}
			if id != o.r.GetApiKey().GetKeyId() {
				t.Fatal("concurrent replay created distinct keys")
			}
			if o.r.GetApiKeySecret() != "" {
				secretCount++
			}
		}
		if secretCount != 1 {
			t.Fatalf("first-secret responses=%d", secretCount)
		}
	})
	sub("audit_failure_rolls_back_whole_creation_and_recovers", func(t *testing.T) {
		_, err := e.owner.Exec(ctx, `CREATE FUNCTION wr21_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.tenant-workload.created' THEN RAISE EXCEPTION 'WR21 injected audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr21_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr21_audit_fault()`)
		if err != nil {
			t.Fatal("task audit fault setup failed")
		}
		faultBody := map[string]any{"tenant_id": tenant.String(), "name": "atomic-fault", "role_ids": []string{e.invokeRoles[0].String()}}
		intent := uuid.NewString()
		request(t, client, token, "POST", "/auth/tenant-workloads", faultBody, intent, 503)
		_, err = e.owner.Exec(ctx, `DROP TRIGGER wr21_audit_fault ON iam_audit_events; DROP FUNCTION wr21_audit_fault()`)
		if err != nil {
			t.Fatal("audit fault recovery failed")
		}
		var count int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM workload_principals WHERE tenant_id=$1 AND normalized_name='atomic-fault'`, tenant).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed audit left a Workload")
		}
		request(t, client, token, "POST", "/auth/tenant-workloads", faultBody, intent, 201)
	})
	sub("lost_secret_recovery_revoke_new_intent_and_disable", func(t *testing.T) {
		request(t, client, token, "DELETE", "/auth/api-keys/"+keyID, nil, "", 204)
		result := request(t, client, token, "POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true}, "", 201)
		newSecret, _ := result["secret"].(string)
		if newSecret == "" || newSecret == secret {
			t.Fatal("explicit replacement secret missing")
		}
		request(t, client, token, "PATCH", "/auth/tenant-workloads/"+principal, map[string]any{"status": "disabled", "expected_version": 1}, "", 200)
		var activeKeys int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2 AND status='active'`, tenant, principal).Scan(&activeKeys); err != nil || activeKeys != 0 {
			t.Fatal("disable failed to revoke keys")
		}
		// Enabled Workload requires its existing non-removed Membership; this case
		// disables the Principal, and never restores old Key credentials.
		result = request(t, client, token, "GET", "/auth/tenant-workloads/"+principal, nil, "", 200)
		if result["status"] != "disabled" {
			t.Fatal("disable state differs")
		}
	})
	if strings.Contains(principal, secret) {
		t.Fatal("invalid evidence identifier")
	}
	recordReference(t, e.run, map[string]any{"stage": "A", "formal_management": "pass", "tenant": tenant, "workload_id": principal, "key_id": keyID, "secret_recorded": false, "business_chain": "not_verified"})
}
