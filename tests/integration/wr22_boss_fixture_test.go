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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
)

type wr22BossEnvironment struct {
	*wr22Environment
	browserEnv                             *wr20Environment
	origin, issuer, dexSecret, dexPassword string
	faults                                 *wr20OIDCFaults
}

func newWR22BossEnvironment(t *testing.T, extras ...wr22BootstrapExtra) *wr22BossEnvironment {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr22" {
		t.Fatal("WR22 run required")
	}
	b := &wr22BossEnvironment{dexSecret: randomPassword(t), dexPassword: randomPassword(t), faults: &wr20OIDCFaults{}}
	var gateway *url.URL
	front := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gateway == nil {
			http.Error(w, "starting", 503)
			return
		}
		httputil.NewSingleHostReverseProxy(gateway).ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	_, port, _ := net.SplitHostPort(front.Listener.Addr().String())
	b.origin = "https://boss.wr22.test:" + port
	b.issuer, _ = wr22BossDex(t, run, b.dexSecret, b.dexPassword, []string{b.origin + "/api/v1/auth/oidc/callback", b.origin + "/api/v1/auth/identity-links/oidc/callback"}, b.faults)
	b.browserEnv = &wr20Environment{run: run, origin: b.origin, issuer: b.issuer, dexSecret: b.dexSecret, dexPassword: b.dexPassword}
	b.wr22Environment = newWR22Environment(t, func(directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, manifest *biz.WorkloadBootstrapManifest) {
		cfg.Runtime.BossOidc = proto.Clone(cfg.Runtime.Oidc).(*conf.OIDC)
		cfg.Runtime.Notification.BossActionUrlBase = b.origin + "/password-action"
		o := cfg.Runtime.BossOidc
		o.ClientId = "ani-boss"
		o.IssuerUrl = b.issuer
		o.ClientSecretFile = filepath.Join(directory, "boss-oidc.secret")
		o.LoginRedirectUri = b.origin + "/api/v1/auth/oidc/callback"
		o.IdentityLinkRedirectUri = b.origin + "/api/v1/auth/identity-links/oidc/callback"
		writeReferencePrivate(t, o.ClientSecretFile, []byte(b.dexSecret))
		certPath, keyPath := writeProcessE2ELeafCertificate(t, directory, "boss-browser", "boss.wr22.test", x509.ExtKeyUsageServerAuth, ca, key)
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal("BOSS TLS setup failed")
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
		for _, extra := range extras {
			extra(directory, ca, key, cfg, manifest)
		}
	})
	gateway, _ = url.Parse("http://" + b.gatewayAddress)
	return b
}

func (b *wr22BossEnvironment) browser(t *testing.T) *http.Client { return b.browserEnv.browser(t) }
func (b *wr22BossEnvironment) request(t *testing.T, c *http.Client, method, path, token string, body any, headers map[string]string) (int, map[string]any, *http.Response) {
	proof := map[string]string{}
	if c.Jar != nil {
		u, _ := url.Parse(b.origin + "/api/v1/auth")
		for _, cookie := range c.Jar.Cookies(u) {
			if cookie.Name == "ani_boss_csrf" {
				proof["X-CSRF-Token"] = cookie.Value
			}
		}
	}
	for k, v := range headers {
		proof[k] = v
	}
	return b.browserEnv.request(t, c, method, path, token, body, proof)
}
func (b *wr22BossEnvironment) beginOIDC(t *testing.T, c *http.Client, key string) (string, string) {
	t.Helper()
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	status, body, _ := b.request(t, c, "POST", "/auth/oidc/begin", "", map[string]any{"audience": "boss", "boundary": map[string]string{"type": "platform"}}, headers)
	if status != 200 {
		t.Fatalf("BOSS begin status=%d reason=%v", status, body["code"])
	}
	location, _ := body["authorization_url"].(string)
	state, _ := body["state"].(string)
	u, err := url.Parse(location)
	issuer, _ := url.Parse(b.issuer)
	if err != nil || u.Host != issuer.Host || u.Path != "/dex/auth" || u.Query().Get("client_id") != "ani-boss" || u.Query().Get("state") != state || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") == "" || u.Query().Get("redirect_uri") != b.origin+"/api/v1/auth/oidc/callback" {
		t.Fatal("BOSS real provider parameters differ")
	}
	return location, state
}
func (b *wr22BossEnvironment) callback(t *testing.T, c *http.Client, code, state string) (int, map[string]any, *http.Response) {
	return b.browserEnv.oidcCallback(t, c, code, state)
}

// Observe the exact identity through the real provider before creating the
// offline owner intent. This performs no database identity or permission seed.
func (b *wr22BossEnvironment) observedIdentity(t *testing.T) biz.OIDCVerifiedIdentity {
	t.Helper()
	ctx := context.Background()
	redirect := b.origin + "/api/v1/auth/oidc/callback"
	p, err := data.NewCoreOSOIDCProvider(ctx, data.CoreOSOIDCProviderConfig{Name: "dex", IssuerURL: b.issuer, ClientID: "ani-boss", ClientSecret: b.dexSecret, RedirectURIs: []string{redirect}, HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	if err != nil {
		t.Fatal("BOSS provider discovery failed")
	}
	state, nonce, verifier := randomPassword(t), randomPassword(t), strings.Repeat("v", 43)
	location, err := p.AuthorizationURL(biz.OIDCAuthorizationRequest{State: state, Nonce: nonce, CodeVerifier: verifier, RedirectURI: redirect})
	if err != nil {
		t.Fatal("BOSS observation authorization failed")
	}
	code, returned := b.dexAuthorize(t, location)
	if returned != state {
		t.Fatal("observation state mismatch")
	}
	i, err := p.ExchangeAndVerify(ctx, biz.OIDCExchangeRequest{Code: code, Nonce: nonce, CodeVerifier: verifier, RedirectURI: redirect})
	if err != nil || i.Issuer != b.issuer || i.Email != "wr22-boss@example.test" || !i.EmailVerified {
		t.Fatal("BOSS exact identity not verified")
	}
	return i
}
func (b *wr22BossEnvironment) registerIntent(t *testing.T, i biz.OIDCVerifiedIdentity) []string {
	t.Helper()
	m := biz.FirstAdministratorManifest{Version: 1, IntentID: mustV7(t), Environment: "wr22-authentication", Email: i.Email, Issuer: i.Issuer, Subject: i.Subject, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	raw, _ := json.Marshal(m)
	digest := sha256.Sum256(raw)
	path := filepath.Join(b.iam.directory, "boss-first-administrator.json")
	writeReferencePrivate(t, path, raw)
	args := []string{"provision-first-administrator", "--manifest", path, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", m.Environment, "--dsn-file", filepath.Join(b.iam.directory, "provisioner.secret")}
	output, err := exec.Command(b.iam.binary, args...).CombinedOutput()
	if err != nil {
		t.Fatal("formal BOSS intent registration failed")
	}
	recordReference(t, b.run, map[string]any{"kind": "first-administrator-registration", "manifest_sha256": hex.EncodeToString(digest[:]), "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
	return args
}
func wr22BossDex(t *testing.T, run, secret, password string, redirects []string, faults ...*wr20OIDCFaults) (string, testcontainers.Container) {
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
	cfg := fmt.Sprintf("issuer: %s/dex\nstorage:\n  type: memory\nweb:\n  http: 0.0.0.0:5556\nlogger:\n  level: error\noauth2:\n  responseTypes: [code]\n  skipApprovalScreen: true\nstaticClients:\n- id: ani-boss\n  secret: %q\n  name: WR22 BOSS\n  redirectURIs:\n", front.URL, secret)
	for _, redirect := range redirects {
		cfg += fmt.Sprintf("  - %q\n", redirect)
	}
	cfg += fmt.Sprintf("- id: wr22-boss-foreign\n  secret: %q\n  name: WR22 BOSS foreign audience\n  redirectURIs:\n", secret)
	for _, redirect := range redirects {
		cfg += fmt.Sprintf("  - %q\n", redirect)
	}
	cfg += fmt.Sprintf("enablePasswordDB: true\nstaticPasswords:\n- email: wr22-boss@example.test\n  hash: %q\n  username: wr22-boss\n  emailVerified: true\n  userID: 08a8684b-db88-4b73-90a9-3cd1661f5466\n", string(hash))
	// This second isolated provider identity is deliberately unlinked. Only the
	// formal identity-link endpoint may attach it to the existing fixture Human.
	cfg += fmt.Sprintf("- email: wr22-0@example.test\n  hash: %q\n  username: wr22-existing-human\n  emailVerified: true\n  userID: 01a09a58-2f9d-720b-b812-7ad77e178299\n", string(hash))
	file := filepath.Join(run, "private", "dex-"+uuid.NewString()+".yaml")
	writeReferencePrivate(t, file, []byte(cfg))
	c, err := testcontainers.Run(context.Background(), dexImage, testcontainers.WithEntrypoint("/usr/local/bin/dex"), testcontainers.WithCmd("serve", "/etc/dex/config.yaml"), testcontainers.WithFiles(testcontainers.ContainerFile{HostFilePath: file, ContainerFilePath: "/etc/dex/config.yaml", FileMode: 0o644}), testcontainers.WithExposedPorts("5556/tcp"), isolatedContainer(t, "wr22-boss-dex", "5556/tcp"), testcontainers.WithWaitStrategy(wait.ForHTTP("/dex/.well-known/openid-configuration").WithPort("5556/tcp").WithStartupTimeout(time.Minute)))
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

func (e *wr22BossEnvironment) dexAuthorize(t *testing.T, location string) (string, string) {
	return e.dexAuthorizeAccount(t, location, "wr22-boss@example.test")
}
func (e *wr22BossEnvironment) dexAuthorizeAccount(t *testing.T, location, account string) (string, string) {
	t.Helper()
	authorization, err := url.Parse(location)
	if err != nil {
		t.Fatal("invalid Dex authorization location")
	}
	expectedCallback := authorization.Query().Get("redirect_uri")
	if expectedCallback != e.origin+"/api/v1/auth/oidc/callback" && expectedCallback != e.origin+"/api/v1/auth/identity-links/oidc/callback" {
		t.Fatal("unregistered Dex callback")
	}
	issuer, _ := url.Parse(e.issuer)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: 8 * time.Second, Jar: jar, CheckRedirect: func(r *http.Request, previous []*http.Request) error {
		if strings.HasPrefix(r.URL.String(), expectedCallback+"?") {
			return http.ErrUseLastResponse
		}
		if r.URL.Host != issuer.Host || len(previous) > 10 {
			return fmt.Errorf("unregistered Dex redirect")
		}
		return nil
	}}
	r, err := c.Get(location)
	if err != nil {
		t.Fatal("real Dex authorization failed")
	}
	formAction := passwordFormAction(t, r.Body, r.Request.URL)
	r.Body.Close()
	action, _ := url.Parse(formAction)
	if action.Host != issuer.Host {
		t.Fatal("Dex form escaped registered issuer")
	}
	r, err = c.PostForm(formAction, url.Values{"login": {account}, "password": {e.dexPassword}})
	if err != nil {
		t.Fatal("real Dex password form failed")
	}
	defer r.Body.Close()
	callback, err := r.Location()
	if err != nil || callback.Scheme+"://"+callback.Host+callback.Path != expectedCallback {
		t.Fatal("Dex did not use fixed callback")
	}
	code, state := callback.Query().Get("code"), callback.Query().Get("state")
	if code == "" || state == "" {
		t.Fatal("Dex callback credential absent")
	}
	return code, state
}
