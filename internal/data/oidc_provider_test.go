package data

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const (
	testOIDCClientID     = "ani-console"
	testOIDCClientSecret = "test-only-client-secret"
	testOIDCRedirectURI  = "https://console.test.example/auth/oidc/callback"
	testOIDCNonce        = "opaque-nonce"
	testOIDCCode         = "sensitive-code-value"
	testOIDCVerifier     = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func TestCoreOSOIDCProviderUsesPKCES256AndVerifiesIDToken(t *testing.T) {
	const (
		clientID     = "ani-console"
		clientSecret = "test-only-client-secret"
		redirectURI  = "https://console.test.example/auth/oidc/callback"
		state        = "opaque-state"
		nonce        = "opaque-nonce"
		code         = "authorization-code"
		verifier     = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge    = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate OIDC test key: %v", err)
	}
	issuer := &oidctest.Server{PublicKeys: []oidctest.PublicKey{{
		PublicKey: &privateKey.PublicKey, KeyID: "test-key", Algorithm: oidc.RS256,
	}}}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/token" {
			issuer.ServeHTTP(response, request)
			return
		}
		if err := request.ParseForm(); err != nil {
			http.Error(response, "invalid form", http.StatusBadRequest)
			return
		}
		requestClientID, requestClientSecret, basicAuthentication := request.BasicAuth()
		if !basicAuthentication || requestClientID != clientID || requestClientSecret != clientSecret ||
			request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("code") != code ||
			request.Form.Get("redirect_uri") != redirectURI || request.Form.Get("code_verifier") != verifier {
			http.Error(response, "invalid authorization-code exchange", http.StatusUnauthorized)
			return
		}
		claims := fmt.Sprintf(
			`{"iss":%q,"aud":%q,"sub":"dex-user-1","exp":%s,"iat":%s,"nonce":%q,"email":"User@Example.COM","email_verified":true}`,
			server.URL, clientID, strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10),
			strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10), nonce,
		)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"access_token": "opaque-access-token", "token_type": "Bearer", "expires_in": 60,
			"id_token": oidctest.SignIDToken(privateKey, "test-key", oidc.RS256, claims),
		})
	}))
	t.Cleanup(server.Close)
	issuer.SetIssuer(server.URL)

	provider, err := NewCoreOSOIDCProvider(context.Background(), CoreOSOIDCProviderConfig{
		Name: "dex", IssuerURL: server.URL, ClientID: clientID, ClientSecret: clientSecret,
		RedirectURIs: []string{redirectURI}, HTTPClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatalf("NewCoreOSOIDCProvider() error = %v", err)
	}
	authorizationURL, err := provider.AuthorizationURL(biz.OIDCAuthorizationRequest{
		State: state, Nonce: nonce, CodeVerifier: verifier, RedirectURI: redirectURI,
	})
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"client_id": clientID, "redirect_uri": redirectURI, "response_type": "code",
		"state": state, "nonce": nonce, "code_challenge": challenge, "code_challenge_method": "S256",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("authorization query %s = %q, want %q", key, got, want)
		}
	}
	if scopes := strings.Fields(query.Get("scope")); !containsString(scopes, oidc.ScopeOpenID) || !containsString(scopes, "email") {
		t.Fatalf("authorization scopes = %#v", scopes)
	}

	identity, err := provider.ExchangeAndVerify(context.Background(), biz.OIDCExchangeRequest{
		Code: code, Nonce: nonce, CodeVerifier: verifier, RedirectURI: redirectURI,
	})
	if err != nil {
		t.Fatalf("ExchangeAndVerify() error = %v", err)
	}
	if identity.Issuer != server.URL || identity.Subject != "dex-user-1" || identity.Email != "User@Example.COM" || !identity.EmailVerified {
		t.Fatalf("ExchangeAndVerify() = %#v", identity)
	}
}

func TestNewCoreOSOIDCProviderRejectsNonCanonicalIssuer(t *testing.T) {
	provider, issuerURL, _ := newOIDCProviderForFailureTest(t, "")
	if provider == nil {
		t.Fatal("test provider is nil")
	}

	_, err := NewCoreOSOIDCProvider(context.Background(), CoreOSOIDCProviderConfig{
		Name: "dex", IssuerURL: " " + issuerURL, ClientID: testOIDCClientID, ClientSecret: testOIDCClientSecret,
		RedirectURIs: []string{testOIDCRedirectURI}, HTTPClient: &http.Client{Timeout: time.Second},
	})
	if !errors.Is(err, ErrInvalidOIDCProviderConfiguration) {
		t.Fatalf("NewCoreOSOIDCProvider() error = %v, want invalid configuration", err)
	}
}

func TestNewCoreOSOIDCProviderRejectsResolverBasedHTTPIssuer(t *testing.T) {
	_, err := NewCoreOSOIDCProvider(context.Background(), CoreOSOIDCProviderConfig{
		Name: "dex", IssuerURL: "http://localhost:5556/dex", ClientID: testOIDCClientID, ClientSecret: testOIDCClientSecret,
		RedirectURIs: []string{testOIDCRedirectURI}, HTTPClient: &http.Client{Timeout: time.Second},
	})
	if !errors.Is(err, ErrInvalidOIDCProviderConfiguration) {
		t.Fatalf("NewCoreOSOIDCProvider() error = %v, want invalid configuration", err)
	}
}

func TestLoadOIDCClientSecretFileRejectsNonVisibleOrUnboundedInput(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents []byte
		want     string
		wantErr  bool
	}{
		{name: "visible ASCII", contents: []byte("test-only-client-secret"), want: "test-only-client-secret"},
		{name: "empty", wantErr: true},
		{name: "trailing newline", contents: []byte("test-only-client-secret\n"), wantErr: true},
		{name: "embedded NUL", contents: []byte("test\x00secret"), wantErr: true},
		{name: "too large", contents: []byte(strings.Repeat("x", 4097)), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			secretPath := filepath.Join(t.TempDir(), "oidc-client-secret")
			if err := os.WriteFile(secretPath, test.contents, 0o600); err != nil {
				t.Fatalf("write OIDC client secret fixture: %v", err)
			}
			secret, err := LoadOIDCClientSecretFile(secretPath)
			if test.wantErr {
				if err == nil || secret != "" {
					t.Fatalf("LoadOIDCClientSecretFile() = %q, %v; want rejection", secret, err)
				}
				return
			}
			if err != nil || secret != test.want {
				t.Fatalf("LoadOIDCClientSecretFile() = %q, %v; want %q", secret, err, test.want)
			}
		})
	}
}

func TestCoreOSOIDCProviderRejectsTokenBindingAndCryptographicFailures(t *testing.T) {
	for _, failure := range []string{"nonce", "issuer", "audience", "signature", "expiry", "pkce"} {
		t.Run(failure, func(t *testing.T) {
			provider, _, _ := newOIDCProviderForFailureTest(t, failure)
			_, err := provider.ExchangeAndVerify(context.Background(), biz.OIDCExchangeRequest{
				Code: testOIDCCode, Nonce: testOIDCNonce, CodeVerifier: testOIDCVerifier, RedirectURI: testOIDCRedirectURI,
			})
			if err == nil {
				t.Fatalf("ExchangeAndVerify() error = nil for %s failure", failure)
			}
			for _, secret := range []string{testOIDCCode, testOIDCNonce, testOIDCVerifier} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("ExchangeAndVerify() error leaked secret %q: %v", secret, err)
				}
			}
		})
	}
}

func TestCoreOSOIDCProviderClassifiesAvailabilityWithoutLeakingUpstreamResponses(t *testing.T) {
	for _, failure := range []string{"token-503", "jwks-503", "jwks-malformed", "token-timeout", "jwks-timeout"} {
		t.Run(failure, func(t *testing.T) {
			provider, _, _ := newOIDCProviderForFailureTest(t, failure)
			ctx := context.Background()
			cancel := func() {}
			if strings.HasSuffix(failure, "-timeout") {
				ctx, cancel = context.WithTimeout(ctx, 25*time.Millisecond)
			}
			defer cancel()
			_, err := provider.ExchangeAndVerify(ctx, biz.OIDCExchangeRequest{
				Code: testOIDCCode, Nonce: testOIDCNonce, CodeVerifier: testOIDCVerifier, RedirectURI: testOIDCRedirectURI,
			})
			if !errors.Is(err, biz.ErrOIDCDependency) {
				t.Fatalf("ExchangeAndVerify() error = %v, want OIDC dependency classification", err)
			}
			if strings.Contains(err.Error(), "test-only-upstream-detail") {
				t.Fatalf("ExchangeAndVerify() leaked upstream response body: %v", err)
			}
			if strings.HasSuffix(failure, "-timeout") && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ExchangeAndVerify() error = %v, want preserved DeadlineExceeded", err)
			}
		})
	}

	provider, _, _ := newOIDCProviderForFailureTest(t, "pkce")
	_, err := provider.ExchangeAndVerify(context.Background(), biz.OIDCExchangeRequest{
		Code: testOIDCCode, Nonce: testOIDCNonce, CodeVerifier: testOIDCVerifier, RedirectURI: testOIDCRedirectURI,
	})
	if err == nil || errors.Is(err, biz.ErrOIDCDependency) {
		t.Fatalf("ExchangeAndVerify(PKCE rejection) error = %v, want credential rejection only", err)
	}
}

func TestCoreOSOIDCProviderRejectsUnconfiguredRedirectBeforeTokenExchange(t *testing.T) {
	provider, _, tokenRequests := newOIDCProviderForFailureTest(t, "")
	const otherRedirectURI = "https://attacker.test.example/callback"
	if _, err := provider.AuthorizationURL(biz.OIDCAuthorizationRequest{
		State: "opaque-state", Nonce: testOIDCNonce, CodeVerifier: testOIDCVerifier, RedirectURI: otherRedirectURI,
	}); !errors.Is(err, ErrInvalidOIDCProviderConfiguration) {
		t.Fatalf("AuthorizationURL() error = %v, want invalid configuration", err)
	}
	if _, err := provider.ExchangeAndVerify(context.Background(), biz.OIDCExchangeRequest{
		Code: testOIDCCode, Nonce: testOIDCNonce, CodeVerifier: testOIDCVerifier, RedirectURI: otherRedirectURI,
	}); err == nil {
		t.Fatal("ExchangeAndVerify() error = nil for unconfigured redirect")
	}
	if got := tokenRequests.Load(); got != 0 {
		t.Fatalf("token endpoint requests = %d, want 0", got)
	}
}

func newOIDCProviderForFailureTest(t *testing.T, failure string) (biz.OIDCProvider, string, *atomic.Int64) {
	t.Helper()
	trustedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate trusted OIDC test key: %v", err)
	}
	untrustedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate untrusted OIDC test key: %v", err)
	}
	issuer := &oidctest.Server{PublicKeys: []oidctest.PublicKey{{
		PublicKey: &trustedKey.PublicKey, KeyID: "test-key", Algorithm: oidc.RS256,
	}}}
	var tokenRequests atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if failure == "jwks-timeout" && request.URL.Path == "/keys" {
			time.Sleep(100 * time.Millisecond)
			return
		}
		if failure == "jwks-503" && request.URL.Path == "/keys" {
			http.Error(response, "test-only-upstream-detail", http.StatusServiceUnavailable)
			return
		}
		if failure == "jwks-malformed" && request.URL.Path == "/keys" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"keys":[`))
			return
		}
		if request.URL.Path != "/token" {
			issuer.ServeHTTP(response, request)
			return
		}
		tokenRequests.Add(1)
		if failure == "token-timeout" {
			time.Sleep(100 * time.Millisecond)
			return
		}
		if failure == "token-503" {
			http.Error(response, `{"error":"temporarily_unavailable","error_description":"test-only-upstream-detail"}`, http.StatusServiceUnavailable)
			return
		}
		if err := request.ParseForm(); err != nil {
			http.Error(response, "invalid form", http.StatusBadRequest)
			return
		}
		requestClientID, requestClientSecret, basicAuthentication := request.BasicAuth()
		expectedVerifier := testOIDCVerifier
		if failure == "pkce" {
			expectedVerifier = strings.Repeat("z", 43)
		}
		if !basicAuthentication || requestClientID != testOIDCClientID || requestClientSecret != testOIDCClientSecret ||
			request.Form.Get("code_verifier") != expectedVerifier {
			http.Error(response, "invalid authorization-code exchange", http.StatusUnauthorized)
			return
		}
		claims := map[string]any{
			"iss": server.URL, "aud": testOIDCClientID, "sub": "dex-user-1",
			"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Add(-time.Second).Unix(),
			"nonce": testOIDCNonce, "email": "User@Example.COM", "email_verified": true,
		}
		switch failure {
		case "nonce":
			claims["nonce"] = "other-nonce"
		case "issuer":
			claims["iss"] = "https://other-issuer.test.example"
		case "audience":
			claims["aud"] = "other-client"
		case "expiry":
			claims["exp"] = time.Now().Add(-time.Minute).Unix()
		}
		encodedClaims, err := json.Marshal(claims)
		if err != nil {
			http.Error(response, "encode claims", http.StatusInternalServerError)
			return
		}
		signingKey := trustedKey
		if failure == "signature" {
			signingKey = untrustedKey
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"access_token": "opaque-access-token", "token_type": "Bearer", "expires_in": 60,
			"id_token": oidctest.SignIDToken(signingKey, "test-key", oidc.RS256, string(encodedClaims)),
		})
	}))
	t.Cleanup(server.Close)
	issuer.SetIssuer(server.URL)

	provider, err := NewCoreOSOIDCProvider(context.Background(), CoreOSOIDCProviderConfig{
		Name: "dex", IssuerURL: server.URL, ClientID: testOIDCClientID, ClientSecret: testOIDCClientSecret,
		RedirectURIs: []string{testOIDCRedirectURI}, HTTPClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatalf("NewCoreOSOIDCProvider() error = %v", err)
	}
	return provider, server.URL, &tokenRequests
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
