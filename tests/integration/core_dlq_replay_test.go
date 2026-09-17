//go:build integration && !governance

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type wr23DLQTestAPI struct {
	call    func(string, string, any, string, int) map[string]any
	command func(string) map[string]any
	inspect func(string) map[string]any
	change  func(string)
	wait    func(string, func() bool)
}

// Raw transport failures are expected in these fault cases. Never log tokens,
// request bodies, cookies or driver details to public evidence.
func wr23DLQRawRequest(e *wr23FormalEnvironment, client *http.Client, access, path, key string, body any) (int, map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", e.boss.origin+"/api/v1"+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", e.boss.origin)
	r.Header.Set("Authorization", "Bearer "+access)
	r.Header.Set("Idempotency-Key", key)
	u, _ := url.Parse(e.boss.origin + "/api/v1/auth")
	if client.Jar != nil {
		for _, c := range client.Jar.Cookies(u) {
			if c.Name == "ani_boss_csrf" {
				r.Header.Set("X-CSRF-Token", c.Value)
			}
		}
	}
	response, err := client.Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	value, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, nil, err
	}
	var doc map[string]any
	if len(value) > 0 {
		if err = json.Unmarshal(value, &doc); err != nil {
			return response.StatusCode, nil, err
		}
	}
	return response.StatusCode, doc, nil
}

var errWR23DLQResponseLost = errors.New("WR23 deliberately discarded committed response")

type wr23DLQLostResponse struct {
	next http.RoundTripper
	path string
	lost atomic.Bool
}

func (r *wr23DLQLostResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := r.next.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if request.URL.Path == r.path && response.StatusCode == 200 && r.lost.CompareAndSwap(false, true) {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		return nil, errWR23DLQResponseLost
	}
	return response, nil
}

func wr23DLQFaultMatrix(t *testing.T, e *wr23FormalEnvironment, browser *http.Client, access *string, base, firstTenant string, api wr23DLQTestAPI) []string {
	t.Helper()
	ctx := context.Background()
	b := e.boss
	a := e.broker.configuration.Authority
	api.change("revoked")
	created := api.call("POST", "/admin/tenants", map[string]any{"name": "wr23-dlq-fault-" + uuid.NewString()[:10], "display_name": "WR23 DLQ faults", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "dlq-fault-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, "", 200)
	tenant, operation := created["id"].(string), created["bootstrap_operation_id"].(string)
	var entry, bootstrap string
	api.wait("second actual Core originals did not reach DLQ", func() bool {
		return b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.lifecycle.v1' AND convert_from(raw_payload,'utf8')::jsonb->'envelope'->>'tenant_id'=$2`, a.ConsumerID, tenant).Scan(&entry) == nil && b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND subject='ani.integration.tenant.iam-bootstrap.v1' AND convert_from(raw_payload,'utf8')::jsonb->'envelope'->>'tenant_id'=$2`, a.ConsumerID, tenant).Scan(&bootstrap) == nil
	})
	api.change("active")
	path := base + "/" + entry + "/replay"
	command := api.command(entry)
	key := uuid.NewString()
	// Kill the actual IAM after receiver effects and before its successful audit
	// can commit. The transaction must disappear with no successful receipt.
	hold, err := b.owner.Begin(ctx)
	if err != nil {
		t.Fatal("own DLQ crash lock")
	}
	defer hold.Rollback(ctx)
	if _, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(230093)`); err != nil {
		t.Fatal("own DLQ audit barrier")
	}
	if _, err = b.owner.Exec(ctx, `CREATE FUNCTION wr23_dlq_crash_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.replayCoreIAMDLQEntry' AND NEW.result='succeeded' THEN PERFORM pg_advisory_xact_lock(230093); END IF; RETURN NEW; END $$; CREATE TRIGGER wr23_dlq_crash_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr23_dlq_crash_fault()`); err != nil {
		t.Fatal("install own DLQ commit barrier")
	}
	type outcome struct {
		status int
		doc    map[string]any
		err    error
	}
	pending := make(chan outcome, 1)
	go func() {
		s, d, e2 := wr23DLQRawRequest(e, browser, *access, path, key, command)
		pending <- outcome{s, d, e2}
	}()
	until := time.Now().Add(1500 * time.Millisecond)
	waiting := false
	for time.Now().Before(until) {
		if b.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity s ON s.pid=l.pid WHERE s.datname=current_database() AND l.locktype='advisory' AND l.objid=230093 AND NOT l.granted)`).Scan(&waiting) == nil && waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("DLQ receive did not reach real pre-commit barrier")
	}
	b.iam.crash()
	if hold.Rollback(ctx) != nil {
		t.Fatal("release own DLQ commit barrier")
	}
	if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr23_dlq_crash_fault ON iam_audit_events; DROP FUNCTION wr23_dlq_crash_fault()`); err != nil {
		t.Fatal("remove own DLQ commit barrier")
	}
	interrupted := <-pending
	if interrupted.err == nil && interrupted.status >= 200 && interrupted.status < 300 {
		t.Fatal("crashed DLQ transaction reported success")
	}
	var count int
	if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND tenant_id=$2)+(SELECT count(*) FROM core_broker_dlq_attempts WHERE consumer_id=$1 AND entry_id=$3 AND outcome<>'failed')`, a.ConsumerID, tenant, entry).Scan(&count) != nil || count != 0 {
		t.Fatal("crashed DLQ transaction partially committed")
	}
	recordReference(t, b.run, map[string]any{"kind": "dlq-process-crash-before-commit", "entry_id": entry, "gateway_status": interrupted.status, "business_receipt_absent": true, "successful_attempt_absent": true, "failure_attempt": "process died before post-rollback recorder; no failure record is fabricated"})
	wr23RestartSameIAM(t, e)
	// Drop an actual successful HTTP response at the client boundary, then retry
	// its exact identity. This is distinct from fabricating an accepted response.
	command = api.command(entry)
	key = uuid.NewString()
	transport := browser.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	lossy := &wr23DLQLostResponse{next: transport, path: "/api/v1" + path}
	copyClient := *browser
	copyClient.Transport = lossy
	_, _, err = wr23DLQRawRequest(e, &copyClient, *access, path, key, command)
	if !errors.Is(err, errWR23DLQResponseLost) || !lossy.lost.Load() {
		t.Fatal("committed DLQ response was not actually discarded")
	}
	var attemptID string
	if b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq_attempts WHERE consumer_id=$1 AND entry_id=$2 AND outcome='received'`, a.ConsumerID, entry).Scan(&attemptID) != nil {
		t.Fatal("discarded response lacked durable receiver attempt")
	}
	replay := api.call("POST", path, command, key, 200)
	if replay["attempt_id"] != attemptID || replay["replayed"] != true {
		t.Fatal("lost response retry duplicated effects")
	}
	api.call("POST", base+"/"+bootstrap+"/replay", api.command(bootstrap), "", 200)
	api.wait("post-crash original Bootstrap worker did not continue", func() bool {
		return b.owner.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_worker_results WHERE tenant_id=$1 AND operation_id=$2`, tenant, operation).Scan(&count) == nil && count == 1
	})
	// A current source duplicate uses the same immutable receipt. Binding versions
	// remain unchanged between these requests; stale fingerprints are not bypassed.
	duplicate := api.call("POST", path, api.command(entry), "", 200)
	if duplicate["outcome"] != "duplicate" {
		t.Fatal("new request for received original did not report duplicate")
	}
	wr23DLQCommitAuthority(t, e, browser, access, entry, path, api)
	// Both the business success audit and the later failure recorder fail. No
	// attempt is invented, and the client receives an unavailable outcome.
	beforeFailure := api.inspect(entry)["entry"].(map[string]any)["last_attempt"]
	failCommand := api.command(entry)
	if _, err = b.owner.Exec(ctx, `CREATE FUNCTION wr23_dlq_all_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.replayCoreIAMDLQEntry' THEN RAISE EXCEPTION 'WR23 own audit storage unavailable' USING ERRCODE='23514'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr23_dlq_all_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr23_dlq_all_audit_fault()`); err != nil {
		t.Fatal("install own audit unavailable fault")
	}
	api.call("POST", path, failCommand, "", 503)
	if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr23_dlq_all_audit_fault ON iam_audit_events; DROP FUNCTION wr23_dlq_all_audit_fault()`); err != nil {
		t.Fatal("remove own audit unavailable fault")
	}
	if api.inspect(entry)["entry"].(map[string]any)["last_attempt"] != beforeFailure {
		t.Fatal("audit outage fabricated a completed attempt")
	}
	// Use the run's real producer key only for declared negative wire cases.
	seed, err := os.ReadFile(e.broker.producerSeed)
	if err != nil {
		t.Fatal("own producer credential unavailable")
	}
	signer, err := nkeys.FromSeed(bytes.TrimSpace(seed))
	clear(seed)
	if err != nil {
		t.Fatal("own producer credential invalid")
	}
	defer signer.Wipe()
	ca, err := os.ReadFile(e.broker.configuration.CAFile)
	roots := x509.NewCertPool()
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		t.Fatal("own broker trust unavailable")
	}
	nc, err := nats.Connect(e.broker.configuration.URL, nats.Nkey(e.broker.producerNKey, signer.Sign), nats.Secure(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: e.broker.configuration.ServerName, RootCAs: roots}), nats.TLSHandshakeFirst(), nats.IgnoreDiscoveredServers(), nats.CustomInboxPrefix(e.broker.inbox+".producer"), nats.Timeout(2*time.Second), nats.ReconnectBufSize(0))
	if err != nil {
		t.Fatal("own negative producer transport failed")
	}
	defer nc.Close()
	js, err := nc.JetStream(nats.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatal("own negative producer JetStream")
	}
	var original []byte
	if e.core.QueryRow(ctx, `SELECT payload FROM core_integration_outbox WHERE tenant_id=$1 AND subject='ani.integration.tenant.lifecycle.v1'`, tenant).Scan(&original) != nil {
		t.Fatal("actual second owner original missing")
	}
	var changed map[string]any
	if json.Unmarshal(original, &changed) != nil {
		t.Fatal("actual owner source decode")
	}
	changed["envelope"].(map[string]any)["tenant_id"] = firstTenant
	cross, _ := json.Marshal(changed)
	for _, test := range []struct {
		name    string
		raw     []byte
		headers nats.Header
		status  int
	}{
		{"invalid_json", []byte("broken"), nil, 400},
		{"cross_tenant_same_event", cross, nil, 409},
		{"same_event_changed_raw", append(append([]byte(nil), original...), ' '), nil, 409},
		{"self_reported_identity_header", original, nats.Header{"X-ANI-Producer": []string{"untrusted"}}, 400},
	} {
		t.Logf("DLQ negative original case: %s", test.name)
		ack, err := js.PublishMsg(&nats.Msg{Subject: "ani.integration.tenant.lifecycle.v1", Data: test.raw, Header: test.headers})
		if err != nil {
			t.Fatal("declared negative wire publication failed")
		}
		var id string
		api.wait("negative source did not durably quarantine: "+test.name, func() bool {
			return b.owner.QueryRow(ctx, `SELECT id::text FROM core_broker_dlq WHERE consumer_id=$1 AND broker_sequence=$2`, a.ConsumerID, int64(ack.Sequence)).Scan(&id) == nil
		})
		before := api.inspect(id)
		api.call("POST", base+"/"+id+"/replay", api.command(id), "", test.status)
		after := api.inspect(id)
		if before["raw_payload"] != after["raw_payload"] || after["entry"].(map[string]any)["last_attempt"] != float64(1) {
			t.Fatal("negative replay changed original or lost attempt")
		}
		if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_authority_receipts WHERE consumer_id=$1 AND broker_sequence=$2`, a.ConsumerID, int64(ack.Sequence)).Scan(&count) != nil || count != 0 {
			t.Fatal("poison/cross-Tenant message reached business receipt")
		}
	}
	// Controlled legacy storage fixture: copy an actual accepted broker original,
	// its exact position/time and headers, without manufacturing old provenance.
	// This is a negative storage case, not an actual historic quarantine claim.
	var position int64
	if b.owner.QueryRow(ctx, `SELECT r.broker_sequence FROM core_broker_authority_receipts r WHERE r.consumer_id=$1 AND NOT EXISTS(SELECT 1 FROM core_broker_dlq d WHERE d.consumer_id=r.consumer_id AND d.broker_sequence=r.broker_sequence) ORDER BY r.broker_sequence DESC LIMIT 1`, a.ConsumerID).Scan(&position) != nil {
		t.Fatal("actual accepted source for legacy negative fixture missing")
	}
	native, err := e.broker.operator.GetMsg(a.Stream, uint64(position))
	if err != nil {
		t.Fatal("actual broker original read failed")
	}
	headers := native.Header
	if headers == nil {
		headers = nats.Header{}
	}
	encoded, _ := json.Marshal(headers)
	legacy := mustV7(t)
	hash := sha256.Sum256(native.Data)
	if _, err = b.owner.Exec(ctx, `INSERT INTO core_broker_dlq(consumer_id,id,broker_sequence,broker_name,account_name,stream_name,subject,raw_payload,raw_sha256,headers,delivery_count,published_at,last_error) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,'authority_denied')`, a.ConsumerID, legacy, position, a.BrokerName, a.Account, a.Stream, native.Subject, native.Data, hash[:], encoded, native.Time); err != nil {
		t.Fatal("prepare bounded missing-provenance negative fixture")
	}
	if api.inspect(legacy.String())["entry"].(map[string]any)["provenance_available"] != false {
		t.Fatal("legacy original acquired fabricated provenance")
	}
	api.call("POST", base+"/"+legacy.String()+"/replay", api.command(legacy.String()), "", 409)
	if b.owner.QueryRow(ctx, `SELECT count(*) FROM core_broker_dlq_context WHERE consumer_id=$1 AND entry_id=$2`, a.ConsumerID, legacy).Scan(&count) != nil || count != 0 {
		t.Fatal("legacy replay backfilled provenance")
	}
	recordReference(t, b.run, map[string]any{"kind": "dlq-missing-provenance-negative-fixture", "entry_id": legacy, "actual_broker_sequence": position, "actual_published_at": native.Time, "source_sha256": hex.EncodeToString(hash[:]), "delivery_count": "fixture field; not a historical redelivery measurement", "historical_quarantine_claim": false, "result": "denied_without_backfill"})
	// Snapshot can activate only after source contiguity is established. Replaying
	// an already received source after the cut must stay duplicate and never rewind.
	rows, err := b.owner.Query(ctx, `SELECT d.id::text FROM core_broker_dlq d WHERE d.consumer_id=$1 AND d.subject='ani.integration.tenant.lifecycle-heartbeat.v1' AND EXISTS(SELECT 1 FROM core_broker_dlq_context c WHERE c.consumer_id=d.consumer_id AND c.entry_id=d.id) AND NOT EXISTS(SELECT 1 FROM core_broker_dlq_attempts a WHERE a.consumer_id=d.consumer_id AND a.entry_id=d.id AND a.outcome<>'failed') ORDER BY d.broker_sequence`, a.ConsumerID)
	if err != nil {
		t.Fatal("remaining real heartbeat originals")
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			t.Fatal("heartbeat identity")
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		api.call("POST", base+"/"+id+"/replay", api.command(id), "", 200)
	}
	config := filepath.Join(b.iam.directory, "runtime.json")
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal("own frozen Snapshot config")
	}
	sum := sha256.Sum256(raw)
	proof := wr23PrivateCommand(t, b.run, "dlq-following-snapshot", exec.Command(b.iam.binary, "begin-core-snapshot", "--config-file", config, "--approved-config-sha256", hex.EncodeToString(sum[:]), "--request-key", uuid.NewString(), "--page-size", "1"))
	var build struct {
		Generation string `json:"generation_id"`
		SourceCut  int64  `json:"source_cut"`
	}
	if json.Unmarshal(proof, &build) != nil || build.Generation == "" {
		t.Fatal("actual authenticated Snapshot cut missing")
	}
	api.wait("DLQ recovery did not allow real full rebuild and atomic activation", func() bool {
		var ready bool
		return b.owner.QueryRow(ctx, `SELECT p.generation_id=$2 AND b.state='activated' AND p.contiguous_sequence=p.highest_sequence FROM core_lifecycle_pipelines p JOIN core_lifecycle_rebuilds b ON b.producer=p.producer AND b.generation_id=p.generation_id WHERE p.producer=$1`, a.Producer, build.Generation).Scan(&ready) == nil && ready
	})
	after := api.call("POST", path, api.command(entry), "", 200)
	if after["outcome"] != "duplicate" {
		t.Fatal("post-cut replay rewrote an existing source")
	}
	var intact bool
	if b.owner.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(status='active' AND lifecycle_version=1 AND NOT repair_required) FROM core_current_lifecycle_facts WHERE producer=$1 AND tenant_id=ANY($2::uuid[])`, a.Producer, []uuid.UUID{uuid.MustParse(firstTenant), uuid.MustParse(tenant)}).Scan(&intact) != nil || !intact {
		t.Fatal("cross-Tenant negative replay or Snapshot changed actual owner facts")
	}
	// A previous successful request cannot erase a later failed request's audit.
	// Change Binding versions only after all new-key original-receive cases.
	beforeRevocation := api.inspect(entry)["entry"].(map[string]any)["last_attempt"].(float64)
	api.change("revoked")
	api.call("POST", path, command, key, 503)
	if api.inspect(entry)["entry"].(map[string]any)["last_attempt"] != beforeRevocation+1 {
		t.Fatal("older success suppressed a new current-authority failure attempt")
	}
	api.change("active")
	restored := api.call("POST", path, command, key, 200)
	if restored["attempt_id"] != attemptID || restored["replayed"] != true {
		t.Fatal("current-authority restoration lost the original committed receipt")
	}
	recordReference(t, b.run, map[string]any{"kind": "dlq-real-fault-matrix", "tenant_id": tenant, "entry_id": entry, "snapshot_generation": build.Generation, "source_cut": build.SourceCut, "covered_outcome": "not exercised: retained receipts and contiguous activation yield duplicate; no fake cut or receipt deletion", "result": "pass"})
	return []string{"actual_iam_crash_before_commit", "actual_committed_response_loss", "same_original_worker_after_restart", "duplicate_receiver_result", "current_authority_commit_races", "unavailable_failure_audit_reported", "poison_original_and_identity_header", "same_event_different_bytes", "cross_tenant_event_conflict", "missing_provenance_no_backfill", "real_snapshot_cut_duplicate_no_rewind", "older_success_preserves_new_failure_audit"}
}

func wr23DLQCommitAuthority(t *testing.T, e *wr23FormalEnvironment, browser *http.Client, access *string, entry, path string, api wr23DLQTestAPI) {
	t.Helper()
	ctx := context.Background()
	b := e.boss
	a := e.broker.configuration.Authority
	command := api.command(entry)
	key := uuid.NewString()
	var actor, session uuid.UUID
	if b.owner.QueryRow(ctx, `SELECT d.actor_id,s.id FROM core_broker_dlq_attempts d JOIN sessions s ON s.principal_id=d.actor_id AND s.audience='boss' AND s.status='active' WHERE d.consumer_id=$1 AND d.entry_id=$2 ORDER BY d.attempt_number DESC,s.created_at DESC LIMIT 1`, a.ConsumerID, entry).Scan(&actor, &session) != nil {
		t.Fatal("actual replay actor/session missing")
	}
	held, err := b.owner.Begin(ctx)
	if err != nil {
		t.Fatal("own authority commit barrier")
	}
	defer held.Rollback(ctx)
	if _, err = held.Exec(ctx, `SELECT pg_advisory_xact_lock(230094)`); err != nil {
		t.Fatal("hold own authority commit barrier")
	}
	if _, err = b.owner.Exec(ctx, `CREATE FUNCTION wr23_dlq_authority_hold() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.platform.replayCoreIAMDLQEntry' AND NEW.result='succeeded' THEN PERFORM pg_advisory_xact_lock(230094); END IF; RETURN NEW; END $$; CREATE TRIGGER wr23_dlq_authority_hold BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr23_dlq_authority_hold()`); err != nil {
		t.Fatal("install own authority barrier")
	}
	type reply struct {
		status int
		doc    map[string]any
		err    error
	}
	pending := make(chan reply, 1)
	token := *access
	go func() {
		s, d, e2 := wr23DLQRawRequest(e, browser, token, path, key, command)
		pending <- reply{s, d, e2}
	}()
	until := time.Now().Add(1500 * time.Millisecond)
	waiting := false
	for time.Now().Before(until) {
		if b.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity s ON s.pid=l.pid WHERE s.datname=current_database() AND l.locktype='advisory' AND l.objid=230094 AND NOT l.granted)`).Scan(&waiting) == nil && waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("actual replay did not retain authority through commit barrier")
	}
	probe, err := b.owner.Begin(ctx)
	if err != nil {
		t.Fatal("Broker authority lock probe")
	}
	var acquired bool
	err = probe.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('ani-iam:core-broker-authority',0))`).Scan(&acquired)
	_ = probe.Rollback(ctx)
	if err != nil || acquired {
		t.Fatal("replay released Broker authority before commit")
	}
	guard, err := b.owner.Begin(ctx)
	if err != nil {
		t.Fatal("Platform role guard probe")
	}
	if _, err = guard.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
		t.Fatal("bounded Platform guard probe")
	}
	_, err = guard.Exec(ctx, `SELECT singleton FROM platform_administrator_guard WHERE singleton FOR UPDATE`)
	_ = guard.Rollback(ctx)
	var locked *pgconn.PgError
	if !errors.As(err, &locked) || locked.Code != "55P03" {
		t.Fatal("Platform guard did not produce the expected lock timeout")
	}
	revoked := make(chan error, 1)
	go func() {
		r, err := b.owner.Exec(ctx, `/* wr23_dlq_session_race */ UPDATE sessions SET status='revoked',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND principal_id=$2 AND audience='boss' AND status='active'`, session, actor)
		if err == nil && r.RowsAffected() != 1 {
			err = errors.New("own Session revoke CAS failed")
		}
		revoked <- err
	}()
	blocked := false
	until = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		if b.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '/* wr23_dlq_session_race */ UPDATE%')`).Scan(&blocked) == nil && blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("Session mutation passed replay's current Human row lock")
	}
	select {
	case <-revoked:
		t.Fatal("Session revocation committed before replay released current authority")
	default:
	}
	if held.Rollback(ctx) != nil {
		t.Fatal("release own replay commit barrier")
	}
	result := <-pending
	if result.err != nil || result.status != 200 || result.doc["outcome"] != "duplicate" {
		t.Fatal("authorized transaction failed before the waiting revocation")
	}
	if err = <-revoked; err != nil {
		t.Fatal("waiting Session revocation did not commit")
	}
	if _, err = b.owner.Exec(ctx, `DROP TRIGGER wr23_dlq_authority_hold ON iam_audit_events; DROP FUNCTION wr23_dlq_authority_hold()`); err != nil {
		t.Fatal("remove own authority barrier")
	}
	denied, _, err := wr23DLQRawRequest(e, browser, token, path, key, command)
	if err != nil || (denied != 401 && denied != 403) {
		t.Fatal("revoked Human Session reused committed DLQ receipt")
	}
	*access = e.login(t, browser)
	recordReference(t, b.run, map[string]any{"kind": "dlq-current-authority-held-through-commit", "entry_id": entry, "broker_exclusive_change_blocked": true, "platform_role_guard_blocked": true, "session_revocation_waited_for_commit": true, "revoked_session_cached_receipt_status": denied, "new_real_boss_login": true})
}
