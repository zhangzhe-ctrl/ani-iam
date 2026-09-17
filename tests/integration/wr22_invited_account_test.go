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
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"os"
	osexec "os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWR22FormalInvitedAccount(t *testing.T) {
	e := newWR22Environment(t)
	ctx := context.Background()
	admin, adminToken, _, _ := e.login(t, 0)
	browser := e.browser(t)
	call := func(t *testing.T, c *http.Client, token, method, path, key string, payload any, want int) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		status, body, response := e.request(t, c, method, path, token, payload, headers)
		if status != want {
			t.Fatalf("invited-account status=%d reason=%v want=%d", status, body["code"], want)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("invited account response cacheable")
		}
		return body
	}
	request := func(t *testing.T, email, key string) string {
		return call(t, browser, "", "POST", "/auth/invited-account-verifications", key, map[string]string{"account": email}, 202)["challenge_id"].(string)
	}
	complete := func(t *testing.T, email, id, code, password, key string, want int) {
		call(t, browser, "", "POST", "/auth/invited-accounts", key, map[string]string{"account": email, "challenge_id": id, "verification_code": code, "new_password": password}, want)
	}
	invite := func(t *testing.T, email string) string {
		return call(t, admin, adminToken, "POST", "/iam/tenants/"+referenceTenants[0].String()+"/invitations", uuid.NewString(), map[string]any{"email": email, "role_ids": []string{e.adminRoles[0].String()}}, 201)["invitation_id"].(string)
	}
	// Explicit component prerequisite: own-run authenticated ciphertext decoding
	// supplies the independent code. This does NOT claim SMTP delivery.
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
	t.Run("generic_unknown_and_existing_account_have_no_delivery_or_authority", func(t *testing.T) {
		for _, email := range []string{"not-invited-" + uuid.NewString() + "@example.test", e.accounts[0]} {
			id := request(t, email, uuid.NewString())
			var deliveries, cleartext int
			if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM iam_invited_account_outbox WHERE challenge_id=$1),(SELECT count(*) FROM iam_invited_account_verifications WHERE id=$1 AND (normalized_email IS NOT NULL OR code_digest IS NOT NULL))`, id).Scan(&deliveries, &cleartext) != nil || deliveries != 0 || cleartext != 0 {
				t.Fatal("generic request leaked eligibility or sent a code")
			}
			complete(t, email, id, "000000", e.password, uuid.NewString(), 401)
		}
	})
	if t.Failed() {
		return
	}
	email := "wr22-invited-" + uuid.NewString() + "@example.test"
	invitation := invite(t, email)
	key := uuid.NewString()
	id := request(t, email, key)
	code := codeFor(t, id)
	t.Run("request_replay_preserves_challenge_and_does_not_create_Human", func(t *testing.T) {
		if got := request(t, email, key); got != id {
			t.Fatal("request replay changed verification identity")
		}
		var humans int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM verified_emails WHERE normalized_email=$1`, email).Scan(&humans) != nil || humans != 0 {
			t.Fatal("email request created a Human")
		}
	})
	t.Run("independent_code_does_not_accept_Invitation_token", func(t *testing.T) {
		complete(t, email, id, "123", e.password, uuid.NewString(), 400)
		wrong := "000000"
		if code == wrong {
			wrong = "000001"
		}
		complete(t, email, id, wrong, e.password, uuid.NewString(), 401)
	})
	if t.Failed() {
		return
	}
	t.Run("concurrent_verified_signup_once_without_Membership_or_Session", func(t *testing.T) {
		completionKey := uuid.NewString()
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); complete(t, email, id, code, e.password, completionKey, 204) }()
		}
		wg.Wait()
		var human uuid.UUID
		var n, identities, credentials, members, platformMembers, sessions, grants, invites, audits int
		if e.owner.QueryRow(ctx, `SELECT principal_id FROM verified_emails WHERE normalized_email=$1`, email).Scan(&human) != nil {
			t.Fatal("independently verified Human missing")
		}
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM principals WHERE id=$1 AND principal_type='human' AND status='active'),(SELECT count(*) FROM identities WHERE principal_id=$1 AND provider='password' AND status='active'),(SELECT count(*) FROM password_credentials WHERE principal_id=$1),(SELECT count(*) FROM tenant_memberships WHERE principal_id=$1),(SELECT count(*) FROM platform_memberships WHERE principal_id=$1),(SELECT count(*) FROM sessions WHERE principal_id=$1),(SELECT count(*) FROM session_grants g JOIN sessions s ON s.id=g.session_id WHERE s.principal_id=$1),(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$2 AND id=$3 AND status='pending' AND accepted_principal_id IS NULL),(SELECT count(*) FROM iam_audit_events WHERE actor_id=$1 AND authentication_method='email_verification' AND action='iam.account.created')`, human, referenceTenants[0], invitation).Scan(&n, &identities, &credentials, &members, &platformMembers, &sessions, &grants, &invites, &audits) != nil || n != 1 || identities != 1 || credentials != 1 || members != 0 || platformMembers != 0 || sessions != 0 || grants != 0 || invites != 1 || audits != 1 {
			t.Fatal("signup created authority, consumed Invitation or duplicated Human")
		}
		complete(t, email, id, code, e.password, uuid.NewString(), 401)
		complete(t, email, id, code, e.password+"changed", completionKey, 409)
		// The existing login denial is a separate prerequisite, not a signup response.
		status, login, _ := e.request(t, browser, "POST", "/auth/password/login", "", map[string]any{"account": email, "password": e.password, "audience": "console", "boundary": map[string]string{"type": "tenant", "tenant_id": referenceTenants[0].String()}}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 401 || login["access_token"] != nil {
			t.Fatal("unjoined Human obtained an ordinary Session")
		}
		var sessionCount int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id=$1`, human).Scan(&sessionCount) != nil || sessionCount != 0 {
			t.Fatal("denied new-account login created Session")
		}

		var cancelled int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_invited_account_outbox WHERE challenge_id=$1 AND status='cancelled' AND payload_ciphertext IS NULL`, id).Scan(&cancelled) != nil || cancelled != 1 {
			t.Fatal("consumed code delivery retained")
		}
	})
	if t.Failed() {
		return
	}
	dbExec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := e.owner.Exec(ctx, sql, args...); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) {
				t.Fatalf("own signup fixture SQLSTATE=%s", pg.Code)
			}
			t.Fatal("own signup fixture mutation failed")
		}
	}
	freshEmail := func() string { return "wr22-code-" + uuid.NewString() + "@example.test" }
	// Reset only this isolated fixture's signup counters between independent
	// cases. The limiter case below performs its full sequence without a reset.
	clearCounters := func(t *testing.T) {
		t.Helper()
		prefix := e.iam.config.Runtime.Redis.Namespace + ":invited-account:"
		var cursor uint64
		for {
			keys, next, err := e.iamRedis.Scan(ctx, cursor, prefix+"*", 100).Result()
			if err != nil {
				t.Fatal("own signup counters unavailable")
			}
			for _, k := range keys {
				if !strings.HasPrefix(k, prefix) {
					t.Fatal("counter escaped own signup namespace")
				}
			}
			if len(keys) > 0 {
				if e.iamRedis.Del(ctx, keys...).Err() != nil {
					t.Fatal("own signup counter reset failed")
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	assertNoHuman := func(t *testing.T, email string) {
		t.Helper()
		var n int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM verified_emails WHERE normalized_email=$1`, email).Scan(&n) != nil || n != 0 {
			t.Fatal("denied verification created Human")
		}
	}
	fault := func(t *testing.T, action string) func() {
		t.Helper()
		if action != "iam.account.verification.requested" && action != "iam.account.created" {
			t.Fatal("unsupported own Audit fault")
		}
		dbExec(t, fmt.Sprintf(`CREATE SEQUENCE wr22_signup_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_signup_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_signup_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='%s' THEN PERFORM nextval('wr22_signup_fault_hits'); RAISE EXCEPTION 'isolated signup Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_signup_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_signup_fault()`, action))
		return func() {
			var hit bool
			if e.owner.QueryRow(ctx, `SELECT is_called FROM wr22_signup_fault_hits`).Scan(&hit) != nil || !hit {
				t.Error("signup Audit fault was not reached")
			}
			dbExec(t, `DROP TRIGGER wr22_signup_fault ON iam_audit_events; DROP FUNCTION wr22_signup_fault(); DROP SEQUENCE wr22_signup_fault_hits`)
		}
	}
	t.Run("positive_request_Audit_failure_rolls_back_verification_and_outbox", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		restore := fault(t, "iam.account.verification.requested")
		defer restore()
		call(t, browser, "", "POST", "/auth/invited-account-verifications", uuid.NewString(), map[string]string{"account": email}, 503)
		sum := sha256.Sum256([]byte(email))
		var n int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_invited_account_verifications WHERE account_digest=$1`, sum[:]).Scan(&n) != nil || n != 0 {
			t.Fatal("request Audit failure left verification")
		}
		assertNoHuman(t, email)
	})
	t.Run("positive_completion_Audit_failure_preserves_code_and_creates_no_identity", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		id := request(t, email, uuid.NewString())
		code := codeFor(t, id)
		restore := fault(t, "iam.account.created")
		restored := false
		defer func() {
			if !restored {
				restore()
			}
		}()
		complete(t, email, id, code, e.password, uuid.NewString(), 503)
		restore()
		restored = true
		assertNoHuman(t, email)
		var identities, pending, delivery int
		if e.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM identities WHERE provider='password' AND subject=$1),(SELECT count(*) FROM iam_invited_account_verifications WHERE id=$2 AND status='pending' AND principal_id IS NULL),(SELECT count(*) FROM iam_invited_account_outbox WHERE challenge_id=$2 AND status='pending' AND payload_ciphertext IS NOT NULL)`, email, id).Scan(&identities, &pending, &delivery) != nil || identities != 0 || pending != 1 || delivery != 1 {
			t.Fatal("completion Audit rollback leaked identity or consumed code")
		}
		complete(t, email, id, code, e.password, uuid.NewString(), 204)
	})
	t.Run("five_incorrect_codes_exhaust_and_cancel_delivery", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		id := request(t, email, uuid.NewString())
		code := codeFor(t, id)
		wrong := "000000"
		if code == wrong {
			wrong = "000001"
		}
		for i := 0; i < 5; i++ {
			complete(t, email, id, wrong, e.password, uuid.NewString(), 401)
		}
		complete(t, email, id, code, e.password, uuid.NewString(), 401)
		assertNoHuman(t, email)
		var attempts, cancelled int
		var state string
		if e.owner.QueryRow(ctx, `SELECT status,failed_attempts FROM iam_invited_account_verifications WHERE id=$1`, id).Scan(&state, &attempts) != nil || state != "exhausted" || attempts != 5 {
			t.Fatal("code attempts did not persist terminal exhaustion")
		}
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_invited_account_outbox WHERE challenge_id=$1 AND status='cancelled' AND payload_ciphertext IS NULL`, id).Scan(&cancelled) != nil || cancelled != 1 {
			t.Fatal("exhausted code delivery still pending")
		}
	})
	t.Run("same_email_new_challenge_supersedes_old_without_extending_replay", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		key := uuid.NewString()
		first := request(t, email, key)
		oldCode := codeFor(t, first)
		second := request(t, email, uuid.NewString())
		newCode := codeFor(t, second)
		if first == second || request(t, email, key) != first {
			t.Fatal("challenge replay changed identity")
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); complete(t, email, first, oldCode, e.password, uuid.NewString(), 401) }()
		go func() { defer wg.Done(); complete(t, email, second, newCode, e.password, uuid.NewString(), 204) }()
		wg.Wait()
		var oldState, newState string
		if e.owner.QueryRow(ctx, `SELECT status FROM iam_invited_account_verifications WHERE id=$1`, first).Scan(&oldState) != nil || oldState != "superseded" {
			t.Fatal("old challenge not terminal")
		}
		if e.owner.QueryRow(ctx, `SELECT status FROM iam_invited_account_verifications WHERE id=$1`, second).Scan(&newState) != nil || newState != "consumed" {
			t.Fatal("new challenge not consumed")
		}
		var n int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM verified_emails WHERE normalized_email=$1`, email).Scan(&n) != nil || n != 1 {
			t.Fatal("cross-challenge completion duplicated Human")
		}
	})
	t.Run("ten_minute_expiry_and_Invitation_cancellation_fail_closed", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		id := request(t, email, uuid.NewString())
		code := codeFor(t, id)
		// Synthetic elapsed time only; original code/pepper remain unchanged and the
		// guard is restored before the real request. This does not claim wall time.
		tx, err := e.owner.Begin(ctx)
		if err != nil {
			t.Fatal("own expiry transaction")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `ALTER TABLE iam_invited_account_verifications DISABLE TRIGGER invited_account_verification_identity`); err != nil {
			t.Fatal("own verification clock control")
		}
		if _, err = tx.Exec(ctx, `UPDATE iam_invited_account_verifications SET created_at=now()-interval '11 minutes',expires_at=now()-interval '1 minute' WHERE id=$1`, id); err != nil {
			t.Fatal("own verification clock fixture")
		}
		if _, err = tx.Exec(ctx, `ALTER TABLE iam_invited_account_verifications ENABLE TRIGGER invited_account_verification_identity`); err != nil || tx.Commit(ctx) != nil {
			t.Fatal("own verification guard restoration")
		}
		complete(t, email, id, code, e.password, uuid.NewString(), 401)
		assertNoHuman(t, email)
		email = freshEmail()
		inv := invite(t, email)
		id = request(t, email, uuid.NewString())
		code = codeFor(t, id)
		call(t, admin, adminToken, "POST", "/iam/tenants/"+referenceTenants[0].String()+"/invitations/"+inv+"/cancel", uuid.NewString(), map[string]any{"expected_version": 1}, 204)
		complete(t, email, id, code, e.password, uuid.NewString(), 401)
		assertNoHuman(t, email)
	})
	t.Run("private_fields_and_Invitation_are_not_signup_credentials", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		inv := invite(t, email)
		call(t, browser, "", "POST", "/auth/invited-account-verifications", uuid.NewString(), map[string]any{"account": email, "principal_id": e.humans[0].String()}, 400)
		call(t, browser, "", "POST", "/auth/invited-accounts", uuid.NewString(), map[string]any{"account": email, "challenge_id": mustV7(t).String(), "verification_code": "000000", "new_password": e.password, "invitation_token": inv}, 400)
		assertNoHuman(t, email)
	})
	t.Run("real_Redis_limits_and_dependency_recovery", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		for i := 0; i < 3; i++ {
			request(t, email, uuid.NewString())
		}
		status, body, response := e.request(t, browser, "POST", "/auth/invited-account-verifications", "", map[string]string{"account": email}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 429 || response.Header.Get("Retry-After") == "" {
			t.Fatalf("account verification throttle status=%d reason=%v", status, body["code"])
		}
		for i := 0; i < 17; i++ {
			request(t, freshEmail(), uuid.NewString())
		}
		call(t, browser, "", "POST", "/auth/invited-account-verifications", uuid.NewString(), map[string]string{"account": freshEmail()}, 429)
		clearCounters(t)
		email = freshEmail()
		invite(t, email)
		id := request(t, email, uuid.NewString())
		code := codeFor(t, id)
		container := e.redisContainer.GetContainerID()
		if osexec.Command("docker", "pause", container).Run() != nil {
			t.Fatal("own Redis pause")
		}
		paused := true
		defer func() {
			if paused {
				_ = osexec.Command("docker", "unpause", container).Run()
			}
		}()
		status, body, _ = e.request(t, browser, "POST", "/auth/invited-accounts", "", map[string]string{"account": email, "challenge_id": id, "verification_code": code, "new_password": e.password}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if !((status == 503 && body["code"] == "IAM_UNAVAILABLE") || (status == 504 && body["code"] == "IAM_TIMEOUT")) {
			t.Fatalf("signup Redis failure status=%d reason=%v", status, body["code"])
		}
		assertNoHuman(t, email)
		if osexec.Command("docker", "unpause", container).Run() != nil {
			t.Fatal("own Redis restore")
		}
		paused = false
		deadline := time.Now().Add(3 * time.Second)
		for e.iamRedis.Ping(ctx).Err() != nil {
			if time.Now().After(deadline) {
				t.Fatal("own Redis did not recover")
			}
			time.Sleep(50 * time.Millisecond)
		}
		complete(t, email, id, code, e.password, uuid.NewString(), 204)
	})
	t.Run("restricted_runtime_verifier_and_delivery_history", func(t *testing.T) {
		runtime := mustPool(t, e.iam.config.Runtime.Postgresql.Dsn)
		defer runtime.Close()
		for _, test := range []struct{ sql, code string }{
			{`UPDATE iam_invited_account_verifications SET code_digest=decode(repeat('00',32),'hex'),version=version+1 WHERE id=$1`, "23514"},
			{`UPDATE iam_invited_account_outbox SET challenge_id='01900000-0000-7000-8000-000000000001',version=version+1 WHERE challenge_id=$1`, "23514"},
			{`DELETE FROM iam_invited_account_verifications WHERE id=$1`, "42501"},
			{`DELETE FROM iam_invited_account_outbox WHERE challenge_id=$1`, "42501"},
		} {
			tx, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal("own runtime invariant transaction")
			}
			_, err = tx.Exec(ctx, test.sql, id)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != test.code {
				t.Error("runtime signup invariant did not return expected SQLSTATE")
			}
			_ = tx.Rollback(ctx)
		}
	})

	t.Run("exact_Workload_caller_before_request_and_completion_receipts", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		invite(t, email)
		requestKey := uuid.NewString()
		id := request(t, email, requestKey)
		code := codeFor(t, id)
		var requestGrant, completeGrant, caller, binding uuid.UUID
		if e.owner.QueryRow(ctx, `SELECT g.id,g.principal_id,b.id FROM workload_grants g JOIN workload_identity_bindings b ON b.principal_id=g.principal_id WHERE g.operation='/iam.v1.AuthenticationService/RequestInvitedAccountVerification' AND g.status='active' AND b.status='active'`).Scan(&requestGrant, &caller, &binding) != nil {
			t.Fatal("own exact verification caller prerequisite")
		}
		if e.owner.QueryRow(ctx, `SELECT id FROM workload_grants WHERE principal_id=$1 AND operation='/iam.v1.AuthenticationService/CompleteInvitedAccount' AND status='active'`, caller).Scan(&completeGrant) != nil {
			t.Fatal("own exact completion caller prerequisite")
		}
		dbExec(t, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=now() WHERE id=$1`, requestGrant)
		call(t, browser, "", "POST", "/auth/invited-account-verifications", requestKey, map[string]string{"account": email}, 403)
		call(t, admin, adminToken, "GET", "/iam/tenants/"+referenceTenants[0].String()+"/roles", "", nil, 200)
		dbExec(t, `UPDATE workload_grants SET status='active',version=version+1,updated_at=now() WHERE id=$1`, requestGrant)
		completionKey := uuid.NewString()
		dbExec(t, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=now() WHERE id=$1`, completeGrant)
		complete(t, email, id, code, e.password, completionKey, 403)
		assertNoHuman(t, email)
		if request(t, email, requestKey) != id {
			t.Fatal("unrelated request receipt lost")
		}
		dbExec(t, `UPDATE workload_grants SET status='active',version=version+1,updated_at=now() WHERE id=$1`, completeGrant)
		complete(t, email, id, code, e.password, completionKey, 204)
		dbExec(t, `UPDATE workload_identity_bindings SET status='revoked',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller)
		complete(t, email, id, code, e.password, completionKey, 401)
		call(t, browser, "", "POST", "/auth/invited-account-verifications", requestKey, map[string]string{"account": email}, 401)
		dbExec(t, `UPDATE workload_identity_bindings SET status='active',version=version+1,updated_at=now() WHERE id=$1 AND principal_id=$2`, binding, caller)
		complete(t, email, id, code, e.password, completionKey, 204)
	})
	t.Run("completion_IP_throttle_precedes_account_transaction", func(t *testing.T) {
		clearCounters(t)
		email := freshEmail()
		unknown := mustV7(t).String()
		for i := 0; i < 20; i++ {
			complete(t, email, unknown, "000000", e.password, uuid.NewString(), 401)
		}
		status, body, response := e.request(t, browser, "POST", "/auth/invited-accounts", "", map[string]string{"account": email, "challenge_id": unknown, "verification_code": "000000", "new_password": e.password}, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != 429 || response.Header.Get("Retry-After") == "" {
			t.Fatalf("completion IP throttle status=%d reason=%v", status, body["code"])
		}
		var denied int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE target_id=$1 AND action='iam.account.verification.denied'`, unknown).Scan(&denied) != nil || denied != 20 {
			t.Fatal("throttled completion reached the account transaction")
		}
		assertNoHuman(t, email)
	})
}
