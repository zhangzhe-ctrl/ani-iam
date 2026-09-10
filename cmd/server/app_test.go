package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestNewTenantIAMAdminRuntimePinsPolicyRevision(t *testing.T) {
	postgresData := data.NewData(nil)
	ids := data.NewUUIDv7Generator()
	clock := data.NewSystemClock()
	limiter := allowingAppAPIKeyCreationLimiter{}

	adminService, err := newTenantIAMAdminRuntime(postgresData, "sha256:stale", ids, clock, limiter)
	if adminService != nil {
		t.Fatal("newTenantIAMAdminRuntime() returned a service for a stale policy revision")
	}
	var mismatch *biz.AuthorizationPolicyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("newTenantIAMAdminRuntime() error = %v, want AuthorizationPolicyMismatchError", err)
	}

	adminService, err = newTenantIAMAdminRuntime(postgresData, data.TargetPolicyRevision, ids, clock, limiter)
	if err != nil {
		t.Fatalf("newTenantIAMAdminRuntime() error = %v", err)
	}
	if adminService == nil {
		t.Fatal("newTenantIAMAdminRuntime() returned nil service for the pinned policy revision")
	}

	adminService, err = newTenantIAMAdminRuntime(postgresData, data.TargetPolicyRevision, ids, clock, nil)
	if adminService != nil || err == nil {
		t.Fatalf("newTenantIAMAdminRuntime() without API key creation limiter = %#v, %v", adminService, err)
	}
}

type allowingAppAPIKeyCreationLimiter struct{}

func (allowingAppAPIKeyCreationLimiter) Acquire(context.Context, biz.TenantScope, uuid.UUID) error {
	return nil
}

func TestBuildAppFailsClosedWhenSigningKeyIsUnavailable(t *testing.T) {
	bootstrap := buildAppTestBootstrap()

	app, err := buildApp(bootstrap, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if app != nil {
		t.Fatal("buildApp() returned an app without its configured signing key")
	}
	if err == nil || !strings.Contains(err.Error(), "access-token private key") {
		t.Fatalf("buildApp() error = %v, want signing-key failure", err)
	}
}

func TestBuildAppFailsClosedWhenOIDCClientSecretIsUnavailable(t *testing.T) {
	bootstrap := buildAppTestBootstrap()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal signing key: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "access-token-key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatalf("write signing key: %v", err)
	}
	bootstrap.Runtime.AccessToken.PrivateKeyFile = keyPath

	app, err := buildApp(bootstrap, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if app != nil {
		t.Fatal("buildApp() returned an app without its configured OIDC client secret")
	}
	if err == nil || !strings.Contains(err.Error(), "OIDC client secret") {
		t.Fatalf("buildApp() error = %v, want OIDC client-secret failure", err)
	}
}

func buildAppTestBootstrap() *conf.Bootstrap {
	return &conf.Bootstrap{
		Profile: conf.IsolatedProfile,
		Server: &conf.Server{
			Grpc: &conf.Server_GRPC{
				Network: "tcp",
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
				Tls: &conf.Server_GRPC_TLS{
					CertificateFile:      "/nonexistent/ani-iam-dp2-05-server.crt",
					PrivateKeyFile:       "/nonexistent/ani-iam-dp2-05-server.key",
					ClientCaFile:         "/nonexistent/ani-iam-dp2-05-client-ca.crt",
					GatewayClientDnsName: "ani-gateway",
				},
			},
			Admin: &conf.Server_Admin{
				Network: "tcp",
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
			},
			ShutdownTimeout: durationpb.New(time.Second),
		},
		Runtime: &conf.Runtime{
			Environment: "wr17-18-isolated", TrustDomain: "iam.wr17-18.test",
			Postgresql: &conf.PostgreSQL{Dsn: "postgresql://ani_iam_runtime@127.0.0.1:1/ani_iam?sslmode=disable"},
			Redis: &conf.Redis{
				Addr:         "127.0.0.1:1",
				Namespace:    "ani-iam:dp2-05:test",
				LoginLimit:   5,
				LoginWindow:  durationpb.New(15 * time.Minute),
				DialTimeout:  durationpb.New(100 * time.Millisecond),
				ReadTimeout:  durationpb.New(100 * time.Millisecond),
				WriteTimeout: durationpb.New(100 * time.Millisecond),
			},
			AccessToken: &conf.AccessToken{
				Issuer:         "ani-iam",
				ActiveKeyId:    "dp2-05-test",
				PrivateKeyFile: "/nonexistent/ani-iam-dp2-05-ed25519.pem",
			},
			Notification: &conf.Notification{
				Address:              "127.0.0.1:1",
				CertificateFile:      "/nonexistent/ani-iam-notification-client.crt",
				PrivateKeyFile:       "/nonexistent/ani-iam-notification-client.key",
				ServerCaFile:         "/nonexistent/ani-notification-server-ca.crt",
				ServerDnsName:        "ani-notification",
				ConsoleActionUrlBase: "https://console.example.test/password-action",
				Locale:               "en-US",
				DispatchInterval:     durationpb.New(time.Second),
				SubmissionTimeout:    durationpb.New(time.Second),
			},
			Oidc: &conf.OIDC{
				Provider:                "dex",
				IssuerUrl:               "https://dex.example.test/dex",
				ClientId:                "ani-console",
				ClientSecretFile:        "/nonexistent/ani-iam-dex-client-secret",
				LoginRedirectUri:        "https://console.example.test/auth/oidc/callback",
				IdentityLinkRedirectUri: "https://console.example.test/auth/oidc/link/callback",
				RecentReauthentication:  durationpb.New(10 * time.Minute),
				HttpTimeout:             durationpb.New(time.Second),
			},
			PolicyRevision: data.TargetPolicyRevision,
		},
	}
}

func TestPasswordActionNotificationWorkerCancelsInFlightDispatchWithoutLoggingErrorText(t *testing.T) {
	dispatcher := &blockingPasswordActionNotificationDispatcher{
		started: make(chan struct{}),
	}
	var logs bytes.Buffer
	worker, err := newPasswordActionNotificationWorker(
		dispatcher,
		10*time.Millisecond,
		time.Second,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatalf("newPasswordActionNotificationWorker() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Start(context.Background()) }()
	select {
	case <-dispatcher.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start a dispatch")
	}
	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if strings.Contains(logs.String(), "test-only-sensitive-action-token") {
		t.Fatalf("worker logged dispatcher error text: %s", logs.String())
	}
}

func TestPasswordActionNotificationWorkerRequiresBoundedConfiguration(t *testing.T) {
	dispatcher := &blockingPasswordActionNotificationDispatcher{started: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name       string
		dispatcher passwordActionNotificationDispatcher
		interval   time.Duration
		timeout    time.Duration
		logger     *slog.Logger
	}{
		{name: "missing dispatcher", interval: time.Second, timeout: time.Second, logger: logger},
		{name: "missing interval", dispatcher: dispatcher, timeout: time.Second, logger: logger},
		{name: "missing timeout", dispatcher: dispatcher, interval: time.Second, logger: logger},
		{name: "missing logger", dispatcher: dispatcher, interval: time.Second, timeout: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if worker, err := newPasswordActionNotificationWorker(test.dispatcher, test.interval, test.timeout, test.logger); err == nil || worker != nil {
				t.Fatalf("newPasswordActionNotificationWorker() = %#v, %v", worker, err)
			}
		})
	}
}

func TestAPIKeyMaintenanceWorkerFlushesUsageAndPublishesOperationalSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)
	want := biz.APIKeyOperationalSnapshot{
		StaleNonExpiringCount:      7,
		UnusualTenantWorkloadCount: 3,
		ObservedAt:                 now,
	}
	flusher := &recordingAPIKeyUsageFlusher{results: []int{256, 256, 12, 0}}
	reader := &recordingAPIKeyOperationalSnapshotReader{snapshot: want}
	sink := &recordingAPIKeyOperationalSnapshotSink{published: make(chan biz.APIKeyOperationalSnapshot, 1)}
	worker, err := newAPIKeyMaintenanceWorker(
		flusher,
		reader,
		sink,
		fixedAPIKeyMaintenanceClock{now: now},
		time.Hour,
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("newAPIKeyMaintenanceWorker() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Start(context.Background()) }()

	select {
	case got := <-sink.published:
		if got != want {
			t.Fatalf("published snapshot = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not publish its initial operational snapshot")
	}
	if flusher.calls != 4 {
		t.Fatalf("Flush() calls = %d, want 4 to drain every pending batch", flusher.calls)
	}
	if reader.calls != 1 || !reader.observedAt.Equal(now) {
		t.Fatalf("GetAPIKeyOperationalSnapshot() = calls %d, observedAt %v; want 1, %v", reader.calls, reader.observedAt, now)
	}

	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestAPIKeyMaintenanceWorkerCancelsInFlightFlushWithoutLoggingErrorText(t *testing.T) {
	flusher := &blockingAPIKeyUsageFlusher{started: make(chan struct{})}
	reader := &cancelledAPIKeyOperationalSnapshotReader{}
	sink := &recordingAPIKeyOperationalSnapshotSink{published: make(chan biz.APIKeyOperationalSnapshot, 1)}
	var logs bytes.Buffer
	worker, err := newAPIKeyMaintenanceWorker(
		flusher,
		reader,
		sink,
		fixedAPIKeyMaintenanceClock{now: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		time.Hour,
		time.Second,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatalf("newAPIKeyMaintenanceWorker() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Start(context.Background()) }()
	select {
	case <-flusher.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start a usage flush")
	}

	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if strings.Contains(logs.String(), "test-only-sensitive-api-key") {
		t.Fatalf("worker logged flusher error text: %s", logs.String())
	}
	select {
	case snapshot := <-sink.published:
		t.Fatalf("worker published snapshot after cancellation: %#v", snapshot)
	default:
	}
}

func TestAPIKeyMaintenanceWorkerBoundsBusyDrainAndStillRefreshesSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 9, 1, 3, 0, 0, time.UTC)
	flusher := &alwaysPendingAPIKeyUsageFlusher{}
	reader := &recordingAPIKeyOperationalSnapshotReader{snapshot: biz.APIKeyOperationalSnapshot{ObservedAt: now}}
	sink := &recordingAPIKeyOperationalSnapshotSink{published: make(chan biz.APIKeyOperationalSnapshot, 1)}
	worker, err := newAPIKeyMaintenanceWorker(
		flusher,
		reader,
		sink,
		fixedAPIKeyMaintenanceClock{now: now},
		time.Hour,
		10*time.Millisecond,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("newAPIKeyMaintenanceWorker() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Start(context.Background()) }()

	select {
	case snapshot := <-sink.published:
		if snapshot.ObservedAt != now {
			t.Fatalf("published snapshot = %#v", snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("busy drain exceeded its deadline or blocked snapshot refresh")
	}
	if flusher.calls < 2 || reader.calls != 1 {
		t.Fatalf("busy drain/snapshot calls = %d/%d, want multiple/1", flusher.calls, reader.calls)
	}

	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestAPIKeyMaintenanceWorkerRequiresBoundedConfiguration(t *testing.T) {
	flusher := &recordingAPIKeyUsageFlusher{}
	reader := &recordingAPIKeyOperationalSnapshotReader{}
	sink := &recordingAPIKeyOperationalSnapshotSink{published: make(chan biz.APIKeyOperationalSnapshot, 1)}
	clock := fixedAPIKeyMaintenanceClock{now: time.Now()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name     string
		flusher  apiKeyUsageFlusher
		reader   biz.APIKeyOperationalSnapshotReader
		sink     apiKeyOperationalSnapshotSink
		clock    biz.Clock
		interval time.Duration
		timeout  time.Duration
		logger   *slog.Logger
	}{
		{name: "missing flusher", reader: reader, sink: sink, clock: clock, interval: time.Second, timeout: time.Second, logger: logger},
		{name: "missing reader", flusher: flusher, sink: sink, clock: clock, interval: time.Second, timeout: time.Second, logger: logger},
		{name: "missing sink", flusher: flusher, reader: reader, clock: clock, interval: time.Second, timeout: time.Second, logger: logger},
		{name: "missing clock", flusher: flusher, reader: reader, sink: sink, interval: time.Second, timeout: time.Second, logger: logger},
		{name: "missing interval", flusher: flusher, reader: reader, sink: sink, clock: clock, timeout: time.Second, logger: logger},
		{name: "missing timeout", flusher: flusher, reader: reader, sink: sink, clock: clock, interval: time.Second, logger: logger},
		{name: "missing logger", flusher: flusher, reader: reader, sink: sink, clock: clock, interval: time.Second, timeout: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker, err := newAPIKeyMaintenanceWorker(test.flusher, test.reader, test.sink, test.clock, test.interval, test.timeout, test.logger)
			if worker != nil || err == nil {
				t.Fatalf("newAPIKeyMaintenanceWorker() = %#v, %v", worker, err)
			}
		})
	}
}

type recordingAPIKeyUsageFlusher struct {
	results []int
	calls   int
}

type blockingAPIKeyUsageFlusher struct {
	started chan struct{}
}

type alwaysPendingAPIKeyUsageFlusher struct {
	calls int
}

func (f *alwaysPendingAPIKeyUsageFlusher) Flush(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
		f.calls++
		return apiKeyUsageBatchSize, nil
	}
}

func (f *blockingAPIKeyUsageFlusher) Flush(ctx context.Context) (int, error) {
	close(f.started)
	<-ctx.Done()
	return 0, errors.New("test-only-sensitive-api-key")
}

func (f *recordingAPIKeyUsageFlusher) Flush(context.Context) (int, error) {
	result := 0
	if f.calls < len(f.results) {
		result = f.results[f.calls]
	}
	f.calls++
	return result, nil
}

type recordingAPIKeyOperationalSnapshotReader struct {
	snapshot   biz.APIKeyOperationalSnapshot
	calls      int
	observedAt time.Time
}

func (r *recordingAPIKeyOperationalSnapshotReader) GetAPIKeyOperationalSnapshot(_ context.Context, observedAt time.Time) (biz.APIKeyOperationalSnapshot, error) {
	r.calls++
	r.observedAt = observedAt
	return r.snapshot, nil
}

type cancelledAPIKeyOperationalSnapshotReader struct{}

func (cancelledAPIKeyOperationalSnapshotReader) GetAPIKeyOperationalSnapshot(ctx context.Context, _ time.Time) (biz.APIKeyOperationalSnapshot, error) {
	return biz.APIKeyOperationalSnapshot{}, ctx.Err()
}

type recordingAPIKeyOperationalSnapshotSink struct {
	published chan biz.APIKeyOperationalSnapshot
}

func (s *recordingAPIKeyOperationalSnapshotSink) SetAPIKeyOperationalSnapshot(snapshot biz.APIKeyOperationalSnapshot) error {
	s.published <- snapshot
	return nil
}

type fixedAPIKeyMaintenanceClock struct {
	now time.Time
}

func (c fixedAPIKeyMaintenanceClock) Now() time.Time {
	return c.now
}

type blockingPasswordActionNotificationDispatcher struct {
	started chan struct{}
}

func (d *blockingPasswordActionNotificationDispatcher) DispatchNext(ctx context.Context) (bool, error) {
	select {
	case <-d.started:
	default:
		close(d.started)
	}
	<-ctx.Done()
	return true, errors.New("test-only-sensitive-action-token")
}
