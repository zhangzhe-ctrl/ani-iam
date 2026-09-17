//go:build integration && !governance

package integration_test

import (
	"bytes"
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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"google.golang.org/protobuf/proto"
)

// Environment setup contains no Human, Membership, Invitation or projection
// inserts. Owners receive separate empty databases and restricted runtime roles.
type wr23FormalBroker struct {
	configuration                     data.CoreNATSConfiguration
	producerSeed, producerNKey, inbox string
	container                         testcontainers.Container
	operator                          nats.JetStreamContext
}

func wr23PrivateJSON(t *testing.T, file string, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode reviewed private configuration")
	}
	writeReferencePrivate(t, file, raw)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func wr23PrivateCommand(t *testing.T, run, tag string, command *exec.Cmd) []byte {
	t.Helper()
	var output, diagnostic bytes.Buffer
	command.Stdout = &output
	command.Stderr = &diagnostic
	err := command.Run()
	raw := append(append([]byte{}, output.Bytes()...), diagnostic.Bytes()...)
	writeReferencePrivate(t, filepath.Join(run, "private", tag+".log"), raw)
	if err != nil {
		t.Fatalf("%s failed; private diagnostic retained", tag)
	}
	return output.Bytes()
}

func newWR23FormalBroker(t *testing.T, environment, trust string) *wr23FormalBroker {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr23" {
		t.Fatal("WR23 broker scope required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	private := filepath.Join(run, "private", "formal-broker")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	ca, key, caFile := writeProcessE2ECertificateAuthority(t, private)
	cert, keyFile := writeProcessE2ELeafCertificate(t, private, "server", "broker.wr23.test", x509.ExtKeyUsageServerAuth, ca, key)
	public := map[string]string{}
	seeds := map[string]string{}
	keys := map[string]nkeys.KeyPair{}
	for _, role := range []string{"operator", "producer", "consumer", "other"} {
		k, err := nkeys.CreateUser()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(k.Wipe)
		keys[role] = k
		public[role], err = k.PublicKey()
		if err != nil {
			t.Fatal(err)
		}
		seed, err := k.Seed()
		if err != nil {
			t.Fatal(err)
		}
		seed = append([]byte(nil), seed...)
		seeds[role] = filepath.Join(private, role+".seed")
		writeReferencePrivate(t, seeds[role], seed)
		clear(seed)
	}
	inbox := "_WR23." + filepath.Base(run)
	manifest := filepath.Join(private, "environment.json")
	digest := wr23PrivateJSON(t, manifest, map[string]any{"version": 1, "run_id": filepath.Base(run), "broker_name": "wr23-broker", "account": "WR23", "stream": "CORE", "consumer": "IAM", "inbox_prefix": inbox, "public_keys": public})
	config := filepath.Join(private, "active.conf")
	wr23PrivateCommand(t, run, "formal-broker-render", exec.CommandContext(ctx, "python3", filepath.Join(findRepositoryRoot(t), "tools/wr23-resume/broker-config.py"), "--manifest", manifest, "--approved-sha256", digest, "--state", "active", "--output", config, "--proof", filepath.Join(run, "formal-broker-render-results.json")))
	c, err := testcontainers.Run(ctx, wr23ResumeNATSImage, testcontainers.WithCmd("-c", "/run/wr23/nats.conf"), testcontainers.WithFiles(
		testcontainers.ContainerFile{HostFilePath: config, ContainerFilePath: "/run/wr23/nats.conf", FileMode: 0600},
		testcontainers.ContainerFile{HostFilePath: cert, ContainerFilePath: "/run/wr23/server.crt", FileMode: 0644},
		testcontainers.ContainerFile{HostFilePath: keyFile, ContainerFilePath: "/run/wr23/server.key", FileMode: 0600},
		testcontainers.ContainerFile{HostFilePath: caFile, ContainerFilePath: "/run/wr23/ca.crt", FileMode: 0644}), testcontainers.WithExposedPorts("4222/tcp", "8222/tcp"), isolatedContainer(t, "nats", "4222/tcp", "8222/tcp"), testcontainers.WithWaitStrategy(wait.ForHTTP("/varz").WithPort("8222/tcp").WithStartupTimeout(30*time.Second)))
	if c != nil {
		t.Cleanup(func() {
			if err := testcontainers.TerminateContainer(c); err != nil {
				t.Error("terminate own formal broker")
			}
		})
	}
	if err != nil {
		t.Fatal("formal broker startup")
	}
	port, err := c.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatal(err)
	}
	address := "tls://127.0.0.1:" + port.Port()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	operator, err := nats.Connect(address, nats.Nkey(public["operator"], keys["operator"].Sign), nats.Secure(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: "broker.wr23.test", RootCAs: roots}), nats.TLSHandshakeFirst(), nats.IgnoreDiscoveredServers(), nats.CustomInboxPrefix(inbox+".operator"), nats.Timeout(2*time.Second), nats.ReconnectBufSize(0))
	if err != nil {
		t.Fatal("formal broker independent operator proof", err)
	}
	t.Cleanup(operator.Close)
	configured, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	configuredSHA := sha256.Sum256(configured)
	actualConfig, err := exec.CommandContext(ctx, "docker", "exec", c.GetContainerID(), "sha256sum", "/run/wr23/nats.conf").Output()
	if err != nil || !strings.HasPrefix(string(actualConfig), hex.EncodeToString(configuredSHA[:])+" ") || operator.ConnectedServerName() != "wr23-broker" {
		t.Fatal("actual initial broker configuration not proven")
	}
	recordReference(t, run, map[string]any{"kind": "formal-broker-initial-config-applied", "container_id": c.GetContainerID(), "configuration_sha256": hex.EncodeToString(configuredSHA[:]), "manifest_sha256": digest, "broker_name": operator.ConnectedServerName(), "account": "WR23", "sole_publisher_nkey": public["producer"], "independent_consumer_nkey": public["consumer"]})
	js, err := operator.JetStream(nats.MaxWait(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	subjects := []string{"ani.integration.tenant.lifecycle.v1", "ani.integration.tenant.iam-bootstrap.v1", "ani.integration.tenant.lifecycle-heartbeat.v1"}
	if _, err = js.AddStream(&nats.StreamConfig{Name: "CORE", Subjects: subjects, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, Replicas: 1, DenyDelete: true, DenyPurge: true, Discard: nats.DiscardNew, MaxMsgSize: 65536, MaxBytes: 128 << 20, MaxAge: 48 * time.Hour}); err != nil {
		t.Fatal("reviewed stream creation", err)
	}
	if _, err = js.AddConsumer("CORE", &nats.ConsumerConfig{Name: "IAM", Durable: "IAM", DeliverPolicy: nats.DeliverAllPolicy, AckPolicy: nats.AckExplicitPolicy, AckWait: 30 * time.Second, MaxDeliver: -1, FilterSubjects: subjects, ReplayPolicy: nats.ReplayInstantPolicy, MaxAckPending: 1, MaxWaiting: 4, MaxRequestBatch: 1, MaxRequestExpires: 2 * time.Second, Replicas: 1}); err != nil {
		t.Fatal("reviewed durable consumer creation", err)
	}
	producerBinding := mustV7(t)
	authority := data.CoreBrokerConfiguration{Environment: environment, TrustDomain: trust, BrokerName: "wr23-broker", Account: "WR23", Stream: "CORE", Consumer: "IAM", ConsumerID: mustV7(t), ConsumerBindingID: mustV7(t), ConsumerNKey: public["consumer"], Producer: "core.wr23.test"}
	for _, subject := range subjects {
		r := data.CoreBrokerRoute{ID: mustV7(t), Subject: subject, ProducerBindingID: producerBinding, ProducerNKey: public["producer"]}
		r.TargetSHA256 = authority.RouteRevision(r)
		authority.Routes = append(authority.Routes, r)
	}
	return &wr23FormalBroker{configuration: data.CoreNATSConfiguration{URL: address, ServerName: "broker.wr23.test", InboxPrefix: inbox + ".consumer", SeedFile: seeds["consumer"], CAFile: caFile, Authority: authority}, producerSeed: seeds["producer"], producerNKey: public["producer"], inbox: inbox, container: c, operator: js}
}

// One task-owned PostgreSQL container hosts independent owner databases. The
// setup credential is not inherited by the runtime processes or their roles.
func wr23OwnerDatabase(t *testing.T, db *postgresEnvironment, name, ownerRole string) (*pgxpool.Pool, string) {
	t.Helper()
	if (name != "wr23_core" || ownerRole != "wr23_core_owner") && (name != "wr23_notification" || ownerRole != "wr23_notify_owner") {
		t.Fatal("unregistered owner database")
	}
	if db.bootstrapDSN == "" {
		t.Fatal("own cluster bootstrap unavailable")
	}
	ctx := context.Background()
	root := mustPool(t, db.bootstrapDSN)
	defer root.Close()
	password := randomPassword(t)
	if _, err := root.Exec(ctx, "CREATE ROLE "+pgx.Identifier{ownerRole}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '"+strings.ReplaceAll(password, "'", "''")+"'"); err != nil {
		t.Fatal("create own database owner")
	}
	if _, err := root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" OWNER "+pgx.Identifier{ownerRole}.Sanitize()); err != nil {
		t.Fatal("create own service database")
	}
	dsn := postgresDSN(ownerRole, password, db.host, name, "wr23-"+name+"-owner")
	pool := mustPool(t, dsn)
	t.Cleanup(pool.Close)
	return pool, dsn
}

func wr23PrepareCore(t *testing.T, db *postgresEnvironment, run, plan string) (map[string]any, *pgxpool.Pool) {
	t.Helper()
	owner, dsn := wr23OwnerDatabase(t, db, "wr23_core", "wr23_core_owner")
	root := mustPool(t, db.bootstrapDSN)
	defer root.Close()
	password := randomPassword(t)
	if _, err := root.Exec(context.Background(), "CREATE ROLE wr23_core_runtime LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION PASSWORD '"+strings.ReplaceAll(password, "'", "''")+"'"); err != nil {
		t.Fatal("create own Core runtime role")
	}
	ownerFile, runtimeFile := filepath.Join(run, "private", "core-owner.secret"), filepath.Join(run, "private", "core-runtime.secret")
	writeReferencePrivate(t, ownerFile, []byte(dsn))
	writeReferencePrivate(t, runtimeFile, []byte(postgresDSN("wr23_core_runtime", password, db.host, "wr23_core", "wr23-core-runtime")))
	file := filepath.Join(run, "private", "core-initialize.json")
	wr23PrivateJSON(t, file, map[string]any{"database": "wr23_core", "runtime_role": "wr23_core_runtime", "database_url_file": ownerFile, "plans": []any{map[string]any{"id": plan, "code": "wr23-standard", "limits": map[string]int64{"cpu_core": 8}}}, "quotas": []any{map[string]any{"resource_type": "cpu_core", "default_quota": 4, "enabled": true}, map[string]any{"resource_type": "memory_gb", "default_quota": 16, "enabled": true}}})
	raw := wr23PrivateCommand(t, run, "core-owner-initialize", exec.Command(filepath.Join(run, "private", "core-lifecycle-admin"), "-action", "initialize", "-config", file))
	recordReference(t, run, map[string]any{"kind": "formal-Core-empty-owner-initialize", "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(raw))})
	return map[string]any{"database": "wr23_core", "runtime_role": "wr23_core_runtime", "producer": "core.wr23.test", "database_url_file": runtimeFile}, owner
}

func wr23Workload(t *testing.T, name, domain string, targets ...[2]string) biz.BootstrapWorkload {
	t.Helper()
	w := biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: name, DNSIdentity: name + "." + domain}
	for _, target := range targets {
		w.Grants = append(w.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: target[0], Operation: target[1]})
	}
	return w
}
func wr23DualCertificate(t *testing.T, directory, name string, ca *x509.Certificate, key ed25519.PrivateKey) (string, string) {
	t.Helper()
	cert, file := writeProcessE2ELeafCertificate(t, directory, name, name, x509.ExtKeyUsageServerAuth, ca, key)
	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("fresh dual certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(cert, file)
	if err != nil {
		t.Fatal("fresh dual private key")
	}
	leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, pair.PrivateKey.(ed25519.PrivateKey).Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	writeProcessE2EPEM(t, cert, "CERTIFICATE", der)
	return cert, file
}

func wr23PrepareNotification(t *testing.T, db *postgresEnvironment, boss *wr22BossEnvironment, console *wr20Environment, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap, manifest *biz.WorkloadBootstrapManifest) *wr22Notification {
	t.Helper()
	run := console.run
	n := &wr22Notification{wr20Notification: &wr20Notification{ownerTest: t, e: console}, boss: boss}
	n.producer = wr23Workload(t, "notification-producer", manifest.TrustDomain, [2]string{"ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"}, [2]string{biz.NotificationAudience, biz.NotificationSubmitOperation}, [2]string{biz.NotificationAudience, biz.NotificationGetOwnOperation})
	// Match the fixed IAM local issuer identity and its actual client certificate.
	n.producer.DNSIdentity = "ani-iam." + manifest.TrustDomain
	n.receiver = wr23Workload(t, "notification", manifest.TrustDomain, [2]string{"ani-iam", biz.VerifyWorkloadCallerRPC}, [2]string{"ani-iam", "/grpc.health.v1.Health/Check"}, [2]string{biz.NotificationAudience, "notification.receive"})
	manifest.Workloads = append(manifest.Workloads, n.producer, n.receiver)
	n.certFile, n.keyFile = wr23DualCertificate(t, directory, n.receiver.DNSIdentity, ca, key)
	cfg.Runtime.Notification.CertificateFile, cfg.Runtime.Notification.PrivateKeyFile = writeProcessE2ELeafCertificate(t, directory, "notification-producer", n.producer.DNSIdentity, x509.ExtKeyUsageClientAuth, ca, key)
	cfg.Runtime.Notification.ClientDnsName = n.producer.DNSIdentity
	cfg.Runtime.Notification.PauseIdentityDelivery = false
	cfg.Runtime.Notification.ConsoleActionUrlBase = console.origin + "/password-action"
	cfg.Runtime.Notification.TenantInvitationUrlBase = console.origin + "/invitation"
	cfg.Runtime.Notification.PlatformInvitationUrlBase = boss.origin + "/invitation"
	cfg.Runtime.Notification.BossActionUrlBase = boss.origin + "/password-action"
	pool, ownerDSN := wr23OwnerDatabase(t, db, "wr23_notification", "wr23_notify_owner")
	n.pool = pool
	root := mustPool(t, db.bootstrapDSN)
	defer root.Close()
	password := randomPassword(t)
	if _, err := root.Exec(context.Background(), "CREATE ROLE wr23_notify_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '"+strings.ReplaceAll(password, "'", "''")+"'"); err != nil {
		t.Fatal("create restricted Notification role")
	}
	ownerFile, runtimeFile := filepath.Join(directory, "notify-owner.secret"), filepath.Join(directory, "notify-runtime.secret")
	writeReferencePrivate(t, ownerFile, []byte(ownerDSN))
	writeReferencePrivate(t, runtimeFile, []byte(postgresDSN("wr23_notify_runtime", password, db.host, "wr23_notification", "wr23-notification-runtime")))
	command := exec.Command(filepath.Join(run, "private", "notification-bootstrap.test"), "-test.run=^TestWR23PrepareRestrictedDatabase$", "-test.v")
	command.Env = append(os.Environ(), "WR23_NOTIFICATION_OWNER_FILE="+ownerFile, "WR23_NOTIFICATION_RUNTIME_FILE="+runtimeFile)
	wr23PrivateCommand(t, run, "notification-schema-prepare", command)
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
			t.Fatal("fresh Notification key")
		}
		return base64.StdEncoding.EncodeToString(v)
	}
	config := map[string]any{"server": map[string]any{"grpc": map[string]any{"network": "tcp", "addr": n.address, "timeout": "2s"}, "admin": map[string]any{"network": "tcp", "addr": admin, "timeout": "1s"}, "shutdown_timeout": "5s"}, "notification": map[string]any{"enabled": true, "postgres_dsn": n.runtimeDSN(t, directory), "active_encryption_key_version": "wr23-1", "encryption_keys": []any{map[string]any{"version": "wr23-1", "material_base64": randomKey()}}, "active_fingerprint_key_version": "wr23-1", "fingerprint_keys": []any{map[string]any{"version": "wr23-1", "material_base64": randomKey()}}, "allowed_action_origins": []string{console.origin, boss.origin}, "smtp": map[string]any{"address": n.smtpAddress, "from": "iam@wr23.test"}, "worker_id": "wr23-notification", "delivery_poll_interval": "0.2s", "maintenance_interval": "0.5s"}, "workload": map[string]any{"registry_file": wr32RegistryPath(t), "registry_sha256": wr32Registry(t).Digest(), "iam_address": cfg.Server.Grpc.Addr, "iam_server_name": "iam.wr17-18.test", "environment": manifest.Environment, "trust_domain": manifest.TrustDomain, "certificate_file": n.certFile, "private_key_file": n.keyFile, "ca_file": cfg.Server.Grpc.Tls.ClientCaFile, "iam_producer_principal_id": n.producer.PrincipalID.String(), "producer_id": "ani-iam"}}
	n.configFile = filepath.Join(directory, "notification.json")
	wr23PrivateJSON(t, n.configFile, config)
	n.start(t)
	return n
}

type wr23FormalEnvironment struct {
	snapshotSecondTLS grpcworkload.TLSFiles
	projectionMode    string
	*wr23CoreEnvironment
	console      *wr20Environment
	notification *wr22Notification
	broker       *wr23FormalBroker
	database     *postgresEnvironment
	gatewayEnv   map[string]string
}

type wr23FormalSetupExtra func(string, *x509.Certificate, ed25519.PrivateKey, *conf.Bootstrap, *biz.WorkloadBootstrapManifest)

func newWR23FormalEnvironment(t *testing.T, extras ...wr23FormalSetupExtra) *wr23FormalEnvironment {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr23" || os.Getenv("WR23_FORMAL_COMBINATION") != "1" {
		t.Fatal("declared WR23 formal combination required")
	}
	projectionMode := "shadow"
	if os.Getenv("WR23_ENFORCED") == "1" {
		wr23EnforcementAdmission(t)
		projectionMode = "enforced"
	}
	db := newPostgresEnvironment(t)
	owner := mustPool(t, db.migrationDSN(primaryDB))
	t.Cleanup(owner.Close)
	var count int
	if err := owner.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM principals)+(SELECT count(*) FROM tenant_access)+(SELECT count(*) FROM tenant_memberships)+(SELECT count(*) FROM platform_memberships)").Scan(&count); err != nil || count != 0 {
		t.Fatal("formal identity database is not empty")
	}
	iamRedis, _ := newIsolatedRedis(t, context.Background())
	gatewayRedis, _ := newIsolatedRedis(t, context.Background())
	b := &wr22BossEnvironment{wr22Environment: &wr22Environment{wr20Environment: &wr20Environment{run: run, owner: owner, iamRedis: iamRedis, gatewayRedis: gatewayRedis}}, dexSecret: randomPassword(t), dexPassword: randomPassword(t)}
	e := &wr23FormalEnvironment{wr23CoreEnvironment: &wr23CoreEnvironment{boss: b, plan: mustV7(t).String()}, database: db, projectionMode: projectionMode}
	gatewayAddress, adminAddress := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	e.adminURL = "http://" + adminAddress
	upstream, _ := url.Parse("http://" + gatewayAddress)
	bossFront := httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(upstream))
	t.Cleanup(bossFront.Close)
	consoleFront := httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(upstream))
	t.Cleanup(consoleFront.Close)
	_, bossPort, _ := net.SplitHostPort(bossFront.Listener.Addr().String())
	_, consolePort, _ := net.SplitHostPort(consoleFront.Listener.Addr().String())
	b.origin = "https://boss.wr23.test:" + bossPort
	e.console = &wr20Environment{run: run, owner: owner, origin: "https://console.wr23.test:" + consolePort, password: randomPassword(t), iamRedis: iamRedis, gatewayRedis: gatewayRedis}
	b.issuer, _ = wr22BossDex(t, run, b.dexSecret, b.dexPassword, []string{b.origin + "/api/v1/auth/oidc/callback", b.origin + "/api/v1/auth/identity-links/oidc/callback"})
	b.browserEnv = &wr20Environment{run: run, origin: b.origin, issuer: b.issuer, dexSecret: b.dexSecret, dexPassword: b.dexPassword}
	consoleSecret := randomPassword(t)
	consoleIssuer, _ := wr20Dex(t, run, consoleSecret, b.dexPassword, []string{e.console.origin + "/api/v1/auth/oidc/callback", e.console.origin + "/api/v1/auth/identity-links/oidc/callback"})
	coreConfig, coreOwner := wr23PrepareCore(t, db, run, e.plan)
	e.core = coreOwner
	const environment = "wr23-lifecycle"
	const trust = "wr23.test"
	e.broker = newWR23FormalBroker(t, environment, trust)
	setup := formalIAMSetup{Environment: environment, TrustDomain: trust, GatewayDNS: "gateway.wr23.test", PrebuiltBinary: filepath.Join(run, "private", "ani-iam-server")}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		cfg.Runtime.Oidc.IssuerUrl = consoleIssuer
		cfg.Runtime.Oidc.LoginRedirectUri = e.console.origin + "/api/v1/auth/oidc/callback"
		cfg.Runtime.Oidc.IdentityLinkRedirectUri = e.console.origin + "/api/v1/auth/identity-links/oidc/callback"
		writeReferencePrivate(t, cfg.Runtime.Oidc.ClientSecretFile, []byte(consoleSecret))
		cfg.Runtime.BossOidc = proto.Clone(cfg.Runtime.Oidc).(*conf.OIDC)
		cfg.Runtime.BossOidc.ClientId = "ani-boss"
		cfg.Runtime.BossOidc.ClientSecretFile = filepath.Join(directory, "boss-oidc.secret")
		writeReferencePrivate(t, cfg.Runtime.BossOidc.ClientSecretFile, []byte(b.dexSecret))
		cfg.Runtime.BossOidc.IssuerUrl = b.issuer
		cfg.Runtime.BossOidc.LoginRedirectUri = b.origin + "/api/v1/auth/oidc/callback"
		cfg.Runtime.BossOidc.IdentityLinkRedirectUri = b.origin + "/api/v1/auth/identity-links/oidc/callback"
		for _, surface := range []struct {
			env   *wr20Environment
			front *httptest.Server
			host  string
		}{{b.browserEnv, bossFront, "boss.wr23.test"}, {e.console, consoleFront, "console.wr23.test"}} {
			certFile, keyFile := writeProcessE2ELeafCertificate(t, directory, surface.host, surface.host, x509.ExtKeyUsageServerAuth, ca, key)
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				t.Fatal("formal browser certificate")
			}
			surface.front.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
			surface.front.StartTLS()
			roots := x509.NewCertPool()
			roots.AddCert(ca)
			expected, _ := url.Parse(surface.env.origin)
			target := surface.front.Listener.Addr().String()
			surface.env.transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != expected.Host {
					return nil, fmt.Errorf("unregistered browser authority")
				}
				return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, target)
			}}
		}
		gateway := wr23Workload(t, "gateway", trust)
		for _, method := range []string{"BeginOIDCLogin", "CompleteOIDCLogin", "RefreshSession", "LogoutSession", "ValidatePrincipal", "RequestInvitedAccountVerification", "CompleteInvitedAccount", "AcceptInvitationWithPassword", "PasswordLogin", "ListSessions"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + method})
		}
		gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/CheckPermission"})
		for _, method := range []string{"ListPlatformMemberships", "CreatePlatformRole", "BindPlatformRole", "UnbindPlatformRole", "ListTenantMemberships", "GetTenantMembership", "ListTenantRoles", "GetTenantRole", "CreateTenantRole", "CreateTenantInvitation", "GetTenantInvitation", "ListTenantInvitations", "GetTenantAccess"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		}
		producer := wr23Workload(t, "broker-producer", trust)
		for _, method := range wr23BootstrapReviewRPCs {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		}
		consumer := wr23Workload(t, "broker-consumer", trust)
		reader := wr23Workload(t, "snapshot-reader", trust, [2]string{"ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"}, [2]string{"ani-core-control", "core.snapshot.begin"}, [2]string{"ani-core-control", "core.snapshot.page"})
		receiver := wr23Workload(t, "snapshot-owner", trust, [2]string{"ani-iam", biz.VerifyWorkloadCallerRPC}, [2]string{"ani-core-control", "core.snapshot.receive"})
		caDigest := sha256.Sum256(ca.Raw)
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: environment, TrustDomain: trust, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour), Workloads: []biz.BootstrapWorkload{gateway, producer, consumer, reader, receiver}}
		if os.Getenv("WR23_SNAPSHOT_READER_BOUNDARY") == "1" {
			second := wr23Workload(t, "snapshot-reader-second", trust, [2]string{"ani-iam", "/iam.v1.AuthenticationService/IssueWorkloadToken"}, [2]string{"ani-core-control", "core.snapshot.begin"}, [2]string{"ani-core-control", "core.snapshot.page"})
			manifest.Workloads = append(manifest.Workloads, second)
			cert, privateKey := writeProcessE2ELeafCertificate(t, directory, "snapshot-reader-second", second.DNSIdentity, x509.ExtKeyUsageClientAuth, ca, key)
			e.snapshotSecondTLS = grpcworkload.TLSFiles{CertificateFile: cert, PrivateKeyFile: privateKey, CAFile: cfg.Server.Grpc.Tls.ClientCaFile}
		}
		for _, extra := range extras {
			extra(directory, ca, key, cfg, &manifest)
		}
		e.notification = wr23PrepareNotification(t, db, b, e.console, directory, ca, key, cfg, &manifest)
		file := filepath.Join(directory, "workloads.json")
		digest := wr23PrivateJSON(t, file, manifest)
		dsnFile := filepath.Join(directory, "provisioner.secret")
		writeReferencePrivate(t, dsnFile, []byte(postgresDSN(provisionerRole, db.provisionerPass, db.host, primaryDB, "wr23-offline-provisioner")))
		wr23PrivateCommand(t, run, "formal-workload-provision", exec.Command(binary, "provision-workloads", "--manifest", file, "--approved-manifest-sha256", digest, "--environment", environment, "--trust-domain", trust, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--registry-file", wr32RegistryPath(t), "--approved-registry-sha256", wr32Registry(t).Digest(), "--dsn-file", dsnFile))
		a := e.broker.configuration.Authority
		registration := data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Mode: "register", Reason: "WR23 formal independent broker identity registration", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: a, Bindings: []data.CoreBrokerBindingChange{{ID: a.Routes[0].ProducerBindingID, PrincipalID: producer.PrincipalID, NKeyPublic: e.broker.producerNKey, Status: "active"}, {ID: a.ConsumerBindingID, PrincipalID: consumer.PrincipalID, NKeyPublic: a.ConsumerNKey, Status: "active"}}}
		authorization := data.CoreBrokerProvisionManifest{Version: 1, ID: mustV7(t), Mode: "grants", Reason: "WR23 explicit exact publish receive and execution authority", ExpiresAt: time.Now().UTC().Add(time.Hour), Configuration: a}
		for _, r := range a.Routes {
			authorization.Grants = append(authorization.Grants, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: producer.PrincipalID, BindingID: r.ProducerBindingID, RouteID: r.ID, Action: "publish", Status: "active"}, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: consumer.PrincipalID, BindingID: a.ConsumerBindingID, RouteID: r.ID, Action: "receive", Status: "active"})
			if r.Subject == "ani.integration.tenant.iam-bootstrap.v1" {
				authorization.Grants = append(authorization.Grants, data.CoreBrokerGrantChange{ID: mustV7(t), PrincipalID: consumer.PrincipalID, BindingID: a.ConsumerBindingID, RouteID: r.ID, Action: "execute", Status: "active"})
			}
		}
		for _, m := range []data.CoreBrokerProvisionManifest{registration, authorization} {
			file := filepath.Join(directory, "broker-"+m.Mode+".json")
			digest := wr23PrivateJSON(t, file, m)
			wr23PrivateCommand(t, run, "formal-broker-"+m.Mode, exec.Command(binary, "provision-core-broker", "--manifest", file, "--approved-manifest-sha256", digest, "--environment", environment, "--trust-domain", trust, "--dsn-file", dsnFile))
		}
		snapshotAddress := reserveIAMProcessLoopbackAddress(t)
		readerCert, readerKey := writeProcessE2ELeafCertificate(t, directory, "snapshot-reader", reader.DNSIdentity, x509.ExtKeyUsageClientAuth, ca, key)
		receiverCert, receiverKey := wr23DualCertificate(t, directory, receiver.DNSIdentity, ca, key)
		cfg.Runtime.CoreLifecycleFile = filepath.Join(directory, "core-lifecycle.json")
		cfg.Runtime.CoreLifecycleSha256 = wr23PrivateJSON(t, cfg.Runtime.CoreLifecycleFile, map[string]any{"version": 1, "projection_mode": projectionMode, "broker": e.broker.configuration, "snapshot": map[string]any{"origin": "https://" + snapshotAddress, "server_name": receiver.DNSIdentity, "iam_address": cfg.Server.Grpc.Addr, "iam_server_name": "iam.wr17-18.test", "certificate_file": readerCert, "private_key_file": readerKey, "ca_file": cfg.Server.Grpc.Tls.ClientCaFile}})
		redisFile := filepath.Join(run, "private", "core-redis.secret")
		writeReferencePrivate(t, redisFile, []byte(referenceRedisURL(gatewayRedis)))
		coreConfig["redis_url_file"] = redisFile
		coreConfig["broker"] = map[string]any{"url": e.broker.configuration.URL, "server_name": e.broker.configuration.ServerName, "broker_name": a.BrokerName, "account": a.Account, "stream": a.Stream, "inbox_prefix": e.broker.inbox + ".producer", "nkey_public": e.broker.producerNKey, "seed_file": e.broker.producerSeed, "ca_file": e.broker.configuration.CAFile}
		coreConfig["snapshot"] = map[string]any{"listen_address": snapshotAddress, "iam_address": cfg.Server.Grpc.Addr, "iam_server_name": "iam.wr17-18.test", "environment": environment, "trust_domain": trust, "registry_file": wr32RegistryPath(t), "registry_sha256": wr32Registry(t).Digest(), "certificate_file": receiverCert, "private_key_file": receiverKey, "ca_file": cfg.Server.Grpc.Tls.ClientCaFile}
		configFile := filepath.Join(run, "private", "core-runtime.json")
		wr23PrivateJSON(t, configFile, coreConfig)
		values := map[string]string{"IAM_TARGET_MODE": "wr23", "IAM_TARGET_GRPC_ADDR": cfg.Server.Grpc.Addr, "IAM_TARGET_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": environment, "IAM_WORKLOAD_TRUST_DOMAIN": trust, "IAM_TARGET_TLS_CA_FILE": cfg.Server.Grpc.Tls.ClientCaFile, "IAM_TARGET_TLS_CERT_FILE": filepath.Join(directory, "gateway-client.pem"), "IAM_TARGET_TLS_KEY_FILE": filepath.Join(directory, "gateway-client-key.pem"), "IAM_BROWSER_BOSS_ORIGIN": b.origin, "IAM_BROWSER_CONSOLE_ORIGIN": e.console.origin, "GATEWAY_LISTEN_ADDR": gatewayAddress, "GATEWAY_HEALTH_LISTEN_ADDR": adminAddress, "ANI_CORE_LIFECYCLE_CONFIG": configFile}
		e.gatewayEnv = values
		e.startGateway = func() *referenceProcess {
			p := startReferenceProcess(t, run, "wr23-formal-gateway-"+mustV7(t).String(), values, "ani-gateway")
			waitReferenceHTTP(t, e.adminURL+"/readyz", p)
			return p
		}
	}
	setup.AfterProcessStart = func(iam *formalIAM) { waitFormalHTTP(t, iam.livenessURL, http.StatusOK); e.gateway = e.startGateway() }
	b.iam = startFormalIAM(t, db, iamRedis, setup)
	e.console.iam = b.iam
	db.bootstrapDSN = ""
	e.notification.waitReady(t, true)
	return e
}
