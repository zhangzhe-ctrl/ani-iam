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
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWR23ResumeFormalSnapshot(t *testing.T) {
	started := time.Now().UTC()
	t.Setenv("WR23_SNAPSHOT_READER_BOUNDARY", "1")
	e := newWR23FormalEnvironment(t)
	b := e.boss
	ctx := context.Background()
	browser := e.firstAdministrator(t)
	access := e.access(t, browser)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, doc, _ := b.request(t, browser, method, path, access, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		if status != want {
			t.Fatalf("formal Snapshot owner %s %s status=%d reason=%v want=%d", method, path, status, doc["code"], want)
		}
		return doc
	}
	member := call("GET", "/iam/platform/members?limit=1", nil, 200)["items"].([]any)[0].(map[string]any)
	role := call("POST", "/iam/platform/roles", map[string]any{"name": "WR23 Snapshot Core manager", "permissions": []string{"tenants/read", "tenants/create", "tenants/update"}}, 201)
	call("POST", "/iam/platform/members/"+member["membership_id"].(string)+"/role-bindings", map[string]any{"role_id": role["role_id"], "expected_membership_version": member["version"]}, 204)
	create := func() string {
		t.Helper()
		return call("POST", "/admin/tenants", map[string]any{"name": "wr23-snapshot-" + uuid.NewString()[:12], "display_name": "WR23 Snapshot", "email": "contact@example.test", "plan_id": e.plan, "admin_email": "snapshot-" + mustV7(t).String() + "@example.test", "admin_locale": "en-US", "idempotency_key": uuid.NewString()}, 200)["id"].(string)
	}
	tenants := []string{create(), create(), create()}
	wait := func(label string, check func() bool) {
		t.Helper()
		until := time.Now().Add(25 * time.Second)
		for time.Now().Before(until) {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(label)
	}
	matches := func(id, status string, version int64) bool {
		var matched bool
		return b.owner.QueryRow(ctx, `SELECT status=$2 AND lifecycle_version=$3 AND NOT repair_required AND fresh_until>clock_timestamp() FROM core_current_lifecycle_facts WHERE producer=$4 AND tenant_id=$1`, id, status, version, e.broker.configuration.Authority.Producer).Scan(&matched) == nil && matched
	}
	wait("initial formal projections unavailable", func() bool {
		return matches(tenants[0], "active", 1) && matches(tenants[1], "active", 1) && matches(tenants[2], "active", 1)
	})
	var c struct {
		Snapshot struct {
			Origin          string `json:"origin"`
			ServerName      string `json:"server_name"`
			IAMAddress      string `json:"iam_address"`
			IAMServerName   string `json:"iam_server_name"`
			CertificateFile string `json:"certificate_file"`
			PrivateKeyFile  string `json:"private_key_file"`
			CAFile          string `json:"ca_file"`
		} `json:"snapshot"`
	}
	raw, err := os.ReadFile(b.iam.config.Runtime.CoreLifecycleFile)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		t.Fatal("read own Snapshot configuration")
	}
	files := grpcworkload.TLSFiles{CertificateFile: c.Snapshot.CertificateFile, PrivateKeyFile: c.Snapshot.PrivateKeyFile, CAFile: c.Snapshot.CAFile}
	registry := wr32Registry(t)
	a := e.broker.configuration.Authority
	source := func(files grpcworkload.TLSFiles) (biz.CoreSnapshotSource, *grpcworkload.WorkloadOnlyClient) {
		t.Helper()
		identity, err := grpcworkload.NewWorkloadOnlyClient(grpcworkload.ClientConfig{Registry: registry, Address: c.Snapshot.IAMAddress, ServerName: c.Snapshot.IAMServerName, Environment: a.Environment, TrustDomain: a.TrustDomain, TLS: files, Timeout: 2 * time.Second})
		if err != nil {
			t.Fatal("actual reader client construction")
		}
		t.Cleanup(func() { _ = identity.Close() })
		client, closeHTTP, err := data.NewCoreSnapshotHTTPClient(data.CoreSnapshotHTTPConfiguration{Origin: c.Snapshot.Origin, ServerName: c.Snapshot.ServerName, Producer: a.Producer, ConsumerID: a.ConsumerID, TLS: files, Registry: registry, TokenSource: identity.HTTPTokenSource})
		if err != nil {
			t.Fatal("actual owner client construction")
		}
		t.Cleanup(closeHTTP)
		return client, identity
	}
	first, identity := source(files)
	second, _ := source(e.snapshotSecondTLS)
	const path = "/api/v1/internal/tenant-lifecycle/snapshots"
	roots := x509.NewCertPool()
	ca, err := os.ReadFile(files.CAFile)
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		t.Fatal("own Snapshot CA")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: c.Snapshot.ServerName, RootCAs: roots}
	rawTransport := &http.Transport{TLSClientConfig: tlsConfig}
	t.Cleanup(rawTransport.CloseIdleConnections)
	rawClient := &http.Client{Transport: rawTransport, Timeout: 3 * time.Second}
	request := func(headers map[string]string) (int, error) {
		req, _ := http.NewRequestWithContext(ctx, "POST", c.Snapshot.Origin+path, bytes.NewBufferString(`{"idempotency_key":"`+uuid.NewString()+`","page_size":1}`))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		response, err := rawClient.Do(req)
		if err != nil {
			return 0, err
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
		return response.StatusCode, nil
	}
	if _, err = request(nil); err == nil {
		t.Fatal("Snapshot accepted connection without client certificate")
	}
	cert, err := tls.LoadX509KeyPair(files.CertificateFile, files.PrivateKeyFile)
	if err != nil {
		t.Fatal("own reader TLS")
	}
	rawTransport.CloseIdleConnections()
	tlsConfig = tlsConfig.Clone()
	tlsConfig.Certificates = []tls.Certificate{cert}
	rawTransport = &http.Transport{TLSClientConfig: tlsConfig}
	t.Cleanup(rawTransport.CloseIdleConnections)
	rawClient.Transport = rawTransport
	for _, headers := range []map[string]string{nil, {"X-ANI-Principal-ID": mustV7(t).String(), "X-ANI-Target-Revision": registry.Revision("ani-core-control", "core.snapshot.begin")}} {
		if status, err := request(headers); err != nil || status != 401 {
			t.Fatalf("Snapshot missing WAT or forged identity header status=%d", status)
		}
	}
	target, err := grpcworkload.RegisteredHTTPWorkloadTarget(registry, "ani-core-control", "POST", path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := identity.HTTPTokenSource(ctx, target)
	if err != nil {
		t.Fatal("actual read-only WAT issuance")
	}
	if status, err := request(map[string]string{grpcworkload.WorkloadHTTPTokenHeader: token, grpcworkload.WorkloadHTTPRevisionHeader: strings.Repeat("0", 64)}); err != nil || status != 401 {
		t.Fatalf("Snapshot stale target revision status=%d", status)
	}
	// Exercise all four current Grant seams through the actual HTTPS owner.
	// Only these fresh test identities are faulted; each restore uses a new version.
	grantCases := []struct{ name, identity, audience, operation string }{
		{"caller_issue_entry", "snapshot-reader." + a.TrustDomain, "ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"},
		{"caller_exact_target", "snapshot-reader." + a.TrustDomain, "ani-core-control", "core.snapshot.begin"},
		{"receiver_verify_entry", c.Snapshot.ServerName, "ani-iam", "/iam.v1.AuthorizationService/VerifyWorkloadCaller"},
		{"receiver_same_audience", c.Snapshot.ServerName, "ani-core-control", "core.snapshot.receive"},
	}
	grantProof := []map[string]any{}
	for _, fault := range grantCases {
		var id uuid.UUID
		var version int64
		if b.owner.QueryRow(ctx, `SELECT g.id,g.version FROM workload_grants g JOIN workload_identity_bindings i ON i.principal_id=g.principal_id AND i.environment=g.environment AND i.trust_domain=g.trust_domain WHERE i.identity_value=$1 AND g.environment=$2 AND g.trust_domain=$3 AND g.audience=$4 AND g.operation=$5 AND g.status='active'`, fault.identity, a.Environment, a.TrustDomain, fault.audience, fault.operation).Scan(&id, &version) != nil {
			t.Fatal("exact HTTP Grant fault target missing")
		}
		frozenToken, err := identity.HTTPTokenSource(ctx, target)
		if err != nil {
			t.Fatal("issue pre-fault WAT")
		}
		changed, err := b.owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND version=$2 AND status='active'`, id, version)
		if err != nil || changed.RowsAffected() != 1 {
			t.Fatal("HTTP Grant revoke fault CAS")
		}
		denied := 0
		if fault.name != "caller_issue_entry" {
			denied, err = request(map[string]string{grpcworkload.WorkloadHTTPTokenHeader: frozenToken, grpcworkload.WorkloadHTTPRevisionHeader: registry.Revision("ani-core-control", "core.snapshot.begin")})
			if err != nil || (denied != 401 && denied != 403) {
				t.Fatalf("HTTP current Grant %s denial status=%d", fault.name, denied)
			}
		}
		// The accepted WR32 contract checks the Issue entry when minting WAT.
		// Receiver calls independently recheck the current caller target Grant.
		if strings.HasPrefix(fault.name, "caller_") {
			if _, issuanceErr := identity.HTTPTokenSource(ctx, target); status.Code(issuanceErr) != codes.PermissionDenied {
				t.Fatal("current revoked caller Grant did not deny WAT issuance")
			}
		}
		changed, err = b.owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND version=$2 AND status='revoked'`, id, version+1)
		if err != nil || changed.RowsAffected() != 1 {
			t.Fatal("HTTP Grant restore fault CAS")
		}
		if fault.name == "caller_exact_target" {
			status, err := request(map[string]string{grpcworkload.WorkloadHTTPTokenHeader: frozenToken, grpcworkload.WorkloadHTTPRevisionHeader: registry.Revision("ani-core-control", "core.snapshot.begin")})
			if err != nil || (status != 401 && status != 403) {
				t.Fatal("old WAT survived exact target Grant version change")
			}
		}
		fresh, err := identity.HTTPTokenSource(ctx, target)
		if err != nil {
			t.Fatal("fresh WAT after explicit restore")
		}
		status, err := request(map[string]string{grpcworkload.WorkloadHTTPTokenHeader: fresh, grpcworkload.WorkloadHTTPRevisionHeader: registry.Revision("ani-core-control", "core.snapshot.begin")})
		if err != nil || status != 200 {
			t.Fatalf("fresh current HTTP authority %s status=%d", fault.name, status)
		}
		proof := map[string]any{"seam": fault.name, "restored_version": version + 2, "fresh_current_call": true}
		if fault.name == "caller_issue_entry" {
			proof["issuer_denial_code"] = "PermissionDenied"
		} else {
			proof["http_denial_status"] = denied
		}
		grantProof = append(grantProof, proof)
	}
	key := uuid.NewString()
	cursor, err := first.Begin(ctx, key, 1)
	if err != nil {
		t.Fatal("actual authenticated Snapshot Begin", err)
	}
	again, err := first.Begin(ctx, key, 1)
	// RFC3339 offsets may differ; cursor identity binds the exact instant.
	again.ExpiresAt = again.ExpiresAt.UTC()
	cursor.ExpiresAt = cursor.ExpiresAt.UTC()
	if err != nil || again != cursor {
		t.Fatalf("owner cursor idempotency err=%v first=%+v repeated=%+v", err, cursor, again)
	}
	if _, err = first.Begin(ctx, key, 2); !errors.Is(err, biz.ErrCoreProjectionConflict) {
		t.Fatal("owner accepted idempotency key with changed page size")
	}
	page, err := first.Page(ctx, cursor, "")
	if err != nil || len(page.Items) != 1 || page.NextToken == "" {
		t.Fatal("actual bound first page", err)
	}
	repeated, err := first.Page(ctx, cursor, "")
	if err != nil || !reflect.DeepEqual(page, repeated) {
		t.Fatal("owner page retry changed durable token")
	}
	if _, err = second.Page(ctx, cursor, ""); !errors.Is(err, biz.ErrCoreProjectionInvalid) {
		t.Fatal("second authorized reader reused first reader cursor")
	}
	another, err := first.Begin(ctx, uuid.NewString(), 1)
	if err != nil {
		t.Fatal("second real cursor")
	}
	if _, err = first.Page(ctx, another, page.NextToken); !errors.Is(err, biz.ErrCoreProjectionInvalid) {
		t.Fatal("cross-cursor page token accepted")
	}
	var oldGeneration string
	if b.owner.QueryRow(ctx, `SELECT generation_id::text FROM core_lifecycle_pipelines WHERE producer=$1`, a.Producer).Scan(&oldGeneration) != nil {
		t.Fatal("read current generation")
	}
	// Hold only this run's owner page-token relation. The formal owner materializes
	// its real cut while the daemon's page request waits; no projection is seeded.
	hold, err := e.core.Begin(ctx)
	if err != nil {
		t.Fatal("own bounded page lock")
	}
	defer hold.Rollback(ctx)
	if _, err = hold.Exec(ctx, `SET LOCAL idle_in_transaction_session_timeout='20s'; LOCK core_lifecycle_snapshot_pages IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal("hold own page boundary")
	}
	configFile := filepath.Join(b.iam.directory, "runtime.json")
	configRaw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal("read exact runtime configuration")
	}
	sum := sha256.Sum256(configRaw)
	command := exec.Command(b.iam.binary, "begin-core-snapshot", "--config-file", configFile, "--approved-config-sha256", hex.EncodeToString(sum[:]), "--request-key", uuid.NewString(), "--page-size", "1")
	proof := wr23PrivateCommand(t, b.run, "formal-snapshot-begin", command)
	var build struct {
		Snapshot   string `json:"snapshot_id"`
		Generation string `json:"generation_id"`
		SourceCut  int64  `json:"source_cut"`
		State      string `json:"state"`
	}
	if json.Unmarshal(proof, &build) != nil || build.State != "loading" || build.Generation == oldGeneration {
		t.Fatal("maintenance entry did not retain a fresh owner cut")
	}
	call("POST", "/admin/tenants/"+tenants[0]+"/freeze", map[string]any{"version": 1, "idempotency_key": uuid.NewString()}, 200)
	tenants = append(tenants, create())
	wait("concurrent real broker increments did not advance prior generation", func() bool { return matches(tenants[0], "frozen", 2) && matches(tenants[3], "active", 1) })
	var beforeActivation string
	if b.owner.QueryRow(ctx, `SELECT generation_id::text FROM core_lifecycle_pipelines WHERE producer=$1`, a.Producer).Scan(&beforeActivation) != nil || beforeActivation != oldGeneration {
		t.Fatal("partial Snapshot became current")
	}
	if err = hold.Rollback(ctx); err != nil {
		t.Fatal("release own page boundary")
	}
	wait("formal daemon did not catch up and atomically activate full rebuild", func() bool {
		var ready bool
		return b.owner.QueryRow(ctx, `SELECT p.generation_id=$2 AND b.state='activated' AND p.contiguous_sequence=p.highest_sequence AND b.applied_through>=b.source_cut FROM core_lifecycle_pipelines p JOIN core_lifecycle_rebuilds b ON b.producer=p.producer AND b.generation_id=p.generation_id WHERE p.producer=$1`, a.Producer, build.Generation).Scan(&ready) == nil && ready && matches(tenants[0], "frozen", 2) && matches(tenants[1], "active", 1) && matches(tenants[2], "active", 1) && matches(tenants[3], "active", 1)
	})
	// Real disconnection and real wall time cross the unchanged 30-second gate.
	// The test runner owns and labels this broker; always resume before cleanup.
	brokerID := e.broker.container.GetContainerID()
	if exec.Command("docker", "pause", brokerID).Run() != nil {
		t.Fatal("pause own broker")
	}
	paused := true
	defer func() {
		if paused {
			_ = exec.Command("docker", "unpause", brokerID).Run()
		}
	}()
	pausedAt := time.Now().UTC()
	call("POST", "/admin/tenants/"+tenants[1]+"/freeze", map[string]any{"version": 1, "idempotency_key": uuid.NewString()}, 200)
	time.Sleep(32 * time.Second)
	var stale bool
	if b.owner.QueryRow(ctx, `SELECT fresh_until<=clock_timestamp() FROM core_current_lifecycle_facts WHERE producer=$1 AND tenant_id=$2`, a.Producer, tenants[1]).Scan(&stale) != nil || !stale {
		t.Fatal("real pipeline did not become stale after 30 seconds")
	}
	if exec.Command("docker", "unpause", brokerID).Run() != nil {
		t.Fatal("resume own broker")
	}
	paused = false
	wait("actual producer backlog and consumer reconnect did not recover", func() bool { return matches(tenants[1], "frozen", 2) })
	var gapFree bool
	wait("unresolved real source gap after reconnect", func() bool {
		return b.owner.QueryRow(ctx, `SELECT contiguous_sequence=highest_sequence FROM core_lifecycle_pipelines WHERE producer=$1`, a.Producer).Scan(&gapFree) == nil && gapFree
	})
	result, _ := json.MarshalIndent(map[string]any{"result": "pass", "started_at": started, "finished_at": time.Now().UTC(), "snapshot_id": build.Snapshot, "generation_id": build.Generation, "source_cut": build.SourceCut, "tenant_count": len(tenants), "real_mtls_wat": true, "current_http_grant_revocations": grantProof, "reader_bound_cursor": true, "cross_cursor_token_denied": true, "concurrent_owner_writes": true, "no_partial_activation": true, "full_rebuild_and_increment_catchup": true, "broker_paused_at": pausedAt, "real_30s_stale": true, "backlog_reconnect_gap_free": gapFree, "grade": "formal owner and IAM daemon, no seeded projection or fake owner"}, "", "  ")
	if os.WriteFile(filepath.Join(b.run, "formal-snapshot-results.json"), append(result, '\n'), 0600) != nil {
		t.Fatal("write Snapshot proof")
	}
}
