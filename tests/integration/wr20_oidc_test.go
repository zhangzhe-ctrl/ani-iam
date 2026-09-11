//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func (e *wr20Environment) beginOIDC(t *testing.T, c *http.Client, key string) (string, string) {
	t.Helper()
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	code, b, _ := e.request(t, c, "POST", "/auth/oidc/begin", "", map[string]any{"audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, headers)
	if code != 200 {
		t.Fatalf("begin OIDC status=%d reason=%s", code, b["code"])
	}
	location, _ := b["authorization_url"].(string)
	state, _ := b["state"].(string)
	u, err := url.Parse(location)
	issuer, _ := url.Parse(e.issuer)
	if err != nil || u.Host != issuer.Host || u.Path != "/dex/auth" || u.Query().Get("state") != state || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("code_challenge") == "" || u.Query().Get("nonce") == "" || u.Query().Get("redirect_uri") != e.origin+"/api/v1/auth/oidc/callback" || u.Query().Get("client_id") != dexClientID {
		t.Fatal("OIDC authorization parameters drifted")
	}
	return location, state
}
func (e *wr20Environment) dexAuthorize(t *testing.T, location string) (string, string) {
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
	r, err = c.PostForm(formAction, url.Values{"login": {"wr20-oidc@example.test"}, "password": {e.dexPassword}})
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
func (e *wr20Environment) oidcCallback(t *testing.T, c *http.Client, code, state string) (int, map[string]any, *http.Response) {
	t.Helper()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return e.request(t, c, "GET", "/auth/oidc/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), "", nil, map[string]string{"Origin": ""})
}
func (e *wr20Environment) alterOIDCOperation(t *testing.T, state string, change func(*biz.OIDCOperation)) {
	t.Helper()
	digest := sha256.Sum256([]byte(state))
	key := e.iam.config.Runtime.Redis.Namespace + ":oidc:operation:" + hex.EncodeToString(digest[:])
	ctx := context.Background()
	encoded, err := e.iamRedis.HGet(ctx, key, "data").Bytes()
	if err != nil {
		t.Fatal("registered flow not found")
	}
	var op biz.OIDCOperation
	if json.Unmarshal(encoded, &op) != nil {
		t.Fatal("flow storage invalid")
	}
	change(&op)
	encoded, _ = json.Marshal(op)
	if e.iamRedis.HSet(ctx, key, "data", encoded).Err() != nil {
		t.Fatal("task flow fault setup failed")
	}
}

// Discover the stable subject from a real signed Dex identity. This is only
// pre-existing-identity setup for login acceptance, never link acceptance.
func (e *wr20Environment) seedExistingOIDCIdentity(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	callback := e.origin + "/api/v1/auth/oidc/callback"
	provider, err := data.NewCoreOSOIDCProvider(ctx, data.CoreOSOIDCProviderConfig{Name: e.iam.config.Runtime.Oidc.Provider, IssuerURL: e.issuer, ClientID: dexClientID, ClientSecret: e.dexSecret, RedirectURIs: []string{callback}, HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	if err != nil {
		t.Fatal("real Dex seed discovery failed")
	}
	state, nonce, verifier := randomPassword(t), randomPassword(t), strings.Repeat("x", 43)
	location, err := provider.AuthorizationURL(biz.OIDCAuthorizationRequest{State: state, Nonce: nonce, CodeVerifier: verifier, RedirectURI: callback})
	if err != nil {
		t.Fatal("real Dex seed authorization failed")
	}
	code, returned := e.dexAuthorize(t, location)
	if returned != state {
		t.Fatal("seed state mismatch")
	}
	identity, err := provider.ExchangeAndVerify(ctx, biz.OIDCExchangeRequest{Code: code, Nonce: nonce, CodeVerifier: verifier, RedirectURI: callback})
	if err != nil {
		t.Fatal("real Dex seed exchange failed")
	}
	if identity.Issuer != e.issuer || identity.Email != "wr20-oidc@example.test" || !identity.EmailVerified {
		t.Fatal("real Dex seed identity invalid")
	}
	if _, err = e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email=$2 WHERE principal_id=$1`, e.humans[0], identity.Email); err != nil {
		t.Fatal(err)
	}
	if _, err = e.owner.Exec(ctx, `INSERT INTO identities(id,principal_id,provider,issuer,subject,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'active',1,now(),now())`, mustV7(t), e.humans[0], e.iam.config.Runtime.Oidc.Provider, identity.Issuer, identity.Subject); err != nil {
		t.Fatal(err)
	}
}

// Obtain an unmodified token issued and signed by an actual registered Dex.
// Only a fault relay swaps this foreign token into the IAM exchange response.
func (e *wr20Environment) foreignIDToken(t *testing.T, clientID, nonce string) string {
	t.Helper()
	verifier := strings.Repeat("f", 43)
	challenge := sha256.Sum256([]byte(verifier))
	callback := e.origin + "/api/v1/auth/oidc/callback"
	q := url.Values{"client_id": {clientID}, "redirect_uri": {callback}, "response_type": {"code"}, "scope": {"openid profile email"}, "state": {randomPassword(t)}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	code, _ := e.dexAuthorize(t, e.issuer+"/auth?"+q.Encode())
	r, err := http.NewRequest("POST", e.issuer+"/token", strings.NewReader(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {verifier}}.Encode()))
	if err != nil {
		t.Fatal("foreign Dex request creation failed")
	}
	r.SetBasicAuth(clientID, e.dexSecret)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(r)
	if err != nil {
		t.Fatal("foreign Dex exchange failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal("foreign token read failed")
	}
	var body map[string]any
	if response.StatusCode != 200 || json.Unmarshal(raw, &body) != nil {
		t.Fatal("foreign Dex token rejected")
	}
	token, _ := body["id_token"].(string)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("foreign token malformed")
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal("foreign claims malformed")
	}
	var claims map[string]any
	if json.Unmarshal(claimsRaw, &claims) != nil || claims["iss"] != e.issuer || claims["aud"] != clientID || claims["nonce"] != nonce {
		t.Fatal("foreign token does not have intended bindings")
	}
	return token
}

func TestWR20StageBOIDCLoginFormalDex(t *testing.T) {
	e := newWR20Environment(t)
	ctx := context.Background()
	sub := func(name string, f func(*testing.T)) {
		if !t.Run(name, f) {
			t.FailNow()
		}
	}
	countSessions := func() int {
		var n int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n) != nil {
			t.Fatal("session count unavailable")
		}
		return n
	}
	sub("same_email_does_not_create_or_merge_identity", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email='wr20-oidc@example.test' WHERE principal_id=$1`, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		before := countSessions()
		c := e.browser(t)
		location, state := e.beginOIDC(t, c, "")
		code, returned := e.dexAuthorize(t, location)
		if returned != state {
			t.Fatal("state mismatch")
		}
		status, b, _ := e.oidcCallback(t, c, code, state)
		if status != 401 || b["code"] != "CREDENTIAL_INVALID" || countSessions() != before {
			t.Fatalf("unlinked email login status=%d reason=%s", status, b["code"])
		}
		var n int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM identities WHERE provider=$1`, e.iam.config.Runtime.Oidc.Provider).Scan(&n) != nil || n != 0 {
			t.Fatal("login auto-linked an email")
		}
	})
	e.seedExistingOIDCIdentity(t)
	sub("real_code_exchange_fixed_303_secure_refresh_and_session", func(t *testing.T) {
		before := countSessions()
		c := e.browser(t)
		location, state := e.beginOIDC(t, c, "")
		code, returned := e.dexAuthorize(t, location)
		if returned != state {
			t.Fatal("state mismatch")
		}
		status, b, r := e.oidcCallback(t, c, code, state)
		if status != 303 || r.Header.Get("Location") != e.origin+"/" || len(b) != 0 || countSessions() != before+1 {
			t.Fatalf("OIDC completion status=%d reason=%s", status, b["code"])
		}
		found := false
		for _, cookie := range r.Cookies() {
			if cookie.Name == "ani_console_refresh" {
				found = true
				if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/api/v1/auth" {
					t.Fatal("OIDC refresh cookie security drift")
				}
			}
		}
		if !found {
			t.Fatal("OIDC omitted refresh cookie")
		}
		status, b, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatalf("OIDC session refresh status=%d reason=%s", status, b["code"])
		}
		token, _ := b["access_token"].(string)
		status, b, _ = e.request(t, c, "GET", "/auth/sessions", token, nil, nil)
		if status != 200 {
			t.Fatal("OIDC session not queryable")
		}
		status, _, _ = e.oidcCallback(t, c, code, state)
		if status != 401 || countSessions() != before+1 {
			t.Fatal("OIDC callback replay created a session")
		}
	})
	sub("begin_idempotency_conflict_and_origin_redirect_guards", func(t *testing.T) {
		c := e.browser(t)
		first, state := e.beginOIDC(t, c, "wr20-oidc-stable")
		second, again := e.beginOIDC(t, c, "wr20-oidc-stable")
		if first != second || state != again {
			t.Fatal("OIDC begin replay changed operation")
		}
		body := map[string]any{"audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[1].String()}}
		code, b, _ := e.request(t, c, "POST", "/auth/oidc/begin", "", body, map[string]string{"Idempotency-Key": "wr20-oidc-stable"})
		if code != 409 {
			t.Fatalf("OIDC begin conflict status=%d reason=%s", code, b["code"])
		}
		body["redirect_uri"] = "https://evil.example.test/"
		code, _, _ = e.request(t, c, "POST", "/auth/oidc/begin", "", body, nil)
		if code != 400 {
			t.Fatal("arbitrary redirect accepted")
		}
		delete(body, "redirect_uri")
		code, _, _ = e.request(t, c, "POST", "/auth/oidc/begin", "", body, map[string]string{"Origin": "https://evil.example.test"})
		if code != 403 {
			t.Fatal("untrusted origin accepted")
		}
	})
	sub("missing_state_cookie_and_cross_browser_flow_are_rejected", func(t *testing.T) {
		c := e.browser(t)
		location, state := e.beginOIDC(t, c, "")
		code, _ := e.dexAuthorize(t, location)
		before := countSessions()
		status, _, _ := e.oidcCallback(t, e.browser(t), code, state)
		if status != 401 {
			t.Fatal("callback accepted without browser proof")
		}
		_, other := e.beginOIDC(t, c, "")
		status, _, _ = e.oidcCallback(t, c, code, state)
		if status != 401 || countSessions() != before {
			t.Fatal("callback crossed browser flow")
		}
		if other == state {
			t.Fatal("independent OIDC flows collided")
		}
	})
	sub("code_from_another_flow_is_rejected_by_iam", func(t *testing.T) {
		a, b := e.browser(t), e.browser(t)
		location, _ := e.beginOIDC(t, a, "")
		code, _ := e.dexAuthorize(t, location)
		_, state := e.beginOIDC(t, b, "")
		before := countSessions()
		status, body, _ := e.oidcCallback(t, b, code, state)
		if status != 401 || body["code"] != "CREDENTIAL_INVALID" || countSessions() != before {
			t.Fatal("authorization code crossed IAM flow")
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*biz.OIDCOperation)
	}{
		{"PKCE", func(o *biz.OIDCOperation) { o.CodeVerifier = strings.Repeat("z", 43) }},
		{"nonce", func(o *biz.OIDCOperation) { o.Nonce = "wrong-nonce" }},
		{"expiry", func(o *biz.OIDCOperation) { o.ExpiresAt = time.Now().Add(-time.Second) }},
		{"redirect", func(o *biz.OIDCOperation) { o.RedirectURI = "https://wrong.example.test/" }},
		{"flow_kind", func(o *biz.OIDCOperation) {
			o.Kind = biz.OIDCFlowIdentityLink
			o.PrincipalID = e.humans[0]
			o.SessionID = mustV7(t)
		}},
	} {
		sub("invalid_"+tc.name+"_cannot_create_session", func(t *testing.T) {
			c := e.browser(t)
			location, state := e.beginOIDC(t, c, "")
			code, _ := e.dexAuthorize(t, location)
			e.alterOIDCOperation(t, state, tc.change)
			before := countSessions()
			status, b, _ := e.oidcCallback(t, c, code, state)
			if status != 401 || countSessions() != before {
				t.Fatalf("invalid OIDC %s status=%d reason=%s", tc.name, status, b["code"])
			}
		})
	}
	for _, kind := range []string{"audience", "issuer", "signature"} {
		sub("real_provider_wrong_"+kind+"_token_rejected", func(t *testing.T) {
			c := e.browser(t)
			location, state := e.beginOIDC(t, c, "")
			code, _ := e.dexAuthorize(t, location)
			u, _ := url.Parse(location)
			nonce := u.Query().Get("nonce")
			issuer := e
			clientID := "wr20-foreign"
			if kind == "issuer" {
				foreignSecret, foreignPassword := randomPassword(t), randomPassword(t)
				foreignIssuer, _ := wr20Dex(t, e.run, foreignSecret, foreignPassword, []string{e.origin + "/api/v1/auth/oidc/callback"})
				issuer = &wr20Environment{run: e.run, origin: e.origin, issuer: foreignIssuer, dexSecret: foreignSecret, dexPassword: foreignPassword}
				clientID = dexClientID
			}
			if kind == "signature" {
				clientID = dexClientID
			}
			token := issuer.foreignIDToken(t, clientID, nonce)
			if kind == "signature" {
				parts := strings.Split(token, ".")
				signature, err := base64.RawURLEncoding.DecodeString(parts[2])
				if err != nil || len(signature) == 0 {
					t.Fatal("signature input malformed")
				}
				signature[0] ^= 0x80
				parts[2] = base64.RawURLEncoding.EncodeToString(signature)
				token = strings.Join(parts, ".")
			}
			before := countSessions()
			e.dexFault.replace(token)
			status, b, _ := e.oidcCallback(t, c, code, state)
			if status != 401 || b["code"] != "CREDENTIAL_INVALID" || countSessions() != before {
				t.Fatalf("foreign %s token status=%d reason=%s", kind, status, b["code"])
			}
		})
	}
	sub("provider_failure_consumes_flow_and_fresh_begin_recovers", func(t *testing.T) {
		c := e.browser(t)
		location, state := e.beginOIDC(t, c, "")
		code, _ := e.dexAuthorize(t, location)
		before := countSessions()
		id := e.dexContainer.GetContainerID()
		if exec.Command("docker", "pause", id).Run() != nil {
			t.Fatal("pause registered Dex failed")
		}
		paused := true
		defer func() {
			if paused {
				_ = exec.Command("docker", "unpause", id).Run()
			}
		}()
		status, b, _ := e.oidcCallback(t, c, code, state)
		if (status != 503 && status != 504) || countSessions() != before {
			t.Fatalf("Dex unavailable status=%d reason=%s", status, b["code"])
		}
		if exec.Command("docker", "unpause", id).Run() != nil {
			t.Fatal("restore registered Dex failed")
		}
		paused = false
		u, _ := url.Parse(e.origin + "/api/v1/auth/oidc/callback")
		c.Jar.SetCookies(u, []*http.Cookie{{Name: "ani_console_oidc_state", Value: state, Path: "/api/v1/auth/oidc/callback", Secure: true, HttpOnly: true}})
		status, _, _ = e.oidcCallback(t, c, code, state)
		if status != 401 {
			t.Fatal("failed OIDC flow was replayable")
		}
		location, state = e.beginOIDC(t, c, "")
		code, _ = e.dexAuthorize(t, location)
		status, b, _ = e.oidcCallback(t, c, code, state)
		if status != 303 {
			t.Fatalf("Dex recovery status=%d reason=%s", status, b["code"])
		}
	})
	recordReference(t, e.run, map[string]any{"stage": "B-login", "result": "pass", "explicit_link": "not_verified_pending_decision", "issuer_audience_negative": "pass"})
}

func (e *wr20Environment) beginLink(t *testing.T, c *http.Client, token, key string) (string, string, *http.Response) {
	t.Helper()
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	status, body, response := e.request(t, c, "POST", "/auth/identity-links/oidc/begin", token, map[string]any{"provider": "dex"}, headers)
	if status != 200 {
		t.Fatalf("link Begin status=%d reason=%s", status, body["code"])
	}
	location, _ := body["authorization_url"].(string)
	state, _ := body["state"].(string)
	u, err := url.Parse(location)
	if err != nil || u.Query().Get("redirect_uri") != e.origin+"/api/v1/auth/identity-links/oidc/callback" || u.Query().Get("state") != state || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") == "" {
		t.Fatal("link authorization contract mismatch")
	}
	if _, ok := body["browser_proof"]; ok {
		t.Fatal("proof leaked into public JSON")
	}
	return location, state, response
}
func (e *wr20Environment) linkCallback(t *testing.T, c *http.Client, code, state string) (int, map[string]any, *http.Response) {
	t.Helper()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return e.request(t, c, "GET", "/auth/identity-links/oidc/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), "", nil, map[string]string{"Origin": ""})
}
func linkCookie(t *testing.T, c *http.Client, origin string) string {
	t.Helper()
	u, _ := url.Parse(origin + "/api/v1/auth/identity-links/oidc/callback")
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == "ani_console_oidc_link_proof" {
			return cookie.Value
		}
	}
	return ""
}
func setLinkCookie(c *http.Client, origin, value string) {
	u, _ := url.Parse(origin + "/api/v1/auth/identity-links/oidc/callback")
	c.Jar.SetCookies(u, []*http.Cookie{{Name: "ani_console_oidc_link_proof", Value: value, Path: u.Path, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}})
}

func TestWR20StageBOIDCLinkFormalDex(t *testing.T) {
	e := newWR20Environment(t)
	ctx := context.Background()
	sub := func(name string, run func(*testing.T)) {
		if !t.Run(name, run) {
			t.FailNow()
		}
	}
	// Only the verified email precondition is seeded. Every OIDC Identity below
	// must be created through the formal authenticated Begin and GET callback.
	if _, err := e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email='wr20-oidc@example.test' WHERE principal_id=$1`, e.humans[0]); err != nil {
		t.Fatal(err)
	}
	e.accounts[0] = "wr20-oidc@example.test"
	reject := func(t *testing.T, c *http.Client, code, state string, expected ...int) {
		t.Helper()
		s, b, _ := e.linkCallback(t, c, code, state)
		want, reason := 401, "CREDENTIAL_INVALID"
		if len(expected) > 0 {
			want = expected[0]
		}
		if want == 403 {
			reason = "PERMISSION_DENIED"
		}
		if s != want || b["code"] != reason {
			t.Fatalf("link rejection status=%d reason=%s", s, b["code"])
		}
	}
	sub("Begin_requires_bearer_origin_and_recent_authentication", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		for _, h := range []map[string]string{{"Origin": "null"}, {"Origin": "https://wrong.wr20.test"}} {
			s, _, _ := e.request(t, c, "POST", "/auth/identity-links/oidc/begin", token, map[string]any{"provider": "dex"}, h)
			if s != 403 {
				t.Fatalf("origin status=%d", s)
			}
		}
		s, _, _ := e.request(t, c, "POST", "/auth/identity-links/oidc/begin", "", map[string]any{"provider": "dex"}, nil)
		if s != 401 {
			t.Fatalf("missing bearer status=%d", s)
		}
		if _, err := e.owner.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '20 minutes',reauthenticated_at=now()-interval '16 minutes' WHERE principal_id=$1`, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		s, _, _ = e.request(t, c, "POST", "/auth/identity-links/oidc/begin", token, map[string]any{"provider": "dex"}, nil)
		if s != 403 {
			t.Fatalf("stale reauthentication status=%d", s)
		}
	})
	sub("Begin_replay_preserves_digest_only_proof_and_session_binding", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		key := randomPassword(t)
		location, state, response := e.beginLink(t, c, token, key)
		proof := linkCookie(t, c, e.origin)
		if len(proof) < 65 {
			t.Fatal("missing proof cookie")
		}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "ani_console_oidc_link_proof" && (!cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/api/v1/auth/identity-links/oidc/callback") {
				t.Fatal("link proof cookie attributes")
			}
		}
		again, againState, replay := e.beginLink(t, c, token, key)
		if again != location || againState != state || linkCookie(t, c, e.origin) != proof || len(replay.Cookies()) != 0 {
			t.Fatal("Begin replay altered flow or proof")
		}
		e.alterOIDCOperation(t, state, func(op *biz.OIDCOperation) {
			_, raw, _ := strings.Cut(proof, ".")
			digest := sha256.Sum256([]byte(raw))
			encoded, _ := json.Marshal(op)
			if op.BrowserProofDigest != hex.EncodeToString(digest[:]) || strings.Contains(string(encoded), raw) || strings.Contains(string(encoded), token) || op.LinkGrantID == [16]byte{} {
				t.Fatal("flow proof or authority storage invalid")
			}
		})
		other, otherToken, _, _ := e.login(t, 0)
		s, _, _ := e.request(t, other, "POST", "/auth/identity-links/oidc/begin", otherToken, map[string]any{"provider": "dex"}, map[string]string{"Idempotency-Key": key})
		if s != 409 {
			t.Fatalf("cross Session key status=%d", s)
		}
		lost := e.browser(t)
		e.beginLink(t, lost, token, key)
		if linkCookie(t, lost, e.origin) != "" {
			t.Fatal("digest-only proof was reissued")
		}
		e.beginLink(t, lost, token, randomPassword(t))
		if linkCookie(t, lost, e.origin) == "" {
			t.Fatal("new Begin did not recover lost proof")
		}
	})
	sub("Missing_forged_and_cross_flow_proof", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		loc, state, _ := e.beginLink(t, c, token, "")
		code, _ := e.dexAuthorize(t, loc)
		reject(t, e.browser(t), code, state)
		setLinkCookie(c, e.origin, state+"."+strings.Repeat("x", 43))
		reject(t, c, code, state)
		c, token, _, _ = e.login(t, 0)
		loc, state, _ = e.beginLink(t, c, token, "")
		code, _ = e.dexAuthorize(t, loc)
		other, otherToken, _, _ := e.login(t, 0)
		_, otherState, _ := e.beginLink(t, other, otherToken, "")
		_, otherProof, _ := strings.Cut(linkCookie(t, other, e.origin), ".")
		setLinkCookie(c, e.origin, state+"."+otherProof)
		reject(t, c, code, state)
		if otherState == state {
			t.Fatal("distinct flows shared state")
		}
	})
	for _, fault := range []string{"principal_disabled", "membership_disabled", "session_revoked", "grant_revoked", "grant_version", "idle_expired", "absolute_expired", "reauth_expired"} {
		sub("Callback_rechecks_"+fault, func(t *testing.T) {
			c, token, login, _ := e.login(t, 0)
			loc, state, _ := e.beginLink(t, c, token, "")
			code, _ := e.dexAuthorize(t, loc)
			session := login["session"].(map[string]any)["session_id"]
			query, restore := "", ""
			switch fault {
			case "principal_disabled":
				query = `UPDATE principals SET status='disabled' WHERE id=$1`
				restore = `UPDATE principals SET status='active' WHERE id=$1`
			case "membership_disabled":
				query = `UPDATE tenant_memberships SET status='suspended' WHERE principal_id=$1 AND tenant_id=$2`
				restore = `UPDATE tenant_memberships SET status='active' WHERE principal_id=$1 AND tenant_id=$2`
			case "session_revoked":
				query = `UPDATE sessions SET status='revoked' WHERE id=$1`
			case "grant_revoked":
				query = `UPDATE session_grants SET status='revoked' WHERE session_id=$1 AND tenant_id=$2`
			case "grant_version":
				query = `UPDATE session_grants SET version=version+1 WHERE session_id=$1 AND tenant_id=$2`
			case "idle_expired":
				query = `UPDATE sessions SET idle_expires_at=now()-interval '1 second' WHERE id=$1`
			case "absolute_expired":
				query = `UPDATE sessions SET idle_expires_at=now()-interval '2 seconds',absolute_expires_at=now()-interval '1 second' WHERE id=$1`
			case "reauth_expired":
				query = `UPDATE sessions SET created_at=now()-interval '20 minutes',reauthenticated_at=now()-interval '16 minutes' WHERE id=$1`
			}
			id := session
			if restore != "" {
				id = e.humans[0]
			}
			args := []any{id}
			if strings.Contains(query, "$2") {
				args = append(args, referenceTenants[0])
			}
			if _, err := e.owner.Exec(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
			if restore != "" {
				defer func() {
					if _, err := e.owner.Exec(ctx, restore, args...); err != nil {
						t.Fatal(err)
					}
				}()
			}
			reject(t, c, code, state, 403)
		})
	}
	for _, fault := range []string{"flow_expired", "wrong_principal", "wrong_session", "wrong_grant", "wrong_redirect", "wrong_kind", "wrong_nonce", "wrong_pkce"} {
		sub(fault, func(t *testing.T) {
			c, token, _, _ := e.login(t, 0)
			loc, state, _ := e.beginLink(t, c, token, "")
			code, _ := e.dexAuthorize(t, loc)
			e.alterOIDCOperation(t, state, func(op *biz.OIDCOperation) {
				switch fault {
				case "flow_expired":
					op.ExpiresAt = time.Now().Add(-time.Second)
				case "wrong_principal":
					op.PrincipalID = e.humans[1]
				case "wrong_session":
					op.SessionID = mustV7(t)
				case "wrong_grant":
					op.LinkGrantID = mustV7(t)
				case "wrong_redirect":
					op.RedirectURI = e.origin + "/wrong"
				case "wrong_kind":
					op.Kind = biz.OIDCFlowLogin
				case "wrong_nonce":
					op.Nonce = strings.Repeat("x", 43)
				case "wrong_pkce":
					op.CodeVerifier = strings.Repeat("x", 43)
				}
			})
			want := 401
			if fault == "wrong_principal" || fault == "wrong_session" || fault == "wrong_grant" {
				want = 403
			}
			reject(t, c, code, state, want)
		})
	}
	sub("Wrong_verified_email_owner", func(t *testing.T) {
		c, token, _, _ := e.login(t, 1)
		loc, state, _ := e.beginLink(t, c, token, "")
		code, _ := e.dexAuthorize(t, loc)
		s, body, _ := e.linkCallback(t, c, code, state)
		if s != 403 || body["code"] != "PERMISSION_DENIED" {
			t.Fatalf("email binding conflict status=%d", s)
		}
	})
	sub("Provider_outage_consumes_flow_then_fresh_begin_recovers", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		loc, state, _ := e.beginLink(t, c, token, "")
		code, _ := e.dexAuthorize(t, loc)
		proofCookie := linkCookie(t, c, e.origin)
		id := e.dexContainer.GetContainerID()
		if exec.Command("docker", "pause", id).Run() != nil {
			t.Fatal("pause registered Dex")
		}
		defer exec.Command("docker", "unpause", id).Run()
		s, _, _ := e.linkCallback(t, c, code, state)
		if s != 503 && s != 504 {
			t.Fatalf("Dex outage status=%d", s)
		}
		if exec.Command("docker", "unpause", id).Run() != nil {
			t.Fatal("restore registered Dex")
		}
		setLinkCookie(c, e.origin, proofCookie)
		reject(t, c, code, state)
	})
	sub("Formal_link_creates_identity_and_audit_then_login_succeeds", func(t *testing.T) {
		var before int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM identities WHERE provider='dex'`).Scan(&before); err != nil || before != 0 {
			t.Fatal("negative cases wrote an identity")
		}
		c, token, _, _ := e.login(t, 0)
		loc, state, _ := e.beginLink(t, c, token, "")
		cookie := linkCookie(t, c, e.origin)
		code, _ := e.dexAuthorize(t, loc)
		s, b, r := e.linkCallback(t, c, code, state)
		if s != 303 || r.Header.Get("Location") != e.origin+"/account" || len(b) != 0 || r.Header.Get("Cache-Control") != "no-store" || linkCookie(t, c, e.origin) != "" {
			t.Fatalf("formal link result status=%d", s)
		}
		var identities, audits int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM identities WHERE provider='dex' AND principal_id=$1`, e.humans[0]).Scan(&identities); err != nil {
			t.Fatal(err)
		}
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE action='iam.oidc.identity.linked' AND actor_id=$1 AND tenant_id=$2`, e.humans[0], referenceTenants[0]).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if identities != 1 || audits != 1 {
			t.Fatalf("durable link counts identities=%d audits=%d", identities, audits)
		}
		setLinkCookie(c, e.origin, cookie)
		reject(t, c, code, state)
		browser := e.browser(t)
		loginLoc, _ := e.beginOIDC(t, browser, "")
		loginCode, loginState := e.dexAuthorize(t, loginLoc)
		status, _, _ := e.oidcCallback(t, browser, loginCode, loginState)
		if status != 303 {
			t.Fatalf("linked identity login status=%d", status)
		}
	})
	sub("Existing_identity_binding_conflict", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		loc, state, _ := e.beginLink(t, c, token, "")
		code, _ := e.dexAuthorize(t, loc)
		s, body, _ := e.linkCallback(t, c, code, state)
		if s != 403 || body["code"] != "PERMISSION_DENIED" {
			t.Fatalf("existing binding status=%d", s)
		}
	})
}
