//go:build integration

package integration_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"
)

func TestWR22FormalInvitationPassword(t *testing.T) {
	b := newWR22BossEnvironment(t)
	e := b.wr22Environment
	ctx := context.Background()
	b.registerIntent(t, b.observedIdentity(t))
	boss := b.browser(t)
	location, state := b.beginOIDC(t, boss, "")
	code, returned := b.dexAuthorize(t, location)
	if state != returned {
		t.Fatal("first administrator state prerequisite")
	}
	status, _, _ := b.callback(t, boss, code, state)
	if status != 303 {
		t.Fatal("first administrator prerequisite")
	}
	status, body, _ := b.request(t, boss, "POST", "/auth/refresh", "", nil, nil)
	if status != 200 {
		t.Fatal("BOSS token prerequisite")
	}
	bossToken := body["access_token"].(string)
	admin, adminToken, _, _ := e.login(t, 0)
	browser := e.browser(t)
	tenant := referenceTenants[0]
	throttle, err := data.NewRedisLoginThrottle(e.iamRedis, data.RedisLoginThrottleConfig{Namespace: e.iam.config.Runtime.Redis.Namespace, Limit: int64(e.iam.config.Runtime.Redis.LoginLimit), Window: e.iam.config.Runtime.Redis.LoginWindow.AsDuration(), BaseDelay: time.Second})
	if err != nil {
		t.Fatal("own throttle observation setup")
	}
	waitCooldown := func(t *testing.T, account string) {
		t.Helper()
		attempt := biz.LoginThrottleAttempt{NormalizedAccount: account, SourceIP: netip.MustParseAddr("127.0.0.1")}
		var limited *biz.AuthenticationRateLimitError
		if !errors.As(throttle.Check(ctx, attempt), &limited) || limited.RetryAfter <= 0 || limited.RetryAfter > 10*time.Second {
			t.Fatal("credential denial did not establish bounded real Redis cooldown")
		}
		deadline := time.Now().Add(12 * time.Second)
		for {
			time.Sleep(limited.RetryAfter + 100*time.Millisecond)
			err := throttle.Check(ctx, attempt)
			if err == nil {
				break
			}
			if !time.Now().Before(deadline) || !errors.As(err, &limited) || limited.RetryAfter <= 0 || limited.RetryAfter > 10*time.Second {
				t.Fatal("real account/IP cooldown did not expire")
			}
		}
	}
	var platformRole uuid.UUID
	if e.owner.QueryRow(ctx, `SELECT id FROM platform_roles WHERE code='platform-admin'`).Scan(&platformRole) != nil {
		t.Fatal("Platform role prerequisite")
	}
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := e.owner.Exec(ctx, sql, args...); err != nil {
			t.Fatal("own invitation entry fixture mutation failed")
		}
	}
	call := func(t *testing.T, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		status, body, r := e.request(t, browser, method, path, "", payload, map[string]string{"Idempotency-Key": key})
		if status != want {
			t.Fatalf("first invitation status=%d reason=%v want=%d", status, body["code"], want)
		}
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("first invitation cacheable")
		}
		if want == 401 {
			waitCooldown(t, payload.(map[string]string)["account"])
		}
		return body
	}
	invite := func(t *testing.T, email string, platform bool) string {
		t.Helper()
		var status int
		var body map[string]any
		if platform {
			status, body, _ = b.request(t, boss, "POST", "/iam/platform/invitations", bossToken, map[string]any{"email": email, "role_ids": []string{platformRole.String()}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		} else {
			status, body, _ = e.request(t, admin, "POST", "/iam/tenants/"+tenant.String()+"/invitations", adminToken, map[string]any{"email": email, "role_ids": []string{e.adminRoles[0].String()}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		}
		if status != 201 {
			t.Fatalf("formal invitation prerequisite status=%d reason=%v", status, body["code"])
		}
		return body["invitation_id"].(string)
	}
	// Component prerequisite only: extract this run's authenticated encrypted
	// outbox payload. Real Notification/SMTP acceptance remains a separate gate.
	codeFor := func(t *testing.T, id string) string {
		t.Helper()
		var delivery, version string
		var encrypted []byte
		if e.owner.QueryRow(ctx, `SELECT id,payload_key_version,payload_ciphertext FROM iam_invited_account_outbox WHERE challenge_id=$1`, id).Scan(&delivery, &version, &encrypted) != nil {
			t.Fatal("own verification outbox prerequisite")
		}
		raw, err := os.ReadFile(e.iam.config.Runtime.Notification.OutboxKeyFile)
		if err != nil {
			t.Fatal("own code key unavailable")
		}
		var cfg struct {
			Keys map[string]string `json:"keys"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			t.Fatal("own code key format")
		}
		key, err := base64.StdEncoding.DecodeString(cfg.Keys[version])
		if err != nil {
			t.Fatal("own code key encoding")
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal("own code key size")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
			t.Fatal("own code ciphertext format")
		}
		plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte("ani-iam:invited-account-delivery:v1|"+version+"|"+id+"|"+delivery))
		if err != nil {
			t.Fatal("own independent delivery authentication")
		}
		var payload struct{ Email, Code string }
		if json.Unmarshal(plain, &payload) != nil || len(payload.Code) != 6 {
			t.Fatal("own verification payload shape")
		}
		return payload.Code
	}
	tokenFor := func(t *testing.T, id, email string, platform bool) string {
		t.Helper()
		var delivery, version string
		var generation int64
		var encrypted, digest []byte
		if platform {
			if e.owner.QueryRow(ctx, `SELECT o.id,o.payload_key_version,o.delivery_generation,o.payload_ciphertext,i.token_digest FROM platform_invitation_outbox o JOIN platform_invitations i ON i.id=o.invitation_id AND i.delivery_generation=o.delivery_generation WHERE i.id=$1`, id).Scan(&delivery, &version, &generation, &encrypted, &digest) != nil {
				t.Fatal("own Platform encrypted outbox prerequisite")
			}
		} else {
			if e.owner.QueryRow(ctx, `SELECT o.id,o.payload_key_version,o.delivery_generation,o.payload_ciphertext,i.token_digest FROM tenant_invitation_outbox o JOIN tenant_invitations i ON i.tenant_id=o.tenant_id AND i.id=o.invitation_id AND i.delivery_generation=o.delivery_generation WHERE i.tenant_id=$1 AND i.id=$2`, tenant, id).Scan(&delivery, &version, &generation, &encrypted, &digest) != nil {
				t.Fatal("own Tenant encrypted outbox prerequisite")
			}
		}
		raw, err := os.ReadFile(e.iam.config.Runtime.Notification.OutboxKeyFile)
		if err != nil {
			t.Fatal("own outbox key unavailable")
		}
		var cfg struct {
			Keys map[string]string `json:"keys"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			t.Fatal("own key shape")
		}
		key, err := base64.StdEncoding.DecodeString(cfg.Keys[version])
		if err != nil {
			t.Fatal("own key encoding")
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal("own key length")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
			t.Fatal("own encrypted delivery shape")
		}
		aad := fmt.Sprintf("ani-iam:tenant-invitation-delivery:v1|%s|%s|%s|%s|%d", version, tenant, id, delivery, generation)
		if platform {
			aad = fmt.Sprintf("ani-iam:platform-invitation-delivery:v1|%s|%s|%s|%d", version, id, delivery, generation)
		}
		plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(aad))
		var payload struct{ Email, Token string }
		if err != nil || json.Unmarshal(plain, &payload) != nil || payload.Email != email {
			t.Fatal("own authenticated delivery payload prerequisite")
		}
		sum := sha256.Sum256([]byte(payload.Token))
		if string(sum[:]) != string(digest) {
			t.Fatal("own payload digest differs")
		}
		return payload.Token
	}

	type recipient struct {
		email, id, token, key string
		human                 uuid.UUID
		platform              bool
	}
	fresh := func(t *testing.T, platform bool) recipient {
		t.Helper()
		r := recipient{email: "wr22-first-" + uuid.NewString() + "@example.test", key: uuid.NewString(), platform: platform}
		r.id = invite(t, r.email, platform)
		r.token = tokenFor(t, r.id, r.email, platform)
		challenge := call(t, "POST", "/auth/invited-account-verifications", uuid.NewString(), map[string]string{"account": r.email}, 202)["challenge_id"].(string)
		code := codeFor(t, challenge)
		call(t, "POST", "/auth/invited-accounts", uuid.NewString(), map[string]string{"account": r.email, "challenge_id": challenge, "verification_code": code, "new_password": e.password}, 204)
		if e.owner.QueryRow(ctx, `SELECT principal_id FROM verified_emails WHERE normalized_email=$1`, r.email).Scan(&r.human) != nil {
			t.Fatal("formal independent signup prerequisite")
		}
		return r
	}
	accept := func(t *testing.T, r recipient, password, token, key string, want int) map[string]any {
		return call(t, "POST", "/auth/invitation-acceptances", key, map[string]string{"account": r.email, "password": password, "invitation_token": token}, want)
	}
	noAuthority := func(t *testing.T, r recipient) {
		t.Helper()
		var n int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE principal_id=$1)+(SELECT count(*) FROM platform_memberships WHERE principal_id=$1)+(SELECT count(*) FROM sessions WHERE principal_id=$1)`, r.human).Scan(&n) != nil || n != 0 {
			t.Fatal("unaccepted Human has authority")
		}
	}
	var joined recipient
	t.Run("independent_signup_then_first_Tenant_Membership_converges_without_Session", func(t *testing.T) {
		r := fresh(t, false)
		noAuthority(t, r)
		var wg sync.WaitGroup
		results := make(chan map[string]any, 3)
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- accept(t, r, e.password, r.token, r.key, 200) }()
		}
		wg.Wait()
		close(results)
		member := ""
		for result := range results {
			if member == "" {
				member = result["membership_id"].(string)
			}
			if result["membership_id"] != member || result["boundary"] != "tenant" || result["tenant_id"] != tenant.String() || len(result) != 6 {
				t.Fatal("metadata result changed or contains credentials")
			}
		}
		var members, bindings, sessions, audits int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND principal_id=$2 AND status='active'),(SELECT count(*) FROM tenant_role_bindings WHERE tenant_id=$1 AND membership_id=$3),(SELECT count(*) FROM sessions WHERE principal_id=$2),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND actor_id=$2 AND action='iam.invitation.accepted' AND authentication_method='password' AND caller_principal_id IS NOT NULL)`, tenant, r.human, member).Scan(&members, &bindings, &sessions, &audits) != nil || members != 1 || bindings != 1 || sessions != 0 || audits != 1 {
			t.Fatal("first Tenant acceptance atomicity or no-Session invariant")
		}
		login := call(t, "POST", "/auth/password/login", uuid.NewString(), map[string]any{"account": r.email, "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": tenant.String()}}, 200)
		if login["access_token"] == nil {
			t.Fatal("normal Console login after first membership failed")
		}
		joined = r
	})
	if t.Failed() {
		return
	}
	t.Run("first_Platform_Membership_has_no_Tenant_and_normal_BOSS_login", func(t *testing.T) {
		r := fresh(t, true)
		noAuthority(t, r)
		v := accept(t, r, e.password, r.token, r.key, 200)
		if v["boundary"] != "platform" || v["tenant_id"] != nil || len(v) != 5 {
			t.Fatal("Platform result has Tenant or credential")
		}
		var members, tenants, sessions int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform_memberships WHERE principal_id=$1 AND status='active'),(SELECT count(*) FROM tenant_memberships WHERE principal_id=$1),(SELECT count(*) FROM sessions WHERE principal_id=$1)`, r.human).Scan(&members, &tenants, &sessions) != nil || members != 1 || tenants != 0 || sessions != 0 {
			t.Fatal("Platform acceptance crossed boundary or minted Session")
		}
		status, v, _ := b.request(t, b.browser(t), "POST", "/auth/password/login", "", map[string]any{"account": r.email, "password": e.password, "audience": "boss", "boundary": map[string]string{"type": "platform"}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 200 || v["access_token"] == nil {
			t.Fatalf("ordinary BOSS login status=%d reason=%v", status, v["code"])
		}
	})
	if t.Failed() {
		return
	}
	t.Run("wrong_password_cannot_use_invitation_and_records_attributed_denial", func(t *testing.T) {
		r := joined
		accept(t, r, e.password+"wrong", r.token, r.key, 401)
		var n, counters int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND boundary='principal' AND action='iam.invitation.authentication.denied' AND actor_id IS NULL AND caller_principal_id IS NOT NULL),(SELECT failed_attempts FROM password_credentials WHERE principal_id=$1)`, r.human).Scan(&n, &counters) != nil || n != 1 || counters != 1 {
			t.Fatal("password denial omitted counter or caller Audit")
		}
		accept(t, r, e.password, r.token, r.key, 200)
	})
	t.Run("correct_password_does_not_replace_independent_token_or_recipient", func(t *testing.T) {
		r := joined
		wrong := r.token[:len(r.token)-1] + "x"
		if wrong == r.token {
			wrong = r.token[:len(r.token)-1] + "y"
		}
		accept(t, r, e.password, wrong, r.key, 403)
		r.email = e.accounts[1]
		accept(t, r, e.password, r.token, r.key, 403)
		accept(t, joined, e.password, joined.token, uuid.NewString(), 409)
	})
	t.Run("current_identity_and_Principal_required_before_receipt_replay", func(t *testing.T) {
		r := joined
		exec(t, `UPDATE identities SET status='disabled',version=version+1 WHERE principal_id=$1 AND provider='password'`, r.human)
		accept(t, r, e.password, r.token, r.key, 401)
		exec(t, `UPDATE identities SET status='active',version=version+1 WHERE principal_id=$1 AND provider='password'`, r.human)
		exec(t, `UPDATE principals SET status='disabled',version=version+1 WHERE id=$1`, r.human)
		accept(t, r, e.password, r.token, r.key, 401)
		exec(t, `UPDATE principals SET status='active',version=version+1 WHERE id=$1`, r.human)
		accept(t, r, e.password, r.token, r.key, 200)
	})
	if t.Failed() {
		return
	}
	t.Run("password_rotation_after_Argon_before_transaction_rejects_receipt", func(t *testing.T) {
		r := joined
		var original string
		if e.owner.QueryRow(ctx, `SELECT password_hash FROM password_credentials WHERE principal_id=$1`, r.human).Scan(&original) != nil {
			t.Fatal("own credential hash prerequisite")
		}
		hash, err := data.NewArgon2idPasswordHasher().Hash(e.password + "replacement")
		if err != nil {
			t.Fatal("own replacement hash")
		}
		tx, err := e.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own password race transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT principal_id FROM password_credentials WHERE principal_id=$1 FOR UPDATE`, r.human); err != nil {
			t.Fatal("own password race lock")
		}
		var xid string
		if tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid) != nil {
			t.Fatal("own transaction id")
		}
		done := make(chan int, 1)
		go func() {
			status, _, _ := e.request(t, browser, "POST", "/auth/invitation-acceptances", "", map[string]string{"account": r.email, "password": e.password, "invitation_token": r.token}, map[string]string{"Idempotency-Key": r.key})
			done <- status
		}()
		reached := false
		deadline := time.Now().Add(400 * time.Millisecond)
		for time.Now().Before(deadline) {
			var n int
			if e.owner.QueryRow(ctx, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.usename='ani_iam_runtime' AND NOT l.granted AND l.locktype='transactionid' AND l.transactionid::text=$1`, xid).Scan(&n) == nil && n > 0 {
				reached = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !reached {
			_ = tx.Rollback(ctx)
			<-done
			t.Fatal("post-Argon invitation barrier not reached")
		}
		if _, err = tx.Exec(ctx, `UPDATE password_credentials SET password_hash=$2,version=version+1 WHERE principal_id=$1`, r.human, hash); err != nil {
			t.Fatal("own password rotation")
		}
		if tx.Commit(ctx) != nil {
			t.Fatal("own rotation commit")
		}
		if status := <-done; status != 401 {
			t.Fatalf("stale password acceptance status=%d", status)
		}
		waitCooldown(t, r.email)
		exec(t, `UPDATE password_credentials SET password_hash=$2,version=version+1 WHERE principal_id=$1`, r.human, original)
		accept(t, r, e.password, r.token, r.key, 200)
	})
	t.Run("Audit_failure_rolls_back_first_Membership_and_invitation", func(t *testing.T) {
		r := fresh(t, false)
		noAuthority(t, r)
		exec(t, `CREATE SEQUENCE wr22_entry_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_entry_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_entry_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.invitation.accepted' THEN PERFORM nextval('wr22_entry_fault_hits'); RAISE EXCEPTION 'isolated first invitation Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_entry_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_entry_fault()`)
		restore := func() {
			exec(t, `DROP TRIGGER wr22_entry_fault ON iam_audit_events; DROP FUNCTION wr22_entry_fault(); DROP SEQUENCE wr22_entry_fault_hits`)
		}
		accept(t, r, e.password, r.token, r.key, 503)
		noAuthority(t, r)
		var hit bool
		var pending int
		if e.owner.QueryRow(ctx, `SELECT is_called FROM wr22_entry_fault_hits`).Scan(&hit) != nil || !hit {
			restore()
			t.Fatal("entry Audit fault not reached")
		}
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND id=$2 AND status='pending'`, tenant, r.id).Scan(&pending) != nil || pending != 1 {
			restore()
			t.Fatal("failed Audit consumed first invitation")
		}
		restore()
		accept(t, r, e.password, r.token, r.key, 200)
	})
	if !t.Failed() {
		recordReference(t, e.run, map[string]any{"stage": "I1", "purpose_only_first_Tenant_and_Platform_acceptance": "pass", "independent_signup": "formal Gateway/IAM/PG/Redis", "no_Session_or_tokens": "pass", "current_password_race_and_replay": "pass", "Notification_SMTP": "not_verified; own encrypted payload fixture"})
	}
}
