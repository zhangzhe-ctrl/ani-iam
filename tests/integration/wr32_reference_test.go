//go:build integration

package integration_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
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
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

// The new audience is absent from the base declaration. Only the declaration,
// offline authority manifest and this owner harness change after mechanism freeze.
func TestWR32NewTargetFormalProcesses(t *testing.T) {
	ctx := context.Background()
	run, goal := isolatedRun(t)
	if goal != "wr32" {
		t.Fatal("WR32-owned remote run required")
	}
	const audience = "wr32-inventory-owner"
	const receiverDNS = "reference.wr32.test"
	if _, ok := wr32Registry(t).Lookup(audience, "reference.status"); ok {
		t.Fatal("target existed before mechanism freeze")
	}
	registration := filepath.Join(findRepositoryRoot(t), "registrations/wr32-reference-targets.v1.json")
	raw, err := os.ReadFile(registration)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	env := newPostgresEnvironment(t)
	owner := mustPool(t, env.migrationDSN(primaryDB))
	defer owner.Close()
	password := "wr32-reference-human-fixture"
	hash, err := data.NewArgon2idPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	seedTargetLoginFixture(t, ctx, env, hash)
	redisClient, _ := newIsolatedRedis(t, ctx)
	callerID, receiverID := mustV7(t), mustV7(t)
	statusGrant, delegatedGrant, receiverGrant, verifyGrant := mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	setup := formalIAMSetup{Environment: "wr32-reference", TrustDomain: "wr32.test", GatewayDNS: "caller.wr32.test"}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		migrator := filepath.Join(directory, "registry-migrator.secret")
		writeReferencePrivate(t, migrator, []byte(env.migrationDSN(primaryDB)))
		var before, after int
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		install := exec.Command(binary, "install-workload-registry", "--registry-file", registration, "--approved-registry-sha256", registry.Digest(), "--previous-registry-sha256", wr32Registry(t).Digest(), "--dsn-file", migrator)
		output, err := install.CombinedOutput()
		if err != nil {
			t.Fatalf("formal reviewed registration install: %v", err)
		}
		var receipt struct {
			RegistrySHA256 string `json:"registry_sha256"`
			PolicyRevision string `json:"policy_revision"`
		}
		if json.Unmarshal(output, &receipt) != nil || receipt.RegistrySHA256 != registry.Digest() || receipt.PolicyRevision == data.TargetPolicyRevision || receipt.PolicyRevision == "" {
			t.Fatal("invalid registry installation receipt")
		}
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&after); err != nil || after != before {
			t.Fatal("registration created Principal")
		}
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM workload_grants`).Scan(&after); err != nil || after != 0 {
			t.Fatal("registration granted authority")
		}
		cfg.Runtime.WorkloadRegistryFile = registration
		cfg.Runtime.WorkloadRegistrySha256 = registry.Digest()
		cfg.Runtime.PolicyRevision = receipt.PolicyRevision
		role := uuid.MustParse("0198f062-b76d-7201-9000-000000000002")
		if _, err := owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','reference-resources','read',now())`, tenantA, role); err != nil {
			t.Fatal(err)
		}
		caDigest := sha256.Sum256(ca.Raw)
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour)}
		caller := biz.BootstrapWorkload{PrincipalID: callerID, BindingID: mustV7(t), Name: "wr32-reference-caller", DNSIdentity: setup.GatewayDNS}
		for _, op := range []string{"/iam.v1.AuthenticationService/PasswordLogin", "/iam.v1.AuthenticationService/ValidatePrincipal", "/iam.v1.AuthenticationService/IssueWorkloadToken", "/iam.v1.AuthenticationService/IssueDelegation", "/iam.v1.AuthorizationService/CheckPermission"} {
			caller.Grants = append(caller.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: op})
		}
		caller.Grants = append(caller.Grants, biz.BootstrapWorkloadGrant{ID: statusGrant, Audience: audience, Operation: "reference.status"}, biz.BootstrapWorkloadGrant{ID: delegatedGrant, Audience: audience, Operation: "reference.read"})
		receiver := biz.BootstrapWorkload{PrincipalID: receiverID, BindingID: mustV7(t), Name: "wr32-reference-receiver", DNSIdentity: receiverDNS, Grants: []biz.BootstrapWorkloadGrant{{ID: verifyGrant, Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadCaller"}, {ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}, {ID: receiverGrant, Audience: audience, Operation: "reference.receive"}}}
		manifest.Workloads = []biz.BootstrapWorkload{caller, receiver}
		manifestRaw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(directory, "wr32-workloads.json")
		writeReferencePrivate(t, manifestPath, manifestRaw)
		provisioner := filepath.Join(directory, "wr32-provisioner.secret")
		writeReferencePrivate(t, provisioner, []byte(postgresDSN(provisionerRole, env.provisionerPass, env.host, primaryDB, "wr32-reference-bootstrap")))
		manifestDigest := sha256.Sum256(manifestRaw)
		command := exec.Command(binary, "provision-workloads", "--registry-file", registration, "--approved-registry-sha256", registry.Digest(), "--manifest", manifestPath, "--approved-manifest-sha256", hex.EncodeToString(manifestDigest[:]), "--environment", setup.Environment, "--trust-domain", setup.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", provisioner)
		output, err = command.CombinedOutput()
		if err != nil {
			t.Fatalf("formal independent authority bootstrap: %v", err)
		}
		var bootstrap biz.WorkloadBootstrapReceipt
		if json.Unmarshal(output, &bootstrap) != nil || bootstrap.ManifestID != manifest.ManifestID {
			t.Fatal("invalid authority receipt")
		}
		writeProcessE2ELeafCertificate(t, directory, "reference-client", receiverDNS, x509.ExtKeyUsageClientAuth, ca, key)
		writeProcessE2ELeafCertificate(t, directory, "reference-server", receiverDNS, x509.ExtKeyUsageServerAuth, ca, key)
		recordReference(t, run, map[string]any{"test": t.Name(), "event": "new-target-installed-and-provisioned", "registry_sha256": registry.Digest(), "policy_revision": receipt.PolicyRevision, "manifest_sha256": hex.EncodeToString(manifestDigest[:]), "distinct_caller_receiver": callerID != receiverID, "registration_created_authority": false})
	}
	runtime := startFormalIAM(t, env, redisClient, setup)
	login := formalLogin(t, runtime, tenantA, "user@example.com", password)
	directory := runtime.directory
	writeJSON := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(directory, name)
		writeReferencePrivate(t, p, raw)
		return p
	}
	tlsFiles := func(prefix string) map[string]any {
		return map[string]any{"CertificateFile": filepath.Join(directory, prefix+".pem"), "PrivateKeyFile": filepath.Join(directory, prefix+"-key.pem"), "CAFile": runtime.config.Server.Grpc.Tls.ClientCaFile}
	}
	iamConfig := func(prefix string) map[string]any {
		return map[string]any{"Address": runtime.config.Server.Grpc.Addr, "ServerName": "iam.wr17-18.test", "Environment": setup.Environment, "TrustDomain": setup.TrustDomain, "PolicyRevision": runtime.config.Runtime.PolicyRevision, "TLS": tlsFiles(prefix)}
	}
	address := reserveIAMProcessLoopbackAddress(t)
	inventory := writeJSON("reference-inventory.json", []map[string]any{{"tenant_id": tenantA.String(), "resource_id": "wr32-owned-resource", "ready": true}})
	receiverConfig := writeJSON("reference-receiver.json", map[string]any{"registry_file": registration, "registry_sha256": registry.Digest(), "iam": iamConfig("reference-client"), "inventory_file": inventory, "listen_address": address, "server_tls": tlsFiles("reference-server")})
	binary := filepath.Join(run, "private/workload-grpc")
	log, err := os.OpenFile(filepath.Join(directory, "reference-receiver.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-mode", "reference-receiver", "-config", receiverConfig)
	command.Env = append(os.Environ(), "GOMAXPROCS=2", "GOMEMLIMIT=256MiB")
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Error("reference receiver failed to exit cleanly")
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("reference receiver required forced termination")
		}
		log.Close()
		recordReference(t, run, map[string]any{"process": "wr32-reference-receiver", "event": "exited", "exit_code": command.ProcessState.ExitCode()})
	})
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	binaryDigest := sha256.Sum256(executable)
	recordReference(t, run, map[string]any{"process": "wr32-reference-receiver", "event": "started", "pid": command.Process.Pid, "binary_sha256": hex.EncodeToString(binaryDigest[:])})
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("reference receiver did not start")
	}
	secret := filepath.Join(directory, "reference-subject.secret")
	writeReferencePrivate(t, secret, []byte(login.AccessToken))
	request := map[string]any{"tenantId": tenantA.String(), "subjectId": actorID.String(), "resourceId": "wr32-owned-resource", "requestId": uuid.NewString()}
	requestFile := writeJSON("reference-request.json", request)
	callerConfig := writeJSON("reference-caller.json", map[string]any{"registry_file": registration, "registry_sha256": registry.Digest(), "iam": iamConfig("gateway-client"), "target_address": address, "target_server_name": receiverDNS, "credential_file": secret, "request_file": requestFile})
	invoke := func(t *testing.T, mode, want string) {
		t.Helper()
		call, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		p := exec.CommandContext(call, binary, "-mode", mode, "-config", callerConfig)
		p.Env = append(os.Environ(), "GOMAXPROCS=2", "GOMEMLIMIT=256MiB")
		output, err := p.CombinedOutput()
		if want == "pass" {
			if err != nil || !strings.Contains(string(output), "result: pass") {
				t.Fatalf("%s failed: %v; sanitized result %s", mode, err, output)
			}
		} else if err == nil || !strings.Contains(string(output), want) {
			t.Fatalf("%s expected %s, got exit %v; sanitized result %s", mode, want, err, output)
		}
		recordReference(t, run, map[string]any{"test": t.Name(), "process": mode, "pid": p.Process.Pid, "exit_code": p.ProcessState.ExitCode(), "binary_sha256": hex.EncodeToString(binaryDigest[:]), "expected": want, "pass": true})
	}
	t.Run("new Workload-only target", func(t *testing.T) { invoke(t, "reference-workload-caller", "pass") })
	t.Run("new delegated subject target", func(t *testing.T) { invoke(t, "reference-caller", "pass") })
	t.Run("owner rejects resource outside its inventory", func(t *testing.T) {
		request["resourceId"] = "other-resource"
		writeJSON("reference-request.json", request)
		invoke(t, "reference-caller", "PermissionDenied")
		request["resourceId"] = "wr32-owned-resource"
		writeJSON("reference-request.json", request)
	})
	t.Run("cross Tenant rejects same caller and subject", func(t *testing.T) {
		request["tenantId"] = tenantB.String()
		writeJSON("reference-request.json", request)
		invoke(t, "reference-caller", "PermissionDenied")
		request["tenantId"] = tenantA.String()
		writeJSON("reference-request.json", request)
	})
	for _, tc := range []struct {
		name, mode string
		id         uuid.UUID
	}{{"receiver target grant despite Verify ingress", "reference-workload-caller", receiverGrant}, {"receiver target grant on delegation", "reference-caller", receiverGrant}, {"caller Workload-only grant", "reference-workload-caller", statusGrant}, {"caller delegated grant", "reference-caller", delegatedGrant}, {"receiver Verify ingress", "reference-workload-caller", verifyGrant}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := owner.Exec(ctx, `UPDATE workload_grants SET status='revoked' WHERE id=$1`, tc.id); err != nil {
				t.Fatal(err)
			}
			invoke(t, tc.mode, "PermissionDenied")
			if _, err := owner.Exec(ctx, `UPDATE workload_grants SET status='active',version=version+1 WHERE id=$1`, tc.id); err != nil {
				t.Fatal(err)
			}
			invoke(t, tc.mode, "pass")
		})
	}
	t.Log("WR32_NEW_TARGET_FORMAL_PASS: reviewed registration, independent authority, Workload-only and subject delegation use the unchanged mechanism; owner and current grants enforce denial")
}
