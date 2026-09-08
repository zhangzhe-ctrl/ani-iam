package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRefreshSessionRotatesOneBoundaryAndSlidesIdleDeadline(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	uow := &recordingSessionContinuityUnitOfWork{}
	tokens := &recordingSessionTokenCodec{issued: "rotated-access-token"}
	throttle := &recordingSessionThrottle{}
	usecase := newSessionTestUsecase(state, uow, tokens, throttle, now, []uuid.UUID{
		uuid.MustParse("0199c71e-c000-7001-9000-000000000001"),
		uuid.MustParse("0199c71e-c000-7001-9000-000000000002"),
		uuid.MustParse("0199c71e-c000-7001-9000-000000000003"),
		uuid.MustParse("0199c71e-c000-7001-9000-000000000004"),
	})

	result, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken:   "old-refresh-secret",
		CSRFToken:      "csrf-proof",
		Origin:         "https://console.test.example",
		IdempotencyKey: "refresh-1",
	})
	if err != nil {
		t.Fatalf("RefreshSession() error = %v", err)
	}
	if throttle.refreshCalls != 1 || throttle.refreshAttempt.Digest != sha256.Sum256([]byte("old-refresh-secret")) {
		t.Fatalf("refresh throttle = calls:%d attempt:%#v", throttle.refreshCalls, throttle.refreshAttempt)
	}
	if result.RefreshToken != "new-refresh-secret" || result.AccessToken != "rotated-access-token" {
		t.Fatalf("RefreshSession() tokens = access:%q refresh:%q", result.AccessToken, result.RefreshToken)
	}
	wantIdle := now.Add(7 * 24 * time.Hour)
	if !result.Session.IdleExpiresAt.Equal(wantIdle) || !result.Session.AbsoluteExpiry.Equal(state.Session.AbsoluteExpiry) {
		t.Fatalf("Session deadlines = idle:%s absolute:%s", result.Session.IdleExpiresAt, result.Session.AbsoluteExpiry)
	}
	if result.Session.Version != state.Session.Version+1 {
		t.Fatalf("Session version = %d, want %d", result.Session.Version, state.Session.Version+1)
	}
	if result.Grant.ID != state.Grant.ID || result.Grant.Version != state.Grant.Version || result.TenantID != state.TenantID {
		t.Fatalf("RefreshSession() boundary = tenant:%s grant:%#v", result.TenantID, result.Grant)
	}
	if tokens.claims.Subject != state.Principal.ID || tokens.claims.SessionID != state.Session.ID ||
		tokens.claims.GrantID != state.Grant.ID || tokens.claims.GrantVersion != state.Grant.Version ||
		tokens.claims.TenantID != state.TenantID || tokens.claims.Audience != AudienceConsole ||
		!tokens.claims.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("issued Access Token claims = %#v", tokens.claims)
	}
	if uow.rotation == nil {
		t.Fatal("RefreshSession() did not commit a rotation")
	}
	if uow.rotation.Expected.Token.Digest != sha256.Sum256([]byte("old-refresh-secret")) ||
		uow.rotation.ReplacementToken.Digest != sha256.Sum256([]byte("new-refresh-secret")) ||
		uow.rotation.ReplacementToken.FamilyID != state.Family.ID ||
		!uow.rotation.ReplacementToken.ExpiresAt.Equal(state.Session.AbsoluteExpiry) {
		t.Fatalf("rotation mutation = %#v", uow.rotation)
	}
	if uow.rotation.Audit.Action != AuditActionSessionRefreshed ||
		uow.rotation.Audit.TargetID != state.Grant.ID ||
		uow.rotation.Audit.TargetVersion != state.Grant.Version ||
		uow.rotation.Audit.RequestID != "refresh-1" {
		t.Fatalf("refresh Audit = %#v", uow.rotation.Audit)
	}
	if uow.rotation.ReuseAudit.Action != AuditActionRefreshTokenReused ||
		uow.rotation.ReuseAudit.TargetVersion != state.Grant.Version+1 {
		t.Fatalf("reuse Audit fallback = %#v", uow.rotation.ReuseAudit)
	}
}

func TestRefreshSessionReuseRevokesOnlyReferencedBoundaryAndReturnsGenericCredentialFailure(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	state.Token.Status = RefreshTokenStatusConsumed
	state.Token.ConsumedAt = now.Add(-time.Minute)
	uow := &recordingSessionContinuityUnitOfWork{}
	tokens := &recordingSessionTokenCodec{issued: "must-not-be-issued"}
	usecase := newSessionTestUsecase(state, uow, tokens, &recordingSessionThrottle{}, now, []uuid.UUID{
		uuid.MustParse("0199c71e-c000-7002-9000-000000000001"),
	})

	_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken:   "old-refresh-secret",
		CSRFToken:      "csrf-proof",
		Origin:         "https://console.test.example",
		IdempotencyKey: "refresh-reuse-1",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("RefreshSession(reuse) error = %v, want %v", err, ErrInvalidCredential)
	}
	if uow.reuse == nil || uow.rotation != nil {
		t.Fatalf("reuse/rotation mutations = %#v / %#v", uow.reuse, uow.rotation)
	}
	if uow.reuse.Expected.TenantID != state.TenantID || uow.reuse.Expected.Grant.ID != state.Grant.ID ||
		uow.reuse.Expected.Family.ID != state.Family.ID || uow.reuse.Audit.TargetID != state.Grant.ID ||
		uow.reuse.Audit.TargetVersion != state.Grant.Version+1 || uow.reuse.Audit.Action != AuditActionRefreshTokenReused {
		t.Fatalf("reuse mutation = %#v", uow.reuse)
	}
	if tokens.claims.TokenID != uuid.Nil {
		t.Fatalf("Access Token was issued during reuse: %#v", tokens.claims)
	}
}

func TestLogoutSessionBuildsOneCurrentSessionRevocation(t *testing.T) {
	now := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	refresh := activeRefreshSessionState(now)
	logout := LogoutSessionState{Principal: refresh.Principal, Session: refresh.Session}
	uow := &recordingSessionContinuityUnitOfWork{}
	reader := &sessionTestReader{logout: logout, logoutFound: true}
	usecase := NewAuthenticationUsecase(
		reader, acceptingPasswordVerifier{}, &recordingSessionThrottle{}, uow,
		&recordingSessionTokenCodec{}, staticSecretGenerator{},
		&fixedIDs{values: []uuid.UUID{uuid.MustParse("0199c71e-c000-7003-9000-000000000001")}},
		fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	_, err := usecase.LogoutSession(context.Background(), LogoutSessionCommand{
		RefreshToken:   "old-refresh-secret",
		CSRFToken:      "csrf-proof",
		Origin:         "https://console.test.example",
		IdempotencyKey: "logout-1",
	})
	if err != nil {
		t.Fatalf("LogoutSession() error = %v", err)
	}
	if uow.logout == nil || uow.logout.Expected.Session.ID != refresh.Session.ID ||
		uow.logout.Expected.Principal.ID != refresh.Principal.ID ||
		uow.logout.RefreshDigest != sha256.Sum256([]byte("old-refresh-secret")) {
		t.Fatalf("logout mutation = %#v", uow.logout)
	}
	if uow.logout.Audit.Action != AuditActionSessionLoggedOut ||
		uow.logout.Audit.Boundary != AuditBoundaryPrincipal ||
		uow.logout.Audit.TargetID != refresh.Session.ID ||
		uow.logout.Audit.TargetVersion != refresh.Session.Version+1 ||
		uow.logout.Audit.RequestID != "logout-1" {
		t.Fatalf("logout Audit = %#v", uow.logout.Audit)
	}
}

func TestSwitchTenantCreatesIndependentBoundaryWithinCurrentSession(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	source := activeRefreshSessionState(now)
	targetTenantID := uuid.MustParse("0199c71e-c000-7201-9000-000000000001")
	targetMembershipID := uuid.MustParse("0199c71e-c000-7201-9000-000000000002")
	state := TenantSwitchState{
		SourceTenantID:   source.TenantID,
		TargetTenantID:   targetTenantID,
		Principal:        source.Principal,
		MembershipID:     targetMembershipID,
		MembershipStatus: MembershipStatusActive,
		TenantAccess:     TenantAccessStatusActive,
		Lifecycle:        TenantLifecycleStatusActive,
		LifecycleFresh:   true,
		Session:          source.Session,
		SourceGrant:      source.Grant,
	}
	claims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: source.Principal.ID, Audience: AudienceConsole,
		TokenID:   uuid.MustParse("0199c71e-c000-7201-9000-000000000003"),
		SessionID: source.Session.ID, GrantID: source.Grant.ID, GrantVersion: source.Grant.Version,
		TenantID: source.TenantID, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		AuthnMethods: append([]AuditAuthenticationMethod(nil), source.Session.AuthnMethods...),
	}
	uow := &recordingSessionContinuityUnitOfWork{}
	tokens := &recordingSessionTokenCodec{issued: "target-access-token", verified: claims}
	reader := &sessionTestReader{tenantSwitch: state}
	usecase := NewAuthenticationUsecase(
		reader, acceptingPasswordVerifier{}, &recordingSessionThrottle{}, uow, tokens,
		staticSecretGenerator{secret: "target-refresh-secret"},
		&fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199c71e-c000-7201-9000-000000000011"),
			uuid.MustParse("0199c71e-c000-7201-9000-000000000012"),
			uuid.MustParse("0199c71e-c000-7201-9000-000000000013"),
			uuid.MustParse("0199c71e-c000-7201-9000-000000000014"),
			uuid.MustParse("0199c71e-c000-7201-9000-000000000015"),
		}},
		fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	result, err := usecase.SwitchTenant(context.Background(), SwitchTenantCommand{
		RawCredential:  "source-access-token",
		TargetTenantID: targetTenantID,
		IdempotencyKey: "switch-tenant-1",
	})
	if err != nil {
		t.Fatalf("SwitchTenant() error = %v", err)
	}
	if uow.tenantSwitch == nil {
		t.Fatal("SwitchTenant() did not commit a target boundary")
	}
	if result.Session.ID != source.Session.ID || result.TenantID != targetTenantID ||
		result.Grant.ID != uow.tenantSwitch.Grant.ID || result.Grant.Version != 1 ||
		result.RefreshToken != "target-refresh-secret" {
		t.Fatalf("SwitchTenant() result = %#v", result)
	}
	if uow.tenantSwitch.Grant.SessionID != source.Session.ID ||
		uow.tenantSwitch.Grant.MembershipID != targetMembershipID ||
		uow.tenantSwitch.Family.GrantID != uow.tenantSwitch.Grant.ID ||
		uow.tenantSwitch.RefreshToken.FamilyID != uow.tenantSwitch.Family.ID {
		t.Fatalf("target boundary mutation = %#v", uow.tenantSwitch)
	}
	if tokens.claims.TenantID != targetTenantID || tokens.claims.SessionID != source.Session.ID ||
		tokens.claims.GrantID != uow.tenantSwitch.Grant.ID || tokens.claims.GrantVersion != 1 {
		t.Fatalf("target Access Token claims = %#v", tokens.claims)
	}
	if uow.tenantSwitch.Audit.Action != AuditActionTenantSwitched ||
		uow.tenantSwitch.Audit.TargetID != uow.tenantSwitch.Grant.ID ||
		uow.tenantSwitch.Audit.TargetVersion != 1 {
		t.Fatalf("tenant-switch Audit = %#v", uow.tenantSwitch.Audit)
	}
}

func TestRefreshSessionRejectsBossAudienceUntilPlatformGrantExists(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	state.Session.Audience = AudienceBoss
	uow := &recordingSessionContinuityUnitOfWork{}
	tokens := &recordingSessionTokenCodec{issued: "must-not-be-issued"}
	usecase := newSessionTestUsecase(state, uow, tokens, &recordingSessionThrottle{}, now, nil)

	_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
		Origin: "https://boss.test.example", IdempotencyKey: "boss-refresh-1",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("RefreshSession(BOSS Tenant boundary) error = %v, want %v", err, ErrInvalidCredential)
	}
	if uow.rotation != nil || uow.reuse != nil || tokens.claims.TokenID != uuid.Nil {
		t.Fatalf("BOSS Tenant boundary reached mutation/token issue: rotation=%#v reuse=%#v claims=%#v", uow.rotation, uow.reuse, tokens.claims)
	}
}

func TestRefreshSessionDoesNotTreatConsumedBossTenantTokenAsReuse(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 40, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	state.Session.Audience = AudienceBoss
	state.Token.Status = RefreshTokenStatusConsumed
	state.Token.ConsumedAt = now.Add(-time.Minute)
	uow := &recordingSessionContinuityUnitOfWork{}
	usecase := newSessionTestUsecase(
		state, uow, &recordingSessionTokenCodec{issued: "must-not-be-issued"},
		&recordingSessionThrottle{}, now, nil,
	)

	_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
		Origin: "https://boss.test.example", IdempotencyKey: "boss-consumed-refresh-1",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("RefreshSession(consumed BOSS Tenant token) error = %v, want %v", err, ErrInvalidCredential)
	}
	if uow.rotation != nil || uow.reuse != nil {
		t.Fatalf("consumed BOSS Tenant token produced mutation: rotation=%#v reuse=%#v", uow.rotation, uow.reuse)
	}
}

func TestSwitchTenantRejectsBossAudienceUntilPlatformGrantExists(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 45, 0, 0, time.UTC)
	source := activeRefreshSessionState(now)
	targetTenantID := uuid.MustParse("0199c71e-c000-7302-9000-000000000001")
	state := TenantSwitchState{
		SourceTenantID: source.TenantID, TargetTenantID: targetTenantID, Principal: source.Principal,
		MembershipID: uuid.MustParse("0199c71e-c000-7302-9000-000000000002"), MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		Session: source.Session, SourceGrant: source.Grant,
	}
	state.Session.Audience = AudienceBoss
	claims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: source.Principal.ID, Audience: AudienceBoss,
		TokenID: uuid.MustParse("0199c71e-c000-7302-9000-000000000003"), SessionID: source.Session.ID,
		GrantID: source.Grant.ID, GrantVersion: source.Grant.Version, TenantID: source.TenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), AuthnMethods: source.Session.AuthnMethods,
	}
	uow := &recordingSessionContinuityUnitOfWork{}
	tokens := &recordingSessionTokenCodec{issued: "must-not-be-issued", verified: claims}
	usecase := NewAuthenticationUsecase(
		&sessionTestReader{tenantSwitch: state}, acceptingPasswordVerifier{}, &recordingSessionThrottle{}, uow,
		tokens, staticSecretGenerator{}, &fixedIDs{}, fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	_, err := usecase.SwitchTenant(context.Background(), SwitchTenantCommand{
		RawCredential: "boss-access-token", TargetTenantID: targetTenantID, IdempotencyKey: "boss-switch-1",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("SwitchTenant(BOSS audience) error = %v, want %v", err, ErrInvalidCredential)
	}
	if uow.tenantSwitch != nil || tokens.claims.TokenID != uuid.Nil {
		t.Fatalf("BOSS tenant switch reached mutation/token issue: mutation=%#v claims=%#v", uow.tenantSwitch, tokens.claims)
	}
}

func TestRefreshSessionFailsClosedBeforePersistenceWhenRedisIsUnavailable(t *testing.T) {
	now := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	reader := &sessionTestReader{refresh: activeRefreshSessionState(now)}
	throttle := &recordingSessionThrottle{err: errors.New("redis unavailable")}
	uow := &recordingSessionContinuityUnitOfWork{}
	usecase := NewAuthenticationUsecase(
		reader, acceptingPasswordVerifier{}, throttle, uow, &recordingSessionTokenCodec{},
		staticSecretGenerator{}, &fixedIDs{}, fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
		Origin: "https://console.test.example", IdempotencyKey: "redis-down-refresh",
	})
	if !errors.Is(err, ErrAuthenticationDependency) {
		t.Fatalf("RefreshSession(Redis unavailable) error = %v", err)
	}
	if reader.refreshCalls != 0 || uow.rotation != nil || uow.reuse != nil {
		t.Fatalf("persistence was reached after Redis failure: reader=%d rotation=%#v reuse=%#v", reader.refreshCalls, uow.rotation, uow.reuse)
	}
}

func TestRefreshSessionRevalidatesBoundaryAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 7, 13, 30, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*RefreshSessionState)
		want   error
	}{
		{name: "principal", mutate: func(state *RefreshSessionState) { state.Principal.Status = PrincipalStatusDisabled }, want: ErrPrincipalInactive},
		{name: "membership", mutate: func(state *RefreshSessionState) { state.MembershipStatus = MembershipStatusSuspended }, want: ErrMembershipInactive},
		{name: "tenant access", mutate: func(state *RefreshSessionState) { state.TenantAccess = TenantAccessStatusSuspended }, want: ErrTenantAccessInactive},
		{name: "lifecycle stale", mutate: func(state *RefreshSessionState) { state.LifecycleFresh = false }, want: ErrTenantLifecycleStale},
		{name: "lifecycle blocked", mutate: func(state *RefreshSessionState) { state.Lifecycle = TenantLifecycleStatus("suspended") }, want: ErrTenantLifecycleBlocked},
		{name: "idle expired", mutate: func(state *RefreshSessionState) { state.Session.IdleExpiresAt = now }, want: ErrInvalidCredential},
		{name: "absolute expired", mutate: func(state *RefreshSessionState) { state.Session.AbsoluteExpiry = now }, want: ErrInvalidCredential},
		{name: "refresh expired", mutate: func(state *RefreshSessionState) { state.Token.ExpiresAt = now }, want: ErrInvalidCredential},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := activeRefreshSessionState(now)
			test.mutate(&state)
			uow := &recordingSessionContinuityUnitOfWork{}
			usecase := newSessionTestUsecase(state, uow, &recordingSessionTokenCodec{}, &recordingSessionThrottle{}, now, nil)
			_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
				RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
				Origin: "https://console.test.example", IdempotencyKey: "invalid-refresh",
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("RefreshSession() error = %v, want %v", err, test.want)
			}
			if uow.rotation != nil || uow.reuse != nil {
				t.Fatalf("invalid state produced mutation: rotation=%#v reuse=%#v", uow.rotation, uow.reuse)
			}
		})
	}
}

func TestRefreshSessionConcurrentReuseResultReturnsGenericFailure(t *testing.T) {
	now := time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	uow := &recordingSessionContinuityUnitOfWork{rotationResult: RefreshSessionMutationResult{Reused: true}}
	usecase := newSessionTestUsecase(state, uow, &recordingSessionTokenCodec{issued: "discarded-access"}, &recordingSessionThrottle{}, now, []uuid.UUID{
		uuid.MustParse("0199c71e-c000-7401-9000-000000000001"),
		uuid.MustParse("0199c71e-c000-7401-9000-000000000002"),
		uuid.MustParse("0199c71e-c000-7401-9000-000000000003"),
		uuid.MustParse("0199c71e-c000-7401-9000-000000000004"),
	})

	_, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
		Origin: "https://console.test.example", IdempotencyKey: "concurrent-reuse",
	})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("RefreshSession(concurrent reuse) error = %v, want %v", err, ErrInvalidCredential)
	}
}

func TestRefreshSessionReturnsTheCommittedSessionVersion(t *testing.T) {
	now := time.Date(2026, 9, 7, 14, 15, 0, 0, time.UTC)
	state := activeRefreshSessionState(now)
	uow := &recordingSessionContinuityUnitOfWork{rotationResult: RefreshSessionMutationResult{SessionVersion: 9}}
	usecase := newSessionTestUsecase(state, uow, &recordingSessionTokenCodec{issued: "access"}, &recordingSessionThrottle{}, now, []uuid.UUID{
		uuid.MustParse("0199c71e-c000-7411-9000-000000000001"),
		uuid.MustParse("0199c71e-c000-7411-9000-000000000002"),
		uuid.MustParse("0199c71e-c000-7411-9000-000000000003"),
		uuid.MustParse("0199c71e-c000-7411-9000-000000000004"),
	})

	result, err := usecase.RefreshSession(context.Background(), RefreshSessionCommand{
		RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
		Origin: "https://console.test.example", IdempotencyKey: "committed-version",
	})
	if err != nil {
		t.Fatalf("RefreshSession() error = %v", err)
	}
	if result.Session.Version != 9 {
		t.Fatalf("Session version = %d, want committed version 9", result.Session.Version)
	}
}

func TestLogoutSessionIsIdempotentWithoutMutationForUnknownOrRevokedSession(t *testing.T) {
	now := time.Date(2026, 9, 7, 14, 30, 0, 0, time.UTC)
	active := activeRefreshSessionState(now)
	tests := []struct {
		name  string
		state LogoutSessionState
		found bool
	}{
		{name: "unknown", found: false},
		{name: "already revoked", found: true, state: LogoutSessionState{Principal: active.Principal, Session: func() Session {
			session := active.Session
			session.Status = SessionStatusRevoked
			return session
		}()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uow := &recordingSessionContinuityUnitOfWork{}
			usecase := NewAuthenticationUsecase(
				&sessionTestReader{logout: test.state, logoutFound: test.found}, acceptingPasswordVerifier{},
				&recordingSessionThrottle{}, uow, &recordingSessionTokenCodec{}, staticSecretGenerator{},
				&fixedIDs{}, fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

			_, err := usecase.LogoutSession(context.Background(), LogoutSessionCommand{
				RefreshToken: "old-refresh-secret", CSRFToken: "csrf-proof",
				Origin: "https://console.test.example", IdempotencyKey: "logout-idempotent",
			})
			if err != nil {
				t.Fatalf("LogoutSession() error = %v", err)
			}
			if uow.logout != nil {
				t.Fatalf("idempotent logout mutation = %#v", uow.logout)
			}
		})
	}
}

func TestSwitchTenantRevalidatesTargetBoundary(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 0, 0, 0, time.UTC)
	source := activeRefreshSessionState(now)
	targetTenantID := uuid.MustParse("0199c71e-c000-7501-9000-000000000001")
	base := TenantSwitchState{
		SourceTenantID: source.TenantID, TargetTenantID: targetTenantID, Principal: source.Principal,
		MembershipID: uuid.MustParse("0199c71e-c000-7501-9000-000000000002"), MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		Session: source.Session, SourceGrant: source.Grant,
	}
	claims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: source.Principal.ID, Audience: source.Session.Audience,
		TokenID: uuid.MustParse("0199c71e-c000-7501-9000-000000000003"), SessionID: source.Session.ID,
		GrantID: source.Grant.ID, GrantVersion: source.Grant.Version, TenantID: source.TenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), AuthnMethods: source.Session.AuthnMethods,
	}
	tests := []struct {
		name   string
		mutate func(*TenantSwitchState)
		want   error
	}{
		{name: "principal", mutate: func(state *TenantSwitchState) { state.Principal.Status = PrincipalStatusDisabled }, want: ErrPrincipalInactive},
		{name: "membership", mutate: func(state *TenantSwitchState) { state.MembershipStatus = MembershipStatusSuspended }, want: ErrMembershipInactive},
		{name: "tenant access", mutate: func(state *TenantSwitchState) { state.TenantAccess = TenantAccessStatusSuspended }, want: ErrTenantAccessInactive},
		{name: "lifecycle stale", mutate: func(state *TenantSwitchState) { state.LifecycleFresh = false }, want: ErrTenantLifecycleStale},
		{name: "lifecycle blocked", mutate: func(state *TenantSwitchState) { state.Lifecycle = TenantLifecycleStatus("suspended") }, want: ErrTenantLifecycleBlocked},
		{name: "session", mutate: func(state *TenantSwitchState) { state.Session.Status = SessionStatusRevoked }, want: ErrInvalidCredential},
		{name: "source grant version", mutate: func(state *TenantSwitchState) { state.SourceGrant.Version++ }, want: ErrInvalidCredential},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := base
			test.mutate(&state)
			uow := &recordingSessionContinuityUnitOfWork{}
			usecase := NewAuthenticationUsecase(
				&sessionTestReader{tenantSwitch: state}, acceptingPasswordVerifier{}, &recordingSessionThrottle{},
				uow, &recordingSessionTokenCodec{verified: claims}, staticSecretGenerator{},
				&fixedIDs{}, fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

			_, err := usecase.SwitchTenant(context.Background(), SwitchTenantCommand{
				RawCredential: "source-access", TargetTenantID: targetTenantID, IdempotencyKey: "invalid-switch",
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("SwitchTenant() error = %v, want %v", err, test.want)
			}
			if uow.tenantSwitch != nil {
				t.Fatalf("invalid switch mutation = %#v", uow.tenantSwitch)
			}
		})
	}
}

func TestSwitchTenantRotatesExistingBoundaryAndGrantVersion(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 30, 0, 0, time.UTC)
	source := activeRefreshSessionState(now)
	targetTenantID := uuid.MustParse("0199c71e-c000-7601-9000-000000000001")
	targetGrant := source.Grant
	targetGrant.ID = uuid.MustParse("0199c71e-c000-7601-9000-000000000002")
	targetGrant.Version = 4
	targetFamily := source.Family
	targetFamily.ID = uuid.MustParse("0199c71e-c000-7601-9000-000000000003")
	targetFamily.GrantID = targetGrant.ID
	state := TenantSwitchState{
		SourceTenantID: source.TenantID, TargetTenantID: targetTenantID, Principal: source.Principal,
		MembershipID: targetGrant.MembershipID, MembershipStatus: MembershipStatusActive,
		TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true,
		Session: source.Session, SourceGrant: source.Grant, TargetGrant: &targetGrant, TargetFamily: &targetFamily,
	}
	claims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: source.Principal.ID, Audience: source.Session.Audience,
		TokenID: uuid.MustParse("0199c71e-c000-7601-9000-000000000004"), SessionID: source.Session.ID,
		GrantID: source.Grant.ID, GrantVersion: source.Grant.Version, TenantID: source.TenantID,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), AuthnMethods: source.Session.AuthnMethods,
	}
	uow := &recordingSessionContinuityUnitOfWork{}
	codec := &recordingSessionTokenCodec{issued: "target-access", verified: claims}
	usecase := NewAuthenticationUsecase(
		&sessionTestReader{tenantSwitch: state}, acceptingPasswordVerifier{}, &recordingSessionThrottle{}, uow, codec,
		staticSecretGenerator{secret: "target-refresh"}, &fixedIDs{values: []uuid.UUID{
			uuid.MustParse("0199c71e-c000-7601-9000-000000000011"),
			uuid.MustParse("0199c71e-c000-7601-9000-000000000012"),
			uuid.MustParse("0199c71e-c000-7601-9000-000000000013"),
		}}, fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

	result, err := usecase.SwitchTenant(context.Background(), SwitchTenantCommand{
		RawCredential: "source-access", TargetTenantID: targetTenantID, IdempotencyKey: "rotate-target",
	})
	if err != nil {
		t.Fatalf("SwitchTenant(existing) error = %v", err)
	}
	if result.Grant.ID != targetGrant.ID || result.Grant.Version != targetGrant.Version+1 ||
		uow.tenantSwitch == nil || uow.tenantSwitch.Family.ID != targetFamily.ID ||
		uow.tenantSwitch.RefreshToken.FamilyID != targetFamily.ID || codec.claims.GrantVersion != targetGrant.Version+1 {
		t.Fatalf("existing target rotation result=%#v mutation=%#v claims=%#v", result, uow.tenantSwitch, codec.claims)
	}
}

func activeRefreshSessionState(now time.Time) RefreshSessionState {
	principalID := uuid.MustParse("0199c71e-c000-7101-9000-000000000001")
	sessionID := uuid.MustParse("0199c71e-c000-7101-9000-000000000002")
	grantID := uuid.MustParse("0199c71e-c000-7101-9000-000000000003")
	familyID := uuid.MustParse("0199c71e-c000-7101-9000-000000000004")
	return RefreshSessionState{
		TenantID:         uuid.MustParse("0199c71e-c000-7101-9000-000000000005"),
		Principal:        Principal{ID: principalID, Status: PrincipalStatusActive},
		MembershipStatus: MembershipStatusActive,
		TenantAccess:     TenantAccessStatusActive,
		Lifecycle:        TenantLifecycleStatusActive,
		LifecycleFresh:   true,
		Session: Session{
			ID: sessionID, PrincipalID: principalID, Audience: AudienceConsole,
			Status: SessionStatusActive, Version: 1,
			AuthnMethods:  []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
			IdleExpiresAt: now.Add(24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour),
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
		Grant: SessionGrant{
			ID: grantID, SessionID: sessionID,
			MembershipID: uuid.MustParse("0199c71e-c000-7101-9000-000000000006"),
			Status:       GrantStatusActive, Version: 3,
		},
		Family: RefreshTokenFamily{ID: familyID, GrantID: grantID, Status: GrantStatusActive, Version: 2},
		Token: RefreshToken{
			ID: uuid.MustParse("0199c71e-c000-7101-9000-000000000007"), FamilyID: familyID,
			Digest: sha256.Sum256([]byte("old-refresh-secret")), Status: RefreshTokenStatusActive,
			IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour),
		},
	}
}

func newSessionTestUsecase(
	state RefreshSessionState,
	uow *recordingSessionContinuityUnitOfWork,
	tokens *recordingSessionTokenCodec,
	throttle *recordingSessionThrottle,
	now time.Time,
	ids []uuid.UUID,
) *AuthenticationUsecase {
	return NewAuthenticationUsecase(
		&sessionTestReader{refresh: state},
		acceptingPasswordVerifier{},
		throttle,
		uow,
		tokens,
		staticSecretGenerator{secret: "new-refresh-secret"},
		&fixedIDs{values: ids},
		fixedAuthClock{now: now}, allowingAPIKeyUsageObserver{})

}

type sessionTestReader struct {
	refresh      RefreshSessionState
	refreshErr   error
	refreshCalls int
	logout       LogoutSessionState
	logoutFound  bool
	tenantSwitch TenantSwitchState
}

func (r *sessionTestReader) LookupPasswordLogin(context.Context, TenantScope, string) (PasswordLoginState, error) {
	return PasswordLoginState{}, nil
}

func (r *sessionTestReader) LookupPasswordActionTarget(context.Context, string, Audience) (PasswordActionTarget, bool, error) {
	return PasswordActionTarget{}, false, nil
}

func (r *sessionTestReader) LookupRefreshSession(context.Context, [sha256.Size]byte) (RefreshSessionState, error) {
	r.refreshCalls++
	return r.refresh, r.refreshErr
}

func (r *sessionTestReader) LookupLogoutSession(context.Context, [sha256.Size]byte) (LogoutSessionState, bool, error) {
	return r.logout, r.logoutFound, nil
}

func (r *sessionTestReader) LookupTenantSwitch(context.Context, TenantScope, AccessTokenClaims) (TenantSwitchState, error) {
	return r.tenantSwitch, nil
}

func (*sessionTestReader) Revision() string { return testPolicyRevision }
func (*sessionTestReader) Lookup(string) (AuthorizationPolicy, bool) {
	return AuthorizationPolicy{}, false
}
func (*sessionTestReader) GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, ErrAPIKeyNotFound
}
func (*sessionTestReader) LookupAuthorization(context.Context, TenantScope, AuthorizationLookup) (AuthorizationState, error) {
	return AuthorizationState{}, ErrAuthenticationDependency
}
func (*sessionTestReader) RecordDeniedAuthorization(context.Context, TenantScope, SecurityAuditEvent) error {
	return ErrAuthenticationDependency
}
func (*sessionTestReader) RecordUnboundAuthorization(context.Context, SecurityAuditEvent) error {
	return ErrAuthenticationDependency
}
func (*sessionTestReader) LookupAPIKeyCredential(context.Context, TenantScope, uuid.UUID, string) (APIKey, error) {
	return APIKey{}, ErrAPIKeyNotFound
}
func (*sessionTestReader) LookupAPIKeyAuthorization(context.Context, TenantScope, uuid.UUID, string, []string) (APIKeyAuthorizationState, error) {
	return APIKeyAuthorizationState{}, ErrAPIKeyNotFound
}

type recordingSessionContinuityUnitOfWork struct {
	rotation       *RefreshSessionMutation
	rotationResult RefreshSessionMutationResult
	err            error
	reuse          *RefreshReuseMutation
	logout         *LogoutSessionMutation
	tenantSwitch   *TenantSwitchMutation
}

func (u *recordingSessionContinuityUnitOfWork) CommitLogin(context.Context, TenantScope, LoginMutation) error {
	return nil
}

func (u *recordingSessionContinuityUnitOfWork) RecordLoginFailure(context.Context, TenantScope, LoginFailureMutation) error {
	return nil
}
func (*recordingSessionContinuityUnitOfWork) RecordTenantPrincipalValidation(context.Context, TenantScope, SecurityAuditEvent) error {
	return nil
}
func (*recordingSessionContinuityUnitOfWork) RecordUnboundPrincipalValidation(context.Context, SecurityAuditEvent) error {
	return nil
}

func (u *recordingSessionContinuityUnitOfWork) RequestPasswordAction(context.Context, PasswordActionRequestMutation) (RequestPasswordActionResult, error) {
	return RequestPasswordActionResult{}, nil
}

func (u *recordingSessionContinuityUnitOfWork) CompletePasswordAction(context.Context, PasswordActionCompletion) (CompletePasswordActionResult, error) {
	return CompletePasswordActionResult{}, nil
}

func (u *recordingSessionContinuityUnitOfWork) RotateRefreshSession(_ context.Context, mutation RefreshSessionMutation) (RefreshSessionMutationResult, error) {
	u.rotation = &mutation
	if !u.rotationResult.Reused && u.rotationResult.SessionVersion == 0 {
		u.rotationResult.SessionVersion = mutation.Expected.Session.Version + 1
	}
	return u.rotationResult, u.err
}

func (u *recordingSessionContinuityUnitOfWork) RevokeRefreshReuse(_ context.Context, mutation RefreshReuseMutation) (bool, error) {
	u.reuse = &mutation
	return true, u.err
}

func (u *recordingSessionContinuityUnitOfWork) LogoutSession(_ context.Context, mutation LogoutSessionMutation) (LogoutSessionMutationResult, error) {
	u.logout = &mutation
	return LogoutSessionMutationResult{}, u.err
}

func (u *recordingSessionContinuityUnitOfWork) SwitchTenant(_ context.Context, _ TenantScope, mutation TenantSwitchMutation) error {
	u.tenantSwitch = &mutation
	return u.err
}

type recordingSessionTokenCodec struct {
	issued    string
	claims    AccessTokenClaims
	verified  AccessTokenClaims
	verifyErr error
}

func (c *recordingSessionTokenCodec) Issue(_ context.Context, claims AccessTokenClaims) (string, error) {
	c.claims = claims
	return c.issued, nil
}

func (c *recordingSessionTokenCodec) Verify(context.Context, string) (AccessTokenClaims, error) {
	return c.verified, c.verifyErr
}

func (*recordingSessionTokenCodec) IssuePasswordAction(context.Context, PasswordActionTokenClaims) (string, error) {
	return "password-action-token", nil
}

func (*recordingSessionTokenCodec) VerifyPasswordAction(context.Context, string) (PasswordActionTokenClaims, error) {
	return PasswordActionTokenClaims{}, ErrPasswordActionInvalid
}

type recordingSessionThrottle struct {
	refreshCalls   int
	refreshAttempt RefreshThrottleAttempt
	err            error
}

func (*recordingSessionThrottle) Check(context.Context, LoginThrottleAttempt) error { return nil }
func (*recordingSessionThrottle) RecordFailure(context.Context, LoginThrottleAttempt) error {
	return nil
}
func (*recordingSessionThrottle) Reset(context.Context, LoginThrottleAttempt) error { return nil }
func (t *recordingSessionThrottle) CheckRefresh(_ context.Context, attempt RefreshThrottleAttempt) error {
	t.refreshCalls++
	t.refreshAttempt = attempt
	return t.err
}

// Existing authentication fakes remain deliberately inert for the new
// continuity seam unless a session test supplies a dedicated recorder.
func (staticPasswordLoginReader) LookupRefreshSession(context.Context, [sha256.Size]byte) (RefreshSessionState, error) {
	return RefreshSessionState{}, ErrInvalidCredential
}
func (staticPasswordLoginReader) LookupLogoutSession(context.Context, [sha256.Size]byte) (LogoutSessionState, bool, error) {
	return LogoutSessionState{}, false, nil
}
func (staticPasswordLoginReader) LookupTenantSwitch(context.Context, TenantScope, AccessTokenClaims) (TenantSwitchState, error) {
	return TenantSwitchState{}, ErrInvalidCredential
}
func (*recordingLoginUnitOfWork) RotateRefreshSession(context.Context, RefreshSessionMutation) (RefreshSessionMutationResult, error) {
	return RefreshSessionMutationResult{}, nil
}
func (*recordingLoginUnitOfWork) RevokeRefreshReuse(context.Context, RefreshReuseMutation) (bool, error) {
	return false, nil
}
func (*recordingLoginUnitOfWork) LogoutSession(context.Context, LogoutSessionMutation) (LogoutSessionMutationResult, error) {
	return LogoutSessionMutationResult{}, nil
}
func (*recordingLoginUnitOfWork) SwitchTenant(context.Context, TenantScope, TenantSwitchMutation) error {
	return nil
}
func (allowingLoginThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error { return nil }
func (*recordingLoginThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error {
	return nil
}
func (*cooldownRecordingLoginThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error {
	return nil
}
func (t failingLoginThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error {
	return t.err
}
func (t selectiveFailingLoginThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error {
	return t.recordFailureErr
}
func (staticTokenIssuer) Verify(context.Context, string) (AccessTokenClaims, error) {
	return AccessTokenClaims{}, ErrAuthorizationCredentialInvalid
}

func (*passwordActionTestReader) LookupRefreshSession(context.Context, [sha256.Size]byte) (RefreshSessionState, error) {
	return RefreshSessionState{}, ErrInvalidCredential
}
func (*passwordActionTestReader) LookupLogoutSession(context.Context, [sha256.Size]byte) (LogoutSessionState, bool, error) {
	return LogoutSessionState{}, false, nil
}
func (*passwordActionTestReader) LookupTenantSwitch(context.Context, TenantScope, AccessTokenClaims) (TenantSwitchState, error) {
	return TenantSwitchState{}, ErrInvalidCredential
}
func (*passwordActionTestReader) Revision() string { return testPolicyRevision }
func (*passwordActionTestReader) Lookup(string) (AuthorizationPolicy, bool) {
	return AuthorizationPolicy{}, false
}
func (*passwordActionTestReader) GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, ErrAPIKeyNotFound
}
func (*passwordActionTestReader) LookupAuthorization(context.Context, TenantScope, AuthorizationLookup) (AuthorizationState, error) {
	return AuthorizationState{}, ErrAuthenticationDependency
}
func (*passwordActionTestReader) RecordDeniedAuthorization(context.Context, TenantScope, SecurityAuditEvent) error {
	return ErrAuthenticationDependency
}
func (*passwordActionTestReader) RecordUnboundAuthorization(context.Context, SecurityAuditEvent) error {
	return ErrAuthenticationDependency
}
func (*passwordActionTestReader) LookupAPIKeyCredential(context.Context, TenantScope, uuid.UUID, string) (APIKey, error) {
	return APIKey{}, ErrAPIKeyNotFound
}
func (*passwordActionTestReader) LookupAPIKeyAuthorization(context.Context, TenantScope, uuid.UUID, string, []string) (APIKeyAuthorizationState, error) {
	return APIKeyAuthorizationState{}, ErrAPIKeyNotFound
}
func (*passwordActionTestUnitOfWork) RotateRefreshSession(context.Context, RefreshSessionMutation) (RefreshSessionMutationResult, error) {
	return RefreshSessionMutationResult{}, nil
}
func (*passwordActionTestUnitOfWork) RevokeRefreshReuse(context.Context, RefreshReuseMutation) (bool, error) {
	return false, nil
}
func (*passwordActionTestUnitOfWork) LogoutSession(context.Context, LogoutSessionMutation) (LogoutSessionMutationResult, error) {
	return LogoutSessionMutationResult{}, nil
}
func (*passwordActionTestUnitOfWork) SwitchTenant(context.Context, TenantScope, TenantSwitchMutation) error {
	return nil
}
func (*passwordActionTestTokenCodec) Verify(context.Context, string) (AccessTokenClaims, error) {
	return AccessTokenClaims{}, ErrAuthorizationCredentialInvalid
}
