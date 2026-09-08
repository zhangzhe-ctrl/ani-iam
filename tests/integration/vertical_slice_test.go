//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

func TestTargetVerticalSliceSchema(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()

	expectedTables := []string{
		"verified_emails",
		"identities",
		"password_credentials",
		"tenant_lifecycle_projections",
		"tenant_role_permissions",
		"sessions",
		"session_grants",
		"refresh_token_families",
		"refresh_tokens",
	}
	for _, tableName := range expectedTables {
		var exists bool
		if err := environment.runtimePool.QueryRow(ctx, `
			SELECT to_regclass('public.' || $1) IS NOT NULL
		`, tableName).Scan(&exists); err != nil {
			t.Fatalf("query %s existence: %v", tableName, err)
		}
		if !exists {
			t.Fatalf("target vertical-slice table %s does not exist", tableName)
		}
	}
}

func TestPostgresFailureMapsToStableIAMUnavailable(t *testing.T) {
	environment := newPostgresEnvironment(t)
	environment.runtimePool.Close()
	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	usecase := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		acceptingIntegrationPasswordVerifier{},
		allowingIntegrationLoginThrottle{},
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		&recordingAccessTokenIssuer{},
		staticIntegrationSecretGenerator{},
		&fixedIDGenerator{},
		fixedClock{}, allowingAPIKeyUsageObserver{})

	authentication := service.NewAuthenticationService(usecase)

	_, err := authentication.PasswordLogin(context.Background(), &iamv1.PasswordLoginRequest{
		Account:        "user@example.com",
		Password:       "correct-password",
		Audience:       iamv1.Audience_AUDIENCE_CONSOLE,
		Boundary:       &iamv1.Boundary{Boundary: &iamv1.Boundary_Tenant{Tenant: &iamv1.TenantBoundary{TenantId: tenantID.String()}}},
		SourceIp:       "203.0.113.10",
		IdempotencyKey: "login-postgres-down",
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.Unavailable || len(grpcStatus.Details()) != 1 {
		t.Fatalf("PostgreSQL failure status = %s details=%#v", grpcStatus.Code(), grpcStatus.Details())
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != "IAM_UNAVAILABLE" || info.GetMetadata()["dependency"] != "authentication" {
		t.Fatalf("PostgreSQL failure ErrorInfo = %#v", info)
	}
}

func TestPostgresTargetLoginAndAuthorization(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	passwordHasher := data.NewArgon2idPasswordHasher()
	passwordHash, err := passwordHasher.Hash("correct-password")
	if err != nil {
		t.Fatalf("hash target password fixture: %v", err)
	}
	fixture := seedTargetLoginFixture(t, ctx, environment, passwordHash)
	now := time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	tokenCodec, err := data.NewJWXAccessTokenCodec(
		"dp2-05-integration-key",
		privateKey,
		map[string]ed25519.PublicKey{"dp2-05-integration-key": privateKey.Public().(ed25519.PublicKey)},
		"ani-iam",
		fixedClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewJWXAccessTokenCodec() error = %v", err)
	}
	redisClient := newVerticalSliceRedisClient(t, ctx)
	loginThrottle, err := data.NewRedisLoginThrottle(redisClient, data.RedisLoginThrottleConfig{
		Namespace: "ani-iam:dp2-05:vertical-slice",
		Limit:     5,
		Window:    15 * time.Minute,
		BaseDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRedisLoginThrottle() error = %v", err)
	}

	authenticationIDs := append([]uuid.UUID{fixture.failureAuditID}, fixture.loginIDs...)
	authentication := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
		passwordHasher,
		loginThrottle,
		data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
		tokenCodec,
		staticIntegrationSecretGenerator{secret: "opaque-refresh-secret"},
		&fixedIDGenerator{ids: authenticationIDs},
		fixedClock{now: now}, allowingAPIKeyUsageObserver{})

	_, err = authentication.PasswordLogin(ctx, biz.PasswordLoginCommand{
		Account:        "user@example.com",
		Password:       "wrong-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		DeviceName:     "integration-browser",
		IdempotencyKey: "login-postgres-invalid",
	})
	if !errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("PasswordLogin(invalid credential) error = %v, want %v", err, biz.ErrInvalidCredential)
	}
	var preLoginSessionCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&preLoginSessionCount); err != nil {
		t.Fatalf("query sessions after invalid credential: %v", err)
	}
	if preLoginSessionCount != 0 {
		t.Fatalf("sessions after invalid credential = %d, want 0", preLoginSessionCount)
	}
	loginCommand := biz.PasswordLoginCommand{
		Account:        " User@Example.COM ",
		Password:       "correct-password",
		Audience:       biz.AudienceConsole,
		TenantID:       tenantA,
		SourceIP:       netip.MustParseAddr("203.0.113.10"),
		DeviceName:     "integration-browser",
		IdempotencyKey: "login-postgres-1",
	}
	_, err = authentication.PasswordLogin(ctx, loginCommand)
	var rateLimit *biz.AuthenticationRateLimitError
	if !errors.Is(err, biz.ErrAuthenticationRateLimited) || !errors.As(err, &rateLimit) ||
		rateLimit.LimitScope != "password_account" || rateLimit.RetryAfter <= 0 || rateLimit.RetryAfter > 60*time.Millisecond {
		t.Fatalf("PasswordLogin() during increasing cooldown error = %#v, want account retry in (0, 60ms]", err)
	}
	timer := time.NewTimer(rateLimit.RetryAfter + 10*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatalf("wait for login cooldown: %v", ctx.Err())
	case <-timer.C:
	}
	login, err := authentication.PasswordLogin(ctx, loginCommand)
	if err != nil {
		t.Fatalf("PasswordLogin() through runtime role after cooldown: %v", err)
	}
	if login.Session.ID != fixture.loginIDs[0] || login.Grant.ID != fixture.loginIDs[1] {
		t.Fatalf("login session/grant = %s/%s", login.Session.ID, login.Grant.ID)
	}
	verifiedClaims, err := tokenCodec.Verify(ctx, login.AccessToken)
	if err != nil {
		t.Fatalf("verify issued Access Token: %v", err)
	}
	if verifiedClaims.Subject != actorID || verifiedClaims.TenantID != tenantA ||
		verifiedClaims.SessionID != login.Session.ID || verifiedClaims.GrantID != login.Grant.ID ||
		verifiedClaims.GrantVersion != login.Grant.Version {
		t.Fatalf("verified Access Token claims = %#v", verifiedClaims)
	}

	var sessionCount, grantCount, auditCount int
	var passwordSessionMethods []string
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = $1`, login.Session.ID).Scan(&sessionCount); err != nil {
		t.Fatalf("query committed Session: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM session_grants WHERE tenant_id = $1 AND id = $2`, tenantA, login.Grant.ID).Scan(&grantCount); err != nil {
		t.Fatalf("query committed Grant: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM iam_audit_events WHERE tenant_id = $1 AND event_id = $2`, tenantA, fixture.loginIDs[5]).Scan(&auditCount); err != nil {
		t.Fatalf("query committed login Audit: %v", err)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT authn_methods FROM sessions WHERE id = $1`, login.Session.ID).Scan(&passwordSessionMethods); err != nil {
		t.Fatalf("query committed Session authn methods: %v", err)
	}
	if sessionCount != 1 || grantCount != 1 || auditCount != 1 {
		t.Fatalf("atomic login rows = session:%d grant:%d audit:%d, want 1/1/1", sessionCount, grantCount, auditCount)
	}
	if len(passwordSessionMethods) != 1 || passwordSessionMethods[0] != "password" {
		t.Fatalf("password Session authn_methods = %#v, want [password]", passwordSessionMethods)
	}

	authorization := biz.NewAuthorizationUsecase(
		targetPolicyRegistry{},
		tokenCodec,
		data.NewPostgresAuthorizationReader(data.NewData(environment.runtimePool)),
		&fixedIDGenerator{ids: []uuid.UUID{fixture.decisionID}},
		fixedClock{now: now.Add(time.Minute)}, allowingAPIKeyUsageObserver{})

	decision, err := authorization.CheckPermission(ctx, biz.CheckPermissionCommand{
		RawCredential:  login.AccessToken,
		OperationID:    "listInstances",
		PolicyRevision: testIntegrationPolicyRevision,
		TargetTenantID: tenantA,
	})
	if err != nil {
		t.Fatalf("CheckPermission() through runtime role: %v", err)
	}
	if !decision.Allowed || decision.Reason != biz.AuthorizationReasonAllowed || decision.DecisionID != fixture.decisionID {
		t.Fatalf("authorization decision = %#v", decision)
	}

	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	if _, err := seedPool.Exec(ctx, `DELETE FROM tenant_role_permissions WHERE tenant_id = $1 AND resource = 'instances' AND action = 'read'`, tenantA); err != nil {
		seedPool.Close()
		t.Fatalf("remove target permission fixture: %v", err)
	}
	seedPool.Close()
	deniedAuthorization := biz.NewAuthorizationUsecase(
		targetPolicyRegistry{},
		tokenCodec,
		data.NewPostgresAuthorizationReader(data.NewData(environment.runtimePool)),
		&fixedIDGenerator{ids: []uuid.UUID{fixture.deniedDecisionID, fixture.deniedAuditID}},
		fixedClock{now: now.Add(time.Minute)}, allowingAPIKeyUsageObserver{})

	denied, err := deniedAuthorization.CheckPermission(ctx, biz.CheckPermissionCommand{
		RawCredential:  login.AccessToken,
		OperationID:    "listInstances",
		PolicyRevision: testIntegrationPolicyRevision,
		TargetTenantID: tenantA,
	})
	if err != nil {
		t.Fatalf("CheckPermission(denied) through runtime role: %v", err)
	}
	if denied.Allowed || denied.Reason != biz.AuthorizationReasonPermissionDenied || denied.DecisionID != fixture.deniedDecisionID {
		t.Fatalf("denied authorization decision = %#v", denied)
	}

	t.Run("required login Audit failure rolls every session row back", func(t *testing.T) {
		secondIDs := []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7301-9000-000000000001"),
			uuid.MustParse("0198f062-b76d-7301-9000-000000000002"),
			uuid.MustParse("0198f062-b76d-7301-9000-000000000003"),
			uuid.MustParse("0198f062-b76d-7301-9000-000000000004"),
			uuid.MustParse("0198f062-b76d-7301-9000-000000000005"),
			fixture.loginIDs[5],
		}
		usecase := biz.NewAuthenticationUsecase(
			data.NewPostgresPasswordLoginReader(data.NewData(environment.runtimePool)),
			passwordHasher,
			loginThrottle,
			data.NewPostgresLoginUnitOfWork(data.NewData(environment.runtimePool)),
			&recordingAccessTokenIssuer{token: "second-access-token"},
			staticIntegrationSecretGenerator{secret: "different-refresh-secret"},
			&fixedIDGenerator{ids: secondIDs},
			fixedClock{now: now.Add(2 * time.Minute)}, allowingAPIKeyUsageObserver{})

		_, err := usecase.PasswordLogin(ctx, biz.PasswordLoginCommand{
			Account:        "user@example.com",
			Password:       "correct-password",
			Audience:       biz.AudienceConsole,
			TenantID:       tenantA,
			SourceIP:       netip.MustParseAddr("203.0.113.10"),
			IdempotencyKey: "login-postgres-audit-rollback",
		})
		if !errors.Is(err, biz.ErrAuditConflict) {
			t.Fatalf("PasswordLogin() error = %v, want %v", err, biz.ErrAuditConflict)
		}
		var count int
		if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = $1`, secondIDs[0]).Scan(&count); err != nil {
			t.Fatalf("query rolled-back Session: %v", err)
		}
		if count != 0 {
			t.Fatalf("rolled-back Session count = %d, want 0", count)
		}
	})

	t.Run("Tenant B cannot consume Tenant A authorization state", func(t *testing.T) {
		scopeB := mustTenantScope(t, tenantB)
		reader := data.NewPostgresAuthorizationReader(data.NewData(environment.runtimePool))
		state, err := reader.LookupAuthorization(ctx, scopeB, biz.AuthorizationLookup{
			PrincipalID:          actorID,
			SessionID:            login.Session.ID,
			GrantID:              login.Grant.ID,
			ExpectedGrantVersion: login.Grant.Version,
			Resource:             "instances",
			Actions:              []string{"read"},
		})
		if err != nil {
			t.Fatalf("Tenant B authorization lookup: %v", err)
		}
		if state.PermissionAllowed {
			t.Fatalf("Tenant B read Tenant A authorization state: %#v", state)
		}

		var leakedTenant uuid.UUID
		err = environment.runtimePool.QueryRow(ctx, `
			SELECT membership.tenant_id
			FROM verified_emails AS email
			JOIN principals AS principal ON principal.id = email.principal_id
			JOIN tenant_memberships AS membership ON membership.principal_id = principal.id
			WHERE $1::uuid IS NOT NULL AND email.normalized_email = $2
		`, tenantB, "user@example.com").Scan(&leakedTenant)
		if err != nil {
			t.Fatalf("execute deliberate login tenant-predicate mutant: %v", err)
		}
		if leakedTenant != tenantA {
			t.Fatalf("query mutant leaked Tenant %s, want Tenant A %s", leakedTenant, tenantA)
		}
	})
}

const testIntegrationPolicyRevision = "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa"

type targetLoginFixture struct {
	membershipID     uuid.UUID
	failureAuditID   uuid.UUID
	loginIDs         []uuid.UUID
	decisionID       uuid.UUID
	deniedDecisionID uuid.UUID
	deniedAuditID    uuid.UUID
}

func seedTargetLoginFixture(t *testing.T, ctx context.Context, environment *postgresEnvironment, passwordHash string) targetLoginFixture {
	t.Helper()
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	defer seedPool.Close()
	fixture := targetLoginFixture{
		membershipID:   uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
		failureAuditID: uuid.MustParse("0198f062-b76d-7101-9000-000000000009"),
		loginIDs: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-7101-9000-000000000001"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000002"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000003"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000004"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000005"),
			uuid.MustParse("0198f062-b76d-7101-9000-000000000006"),
		},
		decisionID:       uuid.MustParse("0198f062-b76d-7101-9000-000000000007"),
		deniedDecisionID: uuid.MustParse("0198f062-b76d-7101-9000-000000000008"),
		deniedAuditID:    uuid.MustParse("0198f062-b76d-7101-9000-00000000000a"),
	}
	identityID := uuid.MustParse("0198f062-b76d-7201-9000-000000000001")
	roleID := uuid.MustParse("0198f062-b76d-7201-9000-000000000002")
	bindingID := uuid.MustParse("0198f062-b76d-7201-9000-000000000003")
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO principals (id, principal_type, status, version, created_at, updated_at) VALUES ($1, 'human', 'active', 1, now(), now())`, []any{actorID}},
		{`INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at) VALUES ($1, 'active', 1, now(), now())`, []any{tenantA}},
		{`INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at) VALUES ($1, 'active', 1, now(), now())`, []any{tenantB}},
		{`INSERT INTO tenant_lifecycle_projections (tenant_id, status, lifecycle_version, effective_at, observed_at, fresh_until) VALUES ($1, 'active', 1, now(), now(), now() + interval '5 minutes')`, []any{tenantA}},
		{`INSERT INTO tenant_lifecycle_projections (tenant_id, status, lifecycle_version, effective_at, observed_at, fresh_until) VALUES ($1, 'active', 1, now(), now(), now() + interval '5 minutes')`, []any{tenantB}},
		{`INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, now(), now())`, []any{tenantA, fixture.membershipID, actorID}},
		{`INSERT INTO verified_emails (principal_id, normalized_email, verified_at, created_at, updated_at) VALUES ($1, 'user@example.com', now(), now(), now())`, []any{actorID}},
		{`INSERT INTO identities (id, principal_id, provider, issuer, subject, status, version, created_at, updated_at) VALUES ($1, $2, 'password', 'ani-local', 'user@example.com', 'active', 1, now(), now())`, []any{identityID, actorID}},
		{`INSERT INTO password_credentials (principal_id, identity_id, password_hash, algorithm, version, created_at, updated_at) VALUES ($1, $2, $3, 'argon2id', 1, now(), now())`, []any{actorID, identityID, passwordHash}},
		{`INSERT INTO tenant_roles (tenant_id, id, code, system_role, system_definition_version, version, created_at, updated_at) VALUES ($1, $2, 'tenant-admin', true, 1, 1, now(), now())`, []any{tenantA, roleID}},
		{`INSERT INTO tenant_role_bindings (tenant_id, id, membership_id, role_id, version, created_at, updated_at) VALUES ($1, $2, $3, $4, 1, now(), now())`, []any{tenantA, bindingID, fixture.membershipID, roleID}},
		{`INSERT INTO tenant_role_permissions (tenant_id, role_id, scope, resource, action, created_at) VALUES ($1, $2, 'tenant', 'instances', 'read', now())`, []any{tenantA, roleID}},
	}
	for _, statement := range statements {
		if _, err := seedPool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed target login fixture: %v", err)
		}
	}
	return fixture
}

type acceptingIntegrationPasswordVerifier struct{}

func (acceptingIntegrationPasswordVerifier) Verify(string, string) (bool, error) { return true, nil }
func (acceptingIntegrationPasswordVerifier) VerifyUnknown(string) error          { return nil }
func (acceptingIntegrationPasswordVerifier) Hash(string) (string, error) {
	return "test-password-hash", nil
}

type allowingIntegrationLoginThrottle struct{}

func (allowingIntegrationLoginThrottle) Check(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}
func (allowingIntegrationLoginThrottle) RecordFailure(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}
func (allowingIntegrationLoginThrottle) Reset(context.Context, biz.LoginThrottleAttempt) error {
	return nil
}

type recordingAccessTokenIssuer struct {
	token  string
	claims biz.AccessTokenClaims
}

func (i *recordingAccessTokenIssuer) Issue(_ context.Context, claims biz.AccessTokenClaims) (string, error) {
	i.claims = claims
	return i.token, nil
}
func (*recordingAccessTokenIssuer) IssuePasswordAction(context.Context, biz.PasswordActionTokenClaims) (string, error) {
	return "test-password-action-token", nil
}
func (*recordingAccessTokenIssuer) VerifyPasswordAction(context.Context, string) (biz.PasswordActionTokenClaims, error) {
	return biz.PasswordActionTokenClaims{}, biz.ErrPasswordActionInvalid
}

type staticIntegrationSecretGenerator struct{ secret string }

func (g staticIntegrationSecretGenerator) NewSecret() (string, error) { return g.secret, nil }

type staticIntegrationAccessTokenVerifier struct {
	claims biz.AccessTokenClaims
	err    error
}

func (v staticIntegrationAccessTokenVerifier) Verify(context.Context, string) (biz.AccessTokenClaims, error) {
	return v.claims, v.err
}

type targetPolicyRegistry struct{}

func (targetPolicyRegistry) Revision() string { return testIntegrationPolicyRevision }

func (targetPolicyRegistry) Lookup(operationID string) (biz.AuthorizationPolicy, bool) {
	if operationID != "listInstances" {
		return biz.AuthorizationPolicy{}, false
	}
	return biz.AuthorizationPolicy{
		OperationID: operationID, Resource: "instances", Actions: []string{"read"}, Scope: biz.PermissionScopeTenant,
		CredentialKinds: []biz.CredentialKind{biz.CredentialKindAccessToken}, PrincipalKinds: []biz.PrincipalType{biz.PrincipalTypeHuman},
	}, true
}

func newVerticalSliceRedisClient(t *testing.T, ctx context.Context) *redis.Client {
	t.Helper()
	container, err := testcontainers.Run(
		ctx,
		redisImage,
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		t.Fatalf("start pinned Redis container: %v", err)
	}
	t.Cleanup(func() {
		terminateContext, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(terminateContext)); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})
	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("Redis endpoint: %v", err)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         endpoint,
		MaxRetries:   -1,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping pinned Redis container: %v", err)
	}
	return client
}
