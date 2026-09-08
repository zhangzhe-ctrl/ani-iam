//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

func TestServicePrincipalCreateUsesRestrictedPostgresAndOneAtomicTenantBoundary(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199ca10-2000-7001-9000-000000000001")
	actorID := uuid.MustParse("0199ca10-2000-7001-9000-000000000002")
	actorMembershipID := uuid.MustParse("0199ca10-2000-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-2000-7001-9000-000000000004")
	now := time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, []uuid.UUID{actorID}, []uuid.UUID{actorMembershipID})

	usecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(data.NewData(environment.runtimePool)),
		data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	result, err := usecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.CreateServicePrincipalCommand{
		Name: "External SDK", RoleIDs: []uuid.UUID{roleID},
		Actor: biz.TenantAuthorizationActor{
			PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
			RequestID: "sp-create", CorrelationID: "sp-create", DecisionID: "sp-create",
		},
	})
	if err != nil {
		t.Fatalf("CreateServicePrincipal() error = %v", err)
	}

	var principalType, principalStatus, profileName, normalizedName, membershipStatus string
	var profileTenantID, membershipTenantID, membershipPrincipalID, bindingRoleID, auditTargetID uuid.UUID
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT p.principal_type,p.status,sp.tenant_id,sp.name,sp.normalized_name,
		       m.tenant_id,m.principal_id,m.status,b.role_id,a.target_id
		FROM principals p
		JOIN service_principals sp ON sp.principal_id=p.id
		JOIN tenant_memberships m ON m.tenant_id=sp.tenant_id AND m.id=sp.membership_id
		JOIN tenant_role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id
		JOIN iam_audit_events a ON a.tenant_id=m.tenant_id AND a.event_id=$2
		WHERE p.id=$1
	`, result.Principal.ID, result.AuditEventID).Scan(
		&principalType, &principalStatus, &profileTenantID, &profileName, &normalizedName,
		&membershipTenantID, &membershipPrincipalID, &membershipStatus, &bindingRoleID, &auditTargetID,
	); err != nil {
		t.Fatalf("query atomic service principal state: %v", err)
	}
	if principalType != "service" || principalStatus != "active" || profileTenantID != tenantID || profileName != "External SDK" || normalizedName != "external sdk" {
		t.Fatalf("principal/profile/profile = %s/%s/%s/%q/%q", principalType, principalStatus, profileTenantID, profileName, normalizedName)
	}
	if membershipTenantID != tenantID || membershipPrincipalID != result.Principal.ID || membershipStatus != "active" || bindingRoleID != roleID || auditTargetID != result.Principal.ID {
		t.Fatalf("membership/binding/audit = %s/%s/%s/%s/%s", membershipTenantID, membershipPrincipalID, membershipStatus, bindingRoleID, auditTargetID)
	}
}

func TestAPIKeyCreatePersistsDigestAndAuditWithoutRawSecret(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199ca10-2200-7001-9000-000000000001")
	actorID := uuid.MustParse("0199ca10-2200-7001-9000-000000000002")
	actorMembershipID := uuid.MustParse("0199ca10-2200-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-2200-7001-9000-000000000004")
	now := time.Date(2026, 9, 8, 13, 30, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, []uuid.UUID{actorID}, []uuid.UUID{actorMembershipID})

	usecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(data.NewData(environment.runtimePool)),
		data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "sp-key-create", CorrelationID: "sp-key-create", DecisionID: "sp-key-create",
	}
	principal, err := usecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.CreateServicePrincipalCommand{
		Name: "Build Automation", RoleIDs: []uuid.UUID{roleID}, Actor: actor,
	})
	if err != nil {
		t.Fatalf("CreateServicePrincipal() error = %v", err)
	}
	created, err := usecase.CreateAPIKey(ctx, mustTenantScope(t, tenantID), biz.CreateAPIKeyCommand{
		PrincipalID: principal.Principal.ID, NeverExpires: true,
		IdempotencyKey: "api-key-create-1", Actor: actor,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	var persistedDigest []byte
	var displayPrefix, status string
	var auditTargetID uuid.UUID
	var rawSecretOccurrences int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT key.secret_digest,key.display_prefix,key.status,audit.target_id,
		       (SELECT count(*) FROM api_keys WHERE secret_digest::text LIKE '%' || $3 || '%')
		FROM api_keys key
		JOIN iam_audit_events audit ON audit.tenant_id=key.tenant_id AND audit.event_id=$2
		WHERE key.tenant_id=$1 AND key.key_id=$4
	`, tenantID, created.AuditEventID, created.Secret, created.APIKey.ID).Scan(
		&persistedDigest, &displayPrefix, &status, &auditTargetID, &rawSecretOccurrences,
	); err != nil {
		t.Fatalf("query API key state: %v", err)
	}
	wantDigest := sha256.Sum256([]byte(created.Secret))
	if string(persistedDigest) != string(wantDigest[:]) || displayPrefix != created.APIKey.DisplayPrefix || status != "active" || auditTargetID != created.APIKey.ID {
		t.Fatalf("persisted API key = digest:%x prefix:%q status:%q audit:%s", persistedDigest, displayPrefix, status, auditTargetID)
	}
	if rawSecretOccurrences != 0 {
		t.Fatalf("raw secret was persisted")
	}
}

func TestRemovingServicePrincipalMembershipDisablesPrincipalAndRevokesEveryKey(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199ca10-2300-7001-9000-000000000001")
	actorID := uuid.MustParse("0199ca10-2300-7001-9000-000000000002")
	actorMembershipID := uuid.MustParse("0199ca10-2300-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-2300-7001-9000-000000000004")
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, []uuid.UUID{actorID}, []uuid.UUID{actorMembershipID})
	dataSet := data.NewData(environment.runtimePool)
	servicePrincipals := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "remove-sp", CorrelationID: "remove-sp", DecisionID: "remove-sp",
	}
	created, err := servicePrincipals.CreateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.CreateServicePrincipalCommand{
		Name: "Removal Bot", RoleIDs: []uuid.UUID{roleID}, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := servicePrincipals.CreateAPIKey(ctx, mustTenantScope(t, tenantID), biz.CreateAPIKeyCommand{
			PrincipalID: created.Principal.ID, NeverExpires: true,
			IdempotencyKey: "remove-key-" + string(rune('1'+index)), Actor: actor,
		}); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	tenantAuthorization := biz.NewTenantAuthorizationUsecase(
		data.NewPostgresTenantAuthorizationUnitOfWork(dataSet), catalog, data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{})

	if _, err := tenantAuthorization.UpdateMembership(ctx, mustTenantScope(t, tenantID), biz.UpdateTenantMembershipCommand{
		MembershipID: created.Principal.MembershipID, Status: biz.MembershipStatusRemoved,
		ExpectedVersion: 1, Actor: actor,
	}); err != nil {
		t.Fatalf("UpdateMembership() error = %v", err)
	}

	var principalStatus, membershipStatus string
	var activeKeys, revokedKeys, disableAudits int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT principal.status,membership.status,
		       count(*) FILTER (WHERE key.status='active'),
		       count(*) FILTER (WHERE key.status='revoked'),
		       (SELECT count(*) FROM iam_audit_events audit
		         WHERE audit.tenant_id=$1 AND audit.action='iam.service-principal.disabled'
		           AND audit.target_id=$2)
		FROM principals principal
		JOIN service_principals profile ON profile.principal_id=principal.id
		JOIN tenant_memberships membership ON membership.tenant_id=profile.tenant_id AND membership.id=profile.membership_id
		LEFT JOIN api_keys key ON key.tenant_id=profile.tenant_id AND key.principal_id=profile.principal_id
		WHERE profile.tenant_id=$1 AND profile.principal_id=$2
		GROUP BY principal.status,membership.status
	`, tenantID, created.Principal.ID).Scan(&principalStatus, &membershipStatus, &activeKeys, &revokedKeys, &disableAudits); err != nil {
		t.Fatal(err)
	}
	if principalStatus != "disabled" || membershipStatus != "removed" || activeKeys != 0 || revokedKeys != 2 || disableAudits != 1 {
		t.Fatalf("cascade = principal:%s membership:%s active:%d revoked:%d audits:%d", principalStatus, membershipStatus, activeKeys, revokedKeys, disableAudits)
	}
	reenabled, err := servicePrincipals.UpdateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.UpdateServicePrincipalCommand{
		PrincipalID: created.Principal.ID, Name: created.Principal.Name, Status: biz.PrincipalStatusActive,
		ExpectedVersion: 2, IdempotencyKey: "reenable-sp", Actor: actor,
	})
	if err != nil {
		t.Fatalf("UpdateServicePrincipal(re-enable) error = %v", err)
	}
	if reenabled.Principal.Status != biz.PrincipalStatusActive || reenabled.RevokedAPIKeys != 0 {
		t.Fatalf("re-enabled principal = %#v, revoked=%d", reenabled.Principal, reenabled.RevokedAPIKeys)
	}
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status='active'),count(*) FILTER (WHERE status='revoked')
		FROM api_keys WHERE tenant_id=$1 AND principal_id=$2
	`, tenantID, created.Principal.ID).Scan(&activeKeys, &revokedKeys); err != nil {
		t.Fatal(err)
	}
	if activeKeys != 0 || revokedKeys != 2 {
		t.Fatalf("re-enable revived old keys: active=%d revoked=%d", activeKeys, revokedKeys)
	}
}

func TestAPIKeyAuthorizesServicePrincipalThroughRestrictedPostgres(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	tenantID := uuid.MustParse("0199ca10-2600-7001-9000-000000000001")
	actorID := uuid.MustParse("0199ca10-2600-7001-9000-000000000002")
	actorMembershipID := uuid.MustParse("0199ca10-2600-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-2600-7001-9000-000000000004")
	decisionID := uuid.MustParse("0199ca10-2600-7001-9000-000000000005")
	now := time.Now().UTC().Truncate(time.Second)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantID, roleID, now, []uuid.UUID{actorID}, []uuid.UUID{actorMembershipID})
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenant_role_permissions (tenant_id,role_id,scope,resource,action,created_at)
		VALUES ($1,$2,'tenant','instances','create',$3)
	`, tenantID, roleID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenant_lifecycle_projections
			(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until)
		VALUES ($1,'active',1,$2,$2,$3)
	`, tenantID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	dataSet := data.NewData(environment.runtimePool)
	servicePrincipals := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "authorize-api-key", CorrelationID: "authorize-api-key", DecisionID: "authorize-api-key",
	}
	created, err := servicePrincipals.CreateServicePrincipal(ctx, mustTenantScope(t, tenantID), biz.CreateServicePrincipalCommand{
		Name: "Authorized SDK", RoleIDs: []uuid.UUID{roleID}, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := servicePrincipals.CreateAPIKey(ctx, mustTenantScope(t, tenantID), biz.CreateAPIKeyCommand{
		PrincipalID: created.Principal.ID, NeverExpires: true, IdempotencyKey: "authorize-key-1", Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := data.NewTargetOperationRegistry(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	authorization := biz.NewAuthorizationUsecase(
		registry, nil, data.NewPostgresAuthorizationReader(dataSet),
		&fixedIDGenerator{ids: []uuid.UUID{decisionID}}, fixedClock{now: now.Add(time.Minute)},
		allowingAPIKeyUsageObserver{},
	)
	decision, err := authorization.CheckPermission(ctx, biz.CheckPermissionCommand{
		RawCredential: key.Secret, OperationID: "createInstance",
		PolicyRevision: data.TargetPolicyRevision, TargetTenantID: tenantID,
	})
	if err != nil {
		t.Fatalf("CheckPermission(API key) error = %v", err)
	}
	if !decision.Allowed || decision.DecisionID != decisionID || decision.Principal.ID != created.Principal.ID || decision.Principal.Type != biz.PrincipalTypeService || decision.Principal.SessionID != uuid.Nil || decision.Principal.GrantID != uuid.Nil {
		t.Fatalf("API key decision = %#v", decision)
	}
}

func TestAPIKeyRealPostgresValidationAndAuthorizationFailureMatrix(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	redisClient := newVerticalSliceRedisClient(t, ctx)
	tenantA := uuid.MustParse("0199d080-3000-7001-9000-000000000001")
	tenantB := uuid.MustParse("0199d080-3000-7001-9000-000000000002")
	actorA := uuid.MustParse("0199d080-3000-7001-9000-000000000003")
	actorB := uuid.MustParse("0199d080-3000-7001-9000-000000000004")
	membershipA := uuid.MustParse("0199d080-3000-7001-9000-000000000005")
	membershipB := uuid.MustParse("0199d080-3000-7001-9000-000000000006")
	roleA := uuid.MustParse("0199d080-3000-7001-9000-000000000007")
	roleB := uuid.MustParse("0199d080-3000-7001-9000-000000000008")
	seededAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantA, roleA, seededAt, []uuid.UUID{actorA}, []uuid.UUID{membershipA})
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantB, roleB, seededAt, []uuid.UUID{actorB}, []uuid.UUID{membershipB})
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	for _, fixture := range []struct {
		tenantID uuid.UUID
		roleID   uuid.UUID
	}{
		{tenantID: tenantA, roleID: roleA},
		{tenantID: tenantB, roleID: roleB},
	} {
		if _, err := owner.Exec(ctx, `
			INSERT INTO tenant_role_permissions (tenant_id,role_id,scope,resource,action,created_at)
			VALUES ($1,$2,'tenant','instances','create',$3)
		`, fixture.tenantID, fixture.roleID, seededAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.Exec(ctx, `
		INSERT INTO tenant_lifecycle_projections
			(tenant_id,status,lifecycle_version,effective_at,observed_at,fresh_until)
		VALUES ($1,'active',1,$2,$2,$3)
	`, tenantA, seededAt, seededAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	dataSet := data.NewData(environment.runtimePool)
	usageAggregator, err := data.NewRedisAPIKeyUsageAggregator(
		redisClient, dataSet,
		data.RedisAPIKeyUsageConfig{Namespace: "ani-iam:dp2-10:validation-matrix", BatchSize: 32},
	)
	if err != nil {
		t.Fatalf("NewRedisAPIKeyUsageAggregator() error = %v", err)
	}
	principalUsecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	actor := biz.TenantAuthorizationActor{
		PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "api-key-matrix", CorrelationID: "api-key-matrix", DecisionID: "api-key-matrix",
	}
	principal, err := principalUsecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantA), biz.CreateServicePrincipalCommand{
		Name: "Validation Matrix", RoleIDs: []uuid.UUID{roleA}, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	activeKey, err := principalUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantA), biz.CreateAPIKeyCommand{
		PrincipalID: principal.Principal.ID, NeverExpires: true, IdempotencyKey: "matrix-active", Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}

	validate := func(rawCredential string, observedAt time.Time) (*iamv1.ValidatePrincipalResponse, error) {
		decisionID, idErr := uuid.NewV7()
		if idErr != nil {
			t.Fatal(idErr)
		}
		auditID, idErr := uuid.NewV7()
		if idErr != nil {
			t.Fatal(idErr)
		}
		authentication := biz.NewAuthenticationUsecase(
			data.NewPostgresPasswordLoginReader(dataSet), nil, nil, data.NewPostgresLoginUnitOfWork(dataSet), nil, nil,
			&fixedIDGenerator{ids: []uuid.UUID{decisionID, auditID}}, fixedClock{now: observedAt}, usageAggregator,
		)
		return service.NewAuthenticationService(authentication).ValidatePrincipal(ctx, &iamv1.ValidatePrincipalRequest{
			Credential: &iamv1.BearerCredential{Value: rawCredential}, OperationId: "createInstance", PolicyRevision: data.TargetPolicyRevision,
		})
	}
	actorForTenantB := biz.TenantAuthorizationActor{
		PrincipalID: actorB, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID: "api-key-missing-lifecycle", CorrelationID: "api-key-missing-lifecycle", DecisionID: "api-key-missing-lifecycle",
	}
	principalB, err := principalUsecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantB), biz.CreateServicePrincipalCommand{
		Name: "Missing Lifecycle", RoleIDs: []uuid.UUID{roleB}, Actor: actorForTenantB,
	})
	if err != nil {
		t.Fatal(err)
	}
	missingLifecycleKey, err := principalUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantB), biz.CreateAPIKeyCommand{
		PrincipalID: principalB.Principal.ID, NeverExpires: true, IdempotencyKey: "matrix-missing-lifecycle", Actor: actorForTenantB,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = validate(missingLifecycleKey.Secret, seededAt.Add(time.Minute))
	assertIntegrationIAMError(t, err, codes.Unavailable, "TENANT_LIFECYCLE_STALE", map[string]string{"tenant_id": tenantB.String()})

	validated, err := validate(activeKey.Secret, seededAt.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ValidatePrincipal(valid) error = %v", err)
	}
	if validated.GetPrincipal().GetPrincipalId() != principal.Principal.ID.String() ||
		validated.GetPrincipal().GetBoundary().GetTenant().GetTenantId() != tenantA.String() ||
		validated.GetPrincipal().GetSessionId() != "" || validated.GetPrincipal().GetGrantId() != "" {
		t.Fatalf("valid API key principal = %#v", validated.GetPrincipal())
	}
	var lastUsedAt *time.Time
	if err := environment.runtimePool.QueryRow(ctx, `SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2`, tenantA, activeKey.APIKey.ID).Scan(&lastUsedAt); err != nil || lastUsedAt != nil {
		t.Fatalf("last_used_at before asynchronous flush = %v, error = %v", lastUsedAt, err)
	}
	if processed, flushErr := usageAggregator.Flush(ctx); flushErr != nil || processed != 1 {
		t.Fatalf("Flush() = %d / %v", processed, flushErr)
	}
	if err := environment.runtimePool.QueryRow(ctx, `SELECT last_used_at FROM api_keys WHERE tenant_id=$1 AND key_id=$2`, tenantA, activeKey.APIKey.ID).Scan(&lastUsedAt); err != nil || lastUsedAt == nil || !lastUsedAt.Equal(seededAt.Add(2*time.Minute)) {
		t.Fatalf("last_used_at after asynchronous flush = %v, error = %v", lastUsedAt, err)
	}

	wrongSecret := "ani_" + activeKey.APIKey.ID.String() + "_wrong-secret"
	_, err = validate(wrongSecret, seededAt.Add(3*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})
	unknownID := uuid.MustParse("0199d080-3000-7001-9000-000000000099")
	_, err = validate("ani_"+unknownID.String()+"_unknown", seededAt.Add(3*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	expiringKey, err := principalUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantA), biz.CreateAPIKeyCommand{
		PrincipalID: principal.Principal.ID, ExpiresAt: expiresAt, IdempotencyKey: "matrix-expired", Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = validate(expiringKey.Secret, expiresAt)
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})

	revokedKey, err := principalUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantA), biz.CreateAPIKeyCommand{
		PrincipalID: principal.Principal.ID, NeverExpires: true, IdempotencyKey: "matrix-revoked", Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := principalUsecase.RevokeAPIKey(ctx, mustTenantScope(t, tenantA), biz.RevokeAPIKeyCommand{
		KeyID: revokedKey.APIKey.ID, IdempotencyKey: "matrix-revoke", Actor: actor,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = validate(revokedKey.Secret, seededAt.Add(3*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})

	assertDenied := func(name, statement string, args ...any) {
		t.Helper()
		if _, execErr := environment.runtimePool.Exec(ctx, statement, args...); execErr != nil {
			t.Fatalf("%s mutate state: %v", name, execErr)
		}
		_, validationErr := validate(activeKey.Secret, seededAt.Add(4*time.Minute))
		assertIntegrationIAMError(t, validationErr, codes.PermissionDenied, "PERMISSION_DENIED", map[string]string{
			"operation_id": "createInstance", "decision_id": "not-issued",
		})
	}
	assertDenied("principal inactive", `UPDATE principals SET status='disabled' WHERE id=$1`, principal.Principal.ID)
	if _, err := environment.runtimePool.Exec(ctx, `UPDATE principals SET status='active' WHERE id=$1`, principal.Principal.ID); err != nil {
		t.Fatal(err)
	}
	assertDenied("membership inactive", `UPDATE tenant_memberships SET status='suspended' WHERE tenant_id=$1 AND id=$2`, tenantA, principal.Principal.MembershipID)
	if _, err := environment.runtimePool.Exec(ctx, `UPDATE tenant_memberships SET status='active' WHERE tenant_id=$1 AND id=$2`, tenantA, principal.Principal.MembershipID); err != nil {
		t.Fatal(err)
	}
	assertDenied("tenant access inactive", `UPDATE tenant_access SET status='suspended' WHERE tenant_id=$1`, tenantA)
	if _, err := environment.runtimePool.Exec(ctx, `UPDATE tenant_access SET status='active' WHERE tenant_id=$1`, tenantA); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET status='suspended', fresh_until=$2 WHERE tenant_id=$1`, tenantA, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err = validate(activeKey.Secret, seededAt.Add(4*time.Minute))
	assertIntegrationIAMError(t, err, codes.PermissionDenied, "PERMISSION_DENIED", map[string]string{
		"operation_id": "createInstance", "decision_id": "not-issued",
	})
	if _, err := owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET status='active' WHERE tenant_id=$1`, tenantA); err != nil {
		t.Fatal(err)
	}

	if _, err := owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET fresh_until=$2 WHERE tenant_id=$1`, tenantA, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = validate(wrongSecret, seededAt.Add(5*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})
	_, err = validate(expiringKey.Secret, expiresAt)
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})
	_, err = validate(revokedKey.Secret, seededAt.Add(5*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})
	_, err = validate(activeKey.Secret, seededAt.Add(5*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unavailable, "TENANT_LIFECYCLE_STALE", map[string]string{"tenant_id": tenantA.String()})
	if _, err := owner.Exec(ctx, `UPDATE tenant_lifecycle_projections SET fresh_until=$2 WHERE tenant_id=$1`, tenantA, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	registry, err := data.NewTargetOperationRegistry(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	authorization := biz.NewAuthorizationUsecase(
		registry, nil, data.NewPostgresAuthorizationReader(dataSet), data.NewUUIDv7Generator(), fixedClock{now: seededAt.Add(6 * time.Minute)},
		usageAggregator,
	)
	authorizationService := service.NewAuthorizationService(authorization)
	check := func(rawCredential string, targetTenantID uuid.UUID) (*iamv1.CheckPermissionResponse, error) {
		return authorizationService.CheckPermission(ctx, &iamv1.CheckPermissionRequest{
			Credential: &iamv1.BearerCredential{Value: rawCredential}, OperationId: "createInstance", PolicyRevision: data.TargetPolicyRevision,
			Target: &iamv1.AuthorizationTarget{TenantId: targetTenantID.String()},
		})
	}
	crossTenant, err := check(activeKey.Secret, tenantB)
	if err != nil || crossTenant.GetDecision().GetAllowed() || crossTenant.GetDecision().GetReason() != string(biz.AuthorizationReasonTenantMismatch) {
		t.Fatalf("cross-tenant CheckPermission = %#v, error = %v", crossTenant, err)
	}
	_, err = check(wrongSecret, tenantB)
	assertIntegrationIAMError(t, err, codes.Unauthenticated, "CREDENTIAL_INVALID", map[string]string{"credential_kind": "api_key"})
	var forgedTenantAuditCount int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*)
		FROM iam_audit_events
		WHERE tenant_id=$1 AND target_id=$2 AND reason='INVALID_CREDENTIAL'
	`, tenantB, activeKey.APIKey.ID).Scan(&forgedTenantAuditCount); err != nil {
		t.Fatalf("query forged tenant audit count: %v", err)
	}
	if forgedTenantAuditCount != 0 {
		t.Fatalf("wrong-secret request wrote %d audit rows into untrusted target tenant", forgedTenantAuditCount)
	}
	var unboundAuditCount int
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT count(*)
		FROM iam_audit_events
		WHERE tenant_id IS NULL AND boundary='principal' AND authentication_method='anonymous'
		  AND target_id=$1 AND reason='INVALID_CREDENTIAL'
	`, activeKey.APIKey.ID).Scan(&unboundAuditCount); err != nil {
		t.Fatalf("query unbound credential-failure audit count: %v", err)
	}
	if unboundAuditCount == 0 {
		t.Fatal("wrong-secret request did not write an unbound credential-failure audit")
	}

	environment.runtimePool.Close()
	_, err = validate(activeKey.Secret, seededAt.Add(7*time.Minute))
	assertIntegrationIAMError(t, err, codes.Unavailable, "IAM_UNAVAILABLE", map[string]string{"dependency": "authentication"})
}

func TestServicePrincipalManagementRealPostgresTenantPaginationConcurrencyAndRollback(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	redisClient := newVerticalSliceRedisClient(t, ctx)
	creationLimiter, err := data.NewRedisAPIKeyCreationLimiter(redisClient, data.RedisAPIKeyCreationLimiterConfig{
		Namespace: "ani-iam:dp2-10:management-api-key-create",
		Limit:     biz.APIKeyCreationRateLimit,
		Window:    biz.APIKeyCreationRateWindow,
	})
	if err != nil {
		t.Fatalf("NewRedisAPIKeyCreationLimiter() error = %v", err)
	}
	tenantA := uuid.MustParse("0199d080-4000-7001-9000-000000000001")
	tenantB := uuid.MustParse("0199d080-4000-7001-9000-000000000002")
	actorA := uuid.MustParse("0199d080-4000-7001-9000-000000000003")
	actorB := uuid.MustParse("0199d080-4000-7001-9000-000000000004")
	membershipA := uuid.MustParse("0199d080-4000-7001-9000-000000000005")
	membershipB := uuid.MustParse("0199d080-4000-7001-9000-000000000006")
	roleA := uuid.MustParse("0199d080-4000-7001-9000-000000000007")
	roleB := uuid.MustParse("0199d080-4000-7001-9000-000000000008")
	seededAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantA, roleA, seededAt, []uuid.UUID{actorA}, []uuid.UUID{membershipA})
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenantB, roleB, seededAt, []uuid.UUID{actorB}, []uuid.UUID{membershipB})
	dataSet := data.NewData(environment.runtimePool)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	reader := data.NewPostgresTenantAuthorizationReader(dataSet)
	mutations := biz.NewTenantAuthorizationUsecase(
		data.NewPostgresTenantAuthorizationUnitOfWork(dataSet), catalog, data.NewUUIDv7Generator(), data.NewSystemClock(),
		creationLimiter,
	)
	admin := service.NewTenantIAMAdminService(reader, mutations)
	trustedContext := func(tenantID, actorID uuid.UUID, suffix string) context.Context {
		return metadata.NewIncomingContext(ctx, metadata.Pairs(
			"x-ani-principal-id", actorID.String(), "x-ani-principal-type", "human", "x-ani-authn-method", "password",
			"x-ani-tenant-id", tenantID.String(), "x-ani-decision-id", "decision-"+suffix,
			"x-request-id", "request-"+suffix, "x-correlation-id", "correlation-"+suffix,
		))
	}
	ctxA := trustedContext(tenantA, actorA, "tenant-a")
	ctxB := trustedContext(tenantB, actorB, "tenant-b")
	createPrincipal := func(callContext context.Context, tenantID, roleID uuid.UUID, name, key string) *iamv1.ServicePrincipal {
		response, createErr := admin.CreateServicePrincipal(callContext, &iamv1.CreateServicePrincipalRequest{
			TenantId: tenantID.String(), Name: name, RoleIds: []string{roleID.String()}, IdempotencyKey: key,
		})
		if createErr != nil {
			t.Fatalf("CreateServicePrincipal(%s) error = %v", name, createErr)
		}
		return response.GetPrincipal()
	}
	principalA1 := createPrincipal(ctxA, tenantA, roleA, "Alpha Bot", "create-alpha")
	principalA2 := createPrincipal(ctxA, tenantA, roleA, "Beta Bot", "create-beta")
	principalA3 := createPrincipal(ctxA, tenantA, roleA, "Gamma Bot", "create-gamma")
	principalB := createPrincipal(ctxB, tenantB, roleB, "Foreign Bot", "create-foreign")
	gotA1, err := admin.GetServicePrincipal(ctxA, &iamv1.GetServicePrincipalRequest{PrincipalId: principalA1.GetPrincipalId()})
	if err != nil || gotA1.GetPrincipal().GetTenantId() != tenantA.String() || gotA1.GetPrincipal().GetName() != "Alpha Bot" {
		t.Fatalf("GetServicePrincipal(positive) = %#v, error = %v", gotA1, err)
	}
	updatedA1, err := admin.UpdateServicePrincipal(ctxA, &iamv1.UpdateServicePrincipalRequest{
		PrincipalId: principalA1.GetPrincipalId(), Name: "Alpha Bot Updated",
		Status: iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE, ExpectedVersion: 1, IdempotencyKey: "update-alpha",
	})
	if err != nil || updatedA1.GetPrincipal().GetName() != "Alpha Bot Updated" || updatedA1.GetPrincipal().GetVersion() != 2 {
		t.Fatalf("UpdateServicePrincipal(positive) = %#v, error = %v", updatedA1, err)
	}
	gotA1, err = admin.GetServicePrincipal(ctxA, &iamv1.GetServicePrincipalRequest{PrincipalId: principalA1.GetPrincipalId()})
	if err != nil || gotA1.GetPrincipal().GetName() != "Alpha Bot Updated" || gotA1.GetPrincipal().GetVersion() != 2 {
		t.Fatalf("GetServicePrincipal(updated) = %#v, error = %v", gotA1, err)
	}
	revocableKey, err := admin.CreateAPIKey(ctxA, &iamv1.CreateAPIKeyRequest{
		PrincipalId: principalA2.GetPrincipalId(), NeverExpires: true, IdempotencyKey: "revocable-management-key",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey(revocable) error = %v", err)
	}
	revokedKey, err := admin.RevokeAPIKey(ctxA, &iamv1.RevokeAPIKeyRequest{
		KeyId: revocableKey.GetApiKey().GetKeyId(), IdempotencyKey: "revoke-management-key",
	})
	if err != nil || revokedKey.GetApiKey().GetStatus() != iamv1.APIKeyStatus_API_KEY_STATUS_REVOKED {
		t.Fatalf("RevokeAPIKey(positive) = %#v, error = %v", revokedKey, err)
	}
	var revokedStatus string
	var revokedVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT status,version FROM api_keys WHERE tenant_id=$1 AND key_id=$2
	`, tenantA, uuid.MustParse(revocableKey.GetApiKey().GetKeyId())).Scan(&revokedStatus, &revokedVersion); err != nil {
		t.Fatal(err)
	}
	if revokedStatus != "revoked" || revokedVersion != 2 {
		t.Fatalf("persisted revoked key = %s/v%d", revokedStatus, revokedVersion)
	}

	firstPage, err := admin.ListServicePrincipals(ctxA, &iamv1.ListServicePrincipalsRequest{
		TenantId: tenantA.String(), Page: &iamv1.CursorPageRequest{PageSize: 2},
	})
	if err != nil || len(firstPage.GetPrincipals()) != 2 || firstPage.GetNextCursor() == "" {
		t.Fatalf("first principal page = %#v, error = %v", firstPage, err)
	}
	secondPage, err := admin.ListServicePrincipals(ctxA, &iamv1.ListServicePrincipalsRequest{
		TenantId: tenantA.String(), Page: &iamv1.CursorPageRequest{PageSize: 2, Cursor: firstPage.GetNextCursor()},
	})
	if err != nil || len(secondPage.GetPrincipals()) != 1 || secondPage.GetNextCursor() != "" {
		t.Fatalf("second principal page = %#v, error = %v", secondPage, err)
	}
	seen := map[string]bool{}
	for _, page := range []*iamv1.ListServicePrincipalsResponse{firstPage, secondPage} {
		for _, principal := range page.GetPrincipals() {
			if principal.GetTenantId() != tenantA.String() {
				t.Fatalf("principal page leaked tenant: %#v", principal)
			}
			seen[principal.GetPrincipalId()] = true
		}
	}
	for _, principal := range []*iamv1.ServicePrincipal{principalA1, principalA2, principalA3} {
		if !seen[principal.GetPrincipalId()] {
			t.Fatalf("principal %s missing from cursor pages", principal.GetPrincipalId())
		}
	}

	expiredAt := seededAt.Add(time.Hour)
	pastUsecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), fixedClock{now: seededAt}, allowingAPIKeyCreationLimiter{},
	)
	expiredKey, err := pastUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantA), biz.CreateAPIKeyCommand{
		PrincipalID: uuid.MustParse(principalA1.GetPrincipalId()), ExpiresAt: expiredAt, IdempotencyKey: "expired-management-key",
		Actor: biz.TenantAuthorizationActor{PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "expired-key", CorrelationID: "expired-key", DecisionID: "expired-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	activeKey, err := admin.CreateAPIKey(ctxA, &iamv1.CreateAPIKeyRequest{
		PrincipalId: principalA1.GetPrincipalId(), NeverExpires: true, IdempotencyKey: "active-management-key",
	})
	if err != nil || activeKey.GetApiKeySecret() == "" {
		t.Fatalf("CreateAPIKey(active) = %#v, error = %v", activeKey, err)
	}
	keysFirst, err := admin.ListAPIKeys(ctxA, &iamv1.ListAPIKeysRequest{
		PrincipalId: principalA1.GetPrincipalId(), Page: &iamv1.CursorPageRequest{PageSize: 1},
	})
	if err != nil || len(keysFirst.GetApiKeys()) != 1 || keysFirst.GetNextCursor() == "" {
		t.Fatalf("first API key page = %#v, error = %v", keysFirst, err)
	}
	keysSecond, err := admin.ListAPIKeys(ctxA, &iamv1.ListAPIKeysRequest{
		PrincipalId: principalA1.GetPrincipalId(), Page: &iamv1.CursorPageRequest{PageSize: 1, Cursor: keysFirst.GetNextCursor()},
	})
	if err != nil || len(keysSecond.GetApiKeys()) != 1 || keysSecond.GetNextCursor() != "" {
		t.Fatalf("second API key page = %#v, error = %v", keysSecond, err)
	}
	statuses := map[string]iamv1.APIKeyStatus{}
	for _, page := range []*iamv1.ListAPIKeysResponse{keysFirst, keysSecond} {
		for _, key := range page.GetApiKeys() {
			statuses[key.GetKeyId()] = key.GetStatus()
		}
	}
	if statuses[expiredKey.APIKey.ID.String()] != iamv1.APIKeyStatus_API_KEY_STATUS_EXPIRED ||
		statuses[activeKey.GetApiKey().GetKeyId()] != iamv1.APIKeyStatus_API_KEY_STATUS_ACTIVE {
		t.Fatalf("projected API key statuses = %#v", statuses)
	}

	staleAt := time.Now().UTC().Add(-biz.APIKeyStaleAfter - time.Hour).Truncate(time.Second)
	staleUsecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), fixedClock{now: staleAt}, allowingAPIKeyCreationLimiter{},
	)
	stalePrincipal, err := staleUsecase.CreateServicePrincipal(ctx, mustTenantScope(t, tenantA), biz.CreateServicePrincipalCommand{
		Name: "Stale Key Bot", RoleIDs: []uuid.UUID{roleA},
		Actor: biz.TenantAuthorizationActor{PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "stale-principal", CorrelationID: "stale-principal", DecisionID: "stale-principal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staleUsecase.CreateAPIKey(ctx, mustTenantScope(t, tenantA), biz.CreateAPIKeyCommand{
		PrincipalID: stalePrincipal.Principal.ID, NeverExpires: true, IdempotencyKey: "stale-key",
		Actor: biz.TenantAuthorizationActor{PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "stale-key", CorrelationID: "stale-key", DecisionID: "stale-key"},
	}); err != nil {
		t.Fatal(err)
	}
	operationalReader, ok := reader.(biz.APIKeyOperationalReader)
	if !ok {
		t.Fatal("PostgreSQL reader does not implement APIKeyOperationalReader")
	}
	staleSignals, err := operationalReader.GetAPIKeyOperationalSignals(ctx, mustTenantScope(t, tenantA), stalePrincipal.Principal.ID, time.Now().UTC())
	if err != nil || staleSignals.StaleNonExpiringCount != 1 || staleSignals.UnusualActiveCount {
		t.Fatalf("stale API key signals = %#v, error = %v", staleSignals, err)
	}

	for index := int64(1); index < biz.APIKeyCreationRateLimit; index++ {
		if _, err := admin.CreateAPIKey(ctxA, &iamv1.CreateAPIKeyRequest{
			PrincipalId: principalA1.GetPrincipalId(), NeverExpires: true, IdempotencyKey: fmt.Sprintf("rate-key-%02d", index),
		}); err != nil {
			t.Fatalf("CreateAPIKey(rate slot %d) error = %v", index, err)
		}
	}
	_, err = admin.CreateAPIKey(ctxA, &iamv1.CreateAPIKeyRequest{
		PrincipalId: principalA1.GetPrincipalId(), NeverExpires: true, IdempotencyKey: "rate-key-denied",
	})
	assertIntegrationIAMError(t, err, codes.ResourceExhausted, "AUTH_RATE_LIMITED", map[string]string{
		"limit_scope": "api_key_creation",
	})
	rateLimitInfo := status.Convert(err).Details()[0].(*errdetails.ErrorInfo)
	retryAfterSeconds, parseErr := strconv.ParseInt(rateLimitInfo.GetMetadata()["retry_after_seconds"], 10, 64)
	if parseErr != nil || retryAfterSeconds < 1 || retryAfterSeconds > int64(biz.APIKeyCreationRateWindow/time.Second) {
		t.Fatalf("retry_after_seconds = %q / %v", rateLimitInfo.GetMetadata()["retry_after_seconds"], parseErr)
	}
	unusualSignals, err := operationalReader.GetAPIKeyOperationalSignals(ctx, mustTenantScope(t, tenantA), uuid.MustParse(principalA1.GetPrincipalId()), time.Now().UTC())
	if err != nil || unusualSignals.ActiveCount < biz.APIKeyUnusualActiveCountThreshold || !unusualSignals.UnusualActiveCount {
		t.Fatalf("unusual API key signals = %#v, error = %v", unusualSignals, err)
	}
	operationalSnapshot, err := data.NewPostgresAPIKeyOperationalSnapshotReader(dataSet).GetAPIKeyOperationalSnapshot(ctx, time.Now().UTC())
	if err != nil || operationalSnapshot.StaleNonExpiringCount < 1 || operationalSnapshot.UnusualServicePrincipalCount < 1 {
		t.Fatalf("operator API key snapshot = %#v, error = %v", operationalSnapshot, err)
	}

	_, err = admin.CreateServicePrincipal(ctxA, &iamv1.CreateServicePrincipalRequest{
		TenantId: tenantB.String(), Name: "Cross Tenant", RoleIds: []string{roleB.String()}, IdempotencyKey: "cross-create",
	})
	assertIntegrationIAMError(t, err, codes.PermissionDenied, "PERMISSION_DENIED", map[string]string{"operation_id": "createServicePrincipal"})
	_, err = admin.ListServicePrincipals(ctxA, &iamv1.ListServicePrincipalsRequest{TenantId: tenantB.String()})
	assertIntegrationIAMError(t, err, codes.PermissionDenied, "PERMISSION_DENIED", map[string]string{"operation_id": "listServicePrincipals"})
	_, err = admin.GetServicePrincipal(ctxA, &iamv1.GetServicePrincipalRequest{PrincipalId: principalB.GetPrincipalId()})
	assertIntegrationIAMError(t, err, codes.NotFound, "NOT_FOUND", map[string]string{"resource_type": "service_principal", "resource_id": principalB.GetPrincipalId()})
	_, err = admin.CreateAPIKey(ctxA, &iamv1.CreateAPIKeyRequest{PrincipalId: principalB.GetPrincipalId(), NeverExpires: true, IdempotencyKey: "cross-key"})
	assertIntegrationIAMError(t, err, codes.NotFound, "NOT_FOUND", map[string]string{"resource_type": "service_principal", "resource_id": principalB.GetPrincipalId()})
	foreignKeys, err := admin.ListAPIKeys(ctxA, &iamv1.ListAPIKeysRequest{PrincipalId: principalB.GetPrincipalId()})
	if err != nil || len(foreignKeys.GetApiKeys()) != 0 {
		t.Fatalf("cross-tenant ListAPIKeys = %#v, error = %v", foreignKeys, err)
	}
	_, err = admin.UpdateServicePrincipal(ctxA, &iamv1.UpdateServicePrincipalRequest{
		PrincipalId: principalB.GetPrincipalId(), Name: "Foreign Updated", Status: iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE,
		ExpectedVersion: 1, IdempotencyKey: "cross-update",
	})
	assertIntegrationIAMError(t, err, codes.NotFound, "NOT_FOUND", map[string]string{"resource_type": "service_principal", "resource_id": principalB.GetPrincipalId()})
	keyB, err := admin.CreateAPIKey(ctxB, &iamv1.CreateAPIKeyRequest{PrincipalId: principalB.GetPrincipalId(), NeverExpires: true, IdempotencyKey: "foreign-key"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.RevokeAPIKey(ctxA, &iamv1.RevokeAPIKeyRequest{KeyId: keyB.GetApiKey().GetKeyId(), IdempotencyKey: "cross-revoke"})
	assertIntegrationIAMError(t, err, codes.NotFound, "NOT_FOUND", map[string]string{"resource_type": "api_key", "resource_id": keyB.GetApiKey().GetKeyId()})

	principalID := uuid.MustParse(principalA2.GetPrincipalId())
	scopeA := mustTenantScope(t, tenantA)
	concurrentResults := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			usecase := biz.NewServicePrincipalUsecase(
				data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
			)
			_, updateErr := usecase.UpdateServicePrincipal(ctx, scopeA, biz.UpdateServicePrincipalCommand{
				PrincipalID: principalID, Name: fmt.Sprintf("Concurrent Bot %d", index), Status: biz.PrincipalStatusActive,
				ExpectedVersion: 1, IdempotencyKey: fmt.Sprintf("concurrent-%d", index),
				Actor: biz.TenantAuthorizationActor{PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: fmt.Sprintf("concurrent-%d", index), CorrelationID: "concurrent", DecisionID: "concurrent"},
			})
			concurrentResults <- updateErr
		}(index)
	}
	successes, conflicts := 0, 0
	for index := 0; index < 2; index++ {
		switch updateErr := <-concurrentResults; {
		case updateErr == nil:
			successes++
		case errors.Is(updateErr, biz.ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent update error = %v", updateErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent update = %d success/%d conflict", successes, conflicts)
	}

	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	if _, err := owner.Exec(ctx, `REVOKE INSERT ON iam_audit_events FROM ani_iam_runtime`); err != nil {
		t.Fatal(err)
	}
	rollbackPrincipalID := uuid.MustParse(principalA3.GetPrincipalId())
	rollbackUsecase := biz.NewServicePrincipalUsecase(
		data.NewPostgresServicePrincipalUnitOfWork(dataSet), data.NewUUIDv7Generator(), data.NewSystemClock(), allowingAPIKeyCreationLimiter{},
	)
	_, err = rollbackUsecase.UpdateServicePrincipal(ctx, mustTenantScope(t, tenantA), biz.UpdateServicePrincipalCommand{
		PrincipalID: rollbackPrincipalID, Name: "Must Roll Back", Status: biz.PrincipalStatusDisabled,
		ExpectedVersion: 1, IdempotencyKey: "rollback-update",
		Actor: biz.TenantAuthorizationActor{PrincipalID: actorA, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "rollback", CorrelationID: "rollback", DecisionID: "rollback"},
	})
	if !errors.Is(err, biz.ErrPersistencePermissionDenied) {
		t.Fatalf("audit failure error = %v, want persistence permission denied", err)
	}
	var rolledBackName, rolledBackStatus string
	var rolledBackVersion int64
	if err := environment.runtimePool.QueryRow(ctx, `
		SELECT profile.name,principal.status,profile.version
		FROM service_principals profile JOIN principals principal ON principal.id=profile.principal_id
		WHERE profile.tenant_id=$1 AND profile.principal_id=$2
	`, tenantA, rollbackPrincipalID).Scan(&rolledBackName, &rolledBackStatus, &rolledBackVersion); err != nil {
		t.Fatal(err)
	}
	if rolledBackName != "Gamma Bot" || rolledBackStatus != "active" || rolledBackVersion != 1 {
		t.Fatalf("audit rollback state = %q/%s/v%d", rolledBackName, rolledBackStatus, rolledBackVersion)
	}
	if _, err := owner.Exec(ctx, `GRANT INSERT ON iam_audit_events TO ani_iam_runtime`); err != nil {
		t.Fatal(err)
	}
}

type allowingAPIKeyCreationLimiter struct{}

func (allowingAPIKeyCreationLimiter) Acquire(context.Context, biz.TenantScope, uuid.UUID) error {
	return nil
}

type allowingAPIKeyUsageObserver struct{}

func (allowingAPIKeyUsageObserver) ObserveAPIKeyUse(context.Context, biz.TenantScope, uuid.UUID, time.Time) error {
	return nil
}

func assertIntegrationIAMError(t *testing.T, err error, wantCode codes.Code, wantReason string, wantMetadata map[string]string) {
	t.Helper()
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != wantCode || len(grpcStatus.Details()) != 1 {
		t.Fatalf("status = %s details=%#v, want %s and one ErrorInfo", grpcStatus.Code(), grpcStatus.Details(), wantCode)
	}
	info, ok := grpcStatus.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetReason() != wantReason || info.GetDomain() != "iam.ani.internal" {
		t.Fatalf("ErrorInfo = %#v, want reason %s", info, wantReason)
	}
	for key, value := range wantMetadata {
		if info.GetMetadata()[key] != value {
			t.Fatalf("metadata[%q] = %q, want %q", key, info.GetMetadata()[key], value)
		}
	}
}
