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

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestNewTenantIAMAdminRuntimePinsPolicyRevision(t *testing.T) {
	postgresData := data.NewData(nil)
	ids := data.NewUUIDv7Generator()
	clock := data.NewSystemClock()

	adminService, err := newTenantIAMAdminRuntime(postgresData, "sha256:stale", ids, clock)
	if adminService != nil {
		t.Fatal("newTenantIAMAdminRuntime() returned a service for a stale policy revision")
	}
	var mismatch *biz.AuthorizationPolicyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("newTenantIAMAdminRuntime() error = %v, want AuthorizationPolicyMismatchError", err)
	}

	adminService, err = newTenantIAMAdminRuntime(postgresData, data.TargetPolicyRevision, ids, clock)
	if err != nil {
		t.Fatalf("newTenantIAMAdminRuntime() error = %v", err)
	}
	if adminService == nil {
		t.Fatal("newTenantIAMAdminRuntime() returned nil service for the pinned policy revision")
	}
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
