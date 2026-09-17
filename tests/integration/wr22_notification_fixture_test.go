//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type wr22Notification struct {
	*wr20Notification
	boss *wr22BossEnvironment
}

func newWR22Notification(t *testing.T) *wr22Notification {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr22" {
		t.Fatal("WR22 isolation required")
	}
	n := &wr22Notification{wr20Notification: &wr20Notification{ownerTest: t, e: &wr20Environment{run: run}}}
	n.boss = newWR22BossEnvironment(t, func(directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, manifest *biz.WorkloadBootstrapManifest) {
		console, _ := url.Parse(cfg.Runtime.Oidc.LoginRedirectUri)
		console.Path = ""
		console.RawPath = ""
		n.e.origin = console.String()
		cfg.Runtime.Notification.PauseIdentityDelivery = false
		cfg.Runtime.Notification.ConsoleActionUrlBase = n.e.origin + "/password-action"
		cfg.Runtime.Notification.TenantInvitationUrlBase = n.e.origin + "/invitation"
		platform, _ := url.Parse(cfg.Runtime.BossOidc.LoginRedirectUri)
		platform.Path = ""
		platform.RawPath = ""
		cfg.Runtime.Notification.PlatformInvitationUrlBase = platform.String() + "/invitation"
		n.producer = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr22-iam-dispatcher", DNSIdentity: "ani-iam.wr22.test"}
		n.receiver = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr22-notification", DNSIdentity: "notification.wr22.test"}
		n.foreign = biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr22-foreign-producer", DNSIdentity: "foreign.wr22.test"}
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
		n.certFile, n.keyFile = writeProcessE2ELeafCertificate(t, directory, "notification", "notification.wr22.test", x509.ExtKeyUsageServerAuth, ca, key)
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

		cfg.Runtime.Notification.CertificateFile, cfg.Runtime.Notification.PrivateKeyFile = writeProcessE2ELeafCertificate(t, directory, "iam-notification-producer", n.producer.DNSIdentity, x509.ExtKeyUsageClientAuth, ca, key)
		cfg.Runtime.Notification.ClientDnsName = n.producer.DNSIdentity
		n.prepareDatabase(t, directory)
		n.prepareSink(t)
		n.address = reserveIAMProcessLoopbackAddress(t)
		admin := reserveIAMProcessLoopbackAddress(t)
		n.readyURL = "http://" + admin + "/readyz"
		n.gate = newWR20ResponseGate(t, n.address)
		cfg.Runtime.Notification.Address = n.gate.listener.Addr().String()
		cfg.Runtime.Notification.ServerDnsName = n.receiver.DNSIdentity
		randomKey := func() string {
			v := make([]byte, 32)
			if _, err := rand.Read(v); err != nil {
				t.Fatal("own Notification key generation")
			}
			return base64.StdEncoding.EncodeToString(v)
		}
		config := map[string]any{"server": map[string]any{"grpc": map[string]any{"network": "tcp", "addr": n.address, "timeout": "2s"}, "admin": map[string]any{"network": "tcp", "addr": admin, "timeout": "1s"}, "shutdown_timeout": "5s"}, "notification": map[string]any{"enabled": true, "postgres_dsn": n.runtimeDSN(t, directory), "active_encryption_key_version": "wr22-1", "encryption_keys": []any{map[string]any{"version": "wr22-1", "material_base64": randomKey()}}, "active_fingerprint_key_version": "wr22-1", "fingerprint_keys": []any{map[string]any{"version": "wr22-1", "material_base64": randomKey()}}, "allowed_action_origins": []string{n.e.origin, platform.String()}, "smtp": map[string]any{"address": n.smtpAddress, "from": "iam@wr22.test"}, "worker_id": "wr22-notification", "delivery_poll_interval": "0.2s", "maintenance_interval": "0.5s"}, "workload": map[string]any{"iam_address": cfg.Server.Grpc.Addr, "iam_server_name": "iam.wr17-18.test", "environment": manifest.Environment, "trust_domain": manifest.TrustDomain, "certificate_file": n.certFile, "private_key_file": n.keyFile, "ca_file": cfg.Server.Grpc.Tls.ClientCaFile, "iam_producer_principal_id": n.producer.PrincipalID.String(), "producer_id": "ani-iam"}}
		raw, _ = json.Marshal(config)
		n.configFile = filepath.Join(directory, "notification.json")
		writeReferencePrivate(t, n.configFile, raw)
		n.start(t)
		cert, keyFile := cfg.Runtime.Notification.CertificateFile, cfg.Runtime.Notification.PrivateKeyFile
		n.client = notificationv1.NewNotificationServiceClient(n.connection(t, n.address, n.receiver.DNSIdentity, cert, keyFile, cfg.Server.Grpc.Tls.ClientCaFile))
		n.issuer = iamv1.NewAuthenticationServiceClient(n.connection(t, cfg.Server.Grpc.Addr, "iam.wr17-18.test", cert, keyFile, cfg.Server.Grpc.Tls.ClientCaFile))
		n.foreignIssuer = iamv1.NewAuthenticationServiceClient(n.connection(t, cfg.Server.Grpc.Addr, "iam.wr17-18.test", n.foreignCert, n.foreignKey, cfg.Server.Grpc.Tls.ClientCaFile))
	})
	n.e = n.boss.wr22Environment.wr20Environment
	n.waitReady(t, true)
	return n
}
func (n *wr22Notification) prepareDatabase(t *testing.T, directory string) {
	ctx := context.Background()
	ownerPass, runtimePass := randomPassword(t), randomPassword(t)
	c, err := postgres.Run(ctx, postgresImage, postgres.WithDatabase("wr22_notification"), postgres.WithUsername("wr22_notify_owner"), postgres.WithPassword(ownerPass), postgres.BasicWaitStrategies(), isolatedContainer(t, "notification-postgres", "5432/tcp"))
	if err != nil {
		t.Fatal("Notification PostgreSQL start failed")
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	endpoint, err := c.Endpoint(ctx, "")
	if err != nil {
		t.Fatal("Notification PostgreSQL endpoint")
	}
	dsn := postgresDSN("wr22_notify_owner", ownerPass, endpoint, "wr22_notification", "wr22-notification-owner")
	runtime := postgresDSN("wr22_notify_runtime", runtimePass, endpoint, "wr22_notification", "wr22-notification-runtime")
	n.pool = mustPool(t, dsn)
	t.Cleanup(n.pool.Close)
	ownerFile, passwordFile, runtimeFile := filepath.Join(directory, "notify-owner.secret"), filepath.Join(directory, "notify-runtime-password.secret"), filepath.Join(directory, "notify-runtime.secret")
	for p, s := range map[string]string{ownerFile: dsn, passwordFile: runtimePass, runtimeFile: runtime} {
		writeReferencePrivate(t, p, []byte(s))
	}
	cmd := exec.Command(filepath.Join(n.e.run, "private", "notification-bootstrap.test"), "-test.run=^TestWR22PrepareRestrictedDatabase$", "-test.v")
	cmd.Env = append(os.Environ(), "WR22_RUN_DIR="+n.e.run, "WR22_NOTIFICATION_OWNER_FILE="+ownerFile, "WR22_NOTIFICATION_RUNTIME_PASSWORD_FILE="+passwordFile, "WR22_NOTIFICATION_RUNTIME_FILE="+runtimeFile)
	output, err := cmd.CombinedOutput()
	writeReferencePrivate(t, filepath.Join(directory, "notification-bootstrap.private.log"), output)
	if err != nil {
		t.Fatal("Notification restricted bootstrap failed")
	}
	recordReference(t, n.e.run, map[string]any{"stage": "C", "assertion": "notification_exact_schema_and_restricted_role", "pass": true, "database": "wr22_notification", "role": "wr22_notify_runtime"})
}

// Read only this run's exact recipient/Delivery-ID message. Raw mail remains
// private; public evidence contains only immutable message and receipt IDs.
func (n *wr22Notification) mailValue(t *testing.T, recipient, notificationID, kind string) string {
	t.Helper()
	var delivery string
	if n.pool.QueryRow(context.Background(), `SELECT delivery_id FROM deliveries WHERE notification_id=$1`, notificationID).Scan(&delivery) != nil {
		t.Fatal("Notification delivery reference")
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(n.sinkAPI + "/api/v1/messages")
		if err != nil {
			time.Sleep(100 * time.Millisecond)
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
			t.Fatal("sink index decoding")
		}
		for _, m := range list.Messages {
			match := false
			for _, to := range m.To {
				match = match || to.Address == recipient
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
			var msg struct{ HTML, Text, MessageID string }
			if json.Unmarshal(raw, &msg) != nil {
				t.Fatal("sink message decoding")
			}
			if !strings.Contains(msg.MessageID, delivery) {
				continue
			}
			value := ""
			if kind == "code" {
				if match := regexp.MustCompile(`<strong>([0-9]{6})</strong>`).FindStringSubmatch(msg.HTML); len(match) == 2 {
					value = match[1]
				}
			} else {
				origin := n.e.origin
				path := "/invitation"
				if kind == "platform" || kind == "boss-password" {
					origin = n.boss.origin
				}
				if kind == "boss-password" {
					path = "/password-action"
				}
				for _, word := range regexp.MustCompile(regexp.QuoteMeta(origin+path)+`\?[^\s"<>]+`).FindAllString(html.UnescapeString(msg.HTML+" "+msg.Text), -1) {
					u, err := url.Parse(strings.Trim(word, "<>\"'()"))
					if err == nil && u.Scheme+"://"+u.Host == origin && u.Path == path {
						value = u.Query().Get("token")
					}
				}
			}
			if value != "" {
				recordReference(t, n.e.run, map[string]any{"stage": "N1", "SMTP_sink_accepted": true, "notification_id": notificationID, "sink_message_id": m.ID})
				return value
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("matching identity message not delivered to SMTP sink")
	return ""
}
