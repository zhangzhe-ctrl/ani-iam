package biz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PlatformLoginAuditWriter interface {
	RecordPlatformLoginFailure(context.Context, SecurityAuditEvent) error
}
type VerifiedPlatformOIDCLogin interface {
	CompleteVerifiedOIDC(context.Context, OIDCVerifiedIdentity, string, string) (LoginResult, error)
}

type BossOIDCConfig struct{ Provider, LoginRedirectURI string }
type BossOIDCUsecase struct {
	config       BossOIDCConfig
	provider     OIDCProvider
	store        OIDCOperationStore
	login        VerifiedPlatformOIDCLogin
	audit        PlatformLoginAuditWriter
	secrets      SecretGenerator
	ids          IDGenerator
	clock        Clock
	identityLink *BossIdentityLinkUsecase
}

func (u *BossOIDCUsecase) WithIdentityLink(link *BossIdentityLinkUsecase) *BossOIDCUsecase {
	u.identityLink = link
	return u
}

func NewBossOIDCUsecase(config BossOIDCConfig, provider OIDCProvider, store OIDCOperationStore, login VerifiedPlatformOIDCLogin, audit PlatformLoginAuditWriter, secrets SecretGenerator, ids IDGenerator, clock Clock) (*BossOIDCUsecase, error) {
	if config.Provider == "" || config.LoginRedirectURI == "" || provider == nil || provider.Name() != config.Provider || store == nil || login == nil || audit == nil || secrets == nil || ids == nil || clock == nil {
		return nil, ErrOIDCConfigurationInvalid
	}
	return &BossOIDCUsecase{config: config, provider: provider, store: store, login: login, audit: audit, secrets: secrets, ids: ids, clock: clock}, nil
}

func (u *BossOIDCUsecase) BeginLogin(ctx context.Context, c BeginOIDCLoginCommand) (BeginOIDCLoginResult, error) {
	if c.Audience != AudienceBoss || c.Boundary != AccessBoundaryPlatform || c.TenantID != uuid.Nil {
		return BeginOIDCLoginResult{}, ErrInvalidCredential
	}
	if c.RedirectURI != u.config.LoginRedirectURI {
		return BeginOIDCLoginResult{}, ErrOIDCRedirectInvalid
	}
	if c.IdempotencyKey == "" || len(c.IdempotencyKey) > 128 {
		return BeginOIDCLoginResult{}, ErrIdempotencyKeyRequired
	}
	var values [3]string
	seen := map[string]bool{}
	for i := range values {
		v, err := u.secrets.NewSecret()
		if err != nil || len(v) < 32 || seen[v] {
			return BeginOIDCLoginResult{}, ErrOIDCDependency
		}
		values[i] = v
		seen[v] = true
	}
	now := u.clock.Now().UTC()
	op := OIDCOperation{Boundary: AccessBoundaryPlatform, Kind: OIDCFlowLogin, Provider: u.config.Provider, Audience: AudienceBoss, State: values[0], Nonce: values[1], CodeVerifier: values[2], RedirectURI: c.RedirectURI, IdempotencyKey: c.IdempotencyKey, RequestFingerprint: oidcRequestFingerprint(OIDCFlowLogin, u.config.Provider, AudienceBoss, uuid.Nil, uuid.Nil, c.RedirectURI), CreatedAt: now, ExpiresAt: now.Add(oidcOperationLifetime)}
	stored, err := u.store.CreateOrGet(ctx, op)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return BeginOIDCLoginResult{}, err
		}
		return BeginOIDCLoginResult{}, ErrOIDCDependency
	}
	if !u.validOperation(stored, stored.State, c.RedirectURI, now) || stored.RequestFingerprint != op.RequestFingerprint {
		return BeginOIDCLoginResult{}, ErrOIDCStateInvalid
	}
	url, err := u.provider.AuthorizationURL(OIDCAuthorizationRequest{State: stored.State, Nonce: stored.Nonce, CodeVerifier: stored.CodeVerifier, RedirectURI: stored.RedirectURI})
	if err != nil || url == "" {
		return BeginOIDCLoginResult{}, ErrOIDCDependency
	}
	return BeginOIDCLoginResult{AuthorizationURL: url, State: stored.State, ExpiresAt: stored.ExpiresAt}, nil
}

func (u *BossOIDCUsecase) CompleteLogin(ctx context.Context, c CompleteOIDCLoginCommand) (LoginResult, error) {
	if strings.TrimSpace(c.Code) == "" || len(c.Code) > 4096 {
		return LoginResult{}, ErrInvalidCredential
	}
	if len(c.State) < 32 || len(c.State) > 512 {
		return LoginResult{}, ErrOIDCStateInvalid
	}
	if c.RedirectURI != u.config.LoginRedirectURI {
		return LoginResult{}, ErrOIDCRedirectInvalid
	}
	op, err := u.store.Consume(ctx, c.State)
	if err != nil {
		if errors.Is(err, ErrOIDCStateInvalid) {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrOIDCDependency
	}
	now := u.clock.Now().UTC()
	if !u.validOperation(op, c.State, c.RedirectURI, now) {
		return LoginResult{}, ErrOIDCStateInvalid
	}
	identity, err := u.provider.ExchangeAndVerify(ctx, OIDCExchangeRequest{Code: c.Code, Nonce: op.Nonce, CodeVerifier: op.CodeVerifier, RedirectURI: op.RedirectURI})
	if err != nil {
		return u.failed(ctx, op, now, classifyOIDCProviderExchangeError(err))
	}
	if identity.Issuer == "" || identity.Subject == "" || !identity.EmailVerified {
		return u.failed(ctx, op, now, ErrOIDCEmailUnverified)
	}
	result, err := u.login.CompleteVerifiedOIDC(ctx, identity, c.DeviceName, op.IdempotencyKey)
	if err != nil {
		return u.failed(ctx, op, now, err)
	}
	return result, nil
}

func (u *BossOIDCUsecase) validOperation(op OIDCOperation, state, redirect string, now time.Time) bool {
	return op.Kind == OIDCFlowLogin && op.Provider == u.config.Provider && op.Audience == AudienceBoss && op.Boundary == AccessBoundaryPlatform && op.TenantID == uuid.Nil && op.State == state && op.RedirectURI == redirect && op.RedirectURI == u.config.LoginRedirectURI && len(op.Nonce) >= 32 && len(op.CodeVerifier) >= 32 && op.IdempotencyKey != "" && !op.CreatedAt.After(now) && !op.ExpiresAt.IsZero() && now.Before(op.ExpiresAt) && op.ExpiresAt.Sub(op.CreatedAt) <= oidcOperationLifetime
}

func (u *BossOIDCUsecase) failed(ctx context.Context, op OIDCOperation, now time.Time, cause error) (LoginResult, error) {
	id, err := u.ids.NewID()
	if err != nil {
		return LoginResult{}, ErrOIDCDependency
	}
	e := SecurityAuditEvent{ID: id, AuthenticationMethod: AuditAuthenticationMethodAnonymous, Boundary: AuditBoundaryPlatform, Action: AuditActionOIDCLoginFailed, TargetType: AuditTargetTypeOIDCOperation, TargetID: id, TargetVersion: 1, Result: AuditResultFailed, Reason: AuditReasonOIDCLoginFailed, RequestID: op.IdempotencyKey, CorrelationID: op.IdempotencyKey, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
	if err = u.audit.RecordPlatformLoginFailure(ctx, e); err != nil {
		return LoginResult{}, ErrOIDCDependency
	}
	return LoginResult{}, cause
}

// The fixed audience and redirect select the flow implementation. Each flow
// independently authenticates its persisted boundary and provider response.
type HumanOIDCUsecase struct {
	console *OIDCUsecase
	boss    *BossOIDCUsecase
}

func NewHumanOIDCUsecase(console *OIDCUsecase, boss *BossOIDCUsecase) *HumanOIDCUsecase {
	return &HumanOIDCUsecase{console: console, boss: boss}
}
func (u *HumanOIDCUsecase) BeginLogin(ctx context.Context, c BeginOIDCLoginCommand) (BeginOIDCLoginResult, error) {
	if c.Audience == AudienceBoss {
		if u.boss == nil {
			return BeginOIDCLoginResult{}, ErrOIDCDependency
		}
		return u.boss.BeginLogin(ctx, c)
	}
	return u.console.BeginLogin(ctx, c)
}
func (u *HumanOIDCUsecase) CompleteLogin(ctx context.Context, c CompleteOIDCLoginCommand) (LoginResult, error) {
	if u.boss != nil && c.RedirectURI == u.boss.config.LoginRedirectURI {
		return u.boss.CompleteLogin(ctx, c)
	}
	return u.console.CompleteLogin(ctx, c)
}
func (u *HumanOIDCUsecase) BeginIdentityLink(ctx context.Context, c BeginOIDCIdentityLinkCommand) (BeginOIDCIdentityLinkResult, error) {
	if u.boss != nil && u.boss.identityLink != nil && c.RedirectURI == u.boss.identityLink.config.RedirectURI {
		return u.boss.identityLink.BeginIdentityLink(ctx, c)
	}
	return u.console.BeginIdentityLink(ctx, c)
}
func (u *HumanOIDCUsecase) CompleteIdentityLink(ctx context.Context, c CompleteOIDCIdentityLinkCommand) (OIDCIdentityLinkResult, error) {
	if u.boss != nil && u.boss.identityLink != nil && c.RedirectURI == u.boss.identityLink.config.RedirectURI {
		return u.boss.identityLink.CompleteIdentityLink(ctx, c)
	}
	return u.console.CompleteIdentityLink(ctx, c)
}
