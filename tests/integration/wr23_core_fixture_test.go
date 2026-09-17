//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"google.golang.org/protobuf/proto"
)

// The WR22 browser/real Dex protocol helpers are reused with fresh WR23
// resources. No WR22 environment constructor or projection/identity seed runs.
type wr23CoreEnvironment struct {
	boss         *wr22BossEnvironment
	core         *pgxpool.Pool
	plan         string
	adminURL     string
	gateway      *referenceProcess
	startGateway func() *referenceProcess
}

func newWR23CoreEnvironment(t *testing.T) *wr23CoreEnvironment {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr23" {
		t.Fatal("WR23 run required")
	}
	ctx := context.Background()
	db := newPostgresEnvironment(t)
	owner := mustPool(t, db.migrationDSN(primaryDB))
	t.Cleanup(owner.Close)
	for _, table := range []string{"principals", "tenant_access", "tenant_memberships", "platform_memberships"} {
		var count int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("formal gate requires empty IAM identity and access tables")
		}
	}
	iamRedis, _ := newIsolatedRedis(t, ctx)
	gatewayRedis, _ := newIsolatedRedis(t, ctx)
	b := &wr22BossEnvironment{wr22Environment: &wr22Environment{wr20Environment: &wr20Environment{run: run, owner: owner, iamRedis: iamRedis, gatewayRedis: gatewayRedis}}, dexSecret: randomPassword(t), dexPassword: randomPassword(t)}
	e := &wr23CoreEnvironment{boss: b, plan: mustV7(t).String()}
	gatewayAddress, adminAddress := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	e.adminURL = "http://" + adminAddress
	gatewayURL, _ := url.Parse("http://" + gatewayAddress)
	front := httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(gatewayURL))
	t.Cleanup(front.Close)
	_, port, _ := net.SplitHostPort(front.Listener.Addr().String())
	b.origin = "https://boss.wr22.test:" + port
	b.issuer, _ = wr22BossDex(t, run, b.dexSecret, b.dexPassword, []string{b.origin + "/api/v1/auth/oidc/callback", b.origin + "/api/v1/auth/identity-links/oidc/callback"})
	b.browserEnv = &wr20Environment{run: run, origin: b.origin, issuer: b.issuer, dexSecret: b.dexSecret, dexPassword: b.dexPassword}
	consoleSecret := randomPassword(t)
	consoleIssuer, _ := wr20Dex(t, run, consoleSecret, b.dexPassword, []string{"https://console.example.test/api/v1/auth/oidc/callback", "https://console.example.test/api/v1/auth/identity-links/oidc/callback"})
	setup := formalIAMSetup{Environment: "wr23-lifecycle", TrustDomain: "wr23.test", GatewayDNS: "gateway.wr23.test"}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		cfg.Runtime.Notification.PauseIdentityDelivery = true
		cfg.Runtime.Oidc.IssuerUrl = consoleIssuer
		cfg.Runtime.Oidc.LoginRedirectUri = "https://console.example.test/api/v1/auth/oidc/callback"
		cfg.Runtime.Oidc.IdentityLinkRedirectUri = "https://console.example.test/api/v1/auth/identity-links/oidc/callback"
		writeReferencePrivate(t, cfg.Runtime.Oidc.ClientSecretFile, []byte(consoleSecret))
		cfg.Runtime.BossOidc = proto.Clone(cfg.Runtime.Oidc).(*conf.OIDC)
		cfg.Runtime.BossOidc.ClientId = "ani-boss"
		cfg.Runtime.BossOidc.ClientSecretFile = filepath.Join(directory, "boss-oidc.secret")
		writeReferencePrivate(t, cfg.Runtime.BossOidc.ClientSecretFile, []byte(b.dexSecret))
		cfg.Runtime.BossOidc.IssuerUrl = b.issuer
		cfg.Runtime.BossOidc.LoginRedirectUri = b.origin + "/api/v1/auth/oidc/callback"
		cfg.Runtime.BossOidc.IdentityLinkRedirectUri = b.origin + "/api/v1/auth/identity-links/oidc/callback"
		cfg.Runtime.Notification.BossActionUrlBase = b.origin + "/password-action"
		cfg.Runtime.Redis.Namespace = "ani-iam:wr23:" + uuid.NewString()
		certPath, keyPath := writeProcessE2ELeafCertificate(t, directory, "boss-browser", "boss.wr22.test", x509.ExtKeyUsageServerAuth, ca, key)
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal("BOSS TLS configuration")
		}
		front.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
		front.StartTLS()
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		b.browserEnv.transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "boss.wr22.test:"+port {
				return nil, fmt.Errorf("unregistered BOSS authority")
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, front.Listener.Addr().String())
		}}
		caDigest := sha256.Sum256(ca.Raw)
		gateway := biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr23-gateway", DNSIdentity: setup.GatewayDNS}
		for _, method := range []string{"BeginOIDCLogin", "CompleteOIDCLogin", "RefreshSession", "LogoutSession", "ValidatePrincipal"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + method})
		}
		gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/CheckPermission"})
		for _, method := range []string{"ListPlatformMemberships", "CreatePlatformRole", "BindPlatformRole", "UnbindPlatformRole"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		}
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour), Workloads: []biz.BootstrapWorkload{gateway}}
		raw, _ := json.Marshal(manifest)
		digest := sha256.Sum256(raw)
		file := filepath.Join(directory, "wr23-workloads.json")
		dsnFile := filepath.Join(directory, "provisioner.secret")
		writeReferencePrivate(t, file, raw)
		writeReferencePrivate(t, dsnFile, []byte(postgresDSN(provisionerRole, db.provisionerPass, db.host, primaryDB, "wr23-offline-provisioner")))
		output, err := exec.Command(binary, "provision-workloads", "--manifest", file, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", setup.Environment, "--trust-domain", setup.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", dsnFile).CombinedOutput()
		if err != nil {
			t.Fatal("formal Gateway Workload provisioning failed")
		}
		recordReference(t, run, map[string]any{"kind": "wr23-workload-provisioning", "manifest_sha256": hex.EncodeToString(digest[:]), "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
	}
	b.iam = startFormalIAM(t, db, iamRedis, setup)
	configuration, coreOwner := prepareWR23CoreDatabase(t, run, e.plan)
	e.core = coreOwner
	redisFile := filepath.Join(run, "private/core-redis.secret")
	writeReferencePrivate(t, redisFile, []byte(referenceRedisURL(gatewayRedis)))
	configuration["redis_url_file"] = redisFile
	configFile := filepath.Join(run, "private/core-runtime.json")
	raw, _ := json.Marshal(configuration)
	writeReferencePrivate(t, configFile, raw)
	values := map[string]string{"IAM_TARGET_MODE": "wr23", "IAM_TARGET_GRPC_ADDR": b.iam.config.Server.Grpc.Addr, "IAM_TARGET_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": setup.Environment, "IAM_WORKLOAD_TRUST_DOMAIN": setup.TrustDomain, "IAM_TARGET_TLS_CA_FILE": b.iam.config.Server.Grpc.Tls.ClientCaFile, "IAM_TARGET_TLS_CERT_FILE": filepath.Join(b.iam.directory, "gateway-client.pem"), "IAM_TARGET_TLS_KEY_FILE": filepath.Join(b.iam.directory, "gateway-client-key.pem"), "IAM_BROWSER_BOSS_ORIGIN": b.origin, "IAM_BROWSER_CONSOLE_ORIGIN": "https://console.example.test", "GATEWAY_LISTEN_ADDR": gatewayAddress, "GATEWAY_HEALTH_LISTEN_ADDR": adminAddress, "ANI_CORE_LIFECYCLE_CONFIG": configFile}
	e.startGateway = func() *referenceProcess {
		p := startReferenceProcess(t, run, "wr23-core-gateway-"+uuid.NewString(), values, "gateway")
		waitReferenceHTTP(t, "http://"+adminAddress+"/readyz", p)
		return p
	}
	e.gateway = e.startGateway()
	return e
}

func prepareWR23CoreDatabase(t *testing.T, run, plan string) (map[string]string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	ownerPassword, runtimePassword := randomPassword(t), randomPassword(t)
	c, err := postgres.Run(ctx, postgresImage, postgres.WithDatabase("wr23_core"), postgres.WithUsername("wr23_core_owner"), postgres.WithPassword(ownerPassword), postgres.BasicWaitStrategies(), isolatedContainer(t, "core-postgres", "5432/tcp"))
	if err != nil {
		t.Fatal("start dedicated Core PostgreSQL")
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(c); err != nil {
			t.Error("terminate dedicated Core PostgreSQL")
		}
	})
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal("Core connection setup")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("Core connection parse")
	}
	u.Host = normalizeProcessE2ELoopbackAddress(t, u.Host)
	owner := mustPool(t, u.String())
	t.Cleanup(owner.Close)
	if _, err = owner.Exec(ctx, "CREATE ROLE wr23_core_runtime LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD '"+runtimePassword+"'"); err != nil {
		t.Fatal("create dedicated Core runtime role")
	}
	ownerFile, runtimeFile := filepath.Join(run, "private/core-owner.secret"), filepath.Join(run, "private/core-runtime.secret")
	writeReferencePrivate(t, ownerFile, []byte(u.String()))
	u.User = url.UserPassword("wr23_core_runtime", runtimePassword)
	writeReferencePrivate(t, runtimeFile, []byte(u.String()))
	config := map[string]any{"database": "wr23_core", "runtime_role": "wr23_core_runtime", "database_url_file": ownerFile, "plans": []any{map[string]any{"id": plan, "code": "wr23-standard", "limits": map[string]int64{"cpu_core": 8}}}, "quotas": []any{map[string]any{"resource_type": "cpu_core", "default_quota": 4, "enabled": true}, map[string]any{"resource_type": "memory_gb", "default_quota": 16, "enabled": true}}}
	file := filepath.Join(run, "private/core-initialize.json")
	raw, _ := json.Marshal(config)
	writeReferencePrivate(t, file, raw)
	output, err := exec.Command(filepath.Join(run, "private/core-lifecycle-admin"), "-action", "initialize", "-config", file).CombinedOutput()
	if err != nil {
		t.Fatal("formal empty Core initialization failed")
	}
	recordReference(t, run, map[string]any{"kind": "wr23-core-schema-initialize", "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
	return map[string]string{"database": "wr23_core", "runtime_role": "wr23_core_runtime", "producer": "core-control-service", "database_url_file": runtimeFile}, owner
}

func (e *wr23CoreEnvironment) firstAdministrator(t *testing.T) *http.Client {
	t.Helper()
	b := e.boss
	i := b.observedIdentity(t)
	manifest := biz.FirstAdministratorManifest{Version: 1, IntentID: mustV7(t), Environment: b.iam.config.Runtime.Environment, Email: i.Email, Issuer: i.Issuer, Subject: i.Subject, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	raw, _ := json.Marshal(manifest)
	digest := sha256.Sum256(raw)
	file := filepath.Join(b.iam.directory, "wr23-first-administrator.json")
	writeReferencePrivate(t, file, raw)
	output, err := exec.Command(b.iam.binary, "provision-first-administrator", "--manifest", file, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", manifest.Environment, "--dsn-file", filepath.Join(b.iam.directory, "provisioner.secret")).CombinedOutput()
	if err != nil {
		t.Fatal("formal first-administrator intent failed")
	}
	recordReference(t, b.run, map[string]any{"kind": "wr23-first-administrator-intent", "manifest_sha256": hex.EncodeToString(digest[:]), "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
	browser := b.browser(t)
	e.login(t, browser)
	return browser
}

func (e *wr23CoreEnvironment) login(t *testing.T, browser *http.Client) string {
	t.Helper()
	b := e.boss
	location, state := b.beginOIDC(t, browser, "")
	code, returned := b.dexAuthorize(t, location)
	if returned != state {
		t.Fatal("BOSS provider state mismatch")
	}
	status, body, _ := b.callback(t, browser, code, state)
	if status != 303 {
		t.Fatalf("formal BOSS callback status=%d reason=%v", status, body["code"])
	}
	return e.access(t, browser)
}
func (e *wr23CoreEnvironment) access(t *testing.T, browser *http.Client) string {
	t.Helper()
	status, body, _ := e.boss.request(t, browser, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatalf("formal BOSS refresh status=%d reason=%v", status, body["code"])
	}
	access, ok := body["access_token"].(string)
	if !ok || access == "" {
		t.Fatal("no BOSS access token")
	}
	return access
}
