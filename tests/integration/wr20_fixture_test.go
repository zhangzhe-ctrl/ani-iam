//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"golang.org/x/crypto/bcrypt"
)

type wr20OIDCFaults struct {
	mu      sync.Mutex
	idToken string
}

func (f *wr20OIDCFaults) replace(token string) { f.mu.Lock(); defer f.mu.Unlock(); f.idToken = token }
func (f *wr20OIDCFaults) take() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.idToken
	f.idToken = ""
	return s
}

type wr20Environment struct {
	dexFault                       *wr20OIDCFaults
	run, origin, password          string
	postgresID                     string
	issuer, dexSecret, dexPassword string
	dexContainer                   testcontainers.Container
	iam                            *formalIAM
	owner                          *pgxpool.Pool
	iamRedis, gatewayRedis         *redis.Client
	redisContainer                 testcontainers.Container
	transport                      *http.Transport
	front                          *httptest.Server
	humans                         []uuid.UUID
	accounts                       []string
}

// Dex is the real pinned provider. This transparent loopback proxy gives it
// a fixed issuer before Docker allocates its task-exclusive published port.
func wr20Dex(t *testing.T, run, secret, password string, redirects []string, faults ...*wr20OIDCFaults) (string, testcontainers.Container) {
	t.Helper()
	var target *url.URL
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target == nil {
			http.Error(w, "starting", 503)
			return
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ModifyResponse = func(response *http.Response) error {
			if len(faults) == 0 || r.URL.Path != "/dex/token" || response.StatusCode != 200 {
				return nil
			}
			token := faults[0].take()
			if token == "" {
				return nil
			}
			raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if err != nil {
				return fmt.Errorf("read real Dex token response")
			}
			var body map[string]any
			if json.Unmarshal(raw, &body) != nil {
				return fmt.Errorf("invalid real Dex token response")
			}
			body["id_token"] = token
			raw, _ = json.Marshal(body)
			response.Body = io.NopCloser(bytes.NewReader(raw))
			response.ContentLength = int64(len(raw))
			response.Header.Set("Content-Length", strconv.Itoa(len(raw)))
			return nil
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal("Dex seed hash failed")
	}
	cfg := fmt.Sprintf("issuer: %s/dex\nstorage:\n  type: memory\nweb:\n  http: 0.0.0.0:5556\nlogger:\n  level: error\noauth2:\n  responseTypes: [code]\n  skipApprovalScreen: true\nstaticClients:\n- id: ani-console\n  secret: %q\n  name: WR20\n  redirectURIs:\n", front.URL, secret)
	for _, redirect := range redirects {
		cfg += fmt.Sprintf("  - %q\n", redirect)
	}
	cfg += fmt.Sprintf("- id: wr20-foreign\n  secret: %q\n  name: WR20 foreign audience\n  redirectURIs:\n", secret)
	for _, redirect := range redirects {
		cfg += fmt.Sprintf("  - %q\n", redirect)
	}
	cfg += fmt.Sprintf("enablePasswordDB: true\nstaticPasswords:\n- email: wr20-oidc@example.test\n  hash: %q\n  username: wr20\n  emailVerified: true\n  userID: 08a8684b-db88-4b73-90a9-3cd1661f5466\n", string(hash))
	file := filepath.Join(run, "private", "dex-"+uuid.NewString()+".yaml")
	writeReferencePrivate(t, file, []byte(cfg))
	c, err := testcontainers.Run(context.Background(), dexImage, testcontainers.WithEntrypoint("/usr/local/bin/dex"), testcontainers.WithCmd("serve", "/etc/dex/config.yaml"), testcontainers.WithFiles(testcontainers.ContainerFile{HostFilePath: file, ContainerFilePath: "/etc/dex/config.yaml", FileMode: 0o644}), testcontainers.WithExposedPorts("5556/tcp"), isolatedContainer(t, "wr20-dex", "5556/tcp"), testcontainers.WithWaitStrategy(wait.ForHTTP("/dex/.well-known/openid-configuration").WithPort("5556/tcp").WithStartupTimeout(time.Minute)))
	if err != nil {
		t.Fatal("pinned Dex startup failed")
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	endpoint, err := c.Endpoint(context.Background(), "")
	if err != nil {
		t.Fatal("Dex endpoint unavailable")
	}
	target, _ = url.Parse("http://" + endpoint)
	return front.URL + "/dex", c
}

type wr20ExtraSetup func(*wr20Environment, *biz.WorkloadBootstrapManifest, *conf.Bootstrap, string, *x509.Certificate, ed25519.PrivateKey)

func newWR20Environment(t *testing.T, extras ...wr20ExtraSetup) *wr20Environment {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr20" {
		t.Fatal("WR20 dedicated run required")
	}
	e := &wr20Environment{run: run, password: randomPassword(t)}
	ctx := context.Background()
	db := newPostgresEnvironment(t)
	resources, err := os.ReadFile(filepath.Join(run, "resources.jsonl"))
	if err != nil {
		t.Fatal("registered resource inventory unavailable")
	}
	for _, line := range bytes.Split(resources, []byte("\n")) {
		var r map[string]string
		if json.Unmarshal(line, &r) == nil && r["event"] == "created" && r["kind"] == "postgres" && r["test"] == t.Name() {
			if e.postgresID != "" {
				t.Fatal("ambiguous PostgreSQL resource")
			}
			e.postgresID = r["container_id"]
		}
	}
	if e.postgresID == "" {
		t.Fatal("PostgreSQL resource was not registered")
	}
	e.owner = mustPool(t, db.migrationDSN(primaryDB))
	t.Cleanup(e.owner.Close)
	hash, err := data.NewArgon2idPasswordHasher().Hash(e.password)
	if err != nil {
		t.Fatal("Human seed hash failed")
	}
	e.humans, _ = seedReferenceHumans(t, e.owner, hash)
	// Stable historic helper supplies only fresh data in this new private DB.
	// Rename its test contacts to this ticket; no existing environment is used.
	for i, id := range e.humans {
		email := fmt.Sprintf("wr20-%d@example.test", i)
		e.accounts = append(e.accounts, email)
		if _, err = e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email=$2 WHERE principal_id=$1`, id, email); err != nil {
			t.Fatal(err)
		}
		if _, err = e.owner.Exec(ctx, `UPDATE identities SET subject=$2 WHERE principal_id=$1`, id, email); err != nil {
			t.Fatal(err)
		}
	}
	e.iamRedis, e.redisContainer = newIsolatedRedis(t, ctx)
	e.gatewayRedis, _ = newIsolatedRedis(t, ctx)
	aniDSN := newReferenceOwnerDatabase(t, run)
	gatewayAddress, adminAddress := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	target, _ := url.Parse("http://" + gatewayAddress)
	e.front = httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(target))
	t.Cleanup(e.front.Close)
	_, port, _ := net.SplitHostPort(e.front.Listener.Addr().String())
	e.origin = "https://console.wr20.test:" + port
	secret := randomPassword(t)
	e.dexSecret, e.dexPassword = secret, randomPassword(t)
	e.dexFault = &wr20OIDCFaults{}
	issuer, dexContainer := wr20Dex(t, run, secret, e.dexPassword, []string{e.origin + "/api/v1/auth/oidc/callback", e.origin + "/api/v1/auth/identity-links/oidc/callback"}, e.dexFault)
	e.issuer, e.dexContainer = issuer, dexContainer
	setup := formalIAMSetup{Environment: "wr20-authentication", TrustDomain: "wr20.test", GatewayDNS: "gateway.wr20.test"}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		cfg.Runtime.Oidc.IssuerUrl = issuer
		cfg.Runtime.Oidc.LoginRedirectUri = e.origin + "/api/v1/auth/oidc/callback"
		cfg.Runtime.Oidc.IdentityLinkRedirectUri = e.origin + "/api/v1/auth/identity-links/oidc/callback"
		writeReferencePrivate(t, cfg.Runtime.Oidc.ClientSecretFile, []byte(secret))
		cfg.Runtime.Redis.Namespace = "ani-iam:wr20:" + uuid.NewString()
		caDigest := sha256.Sum256(ca.Raw)
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour)}
		gateway := biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr20-gateway", DNSIdentity: setup.GatewayDNS}
		for _, method := range []string{"PasswordLogin", "RefreshSession", "LogoutSession", "SwitchTenant", "ListSessions", "BeginOIDCLogin", "CompleteOIDCLogin", "BeginOIDCIdentityLink", "CompleteOIDCIdentityLink", "RequestPasswordAction", "CompletePasswordAction"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + method})
		}
		manifest.Workloads = []biz.BootstrapWorkload{gateway}
		for _, extra := range extras {
			extra(e, &manifest, cfg, directory, ca, key)
		}
		raw, _ := json.Marshal(manifest)
		digest := sha256.Sum256(raw)
		mf, dsnf := filepath.Join(directory, "wr20-bootstrap.json"), filepath.Join(directory, "provisioner.secret")
		writeReferencePrivate(t, mf, raw)
		writeReferencePrivate(t, dsnf, []byte(postgresDSN(provisionerRole, db.provisionerPass, db.host, primaryDB, "wr20-bootstrap")))
		cmd := exec.Command(binary, "provision-workloads", "--manifest", mf, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", setup.Environment, "--trust-domain", setup.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", dsnf)
		output, err := cmd.CombinedOutput()
		if err != nil {
			_ = os.WriteFile(filepath.Join(directory, "bootstrap.private.log"), output, 0o600)
			t.Fatal("formal WR20 bootstrap failed")
		}
		recordReference(t, run, map[string]any{"kind": "bootstrap", "manifest_sha256": hex.EncodeToString(digest[:]), "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
		certFile, keyFile := writeProcessE2ELeafCertificate(t, directory, "browser", "console.wr20.test", x509.ExtKeyUsageServerAuth, ca, key)
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			t.Fatal("HTTPS key pair invalid")
		}
		e.front.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
		e.front.StartTLS()
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		e.transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "console.wr20.test:"+port {
				return nil, fmt.Errorf("unregistered test authority")
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, e.front.Listener.Addr().String())
		}}
	}
	e.iam = startFormalIAM(t, db, e.iamRedis, setup)
	gateway := startReferenceProcess(t, run, "wr20-gateway-"+uuid.NewString(), map[string]string{"IAM_TARGET_MODE": "wr20", "IAM_TARGET_GRPC_ADDR": e.iam.config.Server.Grpc.Addr, "IAM_TARGET_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": setup.Environment, "IAM_WORKLOAD_TRUST_DOMAIN": setup.TrustDomain, "IAM_TARGET_TLS_CA_FILE": e.iam.config.Server.Grpc.Tls.ClientCaFile, "IAM_TARGET_TLS_CERT_FILE": filepath.Join(e.iam.directory, "gateway-client.pem"), "IAM_TARGET_TLS_KEY_FILE": filepath.Join(e.iam.directory, "gateway-client-key.pem"), "IAM_BROWSER_CONSOLE_ORIGIN": e.origin, "GATEWAY_LISTEN_ADDR": gatewayAddress, "GATEWAY_HEALTH_LISTEN_ADDR": adminAddress, "DATABASE_URL": aniDSN, "GATEWAY_REDIS_URL": referenceRedisURL(e.gatewayRedis), "ANI_AUTH_MODE": "auth_service"}, "ani-gateway")
	waitReferenceHTTP(t, "http://"+adminAddress+"/readyz", gateway)
	recordReference(t, run, map[string]any{"stage": "A", "origin": e.origin, "iam_binary": e.iam.binary, "gateway_binary": filepath.Join(run, "private", "ani-gateway"), "source": "formal processes; transparent TLS termination; real PG/Redis/Dex"})
	return e
}

func (e *wr20Environment) browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: e.transport, Jar: jar, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (e *wr20Environment) request(t *testing.T, client *http.Client, method, path, token string, body any, headers map[string]string) (int, map[string]any, *http.Response) {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal("encode request failed")
		}
	}
	r, err := http.NewRequest(method, e.origin+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal("request creation failed")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", uuid.NewString())
	r.Header.Set("Origin", e.origin)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if client.Jar != nil {
		u, _ := url.Parse(e.origin + "/api/v1/auth")
		for _, c := range client.Jar.Cookies(u) {
			if c.Name == "ani_console_csrf" {
				r.Header.Set("X-CSRF-Token", c.Value)
			}
		}
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("formal HTTPS request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal("response read failed")
	}
	var result map[string]any
	if len(raw) > 0 && strings.Contains(response.Header.Get("Content-Type"), "json") {
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("invalid JSON response")
		}
	}
	return response.StatusCode, result, response
}
func (e *wr20Environment) login(t *testing.T, index int) (*http.Client, string, map[string]any, *http.Response) {
	t.Helper()
	c := e.browser(t)
	code, body, response := e.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": e.accounts[index], "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[index].String()}}, nil)
	if code != 200 {
		t.Fatalf("login status %d reason %s", code, body["code"])
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("login produced no access credential")
	}
	return c, token, body, response
}
