//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

var referenceTenants = []uuid.UUID{
	uuid.MustParse("01993000-0019-7000-8000-000000000001"),
	uuid.MustParse("01993000-0019-7000-8000-000000000002"),
}
var referenceInstances = []string{"01993000-0019-7000-9000-000000000001", "01993000-0019-7000-9000-000000000002"}

// This gate is deliberately separate from component/SDK tests. It can only run
// with the approved fixture UIDs, fresh restricted Kubernetes credentials and
// all three binaries built from the current recorded source snapshots.
func TestFormalGatewaySessionKubernetesReference(t *testing.T) {
	if os.Getenv("WR19_REAL_CHAIN") != "1" {
		t.Skip("dedicated authorized Kubernetes chain gate required")
	}
	ctx := context.Background()
	run, _ := isolatedRun(t)
	accessDir := os.Getenv("WR19_K8S_PRIVATE_DIR")
	if !strings.HasPrefix(accessDir, "/home/ubuntu/workspace/ani-iam-runs/wr19-") {
		t.Fatal("task-owned Kubernetes access directory required")
	}
	for _, name := range []string{"session.kubeconfig", "gateway.token", "ca.crt", "fixture-public.json"} {
		info, err := os.Lstat(filepath.Join(accessDir, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			t.Fatal("missing or unsafe private Kubernetes input")
		}
	}
	var fixture struct {
		ClusterUID string    `json:"cluster_uid"`
		Proxy      string    `json:"proxy"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	input, _ := os.ReadFile(filepath.Join(accessDir, "fixture-public.json"))
	if json.Unmarshal(input, &fixture) != nil || fixture.ClusterUID != "f5cafbf1-5246-4f8f-b9da-742182f37528" || time.Until(fixture.ExpiresAt) < 15*time.Minute {
		t.Fatal("Kubernetes input identity or lifetime is not current")
	}
	for _, name := range []string{"ani-gateway", "session-gateway"} {
		if info, err := os.Stat(filepath.Join(run, "private", name)); err != nil || !info.Mode().IsRegular() {
			t.Fatal("formal binaries must be built before the real chain")
		}
	}
	iamDB := newPostgresEnvironment(t)
	owner := mustPool(t, iamDB.migrationDSN(primaryDB))
	defer owner.Close()
	password := "wr19-isolated-reference-human"
	hash, err := data.NewArgon2idPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	humans, roles := seedReferenceHumans(t, owner, hash)
	iamRedis, _ := newIsolatedRedis(t, ctx)
	sessionRedis, _ := newIsolatedRedis(t, ctx)
	gatewayRedis, _ := newIsolatedRedis(t, ctx)
	aniDSN := newReferenceOwnerDatabase(t, run)
	gatewayID, gatewayBinding, sessionID, sessionBinding := mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	setup := formalIAMSetup{Environment: "wr19-reference", TrustDomain: "wr19.test", GatewayDNS: "gateway.wr19.test"}
	var renewalCA *x509.Certificate
	var renewalCAKey ed25519.PrivateKey
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		renewalCA, renewalCAKey = ca, key
		caDigest := sha256.Sum256(ca.Raw)
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour)}
		gateway := biz.BootstrapWorkload{PrincipalID: gatewayID, BindingID: gatewayBinding, Name: "wr19-gateway", DNSIdentity: setup.GatewayDNS}
		for _, operation := range []string{"/iam.v1.AuthenticationService/PasswordLogin", "/iam.v1.AuthenticationService/ValidatePrincipal", "/iam.v1.AuthenticationService/IssueWorkloadToken", "/iam.v1.AuthenticationService/IssueDelegation", "/iam.v1.AuthorizationService/CheckPermission"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: operation})
		}
		gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation})
		receiver := biz.BootstrapWorkload{PrincipalID: sessionID, BindingID: sessionBinding, Name: "wr19-session", DNSIdentity: "session.wr19.test"}
		for _, operation := range []string{"/iam.v1.AuthorizationService/VerifyWorkloadInvocation", biz.VerifySessionContinuationRPC} {
			receiver.Grants = append(receiver.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: operation})
		}
		manifest.Workloads = []biz.BootstrapWorkload{gateway, receiver}
		raw, _ := json.Marshal(manifest)
		digest := sha256.Sum256(raw)
		manifestFile, dsnFile := filepath.Join(directory, "reference-bootstrap.json"), filepath.Join(directory, "provisioner.secret")
		writeReferencePrivate(t, manifestFile, raw)
		writeReferencePrivate(t, dsnFile, []byte(postgresDSN(provisionerRole, iamDB.provisionerPass, iamDB.host, primaryDB, "wr19-reference-bootstrap")))
		command := exec.Command(binary, "provision-workloads", "--manifest", manifestFile, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", setup.Environment, "--trust-domain", setup.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", dsnFile)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatal("formal reference bootstrap failed; private command output retained in memory only")
		}
		var receipt biz.WorkloadBootstrapReceipt
		if json.Unmarshal(out, &receipt) != nil || receipt.ManifestID != manifest.ManifestID {
			t.Fatal("invalid formal bootstrap receipt")
		}
		writeReferenceDualCertificate(t, directory, ca, key)
	}
	iam := startFormalIAM(t, iamDB, iamRedis, setup)
	sessionHTTP, sessionGRPC, gatewayHTTP, gatewayAdmin := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	ticketKey := make([]byte, 32)
	if _, err := rand.Read(ticketKey); err != nil {
		t.Fatal(err)
	}
	ticketKeyFile := filepath.Join(iam.directory, "ticket-key")
	writeReferencePrivate(t, ticketKeyFile, ticketKey)
	origin := "http://" + gatewayHTTP
	sessionEnv := map[string]string{
		"IAM_TARGET_MODE": "wr19", "IAM_GRPC_ADDRESS": iam.config.Server.Grpc.Addr, "IAM_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": setup.Environment, "IAM_WORKLOAD_TRUST_DOMAIN": setup.TrustDomain, "IAM_POLICY_REVISION": data.TargetPolicyRevision,
		"WORKLOAD_TLS_CERT_FILE": filepath.Join(iam.directory, "reference-session.crt"), "WORKLOAD_TLS_KEY_FILE": filepath.Join(iam.directory, "reference-session.key"), "WORKLOAD_TLS_CA_FILE": iam.config.Server.Grpc.Tls.ClientCaFile,
		"KUBECONFIG_FILE": filepath.Join(accessDir, "session.kubeconfig"), "HTTP_ADDR": sessionHTTP, "GRPC_ADDR": sessionGRPC, "PUBLIC_WS_BASE_URL": "ws://" + sessionHTTP + "/api/v1/realtime", "ALLOWED_ORIGINS": origin,
		"STORE_MODE": "redis", "REDIS_URL": referenceRedisURL(sessionRedis), "TICKET_ENCRYPTION_KEY_FILE": ticketKeyFile,
	}
	sessionEnv["HTTPS_PROXY"] = fixture.Proxy
	sessionEnv["NO_PROXY"] = "127.0.0.1,localhost"
	sessionProcess := startReferenceProcess(t, run, "session-gateway", sessionEnv)
	waitReferenceHTTP(t, "http://"+sessionHTTP+"/readyz", sessionProcess)
	gatewayEnv := map[string]string{
		"IAM_TARGET_MODE": "wr19", "IAM_TARGET_GRPC_ADDR": iam.config.Server.Grpc.Addr, "IAM_TARGET_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": setup.Environment, "IAM_WORKLOAD_TRUST_DOMAIN": setup.TrustDomain,
		"IAM_TARGET_TLS_CA_FILE": iam.config.Server.Grpc.Tls.ClientCaFile, "IAM_TARGET_TLS_CERT_FILE": filepath.Join(iam.directory, "gateway-client.pem"), "IAM_TARGET_TLS_KEY_FILE": filepath.Join(iam.directory, "gateway-client-key.pem"),
		"SESSION_GATEWAY_GRPC_ADDR": sessionGRPC, "SESSION_GATEWAY_TLS_SERVER_NAME": "session.wr19.test", "GATEWAY_LISTEN_ADDR": gatewayHTTP, "GATEWAY_HEALTH_LISTEN_ADDR": gatewayAdmin,
		"DATABASE_URL": aniDSN, "GATEWAY_REDIS_URL": referenceRedisURL(gatewayRedis), "WORKLOAD_PROVIDER": "kubernetes_rest", "WORKLOAD_PROVIDER_APPLY_ENABLED": "false", "WORKLOAD_LIFECYCLE_APPLY_ENABLED": "false", "WORKLOAD_OPS_ENABLED": "false",
		"KUBERNETES_API_HOST": "https://10.10.1.66:6443", "KUBERNETES_SERVICE_ACCOUNT_TOKEN_FILE": filepath.Join(accessDir, "gateway.token"), "KUBERNETES_SERVICE_ACCOUNT_CA_FILE": filepath.Join(accessDir, "ca.crt"),
	}
	gatewayEnv["HTTPS_PROXY"] = fixture.Proxy
	gatewayEnv["NO_PROXY"] = "127.0.0.1,localhost"
	gatewayProcess := startReferenceProcess(t, run, "ani-gateway", gatewayEnv)
	waitReferenceHTTP(t, "http://"+gatewayAdmin+"/readyz", gatewayProcess)
	base := "http://" + gatewayHTTP
	tokens := make([]string, 2)
	for index, tenant := range referenceTenants {
		code, body := referenceHTTP(t, "POST", base+"/api/v1/auth/password/login", "", map[string]any{"account": fmt.Sprintf("wr19-%d@example.com", index), "password": password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}})
		if code != 200 {
			t.Fatalf("formal Gateway login status=%d", code)
		}
		if err := json.Unmarshal(body, &struct {
			AccessToken *string `json:"access_token"`
		}{AccessToken: &tokens[index]}); err != nil || tokens[index] == "" {
			t.Fatal("formal Gateway did not return an access credential")
		}
	}
	create := func(index int, key, token string) (int, map[string]string) {
		code, raw := referenceHTTP(t, "POST", base+"/api/v1/instances/"+referenceInstances[index]+"/exec", token, map[string]any{"idempotency_key": key, "container": "shell", "command": []string{"/bin/sh"}, "tty": true, "rows": 24, "cols": 80})
		var response struct {
			ID    string `json:"id"`
			WSURL string `json:"ws_url"`
		}
		if code == 200 && json.Unmarshal(raw, &response) != nil {
			t.Fatal("invalid Session response")
		}
		return code, map[string]string{"id": response.ID, "url": response.WSURL}
	}
	t.Run("lost response retries the existing owner session with current authorization", func(t *testing.T) {
		key := uuid.NewString()
		code, body := referenceHTTP(t, "POST", base+"/api/v1/instances/"+referenceInstances[0]+"/exec", tokens[0], map[string]any{"idempotency_key": key, "container": "shell", "command": []string{"/bin/sh"}, "tty": true, "rows": 24, "cols": 80}, true)
		if code != 200 || len(body) != 0 {
			t.Fatalf("discarded response admission status=%d", code)
		}
		keys, err := sessionRedis.Keys(ctx, "{ani-session-gateway}:session:*").Result()
		if err != nil || len(keys) != 1 {
			t.Fatal("lost response did not leave exactly one committed owner session")
		}
		originalID, err := sessionRedis.HGet(ctx, keys[0], "id").Result()
		if err != nil || originalID == "" {
			t.Fatal("committed owner session identity unavailable")
		}
		code, retried := create(0, key, tokens[0])
		if code != 200 || retried["id"] != originalID {
			t.Fatal("lost response retry created a different owner session")
		}
		conn := referenceConnect(t, retried["url"], origin)
		assertReferenceShell(t, conn)
		_ = conn.Close()
		waitReferenceClosed(t, sessionRedis, retried["id"], "")
	})
	t.Run("two tenants real REST IAM Session Redis Kubernetes exec and close", func(t *testing.T) {
		for index := range referenceTenants {
			key := uuid.NewString()
			code, issued := create(index, key, tokens[index])
			if code != 200 || issued["id"] == "" {
				t.Fatalf("formal session create status=%d", code)
			}
			code, replayed := create(index, key, tokens[index])
			if code != 200 || replayed["id"] != issued["id"] || replayed["url"] != issued["url"] {
				t.Fatal("current lost-response retry did not return the original owner ticket")
			}
			conn := referenceConnect(t, issued["url"], origin)
			assertReferenceShell(t, conn)
			_, response, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second, Subprotocols: []string{"ani.terminal.v1"}}).Dial(issued["url"], http.Header{"Origin": []string{origin}})
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				t.Fatal("consumed ticket opened a second connection")
			}
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			_ = conn.Close()
			waitReferenceClosed(t, sessionRedis, issued["id"], "")
		}
	})
	t.Run("cross tenant and missing identity never reach Session issuance", func(t *testing.T) {
		for _, token := range []string{"", tokens[1]} {
			code, _ := create(0, uuid.NewString(), token)
			if code == 200 {
				t.Fatal("untrusted or cross-tenant request admitted")
			}
		}
	})
	setCreate := func(enabled bool) {
		var err error
		if enabled {
			_, err = owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','instances','create',now())`, referenceTenants[0], roles[0])
		} else {
			_, err = owner.Exec(ctx, `DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='instances' AND action='create'`, referenceTenants[0], roles[0])
		}
		if err != nil {
			t.Fatal("fixture permission change failed")
		}
	}
	t.Run("revocation denies replay and ticket redemption before runtime", func(t *testing.T) {
		key := uuid.NewString()
		code, issued := create(0, key, tokens[0])
		if code != 200 {
			t.Fatalf("create status=%d", code)
		}
		setCreate(false)
		defer setCreate(true)
		if code, _ := create(0, key, tokens[0]); code == 200 {
			t.Fatal("revocation replayed an owner ticket")
		}
		conn, response, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second, Subprotocols: []string{"ani.terminal.v1"}}).Dial(issued["url"], http.Header{"Origin": []string{origin}})
		if response != nil {
			response.Body.Close()
		}
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			t.Fatal("revoked subject redeemed a ticket")
		}
		waitReferenceClosed(t, sessionRedis, issued["id"], "authorization_lost")
	})
	t.Run("established exec closes on current permission revocation", func(t *testing.T) {
		code, issued := create(0, uuid.NewString(), tokens[0])
		if code != 200 {
			t.Fatalf("create status=%d", code)
		}
		conn := referenceConnect(t, issued["url"], origin)
		defer conn.Close()
		assertReferenceShell(t, conn)
		setCreate(false)
		defer setCreate(true)
		started := time.Now()
		_ = conn.SetReadDeadline(started.Add(33 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
		if time.Since(started) > 32500*time.Millisecond {
			t.Fatal("real connection did not close within the accepted recheck bound")
		}
		waitReferenceClosed(t, sessionRedis, issued["id"], "authorization_lost")
	})

	t.Run("IAM outage closes established exec and fresh admission recovers", func(t *testing.T) {
		code, issued := create(0, uuid.NewString(), tokens[0])
		if code != 200 {
			t.Fatalf("create status=%d", code)
		}
		conn := referenceConnect(t, issued["url"], origin)
		defer conn.Close()
		assertReferenceShell(t, conn)
		if err := iam.process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal("cannot suspend registered test IAM")
		}
		defer func() { _ = iam.process.Signal(syscall.SIGCONT) }()
		started := time.Now()
		if code, _ := create(0, uuid.NewString(), tokens[0]); code < 500 {
			t.Fatalf("IAM outage admission status=%d", code)
		}
		_ = conn.SetReadDeadline(started.Add(33 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
		if time.Since(started) > 32500*time.Millisecond {
			t.Fatal("IAM outage did not close the real connection within accepted bound")
		}
		waitReferenceClosed(t, sessionRedis, issued["id"], "authorization_lost")
		if err := iam.process.Signal(syscall.SIGCONT); err != nil {
			t.Fatal("registered IAM did not resume")
		}
		code, recovered := create(0, uuid.NewString(), tokens[0])
		if code != 200 {
			t.Fatalf("IAM recovery create status=%d", code)
		}
		next := referenceConnect(t, recovered["url"], origin)
		assertReferenceShell(t, next)
		_ = next.Close()
		waitReferenceClosed(t, sessionRedis, recovered["id"], "")
	})
	t.Run("normal Gateway certificate renewal preserves bound Workload authority", func(t *testing.T) {
		gatewayProcess.stop()
		writeProcessE2ELeafCertificate(t, iam.directory, "gateway-client", setup.GatewayDNS, x509.ExtKeyUsageClientAuth, renewalCA, renewalCAKey)
		renewed := startReferenceProcess(t, run, "ani-gateway-renewed", gatewayEnv, "ani-gateway")
		waitReferenceHTTP(t, "http://"+gatewayAdmin+"/readyz", renewed)
		code, issued := create(0, uuid.NewString(), tokens[0])
		if code != 200 {
			t.Fatalf("renewed Gateway create status=%d", code)
		}
		conn := referenceConnect(t, issued["url"], origin)
		assertReferenceShell(t, conn)
		_ = conn.Close()
		waitReferenceClosed(t, sessionRedis, issued["id"], "")
	})

	_ = humans
	if !t.Failed() {
		t.Log("WR19 real Gateway/IAM/Session Kubernetes exec chain completed; no token, ticket or terminal bytes recorded")
	}
}

func seedReferenceHumans(t *testing.T, pool *pgxpool.Pool, hash string) ([]uuid.UUID, []uuid.UUID) {
	t.Helper()
	humans, roles := []uuid.UUID{}, []uuid.UUID{}
	ctx := context.Background()
	for index, tenant := range referenceTenants {
		human, identity, membership, role, binding := mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t)
		humans = append(humans, human)
		roles = append(roles, role)
		email := fmt.Sprintf("wr19-%d@example.com", index)
		statements := []struct {
			q string
			a []any
		}{
			{`INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, []any{human}},
			{`INSERT INTO tenant_access(tenant_id,status,version,created_at,updated_at) VALUES($1,'active',1,now(),now())`, []any{tenant}},
			{`INSERT INTO tenant_lifecycle_projections(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until) VALUES($1,'active',1,now(),now(),now()+interval '5 minutes')`, []any{tenant}},
			{`INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, []any{tenant, membership, human}},
			{`INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at) VALUES($1,$2,now(),now(),now())`, []any{human, email}},
			{`INSERT INTO identities(id,principal_id,provider,issuer,subject,status,version,created_at,updated_at) VALUES($1,$2,'password','ani-local',$3,'active',1,now(),now())`, []any{identity, human, email}},
			{`INSERT INTO password_credentials(principal_id,identity_id,password_hash,algorithm,version,created_at,updated_at) VALUES($1,$2,$3,'argon2id',1,now(),now())`, []any{human, identity, hash}},
			{`INSERT INTO tenant_roles(tenant_id,id,code,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,$2,'tenant-admin',true,1,1,now(),now())`, []any{tenant, role}},
			{`INSERT INTO tenant_role_bindings(tenant_id,id,membership_id,role_id,version,created_at,updated_at) VALUES($1,$2,$3,$4,1,now(),now())`, []any{tenant, binding, membership, role}},
			{`INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','instances','read',now()),($1,$2,'tenant','instances','create',now())`, []any{tenant, role}},
		}
		for _, s := range statements {
			if _, err := pool.Exec(ctx, s.q, s.a...); err != nil {
				t.Fatalf("controlled reference Human fixture failed: %v", err)
			}
		}
	}
	return humans, roles
}

func newReferenceOwnerDatabase(t *testing.T, run string) string {
	t.Helper()
	ctx := context.Background()
	adminPassword := randomPassword(t)
	runtimePassword := randomPassword(t)
	c, err := postgres.Run(ctx, postgresImage, postgres.WithDatabase("wr19_ani"), postgres.WithUsername("postgres"), postgres.WithPassword(adminPassword), postgres.BasicWaitStrategies(), isolatedContainer(t, "ani-postgres", "5432/tcp"))
	if err != nil {
		t.Fatal("start isolated ANI owner PostgreSQL failed")
	}
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := testcontainers.TerminateContainer(c, testcontainers.StopContext(clean)); err != nil {
			t.Error("ANI PostgreSQL cleanup failed")
		}
	})
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal("ANI PostgreSQL endpoint unavailable")
	}
	pool := mustPool(t, dsn)
	defer pool.Close()
	for _, name := range []string{"20260501000100_init_schema.sql", "20260502000200_operations_idempotency.sql", "20260519000400_instance_u_vm_protection.sql", "20260520000700_workload_identity_api_keys.sql", "20260730000100_instance_management_lifecycle_ops.sql", "20260812000100_quota_tx_ids.sql", "20260825000100_workload_instances_rls_fix.sql"} {
		raw, err := os.ReadFile(filepath.Join(run, "source-ani/repo/deploy/migrations", name))
		if err != nil {
			t.Fatalf("missing pinned owner migration %s", name)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("owner migration %s failed: %v", name, err)
		}
	}
	grantSource, err := os.ReadFile(filepath.Join(run, "source-ani/repo/deploy/migrations/20260828000200_app_role_privileges.sql"))
	if err != nil {
		t.Fatal("missing fixed owner grant source")
	}
	statement := "GRANT SELECT, INSERT, UPDATE ON workload_instances TO ani_app;"
	if !strings.Contains(string(grantSource), "\n"+statement+"\n") {
		t.Fatal("owner table privilege contract changed")
	}
	if _, err = pool.Exec(ctx, statement); err != nil {
		t.Fatal("owner workload table privilege setup failed")
	}
	if _, err = pool.Exec(ctx, "ALTER ROLE ani_app_user PASSWORD '"+runtimePassword+"' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS"); err != nil {
		t.Fatal("configure restricted ANI database role failed")
	}
	for index, tenant := range referenceTenants {
		name := []string{"wr19-exec-a", "wr19-exec-b"}[index]
		if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,name,display_name) VALUES($1,$2,$2)`, tenant, name); err != nil {
			t.Fatal("ANI Tenant fixture failed")
		}
		refs, _ := json.Marshal([]string{"kubernetes/Deployment/" + name})
		if _, err = pool.Exec(ctx, `INSERT INTO workload_instances(tenant_id,instance_id,name,workload_kind,provider,resource_refs,state) VALUES($1,$2,$3,'container','kubernetes',$4,'running')`, tenant, referenceInstances[index], name, string(refs)); err != nil {
			t.Fatal("ANI resource fixture failed")
		}
	}
	parsed, _ := url.Parse(dsn)
	parsed.User = url.UserPassword("ani_app_user", runtimePassword)
	runtimeDSN := parsed.String()
	runtimePool := mustPool(t, runtimeDSN)
	defer runtimePool.Close()
	var privileged bool
	if err = runtimePool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR oid=(SELECT datdba FROM pg_database WHERE datname=current_database()) FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		t.Fatal("ANI runtime role is privileged")
	}
	return runtimeDSN
}

func writeReferencePrivate(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal("write task-private input failed")
	}
}
func writeReferenceDualCertificate(t *testing.T, directory string, ca *x509.Certificate, key ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: serial, DNSNames: []string{"session.wr19.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, public, key)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	writeReferencePrivate(t, filepath.Join(directory, "reference-session.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	writeReferencePrivate(t, filepath.Join(directory, "reference-session.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
}
func referenceRedisURL(c *redis.Client) string {
	return (&url.URL{Scheme: "redis", User: url.UserPassword("", c.Options().Password), Host: c.Options().Addr, Path: "/0"}).String()
}

type referenceProcess struct {
	done chan struct{}
	err  error
	stop func()
}

func startReferenceProcess(t *testing.T, run, name string, values map[string]string, executable ...string) *referenceProcess {
	t.Helper()
	binaryName := name
	if len(executable) == 1 {
		binaryName = executable[0]
	}
	binary := filepath.Join(run, "private", binaryName)
	logFile := filepath.Join(run, "private", name+".log")
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOMAXPROCS=2"}
	for k, v := range values {
		command.Env = append(command.Env, k+"="+v)
	}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal("formal process start failed")
	}
	p := &referenceProcess{done: make(chan struct{})}
	go func() { p.err = command.Wait(); log.Close(); close(p.done) }()
	var once sync.Once
	p.stop = func() {
		once.Do(func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.done:
				if p.err != nil {
					t.Errorf("%s exit was not successful", name)
				}
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-p.done
				t.Errorf("%s required forced termination", name)
			}
		})
	}
	t.Cleanup(p.stop)
	content, _ := os.ReadFile(binary)
	digest := sha256.Sum256(content)
	recordReference(t, run, map[string]any{"process": name, "pid": command.Process.Pid, "binary_sha256": hex.EncodeToString(digest[:]), "log_reference": logFile, "stage": "started"})
	t.Cleanup(func() {
		p.stop()
		recordReference(t, run, map[string]any{"process": name, "stage": "exited", "success": p.err == nil})
	})
	return p
}
func recordReference(t *testing.T, run string, row any) {
	t.Helper()
	raw, _ := json.Marshal(row)
	file, err := os.OpenFile(filepath.Join(run, "reference-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}
func waitReferenceHTTP(t *testing.T, address string, p *referenceProcess) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			t.Fatal("formal process exited before readiness; inspect private log through sanitized diagnostics")
		default:
		}
		response, err := client.Get(address)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("formal process readiness timed out")
}
func referenceHTTP(t *testing.T, method, address, token string, value any, discard ...bool) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(value)
	request, err := http.NewRequest(method, address, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("invalid test request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", uuid.NewString())
	request.Header.Set("Idempotency-Key", uuid.NewString())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		t.Fatal("formal HTTP call failed")
	}
	defer response.Body.Close()
	if len(discard) == 1 && discard[0] {
		// Simulate losing the response before the caller can learn the ticket or
		// session ID. Only the separate owner database assertion observes commit.
		return response.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal("HTTP response unreadable")
	}
	return response.StatusCode, body
}
func referenceConnect(t *testing.T, address, origin string) *websocket.Conn {
	t.Helper()
	conn, response, err := (&websocket.Dialer{HandshakeTimeout: 4 * time.Second, Subprotocols: []string{"ani.terminal.v1"}}).Dial(address, http.Header{"Origin": []string{origin}})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal("formal Session WebSocket handshake failed")
	}
	return conn
}
func assertReferenceShell(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	marker := "wr19_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	// Split the marker so the terminal echo of the command cannot satisfy the
	// assertion; only the shell's printf output contains the contiguous marker.
	command := "printf '%s%s\\n' '" + marker[:12] + "' '" + marker[12:] + "'\n"
	if err := conn.WriteJSON(map[string]string{"type": "stdin", "data": command}); err != nil {
		t.Fatal("real shell input failed")
	}
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	var output strings.Builder
	for output.Len() < 65536 {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal("real shell output unavailable")
		}
		var frame struct{ Type, Data string }
		if json.Unmarshal(raw, &frame) != nil {
			t.Fatal("invalid terminal frame")
		}
		if frame.Type == "stdout" {
			output.WriteString(frame.Data)
			if strings.Contains(output.String(), marker) {
				_ = conn.SetReadDeadline(time.Time{})
				return
			}
		}
	}
	t.Fatal("real shell command result not observed")
}
func waitReferenceClosed(t *testing.T, client *redis.Client, id, reason string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	ctx := context.Background()
	key := "{ani-session-gateway}:session:" + id
	for time.Now().Before(deadline) {
		fields, err := client.HMGet(ctx, key, "state", "close_reason", "ticket_ciphertext", "authorization_ciphertext").Result()
		if err == nil && len(fields) == 4 && fields[0] == "closed" {
			if reason != "" && fields[1] != reason {
				t.Fatal("wrong Session owner close reason")
			}
			if fields[2] != "" || fields[3] != "" {
				t.Fatal("closed Session retained private reference")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Session owner did not record closure")
}
