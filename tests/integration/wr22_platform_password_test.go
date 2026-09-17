//go:build integration

package integration_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net/http"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestWR22FormalPlatformPassword(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	firstBrowser := b.browser(t)
	location, state := b.beginOIDC(t, firstBrowser, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("OIDC state differs")
	}
	status, _, _ := b.callback(t, firstBrowser, code, state)
	if status != 303 {
		t.Fatal("formal first admin OIDC login failed")
	}
	status, body, _ := b.request(t, firstBrowser, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS refresh prerequisite")
	}
	firstToken, _ := body["access_token"].(string)
	var first, adminRole string
	if b.owner.QueryRow(ctx, `SELECT id FROM platform_memberships`).Scan(&first) != nil || b.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin'`).Scan(&adminRole) != nil {
		t.Fatal("formal first admin graph missing")
	}
	login := func(t *testing.T, c *http.Client, account, password string, want int) map[string]any {
		t.Helper()
		status, body, _ := b.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": account, "password": password, "audience": "boss", "boundary": map[string]string{"type": "platform"}, "device_name": "WR22 BOSS password"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("BOSS password status=%d reason=%v want=%d", status, body["code"], want)
		}
		return body
	}
	mutate := func(t *testing.T, c *http.Client, token, method, path string, payload any, want int) map[string]any {
		t.Helper()
		status, body, _ := b.request(t, c, method, path, token, payload, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("%s %s status=%d reason=%v want=%d", method, path, status, body["code"], want)
		}
		return body
	}
	otherBrowser := b.browser(t)
	human := b.humans[0]
	other := mustV7(t)
	t.Run("global_password_does_not_create_Platform_Membership", func(t *testing.T) {
		login(t, otherBrowser, b.accounts[0], b.password, 403)
		var count, audits, failures int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM platform_memberships WHERE principal_id=$1`, human).Scan(&count) != nil || count != 0 {
			t.Fatal("login created a Membership")
		}
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND boundary='platform' AND reason='MEMBERSHIP_INACTIVE' AND result='denied' AND actor_id IS NULL),(SELECT failed_attempts FROM password_credentials WHERE principal_id=$1)`, human).Scan(&audits, &failures) != nil || audits != 1 || failures != 0 {
			t.Fatal("current identity denial Audit missing or password penalized")
		}
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_password_denial_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_password_denial_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_password_denial_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.boundary='platform' AND NEW.reason='MEMBERSHIP_INACTIVE' THEN PERFORM nextval('wr22_password_denial_fault_hits'); RAISE EXCEPTION 'isolated denial Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_password_denial_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_password_denial_fault()`)
		if err != nil {
			t.Fatal("own denial Audit fault setup")
		}
		login(t, otherBrowser, b.accounts[0], b.password, 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_password_denial_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("denial Audit fault not reached")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_password_denial_fault ON iam_audit_events; DROP FUNCTION wr22_password_denial_fault(); DROP SEQUENCE wr22_password_denial_fault_hits`); err != nil {
			t.Fatal("own denial Audit restoration")
		}
	})
	// A15 isolates login from the still-pending Invitation API. This Membership
	// belongs only to this run's existing verified Console Human/password fixture.
	if _, err := b.owner.Exec(ctx, `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, other, human); err != nil {
		t.Fatal("own Platform Membership prerequisite")
	}
	mutate(t, firstBrowser, firstToken, "POST", "/iam/platform/members/"+other.String()+"/role-bindings", map[string]any{"role_id": adminRole, "expected_membership_version": 1}, 204)
	var otherToken string
	t.Run("real_password_creates_explicit_BOSS_session_and_current_admin_authority", func(t *testing.T) {
		body := login(t, otherBrowser, b.accounts[0], b.password, 200)
		otherToken, _ = body["access_token"].(string)
		principal, _ := body["principal"].(map[string]any)
		grant, _ := body["grant"].(map[string]any)
		boundary, _ := grant["boundary"].(map[string]any)
		if boundary["type"] != "platform" || len(boundary) != 1 || principal["principal_id"] != human.String() || otherToken == "" {
			t.Fatal("password result has another identity boundary")
		}
		var reauth time.Time
		var methods []string
		var sessionID string
		if b.owner.QueryRow(ctx, `SELECT id,authn_methods,reauthenticated_at FROM sessions WHERE principal_id=$1 AND audience='boss' ORDER BY created_at DESC LIMIT 1`, human).Scan(&sessionID, &methods, &reauth) != nil || len(methods) != 1 || methods[0] != "password" || time.Since(reauth) > time.Minute {
			t.Fatal("BOSS password Session/recent authentication missing")
		}
		var grants, tenantGrants int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_session_grants WHERE session_id=$1),(SELECT count(*) FROM session_grants WHERE session_id=$1)`, sessionID).Scan(&grants, &tenantGrants) != nil || grants != 1 || tenantGrants != 0 {
			t.Fatal("password Session Grant crossed boundary")
		}
		mutate(t, otherBrowser, otherToken, "GET", "/iam/platform/members/"+first, nil, 200)
	})
	if t.Failed() {
		return
	}
	t.Run("two_actual_login_capable_admins_cannot_concurrently_suspend_both", func(t *testing.T) {
		type answer struct {
			id     string
			status int
		}
		out := make(chan answer, 2)
		go func() {
			s, _, _ := b.request(t, firstBrowser, "PATCH", "/iam/platform/members/"+first, firstToken, map[string]any{"status": "suspended", "expected_version": 1}, map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- answer{first, s}
		}()
		go func() {
			s, _, _ := b.request(t, otherBrowser, "PATCH", "/iam/platform/members/"+other.String(), otherToken, map[string]any{"status": "suspended", "expected_version": 2}, map[string]string{"Idempotency-Key": uuid.NewString()})
			out <- answer{other.String(), s}
		}()
		a, z := <-out, <-out
		counts := map[int]int{a.status: 1}
		counts[z.status]++
		if counts[200] != 1 || counts[403] != 1 {
			t.Fatal("concurrent current-admin guard did not preserve exactly one")
		}
		suspended := a.id
		if a.status != 200 {
			suspended = z.id
		}
		var version int
		if b.owner.QueryRow(ctx, `SELECT version FROM platform_memberships WHERE id=$1`, suspended).Scan(&version) != nil {
			t.Fatal("own suspended version missing")
		}
		actorBrowser, actorToken := firstBrowser, firstToken
		if suspended == first {
			actorBrowser, actorToken = otherBrowser, otherToken
		}
		mutate(t, actorBrowser, actorToken, "PATCH", "/iam/platform/members/"+suspended, map[string]any{"status": "active", "expected_version": version}, 200)
	})
	t.Run("confirmed_Audit_failure_rolls_back_password_login_and_counter_reset", func(t *testing.T) {
		if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET failed_attempts=2 WHERE principal_id=$1`, human); err != nil {
			t.Fatal("own counter prerequisite")
		}
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_password_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_password_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_password_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.boundary='platform' AND NEW.action='iam.password.login.succeeded' THEN PERFORM nextval('wr22_password_fault_hits'); RAISE EXCEPTION 'isolated password Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_password_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_password_fault()`)
		if err != nil {
			t.Fatal("own password Audit fault setup")
		}
		var before, after, failures int
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id=$1 AND audience='boss'`, human).Scan(&before) != nil {
			t.Fatal("own Session count")
		}
		login(t, b.browser(t), b.accounts[0], b.password, 503)
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE principal_id=$1 AND audience='boss'),(SELECT failed_attempts FROM password_credentials WHERE principal_id=$1),(SELECT is_called FROM wr22_password_fault_hits)`, human).Scan(&after, &failures, &hit) != nil || !hit || after != before || failures != 2 {
			t.Fatal("confirmed Audit failure did not roll back full mutation")
		}
		if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr22_password_fault ON iam_audit_events; DROP FUNCTION wr22_password_fault(); DROP SEQUENCE wr22_password_fault_hits`); err != nil {
			t.Fatal("own Audit fault restoration")
		}
		login(t, b.browser(t), b.accounts[0], b.password, 200)
	})
	t.Run("Credential_rotated_after_Argon_cannot_issue_stale_session", func(t *testing.T) {
		replacement := randomPassword(t)
		hash, err := data.NewArgon2idPasswordHasher().Hash(replacement)
		if err != nil {
			t.Fatal("own replacement hash")
		}
		tx, err := b.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own credential race transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT principal_id FROM password_credentials WHERE principal_id=$1 FOR UPDATE`, human); err != nil {
			t.Fatal("own credential row barrier")
		}
		var transactionID string
		if tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&transactionID) != nil {
			t.Fatal("own row-lock transaction identity")
		}
		done := make(chan int, 1)
		go func() {
			s, _, _ := b.request(t, b.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
			done <- s
		}()
		reached := false
		deadline := time.Now().Add(400 * time.Millisecond)
		for time.Now().Before(deadline) {
			var n int
			if b.owner.QueryRow(ctx, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.usename='ani_iam_runtime' AND NOT l.granted AND l.locktype='transactionid' AND l.transactionid::text=$1`, transactionID).Scan(&n) == nil && n > 0 {
				reached = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !reached {
			_ = tx.Rollback(ctx)
			<-done
			t.Fatal("formal post-Argon lock barrier not reached")
		}
		if _, err = tx.Exec(ctx, `UPDATE password_credentials SET password_hash=$2,version=version+1 WHERE principal_id=$1`, human, hash); err != nil {
			t.Fatal("own credential rotation")
		}
		if tx.Commit(ctx) != nil {
			t.Fatal("own credential rotation commit")
		}
		if status := <-done; status != 401 {
			t.Fatalf("rotated credential login status=%d want=401", status)
		}
		login(t, b.browser(t), b.accounts[0], replacement, 200)
		restored, err := data.NewArgon2idPasswordHasher().Hash(b.password)
		if err != nil {
			t.Fatal("own credential restoration hash")
		}
		if _, err = b.owner.Exec(ctx, `UPDATE password_credentials SET password_hash=$2,version=version+1 WHERE principal_id=$1`, human, restored); err != nil {
			t.Fatal("own credential restoration")
		}
	})
	t.Run("disabled_or_locked_password_is_not_last_admin_capacity", func(t *testing.T) {
		for _, kind := range []string{"identity", "lock"} {
			if kind == "identity" {
				if _, err := b.owner.Exec(ctx, `UPDATE identities SET status='disabled' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
					t.Fatal("own identity fault")
				}
			} else {
				if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=now()+interval '15 minutes' WHERE principal_id=$1`, human); err != nil {
					t.Fatal("own lock fault")
				}
			}
			login(t, b.browser(t), b.accounts[0], b.password, 401)
			var version int
			if b.owner.QueryRow(ctx, `SELECT version FROM platform_memberships WHERE id=$1`, first).Scan(&version) != nil {
				t.Fatal("first version")
			}
			mutate(t, firstBrowser, firstToken, "DELETE", "/iam/platform/members/"+first+"?expected_version="+strconv.Itoa(version), nil, 403)
			if _, err := b.owner.Exec(ctx, `UPDATE identities SET status='active' WHERE principal_id=$1 AND provider='password'`, human); err != nil {
				t.Fatal("own Identity restoration")
			}
			if _, err := b.owner.Exec(ctx, `UPDATE password_credentials SET locked_until=NULL,failed_attempts=0 WHERE principal_id=$1`, human); err != nil {
				t.Fatal("own lock restoration")
			}
		}
	})
	t.Run("Redis_dependency_failure_is_closed_and_recoverable", func(t *testing.T) {
		container := b.redisContainer.GetContainerID()
		if exec.Command("docker", "pause", container).Run() != nil {
			t.Fatal("own Redis pause")
		}
		paused := true
		defer func() {
			if paused {
				_ = exec.Command("docker", "unpause", container).Run()
			}
		}()
		status, body, _ := b.request(t, b.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": b.accounts[0], "password": b.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if !((status == 503 && body["code"] == "IAM_UNAVAILABLE") || (status == 504 && body["code"] == "IAM_TIMEOUT")) {
			t.Fatalf("Redis failure status=%d reason=%v", status, body["code"])
		}
		if exec.Command("docker", "unpause", container).Run() != nil {
			t.Fatal("own Redis restoration")
		}
		paused = false
		// Failure buckets from the two preceding deliberate denials are transient.
		time.Sleep(3 * time.Second)
		login(t, b.browser(t), b.accounts[0], b.password, 200)
	})
	t.Run("unknown_account_is_rejected_without_identity_creation", func(t *testing.T) {
		login(t, b.browser(t), "wr22-unknown-"+uuid.NewString()+"@example.test", b.password, 401)
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A15", "formal_BOSS_password": "pass", "two_actual_admin_session_guard": "pass", "credential_race_barrier": "actual PG row lock after formal Argon verification", "Membership_and_password_creation": "explicit own SQL prerequisites; Invitation/setup not_verified"})
	}
}
