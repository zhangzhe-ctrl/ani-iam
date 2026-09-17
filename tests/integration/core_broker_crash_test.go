//go:build integration && !governance

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
)

func wr23RestartSameIAM(t *testing.T, e *wr23FormalEnvironment) {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr23" || run != e.boss.run || os.Getenv("WR23_FORMAL_COMBINATION") != "1" {
		t.Fatal("restart requires own formal WR23 IAM")
	}
	iam := e.boss.iam
	config := filepath.Join(iam.directory, "runtime.json")
	binary, err := os.ReadFile(iam.binary)
	if err != nil {
		t.Fatal("read own frozen binary")
	}
	binarySHA := sha256.Sum256(binary)
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal("read own exact runtime config")
	}
	configSHA := sha256.Sum256(raw)
	logFile := filepath.Join(run, "private", "iam-restart-"+mustV7(t).String()+".log")
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("own restart log")
	}
	cmd := exec.Command(iam.binary, "-conf", config)
	cmd.Dir = findRepositoryRoot(t)
	cmd.Env = append(os.Environ(), "GOMEMLIMIT=192MiB")
	cmd.Stdout = log
	cmd.Stderr = log
	if cmd.Start() != nil {
		log.Close()
		t.Fatal("restart exact formal IAM")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); log.Close() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Error("restarted IAM graceful exit failed")
			}
		case <-time.After(8 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("restarted IAM required forced stop")
		}
	})
	recordReference(t, run, map[string]any{"process": "ani-iam-server-restarted", "pid": cmd.Process.Pid, "binary_sha256": hex.EncodeToString(binarySHA[:]), "configuration_sha256": hex.EncodeToString(configSHA[:]), "stage": "restarted_same_binary_configuration"})
	waitFormalHTTP(t, iam.livenessURL, http.StatusOK)
	waitFormalHTTP(t, iam.readinessURL, http.StatusOK)
}

func TestWR23ResumeFormalBrokerCrashes(t *testing.T) {
	started := time.Now().UTC()
	e := newWR23FormalEnvironment(t)
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("formal crash owner %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	member := call("GET", "/iam/platform/members?limit=1", nil, 200)["items"].([]any)[0].(map[string]any)
	role := call("POST", "/iam/platform/roles", map[string]any{"name": "WR23 crash Core creator", "permissions": []string{"tenants/create"}}, 201)
	call("POST", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role["role_id"], "expected_membership_version": member["version"]}, 204)
	create := func() map[string]any {
		t.Helper()
		return call("POST", "/admin/tenants", map[string]any{"name": "wr23-crash-" + uuid.NewString()[:12], "display_name": "WR23 crash", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "crash-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, 200)
	}
	wait := func(label string, duration time.Duration, probe func() bool) {
		t.Helper()
		until := time.Now().Add(duration)
		for time.Now().Before(until) {
			if probe() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(label)
	}
	// Block only the owner's local publication receipt after NATS has accepted
	// the actual outbox payload. The separate IAM receipt proves acceptance.
	ownerHold, err := e.core.Begin(ctx)
	if err != nil {
		t.Fatal("own owner fault transaction")
	}
	defer ownerHold.Rollback(ctx)
	if _, err = ownerHold.Exec(ctx, `SELECT pg_advisory_xact_lock(230091)`); err != nil {
		t.Fatal("own publication boundary lock")
	}
	if _, err = e.core.Exec(ctx, `CREATE FUNCTION wr23_owner_publication_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='published' AND NEW.subject='ani.integration.tenant.lifecycle.v1' THEN PERFORM pg_advisory_xact_lock(230091); END IF; RETURN NEW; END $$; CREATE TRIGGER wr23_owner_publication_fault BEFORE UPDATE ON core_integration_outbox FOR EACH ROW EXECUTE FUNCTION wr23_owner_publication_fault()`); err != nil {
		t.Fatal("install own publication receipt boundary")
	}
	first := create()
	tenant := first["id"].(string)
	var event string
	var source, brokerSequence int64
	if e.core.QueryRow(ctx, `SELECT event_id::text,source_sequence FROM core_integration_outbox WHERE tenant_id=$1 AND subject='ani.integration.tenant.lifecycle.v1'`, tenant).Scan(&event, &source) != nil {
		t.Fatal("actual owner outbox source missing")
	}
	wait("broker did not accept original owner payload before receipt fault", 15*time.Second, func() bool {
		return b.owner.QueryRow(ctx, `SELECT broker_sequence FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, e.broker.configuration.Authority.ConsumerID, tenant, event).Scan(&brokerSequence) == nil
	})
	wait("owner did not reach held local receipt commit", 5*time.Second, func() bool {
		var waiting bool
		return e.core.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=current_database() AND l.locktype='advisory' AND l.objid=230091 AND NOT l.granted)`).Scan(&waiting) == nil && waiting
	})
	var state string
	if e.core.QueryRow(ctx, `SELECT state FROM core_integration_outbox WHERE source_sequence=$1`, source).Scan(&state) != nil || state != "publishing" {
		t.Fatal("local receipt committed before simulated crash")
	}
	e.gateway.crash()
	if ownerHold.Rollback(ctx) != nil {
		t.Fatal("release own publication boundary")
	}
	if _, err = e.core.Exec(ctx, `DROP TRIGGER wr23_owner_publication_fault ON core_integration_outbox; DROP FUNCTION wr23_owner_publication_fault()`); err != nil {
		t.Fatal("remove own publication boundary")
	}
	e.gateway = e.startGateway()
	wait("owner did not recover expired publication lease and broker dedupe", 65*time.Second, func() bool {
		var valid bool
		return e.core.QueryRow(ctx, `SELECT state='published' AND attempt_count>=2 AND broker_sequence=$2 FROM core_integration_outbox WHERE source_sequence=$1`, source, brokerSequence).Scan(&valid) == nil && valid
	})
	var count int
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2 AND event_id=$3`, e.broker.configuration.Authority.ConsumerID, tenant, event).Scan(&count) != nil || count != 1 {
		t.Fatal("publisher crash duplicated immutable IAM source receipt")
	}
	if e.core.QueryRow(ctx, `SELECT count(*) FROM core_outbox_outcomes WHERE source_sequence=$1 AND outcome='published'`, source).Scan(&count) != nil || count != 1 {
		t.Fatal("publisher restart did not retain one confirmed publication outcome")
	}
	// The next tenant reaches the real worker, whose transaction is interrupted
	// at its durable result boundary. Receiver state and broker ACK are separate.
	workerHold, err := b.owner.Begin(ctx)
	if err != nil {
		t.Fatal("own worker fault transaction")
	}
	defer workerHold.Rollback(ctx)
	if _, err = workerHold.Exec(ctx, `SELECT pg_advisory_xact_lock(230092)`); err != nil {
		t.Fatal("own worker boundary lock")
	}
	if _, err = b.owner.Exec(ctx, `CREATE FUNCTION wr23_worker_crash_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(230092); RETURN NEW; END $$; CREATE TRIGGER wr23_worker_crash_fault BEFORE INSERT ON core_bootstrap_worker_results FOR EACH ROW EXECUTE FUNCTION wr23_worker_crash_fault()`); err != nil {
		t.Fatal("install own worker boundary")
	}
	second := create()
	secondTenant, operation := second["id"].(string), second["bootstrap_operation_id"].(string)
	wait("actual worker did not reach held durable result", 15*time.Second, func() bool {
		var waiting bool
		return b.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=current_database() AND l.locktype='advisory' AND l.objid=230092 AND NOT l.granted)`).Scan(&waiting) == nil && waiting
	})
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_receipts WHERE tenant_id=$1 AND operation_id=$2`, secondTenant, operation).Scan(&count) != nil || count != 1 {
		t.Fatal("worker ran without original durable receipt")
	}
	b.iam.crash()
	if workerHold.Rollback(ctx) != nil {
		t.Fatal("release own worker boundary")
	}
	if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr23_worker_crash_fault ON core_bootstrap_worker_results; DROP FUNCTION wr23_worker_crash_fault()`); err != nil {
		t.Fatal("remove own worker boundary")
	}
	if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1)+(SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1)`, secondTenant).Scan(&count) != nil || count != 0 {
		t.Fatal("crashed worker partially committed identity effects")
	}
	wr23RestartSameIAM(t, e)
	wait("restarted formal receiver and worker did not complete original operation", 65*time.Second, func() bool {
		return b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1 AND operation_id=$2`, secondTenant, operation).Scan(&count) == nil && count == 1
	})
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1 AND bootstrap_operation_id=$2`, secondTenant, operation).Scan(&count) != nil || count != 1 {
		t.Fatal("worker restart duplicated or changed invitation")
	}
	wait("durable source gap remained after process restarts", 15*time.Second, func() bool {
		var valid bool
		return b.owner.QueryRow(ctx, `SELECT contiguous_sequence=highest_sequence AND progress_at>clock_timestamp()-interval '30 seconds' FROM core_lifecycle_pipelines WHERE producer=$1`, e.broker.configuration.Authority.Producer).Scan(&valid) == nil && valid
	})
	result, _ := json.MarshalIndent(map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "publisher_crash_after_actual_broker_accept": true, "original_source_sequence": source, "original_broker_sequence": brokerSequence, "one_immutable_authority_receipt": true, "worker_crash_rollback": true, "consumer_worker_restart_same_binary_configuration": true, "second_tenant_id": secondTenant, "operation_id": operation, "gap_free": true, "grade": "actual Core outbox/NATS acceptance plus SIGKILL of own formal processes and real lease recovery"}, "", "  ")
	if os.WriteFile(filepath.Join(b.run, "formal-crash-results.json"), append(result, '\n'), 0600) != nil {
		t.Fatal("write crash proof")
	}
}
