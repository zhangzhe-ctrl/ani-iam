package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type guardedSessionListReader struct {
	*sessionTestReader
	reads int
}

func (r *guardedSessionListReader) ListOwnedSessions(context.Context, uuid.UUID, uuid.UUID, int) ([]Session, error) {
	r.reads++
	return nil, nil
}
func (r *guardedSessionListReader) ListOwnedSessionGrants(context.Context, TenantScope, uuid.UUID, uuid.UUID) ([]SessionGrant, error) {
	r.reads++
	return nil, nil
}

func TestListSessionsRevalidatesAuthorityBeforeReadingOwnedData(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*TenantSwitchState, *AccessTokenClaims)
		want   error
	}{
		{"expired access", func(_ *TenantSwitchState, c *AccessTokenClaims) { c.ExpiresAt = now }, ErrInvalidCredential},
		{"revoked session", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.Session.Status = SessionStatusRevoked }, ErrInvalidCredential},
		{"disabled human", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.Principal.Status = PrincipalStatusDisabled }, ErrPrincipalInactive},
		{"suspended membership", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.MembershipStatus = MembershipStatusSuspended }, ErrMembershipInactive},
		{"stale grant", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.SourceGrant.Version++ }, ErrInvalidCredential},
		{"other principal session", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.Session.PrincipalID = uuid.New() }, ErrInvalidCredential},
		{"stale lifecycle", func(s *TenantSwitchState, _ *AccessTokenClaims) { s.LifecycleFresh = false }, ErrTenantLifecycleStale},
		{"workload authentication", func(_ *TenantSwitchState, c *AccessTokenClaims) {
			c.AuthnMethods = []AuditAuthenticationMethod{AuditAuthenticationMethodWorkloadToken}
		}, ErrInvalidCredential},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := activeRefreshSessionState(now)
			state := TenantSwitchState{SourceTenantID: source.TenantID, TargetTenantID: source.TenantID, Principal: source.Principal, MembershipID: source.Grant.MembershipID, MembershipStatus: MembershipStatusActive, TenantAccess: TenantAccessStatusActive, Lifecycle: TenantLifecycleStatusActive, LifecycleFresh: true, Session: source.Session, SourceGrant: source.Grant}
			claims := AccessTokenClaims{Issuer: "ani-iam", Subject: source.Principal.ID, Audience: AudienceConsole, SessionID: source.Session.ID, GrantID: source.Grant.ID, GrantVersion: source.Grant.Version, TenantID: source.TenantID, ExpiresAt: now.Add(time.Minute), AuthnMethods: source.Session.AuthnMethods}
			tc.mutate(&state, &claims)
			reader := &guardedSessionListReader{sessionTestReader: &sessionTestReader{tenantSwitch: state}}
			u := &AuthenticationUsecase{reader: reader, tokens: &recordingSessionTokenCodec{verified: claims}, clock: fixedAuthClock{now: now}}
			_, err := u.ListSessions(context.Background(), ListSessionsCommand{Credential: "signed-human-access"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if reader.reads != 0 {
				t.Fatal("session data read before authority validation")
			}
		})
	}
}

func TestListSessionsDoesNotMisclassifyVerifierOutageAsBadCredential(t *testing.T) {
	for _, failure := range []error{ErrAuthenticationDependency, ErrAuthorizationDependency} {
		u := &AuthenticationUsecase{tokens: &recordingSessionTokenCodec{verifyErr: failure}}
		_, err := u.ListSessions(context.Background(), ListSessionsCommand{Credential: "human-access"})
		if !errors.Is(err, failure) || errors.Is(err, ErrInvalidCredential) {
			t.Fatal("verifier outage misclassified")
		}
	}
}
