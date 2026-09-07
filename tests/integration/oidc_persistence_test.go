//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

var oidcPersistenceTenantID = uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe500")
var oidcPersistenceOtherTenantID = uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe560")

func TestPostgresOIDCLoginAndIdentityLinkAreTransactional(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	principalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe501")
	otherPrincipalID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe502")
	membershipID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe503")
	otherMembershipID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe561")
	seedSessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe504")
	seedGrantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe505")
	existingIdentityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe506")
	seedPool := mustPool(t, environment.migrationDSN(primaryDB))
	seedStatements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO principals (id, principal_type, status, version, created_at, updated_at) VALUES ($1, 'human', 'active', 1, $3, $3), ($2, 'human', 'active', 1, $3, $3)`, []any{principalID, otherPrincipalID, now}},
		{`INSERT INTO tenant_access (tenant_id, status, version, created_at, updated_at) VALUES ($1, 'active', 1, $3, $3), ($2, 'active', 1, $3, $3)`, []any{oidcPersistenceTenantID, oidcPersistenceOtherTenantID, now}},
		{`INSERT INTO tenant_lifecycle_projections (tenant_id, status, lifecycle_version, effective_at, observed_at, fresh_until) VALUES ($1, 'active', 1, $3::timestamptz, $3::timestamptz, $3::timestamptz + interval '1 hour'), ($2, 'active', 1, $3::timestamptz, $3::timestamptz, $3::timestamptz + interval '1 hour')`, []any{oidcPersistenceTenantID, oidcPersistenceOtherTenantID, now}},
		{`INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, $4, $4)`, []any{oidcPersistenceTenantID, membershipID, principalID, now}},
		{`INSERT INTO tenant_memberships (tenant_id, id, principal_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, 'active', 1, $4, $4)`, []any{oidcPersistenceOtherTenantID, otherMembershipID, otherPrincipalID, now}},
		{`INSERT INTO verified_emails (principal_id, normalized_email, verified_at, created_at, updated_at) VALUES ($1, 'user@example.com', $3, $3, $3), ($2, 'other@example.com', $3, $3, $3)`, []any{principalID, otherPrincipalID, now}},
		{`INSERT INTO identities (id, principal_id, provider, issuer, subject, status, version, created_at, updated_at) VALUES ($1, $2, 'dex', 'https://dex.test.example', 'existing-subject', 'active', 1, $3, $3)`, []any{existingIdentityID, principalID, now}},
		{`INSERT INTO sessions (id, principal_id, audience, status, authn_methods, device_name, idle_expires_at, absolute_expires_at, reauthenticated_at, version, created_at, updated_at) VALUES ($1, $2, 'console', 'active', ARRAY['password']::text[], 'browser', $3::timestamptz + interval '7 days', $3::timestamptz + interval '30 days', $3::timestamptz, 1, $3::timestamptz, $3::timestamptz)`, []any{seedSessionID, principalID, now}},
		{`INSERT INTO session_grants (tenant_id, id, session_id, membership_id, status, version, created_at, updated_at) VALUES ($1, $2, $3, $4, 'active', 3, $5, $5)`, []any{oidcPersistenceTenantID, seedGrantID, seedSessionID, membershipID, now}},
	}
	var err error
	failedSeed := -1
	for index, statement := range seedStatements {
		if _, err = seedPool.Exec(ctx, statement.query, statement.args...); err != nil {
			failedSeed = index
			break
		}
	}
	seedPool.Close()
	if err != nil {
		t.Fatalf("seed OIDC persistence fixture %d: %v", failedSeed, err)
	}

	postgresData := data.NewData(environment.runtimePool)
	reader := data.NewPostgresOIDCReader(postgresData)
	uow := data.NewPostgresOIDCUnitOfWork(postgresData)
	scope := mustTenantScope(t, oidcPersistenceTenantID)
	otherScope := mustTenantScope(t, oidcPersistenceOtherTenantID)
	loginState, err := reader.LookupOIDCLogin(ctx, scope, "dex", "https://dex.test.example", "existing-subject")
	if err != nil || loginState.PrincipalID != principalID || loginState.NormalizedEmail != "user@example.com" {
		t.Fatalf("LookupOIDCLogin() = %#v, %v", loginState, err)
	}
	reauthentication, err := reader.LookupOIDCReauthentication(ctx, scope, biz.AccessTokenClaims{
		Subject: principalID, SessionID: seedSessionID, GrantID: seedGrantID,
		GrantVersion: 3, TenantID: oidcPersistenceTenantID,
	})
	if err != nil || reauthentication.ReauthenticatedAt.IsZero() || reauthentication.GrantVersion != 3 {
		t.Fatalf("LookupOIDCReauthentication() = %#v, %v", reauthentication, err)
	}
	if _, err := reader.LookupOIDCLogin(ctx, otherScope, "dex", "https://dex.test.example", "existing-subject"); !errors.Is(err, biz.ErrOIDCIdentityNotFound) {
		t.Fatalf("LookupOIDCLogin(other Tenant) error = %v, want ErrOIDCIdentityNotFound", err)
	}
	if _, err := reader.LookupOIDCReauthentication(ctx, otherScope, biz.AccessTokenClaims{
		Subject: principalID, SessionID: seedSessionID, GrantID: seedGrantID,
		GrantVersion: 3, TenantID: oidcPersistenceOtherTenantID,
	}); !errors.Is(err, biz.ErrOIDCReauthenticationRequired) {
		t.Fatalf("LookupOIDCReauthentication(other Tenant) error = %v, want ErrOIDCReauthenticationRequired", err)
	}
	failureAuditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe507")
	failureAudit := oidcFailureAudit(failureAuditID, now)
	if err := uow.RecordOIDCLoginFailure(ctx, scope, failureAudit); err != nil {
		t.Fatalf("RecordOIDCLoginFailure() error = %v", err)
	}
	if err := uow.RecordOIDCLoginFailure(ctx, scope, failureAudit); !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("RecordOIDCLoginFailure(duplicate Audit) error = %v, want ErrAuditConflict", err)
	}
	var failureAuditTenantID uuid.UUID
	if err := environment.runtimePool.QueryRow(ctx, `SELECT tenant_id FROM iam_audit_events WHERE event_id = $1`, failureAuditID).Scan(&failureAuditTenantID); err != nil || failureAuditTenantID != oidcPersistenceTenantID {
		t.Fatalf("OIDC failure Audit Tenant = %s, %v", failureAuditTenantID, err)
	}
	linkFailureAuditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe508")
	linkFailureAudit := oidcIdentityLinkFailureAudit(linkFailureAuditID, principalID, now)
	if err := uow.RecordOIDCIdentityLinkFailure(ctx, scope, linkFailureAudit); err != nil {
		t.Fatalf("RecordOIDCIdentityLinkFailure() error = %v", err)
	}
	if err := uow.RecordOIDCIdentityLinkFailure(ctx, scope, linkFailureAudit); !errors.Is(err, biz.ErrAuditConflict) {
		t.Fatalf("RecordOIDCIdentityLinkFailure(duplicate Audit) error = %v, want ErrAuditConflict", err)
	}
	var linkFailureActorID uuid.UUID
	if err := environment.runtimePool.QueryRow(ctx, `SELECT actor_id FROM iam_audit_events WHERE tenant_id = $1 AND event_id = $2`, oidcPersistenceTenantID, linkFailureAuditID).Scan(&linkFailureActorID); err != nil || linkFailureActorID != principalID {
		t.Fatalf("OIDC Identity Link failure Audit Actor = %s, %v", linkFailureActorID, err)
	}

	loginSessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe511")
	loginGrantID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe512")
	loginFamilyID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe513")
	loginTokenID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe514")
	loginAuditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe515")
	loginMutation := biz.OIDCLoginMutation{
		IdentityID: existingIdentityID, Provider: "dex", Issuer: "https://dex.test.example",
		Subject: "existing-subject", NormalizedEmail: "user@example.com",
		Session:       biz.Session{ID: loginSessionID, PrincipalID: principalID, Audience: biz.AudienceConsole, Status: biz.SessionStatusActive, AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodOIDC}, DeviceName: "browser", IdleExpiresAt: now.Add(7 * 24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour), ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now},
		Grant:         biz.SessionGrant{ID: loginGrantID, SessionID: loginSessionID, MembershipID: membershipID, Status: biz.GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now},
		RefreshFamily: biz.RefreshTokenFamily{ID: loginFamilyID, GrantID: loginGrantID, Status: biz.GrantStatusActive, CreatedAt: now, UpdatedAt: now},
		RefreshToken:  biz.RefreshToken{ID: loginTokenID, FamilyID: loginFamilyID, Digest: sha256.Sum256([]byte("test-only-refresh")), IssuedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)},
		Audit:         oidcAudit(loginAuditID, principalID, loginSessionID, biz.AuditActionOIDCLoginSucceeded, biz.AuditReasonOIDCLogin, now),
	}
	if err := uow.CommitOIDCLogin(ctx, otherScope, loginMutation); !errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("CommitOIDCLogin(other Tenant) error = %v, want ErrInvalidCredential", err)
	}
	var crossTenantSessionCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = $1`, loginSessionID).Scan(&crossTenantSessionCount); err != nil || crossTenantSessionCount != 0 {
		t.Fatalf("cross-Tenant OIDC Session count = %d, %v", crossTenantSessionCount, err)
	}
	err = uow.CommitOIDCLogin(ctx, scope, loginMutation)
	if err != nil {
		t.Fatalf("CommitOIDCLogin() error = %v", err)
	}
	var persistedOIDCMethods []string
	if err := environment.runtimePool.QueryRow(ctx, `SELECT authn_methods FROM sessions WHERE id = $1`, loginSessionID).Scan(&persistedOIDCMethods); err != nil || len(persistedOIDCMethods) != 1 || persistedOIDCMethods[0] != "oidc" {
		t.Fatalf("OIDC Session authn_methods = %#v, %v, want [oidc]", persistedOIDCMethods, err)
	}

	if _, err := environment.runtimePool.Exec(ctx, `UPDATE identities SET status = 'disabled', version = version + 1, updated_at = $2 WHERE id = $1`, existingIdentityID, now.Add(time.Second)); err != nil {
		t.Fatalf("disable OIDC Identity: %v", err)
	}
	disabledSessionID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe516")
	if err := uow.CommitOIDCLogin(ctx, scope, biz.OIDCLoginMutation{
		IdentityID: existingIdentityID, Provider: "dex", Issuer: "https://dex.test.example",
		Subject: "existing-subject", NormalizedEmail: "user@example.com",
		Session:       biz.Session{ID: disabledSessionID, PrincipalID: principalID, Audience: biz.AudienceConsole, Status: biz.SessionStatusActive, AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodOIDC}, DeviceName: "browser", IdleExpiresAt: now.Add(7 * 24 * time.Hour), AbsoluteExpiry: now.Add(30 * 24 * time.Hour), ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now},
		Grant:         biz.SessionGrant{ID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe517"), SessionID: disabledSessionID, MembershipID: membershipID, Status: biz.GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now},
		RefreshFamily: biz.RefreshTokenFamily{ID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe518"), GrantID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe517"), Status: biz.GrantStatusActive, CreatedAt: now, UpdatedAt: now},
		RefreshToken:  biz.RefreshToken{ID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe519"), FamilyID: uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe518"), Digest: sha256.Sum256([]byte("disabled-refresh")), IssuedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)},
		Audit:         oidcAudit(uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe51a"), principalID, disabledSessionID, biz.AuditActionOIDCLoginSucceeded, biz.AuditReasonOIDCLogin, now),
	}); !errors.Is(err, biz.ErrInvalidCredential) {
		t.Fatalf("CommitOIDCLogin(disabled Identity) error = %v, want ErrInvalidCredential", err)
	}
	var disabledSessionCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = $1`, disabledSessionID).Scan(&disabledSessionCount); err != nil || disabledSessionCount != 0 {
		t.Fatalf("disabled-Identity Session count = %d, %v", disabledSessionCount, err)
	}

	identityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe521")
	linkAuditID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe522")
	linkMutation := biz.OIDCIdentityLinkMutation{
		IdentityID: identityID, PrincipalID: principalID, Provider: "dex",
		Issuer: "https://dex.test.example", Subject: "linked-subject", NormalizedEmail: "user@example.com",
		SessionID: seedSessionID, GrantID: seedGrantID, ExpectedGrantVersion: 3, ReauthenticatedAfter: now.Add(-10 * time.Minute),
		LinkedAt: now, Audit: oidcAudit(linkAuditID, principalID, identityID, biz.AuditActionOIDCIdentityLinked, biz.AuditReasonOIDCIdentityLink, now),
	}
	if _, err := uow.LinkOIDCIdentity(ctx, otherScope, linkMutation); !errors.Is(err, biz.ErrOIDCReauthenticationRequired) {
		t.Fatalf("LinkOIDCIdentity(other Tenant) error = %v, want ErrOIDCReauthenticationRequired", err)
	}
	var crossTenantIdentityCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM identities WHERE id = $1`, identityID).Scan(&crossTenantIdentityCount); err != nil || crossTenantIdentityCount != 0 {
		t.Fatalf("cross-Tenant OIDC Identity count = %d, %v", crossTenantIdentityCount, err)
	}
	result, err := uow.LinkOIDCIdentity(ctx, scope, linkMutation)
	if err != nil || result.IdentityID != identityID || result.PrincipalID != principalID {
		t.Fatalf("LinkOIDCIdentity() = %#v, %v", result, err)
	}

	conflictID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe531")
	if _, err := uow.LinkOIDCIdentity(ctx, scope, biz.OIDCIdentityLinkMutation{
		IdentityID: conflictID, PrincipalID: principalID, Provider: "dex",
		Issuer: "https://dex.test.example", Subject: "conflicting-email-subject", NormalizedEmail: "other@example.com",
		SessionID: seedSessionID, GrantID: seedGrantID, ExpectedGrantVersion: 3, ReauthenticatedAfter: now.Add(-10 * time.Minute),
		LinkedAt: now, Audit: oidcAudit(uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe532"), principalID, conflictID, biz.AuditActionOIDCIdentityLinked, biz.AuditReasonOIDCIdentityLink, now),
	}); !errors.Is(err, biz.ErrOIDCEmailConflict) {
		t.Fatalf("LinkOIDCIdentity(email conflict) error = %v, want ErrOIDCEmailConflict", err)
	}

	rollbackIdentityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe541")
	if _, err := uow.LinkOIDCIdentity(ctx, scope, biz.OIDCIdentityLinkMutation{
		IdentityID: rollbackIdentityID, PrincipalID: principalID, Provider: "dex",
		Issuer: "https://dex.test.example", Subject: "must-roll-back", NormalizedEmail: "user@example.com",
		SessionID: seedSessionID, GrantID: seedGrantID, ExpectedGrantVersion: 3, ReauthenticatedAfter: now.Add(-10 * time.Minute),
		LinkedAt: now, Audit: oidcAudit(linkAuditID, principalID, rollbackIdentityID, biz.AuditActionOIDCIdentityLinked, biz.AuditReasonOIDCIdentityLink, now),
	}); err == nil {
		t.Fatal("LinkOIDCIdentity(duplicate audit) error = nil")
	}
	var rollbackCount int
	if err := environment.runtimePool.QueryRow(ctx, `SELECT count(*) FROM identities WHERE id = $1`, rollbackIdentityID).Scan(&rollbackCount); err != nil || rollbackCount != 0 {
		t.Fatalf("rolled-back Identity count = %d, %v", rollbackCount, err)
	}

	if _, err := environment.runtimePool.Exec(ctx, `UPDATE sessions SET status = 'revoked', updated_at = $2 WHERE id = $1`, seedSessionID, now.Add(time.Second)); err != nil {
		t.Fatalf("revoke link Session: %v", err)
	}
	revokedIdentityID := uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe551")
	if _, err := uow.LinkOIDCIdentity(ctx, scope, biz.OIDCIdentityLinkMutation{
		IdentityID: revokedIdentityID, PrincipalID: principalID, Provider: "dex",
		Issuer: "https://dex.test.example", Subject: "revoked-session-must-fail", NormalizedEmail: "user@example.com",
		SessionID: seedSessionID, GrantID: seedGrantID, ExpectedGrantVersion: 3, ReauthenticatedAfter: now.Add(-10 * time.Minute),
		LinkedAt: now.Add(time.Second), Audit: oidcAudit(uuid.MustParse("0199c6fb-62e4-7d12-a27f-5f3caa1fe552"), principalID, revokedIdentityID, biz.AuditActionOIDCIdentityLinked, biz.AuditReasonOIDCIdentityLink, now.Add(time.Second)),
	}); !errors.Is(err, biz.ErrOIDCReauthenticationRequired) {
		t.Fatalf("LinkOIDCIdentity(revoked Session) error = %v, want ErrOIDCReauthenticationRequired", err)
	}
}

func oidcAudit(id, principalID, targetID uuid.UUID, action biz.AuditAction, reason biz.AuditReason, now time.Time) biz.SecurityAuditEvent {
	return biz.SecurityAuditEvent{
		ID: id, ActorID: principalID, AuthenticationMethod: biz.AuditAuthenticationMethodOIDC,
		Boundary: biz.AuditBoundaryTenant, Action: action, TargetType: biz.AuditTargetTypeIdentity,
		TargetID: targetID, TargetVersion: 1, Result: biz.AuditResultSucceeded, Reason: reason,
		RequestID: id.String(), CorrelationID: id.String(), DecisionID: id.String(),
		SourceService: biz.AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}

func oidcFailureAudit(id uuid.UUID, now time.Time) biz.SecurityAuditEvent {
	return biz.SecurityAuditEvent{
		ID: id, AuthenticationMethod: biz.AuditAuthenticationMethodAnonymous,
		Boundary: biz.AuditBoundaryTenant, Action: biz.AuditActionOIDCLoginFailed,
		TargetType: biz.AuditTargetTypeOIDCOperation, TargetID: id, TargetVersion: 1,
		Result: biz.AuditResultFailed, Reason: biz.AuditReasonOIDCLoginFailed,
		RequestID: "oidc-login-failure", CorrelationID: "oidc-login-failure", DecisionID: id.String(),
		SourceService: biz.AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}

func oidcIdentityLinkFailureAudit(id, principalID uuid.UUID, now time.Time) biz.SecurityAuditEvent {
	return biz.SecurityAuditEvent{
		ID: id, ActorID: principalID, AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		Boundary: biz.AuditBoundaryTenant, Action: biz.AuditActionOIDCIdentityLinkFailed,
		TargetType: biz.AuditTargetTypeOIDCOperation, TargetID: id, TargetVersion: 1,
		Result: biz.AuditResultFailed, Reason: biz.AuditReasonOIDCIdentityLinkFailed,
		RequestID: "oidc-link-failure", CorrelationID: "oidc-link-failure", DecisionID: id.String(),
		SourceService: biz.AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}
