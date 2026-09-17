package biz

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestInvitationAcceptanceTokenBindsExactTarget(t *testing.T) {
	tenant, inv := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	target := InvitationAcceptanceTarget{Boundary: AccessBoundaryTenant, TenantID: tenant, InvitationID: inv}
	raw := "ani_inv_t." + tenant.String() + "." + inv.String() + "." + strings.Repeat("x", 43)
	if _, err := invitationAcceptanceDigest(target, raw); err != nil {
		t.Fatal("canonical tenant invitation rejected")
	}
	platform := InvitationAcceptanceTarget{Boundary: AccessBoundaryPlatform, InvitationID: inv}
	pRaw := "ani_inv_p." + inv.String() + "." + strings.Repeat("y", 43)
	if _, err := invitationAcceptanceDigest(platform, pRaw); err != nil {
		t.Fatal("canonical Platform invitation rejected")
	}
	for _, test := range []struct {
		Target InvitationAcceptanceTarget
		Raw    string
	}{{target, pRaw}, {platform, raw}, {InvitationAcceptanceTarget{Boundary: AccessBoundaryTenant, TenantID: uuid.Must(uuid.NewV7()), InvitationID: inv}, raw}, {target, raw + "="}, {target, strings.TrimSuffix(raw, strings.Repeat("x", 43)) + strings.Repeat("x", 31)}, {target, raw + "\n"}} {
		if _, err := invitationAcceptanceDigest(test.Target, test.Raw); !errors.Is(err, ErrInvitationDenied) {
			t.Fatal("invitation token escaped exact boundary or syntax")
		}
	}
}
func TestInvitationRecipientAuthenticationIndependentOfTarget(t *testing.T) {
	now := time.Now().UTC()
	claims := AccessTokenClaims{Boundary: AccessBoundaryPlatform, ExpiresAt: now.Add(time.Minute), GrantVersion: 1}
	actor := InvitationAcceptanceActor{NormalizedEmail: "verified@example.test", PrincipalStatus: PrincipalStatusActive, MembershipStatus: MembershipStatusActive, SessionStatus: SessionStatusActive, GrantStatus: GrantStatusActive, GrantVersion: 1, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)}
	if validateInvitationAcceptanceActor(claims, actor, now) != nil {
		t.Fatal("current Platform recipient rejected without Tenant")
	}
	actor.MembershipStatus = MembershipStatusSuspended
	if !errors.Is(validateInvitationAcceptanceActor(claims, actor, now), ErrMembershipInactive) {
		t.Fatal("recipient source authority ignored")
	}
	actor.MembershipStatus = MembershipStatusActive
	claims.Boundary = AccessBoundaryTenant
	actor.SourceAccess = TenantAccessStatusActive
	actor.SourceLifecycle = TenantLifecycleStatusActive
	if !errors.Is(validateInvitationAcceptanceActor(claims, actor, now), ErrTenantLifecycleStale) {
		t.Fatal("recipient source Lifecycle stale ignored")
	}
	actor.SourceLifecycleFresh = true
	if validateInvitationAcceptanceActor(claims, actor, now) != nil {
		t.Fatal("current Tenant recipient rejected")
	}
	actor.GrantVersion = 2
	if !errors.Is(validateInvitationAcceptanceActor(claims, actor, now), ErrInvalidCredential) {
		t.Fatal("recipient stale Grant accepted")
	}
}
