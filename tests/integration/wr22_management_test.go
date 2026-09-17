//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestWR22FormalTenantRoles(t *testing.T) {
	e := newWR22Environment(t)
	client, token, _, _ := e.login(t, 0)
	other, otherToken, _, _ := e.login(t, 1)
	base := "/iam/tenants/" + referenceTenants[0].String() + "/roles"
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
	var id string
	t.Run("generated_catalog_boundary_and_pagination", func(t *testing.T) {
		path := "/iam/tenants/" + referenceTenants[0].String() + "/permissions"
		page := request(t, client, token, "GET", path+"?limit=1", nil, "", 200)
		if len(page["permissions"].([]any)) != 1 || page["policy_revision"] == "" || page["next_cursor"] == nil {
			t.Fatal("catalog first page invalid")
		}
		request(t, client, token, "GET", path+"?limit=1&cursor="+page["next_cursor"].(string), nil, "", 200)
		request(t, other, otherToken, "GET", path, nil, "", 403)
		request(t, client, token, "GET", path+"?cursor=invalid", nil, "", 400)
		all := request(t, client, token, "GET", path+"?limit=100", nil, "", 200)
		rawRegistry, err := os.ReadFile("../contracts/workload-operation-registry.v1.json")
		if err != nil {
			t.Fatal(err)
		}
		var registry struct {
			Operations []struct {
				Permission struct {
					Scope    string   `json:"scope"`
					Resource string   `json:"resource"`
					Actions  []string `json:"actions"`
				} `json:"permission"`
			} `json:"operations"`
		}
		if json.Unmarshal(rawRegistry, &registry) != nil {
			t.Fatal("fixed registry cannot be decoded")
		}
		expected := map[string]bool{}
		for _, op := range registry.Operations {
			if op.Permission.Scope == "tenant" {
				for _, action := range op.Permission.Actions {
					expected[op.Permission.Resource+"/"+action] = true
				}
			}
		}
		found := false
		for _, raw := range all["permissions"].([]any) {
			p := raw.(string)
			if p == "instances/read" {
				found = true
			}
			if !expected[p] {
				t.Fatal("platform permission exposed in tenant catalog")
			}
		}
		if !found {
			t.Fatal("generated permission missing")
		}
	})
	intent := uuid.NewString()
	body := map[string]any{"name": "WR22 readers", "permissions": []string{"instances/read"}}
	t.Run("create_replay_and_persisted_audit", func(t *testing.T) {
		r := request(t, client, token, "POST", base, body, intent, 201)
		id = r["role_id"].(string)
		if r["name"] != "WR22 readers" || r["system"] != false || r["version"] != float64(1) {
			t.Fatal("REST role result differs")
		}
		replayed := request(t, client, token, "POST", base, body, intent, 201)
		if replayed["role_id"] != id {
			t.Fatal("replay created another role")
		}
		var count int
		if err := e.owner.QueryRow(context.Background(), `SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.role.created'`, referenceTenants[0], id).Scan(&count); err != nil {
			t.Fatal("audit lookup failed")
		}
		if count != 1 {
			t.Fatal("missing or duplicate audit")
		}
	})
	if id == "" {
		t.Fatal("formal create prerequisite failed")
	}
	t.Run("read_list_and_tenant_boundary", func(t *testing.T) {
		request(t, client, token, "GET", base+"/"+id, nil, "", 200)
		r := request(t, client, token, "GET", base+"?limit=1", nil, "", 200)
		if len(r["items"].([]any)) != 1 || r["next_cursor"] == nil {
			t.Fatal("role pagination failed")
		}
		request(t, other, otherToken, "GET", base+"/"+id, nil, "", 403)
		request(t, client, "", "GET", base, nil, "", 401)
		request(t, client, token, "POST", base, map[string]any{"name": "invalid", "permissions": []string{"invented/read"}}, uuid.NewString(), 400)
	})
	t.Run("update_expected_version_and_system_protection", func(t *testing.T) {
		u := map[string]any{"name": "WR22 creators", "permissions": []string{"instances/create"}, "expected_version": 1}
		key := uuid.NewString()
		r := request(t, client, token, "PATCH", base+"/"+id, u, key, 200)
		if r["version"] != float64(2) || r["name"] != "WR22 creators" {
			t.Fatal("role update differs")
		}
		request(t, client, token, "PATCH", base+"/"+id, u, key, 200)
		request(t, client, token, "PATCH", base+"/"+id, u, uuid.NewString(), 409)
		request(t, client, token, "DELETE", base+"/"+e.adminRoles[0].String()+"?expected_version=1", nil, uuid.NewString(), 403)
	})
	t.Run("audit_failure_then_recovery", func(t *testing.T) {
		if _, err := e.owner.Exec(context.Background(), `REVOKE INSERT ON iam_audit_events FROM ani_iam_runtime`); err != nil {
			t.Fatal("audit fault failed")
		}
		defer func() {
			if _, err := e.owner.Exec(context.Background(), `GRANT INSERT ON iam_audit_events TO ani_iam_runtime`); err != nil {
				t.Fatal("audit recovery failed")
			}
		}()
		request(t, client, token, "PATCH", base+"/"+id, map[string]any{"name": "rollback", "permissions": []string{"instances/read"}, "expected_version": 2}, uuid.NewString(), 503)
		var name string
		var version int64
		if err := e.owner.QueryRow(context.Background(), `SELECT display_name,version FROM tenant_roles WHERE tenant_id=$1 AND id=$2`, referenceTenants[0], id).Scan(&name, &version); err != nil {
			t.Fatal("role read failed")
		}
		if name != "WR22 creators" || version != 2 {
			t.Fatal("failed audit changed role")
		}
	})
	t.Run("versioned_delete_and_replay", func(t *testing.T) {
		request(t, client, token, "DELETE", base+"/"+id, nil, uuid.NewString(), 400)
		key := uuid.NewString()
		path := base + "/" + id + "?expected_version=2"
		request(t, client, token, "DELETE", path, nil, key, 204)
		request(t, client, token, "DELETE", path, nil, key, 204)
		request(t, client, token, "GET", base+"/"+id, nil, "", 404)
	})
}
