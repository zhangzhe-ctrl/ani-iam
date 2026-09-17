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
	"fmt"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type wr21Environment struct {
	*wr20Environment
	adminRoles, invokeRoles []uuid.UUID
}
type wr21BootstrapExtra func(string, *x509.Certificate, ed25519.PrivateKey, *conf.Bootstrap, *biz.WorkloadBootstrapManifest)

func newWR21Environment(t *testing.T, extras ...wr21BootstrapExtra) *wr21Environment {
	t.Helper()
	run, goal := isolatedRun(t)
	if goal != "wr21" && goal != "wr32" {
		t.Fatal("WR21 dedicated run required")
	}
	e := &wr21Environment{wr20Environment: &wr20Environment{run: run, password: randomPassword(t)}}
	ctx := context.Background()
	db := newPostgresEnvironment(t)
	resources, err := os.ReadFile(filepath.Join(run, "resources.jsonl"))
	if err != nil {
		t.Fatal("registered resource inventory unavailable")
	}
	for _, line := range bytes.Split(resources, []byte("\n")) {
		var r map[string]string
		if json.Unmarshal(line, &r) == nil && r["event"] == "created" && r["kind"] == "postgres" && r["test"] == t.Name() {
			if e.postgresID != "" {
				t.Fatal("ambiguous PostgreSQL resource")
			}
			e.postgresID = r["container_id"]
		}
	}
	if e.postgresID == "" {
		t.Fatal("PostgreSQL resource was not registered")
	}
	e.owner = mustPool(t, db.migrationDSN(primaryDB))
	t.Cleanup(e.owner.Close)
	hash, err := data.NewArgon2idPasswordHasher().Hash(e.password)
	if err != nil {
		t.Fatal("Human seed hash failed")
	}
	e.humans, e.adminRoles = seedReferenceHumans(t, e.owner, hash)
	e.seedWR21Roles(t)
	// Stable historic helper supplies only fresh data in this new private DB.
	// Rename its test contacts to this ticket; no existing environment is used.
	for i, id := range e.humans {
		email := fmt.Sprintf("wr21-%d@example.test", i)
		e.accounts = append(e.accounts, email)
		if _, err = e.owner.Exec(ctx, `UPDATE verified_emails SET normalized_email=$2 WHERE principal_id=$1`, id, email); err != nil {
			t.Fatal(err)
		}
		if _, err = e.owner.Exec(ctx, `UPDATE identities SET subject=$2 WHERE principal_id=$1`, id, email); err != nil {
			t.Fatal(err)
		}
	}
	e.iamRedis, e.redisContainer = newIsolatedRedis(t, ctx)
	e.gatewayRedis, _ = newIsolatedRedis(t, ctx)
	aniDSN := newReferenceOwnerDatabase(t, run)
	gatewayAddress, adminAddress := reserveIAMProcessLoopbackAddress(t), reserveIAMProcessLoopbackAddress(t)
	target, _ := url.Parse("http://" + gatewayAddress)
	e.front = httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(target))
	t.Cleanup(e.front.Close)
	_, port, _ := net.SplitHostPort(e.front.Listener.Addr().String())
	e.origin = "https://console.wr21.test:" + port
	secret := randomPassword(t)
	e.dexSecret, e.dexPassword = secret, randomPassword(t)
	e.dexFault = &wr20OIDCFaults{}
	issuer, dexContainer := wr20Dex(t, run, secret, e.dexPassword, []string{e.origin + "/api/v1/auth/oidc/callback", e.origin + "/api/v1/auth/identity-links/oidc/callback"}, e.dexFault)
	e.issuer, e.dexContainer = issuer, dexContainer
	setup := formalIAMSetup{Environment: "wr21-authentication", TrustDomain: "wr21.test", GatewayDNS: "gateway.wr21.test"}
	setup.BeforeStart = func(binary, directory string, ca *x509.Certificate, key ed25519.PrivateKey, cfg *conf.Bootstrap) {
		cfg.Runtime.Oidc.IssuerUrl = issuer
		cfg.Runtime.Oidc.LoginRedirectUri = e.origin + "/api/v1/auth/oidc/callback"
		cfg.Runtime.Oidc.IdentityLinkRedirectUri = e.origin + "/api/v1/auth/identity-links/oidc/callback"
		writeReferencePrivate(t, cfg.Runtime.Oidc.ClientSecretFile, []byte(secret))
		cfg.Runtime.Redis.Namespace = "ani-iam:wr21:" + uuid.NewString()
		caDigest := sha256.Sum256(ca.Raw)
		manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: mustV7(t), Environment: setup.Environment, TrustDomain: setup.TrustDomain, CASHA256: hex.EncodeToString(caDigest[:]), ExpiresAt: time.Now().UTC().Add(time.Hour)}
		gateway := biz.BootstrapWorkload{PrincipalID: mustV7(t), BindingID: mustV7(t), Name: "wr21-gateway", DNSIdentity: setup.GatewayDNS}
		for _, method := range []string{"PasswordLogin", "RefreshSession", "LogoutSession", "SwitchTenant", "ListSessions", "BeginOIDCLogin", "CompleteOIDCLogin", "BeginOIDCIdentityLink", "CompleteOIDCIdentityLink", "RequestPasswordAction", "CompletePasswordAction"} {
			gateway.Grants = append(gateway.Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + method})
		}
		manifest.Workloads = []biz.BootstrapWorkload{gateway}
		for _, method := range []string{"CreateTenantWorkload", "GetTenantWorkload", "ListTenantWorkloads", "UpdateTenantWorkload", "CreateAPIKey", "ListAPIKeys", "RevokeAPIKey", "UpdateTenantMembership", "RemoveTenantMembership", "BindTenantRole", "UnbindTenantRole"} {
			manifest.Workloads[0].Grants = append(manifest.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		}
		for _, method := range []string{"ValidatePrincipal"} {
			manifest.Workloads[0].Grants = append(manifest.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/" + method})
		}
		manifest.Workloads[0].Grants = append(manifest.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/CheckPermission"})
		for _, extra := range extras {
			extra(directory, ca, key, cfg, &manifest)
		}
		raw, _ := json.Marshal(manifest)
		digest := sha256.Sum256(raw)
		mf, dsnf := filepath.Join(directory, "wr21-bootstrap.json"), filepath.Join(directory, "provisioner.secret")
		writeReferencePrivate(t, mf, raw)
		writeReferencePrivate(t, dsnf, []byte(postgresDSN(provisionerRole, db.provisionerPass, db.host, primaryDB, "wr21-bootstrap")))
		cmd := exec.Command(binary, "provision-workloads", "--registry-file", wr32RegistryPath(t), "--approved-registry-sha256", wr32Registry(t).Digest(), "--manifest", mf, "--approved-manifest-sha256", hex.EncodeToString(digest[:]), "--environment", setup.Environment, "--trust-domain", setup.TrustDomain, "--ca-file", cfg.Server.Grpc.Tls.ClientCaFile, "--dsn-file", dsnf)
		output, err := cmd.CombinedOutput()
		if err != nil {
			_ = os.WriteFile(filepath.Join(directory, "bootstrap.private.log"), output, 0o600)
			t.Fatal("formal WR21 bootstrap failed")
		}
		recordReference(t, run, map[string]any{"kind": "bootstrap", "manifest_sha256": hex.EncodeToString(digest[:]), "receipt_sha256": fmt.Sprintf("%x", sha256.Sum256(output)), "pass": true})
		certFile, keyFile := writeProcessE2ELeafCertificate(t, directory, "browser", "console.wr21.test", x509.ExtKeyUsageServerAuth, ca, key)
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			t.Fatal("HTTPS key pair invalid")
		}
		e.front.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
		e.front.StartTLS()
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		e.transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "console.wr21.test:"+port {
				return nil, fmt.Errorf("unregistered test authority")
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, e.front.Listener.Addr().String())
		}}
	}
	e.iam = startFormalIAM(t, db, e.iamRedis, setup)
	gateway := startReferenceProcess(t, run, "wr21-gateway-"+uuid.NewString(), map[string]string{"IAM_TARGET_MODE": "wr21", "IAM_TARGET_GRPC_ADDR": e.iam.config.Server.Grpc.Addr, "IAM_TARGET_TLS_SERVER_NAME": "iam.wr17-18.test", "IAM_WORKLOAD_ENVIRONMENT": setup.Environment, "IAM_WORKLOAD_TRUST_DOMAIN": setup.TrustDomain, "IAM_TARGET_TLS_CA_FILE": e.iam.config.Server.Grpc.Tls.ClientCaFile, "IAM_TARGET_TLS_CERT_FILE": filepath.Join(e.iam.directory, "gateway-client.pem"), "IAM_TARGET_TLS_KEY_FILE": filepath.Join(e.iam.directory, "gateway-client-key.pem"), "IAM_BROWSER_CONSOLE_ORIGIN": e.origin, "GATEWAY_LISTEN_ADDR": gatewayAddress, "GATEWAY_HEALTH_LISTEN_ADDR": adminAddress, "DATABASE_URL": aniDSN, "GATEWAY_REDIS_URL": referenceRedisURL(e.gatewayRedis), "ANI_AUTH_MODE": "auth_service"}, "ani-gateway")
	waitReferenceHTTP(t, "http://"+adminAddress+"/readyz", gateway)
	recordReference(t, run, map[string]any{"stage": "A", "origin": e.origin, "iam_binary": e.iam.binary, "gateway_binary": filepath.Join(run, "private", "ani-gateway"), "source": "formal IAM/Gateway; real PG/Redis; Dex startup dependency; SQL projection/admin/role seed only, no Workload or Key seed"})
	return e
}

// Only prerequisite Tenant/Human/Role projections are seeded. Every tested
// Workload and Key is created through formal management adapters.
func (e *wr21Environment) seedWR21Roles(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for i, tenant := range referenceTenants {
		role := mustV7(t)
		e.invokeRoles = append(e.invokeRoles, role)
		_, err := e.owner.Exec(ctx, `INSERT INTO tenant_roles(tenant_id,id,code,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,$2,'wr21-invoker',false,1,1,now(),now())`, tenant, role)
		if err != nil {
			t.Fatalf("seed custom role failed: %v", err)
		}
		_, err = e.owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) VALUES($1,$2,'tenant','inference-services','invoke',now())`, tenant, role)
		if err != nil {
			t.Fatal("seed explicit invoke permission failed")
		}
		_, err = e.owner.Exec(ctx, `INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at) SELECT $1,$2,scope,resource,action,now() FROM permission_catalog WHERE scope='tenant' AND resource IN ('iam.tenant-workloads','iam.api-keys','iam.memberships','iam.roles','iam.role-bindings') ON CONFLICT DO NOTHING`, tenant, e.adminRoles[i])
		if err != nil {
			t.Fatal("seed admin permissions failed")
		}
		_, err = e.owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET fresh_until=now()+interval '1 hour' WHERE tenant_id=$1`, tenant)
		if err != nil {
			t.Fatal("seed explicit isolated projection freshness failed")
		}
	}
	recordReference(t, e.run, map[string]any{"fixture_layer": "isolated SQL projection and prerequisite admins/roles only; WR23 producer not verified", "tenants": referenceTenants, "invoke_permission_automatic_builtin_assignment": false})
}
