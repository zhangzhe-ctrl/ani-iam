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
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func TestCoreSnapshotHTTPStableCursorAndFailureBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../registrations/workload-targets.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	pki := newNotificationClientTestPKI(t, "snapshot-owner.test", "snapshot-reader.test")
	cursor := coreSnapshotCursorDocument{ID: uuid.NewString(), SourceSequence: 10, BrokerSequence: 15, PageSize: 2, ExpiresAt: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	tenant, _ := uuid.NewV7()
	consumer, _ := uuid.NewV7()
	var mode atomic.Int32
	var calls atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		target, ok := registry.HTTP("ani-core-control", r.Method, r.URL.Path)
		if !ok || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || r.TLS.PeerCertificates[0].DNSNames[0] != "snapshot-reader.test" || r.Header.Get(grpcworkload.WorkloadHTTPTokenHeader) != "current-wat" || r.Header.Get(grpcworkload.WorkloadHTTPRevisionHeader) != registry.Revision(target.Audience, target.Operation) {
			t.Error("missing exact authenticated request")
			w.WriteHeader(403)
			return
		}
		switch mode.Load() {
		case 1:
			w.WriteHeader(403)
			return
		case 2:
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"code":"SNAPSHOT_EXPIRED"}`))
			return
		case 3:
			w.WriteHeader(503)
			return
		case 4:
			w.Header().Set("Location", "https://other.test")
			w.WriteHeader(307)
			return
		case 5:
			_, _ = w.Write([]byte(`{"unexpected":true}`))
			return
		}
		c := cursor
		if mode.Load() == 6 {
			c.SourceSequence++
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			var in struct {
				Key  string `json:"idempotency_key"`
				Size int32  `json:"page_size"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.Key != "018fa9de-8324-7000-8000-000000000004" || in.Size != 2 {
				t.Error("changed request parameters")
			}
			_ = json.NewEncoder(w).Encode(c)
			return
		}
		if r.URL.Query().Get("cursor_id") != cursor.ID {
			t.Error("cursor escaped")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cursor": c, "items": []any{map[string]any{"tenant_id": tenant.String(), "status": "active", "lifecycle_version": 1}}, "next_page_token": ""})
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.clientCAs}
	server.StartTLS()
	defer server.Close()
	config := CoreSnapshotHTTPConfiguration{Origin: server.URL, ServerName: "snapshot-owner.test", Producer: "core.test", ConsumerID: consumer, Registry: registry, TLS: grpcworkload.TLSFiles{CertificateFile: pki.clientCertificateFile, PrivateKeyFile: pki.clientPrivateKeyFile, CAFile: pki.serverCAFile}, TokenSource: func(_ context.Context, target grpcworkload.HTTPWorkloadTarget) (string, error) {
		if target.Audience != "ani-core-control" {
			t.Error("wrong target")
		}
		return "current-wat", nil
	}}
	client, closeClient, err := NewCoreSnapshotHTTPClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	ctx := context.Background()
	cut, err := client.Begin(ctx, "018fa9de-8324-7000-8000-000000000004", 2)
	if err != nil {
		t.Fatal(err)
	}
	if cut.SourceCut != 10 || cut.BrokerAfter != 15 || cut.ConsumerID != consumer {
		t.Fatal("cut mapping changed")
	}
	page, err := client.Page(ctx, cut, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].TenantID != tenant {
		t.Fatalf("page: %+v %v", page, err)
	}
	for _, tc := range []struct {
		mode     int32
		expected error
	}{{1, biz.ErrWorkloadPermissionDenied}, {2, biz.ErrCoreSnapshotExpired}, {3, biz.ErrPersistenceUnavailable}, {4, biz.ErrPersistenceUnavailable}, {5, biz.ErrCoreProjectionInvalid}, {6, biz.ErrCoreProjectionConflict}} {
		mode.Store(tc.mode)
		before := calls.Load()
		if _, err := client.Page(ctx, cut, ""); !errors.Is(err, tc.expected) {
			t.Fatalf("mode %d: %v", tc.mode, err)
		}
		if calls.Load() != before+1 {
			t.Fatal("request retried or redirected")
		}
	}
	mode.Store(0)
	if _, err := client.Page(ctx, cut, ""); err != nil {
		t.Fatal("same cut recovery", err)
	}
	other := cut
	other.ConsumerID, _ = uuid.NewV7()
	before := calls.Load()
	if _, err := client.Page(ctx, other, ""); !errors.Is(err, biz.ErrCoreProjectionInvalid) {
		t.Fatal("consumer binding accepted", err)
	}
	if calls.Load() != before {
		t.Fatal("invalid cursor left client")
	}
	config.ServerName = "wrong.test"
	wrong, closeWrong, err := NewCoreSnapshotHTTPClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWrong()
	if _, err := wrong.Begin(ctx, "018fa9de-8324-7000-8000-000000000004", 2); !errors.Is(err, biz.ErrPersistenceUnavailable) {
		t.Fatal("wrong TLS owner accepted", err)
	}
}
