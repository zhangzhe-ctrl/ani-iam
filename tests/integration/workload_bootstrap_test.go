//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

type bootstrapTestClock struct{ now time.Time }

func (c bootstrapTestClock) Now() time.Time { return c.now }

func newBootstrapManifest(t *testing.T, ca string) (biz.WorkloadBootstrapOwner, biz.WorkloadBootstrapManifest) {
	t.Helper()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	name := "gateway-" + uuid.NewString()[:8]
	o := biz.WorkloadBootstrapOwner{Environment: "wr19-" + uuid.NewString()[:8], TrustDomain: "wr19.test", CASHA256: ca}
	return o, biz.WorkloadBootstrapManifest{Version: 1, ManifestID: id(), Environment: o.Environment, TrustDomain: o.TrustDomain, CASHA256: ca, ExpiresAt: time.Now().UTC().Add(time.Hour), Workloads: []biz.BootstrapWorkload{{PrincipalID: id(), Name: name, BindingID: id(), DNSIdentity: name + ".wr19.test", Grants: []biz.BootstrapWorkloadGrant{{ID: id(), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/PasswordLogin"}, {ID: id(), Audience: "ani-session-gateway", Operation: "session.create"}}}}}
}

func TestWorkloadBootstrapTransactions(t *testing.T) {
	env := newPostgresEnvironment(t)
	ctx := context.Background()
	provisioner := mustPool(t, postgresDSN(provisionerRole, env.provisionerPass, env.host, primaryDB, "wr19-bootstrap"))
	defer provisioner.Close()
	ownerPool := mustPool(t, env.migrationDSN(primaryDB))
	defer ownerPool.Close()
	newUC := func(clock biz.Clock) *biz.WorkloadBootstrap {
		return biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(provisioner)), clock)
	}
	u := newUC(data.NewSystemClock())
	checkCounts := func(m biz.WorkloadBootstrapManifest, expected int) {
		t.Helper()
		var p, r, a int
		if err := ownerPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM principals WHERE id=$1), (SELECT count(*) FROM workload_bootstrap_receipts WHERE manifest_id=$2), (SELECT count(*) FROM iam_audit_events WHERE bootstrap_manifest_id=$2)`, m.Workloads[0].PrincipalID, m.ManifestID).Scan(&p, &r, &a); err != nil {
			t.Fatal(err)
		}
		if p != expected || r != expected || a != expected {
			t.Fatalf("atomic counts principal/receipt/audit=%d/%d/%d want=%d", p, r, a, expected)
		}
	}
	t.Run("concurrent same intent once and lost response restart", func(t *testing.T) {
		o, m := newBootstrapManifest(t, strings.Repeat("a", 64))
		var wg sync.WaitGroup
		receipts := make([]biz.WorkloadBootstrapReceipt, 8)
		errs := make([]error, 8)
		for i := range receipts {
			wg.Add(1)
			go func(i int) { defer wg.Done(); receipts[i], errs[i] = u.Provision(ctx, o, m) }(i)
		}
		wg.Wait()
		for i := range receipts {
			if errs[i] != nil {
				t.Fatalf("concurrent result: %v", errs[i])
			}
			if !reflect.DeepEqual(receipts[0], receipts[i]) {
				t.Fatal("duplicate intent returned different receipt")
			}
		}
		checkCounts(m, 1)
		// A fresh object/connection after a committed but discarded response
		// returns the receipt after expiry and never changes current identity.
		if _, err := ownerPool.Exec(ctx, `UPDATE principals SET status='disabled',version=version+1 WHERE id=$1`, m.Workloads[0].PrincipalID); err != nil {
			t.Fatal(err)
		}
		got, err := newUC(bootstrapTestClock{m.ExpiresAt.Add(time.Hour)}).Provision(ctx, o, m)
		if err != nil || !reflect.DeepEqual(got, receipts[0]) {
			t.Fatalf("restart retry: %v", err)
		}
		var state string
		if err := ownerPool.QueryRow(ctx, `SELECT status FROM principals WHERE id=$1`, m.Workloads[0].PrincipalID).Scan(&state); err != nil || state != "disabled" {
			t.Fatal("retry re-enabled identity")
		}
		changed := m
		changed.ExpiresAt = changed.ExpiresAt.Add(time.Minute)
		if _, err = u.Provision(ctx, o, changed); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
			t.Fatalf("changed intent: %v", err)
		}
		other := m
		other.ManifestID = uuid.Must(uuid.NewV7())
		if _, err = u.Provision(ctx, o, other); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
			t.Fatalf("existing identity adoption: %v", err)
		}
		var count int
		if err := ownerPool.QueryRow(ctx, `SELECT count(*) FROM workload_bootstrap_receipts WHERE manifest_id=$1`, other.ManifestID).Scan(&count); err != nil || count != 0 {
			t.Fatal("conflicting intent was consumed")
		}
		checkCounts(m, 1)
	})
	t.Run("same environment rejects a new bootstrap including concurrent different intents", func(t *testing.T) {
		o, a := newBootstrapManifest(t, strings.Repeat("a", 64))
		_, b := newBootstrapManifest(t, strings.Repeat("a", 64))
		b.Environment = o.Environment
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, m := range []biz.WorkloadBootstrapManifest{a, b} {
			wg.Add(1)
			go func(i int, m biz.WorkloadBootstrapManifest) { defer wg.Done(); _, errs[i] = u.Provision(ctx, o, m) }(i, m)
		}
		wg.Wait()
		successes, conflicts := 0, 0
		for _, err := range errs {
			if err == nil {
				successes++
			} else if errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
				conflicts++
			} else {
				t.Fatalf("concurrent environment result: %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatal("environment consumed more than one initialization intent")
		}
		for i, m := range []biz.WorkloadBootstrapManifest{a, b} {
			expected := 0
			if errs[i] == nil {
				expected = 1
			}
			checkCounts(m, expected)
		}
		_, next := newBootstrapManifest(t, strings.Repeat("a", 64))
		next.Environment = o.Environment
		if _, err := u.Provision(ctx, o, next); !errors.Is(err, biz.ErrWorkloadBootstrapConflict) {
			t.Fatalf("different initialization after consumption: %v", err)
		}
		checkCounts(next, 0)
	})
	t.Run("new expired intent has no state", func(t *testing.T) {
		o, m := newBootstrapManifest(t, strings.Repeat("a", 64))
		m.ExpiresAt = time.Now().Add(-time.Minute)
		if _, err := u.Provision(ctx, o, m); !errors.Is(err, biz.ErrWorkloadBootstrapExpired) {
			t.Fatalf("expiry: %v", err)
		}
		checkCounts(m, 0)
	})
	t.Run("audit failure rolls back identity grants and receipt", func(t *testing.T) {
		o, m := newBootstrapManifest(t, strings.Repeat("a", 64))
		if _, err := ownerPool.Exec(ctx, `CREATE FUNCTION wr19_fail_bootstrap_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'WR19 audit fault'; END $$; CREATE TRIGGER wr19_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr19_fail_bootstrap_audit()`); err != nil {
			t.Fatal(err)
		}
		_, err := u.Provision(ctx, o, m)
		if !errors.Is(err, biz.ErrPersistenceUnavailable) {
			t.Errorf("audit fault: %v", err)
		}
		checkCounts(m, 0)
		if _, err := ownerPool.Exec(ctx, `DROP TRIGGER wr19_audit_fault ON iam_audit_events; DROP FUNCTION wr19_fail_bootstrap_audit()`); err != nil {
			t.Fatal(err)
		}
		if _, err := u.Provision(ctx, o, m); err != nil {
			t.Fatal(err)
		}
		checkCounts(m, 1)
	})
	t.Run("runtime cannot provision and provisioner cannot create Human or mutate", func(t *testing.T) {
		o, m := newBootstrapManifest(t, strings.Repeat("a", 64))
		runtime := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(env.runtimePool)), data.NewSystemClock())
		if _, err := runtime.Provision(ctx, o, m); !errors.Is(err, biz.ErrWorkloadBootstrapDenied) {
			t.Fatalf("runtime: %v", err)
		}
		_, err := provisioner.Exec(ctx, `INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, uuid.Must(uuid.NewV7()))
		assertPGCode(t, err, "42501")
		for _, sql := range []string{`UPDATE workload_grants SET status='revoked'`, `DELETE FROM workload_bootstrap_receipts`, `TRUNCATE principals`, `SELECT * FROM password_credentials`, `SET ROLE ani_iam_migrator`} {
			if _, err := provisioner.Exec(ctx, sql); err == nil {
				t.Fatal("provisioner exceeded bootstrap authority")
			} else {
				assertPGCode(t, err, "42501")
			}
		}
		checkCounts(m, 0)
	})
	t.Run("runtime foundation retains role guards", func(t *testing.T) {
		if err := data.ValidateRuntimeFoundation(ctx, data.NewData(env.runtimePool)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFormalWorkloadBootstrapCommand(t *testing.T) {
	env := newPostgresEnvironment(t)
	run, _ := isolatedRun(t)
	dir := filepath.Join(run, "private", "bootstrap-cli-"+uuid.NewString())
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ca, _, caFile := writeProcessE2ECertificateAuthority(t, dir)
	caDigest := sha256.Sum256(ca.Raw)
	_, m := newBootstrapManifest(t, hex.EncodeToString(caDigest[:]))
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	manifestPath := filepath.Join(dir, "manifest.json")
	dsnPath := filepath.Join(dir, "provisioner.secret")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dsnPath, []byte(postgresDSN(provisionerRole, env.provisionerPass, env.host, primaryDB, "wr19-cli")), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "iam")
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = findRepositoryRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build formal IAM: %v\n%s", err, out)
	}
	args := []string{"provision-workloads", "--manifest", manifestPath, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", m.Environment, "--trust-domain", m.TrustDomain, "--ca-file", caFile, "--dsn-file", dsnPath}
	var first []byte
	for i := range 2 {
		command := exec.Command(binary, args...)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("formal provisioner: %v\n%s", err, out)
		}
		if strings.Contains(string(out), env.provisionerPass) {
			t.Fatal("provisioner leaked credential")
		}
		var receipt biz.WorkloadBootstrapReceipt
		if json.Unmarshal(out, &receipt) != nil || receipt.ManifestID != m.ManifestID {
			t.Fatal("invalid non-secret receipt")
		}
		if i == 0 {
			first = out
		} else if string(first) != string(out) {
			t.Fatal("process restart changed receipt")
		}
	}
	args[4] = strings.Repeat("0", 64)
	if out, err := exec.Command(binary, args...).CombinedOutput(); err == nil || strings.Contains(string(out), env.provisionerPass) {
		t.Fatal("unapproved manifest accepted or credential leaked")
	}
}
