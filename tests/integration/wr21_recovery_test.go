//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWR21StageCRevocationFailureRecovery(t *testing.T) {
	root := t
	c := newWR21Chain(t)
	ctx := context.Background()
	client, actor, _, _ := c.login(t, 0)
	tenant := referenceTenants[0]
	management := func(t *testing.T, method, path string, body any, want int) map[string]any {
		t.Helper()
		code, r, _ := c.request(t, client, method, path, actor, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if code != want {
			t.Fatalf("management status=%d reason=%s want=%d", code, r["code"], want)
		}
		return r
	}
	workload := management(t, "POST", "/auth/tenant-workloads", map[string]any{"tenant_id": tenant.String(), "name": "recovery-bot", "role_ids": []string{c.invokeRoles[0].String()}}, 201)
	principal := workload["principal_id"].(string)
	membership := workload["membership_id"].(string)
	createKey := func(t *testing.T) (string, string) {
		t.Helper()
		r := management(t, "POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true}, 201)
		return r["secret"].(string), r["api_key"].(map[string]any)["key_id"].(string)
	}
	secret, keyID := createKey(t)
	revokedSecret, revokedID := createKey(t)
	management(t, "DELETE", "/auth/api-keys/"+revokedID, nil, 204)
	body := `{"model":"wr21-model-0","messages":[{"role":"user","content":"WR21 recovery transport"}]}`
	invoke := func(t *testing.T, key string, want int) http.Header {
		return c.invoke(t, key, "tenant-0.wr21.test", "/v1/chat/completions", body, nil, want)
	}
	sub := func(name string, fn func(*testing.T)) {
		if !t.Run(name, fn) {
			t.FailNow()
		}
	}
	execIAM := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := c.owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("task IAM state injection failed: %v", err)
		}
	}
	currentMembershipVersion := func(t *testing.T) uint64 {
		t.Helper()
		var v uint64
		if err := c.owner.QueryRow(ctx, `SELECT version FROM tenant_memberships WHERE tenant_id=$1 AND id=$2`, tenant, membership).Scan(&v); err != nil {
			t.Fatal("membership version read failed")
		}
		return v
	}
	currentPrincipalVersion := func(t *testing.T) uint64 {
		t.Helper()
		var v uint64
		if err := c.owner.QueryRow(ctx, `SELECT version FROM workload_principals WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&v); err != nil {
			t.Fatal("Workload version read failed")
		}
		return v
	}
	recovery := func(t *testing.T) { invoke(t, secret, 200); invoke(t, revokedSecret, 401) }
	waitRecovery := func(t *testing.T) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		attempts := 0
		for time.Now().Before(deadline) {
			attempts++
			before, _ := c.sink.seen()
			req, err := http.NewRequest("POST", "http://"+c.address+"/v1/chat/completions", bytes.NewBufferString(body))
			if err != nil {
				t.Fatal("recovery probe request failed")
			}
			req.Host = "tenant-0.wr21.test"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+secret)
			r, err := c.http.Do(req)
			if err != nil {
				t.Fatal("recovery transport failed")
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			after, _ := c.sink.seen()
			if r.StatusCode == 200 {
				if after != before+1 {
					t.Fatal("recovered request did not reach backend once")
				}
				recordReference(t, c.run, map[string]any{"recovery": "pass", "attempts": attempts, "http_status": 200, "max_wait_seconds": 10})
				invoke(t, revokedSecret, 401)
				return
			}
			if (r.StatusCode != 503 && r.StatusCode != 504) || after != before {
				t.Fatalf("recovery transient status=%d sink_delta=%d", r.StatusCode, after-before)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("formal chain did not recover within 10 seconds")
	}
	sub("initial_chain_and_revoked_key", recovery)
	sub("expired_key", func(t *testing.T) {
		r, err := c.iam.admin.CreateAPIKey(ctx, &iamv1.CreateAPIKeyRequest{Credential: &iamv1.BearerCredential{Value: actor}, PrincipalId: principal, ExpiresAt: timestamppb.New(time.Now().Add(3 * time.Second)), IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatal("formal expiring Key creation failed")
		}
		invoke(t, r.GetApiKeySecret(), 200)
		time.Sleep(time.Until(r.GetApiKey().GetExpiresAt().AsTime()) + 100*time.Millisecond)
		invoke(t, r.GetApiKeySecret(), 401)
	})
	sub("membership_suspend_restore_does_not_revoke_key", func(t *testing.T) {
		for _, state := range []iamv1.MembershipStatus{iamv1.MembershipStatus_MEMBERSHIP_STATUS_SUSPENDED, iamv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE} {
			_, err := c.iam.admin.UpdateTenantMembership(ctx, &iamv1.UpdateTenantMembershipRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, Status: state, ExpectedVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
			if err != nil {
				t.Fatalf("formal Membership change failed code=%s", status.Code(err))
			}
			want := 403
			if state == iamv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE {
				want = 200
			}
			invoke(t, secret, want)
			var state string
			if err := c.owner.QueryRow(ctx, `SELECT status FROM api_keys WHERE tenant_id=$1 AND key_id=$2`, tenant, keyID).Scan(&state); err != nil || state != "active" {
				t.Fatal("membership suspension revoked a Key")
			}
		}
	})
	sub("role_permission_and_binding_next_decision", func(t *testing.T) {
		execIAM(t, `DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='inference-services' AND action='invoke'`, tenant, c.invokeRoles[0])
		invoke(t, secret, 403)
		execIAM(t, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','inference-services','invoke',now())`, tenant, c.invokeRoles[0])
		invoke(t, secret, 200)
		_, err := c.iam.admin.UnbindTenantRole(ctx, &iamv1.UnbindTenantRoleRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, RoleId: c.invokeRoles[0].String(), ExpectedMembershipVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatalf("formal Unbind failed code=%s", status.Code(err))
		}
		invoke(t, secret, 403)
		_, err = c.iam.admin.BindTenantRole(ctx, &iamv1.BindTenantRoleRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, RoleId: c.invokeRoles[0].String(), ExpectedMembershipVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatalf("formal Bind failed code=%s", status.Code(err))
		}
		recovery(t)
	})
	sub("creator_authority_is_not_a_workload_snapshot", func(t *testing.T) {
		execIAM(t, `UPDATE tenant_memberships SET status='suspended',version=version+1 WHERE tenant_id=$1 AND principal_id=$2`, tenant, c.humans[0])
		invoke(t, secret, 200)
		management(t, "GET", "/auth/tenant-workloads/"+principal, nil, 403)
		execIAM(t, `UPDATE tenant_memberships SET status='active',version=version+1 WHERE tenant_id=$1 AND principal_id=$2`, tenant, c.humans[0])
		recovery(t)
	})
	sub("tenant_access_and_lifecycle_current_availability", func(t *testing.T) {
		for _, state := range []string{"suspended", "bootstrap_pending"} {
			execIAM(t, `UPDATE tenant_access SET status=$2,version=version+1 WHERE tenant_id=$1`, tenant, state)
			invoke(t, secret, 403)
			execIAM(t, `UPDATE tenant_access SET status='active',version=version+1 WHERE tenant_id=$1`, tenant)
			recovery(t)
		}
		for _, state := range []string{"suspended", "terminated"} {
			execIAM(t, `UPDATE tenant_lifecycle_projections SET status=$2 WHERE tenant_id=$1`, tenant, state)
			invoke(t, secret, 403)
			execIAM(t, `UPDATE tenant_lifecycle_projections SET status='active' WHERE tenant_id=$1`, tenant)
			recovery(t)
		}
		var projection []byte
		if err := c.owner.QueryRow(ctx, `SELECT to_jsonb(p) FROM tenant_lifecycle_projections p WHERE tenant_id=$1`, tenant).Scan(&projection); err != nil {
			t.Fatal("projection snapshot failed")
		}
		execIAM(t, `DELETE FROM tenant_lifecycle_projections WHERE tenant_id=$1`, tenant)
		invoke(t, secret, 503)
		execIAM(t, `INSERT INTO tenant_lifecycle_projections SELECT * FROM jsonb_populate_record(NULL::tenant_lifecycle_projections,$1::jsonb)`, projection)
		recovery(t)
		execIAM(t, `UPDATE tenant_lifecycle_projections SET fresh_until=now()-interval '1 second' WHERE tenant_id=$1`, tenant)
		invoke(t, secret, 503)
		execIAM(t, `UPDATE tenant_lifecycle_projections SET fresh_until=now()+interval '1 hour' WHERE tenant_id=$1`, tenant)
		recovery(t)
	})
	sub("current_caller_binding_and_grant_revoke", func(t *testing.T) {
		// Test-owner authority restoration increments versions. It never restores
		// Tenant Key credentials or claims a new management platform API.
		for _, id := range []uuid.UUID{c.adapterBinding, c.receiverBinding} {
			execIAM(t, `UPDATE workload_identity_bindings SET status='revoked',version=version+1 WHERE id=$1`, id)
			invoke(t, secret, 401)
			execIAM(t, `UPDATE workload_identity_bindings SET status='active',version=version+1 WHERE id=$1`, id)
			recovery(t)
		}
		for _, row := range []struct {
			id        uuid.UUID
			operation string
		}{{c.adapterID, "inference.check_access"}, {c.receiverID, "inference.receive_access"}} {
			execIAM(t, `UPDATE workload_grants SET status='revoked',version=version+1 WHERE principal_id=$1 AND audience='ani-inference-service' AND operation=$2`, row.id, row.operation)
			invoke(t, secret, 403)
			execIAM(t, `UPDATE workload_grants SET status='active',version=version+1 WHERE principal_id=$1 AND audience='ani-inference-service' AND operation=$2`, row.id, row.operation)
			recovery(t)
		}
	})
	sub("workload_disable_enable_never_restores_old_key", func(t *testing.T) {
		old := secret
		management(t, "PATCH", "/auth/tenant-workloads/"+principal, map[string]any{"status": "disabled", "expected_version": currentPrincipalVersion(t)}, 200)
		invoke(t, old, 401)
		management(t, "PATCH", "/auth/tenant-workloads/"+principal, map[string]any{"status": "active", "expected_version": currentPrincipalVersion(t)}, 200)
		invoke(t, old, 401)
		secret, keyID = createKey(t)
		recovery(t)
	})
	sub("restricted_management_and_database_invariants", func(t *testing.T) {
		code, _, _ := c.request(t, client, "GET", "/auth/tenant-workloads/"+principal, secret, nil, nil)
		if code != 403 {
			t.Fatalf("API Key managed IAM: status=%d", code)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE principals SET principal_type='human' WHERE id=$1`, []any{principal}},
			{`UPDATE workload_principals SET name='renamed',normalized_name='renamed' WHERE principal_id=$1`, []any{principal}},
			{`UPDATE workload_principals SET tenant_id=$2 WHERE principal_id=$1`, []any{principal, referenceTenants[1]}},
			{`UPDATE workload_principals SET owner_type='platform',tenant_id=NULL,membership_id=NULL,environment='wr21-authentication',trust_domain='wr21.test' WHERE principal_id=$1`, []any{principal}},
			{`UPDATE workload_principals SET membership_id=NULL WHERE principal_id=$1`, []any{principal}},
			{`UPDATE workload_grants SET principal_id=$2 WHERE principal_id=$1`, []any{c.adapterID, principal}},
		} {
			_, err := c.owner.Exec(ctx, q.sql, q.args...)
			pg, ok := err.(*pgconn.PgError)
			if !ok || (pg.Code != "23514" && pg.Code != "23503") {
				t.Fatalf("immutable authority constraint not rejected: %T", err)
			}
		}
		recovery(t)
	})
	sub("create_revoke_rotate_concurrent_with_real_use", func(t *testing.T) {
		oldSecret, oldID := createKey(t)
		invoke(t, oldSecret, 200)
		start := make(chan struct{})
		var wg sync.WaitGroup
		codesSeen := make(chan int, 3)
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				req, err := http.NewRequest("POST", "http://"+c.address+"/v1/chat/completions", bytes.NewBufferString(body))
				if err != nil {
					codesSeen <- 0
					return
				}
				req.Host = "tenant-0.wr21.test"
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+oldSecret)
				r, err := c.http.Do(req)
				if err != nil {
					codesSeen <- 0
					return
				}
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				codesSeen <- r.StatusCode
			}()
		}
		beforeUse, _ := c.sink.seen()
		close(start)
		nextSecret, nextID := createKey(t)
		management(t, "DELETE", "/auth/api-keys/"+oldID, nil, 204)
		wg.Wait()
		close(codesSeen)
		counts := map[int]int{}
		for code := range codesSeen {
			if code != 200 && code != 401 && code != 403 {
				t.Fatalf("in-flight revoke status=%d", code)
			}
			counts[code]++
		}
		afterUse, _ := c.sink.seen()
		if afterUse-beforeUse != counts[200] {
			t.Fatal("concurrent rejected request reached backend")
		}
		invoke(t, oldSecret, 401)
		invoke(t, nextSecret, 200)
		management(t, "DELETE", "/auth/api-keys/"+nextID, nil, 204)
		invoke(t, nextSecret, 401)
		recovery(t)
		recordReference(t, c.run, map[string]any{"scenario": "create-revoke-rotate-concurrent-use", "inflight_status_counts": counts, "after_revoke_status": 401, "new_key_status": 200, "atomic_rotation": false})
	})
	sub("iam_redis_key_creation_fails_closed_and_recovers", func(t *testing.T) {
		intent := map[string]any{"principal_id": principal, "never_expires": true}
		idem := uuid.NewString()
		var before int
		if err := c.owner.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&before); err != nil {
			t.Fatal("Key count unavailable")
		}
		wr21ContainerFault(t, c.redisContainer.GetContainerID(), func() {
			code, _, _ := c.request(t, client, "POST", "/auth/api-keys", actor, intent, map[string]string{"Idempotency-Key": idem})
			if code != 503 {
				t.Fatalf("IAM Redis failure management status=%d", code)
			}
		})
		var after int
		if err := c.owner.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&after); err != nil || after != before {
			t.Fatal("Redis failure created a Key")
		}
		code, r, _ := c.request(t, client, "POST", "/auth/api-keys", actor, intent, map[string]string{"Idempotency-Key": idem})
		if code != 201 {
			t.Fatalf("Redis restoration retry status=%d", code)
		}
		invoke(t, r["secret"].(string), 200)
		recovery(t)
	})
	sub("owner_rate_limit_retry_after", func(t *testing.T) {
		if _, err := c.db.owner.Exec(ctx, `UPDATE inference_access_policies SET rate_rpm=1 WHERE tenant_id=$1 AND id=$2`, tenant, c.db.policies[0]); err != nil {
			t.Fatal("owner limit fixture failed")
		}
		rateSecret, rateID := createKey(t)
		invoke(t, rateSecret, 200)
		h := invoke(t, rateSecret, 429)
		if h.Get("Retry-After") == "" {
			t.Fatal("owner rate limit missing Retry-After")
		}
		if _, err := c.db.owner.Exec(ctx, `UPDATE inference_access_policies SET rate_rpm=1000 WHERE tenant_id=$1 AND id=$2`, tenant, c.db.policies[0]); err != nil {
			t.Fatal("owner limit restoration failed")
		}
		invoke(t, rateSecret, 200)
		management(t, "DELETE", "/auth/api-keys/"+rateID, nil, 204)
		recovery(t)
	})
	sub("owner_redis_failure_and_restart", func(t *testing.T) {
		id := c.ownerRedisContainer.GetContainerID()
		wr21ContainerFault(t, id, func() { invoke(t, secret, 504) })
		recovery(t)
		c.ownerProcess.stop()
		invoke(t, secret, 503)
		c.ownerProcess = startReferenceProcess(root, c.run, "wr21-inference-restart-"+uuid.NewString(), c.ownerEnv, "inference-service")
		wr21WaitTCP(t, c.ownerAddress, c.ownerProcess)
		waitRecovery(t)
	})
	sub("iam_and_owner_database_failure", func(t *testing.T) {
		wr21ContainerFault(t, c.postgresID, func() { invoke(t, secret, 504) })
		waitRecovery(t)
		wr21ContainerFault(t, c.db.container.GetContainerID(), func() { invoke(t, secret, 504) })
		waitRecovery(t)
	})
	sub("iam_and_adapter_process_failure_and_restart", func(t *testing.T) {
		if err := c.iam.process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal("task IAM pause failed")
		}
		func() { defer c.iam.process.Signal(syscall.SIGCONT); invoke(t, secret, 504) }()
		waitRecovery(t)
		c.adapterProcess.stop()
		invoke(t, secret, 503)
		c.adapterProcess = startReferenceProcess(root, c.run, "wr21-adapter-restart-"+uuid.NewString(), c.adapterEnv, "envoy-authz-adapter")
		wr21WaitTCP(t, c.adapterAddress, c.adapterProcess)
		waitRecovery(t)
		c.iam.stop()
		wrapper := filepath.Join(c.run, "private/iam-restart")
		writeReferencePrivate(t, wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec '%s' -conf '%s'\n", c.iam.binary, filepath.Join(c.iam.directory, "runtime.json"))))
		if err := os.Chmod(wrapper, 0700); err != nil {
			t.Fatal("restart permissions failed")
		}
		p := startReferenceProcess(root, c.run, "wr21-iam-restart-"+uuid.NewString(), nil, "iam-restart")
		waitReferenceHTTP(t, c.iam.readinessURL, p)
		waitRecovery(t)
	})
	sub("removed_membership_terminal_and_old_key_stays_revoked", func(t *testing.T) {
		_, err := c.iam.admin.RemoveTenantMembership(ctx, &iamv1.RemoveTenantMembershipRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, ExpectedVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatalf("formal removal failed code=%s", status.Code(err))
		}
		invoke(t, secret, 401)
		var remaining int
		if err := c.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status<>'removed'`, tenant, principal).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatal("removed Workload retained current membership")
		}
		_, err = c.iam.admin.UpdateTenantMembership(ctx, &iamv1.UpdateTenantMembershipRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, Status: iamv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE, ExpectedVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("removed membership restored or wrong error %s", status.Code(err))
		}
		oldMembership, oldSecret := membership, secret
		enabled := management(t, "PATCH", "/auth/tenant-workloads/"+principal, map[string]any{"status": "active", "expected_version": currentPrincipalVersion(t)}, 200)
		membership = enabled["membership_id"].(string)
		if membership == "" || membership == oldMembership {
			t.Fatal("Enable revived removed membership")
		}
		invoke(t, oldSecret, 401)
		secret, keyID = createKey(t)
		invoke(t, secret, 403)
		_, err = c.iam.admin.BindTenantRole(ctx, &iamv1.BindTenantRoleRequest{Credential: &iamv1.BearerCredential{Value: actor}, TenantId: tenant.String(), MembershipId: membership, RoleId: c.invokeRoles[0].String(), ExpectedMembershipVersion: currentMembershipVersion(t), IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatalf("new Membership Bind failed code=%s", status.Code(err))
		}
		recovery(t)
		invoke(t, oldSecret, 401)
	})
	recordReference(t, c.run, map[string]any{"stage": "C", "selected_recovery_set": "pass", "subject": principal, "key_id": keyID, "additional_binding_matrix_evidence": "stage-B-formal-owner-protocol-probe"})
}
func wr21ContainerFault(t *testing.T, id string, check func()) {
	t.Helper()
	if id == "" {
		t.Fatal("unregistered container")
	}
	if err := exec.Command("docker", "pause", id).Run(); err != nil {
		t.Fatal("task container pause failed")
	}
	defer func() {
		if err := exec.Command("docker", "unpause", id).Run(); err != nil {
			t.Error("task container restore failed")
		}
	}()
	check()
}
