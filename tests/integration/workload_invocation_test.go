//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This proves real IAM bootstrap/mint/verification, separately from the required
// three-process business chain. Human state is the permitted isolated fixture;
// Workloads are provisioned by the formal binary, never by owner SQL seeding.
func TestFormalWorkloadInvocation(t *testing.T) {
	env := newPostgresEnvironment(t)
	ctx := context.Background()
	owner := mustPool(t, env.migrationDSN(primaryDB))
	defer owner.Close()
	password := "wr19-formal-human-fixture"
	hash, err := data.NewArgon2idPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	seedTargetLoginFixture(t, ctx, env, hash)
	role := uuid.MustParse("0198f062-b76d-7201-9000-000000000002")
	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	grantCreate := func() {
		execSQL(`INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','instances','create',now())`, tenantA, role)
	}
	grantCreate()
	redisClient, _ := newIsolatedRedis(t, ctx)
	gatewayID, gatewayBinding, sessionID, sessionBinding := mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	targetGrant, verifyGrant, continuationGrant := mustV7(t), mustV7(t), mustV7(t)
	var receiverTLS *tls.Config
	setup := formalIAMSetup{Environment: "wr19-invocation", TrustDomain: "wr19.test", GatewayDNS: "gateway.wr19.test"}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		digest := sha256.Sum256(ca.Raw)
		m := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(digest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour)}
		gateway := biz.BootstrapWorkload{PrincipalID: gatewayID, BindingID: gatewayBinding, Name: "wr19-gateway", DNSIdentity: setup.GatewayDNS}
		for _, operation := range []string{"/iam.v1.AuthenticationService/PasswordLogin", "/iam.v1.AuthenticationService/ValidatePrincipal", "/iam.v1.AuthenticationService/IssueWorkloadToken", "/iam.v1.AuthenticationService/IssueDelegation", "/iam.v1.AuthorizationService/CheckPermission"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: operation})
		}
		gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: targetGrant, Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation})
		m.Workloads = []biz.BootstrapWorkload{gateway, {PrincipalID: sessionID, BindingID: sessionBinding, Name: "wr19-session", DNSIdentity: "session.wr19.test", Grants: []biz.BootstrapWorkloadGrant{{ID: continuationGrant, Audience: "ani-iam", Operation: biz.VerifySessionContinuationRPC}, {ID: verifyGrant, Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}}}}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		manifestPath, secretPath := filepath.Join(directory, "workloads.json"), filepath.Join(directory, "provisioner.secret")
		if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(secretPath, []byte(postgresDSN(provisionerRole, env.provisionerPass, env.host, primaryDB, "wr19-invocation-bootstrap")), 0o600); err != nil {
			t.Fatal(err)
		}
		approved := sha256.Sum256(raw)
		command := exec.Command(binary, "provision-workloads", "--manifest", manifestPath, "--approved-manifest-sha256", hex.EncodeToString(approved[:]), "--environment", m.Environment, "--trust-domain", m.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", secretPath)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("formal Workload bootstrap failed: %v", err)
		}
		var receipt biz.WorkloadBootstrapReceipt
		if json.Unmarshal(out, &receipt) != nil || receipt.ManifestID != m.ManifestID {
			t.Fatal("invalid bootstrap receipt")
		}
		certFile, keyFile := writeProcessE2ELeafCertificate(t, directory, "session-client", "session.wr19.test", x509.ExtKeyUsageClientAuth, ca, key)
		writeProcessE2ELeafCertificate(t, directory, "example-server", "session.wr19.test", x509.ExtKeyUsageServerAuth, ca, key)
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		receiverTLS = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "iam.wr17-18.test", RootCAs: roots, Certificates: []tls.Certificate{cert}}
	}
	runtime := startFormalIAM(t, env, redisClient, setup)
	receiverConn, err := grpc.NewClient(runtime.config.Server.Grpc.Addr, grpc.WithTransportCredentials(credentials.NewTLS(receiverTLS)), grpc.WithDisableRetry())
	if err != nil {
		t.Fatal(err)
	}
	defer receiverConn.Close()
	receiver := iamv1.NewAuthorizationServiceClient(receiverConn)
	login := formalLogin(t, runtime, tenantA, "user@example.com", password)
	digest := sha256.Sum256([]byte("formal IAM binding fixture; actual owner DTO covered by SDK tests and three-process chain"))
	binding := &iamv1.InvocationBinding{Audience: biz.SessionInvocationAudience, OperationId: biz.SessionInvocationOperation, RpcMethod: biz.SessionInvocationRPC, SourceOperationId: "createInstanceExecSession", TenantId: tenantA.String(), SubjectId: actorID.String(), ResourceId: "wr19-instance", Mode: "exec", RequestSha256: digest[:], PolicyRevision: data.TargetPolicyRevision}
	mintWAT := func() *iamv1.IssueWorkloadTokenResponse {
		t.Helper()
		r, err := runtime.auth.IssueWorkloadToken(ctx, &iamv1.IssueWorkloadTokenRequest{Audience: biz.SessionInvocationAudience, OperationId: biz.SessionInvocationOperation})
		if err != nil || r.GetWorkloadToken() == "" || r.GetPrincipalId() != gatewayID.String() {
			t.Fatalf("formal WAT mint: %v", err)
		}
		if ttl := time.Until(r.ExpiresAt.AsTime()); ttl <= 0 || ttl > 5*time.Minute {
			t.Fatal("WAT TTL out of contract")
		}
		return r
	}
	mintDelegation := func(wat string, b *iamv1.InvocationBinding) (*iamv1.IssueDelegationResponse, error) {
		return runtime.auth.IssueDelegation(ctx, &iamv1.IssueDelegationRequest{WorkloadToken: wat, SubjectCredential: &iamv1.BearerCredential{Value: login.AccessToken}, Binding: b})
	}
	wat := mintWAT()
	delegation, err := mintDelegation(wat.WorkloadToken, binding)
	if err != nil || delegation.GetDelegation() == "" {
		t.Fatalf("formal delegation mint: %v", err)
	}
	if ttl := time.Until(delegation.ExpiresAt.AsTime()); ttl <= 0 || ttl > time.Minute {
		t.Fatal("delegation TTL out of contract")
	}
	request := &iamv1.VerifyWorkloadInvocationRequest{WorkloadToken: wat.WorkloadToken, Delegation: delegation.Delegation, Binding: binding, ObservedPeer: &iamv1.WorkloadPeer{Environment: setup.Environment, TrustDomain: setup.TrustDomain, IdentityKind: "x509_dns", IdentityValue: setup.GatewayDNS}}
	verify := func(r *iamv1.VerifyWorkloadInvocationRequest) error {
		_, err := receiver.VerifyWorkloadInvocation(ctx, r)
		return err
	}
	assertDenied := func(t *testing.T, err error) {
		t.Helper()
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument, codes.FailedPrecondition:
		default:
			t.Fatalf("expected explicit denial, got %v", err)
		}
	}
	t.Run("real bootstrapped caller and subject are distinct", func(t *testing.T) {
		got, err := receiver.VerifyWorkloadInvocation(ctx, request)
		if err != nil || got.GetCaller().GetPrincipalId() != gatewayID.String() || got.GetSubject().GetPrincipalId() != actorID.String() || !proto.Equal(got.GetBinding(), binding) {
			t.Fatalf("formal online verification: %v", err)
		}
		var bootstrapCount, auditCount int
		if err := owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM workload_bootstrap_receipts),(SELECT count(*) FROM iam_audit_events WHERE action IN ('workload.token.issue','workload.delegation.issue'))`).Scan(&bootstrapCount, &auditCount); err != nil || bootstrapCount != 1 || auditCount != 2 {
			t.Fatalf("bootstrap and issuance audit counts %d/%d: %v", bootstrapCount, auditCount, err)
		}
	})

	t.Run("continuation rechecks current receiver caller subject and full binding", func(t *testing.T) {
		verified, err := receiver.VerifyWorkloadInvocation(ctx, request)
		if err != nil || verified.GetContinuation() == "" || time.Until(verified.GetContinuationExpiresAt().AsTime()) <= time.Minute {
			t.Fatalf("continuation issuance: %v", err)
		}
		check := &iamv1.VerifySessionContinuationRequest{Continuation: verified.Continuation, Binding: binding}
		recheck := func() error { _, err := receiver.VerifySessionContinuation(ctx, check); return err }
		if err := recheck(); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name, table, column, inactive, restore string
			id                                     uuid.UUID
		}{
			{"receiver continuation grant", "workload_grants", "status", "revoked", "active", continuationGrant},
			{"original admission verification grant", "workload_grants", "status", "revoked", "active", verifyGrant},
			{"receiver binding", "workload_identity_bindings", "status", "revoked", "active", sessionBinding},
			{"original caller", "principals", "status", "disabled", "active", gatewayID},
			{"caller binding", "workload_identity_bindings", "status", "revoked", "active", gatewayBinding},
			{"caller target grant", "workload_grants", "status", "revoked", "active", targetGrant},
			{"Human subject", "principals", "status", "disabled", "active", actorID},
		} {
			t.Run(tc.name, func(t *testing.T) {
				execSQL("UPDATE "+tc.table+" SET "+tc.column+"=$1 WHERE id=$2", tc.inactive, tc.id)
				assertDenied(t, recheck())
				execSQL("UPDATE "+tc.table+" SET "+tc.column+"=$1 WHERE id=$2", tc.restore, tc.id)
				if err := recheck(); err != nil {
					t.Fatal(err)
				}
			})
		}
		execSQL(`DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='instances' AND action='create'`, tenantA, role)
		assertDenied(t, recheck())
		grantCreate()
		wrong := proto.Clone(check).(*iamv1.VerifySessionContinuationRequest)
		wrong.Binding.TenantId = tenantB.String()
		_, err = receiver.VerifySessionContinuation(ctx, wrong)
		assertDenied(t, err)
		wrong = proto.Clone(check).(*iamv1.VerifySessionContinuationRequest)
		wrong.Binding.RequestSha256[0] ^= 1
		_, err = receiver.VerifySessionContinuation(ctx, wrong)
		assertDenied(t, err)
		_, err = runtime.authorization.VerifySessionContinuation(ctx, check)
		assertDenied(t, err)
		wrong = proto.Clone(check).(*iamv1.VerifySessionContinuationRequest)
		wrong.Continuation = delegation.Delegation
		_, err = receiver.VerifySessionContinuation(ctx, wrong)
		assertDenied(t, err)
		creation := proto.Clone(request).(*iamv1.VerifyWorkloadInvocationRequest)
		creation.Delegation = verified.Continuation
		assertDenied(t, verify(creation))
	})

	for name, mutate := range map[string]func(*iamv1.VerifyWorkloadInvocationRequest){
		"missing WAT":               func(r *iamv1.VerifyWorkloadInvocationRequest) { r.WorkloadToken = "" },
		"missing delegation":        func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Delegation = "" },
		"forged WAT":                func(r *iamv1.VerifyWorkloadInvocationRequest) { r.WorkloadToken = "forged" },
		"token type confusion":      func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Delegation = r.WorkloadToken },
		"peer identity":             func(r *iamv1.VerifyWorkloadInvocationRequest) { r.ObservedPeer.IdentityValue = "forged.wr19.test" },
		"peer environment":          func(r *iamv1.VerifyWorkloadInvocationRequest) { r.ObservedPeer.Environment = "other" },
		"audience":                  func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.Audience = "other" },
		"target operation":          func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.OperationId = "other" },
		"source read cannot create": func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.SourceOperationId = "listInstances" },
		"subject substitution":      func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.SubjectId = sessionID.String() },
		"cross Tenant":              func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.TenantId = tenantB.String() },
		"resource substitution":     func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.ResourceId = "other" },
		"actual request digest":     func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.RequestSha256[0] ^= 1 },
		"exec console interchange": func(r *iamv1.VerifyWorkloadInvocationRequest) {
			r.Binding.Mode = "vm_console"
			r.Binding.SourceOperationId = "createInstanceConsoleSession"
		},
		"policy revision": func(r *iamv1.VerifyWorkloadInvocationRequest) { r.Binding.PolicyRevision = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			r := proto.Clone(request).(*iamv1.VerifyWorkloadInvocationRequest)
			mutate(r)
			assertDenied(t, verify(r))
		})
	}
	t.Run("independent public SDK example runs against formal IAM", func(t *testing.T) {
		runWorkloadExample(t, runtime, login.AccessToken, setup.Environment, setup.TrustDomain)
	})
	t.Run("WAT cannot be substituted even for same caller", func(t *testing.T) {
		r := proto.Clone(request).(*iamv1.VerifyWorkloadInvocationRequest)
		r.WorkloadToken = mintWAT().WorkloadToken
		assertDenied(t, verify(r))
	})
	t.Run("Gateway has no receiver verification grant", func(t *testing.T) {
		_, err := runtime.authorization.VerifyWorkloadInvocation(ctx, request)
		assertDenied(t, err)
	})
	t.Run("receiver own grant must remain current", func(t *testing.T) {
		execSQL(`UPDATE workload_grants SET status='revoked' WHERE id=$1`, verifyGrant)
		assertDenied(t, verify(request))
		execSQL(`UPDATE workload_grants SET status='active' WHERE id=$1`, verifyGrant)
		if err := verify(request); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name, table, inactive string
		id                    uuid.UUID
	}{
		{"caller disabled", "principals", "disabled", gatewayID},
		{"binding revoked", "workload_identity_bindings", "revoked", gatewayBinding},
		{"target grant revoked", "workload_grants", "revoked", targetGrant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execSQL("UPDATE "+tc.table+" SET status=$1 WHERE id=$2", tc.inactive, tc.id)
			assertDenied(t, verify(request))
			execSQL("UPDATE "+tc.table+" SET status='active' WHERE id=$1", tc.id)
			if err := verify(request); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("binding rotation invalidates issued proof and fresh mint recovers", func(t *testing.T) {
		execSQL(`UPDATE workload_identity_bindings SET version=version+1 WHERE id=$1`, gatewayBinding)
		assertDenied(t, verify(request))
		wat = mintWAT()
		delegation, err = mintDelegation(wat.WorkloadToken, binding)
		if err != nil {
			t.Fatal(err)
		}
		request.WorkloadToken, request.Delegation = wat.WorkloadToken, delegation.Delegation
		if err := verify(request); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("current subject permission blocks minted proof and remint", func(t *testing.T) {
		execSQL(`DELETE FROM tenant_role_permissions WHERE tenant_id=$1 AND role_id=$2 AND resource='instances' AND action='create'`, tenantA, role)
		assertDenied(t, verify(request))
		_, err := mintDelegation(wat.WorkloadToken, binding)
		assertDenied(t, err)
		grantCreate()
		if err := verify(request); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cross Tenant cannot mint using Tenant A subject", func(t *testing.T) {
		b := proto.Clone(binding).(*iamv1.InvocationBinding)
		b.TenantId = tenantB.String()
		_, err := mintDelegation(wat.WorkloadToken, b)
		assertDenied(t, err)
	})
	t.Run("mint audit failure releases no credential", func(t *testing.T) {
		execSQL(`CREATE FUNCTION wr19_mint_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action IN ('workload.token.issue','workload.delegation.issue') THEN RAISE EXCEPTION 'WR19 audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr19_mint_audit_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr19_mint_audit_fault()`)
		defer execSQL(`DROP TRIGGER wr19_mint_audit_fault ON iam_audit_events; DROP FUNCTION wr19_mint_audit_fault()`)
		r, err := runtime.auth.IssueWorkloadToken(ctx, &iamv1.IssueWorkloadTokenRequest{Audience: biz.SessionInvocationAudience, OperationId: biz.SessionInvocationOperation})
		if status.Code(err) != codes.Unavailable || r.GetWorkloadToken() != "" {
			t.Fatal("audit failure released WAT")
		}
		d, err := mintDelegation(wat.WorkloadToken, binding)
		if status.Code(err) != codes.Unavailable || d.GetDelegation() != "" {
			t.Fatal("audit failure released delegation")
		}
	})
	t.Run("Human session revocation invalidates delegation", func(t *testing.T) {
		execSQL(`UPDATE sessions SET status='revoked',version=version+1 WHERE principal_id=$1`, actorID)
		assertDenied(t, verify(request))
	})
}

func runWorkloadExample(t *testing.T, runtime *formalIAM, credential, environment, domain string) {
	t.Helper()
	directory := runtime.directory
	binary := filepath.Join(directory, "workload-grpc-example")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(findRepositoryRoot(t), "examples/workload-grpc")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build independent example: %v\n%s", err, out)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	binaryDigest := sha256.Sum256(binaryBytes)
	runDir, _ := isolatedRun(t)
	recordProcess := func(name, event string, cmd *exec.Cmd, configPath string) {
		t.Helper()
		record := map[string]any{"test": t.Name(), "process": name, "event": event, "pid": cmd.Process.Pid, "binary_sha256": hex.EncodeToString(binaryDigest[:]), "config_reference": configPath}
		if cmd.ProcessState != nil {
			record["exit_code"] = cmd.ProcessState.ExitCode()
		}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Error(err)
			return
		}
		file, err := os.OpenFile(filepath.Join(runDir, "example-processes.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Error(err)
		}
	}
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tlsFiles := func(prefix string) map[string]any {
		return map[string]any{"CertificateFile": filepath.Join(directory, prefix+".pem"), "PrivateKeyFile": filepath.Join(directory, prefix+"-key.pem"), "CAFile": runtime.config.Server.Grpc.Tls.ClientCaFile}
	}
	clientConfig := func(prefix string) map[string]any {
		return map[string]any{"Address": runtime.config.Server.Grpc.Addr, "ServerName": "iam.wr17-18.test", "Environment": environment, "TrustDomain": domain, "PolicyRevision": data.TargetPolicyRevision, "TLS": tlsFiles(prefix)}
	}
	inventory := write("example-inventory.json", []map[string]any{{"tenant_id": tenantA.String(), "instance_id": "wr19-instance", "workload_name": "wr19-workload", "ready": true}})
	request := write("example-request.json", map[string]any{"requestId": uuid.NewString(), "idempotencyKey": uuid.NewString(), "principal": map[string]string{"tenantId": tenantA.String(), "subjectId": actorID.String()}, "target": map[string]string{"instanceId": "wr19-instance", "workloadName": "wr19-workload", "workloadKind": "WORKLOAD_KIND_CONTAINER"}, "exec": map[string]any{"command": []string{"true"}, "tty": false, "rows": 24, "cols": 80}})
	secret := filepath.Join(directory, "example-subject.secret")
	if err := os.WriteFile(secret, []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
	address := reserveIAMProcessLoopbackAddress(t)
	receiverConfig := write("example-receiver.json", map[string]any{"iam": clientConfig("session-client"), "inventory_file": inventory, "listen_address": address, "server_tls": tlsFiles("example-server")})
	callerConfig := write("example-caller.json", map[string]any{"iam": clientConfig("gateway-client"), "inventory_file": inventory, "target_address": address, "target_server_name": "session.wr19.test", "credential_file": secret, "request_file": request})
	log, err := os.OpenFile(filepath.Join(directory, "example-receiver.log"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-mode", "receiver", "-config", receiverConfig)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("example receiver exit: %v", err)
			}
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("example receiver required forced termination")
		}
		recordProcess("receiver", "exited", command, receiverConfig)
		log.Close()
	}()
	recordProcess("receiver", "started", command, receiverConfig)
	ready := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("example receiver not ready")
	}
	callCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	caller := exec.CommandContext(callCtx, binary, "-mode", "caller", "-config", callerConfig)
	var output bytes.Buffer
	caller.Stdout, caller.Stderr = &output, &output
	if err := caller.Start(); err != nil {
		t.Fatal("independent example caller did not start")
	}
	recordProcess("caller", "started", caller, callerConfig)
	err = caller.Wait()
	recordProcess("caller", "exited", caller, callerConfig)
	if err != nil || !strings.Contains(output.String(), "authorized call passed") {
		t.Fatalf("independent example call failed: %v", err)
	}
	if strings.Contains(output.String(), credential) {
		t.Fatal("example leaked credential")
	}
	// The same public caller is denied after its real source permission is
	// revoked in the broader IAM test; no test-only SDK constructor is used.
}
