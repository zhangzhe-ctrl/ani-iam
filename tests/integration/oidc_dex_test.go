//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/html"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

const (
	dexImage = "ghcr.io/dexidp/dex:v2.40.0@sha256:3e35d5d0f7dbd33fbadc36a71ff58cf4097ab98d73d22f6cb9a6471a32e028af"

	dexClientID    = "ani-console"
	dexRedirectURI = "https://console.test.example/auth/oidc/callback"
	dexLogin       = "admin@example.com"
	dexVerifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func TestCoreOSOIDCProviderCompletesAuthorizationCodePKCEAgainstPinnedDex(t *testing.T) {
	ctx := context.Background()
	dexClientSecret, dexPassword := randomPassword(t), randomPassword(t)
	recordFixtureSecrets(t, map[string]string{"dex-client": dexClientSecret, "dex-user": dexPassword})
	issuerURL, dexHTTPClient, dexContainer := startPinnedDex(t, ctx, dexClientSecret, dexPassword)
	t.Cleanup(func() {
		terminateContext, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := testcontainers.TerminateContainer(dexContainer, testcontainers.StopContext(terminateContext)); err != nil {
			t.Errorf("terminate Dex container: %v", err)
		}
	})

	provider, err := data.NewCoreOSOIDCProvider(ctx, data.CoreOSOIDCProviderConfig{
		Name: "dex", IssuerURL: issuerURL, ClientID: dexClientID, ClientSecret: dexClientSecret,
		RedirectURIs: []string{dexRedirectURI}, HTTPClient: dexHTTPClient,
	})
	if err != nil {
		t.Fatalf("NewCoreOSOIDCProvider() error = %v", err)
	}

	for _, test := range []struct {
		name             string
		state            string
		nonce            string
		exchangeVerifier string
		wantError        bool
	}{
		{name: "verified token", state: "state-success", nonce: "nonce-success", exchangeVerifier: dexVerifier},
		{name: "wrong PKCE verifier", state: "state-pkce", nonce: "nonce-pkce", exchangeVerifier: strings.Repeat("z", 43), wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizationURL, err := provider.AuthorizationURL(biz.OIDCAuthorizationRequest{
				State: test.state, Nonce: test.nonce, CodeVerifier: dexVerifier, RedirectURI: dexRedirectURI,
			})
			if err != nil {
				t.Fatalf("AuthorizationURL() error = %v", err)
			}
			code, returnedState := completeDexPasswordLogin(t, dexHTTPClient.Transport, authorizationURL, dexPassword)
			if returnedState != test.state {
				t.Fatalf("returned state does not match submitted state")
			}
			identity, err := provider.ExchangeAndVerify(ctx, biz.OIDCExchangeRequest{
				Code: code, Nonce: test.nonce, CodeVerifier: test.exchangeVerifier, RedirectURI: dexRedirectURI,
			})
			if test.wantError {
				if err == nil {
					t.Fatal("ExchangeAndVerify() error = nil, want PKCE rejection")
				}
				return
			}
			if err != nil {
				t.Fatalf("ExchangeAndVerify() error = %v", err)
			}
			if identity.Issuer != issuerURL || identity.Subject == "" ||
				identity.Email != dexLogin || !identity.EmailVerified {
				t.Fatalf("ExchangeAndVerify() returned unexpected verified identity: %#v", identity)
			}
		})
	}
}

func startPinnedDex(t *testing.T, ctx context.Context, clientSecret, password string) (string, *http.Client, testcontainers.Container) {
	t.Helper()
	// Keep the issuer stable inside Dex while allowing Docker to allocate an
	// atomic random host port. The test clients below map only this logical
	// loopback authority to that task-owned port, avoiding a reserve/close/bind
	// race when integration suites run concurrently.
	const issuerURL = "http://127.0.0.1:5556/dex"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`issuer: %s
storage:
  type: memory
web:
  http: 0.0.0.0:5556
logger:
  level: error
oauth2:
  responseTypes: ["code"]
  skipApprovalScreen: true
staticClients:
- id: %s
  secret: %s
  name: ANI Console integration test
  redirectURIs:
  - %s
enablePasswordDB: true
staticPasswords:
- email: %s
  hash: %q
  username: admin
  emailVerified: true
  userID: 08a8684b-db88-4b73-90a9-3cd1661f5466
`, issuerURL, dexClientID, clientSecret, dexRedirectURI, dexLogin, string(passwordHash))
	configPath := filepath.Join(t.TempDir(), "dex.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write isolated Dex config: %v", err)
	}

	dexContainer, err := testcontainers.Run(
		ctx,
		dexImage,
		testcontainers.WithEntrypoint("/usr/local/bin/dex"),
		testcontainers.WithCmd("serve", "/etc/dex/config.yaml"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath: configPath, ContainerFilePath: "/etc/dex/config.yaml", FileMode: 0o644,
		}),
		testcontainers.WithExposedPorts("5556/tcp"),
		isolatedContainer(t, "dex", "5556/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/dex/.well-known/openid-configuration").WithPort("5556/tcp").WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start pinned Dex container: %v", err)
	}
	endpoint, err := dexContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("resolve pinned Dex endpoint: %v", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "127.0.0.1:5556" {
			return nil, fmt.Errorf("unexpected Dex test authority %q", address)
		}
		return dialer.DialContext(ctx, network, endpoint)
	}
	return issuerURL, &http.Client{Timeout: 5 * time.Second, Transport: transport}, dexContainer
}

func completeDexPasswordLogin(t *testing.T, transport http.RoundTripper, authorizationURL, password string) (string, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create Dex cookie jar: %v", err)
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Host == "console.test.example" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	response, err := client.Get(authorizationURL)
	if err != nil {
		t.Fatalf("open Dex authorization URL: %v", err)
	}
	defer response.Body.Close()
	formAction := passwordFormAction(t, response.Body, response.Request.URL)

	form := url.Values{"login": {dexLogin}, "password": {password}}
	request, err := http.NewRequest(http.MethodPost, formAction, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build Dex password request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = client.Do(request)
	if err != nil {
		t.Fatalf("submit Dex password form: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		t.Fatalf("Dex callback status = %d, body = %q", response.StatusCode, string(body))
	}
	location, err := response.Location()
	if err != nil {
		t.Fatalf("parse Dex callback location: %v", err)
	}
	if location.Scheme != "https" || location.Host != "console.test.example" || location.Path != "/auth/oidc/callback" {
		t.Fatalf("Dex callback target is not the fixed redirect")
	}
	code := location.Query().Get("code")
	state := location.Query().Get("state")
	if code == "" || state == "" {
		t.Fatal("Dex callback omitted code or state")
	}
	return code, state
}

func passwordFormAction(t *testing.T, body io.Reader, documentURL *url.URL) string {
	t.Helper()
	document, err := html.Parse(body)
	if err != nil {
		t.Fatalf("parse Dex password page: %v", err)
	}
	var walk func(*html.Node) string
	walk = func(node *html.Node) string {
		if node.Type == html.ElementNode && node.Data == "form" {
			for _, attribute := range node.Attr {
				if attribute.Key == "action" {
					return attribute.Val
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if action := walk(child); action != "" {
				return action
			}
		}
		return ""
	}
	action := walk(document)
	if action == "" {
		t.Fatal("Dex password page omitted form action")
	}
	parsedAction, err := documentURL.Parse(action)
	if err != nil {
		t.Fatalf("parse Dex password form action: %v", err)
	}
	return parsedAction.String()
}
