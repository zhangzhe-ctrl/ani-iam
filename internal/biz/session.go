package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	AuditActionSessionRefreshed   AuditAction     = "iam.session.refreshed"
	AuditActionRefreshTokenReused AuditAction     = "iam.refresh_token.reused"
	AuditActionSessionLoggedOut   AuditAction     = "iam.session.logged_out"
	AuditActionTenantSwitched     AuditAction     = "iam.session.tenant_switched"
	AuditTargetTypeSessionGrant   AuditTargetType = "session_grant"
	AuditReasonSessionRefresh     AuditReason     = "SESSION_REFRESH"
	AuditReasonRefreshTokenReuse  AuditReason     = "REFRESH_TOKEN_REUSE"
	AuditReasonSessionLogout      AuditReason     = "SESSION_LOGOUT"
	AuditReasonTenantSwitch       AuditReason     = "TENANT_SWITCH"
)

var (
	ErrRefreshTokenRequired = errors.New("refresh token is required")
	ErrCSRFTokenRequired    = errors.New("CSRF token is required")
	ErrOriginRequired       = errors.New("origin is required")
)

// RefreshThrottleAttempt contains only a one-way digest. The raw refresh
// credential never becomes a Redis key or value.
type RefreshThrottleAttempt struct {
	Digest [sha256.Size]byte
}

type RefreshSessionState struct {
	TenantID         uuid.UUID
	Principal        Principal
	MembershipStatus MembershipStatus
	TenantAccess     TenantAccessStatus
	Lifecycle        TenantLifecycleStatus
	LifecycleFresh   bool
	Session          Session
	Grant            SessionGrant
	Family           RefreshTokenFamily
	Token            RefreshToken
}

type RefreshSessionCommand struct {
	RefreshToken   string
	CSRFToken      string
	Origin         string
	IdempotencyKey string
}

type RefreshSessionResult = LoginResult

// RefreshSessionMutation carries both possible Audit events because the
// PostgreSQL adapter must resolve an active-vs-consumed race while holding the
// credential row lock. Exactly one event may be persisted.
type RefreshSessionMutation struct {
	Expected         RefreshSessionState
	ReplacementToken RefreshToken
	NewIdleExpiresAt time.Time
	RotatedAt        time.Time
	Audit            SecurityAuditEvent
	ReuseAudit       SecurityAuditEvent
}

type RefreshSessionMutationResult struct {
	Reused         bool
	SessionVersion int64
}

type RefreshReuseMutation struct {
	Expected RefreshSessionState
	ReusedAt time.Time
	Audit    SecurityAuditEvent
}

type LogoutSessionMutation struct {
	RefreshDigest [sha256.Size]byte
	Expected      LogoutSessionState
	LoggedOutAt   time.Time
	Audit         SecurityAuditEvent
}

type LogoutSessionMutationResult struct {
	Changed bool
}

type LogoutSessionCommand struct {
	RefreshToken   string
	CSRFToken      string
	Origin         string
	IdempotencyKey string
}

type LogoutSessionResult struct{}

type LogoutSessionState struct {
	Principal Principal
	Session   Session
}

type TenantSwitchState struct {
	SourceTenantID   uuid.UUID
	TargetTenantID   uuid.UUID
	Principal        Principal
	MembershipID     uuid.UUID
	MembershipStatus MembershipStatus
	TenantAccess     TenantAccessStatus
	Lifecycle        TenantLifecycleStatus
	LifecycleFresh   bool
	Session          Session
	SourceGrant      SessionGrant
	TargetGrant      *SessionGrant
	TargetFamily     *RefreshTokenFamily
}

type TenantSwitchMutation struct {
	Expected         TenantSwitchState
	Grant            SessionGrant
	Family           RefreshTokenFamily
	RefreshToken     RefreshToken
	NewIdleExpiresAt time.Time
	SwitchedAt       time.Time
	Audit            SecurityAuditEvent
}

type SwitchTenantCommand struct {
	RawCredential  string
	TargetTenantID uuid.UUID
	IdempotencyKey string
}

type SwitchTenantResult = LoginResult

type SessionContinuityReader interface {
	LookupRefreshSession(context.Context, [sha256.Size]byte) (RefreshSessionState, error)
	LookupLogoutSession(context.Context, [sha256.Size]byte) (LogoutSessionState, bool, error)
	LookupTenantSwitch(context.Context, TenantScope, AccessTokenClaims) (TenantSwitchState, error)
}

type SessionContinuityUnitOfWork interface {
	RotateRefreshSession(context.Context, RefreshSessionMutation) (RefreshSessionMutationResult, error)
	RevokeRefreshReuse(context.Context, RefreshReuseMutation) (bool, error)
	LogoutSession(context.Context, LogoutSessionMutation) (LogoutSessionMutationResult, error)
	SwitchTenant(context.Context, TenantScope, TenantSwitchMutation) error
}

func (u *AuthenticationUsecase) RefreshSession(ctx context.Context, command RefreshSessionCommand) (RefreshSessionResult, error) {
	rawRefresh := strings.TrimSpace(command.RefreshToken)
	if rawRefresh == "" {
		return RefreshSessionResult{}, ErrRefreshTokenRequired
	}
	if strings.TrimSpace(command.CSRFToken) == "" {
		return RefreshSessionResult{}, ErrCSRFTokenRequired
	}
	if strings.TrimSpace(command.Origin) == "" {
		return RefreshSessionResult{}, ErrOriginRequired
	}
	requestID := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" {
		return RefreshSessionResult{}, ErrIdempotencyKeyRequired
	}

	digest := sha256.Sum256([]byte(rawRefresh))
	if err := u.throttle.CheckRefresh(ctx, RefreshThrottleAttempt{Digest: digest}); err != nil {
		if errors.Is(err, ErrAuthenticationRateLimited) {
			return RefreshSessionResult{}, err
		}
		return RefreshSessionResult{}, fmt.Errorf("check refresh throttle: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	state, err := u.reader.LookupRefreshSession(ctx, digest)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) {
			return RefreshSessionResult{}, ErrInvalidCredential
		}
		return RefreshSessionResult{}, fmt.Errorf("lookup refresh session: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	now := u.clock.Now().UTC()
	if !validRefreshRelationships(state, digest) {
		return RefreshSessionResult{}, ErrInvalidCredential
	}
	// This repository shape represents a Tenant Grant. BOSS requires a
	// Platform Grant, which is deliberately unavailable in DP2-08. Reject the
	// audience before reuse handling so malformed persisted state cannot trigger
	// a Tenant-boundary revocation or Audit under BOSS authority.
	if state.Session.Audience != AudienceConsole {
		return RefreshSessionResult{}, ErrInvalidCredential
	}
	if state.Token.Status == RefreshTokenStatusConsumed {
		ids, idErr := u.newIDs(1)
		if idErr != nil {
			return RefreshSessionResult{}, idErr
		}
		_, revokeErr := u.uow.RevokeRefreshReuse(ctx, RefreshReuseMutation{
			Expected: state,
			ReusedAt: now,
			Audit:    refreshAudit(ids[0], state, AuditActionRefreshTokenReused, AuditReasonRefreshTokenReuse, state.Grant.Version+1, requestID, now),
		})
		if revokeErr != nil {
			return RefreshSessionResult{}, fmt.Errorf("revoke reused refresh token: %w", errors.Join(ErrAuthenticationDependency, revokeErr))
		}
		return RefreshSessionResult{}, ErrInvalidCredential
	}
	if err := validateActiveRefreshState(state, now); err != nil {
		return RefreshSessionResult{}, err
	}

	ids, err := u.newIDs(4)
	if err != nil {
		return RefreshSessionResult{}, err
	}
	replacementSecret, err := u.secrets.NewSecret()
	if err != nil {
		return RefreshSessionResult{}, fmt.Errorf("generate replacement refresh secret: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	newIdleExpiresAt := sessionIdleDeadline(state.Session.Audience, now, state.Session.AbsoluteExpiry)
	accessExpiresAt := sessionAccessDeadline(state.Session.Audience, now, newIdleExpiresAt, state.Session.AbsoluteExpiry)
	replacement := RefreshToken{
		ID:        ids[0],
		FamilyID:  state.Family.ID,
		Digest:    sha256.Sum256([]byte(replacementSecret)),
		Status:    RefreshTokenStatusActive,
		IssuedAt:  now,
		ExpiresAt: state.Session.AbsoluteExpiry.UTC(),
	}
	claims := AccessTokenClaims{
		Issuer:       "ani-iam",
		Subject:      state.Principal.ID,
		Audience:     state.Session.Audience,
		TokenID:      ids[1],
		SessionID:    state.Session.ID,
		GrantID:      state.Grant.ID,
		GrantVersion: state.Grant.Version,
		TenantID:     state.TenantID,
		IssuedAt:     now,
		ExpiresAt:    accessExpiresAt,
		AuthnMethods: append([]AuditAuthenticationMethod(nil), state.Session.AuthnMethods...),
	}
	accessToken, err := u.tokens.Issue(ctx, claims)
	if err != nil {
		return RefreshSessionResult{}, fmt.Errorf("issue refreshed access token: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	mutation := RefreshSessionMutation{
		Expected:         state,
		ReplacementToken: replacement,
		NewIdleExpiresAt: newIdleExpiresAt,
		RotatedAt:        now,
		Audit:            refreshAudit(ids[2], state, AuditActionSessionRefreshed, AuditReasonSessionRefresh, state.Grant.Version, requestID, now),
		ReuseAudit:       refreshAudit(ids[3], state, AuditActionRefreshTokenReused, AuditReasonRefreshTokenReuse, state.Grant.Version+1, requestID, now),
	}
	mutationResult, err := u.uow.RotateRefreshSession(ctx, mutation)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrPrincipalInactive) || errors.Is(err, ErrMembershipInactive) ||
			errors.Is(err, ErrTenantAccessInactive) || errors.Is(err, ErrTenantLifecycleBlocked) || errors.Is(err, ErrTenantLifecycleStale) {
			return RefreshSessionResult{}, err
		}
		return RefreshSessionResult{}, fmt.Errorf("rotate refresh token: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if mutationResult.Reused {
		return RefreshSessionResult{}, ErrInvalidCredential
	}
	if mutationResult.SessionVersion <= state.Session.Version {
		return RefreshSessionResult{}, fmt.Errorf("rotate refresh token: %w", errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState))
	}
	state.Session.IdleExpiresAt = newIdleExpiresAt
	state.Session.UpdatedAt = now
	state.Session.Version = mutationResult.SessionVersion
	return RefreshSessionResult{
		TenantID:             state.TenantID,
		Principal:            state.Principal,
		Session:              state.Session,
		Grant:                state.Grant,
		AccessToken:          accessToken,
		RefreshToken:         replacementSecret,
		AccessTokenExpiresAt: accessExpiresAt,
	}, nil
}

func (u *AuthenticationUsecase) LogoutSession(ctx context.Context, command LogoutSessionCommand) (LogoutSessionResult, error) {
	rawRefresh := strings.TrimSpace(command.RefreshToken)
	if rawRefresh == "" {
		return LogoutSessionResult{}, ErrRefreshTokenRequired
	}
	if strings.TrimSpace(command.CSRFToken) == "" {
		return LogoutSessionResult{}, ErrCSRFTokenRequired
	}
	if strings.TrimSpace(command.Origin) == "" {
		return LogoutSessionResult{}, ErrOriginRequired
	}
	requestID := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" {
		return LogoutSessionResult{}, ErrIdempotencyKeyRequired
	}
	digest := sha256.Sum256([]byte(rawRefresh))
	state, found, err := u.reader.LookupLogoutSession(ctx, digest)
	if err != nil {
		return LogoutSessionResult{}, fmt.Errorf("lookup logout session: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if !found {
		return LogoutSessionResult{}, nil
	}
	if state.Principal.ID == uuid.Nil || state.Session.ID == uuid.Nil || state.Session.PrincipalID != state.Principal.ID {
		return LogoutSessionResult{}, fmt.Errorf("lookup logout session: %w", errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState))
	}
	if state.Session.Status != SessionStatusActive {
		return LogoutSessionResult{}, nil
	}
	ids, err := u.newIDs(1)
	if err != nil {
		return LogoutSessionResult{}, err
	}
	method := AuditAuthenticationMethodInternal
	if len(state.Session.AuthnMethods) > 0 {
		method = state.Session.AuthnMethods[0]
	}
	now := u.clock.Now().UTC()
	audit := SecurityAuditEvent{
		ID: ids[0], ActorID: state.Principal.ID, AuthenticationMethod: method,
		Boundary: AuditBoundaryPrincipal, Action: AuditActionSessionLoggedOut,
		TargetType: AuditTargetTypeSession, TargetID: state.Session.ID,
		TargetVersion: state.Session.Version + 1, Result: AuditResultSucceeded,
		Reason: AuditReasonSessionLogout, RequestID: requestID, CorrelationID: requestID,
		DecisionID: ids[0].String(), SourceService: AuditSourceServiceIAM,
		OccurredAt: now, RecordedAt: now,
	}
	_, err = u.uow.LogoutSession(ctx, LogoutSessionMutation{
		RefreshDigest: digest,
		Expected:      state,
		LoggedOutAt:   audit.OccurredAt,
		Audit:         audit,
	})
	if err != nil {
		return LogoutSessionResult{}, fmt.Errorf("logout current session: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return LogoutSessionResult{}, nil
}

func (u *AuthenticationUsecase) SwitchTenant(ctx context.Context, command SwitchTenantCommand) (SwitchTenantResult, error) {
	rawCredential := strings.TrimSpace(command.RawCredential)
	if rawCredential == "" {
		return SwitchTenantResult{}, ErrAuthorizationCredentialRequired
	}
	requestID := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" {
		return SwitchTenantResult{}, ErrIdempotencyKeyRequired
	}
	scope, err := NewTenantScope(command.TargetTenantID)
	if err != nil {
		return SwitchTenantResult{}, err
	}
	claims, err := u.tokens.Verify(ctx, rawCredential)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return SwitchTenantResult{}, err
		}
		return SwitchTenantResult{}, errors.Join(ErrInvalidCredential, err)
	}
	now := u.clock.Now().UTC()
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil ||
		claims.TenantID == uuid.Nil || claims.GrantVersion <= 0 || !now.Before(claims.ExpiresAt.UTC()) {
		return SwitchTenantResult{}, ErrInvalidCredential
	}
	state, err := u.reader.LookupTenantSwitch(ctx, scope, claims)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrPrincipalInactive) || errors.Is(err, ErrMembershipInactive) ||
			errors.Is(err, ErrTenantAccessInactive) || errors.Is(err, ErrTenantLifecycleBlocked) || errors.Is(err, ErrTenantLifecycleStale) {
			return SwitchTenantResult{}, err
		}
		return SwitchTenantResult{}, fmt.Errorf("lookup tenant switch: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if err := validateTenantSwitchState(state, claims, command.TargetTenantID, now); err != nil {
		return SwitchTenantResult{}, err
	}

	var newGrant SessionGrant
	var newFamily RefreshTokenFamily
	var tokenID, accessTokenID, auditID uuid.UUID
	if state.TargetGrant == nil {
		ids, idErr := u.newIDs(5)
		if idErr != nil {
			return SwitchTenantResult{}, idErr
		}
		newGrant = SessionGrant{
			ID: ids[0], SessionID: state.Session.ID, MembershipID: state.MembershipID,
			Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		newFamily = RefreshTokenFamily{
			ID: ids[1], GrantID: newGrant.ID, Status: GrantStatusActive,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		tokenID, accessTokenID, auditID = ids[2], ids[3], ids[4]
	} else if state.TargetFamily == nil {
		ids, idErr := u.newIDs(4)
		if idErr != nil {
			return SwitchTenantResult{}, idErr
		}
		newGrant = *state.TargetGrant
		newFamily = RefreshTokenFamily{
			ID: ids[0], GrantID: newGrant.ID, Status: GrantStatusActive,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		tokenID, accessTokenID, auditID = ids[1], ids[2], ids[3]
	} else {
		ids, idErr := u.newIDs(3)
		if idErr != nil {
			return SwitchTenantResult{}, idErr
		}
		newGrant = *state.TargetGrant
		newGrant.Version++
		newGrant.UpdatedAt = now
		newFamily = *state.TargetFamily
		newFamily.UpdatedAt = now
		tokenID, accessTokenID, auditID = ids[0], ids[1], ids[2]
	}
	refreshSecret, err := u.secrets.NewSecret()
	if err != nil {
		return SwitchTenantResult{}, fmt.Errorf("generate tenant-switch refresh secret: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	newIdleExpiresAt := sessionIdleDeadline(state.Session.Audience, now, state.Session.AbsoluteExpiry)
	accessExpiresAt := sessionAccessDeadline(state.Session.Audience, now, newIdleExpiresAt, state.Session.AbsoluteExpiry)
	replacement := RefreshToken{
		ID: tokenID, FamilyID: newFamily.ID, Digest: sha256.Sum256([]byte(refreshSecret)),
		Status: RefreshTokenStatusActive, IssuedAt: now, ExpiresAt: state.Session.AbsoluteExpiry.UTC(),
	}
	accessClaims := AccessTokenClaims{
		Issuer: "ani-iam", Subject: state.Principal.ID, Audience: state.Session.Audience,
		TokenID: accessTokenID, SessionID: state.Session.ID, GrantID: newGrant.ID,
		GrantVersion: newGrant.Version, TenantID: command.TargetTenantID,
		IssuedAt: now, ExpiresAt: accessExpiresAt,
		AuthnMethods: append([]AuditAuthenticationMethod(nil), state.Session.AuthnMethods...),
	}
	accessToken, err := u.tokens.Issue(ctx, accessClaims)
	if err != nil {
		return SwitchTenantResult{}, fmt.Errorf("issue tenant-switch access token: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	method := AuditAuthenticationMethodInternal
	if len(state.Session.AuthnMethods) > 0 {
		method = state.Session.AuthnMethods[0]
	}
	audit := SecurityAuditEvent{
		ID: auditID, ActorID: state.Principal.ID, AuthenticationMethod: method,
		Boundary: AuditBoundaryTenant, Action: AuditActionTenantSwitched,
		TargetType: AuditTargetTypeSessionGrant, TargetID: newGrant.ID, TargetVersion: newGrant.Version,
		Result: AuditResultSucceeded, Reason: AuditReasonTenantSwitch,
		RequestID: requestID, CorrelationID: requestID, DecisionID: auditID.String(),
		SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
	if err := u.uow.SwitchTenant(ctx, scope, TenantSwitchMutation{
		Expected: state, Grant: newGrant, Family: newFamily, RefreshToken: replacement,
		NewIdleExpiresAt: newIdleExpiresAt, SwitchedAt: now, Audit: audit,
	}); err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrPrincipalInactive) || errors.Is(err, ErrMembershipInactive) ||
			errors.Is(err, ErrTenantAccessInactive) || errors.Is(err, ErrTenantLifecycleBlocked) || errors.Is(err, ErrTenantLifecycleStale) {
			return SwitchTenantResult{}, err
		}
		return SwitchTenantResult{}, fmt.Errorf("commit tenant switch: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	state.Session.IdleExpiresAt = newIdleExpiresAt
	state.Session.UpdatedAt = now
	state.Session.Version++
	return SwitchTenantResult{
		TenantID: command.TargetTenantID, Principal: state.Principal, Session: state.Session,
		Grant: newGrant, AccessToken: accessToken, RefreshToken: refreshSecret,
		AccessTokenExpiresAt: accessExpiresAt,
	}, nil
}

func validateTenantSwitchState(state TenantSwitchState, claims AccessTokenClaims, targetTenantID uuid.UUID, now time.Time) error {
	if state.SourceTenantID != claims.TenantID || state.TargetTenantID != targetTenantID ||
		state.Principal.ID != claims.Subject || state.Session.ID != claims.SessionID ||
		state.Session.PrincipalID != claims.Subject || state.SourceGrant.ID != claims.GrantID ||
		state.SourceGrant.SessionID != state.Session.ID || state.SourceGrant.Version != claims.GrantVersion ||
		state.MembershipID == uuid.Nil || state.Session.Audience != AudienceConsole || claims.Audience != AudienceConsole ||
		len(state.Session.AuthnMethods) == 0 {
		return ErrInvalidCredential
	}
	if state.Session.Status != SessionStatusActive || state.SourceGrant.Status != GrantStatusActive ||
		!now.Before(state.Session.IdleExpiresAt.UTC()) || !now.Before(state.Session.AbsoluteExpiry.UTC()) {
		return ErrInvalidCredential
	}
	if state.Principal.Status != PrincipalStatusActive {
		return ErrPrincipalInactive
	}
	if state.MembershipStatus != MembershipStatusActive {
		return ErrMembershipInactive
	}
	if state.TenantAccess != TenantAccessStatusActive {
		return ErrTenantAccessInactive
	}
	if !state.LifecycleFresh {
		return ErrTenantLifecycleStale
	}
	if state.Lifecycle != TenantLifecycleStatusActive {
		return ErrTenantLifecycleBlocked
	}
	if state.TargetGrant == nil && state.TargetFamily != nil {
		return errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState)
	}
	if state.TargetGrant != nil && (state.TargetGrant.ID == uuid.Nil || state.TargetGrant.SessionID != state.Session.ID ||
		state.TargetGrant.MembershipID != state.MembershipID || state.TargetGrant.Status != GrantStatusActive || state.TargetGrant.Version <= 0) {
		return errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState)
	}
	if state.TargetFamily != nil && (state.TargetFamily.ID == uuid.Nil ||
		state.TargetFamily.GrantID != state.TargetGrant.ID || state.TargetFamily.Status != GrantStatusActive) {
		return errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState)
	}
	return nil
}

func validRefreshRelationships(state RefreshSessionState, digest [sha256.Size]byte) bool {
	return state.TenantID != uuid.Nil && state.Principal.ID != uuid.Nil &&
		state.Session.ID != uuid.Nil && state.Session.PrincipalID == state.Principal.ID &&
		state.Grant.ID != uuid.Nil && state.Grant.SessionID == state.Session.ID && state.Grant.Version > 0 &&
		state.Family.ID != uuid.Nil && state.Family.GrantID == state.Grant.ID &&
		state.Token.ID != uuid.Nil && state.Token.FamilyID == state.Family.ID && state.Token.Digest == digest
}

func validateActiveRefreshState(state RefreshSessionState, now time.Time) error {
	if state.Token.Status != RefreshTokenStatusActive || state.Family.Status != GrantStatusActive || state.Grant.Status != GrantStatusActive ||
		state.Session.Status != SessionStatusActive || !now.Before(state.Token.ExpiresAt.UTC()) ||
		!now.Before(state.Session.IdleExpiresAt.UTC()) || !now.Before(state.Session.AbsoluteExpiry.UTC()) {
		return ErrInvalidCredential
	}
	if state.Principal.Status != PrincipalStatusActive {
		return ErrPrincipalInactive
	}
	if state.MembershipStatus != MembershipStatusActive {
		return ErrMembershipInactive
	}
	if state.TenantAccess != TenantAccessStatusActive {
		return ErrTenantAccessInactive
	}
	if !state.LifecycleFresh {
		return ErrTenantLifecycleStale
	}
	if state.Lifecycle != TenantLifecycleStatusActive {
		return ErrTenantLifecycleBlocked
	}
	if state.Session.Audience != AudienceConsole || len(state.Session.AuthnMethods) == 0 {
		return ErrInvalidCredential
	}
	return nil
}

func refreshAudit(id uuid.UUID, state RefreshSessionState, action AuditAction, reason AuditReason, version int64, requestID string, now time.Time) SecurityAuditEvent {
	method := AuditAuthenticationMethodInternal
	if len(state.Session.AuthnMethods) > 0 {
		method = state.Session.AuthnMethods[0]
	}
	return SecurityAuditEvent{
		ID: id, ActorID: state.Principal.ID, AuthenticationMethod: method,
		Boundary: AuditBoundaryTenant, Action: action, TargetType: AuditTargetTypeSessionGrant,
		TargetID: state.Grant.ID, TargetVersion: version, Result: AuditResultSucceeded,
		Reason: reason, RequestID: requestID, CorrelationID: requestID, DecisionID: id.String(),
		SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}

func sessionIdleDeadline(audience Audience, now, absolute time.Time) time.Time {
	_, idleLifetime, _ := sessionLifetimePolicy(audience)
	idle := now.Add(idleLifetime)
	return earliestTime(idle, absolute.UTC())
}

func sessionAccessDeadline(audience Audience, now, idle, absolute time.Time) time.Time {
	accessLifetime, _, _ := sessionLifetimePolicy(audience)
	access := now.Add(accessLifetime)
	return earliestTime(access, earliestTime(idle.UTC(), absolute.UTC()))
}

func earliestTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
