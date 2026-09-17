//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net/http"
	"testing"
	"time"
)

// This component test executes the formal IAM/Gateway endpoints and real PG/
// Redis. The action capability is derived by the production codec from the
// durable request using this run's own key; it is NOT Notification/SMTP proof.
func TestWR22PlatformPasswordActionComponents(t *testing.T) {
	b := newWR22BossEnvironment(t)
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	first := b.browser(t)
	location, state := b.beginOIDC(t, first, "")
	code, _ := b.dexAuthorize(t, location)
	s, _, _ := b.callback(t, first, code, state)
	if s != 303 {
		t.Fatal("formal first administrator prerequisite")
	}
	private, err := data.LoadEd25519PrivateKeyFile(b.iam.config.Runtime.AccessToken.PrivateKeyFile)
	if err != nil {
		t.Fatal("own action key load")
	}
	codec, err := data.NewJWXAccessTokenCodec(b.iam.config.Runtime.AccessToken.ActiveKeyId, private, map[string]ed25519.PublicKey{b.iam.config.Runtime.AccessToken.ActiveKeyId: private.Public().(ed25519.PublicKey)}, "ani-iam", data.NewSystemClock())
	if err != nil {
		t.Fatal("own action codec")
	}
	request := func(t *testing.T, account string) (string, string) {
		t.Helper()
		status, body, r := b.request(t, b.browser(t), "POST", "/auth/password-actions", "", map[string]any{"account": account, "audience": "boss"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 202 || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("BOSS password action request status=%d reason=%v", status, body["code"])
		}
		id := body["operation_id"].(string)
		var principal uuid.UUID
		var purpose, audience string
		var issued, expires time.Time
		if b.owner.QueryRow(ctx, `SELECT a.principal_id,a.purpose,a.created_at,a.expires_at,r.audience FROM password_actions a JOIN password_action_requests r ON r.operation_id=a.operation_id WHERE a.operation_id=$1`, id).Scan(&principal, &purpose, &issued, &expires, &audience) != nil || audience != "boss" {
			t.Fatal("durable BOSS action metadata")
		}
		token, err := codec.IssuePasswordAction(ctx, biz.PasswordActionTokenClaims{Issuer: "ani-iam", PrincipalID: principal, OperationID: uuid.MustParse(id), Purpose: biz.PasswordActionPurpose(purpose), IssuedAt: issued, ExpiresAt: expires})
		if err != nil {
			t.Fatal("component action capability derivation")
		}
		return id, token
	}
	complete := func(t *testing.T, token, password, key string, want int) {
		t.Helper()
		status, body, _ := b.request(t, b.browser(t), "POST", "/auth/password-actions/complete", "", map[string]any{"token": token, "new_password": password}, map[string]string{"Idempotency-Key": key})
		if status != want {
			t.Fatalf("password completion status=%d reason=%v want=%d", status, body["code"], want)
		}
	}
	bossLogin := func(t *testing.T, account, password string, want int) (*http.Client, string) {
		t.Helper()
		c := b.browser(t)
		status, body, _ := b.request(t, c, "POST", "/auth/password/login", "", map[string]any{"account": account, "password": password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("BOSS password status=%d reason=%v", status, body["code"])
		}
		token, _ := body["access_token"].(string)
		return c, token
	}
	t.Run("OIDC_only_first_admin_can_set_one_global_password", func(t *testing.T) {
		_, token := request(t, "wr22-boss@example.test")
		password := randomPassword(t)
		complete(t, token, password, uuid.NewString(), 204)
		bossLogin(t, "wr22-boss@example.test", password, 200)
		var count int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships m JOIN verified_emails e ON e.principal_id=m.principal_id WHERE e.normalized_email='wr22-boss@example.test'`).Scan(&count) != nil || count != 0 {
			t.Fatal("password setup created Tenant Membership")
		}
	})
	if t.Failed() {
		return
	}
	human := b.humans[0]
	if _, err := b.owner.Exec(ctx, `INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, mustV7(t), human); err != nil {
		t.Fatal("own target Platform Membership prerequisite")
	}
	bossBrowser, bossToken := bossLogin(t, b.accounts[0], b.password, 200)
	console, consoleToken, _, _ := b.wr22Environment.wr20Environment.login(t, 0)
	foreign, foreignToken, _, _ := b.wr22Environment.wr20Environment.login(t, 1)
	operation, action := request(t, b.accounts[0])
	replacement := randomPassword(t)
	key := uuid.NewString()
	active := func(t *testing.T) int {
		t.Helper()
		var count int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id=$1 AND status='active'`, human).Scan(&count) != nil {
			t.Fatal("own Session count")
		}
		return count
	}
	before := active(t)
	t.Run("confirmed_Audit_failure_rolls_back_both_audiences", func(t *testing.T) {
		_, err := b.owner.Exec(ctx, `CREATE SEQUENCE wr22_reset_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_reset_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_reset_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.password.action.completed' THEN PERFORM nextval('wr22_reset_fault_hits'); RAISE EXCEPTION 'isolated reset Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_reset_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_reset_fault()`)
		if err != nil {
			t.Fatal("own reset Audit fault setup")
		}
		defer func() {
			if _, err := b.owner.Exec(ctx, `DROP TRIGGER wr22_reset_fault ON iam_audit_events; DROP FUNCTION wr22_reset_fault(); DROP SEQUENCE wr22_reset_fault_hits`); err != nil {
				t.Fatal("own reset Audit fault restoration")
			}
		}()
		complete(t, action, replacement, key, 503)
		var hit bool
		if b.owner.QueryRow(ctx, `SELECT is_called FROM wr22_reset_fault_hits`).Scan(&hit) != nil || !hit || active(t) != before {
			t.Fatal("Audit rollback did not preserve target Sessions")
		}
		var status string
		if b.owner.QueryRow(ctx, `SELECT status FROM password_actions WHERE operation_id=$1`, operation).Scan(&status) != nil || status != "active" {
			t.Fatal("failed Audit consumed action")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("reset_serializes_with_BOSS_refresh_and_revokes_all_target_families", func(t *testing.T) {
		refresh := make(chan int, 1)
		go func() { s, _, _ := b.request(t, bossBrowser, "POST", "/auth/refresh", "", nil, nil); refresh <- s }()
		complete(t, action, replacement, key, 204)
		if s := <-refresh; s != 200 && s != 401 {
			t.Fatalf("concurrent BOSS refresh status=%d", s)
		}
		if active(t) != 0 {
			t.Fatal("reset left active Session")
		}
		var grants, families, tokens int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_session_grants WHERE principal_id=$1 AND status='active'),(SELECT count(*) FROM platform_refresh_token_families f JOIN platform_session_grants g ON g.id=f.grant_id WHERE g.principal_id=$1 AND f.status='active'),(SELECT count(*) FROM platform_refresh_tokens t JOIN platform_refresh_token_families f ON f.id=t.family_id JOIN platform_session_grants g ON g.id=f.grant_id WHERE g.principal_id=$1 AND t.status='active')`, human).Scan(&grants, &families, &tokens) != nil || grants+families+tokens != 0 {
			t.Fatal("reset left active Platform authority or replacement token")
		}
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM session_grants g JOIN sessions s ON s.id=g.session_id WHERE s.principal_id=$1 AND g.status='active'),(SELECT count(*) FROM refresh_token_families f JOIN session_grants g ON g.id=f.grant_id AND g.tenant_id=f.tenant_id JOIN sessions s ON s.id=g.session_id WHERE s.principal_id=$1 AND f.status='active'),(SELECT count(*) FROM refresh_tokens t JOIN refresh_token_families f ON f.id=t.family_id AND f.tenant_id=t.tenant_id JOIN session_grants g ON g.id=f.grant_id AND g.tenant_id=f.tenant_id JOIN sessions s ON s.id=g.session_id WHERE s.principal_id=$1 AND t.status='active')`, human).Scan(&grants, &families, &tokens) != nil || grants+families+tokens != 0 {
			t.Fatal("reset left active Tenant authority")
		}
		s, _, _ := b.request(t, bossBrowser, "GET", "/auth/sessions", bossToken, nil, nil)
		if s != 401 {
			t.Fatalf("old BOSS access status=%d", s)
		}
		s, _, _ = b.wr22Environment.wr20Environment.request(t, console, "GET", "/auth/sessions", consoleToken, nil, nil)
		if s != 401 {
			t.Fatalf("old Console access status=%d", s)
		}
		s, _, _ = b.request(t, bossBrowser, "POST", "/auth/refresh", "", nil, nil)
		if s != 401 {
			t.Fatal("reset BOSS replacement refresh accepted")
		}
		s, _, _ = b.wr22Environment.wr20Environment.request(t, console, "POST", "/auth/refresh", "", nil, nil)
		if s != 401 {
			t.Fatal("reset Console refresh accepted")
		}
		s, _, _ = b.wr22Environment.wr20Environment.request(t, foreign, "GET", "/auth/sessions", foreignToken, nil, nil)
		if s != 200 {
			t.Fatal("reset affected unrelated Human")
		}
		complete(t, action, replacement, key, 204)
		complete(t, action, replacement, uuid.NewString(), 401)
		bossLogin(t, b.accounts[0], replacement, 200)
	})
	t.Run("unknown_BOSS_account_remains_uniform_and_creates_no_action", func(t *testing.T) {
		status, body, _ := b.request(t, b.browser(t), "POST", "/auth/password-actions", "", map[string]any{"account": "unknown-" + uuid.NewString() + "@example.test", "audience": "boss"}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 202 {
			t.Fatal("unknown action response differs")
		}
		var count int
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM password_actions WHERE operation_id=$1`, body["operation_id"]).Scan(&count) != nil || count != 0 {
			t.Fatal("unknown BOSS account created action")
		}
	})
	if !t.Failed() {
		recordReference(t, b.run, map[string]any{"stage": "A20-components", "formal_IAM_Gateway_BOSS_request_complete": "pass", "global_reset_and_concurrent_BOSS_refresh": "pass", "action_capability": "production codec derived from own durable action; test prerequisite", "Notification_SMTP": "not_verified"})
	}
}
