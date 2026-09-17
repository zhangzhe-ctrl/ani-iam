package biz

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type rejectedPlatformLoginTx struct {
	PlatformLoginTransaction
	state PlatformFirstAdministratorState
}

func (t *rejectedPlatformLoginTx) LookupOIDC(context.Context, string, string, string) (PlatformLoginState, error) {
	return PlatformLoginState{}, ErrOIDCIdentityNotFound
}
func (t *rejectedPlatformLoginTx) FirstAdministrator(context.Context, string) (PlatformFirstAdministratorState, error) {
	return t.state, nil
}

type rejectedPlatformLoginUOW struct {
	tx    *rejectedPlatformLoginTx
	calls int
}

func (u *rejectedPlatformLoginUOW) WithinPlatformLogin(ctx context.Context, _ string, fn func(PlatformLoginTransaction) error) error {
	u.calls++
	return fn(u.tx)
}

type platformTestIDs struct{}

func (platformTestIDs) NewID() (uuid.UUID, error) { return uuid.NewV7() }

type platformTestSecrets struct{}

func (platformTestSecrets) NewSecret() (string, error) {
	return "unit-only-secret-with-at-least-thirty-two-bytes", nil
}

type platformTestSigner struct{}

func (platformTestSigner) Issue(context.Context, AccessTokenClaims) (string, error) {
	return "unit-only", nil
}

func TestPlatformBootstrapRequiresExactLiveOwnerIntent(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	identity := OIDCVerifiedIdentity{Issuer: "https://idp.example.test", Subject: "exact", Email: "admin@example.test", EmailVerified: true}
	valid := PlatformFirstAdministratorState{Manifest: FirstAdministratorManifest{Version: 1, IntentID: uuid.Must(uuid.NewV7()), Environment: "wr22", Email: identity.Email, Issuer: identity.Issuer, Subject: identity.Subject, ExpiresAt: now.Add(time.Hour)}}
	for name, mutate := range map[string]func(*PlatformFirstAdministratorState){
		"completed":              func(s *PlatformFirstAdministratorState) { s.Completed = true },
		"existing administrator": func(s *PlatformFirstAdministratorState) { s.AdministratorPresent = true },
		"wrong environment":      func(s *PlatformFirstAdministratorState) { s.Manifest.Environment = "foreign" },
		"expired":                func(s *PlatformFirstAdministratorState) { s.Manifest.ExpiresAt = now },
		"wrong issuer":           func(s *PlatformFirstAdministratorState) { s.Manifest.Issuer = "https://foreign.example.test" },
		"wrong subject":          func(s *PlatformFirstAdministratorState) { s.Manifest.Subject = "foreign" },
		"wrong email":            func(s *PlatformFirstAdministratorState) { s.Manifest.Email = "foreign@example.test" },
	} {
		t.Run(name, func(t *testing.T) {
			s := valid
			mutate(&s)
			repo := &rejectedPlatformLoginUOW{tx: &rejectedPlatformLoginTx{state: s}}
			u, err := NewPlatformLoginUsecase(PlatformLoginConfig{Environment: "wr22", Provider: "dex", OIDCIssuer: identity.Issuer, AccessIssuer: "ani-iam"}, repo, roleTestCatalog{}, platformTestSigner{}, platformTestSecrets{}, platformTestIDs{}, fixedOIDCClock{now: now})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = u.CompleteVerifiedOIDC(context.Background(), identity, "test", "unit-request"); err != ErrInvalidCredential || repo.calls != 1 {
				t.Fatal("non-matching owner intent did not fail before writes")
			}
		})
	}
}
