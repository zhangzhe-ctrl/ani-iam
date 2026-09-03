package cp0_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	kratostracing "github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kratos "github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
	kratoslog "github.com/go-kratos/kratos/v3/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	serverpkg "github.com/zhangzhe-ctrl/ani-iam/internal/server"
)

func TestIsolatedRuntimeLifecycle(t *testing.T) {
	cfg := isolatedConfig()
	readiness := serverpkg.NewReadiness()
	var logOutput bytes.Buffer
	logger := newTestLogger(&logOutput)
	observability, err := serverpkg.NewObservability("ani-iam-cp0-test", "test", readiness)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	middlewares := observability.ServerMiddleware(logger)
	grpcServer := serverpkg.NewGRPCServer(cfg.Server.Grpc, middlewares...)
	adminServer := serverpkg.NewAdminServer(cfg.Server.Admin, readiness, observability.Gatherer(), middlewares...)
	grpcEndpoint, err := grpcServer.Endpoint()
	if err != nil {
		t.Fatalf("gRPC Endpoint() error = %v", err)
	}
	adminEndpoint, err := adminServer.Endpoint()
	if err != nil {
		t.Fatalf("admin Endpoint() error = %v", err)
	}

	app := kratos.New(
		kratos.Name("ani-iam-cp0-test"),
		kratos.Logger(logger),
		kratos.Server(grpcServer, adminServer),
		kratos.AfterStart(func(context.Context) error { readiness.Set(true); return nil }),
		kratos.BeforeStop(func(context.Context) error { readiness.Set(false); return nil }),
		kratos.AfterStop(observability.Shutdown),
		kratos.StopTimeout(cfg.Server.ShutdownTimeout.AsDuration()),
	)
	runResult := make(chan error, 1)
	go func() { runResult <- app.Run() }()

	waitForReady(t, readiness)
	assertAdminEndpoint(t, adminEndpoint.Host, "/healthz", http.StatusOK, `"status":"ok"`)
	assertAdminEndpoint(t, adminEndpoint.Host, "/readyz", http.StatusOK, `"status":"ready"`)
	assertAdminEndpoint(t, adminEndpoint.Host, "/metrics", http.StatusOK, "ani_iam_runtime_ready 1")
	assertAdminEndpoint(t, adminEndpoint.Host, "/metrics", http.StatusOK, "server_requests_code_total")
	assertGRPCHealth(t, grpcEndpoint.Host)

	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case err := <-runResult:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop gracefully")
	}
	if readiness.Ready() {
		t.Fatal("runtime remained ready after stop")
	}
	if !strings.Contains(logOutput.String(), `"msg":"server request"`) {
		t.Fatalf("Kratos server logging middleware did not emit request log: %s", logOutput.String())
	}
	if !regexp.MustCompile(`"trace_id":"[0-9a-f]{32}"`).MatchString(logOutput.String()) {
		t.Fatalf("Kratos tracing middleware did not correlate request log: %s", logOutput.String())
	}
}

func TestAdminReadinessUsesKratosErrorEncoding(t *testing.T) {
	cfg := isolatedConfig()
	readiness := serverpkg.NewReadiness()
	logger := newTestLogger(io.Discard)
	observability, err := serverpkg.NewObservability("ani-iam-cp0-test", "test", readiness)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer observability.Shutdown(context.Background())
	adminServer := serverpkg.NewAdminServer(
		cfg.Server.Admin,
		readiness,
		observability.Gatherer(),
		observability.ServerMiddleware(logger)...,
	)

	recorder := httptest.NewRecorder()
	adminServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"reason":"NOT_READY"`) {
		t.Fatalf("GET /readyz = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestCommittedConfigLoadsAsGeneratedType(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test path")
	}
	configPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "configs"))
	c := config.New(config.WithSource(file.NewSource(configPath), env.NewSource("KRATOS")))
	defer c.Close()
	if err := c.Load(); err != nil {
		t.Fatalf("config Load(%s) error = %v", configPath, err)
	}
	var cfg conf.Bootstrap
	if err := c.Scan(&cfg); err != nil {
		t.Fatalf("config Scan(%s) error = %v", configPath, err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config Validate(%s) error = %v", configPath, err)
	}
	if cfg.Profile != conf.IsolatedProfile {
		t.Fatalf("runtime profile = %q", cfg.Profile)
	}
}

func TestBizLayerHasNoFrameworkOrAdapterImports(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test path")
	}
	bizRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "internal", "biz"))
	forbidden := []string{"go-kratos", "protobuf", "grpc", "internal/compat", "internal/data", "database/sql", "pgx", "redis"}
	err := filepath.WalkDir(bizRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range forbidden {
			if strings.Contains(string(contents), `"`+needle) {
				t.Errorf("%s imports forbidden dependency containing %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan biz layer: %v", err)
	}
}

func isolatedConfig() *conf.Bootstrap {
	return &conf.Bootstrap{
		Profile: conf.IsolatedProfile,
		Server: &conf.Server{
			Grpc:            &conf.Server_GRPC{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
			Admin:           &conf.Server_Admin{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
			ShutdownTimeout: durationpb.New(3 * time.Second),
		},
	}
}

func waitForReady(t *testing.T, readiness *serverpkg.Readiness) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if readiness.Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("runtime did not become ready")
}

func assertAdminEndpoint(t *testing.T, addr, path string, status int, bodyContains string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if resp.StatusCode != status || !strings.Contains(string(body), bodyContains) {
		t.Fatalf("GET %s = %d %q, want %d containing %q", path, resp.StatusCode, body, status, bodyContains)
	}
}

func assertGRPCHealth(t *testing.T, addr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	defer conn.Close()
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("gRPC health: %v", err)
	}
	if response.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("gRPC health = %s", response.Status)
	}
}

func newTestLogger(writer io.Writer) *slog.Logger {
	return slog.New(kratoslog.NewHandler(
		kratoslog.WithWriter(writer),
		kratoslog.WithFormat(kratoslog.FormatJSON),
		kratoslog.WithExtractor(kratostracing.TraceAttrs),
		kratoslog.WithFilter(kratoslog.FilterKey("args")),
	))
}
