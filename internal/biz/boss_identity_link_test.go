package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

type bossLinkTestTransaction struct {
	state     PlatformAuthorizationState
	owner     uuid.UUID
	exists    bool
	auditErr  error
	mutations []OIDCIdentityLinkMutation
	audits    []SecurityAuditEvent
}

func (t *bossLinkTestTransaction) WithinPlatformIdentityLink(ctx context.Context, fn func(PlatformIdentityLinkTransaction) error) error {
	return fn(t)
}
func (t *bossLinkTestTransaction) Authentication(context.Context, AccessTokenClaims) (PlatformAuthorizationState, error) {
	return t.state, nil
}
func (t *bossLinkTestTransaction) VerifiedEmailOwner(context.Context, string) (uuid.UUID, error) {
	return t.owner, nil
}
func (t *bossLinkTestTransaction) IdentityExists(context.Context, string, string) (bool, error) {
	return t.exists, nil
}
func (t *bossLinkTestTransaction) CreateIdentity(_ context.Context, m OIDCIdentityLinkMutation) error {
	t.mutations = append(t.mutations, m)
	return nil
}
func (t *bossLinkTestTransaction) AppendAudit(_ context.Context, a SecurityAuditEvent) error {
	t.audits = append(t.audits, a)
	return t.auditErr
}

func bossLinkTestSetup(t *testing.T) (*BossIdentityLinkUsecase, *recordingOIDCOperationStore, *platformAuthTestReader, *bossLinkTestTransaction, AccessTokenClaims) {
	t.Helper()
	_, r, c, _ := platformAuthTestSetup(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	r.state.ReauthenticatedAt = now
	tx := &bossLinkTestTransaction{state: r.state, owner: c.Subject}
	store := &recordingOIDCOperationStore{}
	p := validatingOIDCProvider{name: "dex", verified: OIDCVerifiedIdentity{Issuer: "https://idp.example.test", Subject: "unit-subject", Email: "unit@example.test", EmailVerified: true}}
	u, err := NewBossIdentityLinkUsecase(BossIdentityLinkConfig{Provider: "dex", Issuer: p.verified.Issuer, RedirectURI: "https://boss.example.test/link", RecentReauthentication: 15 * time.Minute}, p, store, r, tx, platformAuthTestVerifier{c}, &sequenceOIDCSecretGenerator{values: []string{strings.Repeat("s", 32), strings.Repeat("n", 32), strings.Repeat("v", 32), strings.Repeat("p", 32)}}, platformTestIDs{}, fixedOIDCClock{now})
	if err != nil {
		t.Fatal(err)
	}
	return u, store, r, tx, c
}
func TestBossIdentityLinkRechecksDatabaseReauthentication(t *testing.T) {
	for name, change := range map[string]func(*AccessTokenClaims, *PlatformAuthorizationState){
		"console":           func(c *AccessTokenClaims, s *PlatformAuthorizationState) { c.Audience = AudienceConsole },
		"implicit boundary": func(c *AccessTokenClaims, s *PlatformAuthorizationState) { c.Boundary = "" },
		"mixed tenant":      func(c *AccessTokenClaims, s *PlatformAuthorizationState) { c.TenantID = uuid.Must(uuid.NewV7()) },
		"expired access":    func(c *AccessTokenClaims, s *PlatformAuthorizationState) { c.ExpiresAt = time.Time{} },
		"stale database reauthentication": func(c *AccessTokenClaims, s *PlatformAuthorizationState) {
			s.ReauthenticatedAt = s.ReauthenticatedAt.Add(-16 * time.Minute)
		},
		"revoked grant": func(c *AccessTokenClaims, s *PlatformAuthorizationState) { s.GrantVersion++ },
		"suspended member": func(c *AccessTokenClaims, s *PlatformAuthorizationState) {
			s.MembershipStatus = MembershipStatusSuspended
		},
	} {
		t.Run(name, func(t *testing.T) {
			u, store, r, _, c := bossLinkTestSetup(t)
			change(&c, &r.state)
			u.tokens = platformAuthTestVerifier{c}
			_, err := u.BeginIdentityLink(context.Background(), BeginOIDCIdentityLinkCommand{Provider: "dex", RedirectURI: u.config.RedirectURI, RawCredential: "unit", IdempotencyKey: "unit"})
			if err == nil || store.created.State != "" {
				t.Fatal("invalid current authentication persisted an operation")
			}
		})
	}
}
func TestBossIdentityLinkBrowserProofAndTransactionTimeGuards(t *testing.T) {
	for _, name := range []string{"success", "proof", "stored boundary", "current member", "transaction grant", "transaction reauthentication", "email owner", "identity conflict", "audit failure"} {
		t.Run(name, func(t *testing.T) {
			u, store, r, tx, c := bossLinkTestSetup(t)
			ctx := context.Background()
			begin, err := u.BeginIdentityLink(ctx, BeginOIDCIdentityLinkCommand{Provider: "dex", RedirectURI: u.config.RedirectURI, RawCredential: "unit", IdempotencyKey: "unit", BrowserCallback: true})
			if err != nil || len(begin.BrowserProof) < 32 {
				t.Fatal("begin failed")
			}
			store.consumed = store.created
			command := CompleteOIDCIdentityLinkCommand{Code: "unit-code", State: begin.State, BrowserProof: begin.BrowserProof, RedirectURI: u.config.RedirectURI}
			switch name {
			case "proof":
				command.BrowserProof = strings.Repeat("x", 32)
			case "stored boundary":
				store.consumed.Audience = AudienceConsole
			case "current member":
				r.state.MembershipStatus = MembershipStatusSuspended
			case "transaction grant":
				tx.state.GrantVersion++
			case "transaction reauthentication":
				tx.state.ReauthenticatedAt = time.Time{}
			case "email owner":
				tx.owner = uuid.Must(uuid.NewV7())
			case "identity conflict":
				tx.exists = true
			case "audit failure":
				tx.auditErr = ErrOIDCDependency
			}
			result, err := u.CompleteIdentityLink(ctx, command)
			if name == "success" {
				if err != nil || result.PrincipalID != c.Subject || len(tx.mutations) != 1 || len(tx.audits) != 1 || tx.audits[0].Boundary != AuditBoundaryPlatform || tx.audits[0].ActorID != c.Subject {
					t.Fatal("explicit existing Human link failed")
				}
				return
			}
			if err == nil || result.IdentityID != uuid.Nil {
				t.Fatal("invalid link succeeded")
			}
			if name != "audit failure" && len(tx.mutations) != 0 {
				t.Fatal("guard ran after mutation")
			}
			if name == "audit failure" && !errors.Is(err, ErrOIDCDependency) {
				t.Fatal("Audit dependency failure was hidden")
			}
		})
	}
}
