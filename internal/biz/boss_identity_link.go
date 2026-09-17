package biz

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type BossIdentityLinkConfig struct {
	Provider, Issuer, RedirectURI string
	RecentReauthentication        time.Duration
}
type PlatformIdentityLinkTransaction interface {
	Authentication(context.Context, AccessTokenClaims) (PlatformAuthorizationState, error)
	VerifiedEmailOwner(context.Context, string) (uuid.UUID, error)
	IdentityExists(context.Context, string, string) (bool, error)
	CreateIdentity(context.Context, OIDCIdentityLinkMutation) error
	AppendAudit(context.Context, SecurityAuditEvent) error
}
type PlatformIdentityLinkUnitOfWork interface {
	WithinPlatformIdentityLink(context.Context, func(PlatformIdentityLinkTransaction) error) error
}
type BossIdentityLinkUsecase struct {
	config   BossIdentityLinkConfig
	provider OIDCProvider
	store    OIDCOperationStore
	reader   PlatformAuthorizationReader
	uow      PlatformIdentityLinkUnitOfWork
	tokens   AccessCredentialVerifier
	secrets  SecretGenerator
	ids      IDGenerator
	clock    Clock
}

func NewBossIdentityLinkUsecase(c BossIdentityLinkConfig, p OIDCProvider, s OIDCOperationStore, r PlatformAuthorizationReader, u PlatformIdentityLinkUnitOfWork, t AccessCredentialVerifier, secrets SecretGenerator, ids IDGenerator, clock Clock) (*BossIdentityLinkUsecase, error) {
	if c.Provider == "" || c.Issuer == "" || c.RedirectURI == "" || c.RecentReauthentication <= 0 || c.RecentReauthentication > 15*time.Minute || p == nil || p.Name() != c.Provider || s == nil || r == nil || u == nil || t == nil || secrets == nil || ids == nil || clock == nil {
		return nil, ErrOIDCConfigurationInvalid
	}
	return &BossIdentityLinkUsecase{c, p, s, r, u, t, secrets, ids, clock}, nil
}
func (u *BossIdentityLinkUsecase) validate(ctx context.Context, c AccessTokenClaims) error {
	if err := u.claims(c); err != nil {
		return err
	}
	s, err := u.reader.LookupPlatformAuthorization(ctx, c, "", nil)
	if err != nil {
		return err
	}
	return u.current(c, s)
}
func (u *BossIdentityLinkUsecase) claims(c AccessTokenClaims) error {
	if c.Boundary != AccessBoundaryPlatform || c.Audience != AudienceBoss || c.TenantID != uuid.Nil || c.Subject == uuid.Nil || c.SessionID == uuid.Nil || c.GrantID == uuid.Nil || c.GrantVersion < 1 || !u.clock.Now().UTC().Before(c.ExpiresAt) || !containsHumanAuthenticationMethod(c.AuthnMethods) {
		return ErrAuthorizationCredentialInvalid
	}
	return nil
}
func (u *BossIdentityLinkUsecase) current(c AccessTokenClaims, s PlatformAuthorizationState) error {
	now := u.clock.Now().UTC()
	if platformAuthorizationDenial(s, c, now) != "" || s.ReauthenticatedAt.IsZero() || s.ReauthenticatedAt.After(now) || now.Sub(s.ReauthenticatedAt) > u.config.RecentReauthentication {
		return ErrOIDCReauthenticationRequired
	}
	return nil
}
func (u *BossIdentityLinkUsecase) credential(ctx context.Context, raw string) (AccessTokenClaims, error) {
	if strings.TrimSpace(raw) == "" {
		return AccessTokenClaims{}, ErrAuthorizationCredentialRequired
	}
	c, err := u.tokens.Verify(ctx, raw)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return AccessTokenClaims{}, err
		}
		return AccessTokenClaims{}, ErrAuthorizationCredentialInvalid
	}
	return c, u.validate(ctx, c)
}
func (u *BossIdentityLinkUsecase) BeginIdentityLink(ctx context.Context, c BeginOIDCIdentityLinkCommand) (BeginOIDCIdentityLinkResult, error) {
	if c.Provider != u.config.Provider {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCConfigurationInvalid
	}
	if c.RedirectURI != u.config.RedirectURI {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCRedirectInvalid
	}
	key := strings.TrimSpace(c.IdempotencyKey)
	if key == "" || len(key) > 128 {
		return BeginOIDCIdentityLinkResult{}, ErrIdempotencyKeyRequired
	}
	claims, err := u.credential(ctx, c.RawCredential)
	if err != nil {
		return BeginOIDCIdentityLinkResult{}, err
	}
	var secrets [4]string
	seen := map[string]bool{}
	for i := range secrets {
		secrets[i], err = u.secrets.NewSecret()
		if err != nil || len(secrets[i]) < 32 || seen[secrets[i]] {
			return BeginOIDCIdentityLinkResult{}, ErrOIDCDependency
		}
		seen[secrets[i]] = true
	}
	now := u.clock.Now().UTC()
	op := OIDCOperation{Boundary: AccessBoundaryPlatform, Kind: OIDCFlowIdentityLink, Provider: u.config.Provider, Audience: AudienceBoss, PrincipalID: claims.Subject, SessionID: claims.SessionID, State: secrets[0], Nonce: secrets[1], CodeVerifier: secrets[2], RedirectURI: u.config.RedirectURI, IdempotencyKey: key, CreatedAt: now, ExpiresAt: now.Add(oidcOperationLifetime), LinkGrantID: claims.GrantID, LinkGrantVersion: claims.GrantVersion, LinkCredentialExpiresAt: claims.ExpiresAt, LinkAuthnMethods: append([]AuditAuthenticationMethod(nil), claims.AuthnMethods...)}
	base := oidcRequestFingerprint(OIDCFlowIdentityLink, u.config.Provider, AudienceBoss, uuid.Nil, claims.Subject, c.RedirectURI)
	fp := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%t", base, claims.SessionID, claims.GrantID, claims.GrantVersion, c.BrowserCallback)))
	op.RequestFingerprint = hex.EncodeToString(fp[:])
	if c.BrowserCallback {
		d := sha256.Sum256([]byte(secrets[3]))
		op.BrowserProofDigest = hex.EncodeToString(d[:])
	}
	saved, err := u.store.CreateOrGet(ctx, op)
	if err != nil {
		return BeginOIDCIdentityLinkResult{}, err
	}
	if !u.validOperation(saved, saved.State) || saved.RequestFingerprint != op.RequestFingerprint {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	location, err := u.provider.AuthorizationURL(OIDCAuthorizationRequest{State: saved.State, Nonce: saved.Nonce, CodeVerifier: saved.CodeVerifier, RedirectURI: saved.RedirectURI})
	if err != nil || location == "" {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCDependency
	}
	result := BeginOIDCIdentityLinkResult{AuthorizationURL: location, State: saved.State, ExpiresAt: saved.ExpiresAt}
	if c.BrowserCallback && saved.State == op.State && saved.BrowserProofDigest == op.BrowserProofDigest {
		result.BrowserProof = secrets[3]
	}
	return result, nil
}
func (u *BossIdentityLinkUsecase) validOperation(o OIDCOperation, state string) bool {
	now := u.clock.Now().UTC()
	return o.Kind == OIDCFlowIdentityLink && o.Boundary == AccessBoundaryPlatform && o.Audience == AudienceBoss && o.TenantID == uuid.Nil && o.Provider == u.config.Provider && o.RedirectURI == u.config.RedirectURI && o.State == state && len(o.Nonce) >= 32 && len(o.CodeVerifier) >= 32 && o.IdempotencyKey != "" && !o.CreatedAt.After(now) && now.Before(o.ExpiresAt) && o.ExpiresAt.Sub(o.CreatedAt) <= oidcOperationLifetime && o.PrincipalID != uuid.Nil && o.SessionID != uuid.Nil && o.LinkGrantID != uuid.Nil && o.LinkGrantVersion > 0
}
func (u *BossIdentityLinkUsecase) CompleteIdentityLink(ctx context.Context, c CompleteOIDCIdentityLinkCommand) (OIDCIdentityLinkResult, error) {
	if c.RedirectURI != u.config.RedirectURI {
		return OIDCIdentityLinkResult{}, ErrOIDCRedirectInvalid
	}
	if c.Code == "" || len(c.Code) > 4096 || len(c.State) < 32 || len(c.State) > 512 {
		return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	if (c.RawCredential == "") == (c.BrowserProof == "") {
		return OIDCIdentityLinkResult{}, ErrAuthorizationCredentialInvalid
	}
	var claims AccessTokenClaims
	var err error
	if c.RawCredential != "" {
		claims, err = u.credential(ctx, c.RawCredential)
		if err != nil {
			return OIDCIdentityLinkResult{}, err
		}
	}
	op, err := u.store.Consume(ctx, c.State)
	if err != nil {
		return OIDCIdentityLinkResult{}, err
	}
	if !u.validOperation(op, c.State) {
		return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	if c.BrowserProof != "" {
		d := sha256.Sum256([]byte(c.BrowserProof))
		if len(c.BrowserProof) < 32 || len(c.BrowserProof) > 512 || len(op.BrowserProofDigest) != sha256.Size*2 || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(d[:])), []byte(op.BrowserProofDigest)) != 1 {
			return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
		}
		claims = AccessTokenClaims{Boundary: AccessBoundaryPlatform, Subject: op.PrincipalID, SessionID: op.SessionID, GrantID: op.LinkGrantID, GrantVersion: op.LinkGrantVersion, Audience: AudienceBoss, ExpiresAt: op.LinkCredentialExpiresAt, AuthnMethods: op.LinkAuthnMethods}
		if err = u.validate(ctx, claims); err != nil {
			return OIDCIdentityLinkResult{}, err
		}
	}
	if op.PrincipalID != claims.Subject || op.SessionID != claims.SessionID || op.LinkGrantID != claims.GrantID || op.LinkGrantVersion != claims.GrantVersion {
		return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	identity, err := u.provider.ExchangeAndVerify(ctx, OIDCExchangeRequest{Code: c.Code, Nonce: op.Nonce, CodeVerifier: op.CodeVerifier, RedirectURI: op.RedirectURI})
	if err != nil {
		return u.failed(ctx, claims, op, classifyOIDCProviderExchangeError(err))
	}
	if identity.Issuer != u.config.Issuer || identity.Subject == "" || !identity.EmailVerified {
		return u.failed(ctx, claims, op, ErrOIDCEmailUnverified)
	}
	email, err := normalizeOIDCEmail(identity.Email)
	if err != nil {
		return u.failed(ctx, claims, op, ErrOIDCEmailUnverified)
	}
	identityID, err := u.ids.NewID()
	if err != nil || identityID.Version() != 7 {
		return OIDCIdentityLinkResult{}, ErrOIDCDependency
	}
	auditID, err := u.ids.NewID()
	if err != nil || auditID.Version() != 7 || auditID == identityID {
		return OIDCIdentityLinkResult{}, ErrOIDCDependency
	}
	var result OIDCIdentityLinkResult
	err = u.uow.WithinPlatformIdentityLink(ctx, func(tx PlatformIdentityLinkTransaction) error {
		if err := u.claims(claims); err != nil {
			return err
		}
		state, err := tx.Authentication(ctx, claims)
		if err != nil {
			return err
		}
		if err = u.current(claims, state); err != nil {
			return err
		}
		owner, err := tx.VerifiedEmailOwner(ctx, email)
		if err != nil {
			return err
		}
		if owner != claims.Subject {
			return ErrOIDCEmailConflict
		}
		exists, err := tx.IdentityExists(ctx, identity.Issuer, identity.Subject)
		if err != nil {
			return err
		}
		if exists {
			return ErrOIDCIdentityConflict
		}
		now := u.clock.Now().UTC()
		audit := SecurityAuditEvent{ID: auditID, ActorID: claims.Subject, AuthenticationMethod: firstHumanAuthenticationMethod(claims.AuthnMethods), Boundary: AuditBoundaryPlatform, Action: AuditActionOIDCIdentityLinked, TargetType: AuditTargetTypeIdentity, TargetID: identityID, TargetVersion: 1, Result: AuditResultSucceeded, Reason: AuditReasonOIDCIdentityLink, RequestID: op.IdempotencyKey, CorrelationID: op.IdempotencyKey, DecisionID: auditID.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		if err = tx.CreateIdentity(ctx, OIDCIdentityLinkMutation{IdentityID: identityID, PrincipalID: claims.Subject, Provider: u.config.Provider, Issuer: identity.Issuer, Subject: identity.Subject, NormalizedEmail: email, LinkedAt: now}); err != nil {
			return err
		}
		if err = tx.AppendAudit(ctx, audit); err != nil {
			return err
		}
		result = OIDCIdentityLinkResult{IdentityID: identityID, PrincipalID: claims.Subject}
		return nil
	})
	if err != nil {
		return u.failed(ctx, claims, op, err)
	}
	return result, nil
}
func (u *BossIdentityLinkUsecase) failed(ctx context.Context, c AccessTokenClaims, o OIDCOperation, cause error) (OIDCIdentityLinkResult, error) {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return OIDCIdentityLinkResult{}, ErrOIDCDependency
	}
	now := u.clock.Now().UTC()
	a := SecurityAuditEvent{ID: id, ActorID: c.Subject, AuthenticationMethod: firstHumanAuthenticationMethod(c.AuthnMethods), Boundary: AuditBoundaryPlatform, Action: AuditActionOIDCIdentityLinkFailed, TargetType: AuditTargetTypeOIDCOperation, TargetID: id, TargetVersion: 1, Result: AuditResultFailed, Reason: AuditReasonOIDCIdentityLinkFailed, RequestID: o.IdempotencyKey, CorrelationID: o.IdempotencyKey, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
	if u.reader.RecordPlatformAuthorization(ctx, a) != nil {
		return OIDCIdentityLinkResult{}, ErrOIDCDependency
	}
	return OIDCIdentityLinkResult{}, cause
}
