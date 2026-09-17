package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type bossLoginTest struct {
	calls  int
	audits []SecurityAuditEvent
}

func (l *bossLoginTest) CompleteVerifiedOIDC(context.Context, OIDCVerifiedIdentity, string, string) (LoginResult, error) {
	l.calls++
	return LoginResult{Boundary: AccessBoundaryPlatform}, nil
}
func (l *bossLoginTest) RecordPlatformLoginFailure(_ context.Context, a SecurityAuditEvent) error {
	l.audits = append(l.audits, a)
	return nil
}
func TestBossOIDCRejectsForeignStoredBoundaryBeforeLogin(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	valid := OIDCOperation{Boundary: AccessBoundaryPlatform, Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceBoss, State: strings.Repeat("s", 32), Nonce: strings.Repeat("n", 32), CodeVerifier: strings.Repeat("v", 32), RedirectURI: "https://boss.example.test/auth/oidc/callback", IdempotencyKey: "unit", CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	for name, change := range map[string]func(*OIDCOperation){
		"tenant":          func(o *OIDCOperation) { o.TenantID = uuid.Must(uuid.NewV7()) },
		"absent boundary": func(o *OIDCOperation) { o.Boundary = "" },
		"console":         func(o *OIDCOperation) { o.Audience = AudienceConsole },
		"expired":         func(o *OIDCOperation) { o.ExpiresAt = now },
		"future":          func(o *OIDCOperation) { o.CreatedAt = now.Add(time.Minute) },
		"link":            func(o *OIDCOperation) { o.Kind = OIDCFlowIdentityLink },
	} {
		t.Run(name, func(t *testing.T) {
			op := valid
			change(&op)
			login := &bossLoginTest{}
			u, err := NewBossOIDCUsecase(BossOIDCConfig{Provider: "dex", LoginRedirectURI: valid.RedirectURI}, validatingOIDCProvider{name: "dex"}, &recordingOIDCOperationStore{consumed: op}, login, login, platformTestSecrets{}, platformTestIDs{}, fixedOIDCClock{now: now})
			if err != nil {
				t.Fatal(err)
			}
			_, err = u.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{State: valid.State, Code: "unit-code", RedirectURI: valid.RedirectURI})
			if !errors.Is(err, ErrOIDCStateInvalid) || login.calls != 0 {
				t.Fatal("foreign stored operation reached login")
			}
		})
	}
}

func TestBossOIDCRequiresIndependentVerifiedEmailProof(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	op := OIDCOperation{Boundary: AccessBoundaryPlatform, Kind: OIDCFlowLogin, Provider: "dex", Audience: AudienceBoss, State: strings.Repeat("s", 32), Nonce: strings.Repeat("n", 32), CodeVerifier: strings.Repeat("v", 32), RedirectURI: "https://boss.example.test/auth/oidc/callback", IdempotencyKey: "unit", CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	login := &bossLoginTest{}
	u, err := NewBossOIDCUsecase(BossOIDCConfig{Provider: "dex", LoginRedirectURI: op.RedirectURI}, validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{Issuer: "https://idp.example.test", Subject: "unit", Email: "unit@example.test"}}, &recordingOIDCOperationStore{consumed: op}, login, login, platformTestSecrets{}, platformTestIDs{}, fixedOIDCClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = u.CompleteLogin(context.Background(), CompleteOIDCLoginCommand{State: op.State, Code: "unit-code", RedirectURI: op.RedirectURI})
	if !errors.Is(err, ErrOIDCEmailUnverified) || login.calls != 0 || len(login.audits) != 1 || login.audits[0].ActorID != uuid.Nil || login.audits[0].Boundary != AuditBoundaryPlatform {
		t.Fatal("unverified email reached login or lost redacted audit")
	}
}
