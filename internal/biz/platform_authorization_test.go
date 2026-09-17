package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type platformAuthTestRegistry struct{ policy AuthorizationPolicy }

func (r platformAuthTestRegistry) Revision() string { return "unit-revision" }
func (r platformAuthTestRegistry) Lookup(id string) (AuthorizationPolicy, bool) {
	return r.policy, id == r.policy.OperationID
}

type platformAuthTestVerifier struct{ claims AccessTokenClaims }

func (v platformAuthTestVerifier) Verify(context.Context, string) (AccessTokenClaims, error) {
	return v.claims, nil
}

type platformAuthTestReader struct {
	state  PlatformAuthorizationState
	calls  int
	audits []SecurityAuditEvent
}

func (r *platformAuthTestReader) LookupPlatformAuthorization(context.Context, AccessTokenClaims, string, []string) (PlatformAuthorizationState, error) {
	r.calls++
	return r.state, nil
}
func (r *platformAuthTestReader) RecordPlatformAuthorization(_ context.Context, a SecurityAuditEvent) error {
	r.audits = append(r.audits, a)
	return nil
}

func platformAuthTestSetup(t *testing.T) (*PlatformAuthorizationUsecase, *platformAuthTestReader, AccessTokenClaims, CheckPermissionCommand) {
	t.Helper()
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	c := AccessTokenClaims{Boundary: AccessBoundaryPlatform, Audience: AudienceBoss, Subject: uuid.Must(uuid.NewV7()), SessionID: uuid.Must(uuid.NewV7()), GrantID: uuid.Must(uuid.NewV7()), GrantVersion: 1, ExpiresAt: now.Add(time.Minute), AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodOIDC}}
	r := &platformAuthTestReader{state: PlatformAuthorizationState{PrincipalStatus: PrincipalStatusActive, MembershipID: uuid.Must(uuid.NewV7()), MembershipStatus: MembershipStatusActive, SessionStatus: SessionStatusActive, IdleExpiresAt: now.Add(time.Minute), AbsoluteExpiresAt: now.Add(time.Hour), GrantStatus: GrantStatusActive, GrantVersion: 1, PermissionAllowed: true}}
	registry := platformAuthTestRegistry{AuthorizationPolicy{OperationID: "listPlatformIAMMembers", Scope: PermissionScopePlatform, Resource: "iam.platform-memberships", Actions: []string{"read"}, CredentialKinds: []CredentialKind{CredentialKindAccessToken}, PrincipalKinds: []PrincipalType{PrincipalTypeHuman}}}
	u, err := NewPlatformAuthorizationUsecase(registry, platformAuthTestVerifier{claims: c}, r, platformTestIDs{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	return u, r, c, CheckPermissionCommand{RawCredential: "unit-signed-token", OperationID: registry.policy.OperationID, PolicyRevision: registry.Revision()}
}

func TestPlatformAuthorizationRequiresExplicitSignedBoundary(t *testing.T) {
	for name, change := range map[string]func(*AccessTokenClaims){"console": func(c *AccessTokenClaims) { c.Audience = AudienceConsole }, "empty": func(c *AccessTokenClaims) { c.Boundary = "" }, "tenant": func(c *AccessTokenClaims) { c.Boundary = AccessBoundaryTenant }, "mixed": func(c *AccessTokenClaims) { c.TenantID = uuid.Must(uuid.NewV7()) }} {
		t.Run(name, func(t *testing.T) {
			u, r, c, command := platformAuthTestSetup(t)
			change(&c)
			u.verifier = platformAuthTestVerifier{claims: c}
			if _, err := u.CheckPermission(context.Background(), command); !errors.Is(err, ErrInvalidCredential) || r.calls != 0 {
				t.Fatal("untrusted Platform boundary reached storage")
			}
		})
	}
}
func TestPlatformAuthorizationHasNoPermissionCacheOrImplicitRole(t *testing.T) {
	u, r, _, c := platformAuthTestSetup(t)
	ctx := context.Background()
	decision, err := u.CheckPermission(ctx, c)
	if err != nil || !decision.Allowed || decision.Principal.Boundary != AccessBoundaryPlatform || decision.Principal.TenantID != uuid.Nil {
		t.Fatal("explicit Platform allow failed")
	}
	r.state.PermissionAllowed = false
	decision, err = u.CheckPermission(ctx, c)
	if err != nil || decision.Allowed || decision.Reason != AuthorizationReasonPermissionDenied || r.calls != 2 || len(r.audits) != 1 {
		t.Fatal("permission removal did not affect next decision")
	}
	r.state.PermissionAllowed = true
	r.state.GrantVersion++
	decision, err = u.CheckPermission(ctx, c)
	if err != nil || decision.Allowed || decision.Reason != AuthorizationReasonGrantVersionMismatch {
		t.Fatal("stale access Grant accepted")
	}
}
func TestPlatformAuthorizationChecksCurrentMembershipAndDeadlines(t *testing.T) {
	for name, change := range map[string]func(*PlatformAuthorizationState){"Human": func(s *PlatformAuthorizationState) { s.PrincipalStatus = PrincipalStatusDisabled }, "Membership": func(s *PlatformAuthorizationState) { s.MembershipStatus = MembershipStatusSuspended }, "Session": func(s *PlatformAuthorizationState) { s.SessionStatus = SessionStatusRevoked }, "Grant": func(s *PlatformAuthorizationState) { s.GrantStatus = GrantStatusRevoked }, "idle": func(s *PlatformAuthorizationState) { s.IdleExpiresAt = time.Time{} }, "absolute": func(s *PlatformAuthorizationState) { s.AbsoluteExpiresAt = time.Time{} }} {
		t.Run(name, func(t *testing.T) {
			u, r, _, c := platformAuthTestSetup(t)
			change(&r.state)
			d, err := u.CheckPermission(context.Background(), c)
			if err != nil || d.Allowed {
				t.Fatal("inactive Platform graph accepted")
			}
			_, err = u.ValidatePrincipal(context.Background(), ValidatePrincipalCommand{RawCredential: c.RawCredential, OperationID: c.OperationID, PolicyRevision: c.PolicyRevision})
			if err == nil {
				t.Fatal("inactive graph authenticated")
			}
		})
	}
}
