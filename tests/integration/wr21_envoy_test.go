//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type wr21InferenceDB struct {
	owner               *pgxpool.Pool
	runtimeDSN          string
	container           testcontainers.Container
	resources, policies []uuid.UUID
}

func newWR21InferenceDB(t *testing.T, run string, tenants ...uuid.UUID) *wr21InferenceDB {
	t.Helper()
	ctx := context.Background()
	c, err := postgres.Run(ctx, postgresImage, postgres.WithDatabase("wr21_inference"), postgres.WithUsername("postgres"), postgres.WithPassword(randomPassword(t)), postgres.BasicWaitStrategies(), isolatedContainer(t, "inference-postgres", "5432/tcp"))
	if err != nil {
		t.Fatal("Inference PostgreSQL start failed")
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(c); err != nil {
			t.Error("Inference PostgreSQL cleanup failed")
		}
	})
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal("Inference PostgreSQL endpoint unavailable")
	}
	d := &wr21InferenceDB{owner: mustPool(t, dsn), container: c}
	t.Cleanup(d.owner.Close)
	for _, name := range []string{"20260501000100_init_schema.sql", "20260814000100_inference_control_plane.sql", "20260831_001_inference_gateway_publication.sql", "20260828_001_inference_access_policy.sql", "20260901_001_inference_access_policy_idempotency.sql"} {
		raw, err := os.ReadFile(filepath.Join(run, "source-ani/repo/deploy/migrations", name))
		if err != nil {
			t.Fatal("owner migration missing")
		}
		if _, err = d.owner.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("owner migration %s: %v", name, err)
		}
		recordReference(t, run, map[string]any{"kind": "inference-migration", "path": name, "sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "pass": true})
	}
	// Current owner CREATE TABLE already has scope/allow/deny and the 3-part PK.
	// The historical conversion migration is inapplicable to this clean schema;
	// its PostgreSQL syntax defect is separately recorded, never patched here.
	var keyPK string
	if err := d.owner.QueryRow(ctx, `SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c WHERE c.conrelid='inference_access_policy_api_keys'::regclass AND c.contype='p'`).Scan(&keyPK); err != nil || keyPK != "PRIMARY KEY (policy_id, api_key_id, effect)" {
		t.Fatal("owner clean-install Key effects constraint differs")
	}
	recordReference(t, run, map[string]any{"owner_key_effects_constraint": keyPK, "clean_install": true, "historical_conversion": "not_applicable; source syntax defect separately recorded"})
	password := randomPassword(t)
	// Dedicated runtime login has only the selected owner's read/event privileges.
	_, err = d.owner.Exec(ctx, "CREATE ROLE wr21_inference_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'; GRANT CONNECT ON DATABASE wr21_inference TO wr21_inference_runtime; GRANT USAGE ON SCHEMA public TO wr21_inference_runtime; GRANT SELECT ON inference_services,inference_operations,inference_access_policies,inference_access_policy_services,inference_access_policy_api_keys TO wr21_inference_runtime; GRANT INSERT ON inference_access_policy_events TO wr21_inference_runtime")
	if err != nil {
		t.Fatal("restricted owner role setup failed")
	}
	parsed, _ := url.Parse(dsn)
	parsed.User = url.UserPassword("wr21_inference_runtime", password)
	d.runtimeDSN = parsed.String()
	runtimePool := mustPool(t, d.runtimeDSN)
	defer runtimePool.Close()
	var privileged bool
	if err = runtimePool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR oid=(SELECT datdba FROM pg_database WHERE datname=current_database()) FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		t.Fatal("Inference runtime is privileged")
	}
	if len(tenants) == 0 {
		tenants = referenceTenants[:]
	}
	for i, tenant := range tenants {
		model, version, resource, policy := mustV7(t), mustV7(t), mustV7(t), mustV7(t)
		d.resources = append(d.resources, resource)
		d.policies = append(d.policies, policy)
		name := fmt.Sprintf("wr21-model-%d", i)
		if _, err = d.owner.Exec(ctx, `INSERT INTO tenants(id,name,display_name) VALUES($1,$2,$2)`, tenant, "wr21-inference-"+strconv.Itoa(i)); err != nil {
			t.Fatal("owner Tenant seed failed")
		}
		if _, err = d.owner.Exec(ctx, `INSERT INTO models(id,tenant_id,name,display_name,status) VALUES($1,$2,$3,$3,'ready')`, model, tenant, name); err != nil {
			t.Fatal("owner model prerequisite failed")
		}
		if _, err = d.owner.Exec(ctx, `INSERT INTO model_versions(id,model_id,version,storage_path) VALUES($1,$2,'wr21','wr21-fixture-not-model-execution')`, version, model); err != nil {
			t.Fatal("owner version prerequisite failed")
		}
		if _, err = d.owner.Exec(ctx, `INSERT INTO inference_services(id,tenant_id,name,served_model_name,model_version_id,status,desired_state,desired_spec,applied_spec,generation,observed_generation,publication_desired,publication_phase,publication_generation,publication_observed_generation,invocation_url) VALUES($1,$2,$3,$3,$4,'running','running','{"replicas":1,"execution_profile":{"task":"generate"}}','{"replicas":1,"execution_profile":{"task":"generate"}}',1,1,'published','published',1,1,$5)`, resource, tenant, name, version, fmt.Sprintf("http://tenant-%d.wr21.test/v1/chat/completions", i)); err != nil {
			t.Fatalf("owner resource seed failed: %v", err)
		}
		if _, err = d.owner.Exec(ctx, `INSERT INTO inference_access_policies(id,tenant_id,name,status,scope_type,allow_all_tenant_keys,rate_qps,rate_rpm) VALUES($1,$2,'wr21-policy','enabled','tenant_default',true,100,1000)`, policy, tenant); err != nil {
			t.Fatal("owner policy prerequisite failed")
		}
	}
	recordReference(t, run, map[string]any{"kind": "owner-prerequisites", "owner": "formal Inference PostgreSQL schema and repository", "resources": d.resources, "policies": d.policies, "runtime_role": "wr21_inference_runtime", "runtime_privileged": false, "seed_layer": "Tenant/model/version/published resource/policy fixtures; no deployment/model execution claim"})
	return d
}

func wr21DualCertificate(t *testing.T, dir, dns string, ca *x509.Certificate, key ed25519.PrivateKey) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("leaf key generation failed")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal("leaf serial failed")
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: dns}, DNSNames: []string{dns}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, pub, key)
	if err != nil {
		t.Fatal("dual leaf creation failed")
	}
	raw, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal("dual leaf serialization failed")
	}
	cert, k := filepath.Join(dir, dns+".pem"), filepath.Join(dir, dns+"-key.pem")
	writeProcessE2EPEM(t, cert, "CERTIFICATE", der)
	writeProcessE2EPEM(t, k, "PRIVATE KEY", raw)
	return cert, k
}

type wr21Sink struct {
	mu      sync.Mutex
	count   int
	headers http.Header
}

func (s *wr21Sink) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	s.headers = r.Header.Clone()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"transport":"wr21-protected-sink","model_execution":false}`))
}
func (s *wr21Sink) seen() (int, http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count, s.headers.Clone()
}

type wr21Chain struct {
	*wr21Environment
	db                                                     *wr21InferenceDB
	ownerRedis                                             *redis.Client
	ownerRedisContainer                                    testcontainers.Container
	sink                                                   *wr21Sink
	http                                                   *http.Client
	address, ownerAddress, adapterAddress, envoyAdmin      string
	adapterID, adapterBinding, receiverID, receiverBinding uuid.UUID
	ownerProcess, adapterProcess, envoyProcess             *referenceProcess
	ownerEnv, adapterEnv                                   map[string]string
}

func newWR21Chain(t *testing.T) *wr21Chain {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr21" && goal != "wr32" {
		t.Fatal("WR21 run required")
	}
	c := &wr21Chain{db: newWR21InferenceDB(t, run), sink: &wr21Sink{}, http: &http.Client{Timeout: 8 * time.Second}}
	c.ownerRedis, c.ownerRedisContainer = newIsolatedRedis(t, context.Background())
	c.ownerAddress = reserveIAMProcessLoopbackAddress(t)
	c.adapterAddress = reserveIAMProcessLoopbackAddress(t)
	c.address = reserveIAMProcessLoopbackAddress(t)
	c.envoyAdmin = reserveIAMProcessLoopbackAddress(t)
	sink := httptest.NewServer(http.HandlerFunc(c.sink.serve))
	t.Cleanup(sink.Close)
	var caFile, envoyCert, envoyKey, extCert, extKey, adapterCert, adapterKey, ownerCert, ownerKey string
	c.wr21Environment = newWR21Environment(t, func(dir string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, m *biz.WorkloadBootstrapManifest) {
		caFile = cfg.Server.Grpc.Tls.ClientCaFile
		adapterCert, adapterKey = writeProcessE2ELeafCertificate(t, dir, "adapter-client", "adapter.wr21.test", x509.ExtKeyUsageClientAuth, ca, key)
		ownerCert, ownerKey = wr21DualCertificate(t, dir, "inference.wr21.test", ca, key)
		envoyCert, envoyKey = writeProcessE2ELeafCertificate(t, dir, "envoy-client", "envoy.wr21.test", x509.ExtKeyUsageClientAuth, ca, key)
		extCert, extKey = writeProcessE2ELeafCertificate(t, dir, "extauth-server", "extauth.wr21.test", x509.ExtKeyUsageServerAuth, ca, key)
		for i, dns := range []string{"adapter.wr21.test", "inference.wr21.test"} {
			w := biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: []string{"wr21-adapter", "wr21-inference"}[i], DNSIdentity: dns}
			methods := []string{"/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}
			audience, operation := biz.InferenceInvocationAudience, biz.InferenceReceiverOperation
			if i == 0 {
				methods = []string{"/iam.v1.AuthenticationService/ValidatePrincipal", "/iam.v1.AuthorizationService/CheckPermission", "/iam.v1.AuthenticationService/IssueWorkloadToken", "/iam.v1.AuthenticationService/IssueDelegation"}
				operation = biz.InferenceInvocationOperation
				c.adapterID, c.adapterBinding = w.PrincipalID, w.BindingID
			} else {
				c.receiverID, c.receiverBinding = w.PrincipalID, w.BindingID
			}
			for _, method := range methods {
				w.Grants = append(w.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: method})
			}
			w.Grants = append(w.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: audience, Operation: operation})
			m.Workloads = append(m.Workloads, w)
		}
	})
	c.ownerEnv = map[string]string{"INFERENCE_ACCESS_CHECK_ONLY": "true", "INFERENCE_ACCESS_LISTEN_ADDR": c.ownerAddress, "INFERENCE_DATABASE_URL": c.db.runtimeDSN, "REDIS_URL": referenceRedisURL(c.ownerRedis), "IAM_ADDR": c.iam.config.Server.Grpc.Addr, "IAM_SERVER_NAME": "iam.wr17-18.test", "IAM_ENVIRONMENT": "wr21-authentication", "IAM_TRUST_DOMAIN": "wr21.test", "IAM_POLICY_REVISION": data.TargetPolicyRevision, "IAM_CLIENT_CERT": ownerCert, "IAM_CLIENT_KEY": ownerKey, "IAM_CA_CERT": caFile}
	c.adapterEnv = map[string]string{"GRPC_LISTEN_ADDR": c.adapterAddress, "IAM_SERVICE_GRPC_ADDR": c.iam.config.Server.Grpc.Addr, "INFERENCE_SERVICE_GRPC_ADDR": c.ownerAddress, "IAM_POLICY_REVISION": data.TargetPolicyRevision, "IAM_CHAT_OPERATION_ID": "invokeInferenceChatCompletions", "IAM_ENVIRONMENT": "wr21-authentication", "IAM_TRUST_DOMAIN": "wr21.test", "INFERENCE_TLS_SERVER_NAME": "inference.wr21.test", "ENVOY_PEER_DNS": "envoy.wr21.test", "ENVOY_TLS_CA_FILE": caFile, "ENVOY_TLS_CERT_FILE": extCert, "ENVOY_TLS_KEY_FILE": extKey, "IAM_TLS_CA_FILE": caFile, "IAM_TLS_CERT_FILE": adapterCert, "IAM_TLS_KEY_FILE": adapterKey, "IAM_TLS_SERVER_NAME": "iam.wr17-18.test"}
	c.ownerProcess = startReferenceProcess(t, run, "wr21-inference-"+uuid.NewString(), c.ownerEnv, "inference-service")
	wr21WaitTCP(t, c.ownerAddress, c.ownerProcess)
	c.adapterProcess = startReferenceProcess(t, run, "wr21-adapter-"+uuid.NewString(), c.adapterEnv, "envoy-authz-adapter")
	wr21WaitTCP(t, c.adapterAddress, c.adapterProcess)
	raw := wr21EnvoyConfig(t, c.address, c.envoyAdmin, c.adapterAddress, strings.TrimPrefix(sink.URL, "http://"), caFile, envoyCert, envoyKey, c.db.resources)
	configFile := filepath.Join(run, "private/envoy.json")
	writeReferencePrivate(t, configFile, raw)
	wrapper := filepath.Join(run, "private/envoy-run")
	writeReferencePrivate(t, wrapper, []byte("#!/bin/sh\nexec \"$(dirname \"$0\")/envoy\" -c \"$(dirname \"$0\")/envoy.json\" --concurrency 2 --log-level warn\n"))
	if err := os.Chmod(wrapper, 0700); err != nil {
		t.Fatal("Envoy wrapper permissions failed")
	}
	c.envoyProcess = startReferenceProcess(t, run, "wr21-envoy-"+uuid.NewString(), nil, "envoy-run")
	wr21WaitTCP(t, c.address, c.envoyProcess)
	recordReference(t, run, map[string]any{"stage": "B", "formal_processes": []string{"Envoy v1.38.4", "envoy-authz-adapter", "ani-iam cmd/server", "inference-service access-check"}, "envoy_config_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "policy_revision": data.TargetPolicyRevision, "direct_caller": c.adapterID, "receiver": c.receiverID, "inference_audience": biz.InferenceInvocationAudience, "inference_method": biz.InferenceInvocationRPC, "operation": "invokeInferenceChatCompletions", "model_execution": false})
	return c
}
func wr21WaitTCP(t *testing.T, address string, p *referenceProcess) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			t.Fatal("formal process exited before listener")
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("formal listener unavailable")
}

func wr21EnvoyConfig(t *testing.T, address, admin, adapter, sink, ca, cert, key string, resources []uuid.UUID, tenants ...uuid.UUID) []byte {
	t.Helper()
	socket := func(address string) map[string]any {
		h, p, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal("invalid private endpoint")
		}
		port, _ := strconv.Atoi(p)
		return map[string]any{"socket_address": map[string]any{"address": h, "port_value": port}}
	}
	endpoint := func(name, address string) map[string]any {
		return map[string]any{"cluster_name": name, "endpoints": []any{map[string]any{"lb_endpoints": []any{map[string]any{"endpoint": map[string]any{"address": socket(address)}}}}}}
	}
	cluster := func(name, address string) map[string]any {
		return map[string]any{"name": name, "type": "STATIC", "connect_timeout": "1s", "load_assignment": endpoint(name, address)}
	}
	auth := cluster("authz", adapter)
	auth["typed_extension_protocol_options"] = map[string]any{"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": map[string]any{"@type": "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions", "explicit_http_config": map[string]any{"http2_protocol_options": map[string]any{}}}}
	auth["transport_socket"] = map[string]any{"name": "envoy.transport_sockets.tls", "typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext", "sni": "extauth.wr21.test", "common_tls_context": map[string]any{"alpn_protocols": []string{"h2"}, "tls_params": map[string]any{"tls_minimum_protocol_version": "TLSv1_3", "tls_maximum_protocol_version": "TLSv1_3", "signature_algorithms": []string{"ed25519"}}, "tls_certificates": []any{map[string]any{"certificate_chain": map[string]any{"filename": cert}, "private_key": map[string]any{"filename": key}}}, "validation_context": map[string]any{"trusted_ca": map[string]any{"filename": ca}, "match_typed_subject_alt_names": []any{map[string]any{"san_type": "DNS", "matcher": map[string]any{"exact": "extauth.wr21.test"}}}}}}}
	var hosts []any
	if len(tenants) == 0 {
		tenants = referenceTenants[:]
	}
	if len(tenants) != 2 || len(resources) != 2 {
		t.Fatal("two exact owner Tenant/resources required")
	}
	for i := 0; i < 3; i++ {
		tenant := tenants[i%2]
		resource := resources[i%2]
		name := fmt.Sprintf("tenant-%d.wr21.test", i)
		if i == 2 {
			resource = resources[1]
		}
		hosts = append(hosts, map[string]any{"name": name, "domains": []string{name}, "routes": []any{map[string]any{"match": map[string]any{"prefix": "/"}, "route": map[string]any{"cluster": "protected-sink", "timeout": "8s"}, "typed_per_filter_config": map[string]any{"envoy.filters.http.ext_authz": map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthzPerRoute", "check_settings": map[string]any{"context_extensions": map[string]string{"ani.target_tenant_id": tenant.String(), "ani.inference_service_id": resource.String()}}}}}}})
	}
	ext := map[string]any{"name": "envoy.filters.http.ext_authz", "typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz", "transport_api_version": "V3", "failure_mode_allow": false, "status_on_error": map[string]any{"code": "ServiceUnavailable"}, "grpc_service": map[string]any{"envoy_grpc": map[string]any{"cluster_name": "authz"}, "timeout": "6s"}, "with_request_body": map[string]any{"max_request_bytes": 65536, "allow_partial_message": false, "pack_as_bytes": true}}}
	hcm := map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager", "stat_prefix": "wr21", "access_log": []any{map[string]any{"name": "envoy.access_loggers.file", "typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog", "path": "/dev/stdout", "log_format": map[string]any{"text_format_source": map[string]any{"inline_string": "code=%RESPONSE_CODE% details=%RESPONSE_CODE_DETAILS% flags=%RESPONSE_FLAGS% upstream_failure=%UPSTREAM_TRANSPORT_FAILURE_REASON%\n"}}}}}, "route_config": map[string]any{"name": "wr21-routes", "virtual_hosts": hosts}, "http_filters": []any{ext, map[string]any{"name": "envoy.filters.http.router", "typed_config": map[string]any{"@type": "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"}}}}
	doc := map[string]any{"admin": map[string]any{"address": socket(admin)}, "static_resources": map[string]any{"listeners": []any{map[string]any{"name": "wr21-http", "address": socket(address), "filter_chains": []any{map[string]any{"filters": []any{map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm}}}}}}, "clusters": []any{auth, cluster("protected-sink", sink)}}}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal("Envoy config serialization failed")
	}
	return raw
}

func (c *wr21Chain) invoke(t *testing.T, key, host, path, body string, headers map[string]string, want int) http.Header {
	t.Helper()
	before, _ := c.sink.seen()
	req, err := http.NewRequest("POST", "http://"+c.address+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal("HTTP request creation failed")
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	remote := ""
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { remote = info.Conn.RemoteAddr().String() }}))
	response, err := c.http.Do(req)
	if err != nil {
		t.Fatal("Envoy HTTP transport failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	after, _ := c.sink.seen()
	if response.StatusCode != want {
		stats, fetchErr := c.http.Get("http://" + c.envoyAdmin + "/stats?filter=cluster.authz")
		if fetchErr == nil {
			raw, _ := io.ReadAll(io.LimitReader(stats.Body, 65536))
			stats.Body.Close()
			writeReferencePrivate(t, filepath.Join(c.run, "private/envoy-authz-stats.txt"), raw)
		}
		t.Fatalf("real Envoy status=%d want=%d; sink_delta=%d; remote=%s; server=%q", response.StatusCode, want, after-before, remote, response.Header.Get("Server"))
	}
	if want == 200 && after != before+1 || want != 200 && after != before {
		t.Fatalf("protected sink delta=%d for status=%d", after-before, want)
	}
	recordReference(t, c.run, map[string]any{"test": t.Name(), "stage": "B/C", "http_status": response.StatusCode, "protected_sink_delta": after - before, "pass": true})
	return response.Header
}

func TestWR21StageBRealEnvoyOwner(t *testing.T) {
	c := newWR21Chain(t)
	ctx := context.Background()
	client, actor, _, _ := c.login(t, 0)
	management := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		code, r, _ := c.request(t, client, method, path, actor, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if code != want {
			t.Fatalf("formal setup management status=%d reason=%s", code, r["code"])
		}
		return r
	}
	workload := management("POST", "/auth/tenant-workloads", map[string]any{"tenant_id": referenceTenants[0].String(), "name": "envoy-bot", "role_ids": []string{c.invokeRoles[0].String()}}, 201)
	principal := workload["principal_id"].(string)
	key := management("POST", "/auth/api-keys", map[string]any{"principal_id": principal, "never_expires": true}, 201)
	secret := key["secret"].(string)
	keyID := key["api_key"].(map[string]any)["key_id"].(string)
	body := `{"model":"wr21-model-0","messages":[{"role":"user","content":"WR21 transport check"}]}`
	sub := func(name string, fn func(*testing.T)) {
		if !t.Run(name, fn) {
			t.FailNow()
		}
	}
	sub("formal_chain_current_authority_real_owner_and_header_spoof", func(t *testing.T) {
		c.invoke(t, secret, "tenant-0.wr21.test", "/v1/chat/completions", body, map[string]string{"x-ani-principal-id": "spoof", "x-ani-tenant-id": referenceTenants[1].String(), "x-ani-extra": "spoof", "x-api-key": "spoof", "cookie": "spoof", "x-ai-eg-model": "wrong"}, 200)
		_, h := c.sink.seen()
		if h.Get("Authorization") != "" || h.Get("Cookie") != "" || h.Get("x-api-key") != "" || h.Get("x-ani-extra") != "" || h.Get("x-ai-eg-model") != "" || h.Get("x-ani-principal-id") != principal || h.Get("x-ani-tenant-id") != referenceTenants[0].String() || h.Get("x-ani-principal-type") != "workload" {
			t.Fatalf("trusted downstream fields: principal=%t tenant=%t type=%t; removed: auth=%t cookie=%t key=%t extra=%t model=%t", h.Get("x-ani-principal-id") == principal, h.Get("x-ani-tenant-id") == referenceTenants[0].String(), h.Get("x-ani-principal-type") == "workload", h.Get("Authorization") == "", h.Get("Cookie") == "", h.Get("x-api-key") == "", h.Get("x-ani-extra") == "", h.Get("x-ai-eg-model") == "")
		}
		var events int
		if err := c.db.owner.QueryRow(ctx, `SELECT count(*) FROM inference_access_policy_events WHERE tenant_id=$1 AND inference_service_id=$2 AND api_key_id=$3 AND decision='allow'`, referenceTenants[0], c.db.resources[0], keyID).Scan(&events); err != nil || events != 0 {
			t.Fatalf("owner stores rejection events only; unexpected allow event count=%d", events)
		}
		recordReference(t, c.run, map[string]any{"stage": "B", "caller_id": c.adapterID, "receiver_id": c.receiverID, "subject_id": principal, "key_id": keyID, "tenant_id": referenceTenants[0], "resource_id": c.db.resources[0], "owner_allow_events_expected": 0, "owner_event_count": events, "decision_id": h.Get("x-ani-decision-id"), "secret_recorded": false})
	})
	sub("missing_forged_unknown_key_and_unopened_path", func(t *testing.T) {
		c.invoke(t, "", "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 401)
		c.invoke(t, secret+"forged", "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 401)
		c.invoke(t, "ani_"+uuid.NewString()+"_unknown", "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 401)
		c.invoke(t, secret, "tenant-0.wr21.test", "/v1/responses", body, nil, 404)
		c.invoke(t, secret, "tenant-1.wr21.test", "/v1/chat/completions", body, nil, 403)
		c.invoke(t, secret, "tenant-2.wr21.test", "/v1/chat/completions", body, nil, 404)
	})
	sub("real_owner_status_and_key_deny_policy", func(t *testing.T) {
		_, err := c.db.owner.Exec(ctx, `UPDATE inference_services SET status='stopped' WHERE tenant_id=$1 AND id=$2`, referenceTenants[0], c.db.resources[0])
		if err != nil {
			t.Fatal("owner status fault failed")
		}
		c.invoke(t, secret, "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 404)
		_, err = c.db.owner.Exec(ctx, `UPDATE inference_services SET status='running' WHERE tenant_id=$1 AND id=$2`, referenceTenants[0], c.db.resources[0])
		if err != nil {
			t.Fatal("owner recovery failed")
		}
		_, err = c.db.owner.Exec(ctx, `INSERT INTO inference_access_policy_api_keys(policy_id,tenant_id,api_key_id,key_prefix,effect) VALUES($1,$2,$3,'wr21','deny')`, c.db.policies[0], referenceTenants[0], keyID)
		if err != nil {
			t.Fatal("owner policy fault failed")
		}
		c.invoke(t, secret, "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 403)
		var deniedEvents int
		if err := c.db.owner.QueryRow(ctx, `SELECT count(*) FROM inference_access_policy_events WHERE tenant_id=$1 AND inference_service_id=$2 AND api_key_id=$3 AND policy_id=$4 AND decision='deny'`, referenceTenants[0], c.db.resources[0], keyID, c.db.policies[0]).Scan(&deniedEvents); err != nil || deniedEvents != 1 {
			t.Fatalf("real owner rejection attribution count=%d", deniedEvents)
		}
		recordReference(t, c.run, map[string]any{"owner_denial_attribution": "pass", "tenant": referenceTenants[0], "resource": c.db.resources[0], "key_id": keyID, "policy": c.db.policies[0], "denial_events": deniedEvents})
		_, err = c.db.owner.Exec(ctx, `DELETE FROM inference_access_policy_api_keys WHERE policy_id=$1 AND tenant_id=$2 AND api_key_id=$3 AND effect='deny'`, c.db.policies[0], referenceTenants[0], keyID)
		if err != nil {
			t.Fatal("owner policy recovery failed")
		}
		c.invoke(t, secret, "tenant-0.wr21.test", "/v1/chat/completions", body, nil, 200)
	})
	sub("formal_receiver_adversarial_binding_matrix", func(t *testing.T) {
		b := map[string]string{"TargetRevision": wr32Registry(t).Revision(biz.InferenceInvocationAudience, biz.InferenceInvocationOperation), "IAM": c.adapterEnv["IAM_SERVICE_GRPC_ADDR"], "IAMName": c.adapterEnv["IAM_TLS_SERVER_NAME"], "Owner": c.ownerAddress, "CA": c.adapterEnv["IAM_TLS_CA_FILE"], "Cert": c.adapterEnv["IAM_TLS_CERT_FILE"], "Key": c.adapterEnv["IAM_TLS_KEY_FILE"], "OtherCert": filepath.Join(c.iam.directory, "gateway-client.pem"), "OtherKey": filepath.Join(c.iam.directory, "gateway-client-key.pem"), "Tenant": referenceTenants[0].String(), "OtherTenant": referenceTenants[1].String(), "Principal": principal, "KeyID": keyID, "Prefix": secret[:12], "Secret": secret, "Resource": c.db.resources[0].String(), "Revision": data.TargetPolicyRevision}
		raw, _ := json.Marshal(b)
		file := filepath.Join(c.run, "private/binding-bundle.json")
		writeReferencePrivate(t, file, raw)
		cmd := exec.Command(filepath.Join(c.run, "private/wr21-adapter-tests"), "-test.run=^TestWR21FormalReceiverBindingMatrix$", "-test.v", "-test.timeout=45s")
		cmd.Env = append(os.Environ(), "WR21_BINDING_BUNDLE="+file)
		output, err := cmd.CombinedOutput()
		writeReferencePrivate(t, filepath.Join(c.run, "private/binding-matrix.log"), output)
		recordReference(t, c.run, map[string]any{"scenario": "formal-receiver-binding-matrix", "pass": err == nil, "log_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "layer": "adversarial protocol client to formal IAM and actual owner; supplements Envoy HTTP chain"})
		if err != nil {
			t.Fatal("formal receiver binding matrix failed; inspect private sanitized assertion log")
		}
	})
	recordReference(t, c.run, map[string]any{"stage": "B", "formal_chain": "pass", "full_stage_C": "not_verified"})
}
