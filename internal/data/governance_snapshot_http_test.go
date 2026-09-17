package data

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func TestGovernanceSnapshotUsesPublicContractAndRealTLS(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	doc := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: []workloadregistry.Target{
		{Audience: governancev1.Audience, Operation: "governance.receive", Mechanism: workloadregistry.Receiver, GrantScope: "receive", Enabled: true},
		{Audience: governancev1.Audience, Operation: governancev1.SnapshotBeginOperation, HTTPMethod: "POST", HTTPPath: governancev1.SnapshotBeginPath, Mechanism: workloadregistry.WorkloadOnly, GrantScope: "snapshot_begin", ReceiverOperation: "governance.receive", Enabled: true},
		{Audience: governancev1.Audience, Operation: governancev1.SnapshotPageOperation, HTTPMethod: "POST", HTTPPath: governancev1.SnapshotPagePath, Mechanism: workloadregistry.WorkloadOnly, GrantScope: "snapshot_page", ReceiverOperation: "governance.receive", Enabled: true},
	}}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	pki := newNotificationClientTestPKI(t, "snapshot-owner.test", "snapshot-reader.test")
	reader, tenant := id(), id()
	now := time.Now().UTC().Truncate(time.Microsecond)
	cursor := governancev1.SnapshotBeginResponse{SnapshotId: id().String(), Epoch: id().String(), Watermark: 10, CapturedAt: now, ExpiresAt: now.Add(5 * time.Minute), TotalCount: 1, Cursor: strings.Repeat("a", 43)}
	var mode atomic.Int32
	var calls atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		target, ok := registry.HTTP(governancev1.Audience, r.Method, r.URL.Path)
		if !ok || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || r.TLS.Version != tls.VersionTLS13 || r.TLS.PeerCertificates[0].DNSNames[0] != "snapshot-reader.test" || r.Header.Get(grpcworkload.WorkloadHTTPTokenHeader) != "current-wat" || r.Header.Get(grpcworkload.WorkloadHTTPRevisionHeader) != registry.Revision(target.Audience, target.Operation) || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("wrong authenticated Snapshot request")
			w.WriteHeader(403)
			return
		}
		switch mode.Load() {
		case 1:
			w.WriteHeader(403)
			return
		case 2:
			w.WriteHeader(410)
			return
		case 3:
			w.WriteHeader(503)
			return
		case 4:
			w.Header().Set("Location", "https://other.test")
			w.WriteHeader(307)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mode.Load() == 5 {
			_, _ = w.Write([]byte(`{"unexpected":true}`))
			return
		}
		if r.URL.Path == governancev1.SnapshotBeginPath {
			var input governancev1.SnapshotBeginRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.PageSize == nil || *input.PageSize != 2 || r.Header.Get("Idempotency-Key") != "stable-key" {
				t.Error("Begin intent or idempotency changed")
			}
			_ = json.NewEncoder(w).Encode(cursor)
			return
		}
		var input governancev1.SnapshotPageRequest
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Cursor != cursor.Cursor || r.Header.Get("Idempotency-Key") != "" {
			t.Error("Page cursor changed")
		}
		response := governancev1.SnapshotPageResponse{SnapshotId: cursor.SnapshotId, Epoch: cursor.Epoch, Watermark: cursor.Watermark, Items: []governancev1.SnapshotItem{{TenantId: tenant.String(), TenantVersion: 7, BusinessStatus: governancev1.Frozen}}, NextCursor: ""}
		if mode.Load() == 6 {
			response.Watermark++
		}
		if mode.Load() == 7 {
			response.Epoch = id().String()
		}
		if mode.Load() == 8 {
			response.Items = append(response.Items, response.Items[0])
		}
		if mode.Load() == 9 {
			response.NextCursor = input.Cursor
		}
		encoded, _ := json.Marshal(response)
		if mode.Load() == 10 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(encoded, &fields) != nil {
				t.Error("invalid test response")
			}
			delete(fields, "watermark")
			encoded, _ = json.Marshal(fields)
		}
		if mode.Load() == 11 {
			encoded = []byte(strings.Replace(string(encoded), `"next_cursor":""`, `"next_cursor":"","next_cursor":""`, 1))
		}
		_, _ = w.Write(encoded)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.clientCAs}
	server.StartTLS()
	defer server.Close()
	config := GovernanceSnapshotHTTPConfiguration{Origin: server.URL, ServerName: "snapshot-owner.test", ReaderID: reader, Registry: registry, TLS: grpcworkload.TLSFiles{CertificateFile: pki.clientCertificateFile, PrivateKeyFile: pki.clientPrivateKeyFile, CAFile: pki.serverCAFile}, TokenSource: func(_ context.Context, target grpcworkload.HTTPWorkloadTarget) (string, error) {
		if target.Audience != governancev1.Audience {
			t.Error("wrong Snapshot audience")
		}
		return "current-wat", nil
	}}
	client, closeClient, err := NewGovernanceSnapshotHTTPClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	cut, err := client.Begin(context.Background(), "stable-key", 2)
	if err != nil || cut.ReaderID != reader || cut.Epoch.String() != cursor.Epoch || cut.Watermark != 10 || cut.TotalCount != 1 {
		t.Fatal("cut mapping", err)
	}
	page, err := client.Page(context.Background(), cut, cut.FirstToken)
	if err != nil || len(page.Items) != 1 || page.Items[0].TenantID != tenant || page.Items[0].Version != 7 || page.Items[0].Status != "frozen" {
		t.Fatal("page mapping", err)
	}
	for _, tc := range []struct {
		mode int32
		want error
	}{{1, biz.ErrWorkloadPermissionDenied}, {2, biz.ErrTenantSnapshotExpired}, {3, biz.ErrPersistenceUnavailable}, {4, biz.ErrPersistenceUnavailable}, {5, biz.ErrTenantLifecycleInvalid}, {6, biz.ErrTenantLifecycleInvalid}, {7, biz.ErrTenantLifecycleInvalid}, {8, biz.ErrTenantLifecycleInvalid}, {9, biz.ErrTenantLifecycleInvalid}, {10, biz.ErrTenantLifecycleInvalid}, {11, biz.ErrTenantLifecycleInvalid}} {
		mode.Store(tc.mode)
		before := calls.Load()
		if _, err := client.Page(context.Background(), cut, cut.FirstToken); !errors.Is(err, tc.want) {
			t.Fatalf("mode %d: %v", tc.mode, err)
		}
		if calls.Load() != before+1 {
			t.Fatal("Snapshot request retried or redirected")
		}
	}
	mode.Store(0)
	if _, err := client.Page(context.Background(), cut, cut.FirstToken); err != nil {
		t.Fatal("same cut recovery", err)
	}
	other := cut
	other.ReaderID = id()
	before := calls.Load()
	if _, err := client.Page(context.Background(), other, other.FirstToken); !errors.Is(err, biz.ErrTenantLifecycleInvalid) || calls.Load() != before {
		t.Fatal("reader mismatch left client")
	}
	expired := cut
	expired.CapturedAt = now.Add(-10 * time.Minute)
	expired.ExpiresAt = now.Add(-5 * time.Minute)
	if _, err := client.Page(context.Background(), expired, expired.FirstToken); !errors.Is(err, biz.ErrTenantSnapshotExpired) {
		t.Fatal("expired cut accepted", err)
	}
	config.Origin = "https://governance.iam-gov.svc.cluster.local:9443"
	separate, closeSeparate, err := NewGovernanceSnapshotHTTPClient(config)
	if err != nil || separate == nil {
		t.Fatal("non-loopback deployment rejected", err)
	}
	closeSeparate()
	config.Origin = server.URL
	config.ServerName = "wrong.test"
	wrong, closeWrong, err := NewGovernanceSnapshotHTTPClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWrong()
	if _, err := wrong.Begin(context.Background(), "stable-key", 2); !errors.Is(err, biz.ErrPersistenceUnavailable) {
		t.Fatal("wrong server TLS identity accepted", err)
	}
}
