//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/durationpb"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Frozen WR-18 acceptance inventory, separate from the product ingress map.
var formalRPCs = map[string][]string{
	"AuthenticationService": {"PasswordLogin", "RequestPasswordAction", "CompletePasswordAction", "BeginOIDCLogin", "CompleteOIDCLogin", "BeginOIDCIdentityLink", "CompleteOIDCIdentityLink", "RefreshSession", "LogoutSession", "SwitchTenant", "ValidatePrincipal"},
	"AuthorizationService":  {"CheckPermission"},
	"IAMAdminService":       {"CreateTenantWorkload", "GetTenantWorkload", "ListTenantWorkloads", "UpdateTenantWorkload", "CreateAPIKey", "ListAPIKeys", "RevokeAPIKey", "GetTenantMembership", "ListTenantMemberships", "UpdateTenantMembership", "RemoveTenantMembership", "GetTenantRole", "ListTenantRoles", "BindTenantRole", "UnbindTenantRole"},
}

func seedFormalIngress(t *testing.T, env *postgresEnvironment) {
	operations := []string{healthpb.Health_Check_FullMethodName}
	for service, methods := range formalRPCs {
		for _, method := range methods {
			operations = append(operations, "/iam.v1."+service+"/"+method)
		}
	}
	seedGatewayWorkload(t, env, operations...)
}

type formalIAM struct {
	process       *os.Process
	conn          *grpc.ClientConn
	auth          iamv1.AuthenticationServiceClient
	admin         iamv1.IAMAdminServiceClient
	authorization iamv1.AuthorizationServiceClient
	readinessURL  string
	livenessURL   string
	stop          func()
	binary        string
	config        *conf.Bootstrap
	directory     string
	tlsConfig     *tls.Config
}

type formalCredentialKey struct{}

type formalIAMSetup struct {
	Environment, TrustDomain, GatewayDNS string
	BeforeStart                          func(string, string, *x509.Certificate, ed25519.PrivateKey, *conf.Bootstrap)
}

func startFormalIAM(t *testing.T, environment *postgresEnvironment, redisClient *redis.Client, options ...formalIAMSetup) *formalIAM {
	t.Helper()
	setup := formalIAMSetup{Environment: "wr17-18-isolated", TrustDomain: "iam.wr17-18.test", GatewayDNS: "ani-gateway"}
	if len(options) > 1 {
		t.Fatal("only one formal IAM setup is accepted")
	}
	if len(options) == 1 {
		setup = options[0]
		if setup.Environment == "" || setup.TrustDomain == "" || setup.GatewayDNS == "" {
			t.Fatal("formal IAM identity configuration is required")
		}
	}
	runDir, _ := isolatedRun(t)
	directory := filepath.Join(runDir, "private", "formal-"+uuid.NewString())
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	ca, caKey, caFile := writeProcessE2ECertificateAuthority(t, directory)
	serverCert, serverKey := writeProcessE2ELeafCertificate(t, directory, "iam-server", "iam.wr17-18.test", x509.ExtKeyUsageServerAuth, ca, caKey)
	clientCert, clientKey := writeProcessE2ELeafCertificate(t, directory, "gateway-client", setup.GatewayDNS, x509.ExtKeyUsageClientAuth, ca, caKey)
	notifyCert, notifyKey := writeProcessE2ELeafCertificate(t, directory, "notify-client", "ani-iam", x509.ExtKeyUsageClientAuth, ca, caKey)
	oidcSecret := filepath.Join(directory, "oidc-secret")
	if err := os.WriteFile(oidcSecret, []byte(randomPassword(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	grpcAddress, adminAddress := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	cfg := &conf.Bootstrap{Profile: conf.IsolatedProfile, Server: &conf.Server{
		Grpc:  &conf.Server_GRPC{Network: "tcp", Addr: grpcAddress, Timeout: durationpb.New(2 * time.Second), Tls: &conf.Server_GRPC_TLS{CertificateFile: serverCert, PrivateKeyFile: serverKey, ClientCaFile: caFile, GatewayClientDnsName: setup.GatewayDNS}},
		Admin: &conf.Server_Admin{Network: "tcp", Addr: adminAddress, Timeout: durationpb.New(time.Second)}, ShutdownTimeout: durationpb.New(5 * time.Second),
	}, Runtime: &conf.Runtime{
		Environment: setup.Environment, TrustDomain: setup.TrustDomain, PolicyRevision: data.TargetPolicyRevision,
		Postgresql:   &conf.PostgreSQL{Dsn: environment.runtimeDSN(primaryDB, "ani-iam-wr18-formal")},
		Redis:        &conf.Redis{Addr: redisClient.Options().Addr, Password: redisClient.Options().Password, Namespace: "ani-iam:wr18:" + uuid.NewString(), LoginLimit: 5, LoginWindow: durationpb.New(15 * time.Minute), DialTimeout: durationpb.New(500 * time.Millisecond), ReadTimeout: durationpb.New(500 * time.Millisecond), WriteTimeout: durationpb.New(500 * time.Millisecond)},
		AccessToken:  &conf.AccessToken{Issuer: "ani-iam", ActiveKeyId: "wr18-fixture", PrivateKeyFile: writeProcessE2EAccessTokenKey(t, directory)},
		Notification: &conf.Notification{Address: "127.0.0.1:1", CertificateFile: notifyCert, PrivateKeyFile: notifyKey, ServerCaFile: caFile, ServerDnsName: "ani-notification", ConsoleActionUrlBase: "https://console.example.test/password-action", Locale: "en-US", DispatchInterval: durationpb.New(time.Second), SubmissionTimeout: durationpb.New(time.Second)},
		// This discovery fixture proves process wiring only. The pinned real Dex
		// regression separately proves the OIDC protocol implementation.
		Oidc: &conf.OIDC{Provider: "dex", IssuerUrl: startProcessE2EOIDCDiscovery(t), ClientId: "ani-console", ClientSecretFile: oidcSecret, LoginRedirectUri: "https://console.example.test/auth/oidc/callback", IdentityLinkRedirectUri: "https://console.example.test/auth/oidc/link/callback", RecentReauthentication: durationpb.New(10 * time.Minute), HttpTimeout: durationpb.New(time.Second)},
	}}
	content, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(directory, "runtime.json")
	if err := os.WriteFile(configFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "ani-iam-server")
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = findRepositoryRoot(t)
	build.Env = append(os.Environ(), "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build formal cmd/server: %v\n%s", err, output)
	}
	if setup.BeforeStart != nil {
		setup.BeforeStart(binary, directory, ca, caKey, cfg)
	}
	logPath := filepath.Join(directory, "process.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-conf", configFile)
	command.Dir = findRepositoryRoot(t)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("formal graceful shutdown: %v", err)
				}
			case <-time.After(8 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Error("formal process required forced kill")
			}
			_ = log.Close()
		})
	}
	t.Cleanup(stop)
	binaryContent, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binaryContent)
	record, _ := json.Marshal(map[string]any{"test": t.Name(), "pid": command.Process.Pid, "binary_sha256": hex.EncodeToString(digest[:]), "directory": directory, "config_reference": configFile, "log_reference": logPath})
	recordFile, err := os.OpenFile(filepath.Join(runDir, "formal-processes.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordFile.Write(append(record, '\n'))
	recordFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	runtime := &formalIAM{process: command.Process, readinessURL: "http://" + adminAddress + "/readyz", livenessURL: "http://" + adminAddress + "/healthz", stop: stop, binary: binary, config: cfg, directory: directory}
	waitFormalHTTP(t, runtime.readinessURL, http.StatusOK)
	roots := x509.NewCertPool()
	caPEM, err := os.ReadFile(caFile)
	if err != nil || !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("load formal CA")
	}
	certificate, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	runtime.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "iam.wr17-18.test", RootCAs: roots, Certificates: []tls.Certificate{certificate}}
	connection, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(credentials.NewTLS(runtime.tlsConfig)), grpc.WithDisableRetry(), grpc.WithUnaryInterceptor(func(ctx context.Context, method string, request, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Client request construction only: actual authentication stays in IAM.
		if credential, ok := ctx.Value(formalCredentialKey{}).(string); ok {
			if message, ok := request.(proto.Message); ok {
				field := message.ProtoReflect().Descriptor().Fields().ByName("credential")
				if field != nil {
					message.ProtoReflect().Set(field, protoreflect.ValueOfMessage((&iamv1.BearerCredential{Value: credential}).ProtoReflect()))
				}
			}
		}
		return invoke(ctx, method, request, reply, cc, opts...)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	runtime.conn = connection
	runtime.auth = iamv1.NewAuthenticationServiceClient(connection)
	runtime.admin = iamv1.NewIAMAdminServiceClient(connection)
	runtime.authorization = iamv1.NewAuthorizationServiceClient(connection)
	return runtime
}

func waitFormalHTTP(t *testing.T, url string, want int) {
	t.Helper()
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("formal HTTP %s did not reach %d", url, want)
}

func formalLogin(t *testing.T, runtime *formalIAM, tenant uuid.UUID, account, password string) *iamv1.PasswordLoginResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := runtime.auth.PasswordLogin(ctx, &iamv1.PasswordLoginRequest{Account: account, Password: password, Audience: iamv1.Audience_AUDIENCE_CONSOLE, Boundary: &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenant.String()}}}, SourceIp: "203.0.113.10", DeviceName: "wr18-formal", IdempotencyKey: uuid.NewString()})
	if err != nil || result.GetAccessToken() == "" || result.GetRefreshToken() == "" {
		t.Fatalf("formal Human login failed: %v", err)
	}
	return result
}

func TestFormalIAMWorkloadAndHumanRuntime(t *testing.T) {
	env := newPostgresEnvironment(t)
	ctx := context.Background()
	password := "formal-fixture-password"
	hash, err := data.NewArgon2idPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedTargetLoginFixture(t, ctx, env, hash)
	seedFormalIngress(t, env)
	redisClient, redisContainer := newIsolatedRedis(t, ctx)
	owner := mustPool(t, env.migrationDSN(primaryDB))
	defer owner.Close()
	roleID := uuid.MustParse("0198f062-b76d-7201-9000-000000000002")
	if _, err := owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) SELECT $1,$2,scope,resource,action,now() FROM permission_catalog WHERE scope='tenant' ON CONFLICT DO NOTHING`, tenantA, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO tenant_memberships(tenant_id,id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,$3,'active',1,now(),now())`, tenantB, mustV7(t), actorID); err != nil {
		t.Fatal(err)
	}
	runtime := startFormalIAM(t, env, redisClient)
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	health, err := healthpb.NewHealthClient(runtime.conn).Check(callCtx, &healthpb.HealthCheckRequest{})
	if err != nil || health.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("formal mTLS health: %v", err)
	}
	// Every frozen method is reachable with a real caller. Invalid payloads must
	// reach the implementation, not a stale ingress whitelist or Unimplemented.
	reached, blocked := 0, 0
	for service, methods := range formalRPCs {
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName("iam.v1." + service))
		if err != nil {
			t.Fatal(err)
		}
		svc := desc.(protoreflect.ServiceDescriptor)
		allowed := map[string]bool{}
		for _, method := range methods {
			allowed[method] = true
		}
		for i := 0; i < svc.Methods().Len(); i++ {
			method := svc.Methods().Get(i)
			rpc := "/iam.v1." + service + "/" + string(method.Name())
			err := runtime.conn.Invoke(callCtx, rpc, dynamicpb.NewMessage(method.Input()), dynamicpb.NewMessage(method.Output()))
			if !allowed[string(method.Name())] {
				if status.Code(err) != codes.PermissionDenied {
					t.Fatalf("later RPC %s=%s", rpc, status.Code(err))
				}
				blocked++
				continue
			}
			if status.Code(err) == codes.Unimplemented || status.Code(err) == codes.Internal || status.Code(err) == codes.Unavailable {
				t.Fatalf("frozen RPC %s failed before implementation: %v", rpc, err)
			}
			for _, detail := range status.Convert(err).Details() {
				if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetMetadata()["decision_id"] == "workload-policy" {
					t.Fatalf("frozen RPC blocked at ingress: %s", rpc)
				}
			}
			reached++
		}
	}
	if reached != 27 || blocked != 45 {
		t.Fatalf("formal inventory=%d/%d", reached, blocked)
	}
	noCert := runtime.tlsConfig.Clone()
	noCert.Certificates = nil
	rejected, err := grpc.NewClient(runtime.config.Server.Grpc.Addr, grpc.WithTransportCredentials(credentials.NewTLS(noCert)))
	if err != nil {
		t.Fatal(err)
	}
	tlsCtx, tlsCancel := context.WithTimeout(ctx, time.Second)
	_, err = healthpb.NewHealthClient(rejected).Check(tlsCtx, &healthpb.HealthCheckRequest{})
	tlsCancel()
	rejected.Close()
	if err == nil {
		t.Fatal("formal entry accepted missing client certificate")
	}

	login := formalLogin(t, runtime, tenantA, "user@example.com", password)
	credential := &iamv1.BearerCredential{Value: login.GetAccessToken()}
	validated, err := runtime.auth.ValidatePrincipal(callCtx, &iamv1.ValidatePrincipalRequest{Credential: credential, OperationId: "listInstances", PolicyRevision: data.TargetPolicyRevision})
	if err != nil || validated.GetPrincipal().GetPrincipalId() != actorID.String() {
		t.Fatalf("formal principal validation: %v", err)
	}
	decision, err := runtime.authorization.CheckPermission(callCtx, &iamv1.CheckPermissionRequest{Credential: credential, OperationId: "listInstances", PolicyRevision: data.TargetPolicyRevision, Target: &iamv1.AuthorizationTarget{TenantId: tenantA.String()}})
	if err != nil || !decision.GetDecision().GetAllowed() {
		t.Fatalf("formal permission: %v", err)
	}
	spoofed := metadata.NewOutgoingContext(callCtx, metadata.Pairs("x-ani-principal-id", mustV7(t).String(), "x-ani-tenant-id", tenantB.String(), "x-ani-decision-id", "forged"))
	created, err := runtime.admin.CreateTenantWorkload(spoofed, &iamv1.CreateTenantWorkloadRequest{Credential: credential, TenantId: tenantA.String(), Name: "Formal Bot", RoleIds: []string{roleID.String()}, IdempotencyKey: "formal-create"})
	if err != nil {
		t.Fatalf("formal Workload creation: %v", err)
	}
	principalID := created.GetPrincipal().GetPrincipalId()
	keyRequest := &iamv1.CreateAPIKeyRequest{Credential: credential, PrincipalId: principalID, NeverExpires: true, IdempotencyKey: "formal-key"}
	key, err := runtime.admin.CreateAPIKey(callCtx, keyRequest)
	if err != nil || key.GetApiKeySecret() == "" || key.GetReplayed() {
		t.Fatalf("formal initial Key: %v", err)
	}
	replay, err := runtime.admin.CreateAPIKey(callCtx, keyRequest)
	if err != nil || replay.GetApiKeySecret() != "" || !replay.GetReplayed() || replay.GetApiKey().GetKeyId() != key.GetApiKey().GetKeyId() {
		t.Fatalf("formal Key replay: %v", err)
	}
	var auditActor, caller uuid.UUID
	if err := env.runtimePool.QueryRow(ctx, `SELECT actor_id,caller_principal_id FROM iam_audit_events WHERE tenant_id=$1 AND target_id=$2 AND action='iam.tenant-workload.created'`, tenantA, principalID).Scan(&auditActor, &caller); err != nil || auditActor != actorID || caller != fixtureGatewayID {
		t.Fatalf("formal audit provenance=%s/%s/%v", auditActor, caller, err)
	}
	if _, err := runtime.admin.UpdateTenantMembership(callCtx, &iamv1.UpdateTenantMembershipRequest{Credential: credential, TenantId: tenantA.String(), MembershipId: fixture.membershipID.String(), Status: iamv1.MembershipStatus_MEMBERSHIP_STATUS_REMOVED, ExpectedVersion: 1, IdempotencyKey: "wrong-remove"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("update bypassed removal authority: %v", err)
	}
	if _, err := owner.Exec(ctx, `DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='iam.api-keys' AND action='create'`, tenantA, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.admin.CreateAPIKey(callCtx, keyRequest); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked Human permission allowed replay: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE workload_grants SET status='revoked',version=version+1 WHERE principal_id=$1 AND operation=$2`, fixtureGatewayID, iamv1.AuthenticationService_ValidatePrincipal_FullMethodName); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.auth.ValidatePrincipal(callCtx, &iamv1.ValidatePrincipalRequest{Credential: credential, OperationId: "listInstances", PolicyRevision: data.TargetPolicyRevision}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked direct caller Grant accepted: %v", err)
	}
	refreshed, err := runtime.auth.RefreshSession(callCtx, &iamv1.RefreshSessionRequest{RefreshToken: login.GetRefreshToken(), CsrfToken: "validated-by-gateway", Origin: "https://console.example.test", IdempotencyKey: "formal-refresh"})
	if err != nil || refreshed.GetLogin().GetAccessToken() == "" {
		t.Fatalf("formal Refresh: %v", err)
	}
	switched, err := runtime.auth.SwitchTenant(callCtx, &iamv1.SwitchTenantRequest{Credential: &iamv1.BearerCredential{Value: refreshed.GetLogin().GetAccessToken()}, TenantId: tenantB.String(), IdempotencyKey: "formal-switch"})
	if err != nil || switched.GetLogin().GetPrincipal().GetBoundary().GetTenant().GetTenantId() != tenantB.String() {
		t.Fatalf("formal SwitchTenant: %v", err)
	}
	if _, err := runtime.auth.LogoutSession(callCtx, &iamv1.LogoutSessionRequest{RefreshToken: switched.GetLogin().GetRefreshToken(), CsrfToken: "validated-by-gateway", Origin: "https://console.example.test", IdempotencyKey: "formal-logout"}); err != nil {
		t.Fatalf("formal Logout: %v", err)
	}
	if _, err := runtime.auth.RefreshSession(callCtx, &iamv1.RefreshSessionRequest{RefreshToken: switched.GetLogin().GetRefreshToken(), CsrfToken: "validated-by-gateway", Origin: "https://console.example.test", IdempotencyKey: "after-logout"}); err == nil {
		t.Fatal("logged-out Session refreshed")
	}
	// Missing database privileges fail readiness, invocation, and a fresh
	// formal startup; owner repair is confined to this isolated test database.
	if _, err := owner.Exec(ctx, `REVOKE SELECT ON workload_identity_bindings FROM ani_iam_runtime`); err != nil {
		t.Fatal(err)
	}
	waitFormalHTTP(t, runtime.readinessURL, http.StatusServiceUnavailable)
	if _, err := runtime.auth.PasswordLogin(callCtx, &iamv1.PasswordLoginRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("missing runtime read privilege accepted: %v", err)
	}
	rejectCtx, rejectCancel := context.WithTimeout(ctx, 4*time.Second)
	rejectedStart := exec.CommandContext(rejectCtx, runtime.binary, "-conf", filepath.Join(runtime.directory, "runtime.json"))
	rejectionOutput, rejectionErr := rejectedStart.CombinedOutput()
	rejectCancel()
	if rejectionErr == nil || !strings.Contains(string(rejectionOutput), "runtime schema revision, role or guards") {
		t.Fatal("formal startup did not reject missing runtime privilege")
	}
	if _, err := owner.Exec(ctx, `GRANT SELECT ON workload_identity_bindings TO ani_iam_runtime`); err != nil {
		t.Fatal(err)
	}
	waitFormalHTTP(t, runtime.readinessURL, http.StatusOK)
	// Stop/start reassigns Docker's random host port. Pause/unpause gives a real
	// unresponsive Redis at the same registered address and tests recovery.
	redisID := redisContainer.GetContainerID()
	if err := exec.Command("docker", "pause", redisID).Run(); err != nil {
		t.Fatal(err)
	}
	paused := true
	defer func() {
		if paused {
			_ = exec.Command("docker", "unpause", redisID).Run()
		}
	}()
	t.Logf("registered dependency fault: docker pause %s", redisID)
	waitFormalHTTP(t, runtime.readinessURL, http.StatusServiceUnavailable)
	waitFormalHTTP(t, runtime.livenessURL, http.StatusOK)
	if err := exec.Command("docker", "unpause", redisID).Run(); err != nil {
		t.Fatal(err)
	}
	paused = false
	t.Logf("registered dependency recovery: docker unpause %s", redisID)
	waitFormalHTTP(t, runtime.readinessURL, http.StatusOK)
	runtime.stop()
	t.Log("WR18_REGRESSION_PASS: 27 original reachable / 45 not granted to this fixture; real Human, Workload, current authority, readiness recovery and graceful shutdown")
}

func TestFormalIAMRejectsInvalidConfiguration(t *testing.T) {
	// No dependencies are started: config rejection must precede any connection.
	root := findRepositoryRoot(t)
	directory := t.TempDir()
	binary := filepath.Join(directory, "ani-iam-server")
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build formal rejection binary: %v\n%s", err, output)
	}
	for _, input := range []string{`{"profile":"legacy-auth"}`, `{"profile":"workload-isolated","runtime":{"environment":"","trust_domain":""}}`} {
		config := filepath.Join(directory, "rejected.json")
		if err := os.WriteFile(config, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		command := exec.CommandContext(ctx, binary, "-conf", config)
		output, err := command.CombinedOutput()
		cancel()
		if err == nil || strings.Contains(string(output), "[gRPC] server listening") {
			t.Fatalf("invalid formal configuration accepted: %v", err)
		}
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatal("invalid config waited on runtime instead of rejecting")
		}
	}
}

// Seed only an isolated owner fixture. Login, token creation, current permission
// evaluation and the subsequent management calls all use the formal process.
func seedFormalHumanCredential(t *testing.T, env *postgresEnvironment, principal, tenant, role uuid.UUID, account, password string) {
	t.Helper()
	ctx := context.Background()
	owner := mustPool(t, env.migrationDSN(primaryDB))
	defer owner.Close()
	hash, err := data.NewArgon2idPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	identity := mustV7(t)
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenant_lifecycle_projections(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until) VALUES($1,'active',1,now(),now(),now()+interval '5 minutes')`, []any{tenant}},
		{`INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at) VALUES($1,$2,now(),now(),now())`, []any{principal, account}},
		{`INSERT INTO identities(id,principal_id,provider,issuer,subject,status,version,created_at,updated_at) VALUES($1,$2,'password','ani-local',$3,'active',1,now(),now())`, []any{identity, principal, account}},
		{`INSERT INTO password_credentials(principal_id,identity_id,password_hash,algorithm,version,created_at,updated_at) VALUES($1,$2,$3,'argon2id',1,now(),now())`, []any{principal, identity, hash}},
		{`INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) SELECT $1,$2,scope,resource,action,now() FROM permission_catalog WHERE scope='tenant' ON CONFLICT DO NOTHING`, []any{tenant, role}},
	} {
		if _, err := owner.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed controlled Human credential: %v", err)
		}
	}
}
