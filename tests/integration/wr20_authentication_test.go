//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWR20StageAFormalPasswordSession(t *testing.T) {
	e := newWR20Environment(t)
	ctx := context.Background()
	sub := func(name string, run func(*testing.T)) {
		if !t.Run(name, run) {
			t.FailNow()
		}
	}
	status := func(t *testing.T, c *http.Client, token string, want int) {
		t.Helper()
		code, body, _ := e.request(t, c, "GET", "/auth/sessions", token, nil, nil)
		if code != want {
			t.Fatalf("session status=%d reason=%s want=%d", code, body["code"], want)
		}
		if want == 401 && body["code"] != "CREDENTIAL_INVALID" {
			t.Fatal("invalid credential reason drift")
		}
		if want == 403 && body["code"] != "PERMISSION_DENIED" {
			t.Fatal("inactive principal or membership reason drift")
		}
	}
	copyCookies := func(c *http.Client) string {
		u, _ := url.Parse(e.origin + "/api/v1/auth")
		parts := []string{}
		for _, cookie := range c.Jar.Cookies(u) {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
		return strings.Join(parts, "; ")
	}
	csrf := func(c *http.Client) string {
		u, _ := url.Parse(e.origin + "/api/v1/auth")
		for _, cookie := range c.Jar.Cookies(u) {
			if cookie.Name == "ani_console_csrf" {
				return cookie.Value
			}
		}
		return ""
	}

	sub("password_login_status_cookie_tls_and_no_legacy_fallback", func(t *testing.T) {
		c, token, body, response := e.login(t, 0)
		status(t, c, token, 200)
		code, list, _ := e.request(t, c, "GET", "/auth/sessions", token, nil, nil)
		if code != 200 || list["items"] == nil {
			t.Fatal("session list REST schema mismatch")
		}
		if _, ok := body["refresh_token"]; ok {
			t.Fatal("refresh leaked to JSON")
		}
		found := false
		for _, cookie := range response.Cookies() {
			if cookie.Name == "ani_console_refresh" {
				found = true
				if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/api/v1/auth" || cookie.MaxAge < 1 {
					t.Fatal("refresh cookie security attributes invalid")
				}
			}
		}
		if !found || len(csrf(c)) < 32 {
			t.Fatal("refresh or independent CSRF cookie absent")
		}
		u, _ := url.Parse(strings.Replace(e.origin, "https:", "http:", 1) + "/api/v1/auth")
		if len(c.Jar.Cookies(u)) != 0 {
			t.Fatal("Secure cookies available on HTTP")
		}
		if response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
			t.Fatal("TLS hostname/CA not verified")
		}
		csrfURL, _ := url.Parse(e.origin + "/api/v1/auth")
		fixedProof := strings.Repeat("a", 43)
		c.Jar.SetCookies(csrfURL, []*http.Cookie{{Name: "ani_console_csrf", Value: fixedProof, Path: "/api/v1/auth", Secure: true}})
		freshStatus, _, _ := e.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": "wr20-0@example.test", "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, nil)
		if freshStatus != 200 || csrf(c) == fixedProof {
			t.Fatal("new login retained a preexisting CSRF proof")
		}
		code, denial, _ := e.request(t, c, "POST", "/auth/token", "", map[string]string{"code": "not-real", "state": "not-real"}, nil)
		if code != 503 || denial["code"] != "AUTHZ_OPERATION_UNREGISTERED" {
			t.Fatalf("disabled legacy code exchange returned %d", code)
		}
	})
	sub("accepted_credential_alternatives_and_login_correlation", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		code, body, _ := e.request(t, e.browser(t), "POST", "/auth/logout", token, nil, nil)
		if code != 401 || body["code"] != "CREDENTIAL_INVALID" {
			t.Fatal("Bearer-only logout was accepted")
		}
		code, body, _ = e.request(t, c, "POST", "/auth/switch-tenant", "", map[string]string{"tenant_id": referenceTenants[0].String()}, nil)
		if code != 401 || body["code"] != "CREDENTIAL_INVALID" {
			t.Fatal("Cookie-only tenant switch was accepted")
		}
		key := randomPassword(t)
		request := map[string]any{"account": e.accounts[0], "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}
		firstStatus, first, _ := e.request(t, c, "POST", "/auth/password/login", "", request, map[string]string{"Idempotency-Key": key})
		secondStatus, second, _ := e.request(t, c, "POST", "/auth/password/login", "", request, map[string]string{"Idempotency-Key": key})
		if firstStatus != 200 || secondStatus != 200 {
			t.Fatal("repeated password login failed")
		}
		if first["access_token"] == second["access_token"] || first["session"].(map[string]any)["session_id"] == second["session"].(map[string]any)["session_id"] {
			t.Fatal("password login unexpectedly replayed a token response")
		}
	})
	sub("wrong_password_unknown_account_and_cross_tenant_are_non_enumerating", func(t *testing.T) {
		var lastCode any
		for _, account := range []string{"wr20-0@example.test", "unknown-wr20@example.test", "wr20-1@example.test"} {
			password := "incorrect"
			if account == "wr20-1@example.test" {
				password = e.password
			}
			code, body, _ := e.request(t, e.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": account, "password": password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, nil)
			if code != 401 || body["code"] != "CREDENTIAL_INVALID" {
				t.Fatalf("negative login status=%d reason=%s", code, body["code"])
			}
			if lastCode != nil && lastCode != body["code"] {
				t.Fatal("login disclosed account existence")
			}
			lastCode = body["code"]
			// Respect the fixed 1s, 2s, 4s IP backoff between distinct negative scenarios.
			time.Sleep(4100 * time.Millisecond)
		}
	})
	sub("origin_csrf_denials_have_no_rotation_side_effect", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		before := copyCookies(c)
		for _, headers := range []map[string]string{{"Origin": "https://evil.wr20.test"}, {"X-CSRF-Token": "forged-proof"}, {"Origin": ""}, {"Origin": "null"}} {
			code, body, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, headers)
			if code != 403 || body["code"] != "PERMISSION_DENIED" {
				t.Fatalf("browser denial status=%d reason=%s", code, body["code"])
			}
		}
		if copyCookies(c) != before {
			t.Fatal("denied browser request changed cookies")
		}
		status(t, c, token, 200)
		code, _, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, map[string]string{"Origin": "", "Referer": e.origin + "/account"})
		if code != 200 {
			t.Fatalf("allowlisted Referer refresh status=%d", code)
		}
	})
	sub("rotation_reuse_revokes_original_family", func(t *testing.T) {
		c, _, _, _ := e.login(t, 0)
		old, proof := copyCookies(c), csrf(c)
		code, body, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 200 {
			t.Fatalf("rotation status=%d reason=%s", code, body["code"])
		}
		fresh, _ := body["access_token"].(string)
		if old == copyCookies(c) {
			t.Fatal("refresh did not rotate")
		}
		raw := &http.Client{Transport: e.transport, Timeout: 6 * time.Second}
		code, body, _ = e.request(t, raw, "POST", "/auth/refresh", "", nil, map[string]string{"Cookie": old, "X-CSRF-Token": proof})
		if code != 401 || body["code"] != "CREDENTIAL_INVALID" {
			t.Fatalf("reuse status=%d reason=%s", code, body["code"])
		}
		status(t, c, fresh, 401)
		code, _, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 401 {
			t.Fatalf("reused family replacement remained usable: %d", code)
		}
	})
	sub("concurrent_refresh_has_one_rotation_and_one_reuse", func(t *testing.T) {
		c, _, _, _ := e.login(t, 0)
		cookies, proof := copyCookies(c), csrf(c)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		tokens := make([]string, 2)
		for i := range codes {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				raw := &http.Client{Transport: e.transport, Timeout: 6 * time.Second}
				code, body, _ := e.request(t, raw, "POST", "/auth/refresh", "", nil, map[string]string{"Cookie": cookies, "X-CSRF-Token": proof})
				codes[i] = code
				tokens[i], _ = body["access_token"].(string)
			}(i)
		}
		wg.Wait()
		if !((codes[0] == 200 && codes[1] == 401) || (codes[1] == 200 && codes[0] == 401)) {
			t.Fatalf("concurrent refresh statuses=%v", codes)
		}
		for _, token := range tokens {
			if token != "" {
				status(t, c, token, 401)
			}
		}
	})
	sub("switch_tenant_independent_grants_and_logout_all_current_session", func(t *testing.T) {
		membership := mustV7(t)
		if _, err := e.owner.Exec(ctx, `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, referenceTenants[1], membership, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		c, original, login, _ := e.login(t, 0)
		sessionID := login["session"].(map[string]any)["session_id"].(string)
		switchKey := randomPassword(t)
		code, body, _ := e.request(t, c, "POST", "/auth/switch-tenant", original, map[string]string{"tenant_id": referenceTenants[1].String()}, map[string]string{"Idempotency-Key": switchKey})
		if code != 200 {
			t.Fatalf("switch status=%d reason=%s", code, body["code"])
		}
		switched, _ := body["access_token"].(string)
		status(t, c, switched, 200)
		status(t, c, original, 200)
		code, repeated, _ := e.request(t, c, "POST", "/auth/switch-tenant", original, map[string]string{"tenant_id": referenceTenants[1].String()}, map[string]string{"Idempotency-Key": switchKey})
		if code != 200 || repeated["access_token"] == switched {
			t.Fatal("tenant switch unexpectedly replayed a token response")
		}
		status(t, c, switched, 401)
		switched, _ = repeated["access_token"].(string)
		status(t, c, switched, 200)
		cookies, proof := copyCookies(c), csrf(c)
		code, body, _ = e.request(t, c, "POST", "/auth/logout", "unusable-access-token", nil, nil)
		if code != 204 {
			t.Fatalf("logout status=%d reason=%s", code, body["code"])
		}
		status(t, c, original, 401)
		status(t, c, switched, 401)
		raw := &http.Client{Transport: e.transport, Timeout: 6 * time.Second}
		code, _, _ = e.request(t, raw, "POST", "/auth/logout", "", nil, map[string]string{"Cookie": cookies, "X-CSRF-Token": proof})
		if code != 204 {
			t.Fatalf("repeated logout status=%d", code)
		}
		var auditCount int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action='iam.session.logged_out'`, sessionID).Scan(&auditCount); err != nil {
			t.Fatal(err)
		}
		if auditCount != 1 {
			t.Fatalf("logout audit count=%d want=1", auditCount)
		}
		code, _, _ = e.request(t, raw, "POST", "/auth/refresh", "", nil, map[string]string{"Cookie": cookies, "X-CSRF-Token": proof})
		if code != 401 {
			t.Fatalf("revoked session refresh status=%d", code)
		}
		other, otherToken, _, _ := e.login(t, 1)
		code, _, _ = e.request(t, other, "POST", "/auth/switch-tenant", otherToken, map[string]string{"tenant_id": referenceTenants[0].String()}, nil)
		if code != 401 && code != 403 {
			t.Fatalf("cross-tenant switch status=%d", code)
		}
		status(t, other, otherToken, 200)
	})
	sub("session_pagination_is_owned_and_grants_are_tenant_scoped", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		seen := map[string]bool{}
		cursor := ""
		var ownCount int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id=$1`, e.humans[0]).Scan(&ownCount); err != nil {
			t.Fatal(err)
		}
		for page := 0; page <= ownCount; page++ {
			code, body, _ := e.request(t, c, "GET", "/auth/sessions?limit=1&cursor="+url.QueryEscape(cursor), token, nil, nil)
			if code != 200 {
				t.Fatalf("session page status=%d reason=%s", code, body["code"])
			}
			items, ok := body["items"].([]any)
			if !ok || len(items) > 1 {
				t.Fatal("invalid bounded session page")
			}
			for _, raw := range items {
				item := raw.(map[string]any)
				id := item["session_id"].(string)
				if seen[id] {
					t.Fatal("session pagination repeated a row")
				}
				seen[id] = true
				var owned bool
				if err := e.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND principal_id=$2)`, id, e.humans[0]).Scan(&owned); err != nil || !owned {
					t.Fatal("session list exposed another Human")
				}
				for _, rawGrant := range item["grants"].([]any) {
					g := rawGrant.(map[string]any)
					if g["boundary"].(map[string]any)["tenant_id"] != referenceTenants[0].String() {
						t.Fatal("session list exposed a different Tenant grant")
					}
				}
			}
			cursor, _ = body["next_cursor"].(string)
			if cursor == "" {
				break
			}
		}
		if len(seen) != ownCount {
			t.Fatalf("session list rows=%d expected=%d", len(seen), ownCount)
		}
		for _, query := range []string{"limit=0", "limit=101", "limit=not-a-number", "cursor=malformed"} {
			code, _, _ := e.request(t, c, "GET", "/auth/sessions?"+query, token, nil, nil)
			if code != 400 {
				t.Fatalf("invalid session page status=%d", code)
			}
		}
	})
	sub("disabled_human_membership_change_and_session_expiry", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		if _, err := e.owner.Exec(ctx, `UPDATE principals SET status='disabled',version=version+1 WHERE id=$1`, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		status(t, c, token, 403)
		code, denied, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 403 || denied["code"] != "PERMISSION_DENIED" {
			t.Fatal("disabled Human refresh did not fail with frozen denial")
		}
		if _, err := e.owner.Exec(ctx, `UPDATE principals SET status='active',version=version+1 WHERE id=$1`, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		c, token, body, _ := e.login(t, 0)
		if _, err := e.owner.Exec(ctx, `UPDATE tenant_memberships SET status='suspended',version=version+1 WHERE tenant_id=$1 AND principal_id=$2`, referenceTenants[0], e.humans[0]); err != nil {
			t.Fatal(err)
		}
		status(t, c, token, 403)
		code, denied, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 403 || denied["code"] != "PERMISSION_DENIED" {
			t.Fatal("suspended membership refresh did not fail with frozen denial")
		}
		if _, err := e.owner.Exec(ctx, `UPDATE tenant_memberships SET status='active',version=version+1 WHERE tenant_id=$1 AND principal_id=$2`, referenceTenants[0], e.humans[0]); err != nil {
			t.Fatal(err)
		}
		c, token, body, _ = e.login(t, 0)
		session := body["session"].(map[string]any)["session_id"].(string)
		if _, err := e.owner.Exec(ctx, `UPDATE sessions SET idle_expires_at=now()-interval '1 second' WHERE id=$1 AND principal_id=$2`, session, e.humans[0]); err != nil {
			t.Fatal(err)
		}
		status(t, c, token, 401)
		code, _, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 401 {
			t.Fatalf("expired session refresh status=%d", code)
		}
	})
	sub("redis_unavailable_fail_closed_and_recovery", func(t *testing.T) {
		c, _, _, _ := e.login(t, 0)
		id := e.redisContainer.GetContainerID()
		if err := exec.Command("docker", "pause", id).Run(); err != nil {
			t.Fatal("pause registered Redis failed")
		}
		paused := true
		defer func() {
			if paused {
				_ = exec.Command("docker", "unpause", id).Run()
			}
		}()
		code, body, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 503 && code != 504 {
			t.Fatalf("dependency failure status=%d reason=%s", code, body["code"])
		}
		if err := exec.Command("docker", "unpause", id).Run(); err != nil {
			t.Fatal("restore registered Redis failed")
		}
		paused = false
		waitFormalHTTP(t, e.iam.readinessURL, 200)
		code, body, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 200 {
			t.Fatalf("dependency recovery status=%d reason=%s", code, body["code"])
		}
	})
	sub("postgres_unavailable_has_no_rotation_and_recovers", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		before := copyCookies(c)
		if err := exec.Command("docker", "pause", e.postgresID).Run(); err != nil {
			t.Fatal("pause registered PostgreSQL failed")
		}
		paused := true
		defer func() {
			if paused {
				_ = exec.Command("docker", "unpause", e.postgresID).Run()
			}
		}()
		code, body, _ := e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 503 && code != 504 {
			t.Fatalf("PostgreSQL failure status=%d reason=%s", code, body["code"])
		}
		if copyCookies(c) != before {
			t.Fatal("database failure changed browser credentials")
		}
		if err := exec.Command("docker", "unpause", e.postgresID).Run(); err != nil {
			t.Fatal("restore registered PostgreSQL failed")
		}
		paused = false
		waitFormalHTTP(t, e.iam.readinessURL, 200)
		status(t, c, token, 200)
		code, body, _ = e.request(t, c, "POST", "/auth/refresh", "", nil, nil)
		if code != 200 {
			t.Fatalf("PostgreSQL recovery status=%d reason=%s", code, body["code"])
		}
	})
	sub("iam_process_unavailable_fails_closed_then_recovers", func(t *testing.T) {
		c, token, _, _ := e.login(t, 0)
		if err := e.iam.process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal("pause registered IAM failed")
		}
		paused := true
		defer func() {
			if paused {
				_ = e.iam.process.Signal(syscall.SIGCONT)
			}
		}()
		code, body, _ := e.request(t, c, "GET", "/auth/sessions", token, nil, nil)
		if code != 503 && code != 504 {
			t.Fatalf("IAM failure status=%d reason=%s", code, body["code"])
		}
		if err := e.iam.process.Signal(syscall.SIGCONT); err != nil {
			t.Fatal("resume registered IAM failed")
		}
		paused = false
		waitFormalHTTP(t, e.iam.readinessURL, 200)
		status(t, c, token, 200)
	})
	if !t.Failed() {
		recordReference(t, e.run, map[string]any{"stage": "A", "result": "pass", "gate": "TestWR20StageAFormalPasswordSession", "scenarios": 11})
		t.Log(fmt.Sprintf("WR20 stage A formal HTTPS/PG/Redis chain passed; credentials and bodies omitted"))
	}
}
