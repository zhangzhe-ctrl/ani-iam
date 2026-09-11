//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const wr20MailpitImage = "ghcr.io/axllent/mailpit@sha256:9d85d6bd20c834ec2b7d08ff97976af7b13e2330d9c56ecade4a231dcd3481ba"

type wr20Notification struct {
	ownerTest                                                                                       *testing.T
	e                                                                                               *wr20Environment
	pool                                                                                            *pgxpool.Pool
	sink                                                                                            testcontainers.Container
	sinkAPI, smtpAddress, address, readyURL, configFile, certFile, keyFile, foreignCert, foreignKey string
	producer, receiver, foreign                                                                     biz.BootstrapWorkload
	process                                                                                         *os.Process
	stop                                                                                            func()
	gate                                                                                            *wr20ResponseGate
	client                                                                                          notificationv1.NotificationServiceClient
	issuer                                                                                          iamv1.AuthenticationServiceClient
	foreignIssuer                                                                                   iamv1.AuthenticationServiceClient
}

// This test-owned TCP gate never terminates TLS or reads credentials. For one
// failure test it discards server-to-client ciphertext while still forwarding
// requests; closing those exact connections simulates a lost response.
type wr20ResponseGate struct {
	listener    net.Listener
	drop        atomic.Bool
	mu          sync.Mutex
	connections []net.Conn
}

func newWR20ResponseGate(t *testing.T, target string) *wr20ResponseGate {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &wr20ResponseGate{listener: l}
	t.Cleanup(func() { _ = l.Close(); g.reset() })
	go func() {
		for {
			inbound, err := l.Accept()
			if err != nil {
				return
			}
			outbound, err := net.DialTimeout("tcp", target, time.Second)
			if err != nil {
				inbound.Close()
				continue
			}
			g.mu.Lock()
			g.connections = append(g.connections, inbound, outbound)
			g.mu.Unlock()
			go func() { defer inbound.Close(); defer outbound.Close(); _, _ = io.Copy(outbound, inbound) }()
			go func() {
				defer inbound.Close()
				defer outbound.Close()
				buf := make([]byte, 32<<10)
				for {
					n, err := outbound.Read(buf)
					if n > 0 && !g.drop.Load() {
						if _, werr := inbound.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return g
}
func (g *wr20ResponseGate) reset() {
	g.drop.Store(false)
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.connections {
		_ = c.Close()
	}
	g.connections = nil
}

func newWR20Notification(t *testing.T) *wr20Notification {
	t.Helper()
	n := &wr20Notification{ownerTest: t}
	n.e = newWR20Environment(t, func(e *wr20Environment, manifest *biz.WorkloadBootstrapManifest, cfg *conf.Bootstrap, directory string, ca *x509.Certificate, key ed25519.PrivateKey) {
		n.e = e
		n.producer = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr20-iam-dispatcher", DNSIdentity: "ani-iam.wr20.test"}
		n.receiver = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr20-notification", DNSIdentity: "notification.wr20.test"}
		n.foreign = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr20-foreign-producer", DNSIdentity: "foreign.wr20.test"}
		for _, w := range []*biz.BootstrapWorkload{&n.producer, &n.foreign} {
			for _, op := range []string{biz.NotificationSubmitOperation, biz.NotificationGetOwnOperation} {
				w.Grants = append(w.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: biz.NotificationAudience, Operation: op})
			}
			w.Grants = append(w.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/IssueWorkloadToken"})
		}
		for _, op := range []string{biz.VerifyWorkloadCallerRPC, "/grpc.health.v1.Health/Check"} {
			n.receiver.Grants = append(n.receiver.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: op})
		}
		manifest.Workloads = append(manifest.Workloads, n.producer, n.receiver, n.foreign)
		n.certFile, n.keyFile = writeProcessE2ELeafCertificate(t, directory, "notification", "notification.wr20.test", x509.ExtKeyUsageServerAuth, ca, key)
		// The same Notification identity is receiver on its port and caller to IAM.
		raw, _ := os.ReadFile(n.certFile)
		block, _ := pem.Decode(raw)
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal("notification certificate")
		}
		pair, err := tls.LoadX509KeyPair(n.certFile, n.keyFile)
		if err != nil {
			t.Fatal("notification key")
		}
		leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, pair.PrivateKey.(ed25519.PrivateKey).Public(), key)
		if err != nil {
			t.Fatal("notification dual-use certificate")
		}
		writeProcessE2EPEM(t, n.certFile, "CERTIFICATE", der)
		n.foreignCert, n.foreignKey = writeProcessE2ELeafCertificate(t, directory, "foreign-producer", n.foreign.DNSIdentity, x509.ExtKeyUsageClientAuth, ca, key)
		n.prepareDatabase(t, directory)
		n.prepareSink(t)
		n.address = reserveIAMProcessLoopbackAddress(t)
		admin := reserveIAMProcessLoopbackAddress(t)
		n.readyURL = "http://" + admin + "/readyz"
		n.gate = newWR20ResponseGate(t, n.address)
		cfg.Runtime.Notification.Address = n.gate.listener.Addr().String()
		cfg.Runtime.Notification.ServerDnsName = n.receiver.DNSIdentity
		cfg.Runtime.Notification.ConsoleActionUrlBase = e.origin + "/password-action"
		randomKey := func() string {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				t.Fatal("notification encryption seed")
			}
			return base64.StdEncoding.EncodeToString(b)
		}
		data := map[string]any{"server": map[string]any{"grpc": map[string]any{"network": "tcp", "addr": n.address, "timeout": "2s"}, "admin": map[string]any{"network": "tcp", "addr": admin, "timeout": "1s"}, "shutdown_timeout": "5s"}, "notification": map[string]any{"enabled": true, "postgres_dsn": n.runtimeDSN(t, directory), "active_encryption_key_version": "wr20-1", "encryption_keys": []any{map[string]any{"version": "wr20-1", "material_base64": randomKey()}}, "active_fingerprint_key_version": "wr20-1", "fingerprint_keys": []any{map[string]any{"version": "wr20-1", "material_base64": randomKey()}}, "allowed_action_origins": []string{e.origin}, "smtp": map[string]any{"address": n.smtpAddress, "from": "iam@wr20.test"}, "worker_id": "wr20-notification", "delivery_poll_interval": "0.2s", "maintenance_interval": "0.5s"}, "workload": map[string]any{"iam_address": cfg.Server.Grpc.Addr, "iam_server_name": "iam.wr17-18.test", "environment": manifest.Environment, "trust_domain": manifest.TrustDomain, "certificate_file": n.certFile, "private_key_file": n.keyFile, "ca_file": cfg.Server.Grpc.Tls.ClientCaFile, "iam_producer_principal_id": n.producer.PrincipalID.String(), "producer_id": "ani-iam"}}
		bytes, _ := json.Marshal(data)
		n.configFile = filepath.Join(directory, "notification.json")
		writeReferencePrivate(t, n.configFile, bytes)
		n.start(t)
		cert, keyFile := cfg.Runtime.Notification.CertificateFile, cfg.Runtime.Notification.PrivateKeyFile
		n.client = notificationv1.NewNotificationServiceClient(n.connection(t, n.address, n.receiver.DNSIdentity, cert, keyFile, cfg.Server.Grpc.Tls.ClientCaFile))
		n.issuer = iamv1.NewAuthenticationServiceClient(n.connection(t, cfg.Server.Grpc.Addr, "iam.wr17-18.test", cert, keyFile, cfg.Server.Grpc.Tls.ClientCaFile))
		n.foreignIssuer = iamv1.NewAuthenticationServiceClient(n.connection(t, cfg.Server.Grpc.Addr, "iam.wr17-18.test", n.foreignCert, n.foreignKey, cfg.Server.Grpc.Tls.ClientCaFile))
	})
	n.waitReady(t, true)
	return n
}
func (n *wr20Notification) prepareDatabase(t *testing.T, directory string) {
	ctx := context.Background()
	ownerPass, runtimePass := randomPassword(t), randomPassword(t)
	c, err := postgres.Run(ctx, postgresImage, postgres.WithDatabase("wr20_notification"), postgres.WithUsername("wr20_notify_owner"), postgres.WithPassword(ownerPass), postgres.BasicWaitStrategies(), isolatedContainer(t, "notification-postgres", "5432/tcp"))
	if err != nil {
		t.Fatal("Notification PostgreSQL start failed")
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	endpoint, err := c.Endpoint(ctx, "")
	if err != nil {
		t.Fatal("Notification PostgreSQL endpoint")
	}
	dsn := postgresDSN("wr20_notify_owner", ownerPass, endpoint, "wr20_notification", "wr20-notification-owner")
	runtime := postgresDSN("wr20_notify_runtime", runtimePass, endpoint, "wr20_notification", "wr20-notification-runtime")
	n.pool = mustPool(t, dsn)
	t.Cleanup(n.pool.Close)
	ownerFile, passwordFile, runtimeFile := filepath.Join(directory, "notify-owner.secret"), filepath.Join(directory, "notify-runtime-password.secret"), filepath.Join(directory, "notify-runtime.secret")
	for p, s := range map[string]string{ownerFile: dsn, passwordFile: runtimePass, runtimeFile: runtime} {
		writeReferencePrivate(t, p, []byte(s))
	}
	cmd := exec.Command(filepath.Join(n.e.run, "private", "notification-bootstrap.test"), "-test.run=^TestWR20PrepareRestrictedDatabase$", "-test.v")
	cmd.Env = append(os.Environ(), "WR20_NOTIFICATION_OWNER_FILE="+ownerFile, "WR20_NOTIFICATION_RUNTIME_PASSWORD_FILE="+passwordFile, "WR20_NOTIFICATION_RUNTIME_FILE="+runtimeFile)
	output, err := cmd.CombinedOutput()
	writeReferencePrivate(t, filepath.Join(directory, "notification-bootstrap.private.log"), output)
	if err != nil {
		t.Fatal("Notification restricted bootstrap failed")
	}
	recordReference(t, n.e.run, map[string]any{"stage": "C", "assertion": "notification_exact_schema_and_restricted_role", "pass": true, "database": "wr20_notification", "role": "wr20_notify_runtime"})
}
func (n *wr20Notification) runtimeDSN(t *testing.T, directory string) string {
	b, err := os.ReadFile(filepath.Join(directory, "notify-runtime.secret"))
	if err != nil {
		t.Fatal("Notification runtime credential")
	}
	return string(b)
}
func (n *wr20Notification) prepareSink(t *testing.T) {
	c, err := testcontainers.Run(context.Background(), wr20MailpitImage, testcontainers.WithEnv(map[string]string{"MP_DATABASE": "/tmp/wr20-mailpit.db", "MP_MAX_MESSAGES": "0", "MP_DISABLE_VERSION_CHECK": "true", "MP_SMTP_DISABLE_RDNS": "true"}), testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"), isolatedContainer(t, "mailpit", "1025/tcp", "8025/tcp"), testcontainers.WithWaitStrategy(wait.ForHTTP("/readyz").WithPort("8025/tcp").WithStartupTimeout(time.Minute)))
	if err != nil {
		if c != nil {
			t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
		}
		writeReferencePrivate(t, filepath.Join(n.e.run, "private", "mailpit-startup-error.log"), []byte(err.Error()))
		t.Fatal("pinned Mailpit startup failed")
	}
	n.sink = c
	smtpPort, err := c.MappedPort(context.Background(), "1025/tcp")
	if err != nil {
		t.Fatal("SMTP endpoint")
	}
	uiPort, err := c.MappedPort(context.Background(), "8025/tcp")
	if err != nil {
		t.Fatal("Mailpit API endpoint")
	}
	n.smtpAddress = "127.0.0.1:" + smtpPort.Port()
	n.sinkAPI = "http://127.0.0.1:" + uiPort.Port()
	t.Cleanup(func() {
		// Preserve sink data privately for evidence/reuse before stopping the task container.
		if reader, err := c.CopyFileFromContainer(context.Background(), "/tmp/wr20-mailpit.db"); err == nil {
			bytes, _ := io.ReadAll(reader)
			reader.Close()
			writeReferencePrivate(t, filepath.Join(n.e.run, "private", "mailpit-"+c.GetContainerID()[:12]+".db"), bytes)
		}
		_ = testcontainers.TerminateContainer(c)
	})
}
func (n *wr20Notification) start(t *testing.T) {
	name := "notification-" + uuid.NewString()
	binary := filepath.Join(n.e.run, "private", "ani-notification-service")
	logPath := filepath.Join(n.e.run, "private", name+".log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-conf", n.configFile)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOMAXPROCS=2"}
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal("Notification formal process start failed")
	}
	n.process = cmd.Process
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); log.Close() }()
	var once sync.Once
	n.stop = func() {
		once.Do(func() {
			_ = cmd.Process.Signal(syscall.SIGCONT)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Notification process failed; private log reference %s", name)
				}
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				n.ownerTest.Error("Notification stop timed out")
			}
			recordReference(t, n.e.run, map[string]any{"process": name, "stage": "exited"})
		})
	}
	n.ownerTest.Cleanup(n.stop)
	binaryBytes, _ := os.ReadFile(binary)
	sum := sha256.Sum256(binaryBytes)
	recordReference(t, n.e.run, map[string]any{"process": name, "pid": cmd.Process.Pid, "stage": "started", "binary_sha256": hex.EncodeToString(sum[:]), "log_reference": logPath})
}
func (n *wr20Notification) waitReady(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		r, _ := http.NewRequestWithContext(ctx, "GET", n.readyURL, nil)
		response, err := http.DefaultClient.Do(r)
		cancel()
		if err == nil {
			response.Body.Close()
			if (response.StatusCode == 200) == want {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Notification readiness did not reach %t", want)
}
func (n *wr20Notification) connection(t *testing.T, address, name, cert, key, ca string) *grpc.ClientConn {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal("client key pair")
	}
	raw, err := os.ReadFile(ca)
	if err != nil {
		t.Fatal("CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatal("CA parse")
	}
	c, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{pair}, ServerName: name})), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal("mTLS connection")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func (n *wr20Notification) wat(t *testing.T, operation string, foreign bool) string {
	t.Helper()
	client := n.issuer
	if foreign {
		client = n.foreignIssuer
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, err := client.IssueWorkloadToken(ctx, &iamv1.IssueWorkloadTokenRequest{Audience: biz.NotificationAudience, OperationId: operation})
	if err != nil {
		t.Fatalf("formal WAT issuance failed: %v", err)
	}
	return r.GetWorkloadToken()
}
func wr20WorkloadContext(token string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if token != "" {
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("ani-workload-token", token))
	}
	return ctx, cancel
}

// Read only the named test message. Raw mail and its action capability never
// enter public evidence or failure strings.
func (n *wr20Notification) mailToken(t *testing.T, recipient, notificationID string) string {
	t.Helper()
	var deliveryID string
	if err := n.pool.QueryRow(context.Background(), "SELECT delivery_id FROM deliveries WHERE notification_id=$1", notificationID).Scan(&deliveryID); err != nil {
		t.Fatal("notification delivery reference")
	}
	deadline := time.Now().Add(100 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(n.sinkAPI + "/api/v1/messages")
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		var list struct {
			Messages []struct {
				ID string
				To []struct{ Address string }
			}
		}
		if json.Unmarshal(raw, &list) != nil {
			t.Fatal("Mailpit list decoding")
		}
		for _, m := range list.Messages {
			match := false
			for _, a := range m.To {
				match = match || a.Address == recipient
			}
			if !match {
				continue
			}
			response, err = http.Get(n.sinkAPI + "/api/v1/message/" + url.PathEscape(m.ID))
			if err != nil {
				continue
			}
			raw, _ = io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			var msg struct {
				Text      string
				HTML      string
				MessageID string
			}
			if json.Unmarshal(raw, &msg) != nil {
				t.Fatal("Mailpit message decoding")
			}
			if !strings.Contains(msg.MessageID, deliveryID) {
				continue
			}
			for _, word := range regexp.MustCompile(regexp.QuoteMeta(n.e.origin+"/password-action")+`\?[^\s"<>]+`).FindAllString(html.UnescapeString(msg.Text+" "+msg.HTML), -1) {
				word = strings.Trim(word, "<>\"'()")
				u, err := url.Parse(word)
				if err == nil && u.Scheme+"://"+u.Host == n.e.origin && u.Path == "/password-action" && u.Query().Get("token") != "" {
					recordReference(t, n.e.run, map[string]any{"stage": "C", "assertion": "SMTP_sink_accepted", "notification_id": notificationID, "sink_message_id": m.ID, "pass": true})
					return u.Query().Get("token")
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("matching SMTP action message was not accepted")
	return ""
}
