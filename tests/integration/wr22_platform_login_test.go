//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// This is the real-PG transaction gate, not evidence of a verified IdP callback.
// Formal BOSS provider acceptance is tested separately after composition wiring.
func TestWR22PlatformOIDCLoginTransactions(t *testing.T) {
	e := newWR22Environment(t)
	ctx := context.Background()
	pool := mustPool(t, e.iam.config.Runtime.Postgresql.Dsn)
	defer pool.Close()
	clock := data.NewSystemClock()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("create isolated signing key failed")
	}
	codec, err := data.NewJWXAccessTokenCodec("wr22-platform-transaction", key, map[string]ed25519.PublicKey{"wr22-platform-transaction": key.Public().(ed25519.PublicKey)}, "ani-iam", clock)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	u, err := biz.NewPlatformLoginUsecase(biz.PlatformLoginConfig{Environment: "wr22-authentication", Provider: e.iam.config.Runtime.Oidc.Provider, OIDCIssuer: e.issuer, AccessIssuer: "ani-iam"}, data.NewPlatformLoginUnitOfWork(data.NewData(pool)), catalog, codec, data.NewSecretGenerator(), data.NewUUIDv7Generator(), clock)
	if err != nil {
		t.Fatal(err)
	}
	identity := biz.OIDCVerifiedIdentity{Issuer: e.issuer, Subject: "wr22-transaction-subject", Email: "wr22-transaction-admin@example.test", EmailVerified: true}
	m := biz.FirstAdministratorManifest{Version: 1, IntentID: mustV7(t), Environment: "wr22-authentication", Email: identity.Email, Issuer: identity.Issuer, Subject: identity.Subject, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	raw, _ := json.Marshal(m)
	digest := sha256.Sum256(raw)
	path := filepath.Join(e.iam.directory, "platform-transaction-intent.json")
	writeReferencePrivate(t, path, raw)
	provision := func() error {
		return exec.Command(e.iam.binary, "provision-first-administrator", "--manifest", path, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", m.Environment, "--dsn-file", filepath.Join(e.iam.directory, "provisioner.secret")).Run()
	}
	if provision() != nil {
		t.Fatal("formal intent provisioner failed")
	}
	login := func(v biz.OIDCVerifiedIdentity) (biz.LoginResult, error) {
		return u.CompleteVerifiedOIDC(ctx, v, "WR22 transaction gate", uuid.NewString())
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if e.owner.QueryRow(ctx, sql, args...).Scan(&n) != nil {
			t.Fatal("query isolated transaction evidence failed")
		}
		return n
	}
	t.Run("exact_verified_identity_required", func(t *testing.T) {
		for _, change := range []func(*biz.OIDCVerifiedIdentity){func(v *biz.OIDCVerifiedIdentity) { v.Issuer = "https://foreign.example.test" }, func(v *biz.OIDCVerifiedIdentity) { v.Subject = "foreign" }, func(v *biz.OIDCVerifiedIdentity) { v.Email = "foreign@example.test" }, func(v *biz.OIDCVerifiedIdentity) { v.EmailVerified = false }} {
			v := identity
			change(&v)
			if _, err := login(v); err == nil {
				t.Fatal("mismatched identity consumed intent")
			}
		}
	})
	t.Run("email_does_not_link_existing_human", func(t *testing.T) {
		foreign := mustV7(t)
		if _, err := e.owner.Exec(ctx, `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now());`, foreign); err != nil {
			t.Fatal("seed isolated conflicting Human failed")
		}
		if _, err := e.owner.Exec(ctx, `INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at) VALUES($1,$2,now(),now(),now())`, foreign, identity.Email); err != nil {
			t.Fatal("seed isolated email conflict failed")
		}
		if _, err := login(identity); err != biz.ErrOIDCEmailConflict {
			t.Fatal("existing email was linked or conflict was misclassified")
		}
		if _, err := e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email='wr22-conflict-retained@example.test' WHERE principal_id=$1`, foreign); err != nil {
			t.Fatal("restore isolated conflict fixture failed")
		}
	})
	t.Run("audit_failure_rolls_back_entire_identity_graph", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `CREATE FUNCTION wr22_reject_platform_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.administrator.bootstrap.completed' THEN RAISE EXCEPTION 'isolated audit failure' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_reject_platform_audit BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_reject_platform_audit()`); err != nil {
			t.Fatal("install isolated audit fault failed")
		}
		if _, err := login(identity); err == nil || !strings.Contains(err.Error(), "SQLSTATE=P0022") {
			t.Fatalf("expected injected audit failure was not reached: %v", err)
		}
		if _, err := e.owner.Exec(ctx, `DROP TRIGGER wr22_reject_platform_audit ON iam_audit_events; DROP FUNCTION wr22_reject_platform_audit()`); err != nil {
			t.Fatal("remove isolated audit fault failed")
		}
		if count(`SELECT count(*) FROM first_administrator_completions`) != 0 || count(`SELECT count(*) FROM platform_memberships`) != 0 || count(`SELECT count(*) FROM platform_roles`) != 0 || count(`SELECT count(*) FROM identities WHERE issuer=$1 AND subject=$2`, identity.Issuer, identity.Subject) != 0 {
			t.Fatal("failed audit left identity or authority graph")
		}
	})
	var results [2]biz.LoginResult
	t.Run("concurrent_verified_flows_create_one_administrator", func(t *testing.T) {
		var wg sync.WaitGroup
		var failures [2]error
		for i := range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); results[i], failures[i] = login(identity) }()
		}
		wg.Wait()
		for i := range 2 {
			if failures[i] != nil {
				t.Fatalf("Platform transaction failed: %v", failures[i])
			}
			claims, err := codec.Verify(ctx, results[i].AccessToken)
			if err != nil || claims.Boundary != biz.AccessBoundaryPlatform || claims.Audience != biz.AudienceBoss || claims.TenantID != uuid.Nil {
				t.Fatal("Platform access token shape differs")
			}
		}
		if results[0].Principal.ID != results[1].Principal.ID || results[0].Session.ID == results[1].Session.ID {
			t.Fatal("independent verified flows did not share one Human with separate Sessions")
		}
		if count(`SELECT count(*) FROM first_administrator_completions`) != 1 || count(`SELECT count(*) FROM platform_memberships`) != 1 || count(`SELECT count(*) FROM platform_role_bindings`) != 1 || count(`SELECT count(*) FROM platform_role_permissions`) != 15 {
			t.Fatal("first administrator graph was duplicated or overprivileged")
		}
		if count(`SELECT count(*) FROM platform_role_permissions WHERE resource IN ('iam.audit-events','iam.recovery-bootstrap','iam.restore-tenant-admin')`) != 0 {
			t.Fatal("ordinary administrator gained dedicated capability implicitly")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("completed_intent_cannot_be_reprovisioned", func(t *testing.T) {
		if provision() == nil {
			t.Fatal("completed bootstrap was re-provisioned")
		}
	})
	t.Run("ordinary_login_rechecks_current_platform_membership", func(t *testing.T) {
		if _, err := e.owner.Exec(ctx, `UPDATE platform_memberships SET status='suspended',version=version+1 WHERE principal_id=$1`, results[0].Principal.ID); err != nil {
			t.Fatal("install isolated membership fault failed")
		}
		if _, err := login(identity); err != biz.ErrMembershipInactive {
			t.Fatal("suspended Platform member logged in")
		}
	})
	recordReference(t, e.run, map[string]any{"check": "platform_login_transaction", "real_postgresql": true, "formal_intent_provisioner": true, "identity_input": "direct verified-identity fixture, not IdP evidence", "atomic_first_administrator_graph": true, "real_boss_oidc_callback": "not_verified"})
}
