package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAccountRequired           = errors.New("account is required")
	ErrPasswordRequired          = errors.New("password is required")
	ErrAudienceRequired          = errors.New("audience is required")
	ErrSourceIPRequired          = errors.New("source IP is required")
	ErrIdempotencyKeyRequired    = errors.New("idempotency key is required")
	ErrInvalidCredential         = errors.New("invalid credential")
	ErrPrincipalInactive         = errors.New("principal is inactive")
	ErrMembershipInactive        = errors.New("tenant membership is inactive")
	ErrTenantAccessInactive      = errors.New("tenant access is inactive")
	ErrTenantLifecycleBlocked    = errors.New("tenant lifecycle blocks access")
	ErrTenantLifecycleStale      = errors.New("tenant lifecycle projection is stale")
	ErrAuthenticationRateLimited = errors.New("authentication rate limit exceeded")
	ErrAuthenticationDependency  = errors.New("authentication dependency is unavailable")
)

type AuthenticationRateLimitError struct {
	LimitScope string
	RetryAfter time.Duration
}

func (e *AuthenticationRateLimitError) Error() string {
	return ErrAuthenticationRateLimited.Error()
}

func (e *AuthenticationRateLimitError) Unwrap() error {
	return ErrAuthenticationRateLimited
}

type PrincipalStatus string

const (
	PrincipalStatusActive   PrincipalStatus = "active"
	PrincipalStatusDisabled PrincipalStatus = "disabled"
)

type TenantAccessStatus string

const (
	TenantAccessStatusBootstrapPending TenantAccessStatus = "bootstrap_pending"
	TenantAccessStatusActive           TenantAccessStatus = "active"
	TenantAccessStatusSuspended        TenantAccessStatus = "suspended"
)

type TenantLifecycleStatus string

const (
	TenantLifecycleStatusActive TenantLifecycleStatus = "active"
)

type SessionStatus string

const (
	SessionStatusActive  SessionStatus = "active"
	SessionStatusRevoked SessionStatus = "revoked"
)

type GrantStatus string

const (
	GrantStatusActive  GrantStatus = "active"
	GrantStatusRevoked GrantStatus = "revoked"
)

type Audience string

const (
	AudienceConsole Audience = "console"
	AudienceBoss    Audience = "boss"
)

const (
	AuditActionPasswordLoginSucceeded AuditAction     = "iam.password.login.succeeded"
	AuditActionPasswordLoginFailed    AuditAction     = "iam.password.login.failed"
	AuditTargetTypeSession            AuditTargetType = "session"
	AuditTargetTypePasswordCredential AuditTargetType = "password_credential"
	AuditReasonPasswordLogin          AuditReason     = "PASSWORD_LOGIN"
	AuditReasonInvalidCredential      AuditReason     = "INVALID_CREDENTIAL"
)

type Principal struct {
	ID     uuid.UUID
	Status PrincipalStatus
}

type PasswordLoginState struct {
	PrincipalID       uuid.UUID
	PrincipalStatus   PrincipalStatus
	MembershipID      uuid.UUID
	MembershipStatus  MembershipStatus
	TenantAccess      TenantAccessStatus
	Lifecycle         TenantLifecycleStatus
	LifecycleFresh    bool
	PasswordHash      string
	FailedAttempts    int
	LockedUntil       time.Time
	CredentialVersion int64
}

type Session struct {
	ID                uuid.UUID
	PrincipalID       uuid.UUID
	Audience          Audience
	Status            SessionStatus
	AuthnMethods      []AuditAuthenticationMethod
	DeviceName        string
	IdleExpiresAt     time.Time
	AbsoluteExpiry    time.Time
	ReauthenticatedAt time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SessionGrant struct {
	ID           uuid.UUID
	SessionID    uuid.UUID
	MembershipID uuid.UUID
	Status       GrantStatus
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshTokenFamily struct {
	ID        uuid.UUID
	GrantID   uuid.UUID
	Status    GrantStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RefreshToken struct {
	ID        uuid.UUID
	FamilyID  uuid.UUID
	Digest    [sha256.Size]byte
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type AccessTokenClaims struct {
	Issuer       string
	Subject      uuid.UUID
	Audience     Audience
	TokenID      uuid.UUID
	SessionID    uuid.UUID
	GrantID      uuid.UUID
	GrantVersion int64
	TenantID     uuid.UUID
	IssuedAt     time.Time
	ExpiresAt    time.Time
	AuthnMethods []AuditAuthenticationMethod
}

type PasswordActionPurpose string

const (
	PasswordActionPurposeSetup PasswordActionPurpose = "setup"
	PasswordActionPurposeReset PasswordActionPurpose = "reset"
)

// PasswordActionTokenClaims is the complete signed capability used to consume
// one password action. It contains no password or recipient address.
type PasswordActionTokenClaims struct {
	Issuer      string
	PrincipalID uuid.UUID
	OperationID uuid.UUID
	Purpose     PasswordActionPurpose
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

type PasswordLoginCommand struct {
	Account        string
	Password       string
	Audience       Audience
	TenantID       uuid.UUID
	SourceIP       netip.Addr
	DeviceName     string
	IdempotencyKey string
}

// LoginResult is the shared Human login result produced by Password and OIDC
// authentication. Transport-specific response mapping stays in service.
type LoginResult struct {
	TenantID             uuid.UUID
	Principal            Principal
	Session              Session
	Grant                SessionGrant
	AccessToken          string
	RefreshToken         string
	AccessTokenExpiresAt time.Time
}

type PasswordLoginResult = LoginResult

// LoginThrottleAttempt is the normalized, non-secret abuse-control input. The
// adapter hashes both values before constructing storage keys.
type LoginThrottleAttempt struct {
	NormalizedAccount string
	SourceIP          netip.Addr
}

type LoginMutation struct {
	NormalizedAccount string
	CredentialVersion int64
	Session           Session
	Grant             SessionGrant
	RefreshFamily     RefreshTokenFamily
	RefreshToken      RefreshToken
	Audit             SecurityAuditEvent
}

type LoginFailureMutation struct {
	PrincipalID uuid.UUID
	FailedAt    time.Time
	LockUntil   time.Time
	Audit       SecurityAuditEvent
}

type PasswordLoginReader interface {
	LookupPasswordLogin(context.Context, TenantScope, string) (PasswordLoginState, error)
}

type PasswordVerifier interface {
	Verify(encodedHash string, password string) (bool, error)
	VerifyUnknown(password string) error
}

type LoginThrottle interface {
	Check(context.Context, LoginThrottleAttempt) error
	RecordFailure(context.Context, LoginThrottleAttempt) error
	Reset(context.Context, LoginThrottleAttempt) error
}

type LoginUnitOfWork interface {
	CommitLogin(context.Context, TenantScope, LoginMutation) error
	RecordLoginFailure(context.Context, TenantScope, LoginFailureMutation) error
}

type AccessTokenIssuer interface {
	Issue(context.Context, AccessTokenClaims) (string, error)
}

type SecretGenerator interface {
	NewSecret() (string, error)
}

type AuthenticationUsecase struct {
	reader   AuthenticationReader
	password AuthenticationPassword
	throttle LoginThrottle
	uow      AuthenticationUnitOfWork
	tokens   AuthenticationTokenCodec
	secrets  SecretGenerator
	ids      IDGenerator
	clock    Clock
}

func NewAuthenticationUsecase(
	reader AuthenticationReader,
	password AuthenticationPassword,
	throttle LoginThrottle,
	uow AuthenticationUnitOfWork,
	tokens AuthenticationTokenCodec,
	secrets SecretGenerator,
	ids IDGenerator,
	clock Clock,
) *AuthenticationUsecase {
	return &AuthenticationUsecase{
		reader:   reader,
		password: password,
		throttle: throttle,
		uow:      uow,
		tokens:   tokens,
		secrets:  secrets,
		ids:      ids,
		clock:    clock,
	}
}

func (u *AuthenticationUsecase) PasswordLogin(ctx context.Context, command PasswordLoginCommand) (PasswordLoginResult, error) {
	normalizedAccount := strings.ToLower(strings.TrimSpace(command.Account))
	if normalizedAccount == "" {
		return PasswordLoginResult{}, ErrAccountRequired
	}
	if command.Password == "" {
		return PasswordLoginResult{}, ErrPasswordRequired
	}
	if command.Audience == "" {
		return PasswordLoginResult{}, ErrAudienceRequired
	}
	if !command.SourceIP.IsValid() {
		return PasswordLoginResult{}, ErrSourceIPRequired
	}
	if command.IdempotencyKey == "" {
		return PasswordLoginResult{}, ErrIdempotencyKeyRequired
	}
	scope, err := NewTenantScope(command.TenantID)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	attempt := LoginThrottleAttempt{
		NormalizedAccount: normalizedAccount,
		SourceIP:          command.SourceIP.Unmap(),
	}
	if err := u.throttle.Check(ctx, attempt); err != nil {
		if errors.Is(err, ErrAuthenticationRateLimited) {
			return PasswordLoginResult{}, err
		}
		return PasswordLoginResult{}, fmt.Errorf("check login throttle: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	now := u.clock.Now().UTC()
	state, err := u.reader.LookupPasswordLogin(ctx, scope, normalizedAccount)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) {
			if err := u.password.VerifyUnknown(command.Password); err != nil {
				return PasswordLoginResult{}, fmt.Errorf("dummy password verification: %w", errors.Join(ErrAuthenticationDependency, err))
			}
			return u.recordAnonymousInvalidCredential(ctx, scope, attempt, command.IdempotencyKey, now)
		}
		return PasswordLoginResult{}, fmt.Errorf("lookup password login: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if state.LockedUntil.After(now) {
		if err := u.password.VerifyUnknown(command.Password); err != nil {
			return PasswordLoginResult{}, fmt.Errorf("dummy locked-password verification: %w", errors.Join(ErrAuthenticationDependency, err))
		}
		return u.recordAnonymousInvalidCredential(ctx, scope, attempt, command.IdempotencyKey, now)
	}
	valid, err := u.password.Verify(state.PasswordHash, command.Password)
	if err != nil {
		return PasswordLoginResult{}, fmt.Errorf("password verifier: %w", err)
	}
	if !valid {
		return u.recordKnownInvalidCredential(ctx, scope, attempt, state, command.IdempotencyKey, now)
	}
	if state.PrincipalStatus != PrincipalStatusActive {
		return PasswordLoginResult{}, ErrPrincipalInactive
	}
	if state.MembershipStatus != MembershipStatusActive {
		return PasswordLoginResult{}, ErrMembershipInactive
	}
	if state.TenantAccess != TenantAccessStatusActive {
		return PasswordLoginResult{}, ErrTenantAccessInactive
	}
	if !state.LifecycleFresh {
		return PasswordLoginResult{}, ErrTenantLifecycleStale
	}
	if state.Lifecycle != TenantLifecycleStatusActive {
		return PasswordLoginResult{}, ErrTenantLifecycleBlocked
	}
	ids, err := u.newIDs(6)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	accessExpiresAt, idleExpiresAt, absoluteExpiresAt := loginDeadlines(command.Audience, now)
	session := Session{
		ID:                ids[0],
		PrincipalID:       state.PrincipalID,
		Audience:          command.Audience,
		Status:            SessionStatusActive,
		AuthnMethods:      []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
		DeviceName:        strings.TrimSpace(command.DeviceName),
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiry:    absoluteExpiresAt,
		ReauthenticatedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	grant := SessionGrant{
		ID:           ids[1],
		SessionID:    session.ID,
		MembershipID: state.MembershipID,
		Status:       GrantStatusActive,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	family := RefreshTokenFamily{
		ID:        ids[2],
		GrantID:   grant.ID,
		Status:    GrantStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	refreshSecret, err := u.secrets.NewSecret()
	if err != nil {
		return PasswordLoginResult{}, fmt.Errorf("generate refresh secret: %w", err)
	}
	refresh := RefreshToken{
		ID:        ids[3],
		FamilyID:  family.ID,
		Digest:    sha256.Sum256([]byte(refreshSecret)),
		IssuedAt:  now,
		ExpiresAt: absoluteExpiresAt,
	}
	claims := AccessTokenClaims{
		Issuer:       "ani-iam",
		Subject:      state.PrincipalID,
		Audience:     command.Audience,
		TokenID:      ids[4],
		SessionID:    session.ID,
		GrantID:      grant.ID,
		GrantVersion: grant.Version,
		TenantID:     command.TenantID,
		IssuedAt:     now,
		ExpiresAt:    accessExpiresAt,
		AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword},
	}
	accessToken, err := u.tokens.Issue(ctx, claims)
	if err != nil {
		return PasswordLoginResult{}, fmt.Errorf("issue access token: %w", err)
	}
	audit := SecurityAuditEvent{
		ID:                   ids[5],
		ActorID:              state.PrincipalID,
		AuthenticationMethod: AuditAuthenticationMethodPassword,
		Boundary:             AuditBoundaryTenant,
		Action:               AuditActionPasswordLoginSucceeded,
		TargetType:           AuditTargetTypeSession,
		TargetID:             session.ID,
		TargetVersion:        grant.Version,
		Result:               AuditResultSucceeded,
		Reason:               AuditReasonPasswordLogin,
		RequestID:            command.IdempotencyKey,
		CorrelationID:        command.IdempotencyKey,
		DecisionID:           ids[5].String(),
		SourceService:        AuditSourceServiceIAM,
		OccurredAt:           now,
		RecordedAt:           now,
	}
	mutation := LoginMutation{
		NormalizedAccount: normalizedAccount,
		CredentialVersion: state.CredentialVersion,
		Session:           session,
		Grant:             grant,
		RefreshFamily:     family,
		RefreshToken:      refresh,
		Audit:             audit,
	}
	if err := u.uow.CommitLogin(ctx, scope, mutation); err != nil {
		return PasswordLoginResult{}, fmt.Errorf("commit password login: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	// PostgreSQL owns the durable login decision. Resetting the transient account
	// bucket only after that commit prevents a failed login transaction from
	// weakening abuse-control state. A reset outage leaves the old bucket in
	// place (the fail-closed direction) and must not turn the committed login
	// into an error that a caller could retry into a second Session.
	_ = u.throttle.Reset(ctx, attempt)
	return PasswordLoginResult{
		TenantID:             command.TenantID,
		Principal:            Principal{ID: state.PrincipalID, Status: state.PrincipalStatus},
		Session:              session,
		Grant:                grant,
		AccessToken:          accessToken,
		RefreshToken:         refreshSecret,
		AccessTokenExpiresAt: accessExpiresAt,
	}, nil
}

func (u *AuthenticationUsecase) recordInvalidCredential(ctx context.Context, attempt LoginThrottleAttempt) (PasswordLoginResult, error) {
	if err := u.throttle.RecordFailure(ctx, attempt); err != nil {
		return PasswordLoginResult{}, fmt.Errorf("record login throttle failure: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return PasswordLoginResult{}, ErrInvalidCredential
}

func (u *AuthenticationUsecase) recordKnownInvalidCredential(
	ctx context.Context,
	scope TenantScope,
	attempt LoginThrottleAttempt,
	state PasswordLoginState,
	requestID string,
	failedAt time.Time,
) (PasswordLoginResult, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	mutation := LoginFailureMutation{
		PrincipalID: state.PrincipalID,
		FailedAt:    failedAt,
		LockUntil:   failedAt.Add(15 * time.Minute),
		Audit: SecurityAuditEvent{
			ID:                   ids[0],
			ActorID:              uuid.Nil,
			AuthenticationMethod: AuditAuthenticationMethodAnonymous,
			Boundary:             AuditBoundaryTenant,
			Action:               AuditActionPasswordLoginFailed,
			TargetType:           AuditTargetTypePasswordCredential,
			TargetID:             state.PrincipalID,
			TargetVersion:        state.CredentialVersion + 1,
			Result:               AuditResultFailed,
			Reason:               AuditReasonInvalidCredential,
			RequestID:            requestID,
			CorrelationID:        requestID,
			DecisionID:           ids[0].String(),
			SourceService:        AuditSourceServiceIAM,
			OccurredAt:           failedAt,
			RecordedAt:           failedAt,
		},
	}
	if err := u.uow.RecordLoginFailure(ctx, scope, mutation); err != nil {
		return PasswordLoginResult{}, fmt.Errorf("record password login failure: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return u.recordInvalidCredential(ctx, attempt)
}

func (u *AuthenticationUsecase) recordAnonymousInvalidCredential(
	ctx context.Context,
	scope TenantScope,
	attempt LoginThrottleAttempt,
	requestID string,
	failedAt time.Time,
) (PasswordLoginResult, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	mutation := LoginFailureMutation{
		FailedAt:  failedAt,
		LockUntil: failedAt.Add(15 * time.Minute),
		Audit: SecurityAuditEvent{
			ID:                   ids[0],
			ActorID:              uuid.Nil,
			AuthenticationMethod: AuditAuthenticationMethodAnonymous,
			Boundary:             AuditBoundaryPrincipal,
			Action:               AuditActionPasswordLoginFailed,
			TargetType:           AuditTargetTypePasswordCredential,
			TargetID:             ids[0],
			TargetVersion:        1,
			Result:               AuditResultFailed,
			Reason:               AuditReasonInvalidCredential,
			RequestID:            requestID,
			CorrelationID:        requestID,
			DecisionID:           ids[0].String(),
			SourceService:        AuditSourceServiceIAM,
			OccurredAt:           failedAt,
			RecordedAt:           failedAt,
		},
	}
	if err := u.uow.RecordLoginFailure(ctx, scope, mutation); err != nil {
		return PasswordLoginResult{}, fmt.Errorf("record redacted password login failure: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return u.recordInvalidCredential(ctx, attempt)
}

func (u *AuthenticationUsecase) newIDs(count int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		id, err := u.ids.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate authentication ID: %w", err)
		}
		if id == uuid.Nil || id.Version() != 7 {
			return nil, ErrInvalidGeneratedID
		}
		ids[index] = id
	}
	return ids, nil
}

func loginDeadlines(audience Audience, now time.Time) (access, idle, absolute time.Time) {
	if audience == AudienceBoss {
		return now.Add(10 * time.Minute), now.Add(30 * time.Minute), now.Add(8 * time.Hour)
	}
	return now.Add(15 * time.Minute), now.Add(7 * 24 * time.Hour), now.Add(30 * 24 * time.Hour)
}
