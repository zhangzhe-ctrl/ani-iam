package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/idna"
)

const oidcOperationLifetime = 10 * time.Minute

var (
	ErrOIDCConfigurationInvalid     = errors.New("OIDC configuration is invalid")
	ErrOIDCRedirectInvalid          = errors.New("OIDC redirect URI is invalid")
	ErrOIDCStateInvalid             = errors.New("OIDC state is invalid")
	ErrOIDCIdentityNotFound         = errors.New("OIDC identity is not found")
	ErrOIDCIdentityConflict         = errors.New("OIDC identity conflicts with another principal")
	ErrOIDCEmailConflict            = errors.New("OIDC verified email conflicts with another principal")
	ErrOIDCEmailUnverified          = errors.New("OIDC email is not verified")
	ErrOIDCReauthenticationRequired = errors.New("recent reauthentication is required")
	ErrOIDCDependency               = errors.New("OIDC dependency is unavailable")
)

type OIDCFlowKind string

const (
	OIDCFlowLogin        OIDCFlowKind = "login"
	OIDCFlowIdentityLink OIDCFlowKind = "identity_link"
)

type OIDCUsecaseConfig struct {
	Provider                string
	LoginRedirectURI        string
	IdentityLinkRedirectURI string
	RecentReauthentication  time.Duration
}

type OIDCOperation struct {
	Kind               OIDCFlowKind
	Provider           string
	Audience           Audience
	TenantID           uuid.UUID
	PrincipalID        uuid.UUID
	SessionID          uuid.UUID
	State              string
	Nonce              string
	CodeVerifier       string
	RedirectURI        string
	IdempotencyKey     string
	RequestFingerprint string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type OIDCAuthorizationRequest struct {
	State        string
	Nonce        string
	CodeVerifier string
	RedirectURI  string
}

type OIDCExchangeRequest struct {
	Code         string
	Nonce        string
	CodeVerifier string
	RedirectURI  string
}

type OIDCVerifiedIdentity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
}

type OIDCProvider interface {
	Name() string
	AuthorizationURL(OIDCAuthorizationRequest) (string, error)
	ExchangeAndVerify(context.Context, OIDCExchangeRequest) (OIDCVerifiedIdentity, error)
}

type OIDCOperationStore interface {
	CreateOrGet(context.Context, OIDCOperation) (OIDCOperation, error)
	Consume(context.Context, string) (OIDCOperation, error)
}

type BeginOIDCLoginCommand struct {
	Audience       Audience
	TenantID       uuid.UUID
	RedirectURI    string
	IdempotencyKey string
}

type BeginOIDCLoginResult struct {
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

type CompleteOIDCLoginCommand struct {
	Code        string
	State       string
	RedirectURI string
	DeviceName  string
}

type OIDCLoginState struct {
	IdentityID       uuid.UUID
	PrincipalID      uuid.UUID
	PrincipalStatus  PrincipalStatus
	MembershipID     uuid.UUID
	MembershipStatus MembershipStatus
	TenantAccess     TenantAccessStatus
	Lifecycle        TenantLifecycleStatus
	LifecycleFresh   bool
	NormalizedEmail  string
}

type OIDCLoginMutation struct {
	IdentityID      uuid.UUID
	Provider        string
	Issuer          string
	Subject         string
	NormalizedEmail string
	Session         Session
	Grant           SessionGrant
	RefreshFamily   RefreshTokenFamily
	RefreshToken    RefreshToken
	Audit           SecurityAuditEvent
}

type OIDCLoginReader interface {
	LookupOIDCLogin(context.Context, TenantScope, string, string, string) (OIDCLoginState, error)
}

type OIDCReauthenticationState struct {
	PrincipalID       uuid.UUID
	PrincipalStatus   PrincipalStatus
	SessionID         uuid.UUID
	SessionStatus     SessionStatus
	GrantID           uuid.UUID
	GrantStatus       GrantStatus
	GrantVersion      int64
	TenantID          uuid.UUID
	ReauthenticatedAt time.Time
}

type OIDCReader interface {
	OIDCLoginReader
	LookupOIDCReauthentication(context.Context, TenantScope, AccessTokenClaims) (OIDCReauthenticationState, error)
}

type OIDCTokenCodec interface {
	AccessTokenIssuer
	AccessCredentialVerifier
}

type OIDCLoginUnitOfWork interface {
	CommitOIDCLogin(context.Context, TenantScope, OIDCLoginMutation) error
	RecordOIDCLoginFailure(context.Context, TenantScope, SecurityAuditEvent) error
}

type OIDCIdentityLinkMutation struct {
	IdentityID           uuid.UUID
	PrincipalID          uuid.UUID
	Provider             string
	Issuer               string
	Subject              string
	NormalizedEmail      string
	SessionID            uuid.UUID
	GrantID              uuid.UUID
	ExpectedGrantVersion int64
	ReauthenticatedAfter time.Time
	LinkedAt             time.Time
	Audit                SecurityAuditEvent
}

type OIDCIdentityLinkResult struct {
	IdentityID  uuid.UUID
	PrincipalID uuid.UUID
}

type OIDCUnitOfWork interface {
	OIDCLoginUnitOfWork
	LinkOIDCIdentity(context.Context, TenantScope, OIDCIdentityLinkMutation) (OIDCIdentityLinkResult, error)
	RecordOIDCIdentityLinkFailure(context.Context, TenantScope, SecurityAuditEvent) error
}

const (
	AuditActionOIDCLoginSucceeded     AuditAction     = "iam.oidc.login.succeeded"
	AuditActionOIDCLoginFailed        AuditAction     = "iam.oidc.login.failed"
	AuditActionOIDCIdentityLinked     AuditAction     = "iam.oidc.identity.linked"
	AuditActionOIDCIdentityLinkFailed AuditAction     = "iam.oidc.identity.link.failed"
	AuditTargetTypeIdentity           AuditTargetType = "identity"
	AuditTargetTypeOIDCOperation      AuditTargetType = "oidc_operation"
	AuditReasonOIDCLogin              AuditReason     = "OIDC_LOGIN"
	AuditReasonOIDCLoginFailed        AuditReason     = "OIDC_LOGIN_FAILED"
	AuditReasonOIDCIdentityLink       AuditReason     = "OIDC_IDENTITY_LINK"
	AuditReasonOIDCIdentityLinkFailed AuditReason     = "OIDC_IDENTITY_LINK_FAILED"
)

type OIDCUsecase struct {
	config   OIDCUsecaseConfig
	provider OIDCProvider
	store    OIDCOperationStore
	reader   OIDCReader
	uow      OIDCUnitOfWork
	tokens   OIDCTokenCodec
	secrets  SecretGenerator
	ids      IDGenerator
	clock    Clock
}

func NewOIDCUsecase(
	config OIDCUsecaseConfig,
	provider OIDCProvider,
	store OIDCOperationStore,
	reader OIDCReader,
	uow OIDCUnitOfWork,
	tokens OIDCTokenCodec,
	secrets SecretGenerator,
	ids IDGenerator,
	clock Clock,
) (*OIDCUsecase, error) {
	if config.Provider == "" || config.LoginRedirectURI == "" || config.IdentityLinkRedirectURI == "" ||
		config.Provider != strings.TrimSpace(config.Provider) || config.LoginRedirectURI != strings.TrimSpace(config.LoginRedirectURI) ||
		config.IdentityLinkRedirectURI != strings.TrimSpace(config.IdentityLinkRedirectURI) ||
		config.RecentReauthentication <= 0 || provider == nil || store == nil || reader == nil || uow == nil || tokens == nil ||
		secrets == nil || ids == nil || clock == nil ||
		strings.TrimSpace(provider.Name()) != config.Provider {
		return nil, ErrOIDCConfigurationInvalid
	}
	return &OIDCUsecase{
		config: config, provider: provider, store: store, reader: reader, uow: uow,
		tokens: tokens, secrets: secrets, ids: ids, clock: clock,
	}, nil
}

type BeginOIDCIdentityLinkCommand struct {
	RawCredential  string
	Provider       string
	RedirectURI    string
	IdempotencyKey string
}

type BeginOIDCIdentityLinkResult struct {
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

func (u *OIDCUsecase) BeginIdentityLink(ctx context.Context, command BeginOIDCIdentityLinkCommand) (BeginOIDCIdentityLinkResult, error) {
	credential := command.RawCredential
	if strings.TrimSpace(credential) == "" {
		return BeginOIDCIdentityLinkResult{}, ErrAuthorizationCredentialRequired
	}
	if command.Provider != u.config.Provider {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCConfigurationInvalid
	}
	redirectURI := command.RedirectURI
	if redirectURI != u.config.IdentityLinkRedirectURI {
		return BeginOIDCIdentityLinkResult{}, ErrOIDCRedirectInvalid
	}
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if idempotencyKey == "" {
		return BeginOIDCIdentityLinkResult{}, ErrIdempotencyKeyRequired
	}
	now := u.clock.Now().UTC()
	claims, _, err := u.validateIdentityLinkAuthentication(ctx, credential, now)
	if err != nil {
		return BeginOIDCIdentityLinkResult{}, err
	}
	secrets, err := u.newSecrets(3)
	if err != nil {
		return BeginOIDCIdentityLinkResult{}, err
	}
	operation := OIDCOperation{
		Kind: OIDCFlowIdentityLink, Provider: u.config.Provider, Audience: claims.Audience,
		TenantID: claims.TenantID, PrincipalID: claims.Subject, SessionID: claims.SessionID,
		State: secrets[0], Nonce: secrets[1], CodeVerifier: secrets[2], RedirectURI: redirectURI,
		IdempotencyKey: idempotencyKey,
		RequestFingerprint: oidcRequestFingerprint(
			OIDCFlowIdentityLink, u.config.Provider, claims.Audience, claims.TenantID, claims.Subject, redirectURI,
		),
		CreatedAt: now, ExpiresAt: now.Add(oidcOperationLifetime),
	}
	stored, err := u.store.CreateOrGet(ctx, operation)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return BeginOIDCIdentityLinkResult{}, err
		}
		return BeginOIDCIdentityLinkResult{}, fmt.Errorf("persist OIDC identity-link operation: %w", errors.Join(ErrOIDCDependency, err))
	}
	authorizationURL, err := u.provider.AuthorizationURL(OIDCAuthorizationRequest{
		State: stored.State, Nonce: stored.Nonce, CodeVerifier: stored.CodeVerifier, RedirectURI: stored.RedirectURI,
	})
	if err != nil || strings.TrimSpace(authorizationURL) == "" {
		return BeginOIDCIdentityLinkResult{}, fmt.Errorf("build OIDC identity-link authorization URL: %w", errors.Join(ErrOIDCDependency, err))
	}
	return BeginOIDCIdentityLinkResult{
		AuthorizationURL: authorizationURL, State: stored.State, ExpiresAt: stored.ExpiresAt,
	}, nil
}

type CompleteOIDCIdentityLinkCommand struct {
	RawCredential string
	Code          string
	State         string
	RedirectURI   string
}

func (u *OIDCUsecase) CompleteIdentityLink(ctx context.Context, command CompleteOIDCIdentityLinkCommand) (OIDCIdentityLinkResult, error) {
	credential := command.RawCredential
	code := command.Code
	state := command.State
	redirectURI := command.RedirectURI
	if strings.TrimSpace(credential) == "" {
		return OIDCIdentityLinkResult{}, ErrAuthorizationCredentialRequired
	}
	if strings.TrimSpace(code) == "" {
		return OIDCIdentityLinkResult{}, ErrInvalidCredential
	}
	if strings.TrimSpace(state) == "" {
		return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	if redirectURI != u.config.IdentityLinkRedirectURI {
		return OIDCIdentityLinkResult{}, ErrOIDCRedirectInvalid
	}
	now := u.clock.Now().UTC()
	claims, scope, err := u.validateIdentityLinkAuthentication(ctx, credential, now)
	if err != nil {
		return OIDCIdentityLinkResult{}, err
	}
	operation, err := u.store.Consume(ctx, state)
	if err != nil {
		if errors.Is(err, ErrOIDCStateInvalid) {
			return OIDCIdentityLinkResult{}, err
		}
		return OIDCIdentityLinkResult{}, fmt.Errorf("consume OIDC identity-link operation: %w", errors.Join(ErrOIDCDependency, err))
	}
	if operation.Kind != OIDCFlowIdentityLink || operation.Provider != u.config.Provider ||
		operation.Audience != claims.Audience || operation.TenantID != claims.TenantID ||
		operation.PrincipalID != claims.Subject || operation.SessionID != claims.SessionID ||
		operation.State != state || operation.RedirectURI != redirectURI || operation.Nonce == "" || operation.CodeVerifier == "" ||
		operation.ExpiresAt.IsZero() || !now.Before(operation.ExpiresAt.UTC()) {
		return OIDCIdentityLinkResult{}, ErrOIDCStateInvalid
	}
	identity, err := u.provider.ExchangeAndVerify(ctx, OIDCExchangeRequest{
		Code: code, Nonce: operation.Nonce, CodeVerifier: operation.CodeVerifier, RedirectURI: operation.RedirectURI,
	})
	if err != nil {
		return u.recordOIDCIdentityLinkFailure(ctx, scope, claims, operation, now, classifyOIDCProviderExchangeError(err))
	}
	if identity.Issuer == "" || identity.Subject == "" || !identity.EmailVerified {
		return u.recordOIDCIdentityLinkFailure(ctx, scope, claims, operation, now, ErrOIDCEmailUnverified)
	}
	normalizedEmail, err := normalizeOIDCEmail(identity.Email)
	if err != nil {
		return u.recordOIDCIdentityLinkFailure(ctx, scope, claims, operation, now, ErrOIDCEmailUnverified)
	}
	ids, err := u.newIDs(2)
	if err != nil {
		return OIDCIdentityLinkResult{}, err
	}
	auditMethod := firstHumanAuthenticationMethod(claims.AuthnMethods)
	audit := SecurityAuditEvent{
		ID: ids[1], ActorID: claims.Subject, AuthenticationMethod: auditMethod,
		Boundary: AuditBoundaryTenant, Action: AuditActionOIDCIdentityLinked, TargetType: AuditTargetTypeIdentity,
		TargetID: ids[0], TargetVersion: 1, Result: AuditResultSucceeded, Reason: AuditReasonOIDCIdentityLink,
		RequestID: operation.IdempotencyKey, CorrelationID: operation.IdempotencyKey, DecisionID: ids[1].String(),
		SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
	result, err := u.uow.LinkOIDCIdentity(ctx, scope, OIDCIdentityLinkMutation{
		IdentityID: ids[0], PrincipalID: claims.Subject, Provider: operation.Provider,
		Issuer: identity.Issuer, Subject: identity.Subject, NormalizedEmail: normalizedEmail,
		SessionID: claims.SessionID, GrantID: claims.GrantID, ExpectedGrantVersion: claims.GrantVersion,
		ReauthenticatedAfter: now.Add(-u.config.RecentReauthentication),
		LinkedAt:             now, Audit: audit,
	})
	if err != nil {
		if errors.Is(err, ErrOIDCReauthenticationRequired) || errors.Is(err, ErrOIDCIdentityConflict) || errors.Is(err, ErrOIDCEmailConflict) {
			return u.recordOIDCIdentityLinkFailure(ctx, scope, claims, operation, now, err)
		}
		failure := fmt.Errorf("commit OIDC identity link: %w", errors.Join(ErrOIDCDependency, err))
		return u.recordOIDCIdentityLinkFailure(ctx, scope, claims, operation, now, failure)
	}
	return result, nil
}

func (u *OIDCUsecase) recordOIDCIdentityLinkFailure(
	ctx context.Context,
	scope TenantScope,
	claims AccessTokenClaims,
	operation OIDCOperation,
	failedAt time.Time,
	failure error,
) (OIDCIdentityLinkResult, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return OIDCIdentityLinkResult{}, err
	}
	audit := SecurityAuditEvent{
		ID: ids[0], ActorID: claims.Subject, AuthenticationMethod: firstHumanAuthenticationMethod(claims.AuthnMethods),
		Boundary: AuditBoundaryTenant, Action: AuditActionOIDCIdentityLinkFailed, TargetType: AuditTargetTypeOIDCOperation,
		TargetID: ids[0], TargetVersion: 1, Result: AuditResultFailed, Reason: AuditReasonOIDCIdentityLinkFailed,
		RequestID: operation.IdempotencyKey, CorrelationID: operation.IdempotencyKey, DecisionID: ids[0].String(),
		SourceService: AuditSourceServiceIAM, OccurredAt: failedAt, RecordedAt: failedAt,
	}
	if err := u.uow.RecordOIDCIdentityLinkFailure(ctx, scope, audit); err != nil {
		return OIDCIdentityLinkResult{}, fmt.Errorf("record redacted OIDC identity-link failure: %w", errors.Join(ErrOIDCDependency, err))
	}
	return OIDCIdentityLinkResult{}, failure
}

func (u *OIDCUsecase) validateIdentityLinkAuthentication(
	ctx context.Context,
	credential string,
	now time.Time,
) (AccessTokenClaims, TenantScope, error) {
	if credential == "" {
		return AccessTokenClaims{}, TenantScope{}, ErrAuthorizationCredentialRequired
	}
	claims, err := u.tokens.Verify(ctx, credential)
	if err != nil {
		return AccessTokenClaims{}, TenantScope{}, errors.Join(ErrAuthorizationCredentialInvalid, err)
	}
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.GrantVersion <= 0 ||
		claims.TenantID == uuid.Nil || claims.ExpiresAt.IsZero() || !now.Before(claims.ExpiresAt.UTC()) ||
		!containsHumanAuthenticationMethod(claims.AuthnMethods) {
		return AccessTokenClaims{}, TenantScope{}, ErrAuthorizationCredentialInvalid
	}
	scope, err := NewTenantScope(claims.TenantID)
	if err != nil {
		return AccessTokenClaims{}, TenantScope{}, ErrAuthorizationCredentialInvalid
	}
	reauthentication, err := u.reader.LookupOIDCReauthentication(ctx, scope, claims)
	if err != nil {
		if errors.Is(err, ErrOIDCReauthenticationRequired) {
			return AccessTokenClaims{}, TenantScope{}, ErrOIDCReauthenticationRequired
		}
		return AccessTokenClaims{}, TenantScope{}, fmt.Errorf("lookup OIDC link authentication: %w", errors.Join(ErrOIDCDependency, err))
	}
	if !validOIDCReauthentication(claims, reauthentication, now, u.config.RecentReauthentication) {
		return AccessTokenClaims{}, TenantScope{}, ErrOIDCReauthenticationRequired
	}
	return claims, scope, nil
}

func containsHumanAuthenticationMethod(methods []AuditAuthenticationMethod) bool {
	for _, method := range methods {
		if method == AuditAuthenticationMethodPassword || method == AuditAuthenticationMethodOIDC {
			return true
		}
	}
	return false
}

func firstHumanAuthenticationMethod(methods []AuditAuthenticationMethod) AuditAuthenticationMethod {
	for _, method := range methods {
		if method == AuditAuthenticationMethodPassword || method == AuditAuthenticationMethodOIDC {
			return method
		}
	}
	return ""
}

func validOIDCReauthentication(claims AccessTokenClaims, state OIDCReauthenticationState, now time.Time, window time.Duration) bool {
	return state.PrincipalID == claims.Subject && state.PrincipalStatus == PrincipalStatusActive &&
		state.SessionID == claims.SessionID && state.SessionStatus == SessionStatusActive &&
		state.GrantID == claims.GrantID && state.GrantStatus == GrantStatusActive &&
		state.GrantVersion == claims.GrantVersion && state.TenantID == claims.TenantID &&
		!state.ReauthenticatedAt.IsZero() && !state.ReauthenticatedAt.After(now) &&
		now.Sub(state.ReauthenticatedAt.UTC()) <= window
}

func (u *OIDCUsecase) BeginLogin(ctx context.Context, command BeginOIDCLoginCommand) (BeginOIDCLoginResult, error) {
	if command.Audience == "" {
		return BeginOIDCLoginResult{}, ErrAudienceRequired
	}
	if command.Audience != AudienceConsole {
		return BeginOIDCLoginResult{}, ErrOIDCDependency
	}
	if _, err := NewTenantScope(command.TenantID); err != nil {
		return BeginOIDCLoginResult{}, err
	}
	redirectURI := command.RedirectURI
	if redirectURI != u.config.LoginRedirectURI {
		return BeginOIDCLoginResult{}, ErrOIDCRedirectInvalid
	}
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if idempotencyKey == "" {
		return BeginOIDCLoginResult{}, ErrIdempotencyKeyRequired
	}
	secrets, err := u.newSecrets(3)
	if err != nil {
		return BeginOIDCLoginResult{}, err
	}
	now := u.clock.Now().UTC()
	operation := OIDCOperation{
		Kind:               OIDCFlowLogin,
		Provider:           u.config.Provider,
		Audience:           command.Audience,
		TenantID:           command.TenantID,
		State:              secrets[0],
		Nonce:              secrets[1],
		CodeVerifier:       secrets[2],
		RedirectURI:        redirectURI,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: oidcRequestFingerprint(OIDCFlowLogin, u.config.Provider, command.Audience, command.TenantID, uuid.Nil, redirectURI),
		CreatedAt:          now,
		ExpiresAt:          now.Add(oidcOperationLifetime),
	}
	stored, err := u.store.CreateOrGet(ctx, operation)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return BeginOIDCLoginResult{}, err
		}
		return BeginOIDCLoginResult{}, fmt.Errorf("persist OIDC login operation: %w", errors.Join(ErrOIDCDependency, err))
	}
	authorizationURL, err := u.provider.AuthorizationURL(OIDCAuthorizationRequest{
		State:        stored.State,
		Nonce:        stored.Nonce,
		CodeVerifier: stored.CodeVerifier,
		RedirectURI:  stored.RedirectURI,
	})
	if err != nil || strings.TrimSpace(authorizationURL) == "" {
		return BeginOIDCLoginResult{}, fmt.Errorf("build OIDC authorization URL: %w", errors.Join(ErrOIDCDependency, err))
	}
	return BeginOIDCLoginResult{
		AuthorizationURL: authorizationURL,
		State:            stored.State,
		ExpiresAt:        stored.ExpiresAt,
	}, nil
}

func (u *OIDCUsecase) CompleteLogin(ctx context.Context, command CompleteOIDCLoginCommand) (LoginResult, error) {
	code := command.Code
	state := command.State
	redirectURI := command.RedirectURI
	if strings.TrimSpace(code) == "" {
		return LoginResult{}, ErrInvalidCredential
	}
	if strings.TrimSpace(state) == "" {
		return LoginResult{}, ErrOIDCStateInvalid
	}
	if redirectURI != u.config.LoginRedirectURI {
		return LoginResult{}, ErrOIDCRedirectInvalid
	}
	operation, err := u.store.Consume(ctx, state)
	if err != nil {
		if errors.Is(err, ErrOIDCStateInvalid) {
			return LoginResult{}, err
		}
		return LoginResult{}, fmt.Errorf("consume OIDC login operation: %w", errors.Join(ErrOIDCDependency, err))
	}
	now := u.clock.Now().UTC()
	if operation.Kind != OIDCFlowLogin || operation.Provider != u.config.Provider || operation.Audience != AudienceConsole ||
		operation.TenantID == uuid.Nil || operation.State != state || operation.RedirectURI != redirectURI ||
		operation.Nonce == "" || operation.CodeVerifier == "" || strings.TrimSpace(operation.IdempotencyKey) == "" ||
		operation.ExpiresAt.IsZero() || !now.Before(operation.ExpiresAt.UTC()) {
		return LoginResult{}, ErrOIDCStateInvalid
	}
	scope, err := NewTenantScope(operation.TenantID)
	if err != nil {
		return LoginResult{}, ErrOIDCStateInvalid
	}
	identity, err := u.provider.ExchangeAndVerify(ctx, OIDCExchangeRequest{
		Code:         code,
		Nonce:        operation.Nonce,
		CodeVerifier: operation.CodeVerifier,
		RedirectURI:  operation.RedirectURI,
	})
	if err != nil {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, classifyOIDCProviderExchangeError(err))
	}
	if identity.Issuer == "" || identity.Subject == "" || !identity.EmailVerified {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrOIDCEmailUnverified)
	}
	normalizedEmail, err := normalizeOIDCEmail(identity.Email)
	if err != nil {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrOIDCEmailUnverified)
	}
	loginState, err := u.reader.LookupOIDCLogin(ctx, scope, operation.Provider, identity.Issuer, identity.Subject)
	if err != nil {
		if errors.Is(err, ErrOIDCIdentityNotFound) {
			return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrInvalidCredential)
		}
		return LoginResult{}, fmt.Errorf("lookup OIDC identity: %w", errors.Join(ErrOIDCDependency, err))
	}
	if strings.ToLower(strings.TrimSpace(loginState.NormalizedEmail)) != normalizedEmail {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrInvalidCredential)
	}
	if loginState.IdentityID == uuid.Nil || loginState.PrincipalID == uuid.Nil || loginState.MembershipID == uuid.Nil {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrInvalidCredential)
	}
	if loginState.PrincipalStatus != PrincipalStatusActive {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrPrincipalInactive)
	}
	if loginState.MembershipStatus != MembershipStatusActive {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrMembershipInactive)
	}
	if loginState.TenantAccess != TenantAccessStatusActive {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrTenantAccessInactive)
	}
	if !loginState.LifecycleFresh {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrTenantLifecycleStale)
	}
	if loginState.Lifecycle != TenantLifecycleStatusActive {
		return u.recordOIDCLoginFailure(ctx, scope, operation, now, ErrTenantLifecycleBlocked)
	}
	ids, err := u.newIDs(6)
	if err != nil {
		return LoginResult{}, err
	}
	accessExpiresAt, idleExpiresAt, absoluteExpiresAt := loginDeadlines(operation.Audience, now)
	session := Session{
		ID: ids[0], PrincipalID: loginState.PrincipalID, Audience: operation.Audience,
		Status: SessionStatusActive, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodOIDC},
		DeviceName:    strings.TrimSpace(command.DeviceName),
		IdleExpiresAt: idleExpiresAt, AbsoluteExpiry: absoluteExpiresAt, ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	grant := SessionGrant{
		ID: ids[1], SessionID: session.ID, MembershipID: loginState.MembershipID,
		Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	family := RefreshTokenFamily{
		ID: ids[2], GrantID: grant.ID, Status: GrantStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	refreshSecret, err := u.secrets.NewSecret()
	if err != nil || strings.TrimSpace(refreshSecret) == "" {
		return LoginResult{}, fmt.Errorf("generate OIDC refresh secret: %w", errors.Join(ErrOIDCDependency, err))
	}
	refresh := RefreshToken{
		ID: ids[3], FamilyID: family.ID, Digest: sha256.Sum256([]byte(refreshSecret)),
		IssuedAt: now, ExpiresAt: absoluteExpiresAt,
	}
	claims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: loginState.PrincipalID, Audience: operation.Audience,
		TokenID: ids[4], SessionID: session.ID, GrantID: grant.ID, GrantVersion: grant.Version,
		TenantID: operation.TenantID, IssuedAt: now, ExpiresAt: accessExpiresAt,
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodOIDC},
	}
	accessToken, err := u.tokens.Issue(ctx, claims)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue OIDC access token: %w", errors.Join(ErrOIDCDependency, err))
	}
	audit := SecurityAuditEvent{
		ID: ids[5], ActorID: loginState.PrincipalID, AuthenticationMethod: AuditAuthenticationMethodOIDC,
		Boundary: AuditBoundaryTenant, Action: AuditActionOIDCLoginSucceeded, TargetType: AuditTargetTypeSession,
		TargetID: session.ID, TargetVersion: grant.Version, Result: AuditResultSucceeded,
		Reason: AuditReasonOIDCLogin, RequestID: operation.IdempotencyKey, CorrelationID: operation.IdempotencyKey,
		DecisionID: ids[5].String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
	if err := u.uow.CommitOIDCLogin(ctx, scope, OIDCLoginMutation{
		IdentityID: loginState.IdentityID, Provider: operation.Provider, Issuer: identity.Issuer,
		Subject: identity.Subject, NormalizedEmail: normalizedEmail,
		Session: session, Grant: grant, RefreshFamily: family, RefreshToken: refresh, Audit: audit,
	}); err != nil {
		if isOIDCLoginStateError(err) {
			return u.recordOIDCLoginFailure(ctx, scope, operation, now, err)
		}
		return LoginResult{}, fmt.Errorf("commit OIDC login: %w", errors.Join(ErrOIDCDependency, err))
	}
	return LoginResult{
		TenantID:  operation.TenantID,
		Principal: Principal{ID: loginState.PrincipalID, Status: loginState.PrincipalStatus},
		Session:   session, Grant: grant, AccessToken: accessToken, RefreshToken: refreshSecret,
		AccessTokenExpiresAt: accessExpiresAt,
	}, nil
}

func (u *OIDCUsecase) recordOIDCLoginFailure(
	ctx context.Context,
	scope TenantScope,
	operation OIDCOperation,
	failedAt time.Time,
	failure error,
) (LoginResult, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return LoginResult{}, err
	}
	audit := SecurityAuditEvent{
		ID: ids[0], ActorID: uuid.Nil, AuthenticationMethod: AuditAuthenticationMethodAnonymous,
		Boundary: AuditBoundaryTenant, Action: AuditActionOIDCLoginFailed, TargetType: AuditTargetTypeOIDCOperation,
		TargetID: ids[0], TargetVersion: 1, Result: AuditResultFailed, Reason: AuditReasonOIDCLoginFailed,
		RequestID: operation.IdempotencyKey, CorrelationID: operation.IdempotencyKey, DecisionID: ids[0].String(),
		SourceService: AuditSourceServiceIAM, OccurredAt: failedAt, RecordedAt: failedAt,
	}
	if err := u.uow.RecordOIDCLoginFailure(ctx, scope, audit); err != nil {
		return LoginResult{}, fmt.Errorf("record redacted OIDC login failure: %w", errors.Join(ErrOIDCDependency, err))
	}
	return LoginResult{}, failure
}

func classifyOIDCProviderExchangeError(err error) error {
	if errors.Is(err, ErrOIDCDependency) {
		return ErrOIDCDependency
	}
	return ErrInvalidCredential
}

func isOIDCLoginStateError(err error) bool {
	return errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrPrincipalInactive) ||
		errors.Is(err, ErrMembershipInactive) || errors.Is(err, ErrTenantAccessInactive) ||
		errors.Is(err, ErrTenantLifecycleStale) || errors.Is(err, ErrTenantLifecycleBlocked)
}

func normalizeOIDCEmail(value string) (string, error) {
	email := strings.TrimSpace(value)
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.Contains(email[:at], "@") ||
		strings.ContainsAny(email[:at], " \t\r\n") {
		return "", ErrOIDCEmailUnverified
	}
	domain, err := idna.Lookup.ToASCII(strings.ToLower(email[at+1:]))
	if err != nil || domain == "" {
		return "", ErrOIDCEmailUnverified
	}
	return strings.ToLower(email[:at]) + "@" + strings.ToLower(domain), nil
}

func (u *OIDCUsecase) newSecrets(count int) ([]string, error) {
	values := make([]string, count)
	seen := make(map[string]struct{}, count)
	for index := range values {
		value, err := u.secrets.NewSecret()
		if err != nil {
			return nil, fmt.Errorf("generate OIDC secret: %w", errors.Join(ErrOIDCDependency, err))
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, ErrOIDCDependency
		}
		if _, exists := seen[value]; exists {
			return nil, ErrOIDCDependency
		}
		seen[value] = struct{}{}
		values[index] = value
	}
	return values, nil
}

func (u *OIDCUsecase) newIDs(count int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		id, err := u.ids.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate OIDC ID: %w", errors.Join(ErrOIDCDependency, err))
		}
		if id == uuid.Nil || id.Version() != 7 {
			return nil, ErrInvalidGeneratedID
		}
		ids[index] = id
	}
	return ids, nil
}

func oidcRequestFingerprint(kind OIDCFlowKind, provider string, audience Audience, tenantID, principalID uuid.UUID, redirectURI string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		string(kind), provider, string(audience), tenantID.String(), principalID.String(), redirectURI,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
