package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlatformPasswordState carries only global Human and Platform identity state.
type PlatformPasswordState struct {
	Login             PlatformLoginState
	PasswordHash      string
	CredentialVersion int64
	LockedUntil       time.Time
}
type PlatformPasswordReader interface {
	ReadPlatformPassword(context.Context, string) (PlatformPasswordState, error)
}
type PlatformPasswordTransaction interface {
	LockPassword(context.Context, string) (PlatformPasswordState, error)
	ResetPasswordFailures(context.Context, uuid.UUID, int64, time.Time) error
	RecordPasswordFailure(context.Context, LoginFailureMutation) error
	SaveLogin(context.Context, PlatformLoginMutation) error
}
type PlatformPasswordUnitOfWork interface {
	WithinPlatformPassword(context.Context, func(PlatformPasswordTransaction) error) error
}
type PlatformPasswordUsecase struct {
	issuer   string
	reader   PlatformPasswordReader
	uow      PlatformPasswordUnitOfWork
	password PasswordVerifier
	throttle LoginThrottle
	tokens   AccessTokenIssuer
	secrets  SecretGenerator
	ids      IDGenerator
	clock    Clock
}

func NewPlatformPasswordUsecase(issuer string, reader PlatformPasswordReader, uow PlatformPasswordUnitOfWork, password PasswordVerifier, throttle LoginThrottle, tokens AccessTokenIssuer, secrets SecretGenerator, ids IDGenerator, clock Clock) (*PlatformPasswordUsecase, error) {
	if issuer == "" || reader == nil || uow == nil || password == nil || throttle == nil || tokens == nil || secrets == nil || ids == nil || clock == nil {
		return nil, ErrAuthenticationDependency
	}
	return &PlatformPasswordUsecase{issuer, reader, uow, password, throttle, tokens, secrets, ids, clock}, nil
}
func (u *PlatformPasswordUsecase) PasswordLogin(ctx context.Context, c PasswordLoginCommand) (LoginResult, error) {
	if c.Audience != AudienceBoss || c.TenantID != uuid.Nil {
		return LoginResult{}, ErrInvalidCredential
	}
	account := strings.ToLower(strings.TrimSpace(c.Account))
	if account == "" {
		return LoginResult{}, ErrAccountRequired
	}
	if c.Password == "" {
		return LoginResult{}, ErrPasswordRequired
	}
	if !c.SourceIP.IsValid() {
		return LoginResult{}, ErrSourceIPRequired
	}
	if c.IdempotencyKey == "" || len(c.IdempotencyKey) > 128 {
		return LoginResult{}, ErrIdempotencyKeyRequired
	}
	if len(c.DeviceName) > 128 {
		return LoginResult{}, ErrInvalidCredential
	}
	attempt := LoginThrottleAttempt{NormalizedAccount: account, SourceIP: c.SourceIP.Unmap()}
	if err := u.throttle.Check(ctx, attempt); err != nil {
		if errors.Is(err, ErrAuthenticationRateLimited) {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrAuthenticationDependency
	}
	state, err := u.reader.ReadPlatformPassword(ctx, account)
	if err != nil && !errors.Is(err, ErrInvalidCredential) {
		return LoginResult{}, ErrAuthenticationDependency
	}
	now := u.clock.Now().UTC()
	known := err == nil && !state.LockedUntil.After(now)
	if !known {
		if u.password.VerifyUnknown(c.Password) != nil {
			return LoginResult{}, ErrAuthenticationDependency
		}
		return u.failure(ctx, attempt, nil, c.IdempotencyKey)
	}
	valid, err := u.password.Verify(state.PasswordHash, c.Password)
	if err != nil {
		return LoginResult{}, ErrAuthenticationDependency
	}
	if !valid {
		return u.failure(ctx, attempt, &state, c.IdempotencyKey)
	}
	var result LoginResult
	err = u.uow.WithinPlatformPassword(ctx, func(tx PlatformPasswordTransaction) error {
		current, err := tx.LockPassword(ctx, account)
		if err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		if current.Login.Principal.ID != state.Login.Principal.ID || current.Login.IdentityID != state.Login.IdentityID || current.CredentialVersion != state.CredentialVersion || current.PasswordHash != state.PasswordHash || current.LockedUntil.After(now) || !current.Login.IdentityActive {
			return ErrInvalidCredential
		}
		if current.Login.Principal.Status != PrincipalStatusActive {
			return ErrPrincipalInactive
		}
		if current.Login.MembershipID == uuid.Nil || current.Login.MembershipStatus != MembershipStatusActive {
			return ErrMembershipInactive
		}
		var ids [6]uuid.UUID
		seen := map[uuid.UUID]bool{}
		for i := range ids {
			ids[i], err = u.ids.NewID()
			if err != nil || ids[i].Version() != 7 || seen[ids[i]] {
				return ErrAuthenticationDependency
			}
			seen[ids[i]] = true
		}
		secret, err := u.secrets.NewSecret()
		if err != nil || len(secret) < 32 {
			return ErrAuthenticationDependency
		}
		accessExpiry, idleExpiry, absoluteExpiry := loginDeadlines(AudienceBoss, now)
		session := Session{ID: ids[0], PrincipalID: current.Login.Principal.ID, Audience: AudienceBoss, Status: SessionStatusActive, Version: 1, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodPassword}, DeviceName: strings.TrimSpace(c.DeviceName), IdleExpiresAt: idleExpiry, AbsoluteExpiry: absoluteExpiry, ReauthenticatedAt: now, CreatedAt: now, UpdatedAt: now}
		grant := SessionGrant{ID: ids[1], SessionID: session.ID, MembershipID: current.Login.MembershipID, Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
		family := RefreshTokenFamily{ID: ids[2], GrantID: grant.ID, Status: GrantStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
		refresh := RefreshToken{ID: ids[3], FamilyID: family.ID, Digest: sha256.Sum256([]byte(secret)), Status: RefreshTokenStatusActive, IssuedAt: now, ExpiresAt: absoluteExpiry}
		audit := SecurityAuditEvent{ID: ids[5], ActorID: session.PrincipalID, AuthenticationMethod: AuditAuthenticationMethodPassword, Boundary: AuditBoundaryPlatform, Action: AuditActionPasswordLoginSucceeded, TargetType: AuditTargetTypeSession, TargetID: session.ID, TargetVersion: 1, Result: AuditResultSucceeded, Reason: AuditReasonPasswordLogin, RequestID: c.IdempotencyKey, CorrelationID: c.IdempotencyKey, DecisionID: ids[5].String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		access, err := u.tokens.Issue(ctx, AccessTokenClaims{Boundary: AccessBoundaryPlatform, Issuer: u.issuer, Subject: session.PrincipalID, Audience: AudienceBoss, TokenID: ids[4], SessionID: session.ID, GrantID: grant.ID, GrantVersion: 1, IssuedAt: now, ExpiresAt: accessExpiry, AuthnMethods: session.AuthnMethods})
		if err != nil {
			return ErrAuthenticationDependency
		}
		if err = tx.ResetPasswordFailures(ctx, session.PrincipalID, current.CredentialVersion, now); err != nil {
			return err
		}
		if err = tx.SaveLogin(ctx, PlatformLoginMutation{Session: session, Grant: grant, Family: family, Refresh: refresh, Audit: audit}); err != nil {
			return err
		}
		result = LoginResult{Boundary: AccessBoundaryPlatform, Principal: current.Login.Principal, Session: session, Grant: grant, AccessToken: access, RefreshToken: secret, AccessTokenExpiresAt: accessExpiry}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrPrincipalInactive) || errors.Is(err, ErrMembershipInactive) {
			if auditErr := u.denied(ctx, state, c.IdempotencyKey, err); auditErr != nil {
				return LoginResult{}, ErrAuthenticationDependency
			}
		}
		return LoginResult{}, err
	}
	// A transient reset failure retains the stricter bucket after the durable commit.
	_ = u.throttle.Reset(ctx, attempt)
	return result, nil
}

// A current-state denial changes no authentication state. Its Audit still has
// to commit before the caller receives a credential/authority rejection.
func (u *PlatformPasswordUsecase) denied(ctx context.Context, state PlatformPasswordState, requestID string, cause error) error {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return ErrAuthenticationDependency
	}
	reason := AuditReasonInvalidCredential
	if errors.Is(cause, ErrPrincipalInactive) {
		reason = AuditReasonPrincipalInactive
	}
	if errors.Is(cause, ErrMembershipInactive) {
		reason = AuditReasonMembershipInactive
	}
	return u.uow.WithinPlatformPassword(ctx, func(tx PlatformPasswordTransaction) error {
		now := u.clock.Now().UTC()
		audit := SecurityAuditEvent{ID: id, AuthenticationMethod: AuditAuthenticationMethodAnonymous, Boundary: AuditBoundaryPlatform, Action: AuditActionPasswordLoginFailed, TargetType: AuditTargetTypePasswordCredential, TargetID: state.Login.Principal.ID, TargetVersion: state.CredentialVersion, Result: AuditResultDenied, Reason: reason, RequestID: requestID, CorrelationID: requestID, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		// PrincipalID remains absent: lack of Membership or a concurrent change
		// must not penalize the password's durable failure counter.
		return tx.RecordPasswordFailure(ctx, LoginFailureMutation{Audit: audit})
	})
}
func (u *PlatformPasswordUsecase) failure(ctx context.Context, attempt LoginThrottleAttempt, observed *PlatformPasswordState, requestID string) (LoginResult, error) {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return LoginResult{}, ErrAuthenticationDependency
	}
	err = u.uow.WithinPlatformPassword(ctx, func(tx PlatformPasswordTransaction) error {
		now := u.clock.Now().UTC()
		m := LoginFailureMutation{FailedAt: now, LockUntil: now.Add(15 * time.Minute), Audit: SecurityAuditEvent{ID: id, AuthenticationMethod: AuditAuthenticationMethodAnonymous, Boundary: AuditBoundaryPlatform, Action: AuditActionPasswordLoginFailed, TargetType: AuditTargetTypePasswordCredential, TargetID: id, TargetVersion: 1, Result: AuditResultFailed, Reason: AuditReasonInvalidCredential, RequestID: requestID, CorrelationID: requestID, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}}
		if observed != nil {
			current, err := tx.LockPassword(ctx, attempt.NormalizedAccount)
			if err != nil && !errors.Is(err, ErrInvalidCredential) {
				return err
			}
			if err == nil && current.Login.Principal.ID == observed.Login.Principal.ID && current.Login.IdentityID == observed.Login.IdentityID && current.PasswordHash == observed.PasswordHash && !current.LockedUntil.After(now) {
				m.PrincipalID = current.Login.Principal.ID
				m.Audit.TargetID = m.PrincipalID
			}
		}
		return tx.RecordPasswordFailure(ctx, m)
	})
	if err != nil {
		return LoginResult{}, ErrAuthenticationDependency
	}
	if u.throttle.RecordFailure(ctx, attempt) != nil {
		return LoginResult{}, ErrAuthenticationDependency
	}
	return LoginResult{}, ErrInvalidCredential
}
