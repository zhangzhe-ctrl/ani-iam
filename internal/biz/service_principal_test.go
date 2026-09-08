package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type allowingAPIKeyCreationLimiter struct{}

func (allowingAPIKeyCreationLimiter) Acquire(context.Context, TenantScope, uuid.UUID) error {
	return nil
}

func TestServicePrincipalCreateCommitsProfileMembershipBindingsAndAuditAtomically(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-1000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-1000-7001-9000-000000000002")
	membershipID := uuid.MustParse("0199ca10-1000-7001-9000-000000000003")
	roleID := uuid.MustParse("0199ca10-1000-7001-9000-000000000004")
	bindingID := uuid.MustParse("0199ca10-1000-7001-9000-000000000005")
	auditID := uuid.MustParse("0199ca10-1000-7001-9000-000000000006")
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	scope, err := NewTenantScope(tenantID)
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}
	tx := &recordingServicePrincipalTransaction{
		roles: map[uuid.UUID]TenantRole{roleID: {ID: roleID, Code: "sdk-client", Version: 1}},
	}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx},
		&fixedIDs{values: []uuid.UUID{principalID, membershipID, bindingID, auditID}},
		fixedAuthClock{now: now}, allowingAPIKeyCreationLimiter{})

	result, err := usecase.CreateServicePrincipal(context.Background(), scope, CreateServicePrincipalCommand{
		Name: "  Build Bot  ", RoleIDs: []uuid.UUID{roleID}, Actor: validTenantAuthorizationActor(),
	})
	if err != nil {
		t.Fatalf("CreateServicePrincipal() error = %v", err)
	}
	if result.Principal.ID != principalID || result.Principal.Name != "Build Bot" || result.Principal.NormalizedName != "build bot" || result.Principal.MembershipID != membershipID || result.Principal.Status != PrincipalStatusActive || result.Principal.Version != 1 {
		t.Fatalf("principal = %#v", result.Principal)
	}
	if tx.commits != 1 || tx.mutation.Principal.ID != principalID || tx.mutation.Membership.ID != membershipID || tx.mutation.Membership.PrincipalID != principalID {
		t.Fatalf("commits/mutation = %d/%#v", tx.commits, tx.mutation)
	}
	if len(tx.mutation.Bindings) != 1 || tx.mutation.Bindings[0].ID != bindingID || tx.mutation.Bindings[0].RoleID != roleID || tx.mutation.Bindings[0].MembershipID != membershipID {
		t.Fatalf("bindings = %#v", tx.mutation.Bindings)
	}
	if tx.mutation.Audit.ID != auditID || tx.mutation.Audit.Action != AuditActionServicePrincipalCreated || tx.mutation.Audit.TargetID != principalID || tx.mutation.Audit.TargetVersion != 1 {
		t.Fatalf("audit = %#v", tx.mutation.Audit)
	}
}

func TestAPIKeyCreateReturnsSecretOnceAndCommitsOnlyDigestWithAudit(t *testing.T) {
	tenantID := uuid.MustParse("0199ca10-2000-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-2000-7001-9000-000000000002")
	membershipID := uuid.MustParse("0199ca10-2000-7001-9000-000000000003")
	keyID := uuid.MustParse("0199ca10-2000-7001-9000-000000000004")
	auditID := uuid.MustParse("0199ca10-2000-7001-9000-000000000005")
	now := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)
	scope, err := NewTenantScope(tenantID)
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}
	tx := &recordingServicePrincipalTransaction{
		principals: map[uuid.UUID]ServicePrincipal{principalID: {
			ID: principalID, MembershipID: membershipID, Name: "Build Bot",
			Status: PrincipalStatusActive, Version: 1,
		}},
		credential: APIKeyCredential{
			Raw:           "ani_" + keyID.String() + "_high-entropy-test-secret",
			DisplayPrefix: "ani_" + keyID.String()[:8],
			Digest:        sha256.Sum256([]byte("ani_" + keyID.String() + "_high-entropy-test-secret")),
		},
	}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx},
		&fixedIDs{values: []uuid.UUID{keyID, auditID}},
		fixedAuthClock{now: now},
		&recordingAPIKeyCreationLimiter{},
	)

	result, err := usecase.CreateAPIKey(context.Background(), scope, CreateAPIKeyCommand{
		PrincipalID: principalID, NeverExpires: true, IdempotencyKey: "create-key-1",
		Actor: validTenantAuthorizationActor(),
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	wantSecret := "ani_" + keyID.String() + "_high-entropy-test-secret"
	if result.Secret != wantSecret || result.APIKey.ID != keyID || result.APIKey.PrincipalID != principalID || result.APIKey.Status != APIKeyStatusActive || !result.APIKey.NeverExpires {
		t.Fatalf("result = %#v", result)
	}
	if tx.apiKeyMutation.APIKey.Digest != sha256.Sum256([]byte(wantSecret)) {
		t.Fatalf("persisted digest = %x", tx.apiKeyMutation.APIKey.Digest)
	}
	if strings.Contains(tx.apiKeyMutation.APIKey.DisplayPrefix, "high-entropy-test-secret") {
		t.Fatalf("display prefix contains secret: %q", tx.apiKeyMutation.APIKey.DisplayPrefix)
	}
	if tx.apiKeyMutation.Audit.ID != auditID || tx.apiKeyMutation.Audit.Action != AuditActionAPIKeyCreated || tx.apiKeyMutation.Audit.TargetID != keyID {
		t.Fatalf("audit = %#v", tx.apiKeyMutation.Audit)
	}
}

func TestAPIKeyCreateRequiresExactlyOneExpiryMode(t *testing.T) {
	principalID := uuid.MustParse("0199ca10-2100-7001-9000-000000000001")
	scope, _ := NewTenantScope(uuid.MustParse("0199ca10-2100-7001-9000-000000000002"))
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: &recordingServicePrincipalTransaction{}},
		&fixedIDs{}, fixedAuthClock{now: time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)}, allowingAPIKeyCreationLimiter{})

	for _, command := range []CreateAPIKeyCommand{
		{PrincipalID: principalID, IdempotencyKey: "missing-expiry", Actor: validTenantAuthorizationActor()},
		{PrincipalID: principalID, NeverExpires: true, ExpiresAt: time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC), IdempotencyKey: "both-expiry", Actor: validTenantAuthorizationActor()},
	} {
		if _, err := usecase.CreateAPIKey(context.Background(), scope, command); !errors.Is(err, ErrAPIKeyExpiryInvalid) {
			t.Fatalf("CreateAPIKey() error = %v, want %v", err, ErrAPIKeyExpiryInvalid)
		}
	}
}

func TestAPIKeyCreateRateLimitRunsBeforeSecretGeneration(t *testing.T) {
	principalID := uuid.MustParse("0199ca10-2150-7001-9000-000000000001")
	scope, _ := NewTenantScope(uuid.MustParse("0199ca10-2150-7001-9000-000000000002"))
	rateLimit := &AuthenticationRateLimitError{LimitScope: "api_key_creation", RetryAfter: APIKeyCreationRateWindow}
	tx := &recordingServicePrincipalTransaction{
		principals: map[uuid.UUID]ServicePrincipal{principalID: {ID: principalID, Status: PrincipalStatusActive, Version: 1}},
	}
	limiter := &recordingAPIKeyCreationLimiter{err: rateLimit}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx}, &fixedIDs{},
		fixedAuthClock{now: time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)},
		limiter,
	)

	result, err := usecase.CreateAPIKey(context.Background(), scope, CreateAPIKeyCommand{
		PrincipalID: principalID, NeverExpires: true, IdempotencyKey: "rate-limited", Actor: validTenantAuthorizationActor(),
	})
	if !errors.Is(err, ErrAuthenticationRateLimited) || result.Secret != "" || limiter.calls != 1 || tx.commits != 0 || tx.credentialIssueCalls != 0 || tx.apiKeyMutation.APIKey.ID != uuid.Nil {
		t.Fatalf("rate-limited create = result:%#v error:%v limiter/transaction/credential-calls:%d/%d/%d mutation:%#v", result, err, limiter.calls, tx.commits, tx.credentialIssueCalls, tx.apiKeyMutation)
	}
}

func TestAPIKeyCreateFailsClosedWhenRequiredLimiterIsMissing(t *testing.T) {
	principalID := uuid.MustParse("0199ca10-2170-7001-9000-000000000001")
	scope, _ := NewTenantScope(uuid.MustParse("0199ca10-2170-7001-9000-000000000002"))
	tx := &recordingServicePrincipalTransaction{
		principals: map[uuid.UUID]ServicePrincipal{principalID: {ID: principalID, Status: PrincipalStatusActive, Version: 1}},
	}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx},
		&fixedIDs{},
		fixedAuthClock{now: time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)},
		nil,
	)

	result, err := usecase.CreateAPIKey(context.Background(), scope, CreateAPIKeyCommand{
		PrincipalID: principalID, NeverExpires: true, IdempotencyKey: "missing-limiter", Actor: validTenantAuthorizationActor(),
	})
	if !errors.Is(err, ErrAuthenticationDependency) || result.Secret != "" || tx.commits != 0 || tx.credentialIssueCalls != 0 {
		t.Fatalf("CreateAPIKey() = result:%#v error:%v commits/credential-calls:%d/%d", result, err, tx.commits, tx.credentialIssueCalls)
	}
}

func TestServicePrincipalDisableRevokesEveryActiveKeyWithAudit(t *testing.T) {
	principalID := uuid.MustParse("0199ca10-2400-7001-9000-000000000001")
	membershipID := uuid.MustParse("0199ca10-2400-7001-9000-000000000002")
	auditID := uuid.MustParse("0199ca10-2400-7001-9000-000000000003")
	now := time.Date(2026, 9, 8, 14, 30, 0, 0, time.UTC)
	scope, _ := NewTenantScope(uuid.MustParse("0199ca10-2400-7001-9000-000000000004"))
	tx := &recordingServicePrincipalTransaction{principals: map[uuid.UUID]ServicePrincipal{principalID: {
		ID: principalID, MembershipID: membershipID, Name: "Build Bot", NormalizedName: "build bot",
		Status: PrincipalStatusActive, Version: 1,
	}}}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx}, &fixedIDs{values: []uuid.UUID{auditID}},
		fixedAuthClock{now: now}, allowingAPIKeyCreationLimiter{})

	result, err := usecase.UpdateServicePrincipal(context.Background(), scope, UpdateServicePrincipalCommand{
		PrincipalID: principalID, Name: "Renamed Bot", Status: PrincipalStatusDisabled,
		ExpectedVersion: 1, IdempotencyKey: "disable-sp-1", Actor: validTenantAuthorizationActor(),
	})
	if err != nil {
		t.Fatalf("UpdateServicePrincipal() error = %v", err)
	}
	if result.Principal.Status != PrincipalStatusDisabled || result.Principal.Name != "Renamed Bot" || result.RevokedAPIKeys != 4 || tx.servicePrincipalUpdates != 1 {
		t.Fatalf("result/updates = %#v/%d", result, tx.servicePrincipalUpdates)
	}
	if tx.servicePrincipalAudit.Action != AuditActionServicePrincipalDisabled || tx.servicePrincipalAudit.TargetVersion != 2 {
		t.Fatalf("audit = %#v", tx.servicePrincipalAudit)
	}
}

func TestAPIKeyRevokeIsLifecycleIdempotent(t *testing.T) {
	keyID := uuid.MustParse("0199ca10-2500-7001-9000-000000000001")
	principalID := uuid.MustParse("0199ca10-2500-7001-9000-000000000002")
	auditID := uuid.MustParse("0199ca10-2500-7001-9000-000000000003")
	scope, _ := NewTenantScope(uuid.MustParse("0199ca10-2500-7001-9000-000000000004"))
	tx := &recordingServicePrincipalTransaction{apiKeys: map[uuid.UUID]APIKey{keyID: {
		ID: keyID, PrincipalID: principalID, Status: APIKeyStatusActive, Version: 1,
	}}}
	usecase := NewServicePrincipalUsecase(
		&recordingServicePrincipalUnitOfWork{tx: tx}, &fixedIDs{values: []uuid.UUID{auditID}},
		fixedAuthClock{now: time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)}, allowingAPIKeyCreationLimiter{})

	first, err := usecase.RevokeAPIKey(context.Background(), scope, RevokeAPIKeyCommand{
		KeyID: keyID, IdempotencyKey: "revoke-1", Actor: validTenantAuthorizationActor(),
	})
	if err != nil || first.APIKey.Status != APIKeyStatusRevoked || tx.apiKeyRevokes != 1 {
		t.Fatalf("first revoke = %#v err=%v calls=%d", first, err, tx.apiKeyRevokes)
	}
	second, err := usecase.RevokeAPIKey(context.Background(), scope, RevokeAPIKeyCommand{
		KeyID: keyID, IdempotencyKey: "revoke-2", Actor: validTenantAuthorizationActor(),
	})
	if err != nil || second.APIKey.Status != APIKeyStatusRevoked || tx.apiKeyRevokes != 1 {
		t.Fatalf("second revoke = %#v err=%v calls=%d", second, err, tx.apiKeyRevokes)
	}
}

type recordingServicePrincipalUnitOfWork struct {
	tx *recordingServicePrincipalTransaction
}

func (u *recordingServicePrincipalUnitOfWork) WithinServicePrincipal(ctx context.Context, _ TenantScope, fn func(context.Context, ServicePrincipalTransaction) error) error {
	u.tx.commits++
	return fn(ctx, u.tx)
}

type recordingServicePrincipalTransaction struct {
	roles                   map[uuid.UUID]TenantRole
	principals              map[uuid.UUID]ServicePrincipal
	commits                 int
	mutation                ServicePrincipalCreateMutation
	apiKeyMutation          APIKeyCreateMutation
	apiKeys                 map[uuid.UUID]APIKey
	servicePrincipalUpdates int
	servicePrincipalAudit   SecurityAuditEvent
	apiKeyRevokes           int
	credential              APIKeyCredential
	credentialIssueCalls    int
}

func (t *recordingServicePrincipalTransaction) GetRole(_ context.Context, _ TenantScope, roleID uuid.UUID) (TenantRole, error) {
	return t.roles[roleID], nil
}

func (t *recordingServicePrincipalTransaction) CreateServicePrincipal(_ context.Context, _ TenantScope, mutation ServicePrincipalCreateMutation) error {
	t.mutation = mutation
	return nil
}

func (t *recordingServicePrincipalTransaction) GetServicePrincipal(_ context.Context, _ TenantScope, principalID uuid.UUID) (ServicePrincipal, error) {
	principal, ok := t.principals[principalID]
	if !ok {
		return ServicePrincipal{}, ErrServicePrincipalNotFound
	}
	return principal, nil
}

func (t *recordingServicePrincipalTransaction) IssueAPIKeyCredential(_ context.Context, keyID uuid.UUID) (APIKeyCredential, error) {
	t.credentialIssueCalls++
	if t.credential.Raw != "" {
		return t.credential, nil
	}
	raw := "ani_" + keyID.String() + "_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGH"
	return APIKeyCredential{
		Raw: raw, DisplayPrefix: "ani_" + keyID.String()[:8], Digest: sha256.Sum256([]byte(raw)),
	}, nil
}

type recordingAPIKeyCreationLimiter struct {
	calls int
	err   error
}

func (l *recordingAPIKeyCreationLimiter) Acquire(context.Context, TenantScope, uuid.UUID) error {
	l.calls++
	return l.err
}

func (t *recordingServicePrincipalTransaction) CreateAPIKey(_ context.Context, _ TenantScope, mutation APIKeyCreateMutation) error {
	t.apiKeyMutation = mutation
	return nil
}

func (t *recordingServicePrincipalTransaction) UpdateServicePrincipal(_ context.Context, _ TenantScope, principal ServicePrincipal, _ int64) (ServicePrincipal, int64, error) {
	t.servicePrincipalUpdates++
	principal.Version++
	t.principals[principal.ID] = principal
	return principal, 4, nil
}

func (t *recordingServicePrincipalTransaction) GetAPIKey(_ context.Context, _ TenantScope, keyID uuid.UUID) (APIKey, error) {
	key, ok := t.apiKeys[keyID]
	if !ok {
		return APIKey{}, ErrAPIKeyNotFound
	}
	return key, nil
}

func (t *recordingServicePrincipalTransaction) RevokeAPIKey(_ context.Context, _ TenantScope, key APIKey, _ int64) (APIKey, error) {
	t.apiKeyRevokes++
	key.Status = APIKeyStatusRevoked
	key.Version++
	t.apiKeys[key.ID] = key
	return key, nil
}

func (t *recordingServicePrincipalTransaction) AppendAudit(_ context.Context, _ TenantScope, audit SecurityAuditEvent) error {
	t.servicePrincipalAudit = audit
	return nil
}
