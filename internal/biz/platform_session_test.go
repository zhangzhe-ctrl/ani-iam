package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type platformSessionTestRepo struct {
	PlatformSessionTransaction
	state                      PlatformSessionState
	found                      bool
	rotated, reused, loggedOut bool
	audits                     []SecurityAuditEvent
}

func (r *platformSessionTestRepo) WithinPlatformSession(ctx context.Context, _ [sha256.Size]byte, fn func(PlatformSessionTransaction, PlatformSessionState, bool) error) error {
	return fn(r, r.state, r.found)
}
func (r *platformSessionTestRepo) Rotate(_ context.Context, s PlatformSessionState, _ RefreshToken, _, _ time.Time) (int64, error) {
	r.rotated = true
	return s.Session.Version + 1, nil
}
func (r *platformSessionTestRepo) RevokeFamily(context.Context, PlatformSessionState, time.Time) error {
	r.reused = true
	return nil
}
func (r *platformSessionTestRepo) Logout(context.Context, PlatformSessionState, time.Time) error {
	r.loggedOut = true
	return nil
}
func (r *platformSessionTestRepo) AppendAudit(_ context.Context, a SecurityAuditEvent) error {
	r.audits = append(r.audits, a)
	return nil
}

type platformSessionTestThrottle struct {
	LoginThrottle
	err error
}

func (t platformSessionTestThrottle) CheckRefresh(context.Context, RefreshThrottleAttempt) error {
	return t.err
}

func platformSessionFixture(now time.Time) PlatformSessionState {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	s := PlatformSessionState{Principal: Principal{ID: id(), Status: PrincipalStatusActive}, MembershipID: id(), MembershipStatus: MembershipStatusActive}
	s.Session = Session{ID: id(), PrincipalID: s.Principal.ID, Audience: AudienceBoss, Status: SessionStatusActive, Version: 2, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodOIDC}, IdleExpiresAt: now.Add(time.Minute), AbsoluteExpiry: now.Add(time.Hour), ReauthenticatedAt: now.Add(-time.Minute)}
	s.Grant = SessionGrant{ID: id(), SessionID: s.Session.ID, MembershipID: s.MembershipID, Status: GrantStatusActive, Version: 1}
	s.Family = RefreshTokenFamily{ID: id(), GrantID: s.Grant.ID, Status: GrantStatusActive, Version: 1}
	s.Token = RefreshToken{ID: id(), FamilyID: s.Family.ID, Digest: sha256.Sum256([]byte("unit-old-refresh")), Status: RefreshTokenStatusActive, ExpiresAt: s.Session.AbsoluteExpiry}
	return s
}
func TestPlatformRefreshValidatesCurrentGraph(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	for name, change := range map[string]func(*PlatformSessionState){
		"console":              func(s *PlatformSessionState) { s.Session.Audience = AudienceConsole },
		"wrong Human":          func(s *PlatformSessionState) { s.Session.PrincipalID = uuid.Must(uuid.NewV7()) },
		"wrong membership":     func(s *PlatformSessionState) { s.Grant.MembershipID = uuid.Must(uuid.NewV7()) },
		"disabled Human":       func(s *PlatformSessionState) { s.Principal.Status = PrincipalStatusDisabled },
		"suspended membership": func(s *PlatformSessionState) { s.MembershipStatus = MembershipStatusSuspended },
		"revoked grant":        func(s *PlatformSessionState) { s.Grant.Status = GrantStatusRevoked },
		"expired idle":         func(s *PlatformSessionState) { s.Session.IdleExpiresAt = now },
		"expired token":        func(s *PlatformSessionState) { s.Token.ExpiresAt = now },
	} {
		t.Run(name, func(t *testing.T) {
			s := platformSessionFixture(now)
			change(&s)
			r := &platformSessionTestRepo{state: s, found: true}
			u, err := NewPlatformSessionUsecase("https://boss.example.test", "ani-iam", r, platformSessionTestThrottle{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedOIDCClock{now: now})
			if err != nil {
				t.Fatal(err)
			}
			_, err = u.RefreshSession(context.Background(), RefreshSessionCommand{RefreshToken: "unit-old-refresh", CSRFToken: "csrf", Origin: u.origin, IdempotencyKey: "unit"})
			if err == nil || r.rotated || len(r.audits) > 0 {
				t.Fatal("invalid graph reached mutation")
			}
		})
	}
}
func TestPlatformRefreshPreservesBoundaryAndReauthentication(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	s := platformSessionFixture(now)
	r := &platformSessionTestRepo{state: s, found: true}
	signer := &recordingOIDCAccessTokenIssuer{token: "unit-token"}
	u, err := NewPlatformSessionUsecase("https://boss.example.test", "ani-iam", r, platformSessionTestThrottle{}, signer, platformTestSecrets{}, platformTestIDs{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	c := RefreshSessionCommand{RefreshToken: "unit-old-refresh", CSRFToken: "csrf", Origin: u.origin, IdempotencyKey: "unit"}
	result, err := u.RefreshSession(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !r.rotated || result.Boundary != AccessBoundaryPlatform || result.TenantID != uuid.Nil || !result.Session.ReauthenticatedAt.Equal(s.Session.ReauthenticatedAt) || result.Session.Version != 3 || len(r.audits) != 1 || r.audits[0].Boundary != AuditBoundaryPlatform {
		t.Fatal("rotation lost Platform invariants")
	}
	r.state.Token.Status = RefreshTokenStatusConsumed
	r.audits = nil
	if _, err = u.RefreshSession(context.Background(), c); !errors.Is(err, ErrInvalidCredential) || !r.reused || len(r.audits) != 1 || r.audits[0].Action != AuditActionRefreshTokenReused {
		t.Fatal("reuse was not revoked and audited")
	}
}
