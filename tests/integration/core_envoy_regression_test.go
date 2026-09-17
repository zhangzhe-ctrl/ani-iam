//go:build integration && !governance

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Reuse WR21's actual Envoy and Inference owner harness, with WR23's real
// Core-created tenants and invited accounts. Static published model resources
// remain owner prerequisites, not evidence of model execution or deployment.
func TestWR23ResumeFormalEnvoyInference(t *testing.T) {
	started := time.Now().UTC()
	run, goal := isolatedRun(t)
	if goal != "wr23" || os.Getenv("WR23_FORMAL_EXTENSION") != "envoy" {
		t.Fatal("dedicated WR23 Envoy budget required")
	}
	c := &wr21Chain{sink: &wr21Sink{}, http: &http.Client{Timeout: 8 * time.Second}}
	c.ownerAddress = reserveIAMProcessLoopbackAddress(t)
	c.adapterAddress = reserveIAMProcessLoopbackAddress(t)
	c.address = reserveIAMProcessLoopbackAddress(t)
	c.envoyAdmin = reserveIAMProcessLoopbackAddress(t)
	sink := httptest.NewServer(http.HandlerFunc(c.sink.serve))
	t.Cleanup(sink.Close)
	var caFile, envoyCert, envoyKey, extCert, extKey, adapterCert, adapterKey, ownerCert, ownerKey string
	e := newWR23FormalEnvironment(t, func(dir string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, m *biz.WorkloadBootstrapManifest) {
		caFile = cfg.Server.Grpc.Tls.ClientCaFile
		adapterCert, adapterKey = writeProcessE2ELeafCertificate(t, dir, "adapter-client", "adapter.wr23.test", x509.ExtKeyUsageClientAuth, ca, key)
		ownerCert, ownerKey = wr23DualCertificate(t, dir, "inference.wr23.test", ca, key)
		envoyCert, envoyKey = writeProcessE2ELeafCertificate(t, dir, "envoy-client", "envoy.wr21.test", x509.ExtKeyUsageClientAuth, ca, key)
		extCert, extKey = writeProcessE2ELeafCertificate(t, dir, "extauth-server", "extauth.wr21.test", x509.ExtKeyUsageServerAuth, ca, key)
		for _, method := range []string{"CreateTenantWorkload", "GetTenantWorkload", "UpdateTenantWorkload", "CreateAPIKey", "RevokeAPIKey"} {
			m.Workloads[0].Grants = append(m.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		}
		adapter := wr23Workload(t, "adapter", "wr23.test",
			[2]string{"ani-iam", "/iam.v1.AuthenticationService/ValidatePrincipal"},
			[2]string{"ani-iam", "/iam.v1.AuthorizationService/CheckPermission"},
			[2]string{"ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"},
			[2]string{"ani-iam", "/iam.v1.AuthenticationService/IssueDelegation"},
			[2]string{biz.InferenceInvocationAudience, biz.InferenceInvocationOperation})
		receiver := wr23Workload(t, "inference", "wr23.test",
			[2]string{"ani-iam", "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"},
			[2]string{biz.InferenceInvocationAudience, biz.InferenceReceiverOperation})
		c.adapterID, c.adapterBinding, c.receiverID, c.receiverBinding = adapter.PrincipalID, adapter.BindingID, receiver.PrincipalID, receiver.BindingID
		m.Workloads = append(m.Workloads, adapter, receiver)
	})
	actors := wr23RunFormalLifecycleChain(t, e)
	c.wr21Environment = &wr21Environment{wr20Environment: e.console}
	ctx := context.Background()
	bossAccess := e.access(t, actors.boss)
	code, second, _ := e.boss.request(t, actors.boss, "POST", "/admin/tenants", bossAccess, map[string]any{"name": "wr23-inference-other-" + uuid.NewString()[:12], "display_name": "WR23 other resource Tenant", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "other-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != 200 {
		t.Fatalf("second real Core Tenant status=%d reason=%v", code, second["code"])
	}
	tenants := []uuid.UUID{uuid.MustParse(actors.tenant), uuid.MustParse(second["id"].(string))}
	c.db = newWR21InferenceDB(t, run, tenants...)
	c.ownerRedis, c.ownerRedisContainer = newIsolatedRedis(t, ctx)
	c.ownerEnv = map[string]string{"INFERENCE_ACCESS_CHECK_ONLY": "true", "INFERENCE_ACCESS_LISTEN_ADDR": c.ownerAddress, "INFERENCE_DATABASE_URL": c.db.runtimeDSN, "REDIS_URL": referenceRedisURL(c.ownerRedis), "IAM_ADDR": e.boss.iam.config.Server.Grpc.Addr, "IAM_SERVER_NAME": "iam.wr17-18.test", "IAM_ENVIRONMENT": "wr23-lifecycle", "IAM_TRUST_DOMAIN": "wr23.test", "IAM_POLICY_REVISION": data.TargetPolicyRevision, "IAM_CLIENT_CERT": ownerCert, "IAM_CLIENT_KEY": ownerKey, "IAM_CA_CERT": caFile}
	c.adapterEnv = map[string]string{"GRPC_LISTEN_ADDR": c.adapterAddress, "IAM_SERVICE_GRPC_ADDR": e.boss.iam.config.Server.Grpc.Addr, "INFERENCE_SERVICE_GRPC_ADDR": c.ownerAddress, "IAM_POLICY_REVISION": data.TargetPolicyRevision, "IAM_CHAT_OPERATION_ID": "invokeInferenceChatCompletions", "IAM_ENVIRONMENT": "wr23-lifecycle", "IAM_TRUST_DOMAIN": "wr23.test", "INFERENCE_TLS_SERVER_NAME": "inference.wr23.test", "ENVOY_PEER_DNS": "envoy.wr21.test", "ENVOY_TLS_CA_FILE": caFile, "ENVOY_TLS_CERT_FILE": extCert, "ENVOY_TLS_KEY_FILE": extKey, "IAM_TLS_CA_FILE": caFile, "IAM_TLS_CERT_FILE": adapterCert, "IAM_TLS_KEY_FILE": adapterKey, "IAM_TLS_SERVER_NAME": "iam.wr17-18.test"}
	c.ownerProcess = startReferenceProcess(t, run, "wr23-inference-"+uuid.NewString(), c.ownerEnv, "inference-service")
	wr21WaitTCP(t, c.ownerAddress, c.ownerProcess)
	c.adapterProcess = startReferenceProcess(t, run, "wr23-adapter-"+uuid.NewString(), c.adapterEnv, "envoy-authz-adapter")
	wr21WaitTCP(t, c.adapterAddress, c.adapterProcess)
	raw := wr21EnvoyConfig(t, c.address, c.envoyAdmin, c.adapterAddress, strings.TrimPrefix(sink.URL, "http://"), caFile, envoyCert, envoyKey, c.db.resources, tenants...)
	writeReferencePrivate(t, filepath.Join(run, "private/envoy.json"), raw)
	wrapper := filepath.Join(run, "private/envoy-run")
	writeReferencePrivate(t, wrapper, []byte("#!/bin/sh\nexec \"$(dirname \"$0\")/envoy\" -c \"$(dirname \"$0\")/envoy.json\" --concurrency 2 --log-level warn\n"))
	if os.Chmod(wrapper, 0700) != nil {
		t.Fatal("owned Envoy wrapper mode")
	}
	c.envoyProcess = startReferenceProcess(t, run, "wr23-envoy-"+uuid.NewString(), nil, "envoy-run")
	wr21WaitTCP(t, c.address, c.envoyProcess)
	management := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, doc, _ := e.console.request(t, actors.admin, method, path, actors.adminToken, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("formal management %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	role := management("POST", "/iam/tenants/"+actors.tenant+"/roles", map[string]any{"name": "WR23 model invoker", "permissions": []string{"inference-services/invoke"}}, 201)["role_id"].(string)
	w := management("POST", "/auth/tenant-workloads", map[string]any{"tenant_id": actors.tenant, "name": "WR23 actual invited admin bot", "role_ids": []string{role}}, 201)
	principal := w["principal_id"].(string)
	key := management("POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true}, 201)
	secret := key["secret"].(string)
	keyID := key["api_key"].(map[string]any)["key_id"].(string)
	body := `{"model":"wr21-model-0","messages":[{"role":"user","content":"WR23 current Core fact regression"}]}`
	invoke := func(secret, host string, want int) {
		t.Helper()
		c.invoke(t, secret, host, "/v1/chat/completions", body, nil, want)
	}
	c.invoke(t, secret, "tenant-0.wr21.test", "/v1/chat/completions", body, map[string]string{"x-ani-principal-id": "spoof", "x-ani-tenant-id": tenants[1].String(), "x-api-key": "spoof", "cookie": "spoof"}, 200)
	_, headers := c.sink.seen()
	if headers.Get("x-ani-principal-id") != principal || headers.Get("x-ani-tenant-id") != actors.tenant || headers.Get("x-ani-principal-type") != "workload" || headers.Get("Authorization") != "" || headers.Get("Cookie") != "" || headers.Get("x-api-key") != "" {
		t.Fatal("Envoy trusted downstream identity boundary")
	}
	invoke("", "tenant-0.wr21.test", 401)
	invoke(secret+"forged", "tenant-0.wr21.test", 401)
	invoke(secret, "tenant-1.wr21.test", 403)
	invoke(secret, "tenant-2.wr21.test", 404)
	// An owner policy remains an independent authority after IAM success.
	if _, err := c.db.owner.Exec(ctx, `INSERT INTO inference_access_policy_api_keys(policy_id,tenant_id,api_key_id,key_prefix,effect) VALUES($1,$2,$3,'wr23','deny')`, c.db.policies[0], tenants[0], keyID); err != nil {
		t.Fatal("owner negative policy setup")
	}
	invoke(secret, "tenant-0.wr21.test", 403)
	var denials int
	if err := c.db.owner.QueryRow(ctx, `SELECT count(*) FROM inference_access_policy_events WHERE tenant_id=$1 AND inference_service_id=$2 AND api_key_id=$3 AND policy_id=$4 AND decision='deny'`, tenants[0], c.db.resources[0], keyID, c.db.policies[0]).Scan(&denials); err != nil || denials != 1 {
		t.Fatal("missing current owner denial audit")
	}
	if _, err := c.db.owner.Exec(ctx, `DELETE FROM inference_access_policy_api_keys WHERE policy_id=$1 AND tenant_id=$2 AND api_key_id=$3 AND effect='deny'`, c.db.policies[0], tenants[0], keyID); err != nil {
		t.Fatal("own owner negative policy cleanup")
	}
	invoke(secret, "tenant-0.wr21.test", 200)
	expectedLifecycleVersion := int64(1)
	earlyRejection := "not_verified"
	if e.projectionMode == "enforced" {
		ownerEvents := func() int {
			t.Helper()
			var n int
			if c.db.owner.QueryRow(ctx, `SELECT count(*) FROM inference_access_policy_events WHERE tenant_id=$1 AND api_key_id=$2`, tenants[0], keyID).Scan(&n) != nil {
				t.Fatal("owner event observation")
			}
			return n
		}
		priorEvents := ownerEvents()
		priorSink, _ := c.sink.seen()
		change := func(action, state string, version int64) {
			t.Helper()
			code, doc, _ := e.boss.request(t, actors.boss, "POST", "/admin/tenants/"+actors.tenant+"/"+action, bossAccess, map[string]any{"version": version, "idempotency_key": uuid.NewString()}, nil)
			if code != 200 || doc["status"] != state || doc["lifecycle_version"] != float64(version+1) {
				t.Fatalf("enforced actual Core %s status=%d", action, code)
			}
			until := time.Now().Add(10 * time.Second)
			for time.Now().Before(until) {
				var ready bool
				if e.boss.owner.QueryRow(ctx, `SELECT status=$3 AND lifecycle_version=$4 AND NOT repair_required AND fresh_until>clock_timestamp() FROM core_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2`, e.broker.configuration.Authority.Producer, tenants[0], state, version+1).Scan(&ready) == nil && ready {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatal("enforced actual current lifecycle propagation")
		}
		change("freeze", "frozen", 1)
		invoke(secret, "tenant-0.wr21.test", 403)
		afterSink, _ := c.sink.seen()
		if ownerEvents() != priorEvents || afterSink != priorSink {
			t.Fatal("frozen request reached resource owner or protected sink instead of IAM early rejection")
		}
		change("unfreeze", "active", 2)
		invoke(secret, "tenant-0.wr21.test", 200)
		// Successful owner checks do not emit AccessPolicyEvent rows. Prove
		// restored owner authority with its actual deny policy and deny audit.
		if _, err := c.db.owner.Exec(ctx, `INSERT INTO inference_access_policy_api_keys(policy_id,tenant_id,api_key_id,key_prefix,effect) VALUES($1,$2,$3,'wr23','deny')`, c.db.policies[0], tenants[0], keyID); err != nil {
			t.Fatal("restored owner negative policy setup")
		}
		invoke(secret, "tenant-0.wr21.test", 403)
		if ownerEvents() != priorEvents+1 {
			t.Fatal("restored owner denial audit missing")
		}
		if _, err := c.db.owner.Exec(ctx, `DELETE FROM inference_access_policy_api_keys WHERE policy_id=$1 AND tenant_id=$2 AND api_key_id=$3 AND effect='deny'`, c.db.policies[0], tenants[0], keyID); err != nil {
			t.Fatal("restored owner negative policy cleanup")
		}
		invoke(secret, "tenant-0.wr21.test", 200)
		expectedLifecycleVersion = 3
		earlyRejection = "pass"
	}
	management("DELETE", "/auth/api-keys/"+keyID, nil, 204)
	invoke(secret, "tenant-0.wr21.test", 401)
	var fresh bool
	if err := e.boss.owner.QueryRow(ctx, `SELECT status='active' AND lifecycle_version=$3 AND NOT repair_required AND fresh_until>clock_timestamp() FROM core_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2`, e.broker.configuration.Authority.Producer, tenants[0], expectedLifecycleVersion).Scan(&fresh); err != nil || !fresh {
		t.Fatal("real current Core projection unavailable during regression")
	}
	result := map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "tenant_id": actors.tenant, "other_tenant_id": tenants[1], "human_id": actors.adminID, "workload_id": principal, "key_id": keyID, "caller_id": c.adapterID, "receiver_id": c.receiverID, "owner_resource": c.db.resources[0], "owner_denial_events": denials, "envoy_config_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "grade": "real Core/NATS/onboarding/Gateway management and Envoy/adapter/IAM/Inference owner authorization; static owner resource prerequisites; no model execution", "projection_mode": e.projectionMode, "iam_early_rejection": earlyRejection}
	public, _ := json.MarshalIndent(result, "", "  ")
	if os.WriteFile(filepath.Join(run, "formal-envoy-results.json"), append(public, '\n'), 0600) != nil {
		t.Fatal("persist Envoy regression evidence")
	}
}
