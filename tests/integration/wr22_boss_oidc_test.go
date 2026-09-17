//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestWR22FormalBossOIDCAndSessions(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if b.owner.QueryRow(ctx, query, args...).Scan(&n) != nil {
			t.Fatal("BOSS evidence query failed")
		}
		return n
	}
	humansBefore := count(`SELECT count(*) FROM principals WHERE principal_type='human'`)
	callback := func(t *testing.T, c *http.Client) (int, map[string]any, *http.Response) {
		t.Helper()
		location, state := b.beginOIDC(t, c, "")
		code, returned := b.dexAuthorize(t, location)
		if state != returned {
			t.Fatal("real BOSS state differs")
		}
		return b.callback(t, c, code, state)
	}
	sub := func(name string, fn func(*testing.T)) {
		if !t.Run(name, fn) {
			t.FailNow()
		}
	}
	sub("unknown_Human_cannot_login_or_signup_without_intent", func(t *testing.T) {
		status, body, _ := callback(t, b.browser(t))
		if status != 401 || body["code"] != "CREDENTIAL_INVALID" || count(`SELECT count(*) FROM principals WHERE principal_type='human'`) != humansBefore {
			t.Fatalf("unknown BOSS status=%d reason=%v", status, body["code"])
		}
	})
	identity := b.observedIdentity(t)
	args := b.registerIntent(t, identity)
	sub("formal_registration_creates_intent_only", func(t *testing.T) {
		if count(`SELECT count(*) FROM platform_memberships`) != 0 || count(`SELECT count(*) FROM principals WHERE principal_type='human'`) != humansBefore || count(`SELECT count(*) FROM first_administrator_intents`) != 1 {
			t.Fatal("intent registration created authority")
		}
	})
	sub("real_nonce_mismatch_cannot_consume_intent", func(t *testing.T) {
		c := b.browser(t)
		location, state := b.beginOIDC(t, c, "")
		code, returned := b.dexAuthorize(t, location)
		if returned != state {
			t.Fatal("real state mismatch")
		}
		digest := sha256.Sum256([]byte(state))
		key := b.iam.config.Runtime.Redis.Namespace + ":boss:oidc:operation:" + hex.EncodeToString(digest[:])
		raw, err := b.iamRedis.HGet(ctx, key, "data").Bytes()
		if err != nil {
			t.Fatal("own BOSS flow missing")
		}
		var op biz.OIDCOperation
		if json.Unmarshal(raw, &op) != nil {
			t.Fatal("own BOSS flow malformed")
		}
		op.Nonce = randomPassword(t)
		raw, _ = json.Marshal(op)
		if b.iamRedis.HSet(ctx, key, "data", raw).Err() != nil {
			t.Fatal("own nonce fault failed")
		}
		status, body, _ := b.callback(t, c, code, state)
		if status != 401 || count(`SELECT count(*) FROM first_administrator_completions`) != 0 {
			t.Fatalf("nonce fault status=%d reason=%v", status, body["code"])
		}
	})
	sub("confirmed_audit_failure_rolls_back_entire_first_admin_graph", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_boss_audit_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_boss_audit_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_boss_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.administrator.bootstrap.completed' THEN PERFORM nextval('wr22_boss_audit_fault_hits'); RAISE EXCEPTION 'isolated BOSS audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_boss_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_boss_audit_fault()`)
		if err != nil {
			t.Fatal("own audit fault setup failed")
		}
		status, body, _ := callback(t, b.browser(t))
		var called bool
		var hits int64
		if b.owner.QueryRow(ctx, `SELECT last_value,is_called FROM wr22_boss_audit_fault_hits`).Scan(&hits, &called) != nil || !called || hits != 1 {
			t.Fatal("specific audit fault was not reached")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_boss_audit_fault ON iam_audit_events; DROP FUNCTION wr22_boss_audit_fault(); DROP SEQUENCE wr22_boss_audit_fault_hits`); err != nil {
			t.Fatal("own audit fault restoration failed")
		}
		if status != 503 || count(`SELECT count(*) FROM first_administrator_completions`) != 0 || count(`SELECT count(*) FROM platform_memberships`) != 0 || count(`SELECT count(*) FROM principals WHERE principal_type='human'`) != humansBefore || count(`SELECT count(*) FROM platform_roles`) != 0 || count(`SELECT count(*) FROM platform_session_grants`) != 0 {
			t.Fatalf("audit rollback status=%d reason=%v", status, body["code"])
		}
	})
	c := b.browser(t)
	var access string
	var claims biz.AccessTokenClaims
	var reauthenticated time.Time
	var originalCookies []*http.Cookie
	private, err := data.LoadEd25519PrivateKeyFile(b.iam.config.Runtime.AccessToken.PrivateKeyFile)
	if err != nil {
		t.Fatal("own verifier key load failed")
	}
	codec, err := data.NewJWXAccessTokenCodec(b.iam.config.Runtime.AccessToken.ActiveKeyId, private, map[string]ed25519.PublicKey{b.iam.config.Runtime.AccessToken.ActiveKeyId: private.Public().(ed25519.PublicKey)}, "ani-iam", data.NewSystemClock())
	if err != nil {
		t.Fatal("own token verifier unavailable")
	}
	authURL, _ := url.Parse(b.origin + "/api/v1/auth")
	sub("real_BOSS_callback_creates_one_admin_and_secure_cookies", func(t *testing.T) {
		location, state := b.beginOIDC(t, c, "")
		code, returned := b.dexAuthorize(t, location)
		if returned != state {
			t.Fatal("BOSS state mismatch")
		}
		status, body, response := b.callback(t, c, code, state)
		if status != 303 || response.Header.Get("Location") != b.origin+"/" || len(body) != 0 {
			t.Fatalf("BOSS callback status=%d reason=%v", status, body["code"])
		}
		found := false
		for _, cookie := range response.Cookies() {
			if cookie.Name == "ani_boss_refresh" {
				found = true
				if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/api/v1/auth" {
					t.Fatal("BOSS credential cookie security differs")
				}
			}
			if cookie.Name == "ani_console_refresh" {
				t.Fatal("BOSS emitted Console credential")
			}
		}
		if !found || count(`SELECT count(*) FROM first_administrator_completions`) != 1 || count(`SELECT count(*) FROM platform_memberships WHERE status='active'`) != 1 || count(`SELECT count(*) FROM platform_role_bindings`) != 1 || count(`SELECT count(*) FROM tenant_memberships WHERE principal_id IN (SELECT principal_id FROM platform_memberships)`) != 0 || count(`SELECT count(*) FROM platform_role_permissions WHERE resource LIKE '%recovery%' OR resource='iam.audit-events'`) != 0 {
			t.Fatal("first administrator graph or least authority differs")
		}
		originalCookies = c.Jar.Cookies(authURL)
		status, _, _ = b.callback(t, c, code, state)
		if status != 401 || count(`SELECT count(*) FROM sessions WHERE audience='boss'`) != 1 {
			t.Fatal("BOSS callback replay created Session")
		}
		if exec.Command(b.iam.binary, args...).Run() == nil {
			t.Fatal("completed owner intent was accepted again")
		}
	})
	sub("refresh_verifies_platform_boundary_and_preserves_recent_auth_time", func(t *testing.T) {
		if b.owner.QueryRow(ctx, `SELECT reauthenticated_at FROM sessions WHERE audience='boss'`).Scan(&reauthenticated) != nil {
			t.Fatal("BOSS reauthentication evidence missing")
		}
		status, body, _ := b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatalf("BOSS refresh status=%d reason=%v", status, body["code"])
		}
		access, _ = body["access_token"].(string)
		claims, err = codec.Verify(ctx, access)
		if err != nil || claims.Boundary != biz.AccessBoundaryPlatform || claims.Audience != biz.AudienceBoss || claims.TenantID != uuid.Nil || claims.ExpiresAt.Sub(claims.IssuedAt) > 10*time.Minute {
			t.Fatal("signed BOSS claims differ")
		}
		var after, idle, absolute time.Time
		var version int64
		if b.owner.QueryRow(ctx, `SELECT reauthenticated_at,idle_expires_at,absolute_expires_at,version FROM sessions WHERE id=$1`, claims.SessionID).Scan(&after, &idle, &absolute, &version) != nil || !after.Equal(reauthenticated) || version != 2 || idle.Sub(time.Now()) > 30*time.Minute || absolute.Sub(reauthenticated) > 8*time.Hour {
			t.Fatal("BOSS refresh extended reauthentication or lifetime")
		}
	})
	sub("csrf_origin_and_Console_cookie_cannot_refresh_BOSS", func(t *testing.T) {
		for _, headers := range []map[string]string{{"X-CSRF-Token": "wrong"}, {"Origin": b.wr22Environment.origin}} {
			status, _, _ := b.request(t, c, "POST", "/auth/refresh", "", nil, headers)
			if status != 403 {
				t.Fatalf("BOSS browser proof status=%d", status)
			}
		}
		foreign := b.browser(t)
		cookies := c.Jar.Cookies(authURL)
		for _, cookie := range cookies {
			if cookie.Name == "ani_boss_refresh" {
				cookie.Name = "ani_console_refresh"
			}
			if cookie.Name == "ani_boss_csrf" {
				cookie.Name = "ani_console_csrf"
			}
		}
		foreign.Jar.SetCookies(authURL, cookies)
		status, _, _ := b.request(t, foreign, "POST", "/auth/refresh", "", nil, nil)
		if status != 403 {
			t.Fatalf("cross-boundary cookie status=%d", status)
		}
	})
	sub("reuse_revokes_family_once_and_invalidates_access_grant_version", func(t *testing.T) {
		stale := b.browser(t)
		stale.Jar.SetCookies(authURL, originalCookies)
		status, body, _ := b.request(t, stale, "POST", "/auth/refresh", "", nil, nil)
		if status != 401 {
			t.Fatalf("BOSS reuse status=%d reason=%v", status, body["code"])
		}
		var version int64
		if b.owner.QueryRow(ctx, `SELECT version FROM platform_session_grants WHERE id=$1`, claims.GrantID).Scan(&version) != nil || version != claims.GrantVersion+1 || count(`SELECT count(*) FROM platform_refresh_token_families WHERE grant_id=$1 AND status='revoked'`, claims.GrantID) != 1 || count(`SELECT count(*) FROM iam_audit_events WHERE action='iam.refresh_token.reused' AND target_id=$1`, claims.GrantID) != 1 {
			t.Fatal("BOSS reuse did not revoke exact Family and Grant version")
		}
		status, _, _ = b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 401 {
			t.Fatal("replacement survived reuse revocation")
		}
	})
	sub("existing_identity_login_and_idempotent_logout", func(t *testing.T) {
		c := b.browser(t)
		status, body, _ := callback(t, c)
		if status != 303 {
			t.Fatalf("normal BOSS login status=%d reason=%v", status, body["code"])
		}
		status, body, _ = b.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if status != 200 {
			t.Fatal("normal BOSS refresh failed")
		}
		token, _ := body["access_token"].(string)
		v, err := codec.Verify(ctx, token)
		if err != nil {
			t.Fatal("normal BOSS token invalid")
		}
		cookies := c.Jar.Cookies(authURL)
		key := uuid.NewString()
		status, body, _ = b.request(t, c, "POST", "/auth/logout", "", nil, map[string]string{"Idempotency-Key": key})
		if status != 204 {
			t.Fatalf("BOSS logout status=%d reason=%v", status, body["code"])
		}
		c.Jar.SetCookies(authURL, cookies)
		status, _, _ = b.request(t, c, "POST", "/auth/logout", "", nil, map[string]string{"Idempotency-Key": key})
		if status != 204 {
			t.Fatal("BOSS logout replay failed")
		}
		if count(`SELECT count(*) FROM sessions WHERE id=$1 AND status='revoked'`, v.SessionID) != 1 || count(`SELECT count(*) FROM platform_session_grants WHERE session_id=$1 AND status='active'`, v.SessionID) != 0 || count(`SELECT count(*) FROM iam_audit_events WHERE action='iam.session.logged_out' AND target_id=$1`, v.SessionID) != 1 {
			t.Fatal("BOSS logout graph or audit differs")
		}
	})
	recordReference(t, b.run, map[string]any{"stage": "A8", "formal_boss_oidc_first_administrator_refresh_logout": "pass", "caller": "formal ANI Gateway Workload", "provider": "real pinned Dex ani-boss", "platform_validation_and_administration": "not_verified"})
}
