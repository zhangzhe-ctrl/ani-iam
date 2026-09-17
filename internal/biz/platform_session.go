package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlatformSessionState never contains a Tenant identifier or TenantScope.
type PlatformSessionState struct {
	Principal        Principal
	MembershipID     uuid.UUID
	MembershipStatus MembershipStatus
	Session          Session
	Grant            SessionGrant
	Family           RefreshTokenFamily
	Token            RefreshToken
}

type PlatformSessionTransaction interface {
	Rotate(context.Context, PlatformSessionState, RefreshToken, time.Time, time.Time) (int64, error)
	RevokeFamily(context.Context, PlatformSessionState, time.Time) error
	Logout(context.Context, PlatformSessionState, time.Time) error
	AppendAudit(context.Context, SecurityAuditEvent) error
}
type PlatformSessionUnitOfWork interface {
	WithinPlatformSession(context.Context, [sha256.Size]byte, func(PlatformSessionTransaction, PlatformSessionState, bool) error) error
}

type PlatformSessionUsecase struct {
	origin, issuer string
	uow            PlatformSessionUnitOfWork
	throttle       LoginThrottle
	tokens         AccessTokenIssuer
	secrets        SecretGenerator
	ids            IDGenerator
	clock          Clock
}

func NewPlatformSessionUsecase(origin, issuer string, uow PlatformSessionUnitOfWork, throttle LoginThrottle, tokens AccessTokenIssuer, secrets SecretGenerator, ids IDGenerator, clock Clock) (*PlatformSessionUsecase, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || issuer == "" || uow == nil || throttle == nil || tokens == nil || secrets == nil || ids == nil || clock == nil {
		return nil, ErrOIDCConfigurationInvalid
	}
	return &PlatformSessionUsecase{origin: origin, issuer: issuer, uow: uow, throttle: throttle, tokens: tokens, secrets: secrets, ids: ids, clock: clock}, nil
}

func (u *PlatformSessionUsecase) RefreshSession(ctx context.Context, c RefreshSessionCommand) (RefreshSessionResult, error) {
	if err := u.validateCommand(c.RefreshToken, c.CSRFToken, c.Origin, c.IdempotencyKey); err != nil {
		return RefreshSessionResult{}, err
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(c.RefreshToken)))
	if err := u.throttle.CheckRefresh(ctx, RefreshThrottleAttempt{Digest: digest}); err != nil {
		if errors.Is(err, ErrAuthenticationRateLimited) {
			return RefreshSessionResult{}, err
		}
		return RefreshSessionResult{}, ErrAuthenticationDependency
	}
	var result RefreshSessionResult
	var reused bool
	err := u.uow.WithinPlatformSession(ctx, digest, func(tx PlatformSessionTransaction, s PlatformSessionState, found bool) error {
		if !found || !validPlatformSessionRelationships(s, digest) {
			return ErrInvalidCredential
		}
		now := u.clock.Now().UTC()
		if s.Token.Status == RefreshTokenStatusConsumed {
			reused = true
			if s.Family.Status != GrantStatusActive {
				return nil
			}
			if err := tx.RevokeFamily(ctx, s, now); err != nil {
				return err
			}
			return u.appendAudit(ctx, tx, s, AuditActionRefreshTokenReused, AuditReasonRefreshTokenReuse, s.Grant.Version+1, c.IdempotencyKey, now)
		}
		if err := validatePlatformRefresh(s, now); err != nil {
			return err
		}
		refreshID, err := u.ids.NewID()
		if err != nil {
			return ErrAuthenticationDependency
		}
		accessID, err := u.ids.NewID()
		if err != nil || refreshID.Version() != 7 || accessID.Version() != 7 || refreshID == accessID {
			return ErrAuthenticationDependency
		}
		secret, err := u.secrets.NewSecret()
		if err != nil || len(secret) < 32 {
			return ErrAuthenticationDependency
		}
		replacement := RefreshToken{ID: refreshID, FamilyID: s.Family.ID, Digest: sha256.Sum256([]byte(secret)), Status: RefreshTokenStatusActive, IssuedAt: now, ExpiresAt: s.Session.AbsoluteExpiry}
		idle := sessionIdleDeadline(AudienceBoss, now, s.Session.AbsoluteExpiry)
		expiry := sessionAccessDeadline(AudienceBoss, now, idle, s.Session.AbsoluteExpiry)
		access, err := u.tokens.Issue(ctx, AccessTokenClaims{Boundary: AccessBoundaryPlatform, Issuer: u.issuer, Subject: s.Principal.ID, Audience: AudienceBoss, TokenID: accessID, SessionID: s.Session.ID, GrantID: s.Grant.ID, GrantVersion: s.Grant.Version, IssuedAt: now, ExpiresAt: expiry, AuthnMethods: append([]AuditAuthenticationMethod(nil), s.Session.AuthnMethods...)})
		if err != nil {
			return ErrAuthenticationDependency
		}
		version, err := tx.Rotate(ctx, s, replacement, idle, now)
		if err != nil {
			return err
		}
		if version != s.Session.Version+1 {
			return ErrInvalidPersistenceState
		}
		if err = u.appendAudit(ctx, tx, s, AuditActionSessionRefreshed, AuditReasonSessionRefresh, s.Grant.Version, c.IdempotencyKey, now); err != nil {
			return err
		}
		s.Session.IdleExpiresAt = idle
		s.Session.UpdatedAt = now
		s.Session.Version = version
		result = RefreshSessionResult{Boundary: AccessBoundaryPlatform, Principal: s.Principal, Session: s.Session, Grant: s.Grant, AccessToken: access, RefreshToken: secret, AccessTokenExpiresAt: expiry}
		return nil
	})
	if err != nil {
		return RefreshSessionResult{}, err
	}
	if reused {
		return RefreshSessionResult{}, ErrInvalidCredential
	}
	return result, nil
}

func (u *PlatformSessionUsecase) LogoutSession(ctx context.Context, c LogoutSessionCommand) (LogoutSessionResult, error) {
	if err := u.validateCommand(c.RefreshToken, c.CSRFToken, c.Origin, c.IdempotencyKey); err != nil {
		return LogoutSessionResult{}, err
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(c.RefreshToken)))
	err := u.uow.WithinPlatformSession(ctx, digest, func(tx PlatformSessionTransaction, s PlatformSessionState, found bool) error {
		if !found {
			return nil
		}
		if !validPlatformSessionRelationships(s, digest) {
			return ErrInvalidCredential
		}
		if s.Session.Status != SessionStatusActive {
			return nil
		}
		now := u.clock.Now().UTC()
		if err := tx.Logout(ctx, s, now); err != nil {
			return err
		}
		return u.appendAudit(ctx, tx, s, AuditActionSessionLoggedOut, AuditReasonSessionLogout, s.Session.Version+1, c.IdempotencyKey, now)
	})
	return LogoutSessionResult{}, err
}

func (u *PlatformSessionUsecase) validateCommand(refresh, csrf, origin, key string) error {
	if strings.TrimSpace(refresh) == "" {
		return ErrRefreshTokenRequired
	}
	if strings.TrimSpace(csrf) == "" {
		return ErrCSRFTokenRequired
	}
	if origin != u.origin {
		return ErrInvalidCredential
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 {
		return ErrIdempotencyKeyRequired
	}
	return nil
}
func validPlatformSessionRelationships(s PlatformSessionState, digest [sha256.Size]byte) bool {
	return s.Principal.ID != uuid.Nil && s.MembershipID != uuid.Nil && s.Grant.MembershipID == s.MembershipID && s.Session.ID != uuid.Nil && s.Session.PrincipalID == s.Principal.ID && s.Session.Audience == AudienceBoss && len(s.Session.AuthnMethods) > 0 && s.Grant.ID != uuid.Nil && s.Grant.SessionID == s.Session.ID && s.Grant.Version > 0 && s.Family.ID != uuid.Nil && s.Family.GrantID == s.Grant.ID && s.Token.ID != uuid.Nil && s.Token.FamilyID == s.Family.ID && s.Token.Digest == digest
}
func validatePlatformRefresh(s PlatformSessionState, now time.Time) error {
	if s.Token.Status != RefreshTokenStatusActive || s.Family.Status != GrantStatusActive || s.Grant.Status != GrantStatusActive || s.Session.Status != SessionStatusActive || !now.Before(s.Token.ExpiresAt) || !now.Before(s.Session.IdleExpiresAt) || !now.Before(s.Session.AbsoluteExpiry) {
		return ErrInvalidCredential
	}
	if s.Principal.Status != PrincipalStatusActive {
		return ErrPrincipalInactive
	}
	if s.MembershipStatus != MembershipStatusActive {
		return ErrMembershipInactive
	}
	return nil
}
func (u *PlatformSessionUsecase) appendAudit(ctx context.Context, tx PlatformSessionTransaction, s PlatformSessionState, action AuditAction, reason AuditReason, version int64, key string, now time.Time) error {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return ErrAuthenticationDependency
	}
	targetType, targetID := AuditTargetTypeSessionGrant, s.Grant.ID
	if action == AuditActionSessionLoggedOut {
		targetType, targetID = AuditTargetTypeSession, s.Session.ID
	}
	return tx.AppendAudit(ctx, SecurityAuditEvent{ID: id, ActorID: s.Principal.ID, AuthenticationMethod: s.Session.AuthnMethods[0], Boundary: AuditBoundaryPlatform, Action: action, TargetType: targetType, TargetID: targetID, TargetVersion: version, Result: AuditResultSucceeded, Reason: reason, RequestID: key, CorrelationID: key, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now})
}
