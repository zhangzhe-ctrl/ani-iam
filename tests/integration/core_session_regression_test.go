//go:build integration && !governance

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR23ResumeFormalSession(t *testing.T) {
	started := time.Now().UTC()
	run, goal := isolatedRun(t)
	if goal != "wr23" || os.Getenv("WR23_FORMAL_EXTENSION") != "session" {
		t.Fatal("dedicated Session combination required")
	}
	var sessionCert, sessionKey string
	e := newWR23FormalEnvironment(t, func(dir string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, m *biz.WorkloadBootstrapManifest) {
		sessionCert, sessionKey = wr23DualCertificate(t, dir, "session.wr23.test", ca, key)
		for _, target := range [][2]string{{"ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"}, {"ani-iam", "/iam.v1.AuthenticationService/IssueDelegation"}, {"ani-iam", "/iam.v1.IAMAdminService/UpdateTenantRole"}, {biz.SessionInvocationAudience, biz.SessionInvocationOperation}} {
			m.Workloads[0].Grants = append(m.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: target[0], Operation: target[1]})
		}
		m.Workloads = append(m.Workloads, wr23Workload(t, "session", "wr23.test", [2]string{"ani-iam", "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}, [2]string{"ani-iam", biz.VerifySessionContinuationRPC}, [2]string{biz.SessionInvocationAudience, "session.receive"}))
	})
	actors := wr23RunFormalLifecycleChain(t, e)
	ctx := context.Background()
	status, other, _ := e.boss.request(t, actors.boss, "POST", "/admin/tenants", e.access(t, actors.boss), map[string]any{"name": "wr23-session-other-" + uuid.NewString()[:12], "display_name": "WR23 other Session resource Tenant", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "session-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, map[string]string{"Idempotency-Key": uuid.NewString()})
	if status != 200 {
		t.Fatalf("real Core Session Tenant status=%d reason=%v", status, other["code"])
	}
	tenants := []uuid.UUID{uuid.MustParse(actors.tenant), uuid.MustParse(other["id"].(string))}
	wr23PrivateJSON(t, filepath.Join(run, "private/session-tenants.json"), map[string]any{"run": filepath.Base(run), "source": "formal_Core_gateway", "tenants": tenants})
	clusterScript := filepath.Join(run, "source/tools/wr23-resume/session-cluster.py")
	// Register recovery before attempting cluster creation, including partial setup.
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join(run, "session-cluster.json")); err == nil {
			wr23PrivateCommand(t, run, "session-cluster-cleanup", exec.Command("python3", clusterScript, "cleanup"))
		}
	})
	wr23PrivateCommand(t, run, "session-cluster-prepare", exec.Command("python3", clusterScript, "prepare"))
	accessDir := filepath.Join(run, "private/session-access")
	var fixture struct {
		ClusterUID string    `json:"cluster_uid"`
		Server     string    `json:"server"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	raw, err := os.ReadFile(filepath.Join(accessDir, "fixture-public.json"))
	if err != nil || json.Unmarshal(raw, &fixture) != nil || fixture.ClusterUID == "" || time.Until(fixture.ExpiresAt) < 15*time.Minute {
		t.Fatal("current own cluster identity/lifetime unavailable")
	}
	ownerDSN := newReferenceOwnerDatabase(t, run, tenants...)
	sessionRedis, _ := newIsolatedRedis(t, ctx)
	sessionHTTP, sessionGRPC := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	resourceHTTP, resourceAdmin := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	// The formal Console uses HTTPS. Terminate its WSS connection at an actual
	// loopback TLS edge, forwarding to the unchanged Session HTTP listener.
	upstream, _ := url.Parse("http://" + sessionHTTP)
	resourceUpstream, _ := url.Parse("http://" + resourceHTTP)
	edgeRoutes := http.NewServeMux()
	edgeRoutes.Handle("/api/v1/realtime/", httputil.NewSingleHostReverseProxy(upstream))
	edgeRoutes.Handle("/api/v1/instances/", httputil.NewSingleHostReverseProxy(resourceUpstream))
	front := httptest.NewUnstartedServer(edgeRoutes)
	t.Cleanup(front.Close)
	certificate, err := tls.LoadX509KeyPair(sessionCert, sessionKey)
	if err != nil {
		t.Fatal("Session edge certificate")
	}
	front.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	front.StartTLS()
	_, edgePort, _ := net.SplitHostPort(front.Listener.Addr().String())
	edgeHost := net.JoinHostPort("session.wr23.test", edgePort)
	publicWS := "wss://" + edgeHost + "/api/v1/realtime"
	caBytes, err := os.ReadFile(e.boss.iam.config.Server.Grpc.Tls.ClientCaFile)
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(caBytes) {
		t.Fatal("Session edge CA")
	}
	dialer := &websocket.Dialer{HandshakeTimeout: 4 * time.Second, Subprotocols: []string{"ani.terminal.v1"}, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != edgeHost {
			return nil, fmt.Errorf("unregistered Session edge authority")
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, front.Listener.Addr().String())
	}}
	resourceSurface := &wr20Environment{run: run, origin: "https://" + edgeHost, transport: &http.Transport{TLSClientConfig: dialer.TLSClientConfig.Clone(), DialContext: dialer.NetDialContext}}
	t.Cleanup(resourceSurface.transport.CloseIdleConnections)
	resourceBrowser := resourceSurface.browser(t)
	connect := func(address string) *websocket.Conn {
		t.Helper()
		conn, response, err := dialer.Dial(address, http.Header{"Origin": []string{e.console.origin}})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			t.Fatal("formal Session WSS handshake failed")
		}
		return conn
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("fresh own Session ticket key")
	}
	keyFile := filepath.Join(run, "private/session-ticket.key")
	writeReferencePrivate(t, keyFile, key)
	sessionEnv := map[string]string{
		"IAM_TARGET_MODE": "wr19", "IAM_GRPC_ADDRESS": e.boss.iam.config.Server.Grpc.Addr, "IAM_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": "wr23-lifecycle", "IAM_WORKLOAD_TRUST_DOMAIN": "wr23.test", "IAM_POLICY_REVISION": data.TargetPolicyRevision,
		"WORKLOAD_TLS_CERT_FILE": sessionCert, "WORKLOAD_TLS_KEY_FILE": sessionKey, "WORKLOAD_TLS_CA_FILE": e.boss.iam.config.Server.Grpc.Tls.ClientCaFile,
		"KUBECONFIG_FILE": filepath.Join(accessDir, "session.kubeconfig"), "HTTP_ADDR": sessionHTTP, "GRPC_ADDR": sessionGRPC, "PUBLIC_WS_BASE_URL": publicWS, "ALLOWED_ORIGINS": e.console.origin,
		"STORE_MODE": "redis", "REDIS_URL": referenceRedisURL(sessionRedis), "TICKET_ENCRYPTION_KEY_FILE": keyFile, "NO_PROXY": "127.0.0.1,localhost",
	}
	process := startReferenceProcess(t, run, "wr23-session-"+uuid.NewString(), sessionEnv, "session-gateway")
	waitReferenceHTTP(t, "http://"+sessionHTTP+"/readyz", process)
	// Keep the actual Core owner and its publisher/heartbeat running. The same
	// fixed Gateway binary serves resources through its existing WR19 profile.
	resourceValues := map[string]string{}
	for name, value := range e.gatewayEnv {
		resourceValues[name] = value
	}
	delete(resourceValues, "ANI_CORE_LIFECYCLE_CONFIG")
	for name, value := range map[string]string{"IAM_TARGET_MODE": "wr19", "GATEWAY_LISTEN_ADDR": resourceHTTP, "GATEWAY_HEALTH_LISTEN_ADDR": resourceAdmin, "SESSION_GATEWAY_GRPC_ADDR": sessionGRPC, "SESSION_GATEWAY_TLS_SERVER_NAME": "session.wr23.test", "DATABASE_URL": ownerDSN, "GATEWAY_REDIS_URL": referenceRedisURL(e.console.gatewayRedis), "WORKLOAD_PROVIDER": "kubernetes_rest", "WORKLOAD_PROVIDER_APPLY_ENABLED": "false", "WORKLOAD_LIFECYCLE_APPLY_ENABLED": "false", "WORKLOAD_OPS_ENABLED": "false", "KUBERNETES_API_HOST": fixture.Server, "KUBERNETES_SERVICE_ACCOUNT_TOKEN_FILE": filepath.Join(accessDir, "gateway.token"), "KUBERNETES_SERVICE_ACCOUNT_CA_FILE": filepath.Join(accessDir, "ca.crt"), "NO_PROXY": "127.0.0.1,localhost"} {
		resourceValues[name] = value
	}
	// The fixed external-API client accepts an explicit bearer token; its
	// TokenFile option is used only with in-cluster ServiceHost discovery.
	gatewayToken, err := os.ReadFile(filepath.Join(accessDir, "gateway.token"))
	if err != nil || len(gatewayToken) == 0 {
		t.Fatal("current private Kubernetes reader credential")
	}
	resourceValues["KUBERNETES_BEARER_TOKEN"] = string(gatewayToken)
	resourceGateway := startReferenceProcess(t, run, "wr23-resource-gateway-"+uuid.NewString(), resourceValues, "ani-gateway")
	waitReferenceHTTP(t, "http://"+resourceAdmin+"/readyz", resourceGateway)
	version := int64(1)
	setPermission := func(enabled bool) {
		t.Helper()
		permissions := []string{"iam.memberships/read", "instances/read"}
		if enabled {
			permissions = append(permissions, "instances/create")
		}
		status, doc, _ := e.console.request(t, actors.admin, "PATCH", "/iam/tenants/"+actors.tenant+"/roles/"+actors.memberRole, actors.adminToken, map[string]any{"name": "WR23 Session ordinary member", "permissions": permissions, "expected_version": version}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 200 || doc["version"] != float64(version+1) {
			t.Fatalf("actual custom role update status=%d reason=%v", status, doc["code"])
		}
		version++
	}
	setPermission(true)
	create := func(index int, intent, token string, want int) map[string]any {
		t.Helper()
		status, doc, _ := resourceSurface.request(t, resourceBrowser, "POST", "/instances/"+referenceInstances[index]+"/exec", token, map[string]any{"idempotency_key": intent, "container": "shell", "command": []string{"/bin/sh"}, "tty": true, "rows": 24, "cols": 80}, map[string]string{"Origin": e.console.origin})
		if status != want {
			t.Fatalf("actual Gateway Session status=%d reason=%v want=%d", status, doc["code"], want)
		}
		return doc
	}
	intent := uuid.NewString()
	issued := create(0, intent, actors.memberToken, 200)
	retried := create(0, intent, actors.memberToken, 200)
	if retried["id"] != issued["id"] || retried["ws_url"] != issued["ws_url"] {
		t.Fatal("same-key Session retry changed owner ticket")
	}
	conn := connect(issued["ws_url"].(string))
	assertReferenceShell(t, conn)
	duplicate, response, err := dialer.Dial(issued["ws_url"].(string), http.Header{"Origin": []string{e.console.origin}})
	if response != nil {
		response.Body.Close()
	}
	if duplicate != nil {
		duplicate.Close()
	}
	if err == nil {
		t.Fatal("consumed Session ticket opened twice")
	}
	conn.Close()
	waitReferenceClosed(t, sessionRedis, issued["id"].(string), "")
	create(0, uuid.NewString(), "", 401)
	create(1, uuid.NewString(), actors.memberToken, 403)
	intent = uuid.NewString()
	issued = create(0, intent, actors.memberToken, 200)
	setPermission(false)
	create(0, intent, actors.memberToken, 403)
	denied, response, err := dialer.Dial(issued["ws_url"].(string), http.Header{"Origin": []string{e.console.origin}})
	if response != nil {
		response.Body.Close()
	}
	if denied != nil {
		denied.Close()
	}
	if err == nil {
		t.Fatal("revoked member redeemed a ticket")
	}
	waitReferenceClosed(t, sessionRedis, issued["id"].(string), "authorization_lost")
	setPermission(true)
	issued = create(0, uuid.NewString(), actors.memberToken, 200)
	conn = connect(issued["ws_url"].(string))
	assertReferenceShell(t, conn)
	setPermission(false)
	revoked := time.Now()
	conn.SetReadDeadline(revoked.Add(33 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	conn.Close()
	if time.Since(revoked) > 32500*time.Millisecond {
		t.Fatal("actual established Session exceeded current-permission recheck bound")
	}
	waitReferenceClosed(t, sessionRedis, issued["id"].(string), "authorization_lost")
	setPermission(true)
	issued = create(0, uuid.NewString(), actors.memberToken, 200)
	conn = connect(issued["ws_url"].(string))
	assertReferenceShell(t, conn)
	if e.boss.iam.process.Signal(syscall.SIGSTOP) != nil {
		t.Fatal("pause own IAM for Session dependency fault")
	}
	defer e.boss.iam.process.Signal(syscall.SIGCONT)
	faultStarted := time.Now()
	conn.SetReadDeadline(faultStarted.Add(33 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	conn.Close()
	if time.Since(faultStarted) > 32500*time.Millisecond {
		t.Fatal("Session stayed connected during IAM outage")
	}
	waitReferenceClosed(t, sessionRedis, issued["id"].(string), "authorization_lost")
	if e.boss.iam.process.Signal(syscall.SIGCONT) != nil {
		t.Fatal("resume own IAM")
	}
	waitFormalHTTP(t, e.boss.iam.livenessURL, 200)
	// Once current owner facts are fresh again, a new authorized request succeeds.
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		var fresh bool
		if e.boss.owner.QueryRow(ctx, `SELECT fresh_until>clock_timestamp() AND NOT repair_required FROM core_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2`, e.broker.configuration.Authority.Producer, tenants[0]).Scan(&fresh) == nil && fresh {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	issued = create(0, uuid.NewString(), actors.memberToken, 200)
	conn = connect(issued["ws_url"].(string))
	assertReferenceShell(t, conn)
	conn.Close()
	waitReferenceClosed(t, sessionRedis, issued["id"].(string), "")
	result := map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "tenant_id": actors.tenant, "other_tenant_id": tenants[1], "human_id": actors.memberID, "custom_role_id": actors.memberRole, "cluster_uid": fixture.ClusterUID, "grade": "real Core/NATS/invited ordinary member/Gateway/IAM/Session/Redis/Kubernetes exec; static resource-owner prerequisites; current permission and IAM outage checks", "projection_mode": e.projectionMode}
	public, _ := json.MarshalIndent(result, "", "  ")
	if os.WriteFile(filepath.Join(run, "formal-session-results.json"), append(public, '\n'), 0600) != nil {
		t.Fatal("persist Session evidence")
	}
}
