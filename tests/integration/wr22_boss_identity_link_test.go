//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWR22FormalBossIdentityLink(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	human := b.humans[0]
	b.registerIntent(t, b.observedIdentity(t))
	first := b.browser(t)
	location, state := b.beginOIDC(t, first, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("first administrator state")
	}
	status, _, _ := b.callback(t, first, code, state)
	if status != 303 {
		t.Fatal("formal first administrator login")
	}
	// Only the Membership is a SQL prerequisite. Password login and every link
	// below execute through the formal Gateway/IAM processes and real Dex.
	member := mustV7(t)
	if _, err := b.owner.Exec(ctx, `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, member, human); err != nil {
		t.Fatal("own Membership prerequisite")
	}
	login := func(t *testing.T) (*http.Client, string) {
		t.Helper()
		c := b.browser(t)
		s, body, _ := b.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if s != 200 {
			t.Fatalf("formal BOSS password prerequisite status=%d reason=%v", s, body["code"])
		}
		return c, body["access_token"].(string)
	}
	browser, token := login(t)
	cookieURL, _ := url.Parse(b.origin + "/api/v1/auth/identity-links/oidc/callback")
	cookie := func(c *http.Client) string {
		for _, v := range c.Jar.Cookies(cookieURL) {
			if v.Name == "ani_boss_oidc_link_proof" {
				return v.Value
			}
		}
		return ""
	}
	setCookie := func(c *http.Client, name, value string) {
		c.Jar.SetCookies(cookieURL, []*http.Cookie{{Name: name, Value: value, Path: cookieURL.Path, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}})
	}
	begin := func(t *testing.T, c *http.Client, credential, key string) (string, string) {
		t.Helper()
		location, state, response := b.browserEnv.beginLink(t, c, credential, key)
		for _, v := range response.Cookies() {
			if v.Name != "ani_boss_oidc_link_proof" || !v.HttpOnly || !v.Secure || v.Domain != "" || v.SameSite != http.SameSiteLaxMode || v.Path != cookieURL.Path {
				t.Fatal("BOSS link cookie boundary")
			}
		}
		return location, state
	}
	callback := func(t *testing.T, c *http.Client, code, state string, want int) {
		t.Helper()
		s, body, _ := b.browserEnv.linkCallback(t, c, code, state)
		if s != want {
			t.Fatalf("formal BOSS link callback status=%d reason=%v want=%d", s, body["code"], want)
		}
	}
	counts := func(t *testing.T) (int, int) {
		t.Helper()
		var identities, audits int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM identities WHERE principal_id=$1 AND issuer=$2),(SELECT count(*) FROM iam_audit_events WHERE actor_id=$1 AND boundary='platform' AND action='iam.oidc.identity.linked')`, human, b.issuer).Scan(&identities, &audits) != nil {
			t.Fatal("link result query")
		}
		return identities, audits
	}
	t.Run("unknown_OIDC_email_does_not_implicitly_link", func(t *testing.T) {
		c := b.browser(t)
		l, s := b.beginOIDC(t, c, "")
		code, returned := b.dexAuthorizeAccount(t, l, b.accounts[0])
		if s != returned {
			t.Fatal("provider state")
		}
		status, _, _ := b.callback(t, c, code, s)
		if status != 401 {
			t.Fatalf("unlinked OIDC login status=%d", status)
		}
		i, a := counts(t)
		if i != 0 || a != 0 {
			t.Fatal("OIDC login implicitly linked identity")
		}
	})
	t.Run("Begin_replay_and_separate_cookie", func(t *testing.T) {
		key := uuid.NewString()
		l, s := begin(t, browser, token, key)
		proof := cookie(browser)
		if len(proof) < 65 || strings.Contains(proof, token) {
			t.Fatal("opaque proof missing or bearer leaked")
		}
		again, againState := begin(t, browser, token, key)
		if l != again || s != againState || proof != cookie(browser) {
			t.Fatal("Begin replay changed proof")
		}
		status, _, _ := b.request(t, browser, "POST", "/auth/identity-links/oidc/begin", token, map[string]any{"provider": "dex"}, map[string]string{"Origin": "https://wrong.example.test", "Idempotency-Key": uuid.NewString()})
		if status != 403 {
			t.Fatal("wrong Origin accepted")
		}
		code, returned := b.dexAuthorizeAccount(t, l, b.accounts[0])
		if returned != s {
			t.Fatal("provider state")
		}
		onlyConsole := b.browser(t)
		setCookie(onlyConsole, "ani_console_oidc_link_proof", proof)
		callback(t, onlyConsole, code, s, 401)
		ambiguous := b.browser(t)
		setCookie(ambiguous, "ani_console_oidc_link_proof", proof)
		setCookie(ambiguous, "ani_boss_oidc_link_proof", proof)
		callback(t, ambiguous, code, s, 401)
		callback(t, browser, code, strings.Repeat("z", 32), 401)
	})
	t.Run("database_reauthentication_cannot_be_renewed_by_refresh", func(t *testing.T) {
		c, credential := login(t)
		if _, err := b.owner.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '17 minutes',reauthenticated_at=now()-interval '16 minutes' WHERE principal_id=$1 AND audience='boss'`, human); err != nil {
			t.Fatal("own reauthentication expiry")
		}
		status, _, _ := b.request(t, c, "POST", "/auth/identity-links/oidc/begin", credential, map[string]any{"provider": "dex"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 403 {
			t.Fatalf("stale reauthentication status=%d", status)
		}
		status, body, _ := b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("formal refresh prerequisite")
		}
		credential = body["access_token"].(string)
		status, _, _ = b.request(t, c, "POST", "/auth/identity-links/oidc/begin", credential, map[string]any{"provider": "dex"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 403 {
			t.Fatal("refresh renewed reauthentication")
		}
	})
	browser, token = login(t)
	t.Run("current_Membership_is_rechecked_before_callback", func(t *testing.T) {
		l, s := begin(t, browser, token, "")
		code, _ := b.dexAuthorizeAccount(t, l, b.accounts[0])
		if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='suspended',version=version+1 WHERE id=$1`, member); err != nil {
			t.Fatal("own member suspension")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `UPDATE platform_memberships SET status='active',version=version+1 WHERE id=$1`, member); err != nil {
				t.Fatal("own member restoration")
			}
		}()
		callback(t, browser, code, s, 403)
		i, a := counts(t)
		if i != 0 || a != 0 {
			t.Fatal("inactive member linked")
		}
	})
	t.Run("verified_email_owned_by_another_Human_is_rejected", func(t *testing.T) {
		l, s := begin(t, browser, token, "")
		code, _ := b.dexAuthorize(t, l)
		callback(t, browser, code, s, 403)
		i, a := counts(t)
		if i != 0 || a != 0 {
			t.Fatal("foreign email linked")
		}
	})
	t.Run("confirmed_Audit_failure_rolls_back_Identity", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_link_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_link_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_link_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.boundary='platform' AND NEW.action='iam.oidc.identity.linked' THEN PERFORM nextval('wr22_link_fault_hits'); RAISE EXCEPTION 'isolated link Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_link_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_link_fault()`)
		if err != nil {
			t.Fatal("own Audit fault setup")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_link_fault ON iam_audit_events; DROP FUNCTION wr22_link_fault(); DROP SEQUENCE wr22_link_fault_hits`); err != nil {
				t.Fatal("own Audit fault restoration")
			}
		}()
		l, s := begin(t, browser, token, "")
		code, _ := b.dexAuthorizeAccount(t, l, b.accounts[0])
		callback(t, browser, code, s, 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_link_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("Audit fault not reached")
		}
		i, a := counts(t)
		if i != 0 || a != 0 {
			t.Fatal("failed Audit left identity mutation")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("formal_link_then_real_OIDC_login_and_single_use", func(t *testing.T) {
		l, s := begin(t, browser, token, "")
		proof := cookie(browser)
		code, _ := b.dexAuthorizeAccount(t, l, b.accounts[0])
		status, body, response := b.browserEnv.linkCallback(t, browser, code, s)
		if status != 303 || len(body) != 0 || response.Header.Get("Location") != b.origin+"/account" || cookie(browser) != "" {
			t.Fatalf("formal link success status=%d", status)
		}
		i, a := counts(t)
		if i != 1 || a != 1 {
			t.Fatal("Identity and Audit were not both committed")
		}
		setCookie(browser, "ani_boss_oidc_link_proof", proof)
		callback(t, browser, code, s, 401)
		c := b.browser(t)
		l, s = b.beginOIDC(t, c, "")
		code, _ = b.dexAuthorizeAccount(t, l, b.accounts[0])
		status, _, _ = b.callback(t, c, code, s)
		if status != 303 {
			t.Fatalf("linked identity login status=%d", status)
		}
		status, body, _ = b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 || body["principal"].(map[string]any)["principal_id"] != human.String() {
			t.Fatal("linked login principal differs")
		}
		l, s = begin(t, browser, token, "")
		code, _ = b.dexAuthorizeAccount(t, l, b.accounts[0])
		callback(t, browser, code, s, 403)
		i, a = counts(t)
		if i != 1 || a != 1 {
			t.Fatal("duplicate link changed durable identity")
		}
		var reauth time.Time
		if b.owner.QueryRow(ctx, `SELECT reauthenticated_at FROM sessions WHERE principal_id=$1 AND audience='boss' ORDER BY created_at DESC LIMIT 1`, human).Scan(&reauth) != nil || time.Since(reauth) > time.Minute {
			t.Fatal("linked real OIDC reauthentication missing")
		}
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A19", "formal_BOSS_Identity_link": "pass", "provider": "real pinned Dex", "Membership_initial_state": "own SQL prerequisite; Invitation not_verified", "Audit_fault": "positively confirmed P0022 rollback", "implicit_email_link": "rejected"})
	}
}
