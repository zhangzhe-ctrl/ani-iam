//go:build integration

package integration_test

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestRealIAMAdapterToNotificationProcessMutualTLS(t *testing.T) {
	const notificationRuntimeCommit = "a477a38280c8626b0fdf6664e7afb049d22c2a58"
	notificationDirectory := strings.TrimSpace(os.Getenv("DP2_NOTIFICATION_DIR"))
	if notificationDirectory == "" {
		t.Skip("DP2_NOTIFICATION_DIR must name the fixed Notification L3 worktree")
	}
	if info, err := os.Stat(filepath.Join(notificationDirectory, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("fixed Notification module is unavailable at %s", notificationDirectory)
	}
	head := exec.Command("git", "-C", notificationDirectory, "rev-parse", "HEAD")
	headOutput, err := head.Output()
	if err != nil {
		t.Fatalf("read fixed Notification commit: %v", err)
	}
	if got := strings.TrimSpace(string(headOutput)); got != notificationRuntimeCommit {
		t.Fatalf("Notification runtime commit = %q, want %q", got, notificationRuntimeCommit)
	}
	status := exec.Command("git", "-C", notificationDirectory, "status", "--porcelain=v1", "--untracked-files=all")
	statusOutput, err := status.Output()
	if err != nil {
		t.Fatalf("read fixed Notification worktree status: %v", err)
	}
	if len(statusOutput) != 0 {
		t.Fatalf("fixed Notification worktree is dirty:\n%s", statusOutput)
	}

	directory := t.TempDir()
	caCertificate, caKey, caFile := writeProcessE2ECertificateAuthority(t, directory)
	serverCertificateFile, serverPrivateKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "notification-server", "ani-notification", x509.ExtKeyUsageServerAuth, caCertificate, caKey,
	)
	clientCertificateFile, clientPrivateKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "iam-notification-client", "ani-iam", x509.ExtKeyUsageClientAuth, caCertificate, caKey,
	)
	grpcAddress := reserveIAMProcessLoopbackAddress(t)
	adminAddress := reserveIAMProcessLoopbackAddress(t)
	binary := filepath.Join(directory, "ani-notification-service")
	build := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-o", binary, "./cmd/ani-notification-service")
	build.Dir = notificationDirectory
	build.Env = append(os.Environ(), "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixed Notification process: %v\n%s", err, output)
	}

	logFilePath := filepath.Join(directory, "notification-process.log")
	logFile, err := os.Create(logFilePath)
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binary, "-conf", filepath.Join(notificationDirectory, "configs"))
	process.Dir = notificationDirectory
	process.Stdout = logFile
	process.Stderr = logFile
	process.Env = append(os.Environ(),
		"ANI_SERVER_GRPC_ADDR="+grpcAddress,
		"ANI_SERVER_ADMIN_ADDR="+adminAddress,
		"ANI_SERVER_GRPC_TLS_CERTIFICATE_FILE="+serverCertificateFile,
		"ANI_SERVER_GRPC_TLS_PRIVATE_KEY_FILE="+serverPrivateKeyFile,
		"ANI_SERVER_GRPC_TLS_CLIENT_CA_FILE="+caFile,
		"ANI_SERVER_GRPC_TLS_IAM_CLIENT_DNS_NAME=ani-iam",
		"ANI_SERVER_SHUTDOWN_TIMEOUT=2s",
	)
	if err := process.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start fixed Notification process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = process.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = process.Process.Kill()
				<-done
			}
		}
		_ = logFile.Close()
	})
	waitForNotificationProcessHealth(t, adminAddress, done, &exited, logFilePath)

	client, err := data.NewNotificationGRPCClient(data.NotificationGRPCClientConfig{
		Address:         grpcAddress,
		CertificateFile: clientCertificateFile,
		PrivateKeyFile:  clientPrivateKeyFile,
		ServerCAFile:    caFile,
		ServerDNSName:   "ani-notification",
	})
	if err != nil {
		t.Fatalf("configure real IAM Notification client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	submitter, err := data.NewGRPCPasswordActionNotificationSubmitter(client, data.PasswordActionNotificationSubmitterConfig{
		ConsoleActionURLBase: "https://console.example.test/password-action",
		Locale:               "en-US",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = submitter.SubmitPasswordActionNotification(ctx, biz.PasswordActionNotificationSubmission{
		RequestID:        uuid.MustParse("0198f062-b76d-7001-9000-000000000401"),
		SourceID:         uuid.MustParse("0198f062-b76d-7001-9000-000000000402"),
		SourceVersion:    1,
		CorrelationID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000402"),
		PrincipalID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000403"),
		DestinationEmail: "process-l3@example.test",
		Purpose:          biz.PasswordActionPurposeReset,
		Audience:         biz.AudienceConsole,
		ActionToken:      "test-only-process-l3-action-token",
		OccurredAt:       now,
		DeliverBefore:    now.Add(30 * time.Minute),
	})
	if !errors.Is(err, biz.ErrPasswordActionNotificationRetryable) || !strings.Contains(err.Error(), "STORAGE_UNAVAILABLE") {
		t.Fatalf("real IAM to Notification submission error = %v, want authenticated local runtime-disabled boundary", err)
	}
	if strings.Contains(err.Error(), "test-only-process-l3-action-token") {
		t.Fatal("real cross-service error exposed the action token")
	}
}

func waitForNotificationProcessHealth(
	t *testing.T,
	adminAddress string,
	done <-chan error,
	exited *bool,
	logFilePath string,
) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case processErr := <-done:
			*exited = true
			logs, _ := os.ReadFile(logFilePath)
			t.Fatalf("Notification process exited before health: %v\n%s", processErr, logs)
		default:
		}
		response, requestErr := client.Get("http://" + adminAddress + "/healthz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	logs, _ := os.ReadFile(logFilePath)
	t.Fatalf("Notification process did not become healthy\n%s", logs)
}
