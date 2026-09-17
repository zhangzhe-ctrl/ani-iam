//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestWR22FormalTenantMembership(t *testing.T) {
	e := newWR22Environment(t)
	client, token, _, _ := e.login(t, 0)
	other, otherToken, _, _ := e.login(t, 1)
	base := "/iam/tenants/" + referenceTenants[0].String()
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
	var own uuid.UUID
	if err := e.owner.QueryRow(context.Background(), `SELECT id FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, referenceTenants[0], e.humans[0]).Scan(&own); err != nil {
		t.Fatal("actor membership prerequisite unavailable")
	}
	memberBase := base + "/members/"
	t.Run("last_admin_concurrent_removal_and_unbinding", func(t *testing.T) {
		var wg sync.WaitGroup
		for _, path := range []string{memberBase + own.String() + "?expected_version=1", memberBase + own.String() + "/role-bindings/" + e.adminRoles[0].String() + "?expected_membership_version=1"} {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				result := request(t, client, token, "DELETE", path, nil, uuid.NewString(), 403)
				if result["code"] != "PERMISSION_DENIED" {
					t.Errorf("last admin reason=%s", result["code"])
				}
			}(path)
		}
		wg.Wait()
		v := request(t, client, token, "GET", memberBase+own.String(), nil, "", 200)
		if v["status"] != "active" || v["version"] != float64(1) {
			t.Fatal("guard changed last admin")
		}
	})
	role := request(t, client, token, "POST", base+"/roles", map[string]any{"name": "WR22 member reader", "permissions": []string{"instances/read"}}, uuid.NewString(), 201)["role_id"].(string)
	workload := request(t, client, token, "POST", "/auth/tenant-workloads", map[string]any{"tenant_id": referenceTenants[0].String(), "name": "WR22 managed member", "role_ids": []string{role}}, uuid.NewString(), 201)
	member := workload["membership_id"].(string)
	path := memberBase + member
	t.Run("get_list_and_cross_tenant", func(t *testing.T) {
		v := request(t, client, token, "GET", path, nil, "", 200)
		if v["principal"].(map[string]any)["principal_type"] != "workload" || v["boundary"].(map[string]any)["tenant_id"] != referenceTenants[0].String() {
			t.Fatal("membership boundary differs")
		}
		page := request(t, client, token, "GET", base+"/members?limit=1", nil, "", 200)
		if len(page["items"].([]any)) != 1 || page["next_cursor"] == nil {
			t.Fatal("membership pagination differs")
		}
		request(t, client, token, "GET", base+"/members?limit=1&cursor="+page["next_cursor"].(string), nil, "", 200)
		request(t, other, otherToken, "GET", path, nil, "", 403)
		request(t, client, "", "GET", base+"/members", nil, "", 401)
	})
	t.Run("cas_binding_and_references", func(t *testing.T) {
		request(t, client, token, "DELETE", base+"/roles/"+role+"?expected_version=1", nil, uuid.NewString(), 409)
		request(t, client, token, "POST", path+"/role-bindings", map[string]any{"role_id": e.adminRoles[0].String(), "expected_membership_version": 1}, uuid.NewString(), 204)
		request(t, client, token, "DELETE", memberBase+own.String()+"?expected_version=1", nil, uuid.NewString(), 403)
		request(t, client, token, "DELETE", path+"/role-bindings/"+e.adminRoles[0].String()+"?expected_membership_version=2", nil, uuid.NewString(), 204)
		b := map[string]any{"role_id": e.invokeRoles[0].String(), "expected_membership_version": 3}
		key := uuid.NewString()
		request(t, client, token, "POST", path+"/role-bindings", b, key, 204)
		request(t, client, token, "POST", path+"/role-bindings", b, key, 204)
		request(t, client, token, "POST", path+"/role-bindings", b, uuid.NewString(), 409)
		request(t, client, token, "DELETE", path+"/role-bindings/"+e.invokeRoles[0].String()+"?expected_membership_version=4", nil, uuid.NewString(), 204)
	})
	t.Run("suspend_resume_remove_and_no_resurrection", func(t *testing.T) {
		for index, state := range []string{"suspended", "active"} {
			key := uuid.NewString()
			body := map[string]any{"status": state, "expected_version": 5 + index}
			v := request(t, client, token, "PATCH", path, body, key, 200)
			if v["version"] != float64(6+index) || v["status"] != state {
				t.Fatal("membership update result differs")
			}
			request(t, client, token, "PATCH", path, body, key, 200)
		}
		request(t, client, token, "PATCH", path, map[string]any{"status": "removed", "expected_version": 7}, uuid.NewString(), 400)
		request(t, client, token, "DELETE", path, nil, uuid.NewString(), 400)
		key := uuid.NewString()
		request(t, client, token, "DELETE", path+"?expected_version=7", nil, key, 204)
		request(t, client, token, "DELETE", path+"?expected_version=7", nil, key, 204)
		request(t, client, token, "PATCH", path, map[string]any{"status": "active", "expected_version": 8}, uuid.NewString(), 400)
		var status string
		var version int64
		if err := e.owner.QueryRow(context.Background(), `SELECT status,version FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, referenceTenants[0], member).Scan(&status, &version); err != nil {
			t.Fatal("membership persisted lookup failed")
		}
		if status != "removed" || version != 8 {
			t.Fatal("removed membership changed")
		}
	})
	t.Run("current_authority_precedes_idempotent_replay", func(t *testing.T) {
		key := uuid.NewString()
		body := map[string]any{"name": "before revoke", "permissions": []string{"instances/read"}}
		request(t, client, token, "POST", base+"/roles", body, key, 201)
		// This fault affects only the isolated test actor. Restoration preserves
		// the invariant after proving current Session state rejects the replay.
		if _, err := e.owner.Exec(context.Background(), `UPDATE principals SET status='disabled' WHERE id=$1`, e.humans[0]); err != nil {
			t.Fatal("isolated actor fault failed")
		}
		defer func() {
			if _, err := e.owner.Exec(context.Background(), `UPDATE principals SET status='active' WHERE id=$1`, e.humans[0]); err != nil {
				t.Error("isolated actor recovery failed")
			}
		}()
		code, result, _ := e.request(t, client, "POST", base+"/roles", token, body, map[string]string{"Idempotency-Key": key})
		if code != 403 {
			t.Fatal(fmt.Sprintf("revoked actor replay status=%d reason=%s", code, result["code"]))
		}
	})
}
