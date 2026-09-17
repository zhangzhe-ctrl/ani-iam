//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestWR22FormalFirstAdministratorRegistration(t *testing.T) {
	e := newWR22Environment(t)
	ctx := context.Background()
	var humansBefore int
	if e.owner.QueryRow(ctx, `SELECT count(*) FROM principals WHERE principal_type='human'`).Scan(&humansBefore) != nil {
		t.Fatal("count prerequisite Humans failed")
	}
	manifest := func() biz.FirstAdministratorManifest {
		return biz.FirstAdministratorManifest{Version: 1, IntentID: mustV7(t), Environment: "wr22-authentication", Email: "first-admin@example.test", Issuer: e.issuer, Subject: "exact-first-admin", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	}
	prepare := func(m biz.FirstAdministratorManifest, dsn string) []string {
		t.Helper()
		raw, _ := json.Marshal(m)
		digest := sha256.Sum256(raw)
		path := filepath.Join(e.iam.directory, "administrator-"+m.IntentID.String()+".json")
		writeReferencePrivate(t, path, raw)
		return []string{"provision-first-administrator", "--manifest", path, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", m.Environment, "--dsn-file", dsn}
	}
	invoke := func(args []string) ([]byte, error) { return exec.Command(e.iam.binary, args...).CombinedOutput() }
	dsnPath := filepath.Join(e.iam.directory, "provisioner.secret")
	reject := func(t *testing.T, m biz.FirstAdministratorManifest, dsn string) {
		t.Helper()
		output, err := invoke(prepare(m, dsn))
		if err == nil {
			t.Fatal("formal provisioner accepted forbidden registration")
		}
		if bytes.Contains(output, []byte(m.Email)) || bytes.Contains(output, []byte(m.Subject)) {
			t.Fatal("provisioner disclosed identity input")
		}
	}
	t.Run("runtime_cannot_register_intent", func(t *testing.T) {
		path := filepath.Join(e.iam.directory, "runtime-for-negative.secret")
		writeReferencePrivate(t, path, []byte(e.iam.config.Runtime.Postgresql.Dsn))
		reject(t, manifest(), path)
	})
	t.Run("expired_new_intent_rejected", func(t *testing.T) { m := manifest(); m.ExpiresAt = time.Now().Add(-time.Minute); reject(t, m, dsnPath) })
	t.Run("audit_failure_rolls_back_intent", func(t *testing.T) {
		m := manifest()
		if _, err := e.owner.Exec(ctx, `CREATE FUNCTION wr22_reject_admin_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.authentication_method='administrator_provisioner' THEN RAISE EXCEPTION 'isolated audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_reject_admin_audit BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_reject_admin_audit()`); err != nil {
			t.Fatal("install isolated audit fault failed")
		}
		reject(t, m, dsnPath)
		if _, err := e.owner.Exec(ctx, `DROP TRIGGER wr22_reject_admin_audit ON iam_audit_events; DROP FUNCTION wr22_reject_admin_audit()`); err != nil {
			t.Fatal("remove isolated audit fault failed")
		}
		var count int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM first_administrator_intents WHERE intent_id=$1`, m.IntentID).Scan(&count) != nil || count != 0 {
			t.Fatal("failed audit left administrator intent")
		}
	})
	m := manifest()
	m.ExpiresAt = time.Now().UTC().Add(4 * time.Second)
	args := prepare(m, dsnPath)
	var receipts [2]biz.FirstAdministratorReceipt
	t.Run("concurrent_same_intent_single_receipt", func(t *testing.T) {
		var wg sync.WaitGroup
		var outputs [2][]byte
		var failures [2]error
		for i := range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); outputs[i], failures[i] = invoke(args) }()
		}
		wg.Wait()
		for i := range 2 {
			if failures[i] != nil || json.Unmarshal(outputs[i], &receipts[i]) != nil {
				_ = os.WriteFile(filepath.Join(e.iam.directory, "first-administrator.private.log"), outputs[i], 0o600)
				t.Fatal("formal intent registration failed")
			}
		}
		if receipts[0].IntentID != receipts[1].IntentID || receipts[0].AuditEventID != receipts[1].AuditEventID ||
			receipts[0].IntentSHA256 != receipts[1].IntentSHA256 || !receipts[0].RegisteredAt.Equal(receipts[1].RegisteredAt) || receipts[0].IntentID != m.IntentID {
			t.Fatalf("concurrent receipt differs: intent_equal=%t audit_equal=%t digest_equal=%t timestamp_equal=%t timestamp_delta_ns=%d", receipts[0].IntentID == receipts[1].IntentID, receipts[0].AuditEventID == receipts[1].AuditEventID, receipts[0].IntentSHA256 == receipts[1].IntentSHA256, receipts[0].RegisteredAt.Equal(receipts[1].RegisteredAt), receipts[0].RegisteredAt.Sub(receipts[1].RegisteredAt).Nanoseconds())
		}
	})
	if t.Failed() {
		return
	}
	t.Run("pending_conflict_and_explicit_successor", func(t *testing.T) {
		reject(t, manifest(), dsnPath)
		early := manifest()
		early.Supersedes = m.IntentID
		reject(t, early, dsnPath)
		if delay := time.Until(m.ExpiresAt.Add(50 * time.Millisecond)); delay > 0 {
			time.Sleep(delay)
		}
		reject(t, manifest(), dsnPath)
		wrong := manifest()
		wrong.Supersedes = mustV7(t)
		reject(t, wrong, dsnPath)
		next := manifest()
		next.Supersedes = m.IntentID
		output, err := invoke(prepare(next, dsnPath))
		var receipt biz.FirstAdministratorReceipt
		if err != nil || json.Unmarshal(output, &receipt) != nil || receipt.IntentID != next.IntentID {
			t.Fatal("explicit expired-intent successor failed")
		}
		var count int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM first_administrator_intents`).Scan(&count) != nil || count != 2 {
			t.Fatal("immutable intent history differs")
		}
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE authentication_method='administrator_provisioner' AND provisioner_role='ani_iam_provisioner' AND actor_id IS NULL`).Scan(&count) != nil || count != 2 {
			t.Fatal("database-authenticated audit attribution differs")
		}
	})
	t.Run("registration_creates_no_human_or_session", func(t *testing.T) {
		var humans, completions int
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM principals WHERE principal_type='human'`).Scan(&humans) != nil || humans != humansBefore {
			t.Fatal("registration created active Human")
		}
		if e.owner.QueryRow(ctx, `SELECT count(*) FROM first_administrator_completions`).Scan(&completions) != nil || completions != 0 {
			t.Fatal("registration pretended OIDC completion")
		}
	})
	recordReference(t, e.run, map[string]any{"check": "first_administrator_registration", "formal_cli": true, "real_postgresql": true, "concurrent_receipt": true, "audit_failure_rollback": true, "expired_explicit_successor": true, "runtime_registration_denied": true, "boss_oidc_completion": "not_verified"})
}
