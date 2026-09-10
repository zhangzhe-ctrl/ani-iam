//go:build integration

package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

const envoyRealIAMOperationID = "createSandboxCodeRun"

type envoyRealIAMCredentialBundle struct {
	ValidSecret   string `json:"valid_secret"`
	ExpiredSecret string `json:"expired_secret"`
	RevokedSecret string `json:"revoked_secret"`
	DeniedSecret  string `json:"denied_secret"`
	ValidKeyID    string `json:"valid_key_id"`
}

func TestEnvoyAdapterIndependentProcessUsesRealIAMRestrictedPostgresAndRedis(t *testing.T) {
	adapterDirectory := strings.TrimSpace(os.Getenv("DP2_ANI_ENVOY_ADAPTER_DIR"))
	if adapterDirectory == "" {
		t.Skip("DP2_ANI_ENVOY_ADAPTER_DIR must name the fixed ANI DP2 Envoy Adapter directory")
	}
	ctx := context.Background()
	environment := newPostgresEnvironment(t)
	redisClient := newVerticalSliceRedisClient(t, ctx)
	const redisNamespace = "ani-iam:dp2-10:envoy-real-iam"

	tenantID := uuid.MustParse("0199d080-6100-7001-9000-000000000001")
	actorID := uuid.MustParse("0199d080-6100-7001-9000-000000000002")
	actorMembershipID := uuid.MustParse("0199d080-6100-7001-9000-000000000003")
	allowedRoleID := uuid.MustParse("0199d080-6100-7001-9000-000000000004")
	deniedRoleID := uuid.MustParse("0199d080-6100-7001-9000-000000000005")
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedTenantAuthorizationBoundary(
		t, ctx, environment.runtimePool, tenantID, allowedRoleID, now,
		[]uuid.UUID{actorID}, []uuid.UUID{actorMembershipID},
	)
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO tenant_lifecycle_projections
			(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until)
			VALUES ($1,'active',1,$2,$2,$3)`, []any{tenantID, now, now.Add(time.Hour)}},
		{`INSERT INTO tenant_roles
			(tenant_id,id,code,system_role,system_definition_version,version,created_at,updated_at)
			VALUES ($1,$2,'envoy-denied',false,1,1,$3,$3)`, []any{tenantID, deniedRoleID, now}},
		{`INSERT INTO tenant_role_permissions
			(tenant_id,role_id,scope,resource,action,created_at)
			VALUES ($1,$2,'tenant','instances','create',$3)`, []any{tenantID, allowedRoleID, now}},
	} {
		if _, err := owner.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed real IAM Envoy boundary: %v", err)
		}
	}

	dataSet := data.NewData(environment.runtimePool)
	limiter, err := data.NewRedisAPIKeyCreationLimiter(redisClient, data.RedisAPIKeyCreationLimiterConfig{
		Namespace: redisNamespace,
		Limit:     biz.APIKeyCreationRateLimit,
		Window:    biz.APIKeyCreationRateWindow,
	})
	if err != nil {
		t.Fatalf("NewRedisAPIKeyCreationLimiter() error = %v", err)
	}
	aggregator, err := data.NewRedisAPIKeyUsageAggregator(
		redisClient, dataSet, data.RedisAPIKeyUsageConfig{Namespace: redisNamespace, BatchSize: 32},
	)
	if err != nil {
		t.Fatalf("NewRedisAPIKeyUsageAggregator() error = %v", err)
	}
	principals := biz.NewTenantWorkloadUsecase(
		data.NewPostgresTenantWorkloadUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), limiter,
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "envoy-real-iam", CorrelationID: "envoy-real-iam", DecisionID: "envoy-real-iam",
	}
	createPrincipal := func(name string, roleID uuid.UUID) biz.TenantWorkload {
		t.Helper()
		result, createErr := principals.CreateTenantWorkload(ctx, mustTenantScope(t, tenantID), biz.CreateTenantWorkloadCommand{IdempotencyKey: uuid.NewString(),
			Name: name, RoleIDs: []uuid.UUID{roleID}, Actor: actor,
		})
		if createErr != nil {
			t.Fatalf("CreateTenantWorkload(%s) error = %v", name, createErr)
		}
		return result.Principal
	}
	createKey := func(usecase *biz.TenantWorkloadUsecase, principalID uuid.UUID, key string, expiresAt time.Time) biz.CreateAPIKeyResult {
		t.Helper()
		result, createErr := usecase.CreateAPIKey(ctx, mustTenantScope(t, tenantID), biz.CreateAPIKeyCommand{
			PrincipalID: principalID, NeverExpires: expiresAt.IsZero(), ExpiresAt: expiresAt,
			IdempotencyKey: key, Actor: actor,
		})
		if createErr != nil {
			t.Fatalf("CreateAPIKey(%s) error = %v", key, createErr)
		}
		return result
	}

	allowedPrincipal := createPrincipal("Envoy Allowed", allowedRoleID)
	deniedPrincipal := createPrincipal("Envoy Denied", deniedRoleID)
	validKey := createKey(principals, allowedPrincipal.ID, "envoy-valid", time.Time{})
	pastPrincipals := biz.NewTenantWorkloadUsecase(
		data.NewPostgresTenantWorkloadUnitOfWork(dataSet), data.NewUUIDv7Generator(),
		fixedClock{now: now.Add(-2 * time.Hour)}, limiter,
	)
	expiredKey := createKey(pastPrincipals, allowedPrincipal.ID, "envoy-expired", now.Add(-time.Hour))
	revokedKey := createKey(principals, allowedPrincipal.ID, "envoy-revoked", time.Time{})
	if _, err := principals.RevokeAPIKey(ctx, mustTenantScope(t, tenantID), biz.RevokeAPIKeyCommand{
		KeyID: revokedKey.APIKey.ID, IdempotencyKey: "envoy-revoke", Actor: actor,
	}); err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	deniedKey := createKey(principals, deniedPrincipal.ID, "envoy-denied", time.Time{})

	registry, err := data.NewTargetOperationRegistry(data.TargetPolicyRevision)
	if err != nil {
		t.Fatalf("NewTargetOperationRegistry() error = %v", err)
	}
	authorization := biz.NewAuthorizationUsecase(
		registry, nil, data.NewPostgresAuthorizationReader(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), aggregator,
	)
	serverRuntime := startRealIAMAuthorizationServer(t, service.NewAuthorizationService(authorization))
	bundle := envoyRealIAMCredentialBundle{
		ValidSecret: validKey.Secret, ExpiredSecret: expiredKey.Secret,
		RevokedSecret: revokedKey.Secret, DeniedSecret: deniedKey.Secret,
		ValidKeyID: validKey.APIKey.ID.String(),
	}
	runExternalEnvoyAdapterRealIAMTest(t, adapterDirectory, serverRuntime, tenantID, allowedPrincipal.ID, bundle)

	pending, err := redisClient.ZCard(ctx, redisNamespace+":api-key:usage").Result()
	if err != nil || pending != 1 {
		t.Fatalf("real Envoy/IAM pending API key usage = %d / %v", pending, err)
	}
	var lastUsedAt *time.Time
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2
	`, tenantID, validKey.APIKey.ID).Scan(&lastUsedAt); err != nil || lastUsedAt != nil {
		t.Fatalf("last_used_at before real Envoy/IAM flush = %v / %v", lastUsedAt, err)
	}
	if processed, err := aggregator.Flush(ctx); err != nil || processed != 1 {
		t.Fatalf("Flush() after real Envoy/IAM = %d / %v", processed, err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2
	`, tenantID, validKey.APIKey.ID).Scan(&lastUsedAt); err != nil || lastUsedAt == nil {
		t.Fatalf("last_used_at after real Envoy/IAM flush = %v / %v", lastUsedAt, err)
	}
}

type realIAMAuthorizationRuntime struct {
	address        string
	serverName     string
	caFile         string
	clientCertFile string
	clientKeyFile  string
}

func startRealIAMAuthorizationServer(t *testing.T, authorization *service.AuthorizationService) realIAMAuthorizationRuntime {
	t.Helper()
	directory := t.TempDir()
	caCertificate, caKey, caFile := writeProcessE2ECertificateAuthority(t, directory)
	serverName := "iam.dp2-10.test"
	serverCertFile, serverKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "iam-server", serverName, x509.ExtKeyUsageServerAuth, caCertificate, caKey,
	)
	clientCertFile, clientKeyFile := writeProcessE2ELeafCertificate(
		t, directory, "envoy-client", "envoy-authz-adapter", x509.ExtKeyUsageClientAuth, caCertificate, caKey,
	)
	serverCertificate, err := tls.LoadX509KeyPair(serverCertFile, serverKeyFile)
	if err != nil {
		t.Fatalf("load IAM server certificate: %v", err)
	}
	clientRoots := x509.NewCertPool()
	clientRoots.AddCert(caCertificate)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for real IAM authorization server: %v", err)
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientRoots,
	})))
	iamv1.RegisterAuthorizationServiceServer(grpcServer, authorization)
	t.Cleanup(grpcServer.Stop)
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve real IAM authorization fixture: %v", err)
		}
	}()
	return realIAMAuthorizationRuntime{
		address: listener.Addr().String(), serverName: serverName, caFile: caFile,
		clientCertFile: clientCertFile, clientKeyFile: clientKeyFile,
	}
}

func runExternalEnvoyAdapterRealIAMTest(
	t *testing.T,
	adapterDirectory string,
	runtime realIAMAuthorizationRuntime,
	tenantID uuid.UUID,
	principalID uuid.UUID,
	bundle envoyRealIAMCredentialBundle,
) {
	t.Helper()
	credentialReader, credentialWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create credential pipe: %v", err)
	}
	if err := json.NewEncoder(credentialWriter).Encode(bundle); err != nil {
		credentialReader.Close()
		credentialWriter.Close()
		t.Fatalf("encode credential pipe: %v", err)
	}
	if err := credentialWriter.Close(); err != nil {
		credentialReader.Close()
		t.Fatalf("close credential pipe writer: %v", err)
	}
	defer credentialReader.Close()

	commandContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(
		commandContext, "go", "test", "-tags=integration", "-run", "^TestEnvoyAdapterExternalRealIAM$", "-count=1", "-v", ".",
	)
	command.Dir = adapterDirectory
	command.ExtraFiles = []*os.File{credentialReader}
	command.Env = append(os.Environ(),
		"DP2_REAL_IAM_ADDRESS="+runtime.address,
		"DP2_REAL_IAM_SERVER_NAME="+runtime.serverName,
		"DP2_REAL_IAM_CA_FILE="+runtime.caFile,
		"DP2_REAL_IAM_CLIENT_CERT_FILE="+runtime.clientCertFile,
		"DP2_REAL_IAM_CLIENT_KEY_FILE="+runtime.clientKeyFile,
		"DP2_REAL_IAM_POLICY_REVISION="+data.TargetPolicyRevision,
		"DP2_REAL_IAM_OPERATION_ID="+envoyRealIAMOperationID,
		"DP2_REAL_IAM_TENANT_ID="+tenantID.String(),
		"DP2_REAL_IAM_PRINCIPAL_ID="+principalID.String(),
		"DP2_REAL_IAM_RESOURCE_ID=service-envoy-real-iam",
		"DP2_REAL_IAM_CREDENTIAL_FD=3",
	)
	output, err := command.CombinedOutput()
	redactedOutput := string(output)
	for _, secret := range []string{bundle.ValidSecret, bundle.ExpiredSecret, bundle.RevokedSecret, bundle.DeniedSecret} {
		redactedOutput = strings.ReplaceAll(redactedOutput, secret, "[REDACTED_API_KEY]")
	}
	if err != nil {
		t.Fatalf("external Envoy Adapter real-IAM test failed: %v\n%s", err, redactedOutput)
	}
	if !strings.Contains(redactedOutput, "DP2_ENVOY_REAL_IAM_PASS") {
		t.Fatalf("external Envoy Adapter real-IAM marker missing:\n%s", redactedOutput)
	}
}
