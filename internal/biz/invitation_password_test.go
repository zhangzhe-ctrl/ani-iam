package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type invitationFailureTestRepo struct {
	InvitationPasswordRepository
	increment bool
	calls     int
}

func (r *invitationFailureTestRepo) RecordInvitationPasswordFailure(_ context.Context, _ InvitationPasswordState, _ SecurityAuditEvent, increment bool) error {
	r.calls++
	r.increment = increment
	return nil
}
func TestInvitationPasswordEveryCredentialDenialContributesToPublicThrottle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal uuid.UUID
		increment bool
	}{
		{"unknown", uuid.Nil, false}, {"disabled", uuid.Must(uuid.NewV7()), false}, {"wrong-password", uuid.Must(uuid.NewV7()), true}, {"rotated-after-verification", uuid.Must(uuid.NewV7()), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &invitationFailureTestRepo{}
			throttle := &recordingLoginThrottle{}
			u := &InvitationPasswordUsecase{repo: repo, throttle: throttle, acceptance: &InvitationAcceptanceUsecase{ids: platformTestIDs{}, clock: fixedAuthClock{now: time.Now().UTC()}}}
			attempt := LoginThrottleAttempt{NormalizedAccount: "invited@example.test", SourceIP: netip.MustParseAddr("192.0.2.22")}
			err := u.denied(context.Background(), InvitationPasswordState{PrincipalID: tc.principal}, DirectCaller{}, "denied", attempt, tc.increment)
			if !errors.Is(err, ErrInvalidCredential) || repo.calls != 1 || repo.increment != tc.increment || throttle.failureCalls != 1 || throttle.failed != attempt {
				t.Fatal("credential denial bypassed or duplicated abuse control")
			}
		})
	}
}

func TestInvitationPasswordProofRechecksCurrentIdentity(t *testing.T) {
	now := time.Now().UTC()
	original := InvitationPasswordState{PrincipalID: uuid.Must(uuid.NewV7()), IdentityID: uuid.Must(uuid.NewV7()), PrincipalStatus: PrincipalStatusActive, IdentityActive: true, NormalizedEmail: "verified@example.test", PasswordHash: "opaque-verified-hash", CredentialVersion: 2}
	counterReset := original
	counterReset.CredentialVersion++
	if validateInvitationPassword(original, counterReset, now) != nil {
		t.Fatal("counter-only update prevented independently authenticated retry")
	}
	for _, change := range []func(*InvitationPasswordState){func(s *InvitationPasswordState) { s.IdentityActive = false }, func(s *InvitationPasswordState) { s.PasswordHash = "rotated" }, func(s *InvitationPasswordState) { s.IdentityID = uuid.Must(uuid.NewV7()) }, func(s *InvitationPasswordState) { s.NormalizedEmail = "changed@example.test" }, func(s *InvitationPasswordState) { s.CredentialVersion = 1 }, func(s *InvitationPasswordState) { s.LockedUntil = now.Add(time.Minute) }, func(s *InvitationPasswordState) { s.PrincipalStatus = "disabled" }} {
		current := original
		change(&current)
		if !errors.Is(validateInvitationPassword(original, current, now), ErrInvalidCredential) {
			t.Fatal("stale independent password authority accepted")
		}
	}
}
func TestInvitationPasswordTargetRequiresCanonicalBoundary(t *testing.T) {
	tenant, id := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	secret := strings.Repeat("a", 43)
	good := []string{"ani_inv_t." + tenant.String() + "." + id.String() + "." + secret, "ani_inv_p." + id.String() + "." + secret}
	for _, v := range good {
		if _, err := invitationTargetFromToken(v); err != nil {
			t.Fatal("canonical target rejected")
		}
	}
	for _, v := range []string{strings.ToUpper(good[0]), "ani_inv_t." + uuid.Nil.String() + "." + id.String() + "." + secret, good[1] + "=", good[0] + ".extra", "ani_inv_p." + uuid.NewString() + "." + secret} {
		if _, err := invitationTargetFromToken(v); !errors.Is(err, ErrInvitationDenied) {
			t.Fatal("ambiguous or non-v7 invitation target accepted")
		}
	}
}

func TestInvitationAcceptanceCapabilityPreservesSuccessfulSessionProof(t *testing.T) {
	now := time.Now().UTC()
	claims := AccessTokenClaims{Boundary: AccessBoundaryTenant, ExpiresAt: now.Add(time.Minute), GrantVersion: 1}
	actor := InvitationAcceptanceActor{NormalizedEmail: "verified@example.test", PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive, SessionStatus: SessionStatusActive, GrantStatus: GrantStatusActive, GrantVersion: 1, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), SourceAccess: TenantAccessStatusActive, SourceLifecycle: TenantLifecycleStatusActive, SourceLifecycleFresh: true}
	if (InvitationAcceptanceCapability{claims: claims}).validateActor(actor, now) != nil {
		t.Fatal("successful Session proof became a non-nil wrapped error")
	}
}
