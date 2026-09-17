//go:build integration && !governance

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Rehearsal deliberately cannot produce active_24h=pass. Both cases use real
// wall time and the same owner/transport/runtime path; no clock is substituted.
func TestWR23ResumeFormalShadow(t *testing.T) {
	kind := os.Getenv("WR23_SHADOW_KIND")
	duration, rebuildAfter, refreshAfter, loginAfter := 24*time.Hour, 12*time.Hour, 5*time.Minute, 4*time.Hour
	switch kind {
	case "active24h":
		wr23ShadowAdmission(t)
	case "accepted12h30":
		wr23ShadowAdmission(t)
		duration = 12*time.Hour + 30*time.Minute
	case "rehearsal":
		duration, rebuildAfter, refreshAfter, loginAfter = 2*time.Minute, 45*time.Second, 25*time.Second, 55*time.Second
	default:
		t.Fatal("explicit shadow kind required")
	}
	e := newWR23FormalEnvironment(t)
	actors := wr23RunFormalLifecycleChain(t, e)
	b, ctx := e.boss, context.Background()
	run := b.run
	bossToken := e.access(t, actors.boss)
	memberToken := actors.memberToken
	started := time.Now()
	lastRefresh, lastLogin := started, started
	state := map[string]any{"result": "running", "kind": kind, "active_24h": "not_verified", "started_at": started.UTC(), "duration_seconds": duration.Seconds(), "sample_interval_seconds": 15, "poll_milliseconds": 200, "tenant_id": actors.tenant, "samples": 0, "authorization_comparisons": 0, "unexplained_authorization_differences": 0, "propagation_origin": "Core outbox occurred_at", "propagation_end": "first observer-visible committed current IAM generation with exact immutable receipt", "pid": os.Getpid()}
	frozen := map[string]string{}
	for label, path := range map[string]string{"source": filepath.Join(run, "source.json"), "runtime": filepath.Join(b.iam.directory, "runtime.json"), "core_lifecycle": b.iam.config.Runtime.CoreLifecycleFile, "core_runtime": filepath.Join(run, "private/core-runtime.json"), "registry": wr32RegistryPath(t), "images": filepath.Join(run, "formal-images-results.json")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("shadow input digest unavailable", label)
		}
		frozen[label] = hexDigest(raw)
	}
	state["frozen_inputs_sha256"] = frozen
	wr23ShadowWrite(t, run, "shadow-started-results.json", state)
	success := false
	defer func() {
		state["finished_at"] = time.Now().UTC()
		state["elapsed_seconds"] = time.Since(started).Seconds()
		if !success {
			state["result"] = "fail"
		}
		wr23ShadowWrite(t, run, "shadow-state-results.json", state)
		wr23ShadowWrite(t, run, "shadow-finished-results.json", state)
	}()
	samples, err := os.OpenFile(filepath.Join(run, "shadow-samples.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("exclusive shadow sample file", err)
	}
	defer samples.Close()
	latencies := []float64{}
	var previousEvent time.Time
	version := int64(1)
	status := "active"
	count := 0
	var build struct {
		Snapshot   string `json:"snapshot_id"`
		Generation string `json:"generation_id"`
		SourceCut  int64  `json:"source_cut"`
		State      string `json:"state"`
	}
	var buildStarted time.Time
	buildFinished := false
	bossCall := func(method, path string, body any) map[string]any {
		t.Helper()
		code, doc, _ := b.request(t, actors.boss, method, path, bossToken, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if code != 200 {
			t.Fatalf("shadow Core %s status=%d reason=%v", path, code, doc["code"])
		}
		return doc
	}
	for time.Since(started) < duration || (kind == "accepted12h30" && (!buildFinished || time.Since(state["rebuild_activated_at"].(time.Time)) < 30*time.Minute)) {
		scheduled := started.Add(time.Duration(count) * 15 * time.Second)
		if pause := time.Until(scheduled); pause > 0 {
			time.Sleep(pause)
		}
		if time.Since(started) >= duration && (kind != "accepted12h30" || (buildFinished && time.Since(state["rebuild_activated_at"].(time.Time)) >= 30*time.Minute)) {
			break
		}
		// Refresh on the restored Tenant, before the next freeze. Re-login uses the
		// existing BOSS identity through actual OIDC; first-admin intent is not reused.
		if status == "active" && time.Since(lastRefresh) >= refreshAfter {
			if time.Since(lastLogin) >= loginAfter {
				actors.boss = b.browser(t)
				bossToken = e.login(t, actors.boss)
				lastLogin = time.Now()
				state["last_real_boss_login_at"] = lastLogin.UTC()
			} else {
				bossToken = e.access(t, actors.boss)
			}
			code, doc, _ := e.console.request(t, actors.member, "POST", "/auth/refresh", "", nil, nil)
			if code != 200 {
				t.Fatalf("shadow real Console refresh status=%d reason=%v", code, doc["code"])
			}
			var ok bool
			memberToken, ok = doc["access_token"].(string)
			if !ok || memberToken == "" {
				t.Fatal("shadow refresh missing token")
			}
			lastRefresh = time.Now()
			state["last_real_refresh_at"] = lastRefresh.UTC()
		}
		if build.Snapshot == "" && time.Since(started) >= rebuildAfter {
			configFile := filepath.Join(b.iam.directory, "runtime.json")
			command := exec.Command(b.iam.binary, "begin-core-snapshot", "--config-file", configFile, "--approved-config-sha256", frozen["runtime"], "--request-key", uuid.NewString(), "--page-size", "1")
			proof := wr23PrivateCommand(t, run, "shadow-snapshot-begin", command)
			if json.Unmarshal(proof, &build) != nil || build.Snapshot == "" || build.Generation == "" || build.State != "loading" {
				t.Fatal("shadow authenticated full rebuild did not begin")
			}
			buildStarted = time.Now()
			state["snapshot_id"] = build.Snapshot
			state["snapshot_source_cut"] = build.SourceCut
		}
		action, next := "freeze", "frozen"
		if status == "frozen" {
			action, next = "unfreeze", "active"
		}
		response := bossCall("POST", "/admin/tenants/"+actors.tenant+"/"+action, map[string]any{"version": version, "idempotency_key": uuid.NewString()})
		version++
		if response["status"] != next || response["lifecycle_version"] != float64(version) {
			t.Fatal("shadow actual Core change differs")
		}
		var event, rawSHA string
		var sequence int64
		var occurred time.Time
		if e.core.QueryRow(ctx, `SELECT event_id::text,source_sequence,encode(payload_sha256,'hex'),occurred_at FROM core_integration_outbox WHERE tenant_id=$1 AND aggregate_version=$2 AND subject='ani.integration.tenant.lifecycle.v1'`, actors.tenant, version).Scan(&event, &sequence, &rawSHA, &occurred) != nil {
			t.Fatal("shadow actual outbox identity unavailable")
		}
		if !previousEvent.IsZero() && occurred.Sub(previousEvent) > 45*time.Second {
			t.Fatal("shadow lifecycle activity gap exceeds 45 seconds")
		}
		previousEvent = occurred
		var observed time.Time
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			var ready bool
			err := b.owner.QueryRow(ctx, `SELECT f.status=$3 AND f.lifecycle_version=$4 AND NOT f.repair_required AND f.fresh_until>clock_timestamp() AND r.event_id=$5 AND encode(r.raw_sha256,'hex')=$6 AND r.outcome='applied' FROM core_current_lifecycle_facts f JOIN core_integration_receipts r ON r.producer=f.producer AND r.tenant_id=f.tenant_id AND r.source_sequence=$7 WHERE f.producer=$1 AND f.tenant_id=$2`, e.broker.configuration.Authority.Producer, actors.tenant, next, version, event, rawSHA, sequence).Scan(&ready)
			if err == nil && ready {
				observed = time.Now()
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if observed.IsZero() {
			t.Fatal("shadow lifecycle propagation did not complete within 30 seconds")
		}
		latency := observed.Sub(occurred).Seconds()
		if latency < 0 {
			t.Fatal("shadow clock moved before Core event")
		}
		latencies = append(latencies, latency)
		owner := bossCall("GET", "/admin/tenants/"+actors.tenant, nil)
		if owner["status"] != next || owner["lifecycle_version"] != float64(version) {
			t.Fatal("shadow Core authority changed unexpectedly")
		}
		want := 200
		if next != "active" {
			want = 403
		}
		code, doc, _ := e.console.request(t, actors.member, "GET", "/iam/tenants/"+actors.tenant+"/members", memberToken, nil, nil)
		// A second actual request must remain forbidden under the immutable ordinary
		// reader role. No writes are performed by either comparison request.
		denied, denial, _ := e.console.request(t, actors.member, "GET", "/iam/tenants/"+actors.tenant+"/roles", memberToken, nil, nil)
		difference := code != want || denied != 403
		var gaps, dlq int
		if b.owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM core_current_lifecycle_facts WHERE producer=$1 AND repair_required),(SELECT count(*) FROM core_broker_dlq)`, e.broker.configuration.Authority.Producer).Scan(&gaps, &dlq) != nil {
			t.Fatal("shadow gap/quarantine observation unavailable")
		}
		if build.Snapshot != "" && !buildFinished {
			var ready bool
			if b.owner.QueryRow(ctx, `SELECT p.generation_id=$2 AND r.state='activated' AND p.contiguous_sequence=p.highest_sequence AND r.applied_through>=r.source_cut FROM core_lifecycle_pipelines p JOIN core_lifecycle_rebuilds r ON r.producer=p.producer AND r.generation_id=$2 WHERE p.producer=$1`, e.broker.configuration.Authority.Producer, build.Generation).Scan(&ready) == nil && ready {
				buildFinished = true
				state["full_rebuild"] = "pass"
				state["rebuild_activated_at"] = time.Now().UTC()
			} else if time.Since(buildStarted) > 45*time.Second {
				t.Fatal("shadow full rebuild did not atomically activate")
			}
		}
		count++
		sample := map[string]any{"index": count, "event_id": event, "source_sequence": sequence, "raw_sha256": rawSHA, "tenant_id": actors.tenant, "lifecycle_version": version, "owner_status": next, "occurred_at": occurred.UTC(), "observed_at": observed.UTC(), "propagation_seconds": latency, "membership_read_status": code, "membership_read_reason": doc["code"], "expected_membership_read_status": want, "role_read_status": denied, "role_read_reason": denial["code"], "unexplained_difference": difference, "unresolved_tenant_gaps": gaps, "quarantined_events": dlq}
		raw, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = samples.Write(append(raw, '\n')); err != nil || samples.Sync() != nil {
			t.Fatal("durable shadow sample write failed")
		}
		state["samples"] = count
		state["authorization_comparisons"] = 2 * count
		state["last_activity_at"] = occurred.UTC()
		state["last_observed_at"] = observed.UTC()
		state["unresolved_tenant_gaps"] = gaps
		state["quarantined_events"] = dlq
		if difference {
			state["unexplained_authorization_differences"] = 1
		}
		wr23ShadowWrite(t, run, "shadow-state-results.json", state)
		if difference || gaps != 0 || dlq != 0 {
			t.Fatal("shadow unexplained difference, gap or quarantine; sample retained")
		}
		status = next
	}
	if !buildFinished {
		t.Fatal("shadow full rebuild incomplete")
	}
	if count < int(duration/(15*time.Second)) || time.Since(started) < duration {
		t.Fatal("shadow active window or denominator incomplete")
	}
	sort.Float64s(latencies)
	p99 := latencies[int(math.Ceil(.99*float64(len(latencies))))-1]
	state["p99_seconds"] = p99
	state["max_propagation_seconds"] = latencies[len(latencies)-1]
	if p99 > 5 {
		t.Fatal("shadow p99 exceeds 5 seconds")
	}
	state["post_rebuild_seconds"] = time.Since(state["rebuild_activated_at"].(time.Time)).Seconds()
	state["result"] = "pass"
	if kind == "active24h" {
		state["active_24h"] = "pass"
	}
	success = true
}

func hexDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func wr23ShadowWrite(t *testing.T, run, name string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal("shadow metadata encoding failed")
	}
	path := filepath.Join(run, name)
	f, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal("shadow metadata open failed")
	}
	_, writeErr := f.Write(append(raw, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(path+".tmp", path) != nil {
		t.Fatal("durable shadow metadata write failed")
	}
	d, err := os.Open(run)
	if err != nil {
		t.Fatal("shadow metadata directory open failed")
	}
	defer d.Close()
	if d.Sync() != nil {
		t.Fatal("shadow metadata directory sync failed")
	}
}

func wr23ShadowAdmission(t *testing.T) {
	t.Helper()
	run := os.Getenv("WR23_RESUME_RUN_DIR")
	var spec struct {
		Final json.RawMessage `json:"final_candidate"`
	}
	raw, err := os.ReadFile(filepath.Join(run, "run.json"))
	if err != nil || json.Unmarshal(raw, &spec) != nil || len(spec.Final) == 0 {
		t.Fatal("24h requires a frozen final candidate")
	}
	var final struct {
		Manifest string `json:"manifest_sha256"`
	}
	var admission struct {
		Result   string            `json:"result"`
		Manifest string            `json:"manifest_sha256"`
		Gates    map[string]string `json:"gates"`
	}
	raw, err = os.ReadFile(filepath.Join(run, "shadow-admission.json"))
	if err != nil || json.Unmarshal(raw, &admission) != nil || json.Unmarshal(spec.Final, &final) != nil || final.Manifest == "" || admission.Manifest != final.Manifest || admission.Result != "pass" {
		t.Fatal("24h admission missing or candidate differs")
	}
	for _, gate := range []string{"A", "B", "dlq_inspect_replay", "envoy", "session", "required_aggregate", "fixed_tools_oci_config"} {
		if admission.Gates[gate] != "pass" {
			t.Fatal("24h prerequisite not passed", gate)
		}
	}
}

// Checked before creating an environment or its enforced configuration.
func wr23EnforcementAdmission(t *testing.T) {
	t.Helper()
	run := os.Getenv("WR23_RESUME_RUN_DIR")
	var spec struct {
		Final struct {
			Manifest string `json:"manifest_sha256"`
		} `json:"final_candidate"`
	}
	var admission struct {
		Result   string `json:"result"`
		Manifest string `json:"manifest_sha256"`
		ProofSHA string `json:"shadow_proof_sha256"`
	}
	var proof struct {
		Result      string    `json:"result"`
		Kind        string    `json:"kind"`
		Active      string    `json:"active_24h"`
		Rebuild     string    `json:"full_rebuild"`
		Elapsed     float64   `json:"elapsed_seconds"`
		P99         float64   `json:"p99_seconds"`
		Samples     int       `json:"samples"`
		Gaps        int       `json:"unresolved_tenant_gaps"`
		Differences int       `json:"unexplained_authorization_differences"`
		Started     time.Time `json:"started_at"`
		Finished    time.Time `json:"finished_at"`
		Rebuilt     time.Time `json:"rebuild_activated_at"`
		PostRebuild float64   `json:"post_rebuild_seconds"`
		Quarantined int       `json:"quarantined_events"`
	}
	raw, err := os.ReadFile(filepath.Join(run, "run.json"))
	if err != nil || json.Unmarshal(raw, &spec) != nil || spec.Final.Manifest == "" {
		t.Fatal("enforcement requires final candidate")
	}
	raw, err = os.ReadFile(filepath.Join(run, "enforcement-admission.json"))
	if err != nil || json.Unmarshal(raw, &admission) != nil || admission.Result != "pass" || admission.Manifest != spec.Final.Manifest {
		t.Fatal("enforcement candidate admission absent")
	}
	raw, err = os.ReadFile(filepath.Join(run, "enforcement-shadow-proof.json"))
	if err != nil || hexDigest(raw) != admission.ProofSHA || json.Unmarshal(raw, &proof) != nil {
		t.Fatal("enforcement 24h proof digest differs")
	}
	durationPassed := wr23ObservationDurationPassed(proof.Kind, proof.Active, proof.Elapsed, proof.Samples, proof.Started, proof.Finished, proof.Rebuilt, proof.PostRebuild)
	if proof.Result != "pass" || !durationPassed || proof.Rebuild != "pass" || proof.P99 > 5 || proof.P99 < 0 || proof.Gaps != 0 || proof.Differences != 0 || proof.Quarantined != 0 {
		t.Fatal("enforcement requires the complete approved observation gates")
	}
}

// The user-approved shorter window is explicit and never produces active_24h=pass.
func wr23ObservationDurationPassed(kind, active string, elapsed float64, samples int, started, finished, rebuilt time.Time, post float64) bool {
	if kind == "active24h" {
		return active == "pass" && elapsed >= 86400 && finished.Sub(started) >= 24*time.Hour && samples >= 5760
	}
	return kind == "accepted12h30" && active == "not_verified" && elapsed >= 45000 && finished.Sub(started) >= 12*time.Hour+30*time.Minute && samples >= 3000 && post >= 1800 && !rebuilt.Before(started) && finished.Sub(rebuilt) >= 30*time.Minute
}

func TestWR23ObservationDurationAdmission(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, kind, active string
		elapsed            float64
		samples            int
		finish, rebuild    time.Duration
		post               float64
		want               bool
	}{
		{"approved", "accepted12h30", "not_verified", 45000, 3000, 750 * time.Minute, 720 * time.Minute, 1800, true},
		{"old24h", "active24h", "pass", 86400, 5760, 24 * time.Hour, 12 * time.Hour, 43200, true},
		{"no_fake24h", "accepted12h30", "pass", 45000, 3000, 750 * time.Minute, 720 * time.Minute, 1800, false},
		{"short_elapsed", "accepted12h30", "not_verified", 44999, 3000, 750 * time.Minute, 720 * time.Minute, 1800, false},
		{"short_wall", "accepted12h30", "not_verified", 45000, 3000, 749 * time.Minute, 720 * time.Minute, 1800, false},
		{"small_denominator", "accepted12h30", "not_verified", 45000, 2999, 750 * time.Minute, 720 * time.Minute, 1800, false},
		{"short_post", "accepted12h30", "not_verified", 45000, 3000, 750 * time.Minute, 720 * time.Minute, 1799, false},
		{"late_rebuild", "accepted12h30", "not_verified", 45000, 3000, 750 * time.Minute, 721 * time.Minute, 1800, false},
		{"rebuild_before_start", "accepted12h30", "not_verified", 45000, 3000, 750 * time.Minute, -time.Minute, 1800, false},
		{"unknown_kind", "rehearsal", "not_verified", 45000, 3000, 750 * time.Minute, 720 * time.Minute, 1800, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wr23ObservationDurationPassed(tc.kind, tc.active, tc.elapsed, tc.samples, start, start.Add(tc.finish), start.Add(tc.rebuild), tc.post); got != tc.want {
				t.Fatalf("duration admission got %v want %v", got, tc.want)
			}
		})
	}
}
