//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestRealIAMGatewayProcessVerticalSlice(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DP2_ANI_GATEWAY_DIR")) == "" {
		t.Skip("DP2_ANI_GATEWAY_DIR must name the fixed ANI DP2 worktree")
	}
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	passwordHash, err := data.NewArgon2idPasswordHasher().Hash("correct-password")
	if err != nil {
		t.Fatalf("hash E2E password fixture: %v", err)
	}
	seedTargetLoginFixture(t, ctx, environment, passwordHash)
	seedDeniedTargetLoginFixture(t, ctx, environment, passwordHash)
	redisClient := newVerticalSliceRedisClient(t, ctx)
	redisAddress := normalizeProcessE2ELoopbackAddress(t, redisClient.Options().Addr)

	runtime := startIAMProcessForGatewayE2E(t, environment, redisAddress)
	output := runGatewayProcessE2E(t, runtime, redisAddress)
	if !strings.Contains(output, "DP2_GATEWAY_PROCESS_E2E_PASS") {
		t.Fatalf("Gateway process did not emit success marker:\n%s", output)
	}
	if !strings.Contains(output, "DP2_GATEWAY_PROCESS_E2E_UNAVAILABLE_PASS") {
		t.Fatalf("Gateway process did not emit real unavailable-IAM marker:\n%s", output)
	}
	if !strings.Contains(output, "DP2_GATEWAY_PROCESS_E2E_TIMEOUT_PASS") {
		t.Fatalf("Gateway process did not emit real IAM-timeout marker:\n%s", output)
	}

	var sessions, grants, audits int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE principal_id = $1`, actorID).Scan(&sessions); err != nil {
		t.Fatalf("query process E2E Sessions: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*)
		FROM session_grants AS grant_record
		JOIN sessions AS session_record ON session_record.id = grant_record.session_id
		WHERE grant_record.tenant_id = $1 AND session_record.principal_id = $2
	`, tenantA, actorID).Scan(&grants); err != nil {
		t.Fatalf("query process E2E Grants: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id = $1 AND actor_id = $2 AND action = 'iam.password.login.succeeded'`, tenantA, actorID).Scan(&audits); err != nil {
		t.Fatalf("query process E2E Audit: %v", err)
	}
	if sessions != 1 || grants != 1 || audits != 1 {
		t.Fatalf("process E2E atomic rows = Sessions:%d Grants:%d Audit:%d, want 1/1/1", sessions, grants, audits)
	}
}

func seedDeniedTargetLoginFixture(t *testing.T, ctx context.Context, environment *postgresEnvironment, passwordHash string) {
	t.Helper()
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	principalID := "0198f062-b76d-7301-9000-000000000001"
	membershipID := "0198f062-b76d-7301-9000-000000000002"
	identityID := "0198f062-b76d-7301-9000-000000000003"
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO principals (id, principal_type, status, version, created_at, updated_at) VALUES ($1, 'human', 'active', 1, now(), now())`, []any{principalID}},
		{`INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, now(), now())`, []any{tenantA, membershipID, principalID}},
		{`INSERT INTO verified_emails (principal_id, normalized_email, verified_at, created_at, updated_at) VALUES ($1, 'denied@example.com', now(), now(), now())`, []any{principalID}},
		{`INSERT INTO identities (id, principal_id, provider, issuer, subject, status, version, created_at, updated_at) VALUES ($1, $2, 'password', 'ani-local', 'denied@example.com', 'active', 1, now(), now())`, []any{identityID, principalID}},
		{`INSERT INTO password_credentials (principal_id, identity_id, password_hash, algorithm, version, created_at, updated_at) VALUES ($1, $2, $3, 'argon2id', 1, now(), now())`, []any{principalID, identityID, passwordHash}},
	}
	for _, statement := range statements {
		if _, err := seedPool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed denied target login fixture: %v", err)
		}
	}
}

type iamGatewayProcessRuntime struct {
	address        string
	serverName     string
	caFile         string
	clientCertFile string
	clientKeyFile  string
}

func startIAMProcessForGatewayE2E(t *testing.T, environment *postgresEnvironment, redisAddress string) iamGatewayProcessRuntime {
	t.Helper()
	repositoryRoot := findRepositoryRoot(t)
	directory := t.TempDir()
	caCertificate, caKey, caFile := writeProcessE2ECertificateAuthority(t, directory)
	serverName := "iam.dp2.test"
	serverCertFile, serverKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "iam-server", serverName, x509.ExtKeyUsageServerAuth, caCertificate, caKey,
	)
	clientCertFile, clientKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "gateway-client", "ani-gateway", x509.ExtKeyUsageClientAuth, caCertificate, caKey,
	)
	accessTokenKeyFile := writeProcessE2EAccessTokenKey(t, directory)
	grpcAddress := reserveIAMProcessLoopbackAddress(t)
	adminAddress := reserveIAMProcessLoopbackAddress(t)
	configFile := filepath.Join(directory, "runtime.yaml")
	configDocument := fmt.Sprintf(`profile: direct-p2-isolated
server:
  grpc:
    network: tcp
    addr: %q
    timeout: 1s
    tls:
      certificate_file: %q
      private_key_file: %q
      client_ca_file: %q
      gateway_client_dns_name: ani-gateway
  admin:
    network: tcp
    addr: %q
    timeout: 1s
  shutdown_timeout: 5s
runtime:
  postgresql:
    dsn: %q
  redis:
    addr: %q
    database: 0
    namespace: ani-iam:dp2-05:process-e2e
    login_limit: 5
    login_window: 900s
    dial_timeout: 0.5s
    read_timeout: 0.5s
    write_timeout: 0.5s
  access_token:
    issuer: ani-iam
    active_key_id: dp2-05-process-e2e
    private_key_file: %q
  policy_revision: %s
`, grpcAddress, serverCertFile, serverKeyFile, caFile, adminAddress,
		postgresDSN(runtimeRole, environment.runtimePass, normalizeProcessE2ELoopbackAddress(t, environment.host), primaryDB, "ani-iam-dp2-05-process-e2e"), redisAddress,
		accessTokenKeyFile, testIntegrationPolicyRevision)
	if err := os.WriteFile(configFile, []byte(configDocument), 0o600); err != nil {
		t.Fatalf("write IAM process config: %v", err)
	}

	serverBinary := filepath.Join(directory, "ani-iam-server")
	build := exec.Command("go", "build", "-o", serverBinary, "./cmd/server")
	build.Dir = repositoryRoot
	build.Env = append(os.Environ(), "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build IAM server process: %v\n%s", err, output)
	}
	logFilePath := filepath.Join(directory, "iam-process.log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		t.Fatalf("create IAM process log: %v", err)
	}
	command := exec.Command(serverBinary, "-conf", configFile)
	command.Dir = repositoryRoot
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start IAM server process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				<-done
			}
		}
		_ = logFile.Close()
	})

	readinessURL := "http://" + adminAddress + "/readyz"
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case processErr := <-done:
			exited = true
			_ = logFile.Close()
			logs, _ := os.ReadFile(logFilePath)
			t.Fatalf("IAM process exited before readiness: %v\n%s", processErr, logs)
		default:
		}
		response, requestErr := client.Get(readinessURL)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return iamGatewayProcessRuntime{
					address:        grpcAddress,
					serverName:     serverName,
					caFile:         caFile,
					clientCertFile: clientCertFile,
					clientKeyFile:  clientKeyFile,
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	logs, _ := os.ReadFile(logFilePath)
	t.Fatalf("IAM process did not become ready at %s\n%s", readinessURL, logs)
	return iamGatewayProcessRuntime{}
}

func runGatewayProcessE2E(t *testing.T, runtime iamGatewayProcessRuntime, redisAddress string) string {
	t.Helper()
	gatewayDirectory := filepath.Join(strings.TrimSpace(os.Getenv("DP2_ANI_GATEWAY_DIR")), "repo", "services", "ani-gateway")
	if info, err := os.Stat(filepath.Join(gatewayDirectory, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("fixed ANI Gateway module is unavailable at %s", gatewayDirectory)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"go", "test", "-tags=integration", "./internal/router",
		"-run", "^TestTargetIAMProcessE2EChild$", "-count=1", "-v",
	)
	command.Dir = gatewayDirectory
	command.Env = append(os.Environ(),
		"GOPROXY=off",
		"DP2_GATEWAY_E2E_CHILD=1",
		"DP2_E2E_IAM_ADDR="+runtime.address,
		"DP2_E2E_IAM_SERVER_NAME="+runtime.serverName,
		"DP2_E2E_IAM_CA_FILE="+runtime.caFile,
		"DP2_E2E_GATEWAY_CERT_FILE="+runtime.clientCertFile,
		"DP2_E2E_GATEWAY_KEY_FILE="+runtime.clientKeyFile,
		"DP2_E2E_REDIS_URL=redis://"+redisAddress+"/0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Gateway process E2E: %v\n%s", err, output)
	}
	return string(output)
}

func reserveIAMProcessLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve IAM process address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release IAM process address: %v", err)
	}
	return address
}

func normalizeProcessE2ELoopbackAddress(t *testing.T, address string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse testcontainer loopback address %q: %v", address, err)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("testcontainer address %q is not loopback", address)
	}
	return net.JoinHostPort(host, port)
}

func writeProcessE2ECertificateAuthority(t *testing.T, directory string) (*x509.Certificate, ed25519.PrivateKey, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "DP2-05 process E2E CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(directory, "ca.pem")
	writeProcessE2EPEM(t, caFile, "CERTIFICATE", der)
	return certificate, privateKey, caFile
}

func writeProcessE2ELeafCertificate(t *testing.T, directory, prefix, commonName string, usage x509.ExtKeyUsage, caCertificate *x509.Certificate, caKey ed25519.PrivateKey) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     []string{commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, publicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificateFile := filepath.Join(directory, prefix+".pem")
	privateKeyFile := filepath.Join(directory, prefix+"-key.pem")
	writeProcessE2EPEM(t, certificateFile, "CERTIFICATE", der)
	writeProcessE2EPEM(t, privateKeyFile, "PRIVATE KEY", privateKeyDER)
	return certificateFile, privateKeyFile
}

func writeProcessE2EAccessTokenKey(t *testing.T, directory string) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "access-token-key.pem")
	writeProcessE2EPEM(t, path, "PRIVATE KEY", der)
	return path
}

func writeProcessE2EPEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	contents := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", filepath.Base(path), err)
	}
}
