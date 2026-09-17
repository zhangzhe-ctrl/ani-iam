package biz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PlatformAuthorizationState struct {
	PrincipalStatus                                     PrincipalStatus
	MembershipID                                        uuid.UUID
	MembershipStatus                                    MembershipStatus
	SessionStatus                                       SessionStatus
	IdleExpiresAt, AbsoluteExpiresAt, ReauthenticatedAt time.Time
	GrantStatus                                         GrantStatus
	GrantVersion                                        int64
	PermissionAllowed                                   bool
}
type PlatformAuthorizationReader interface {
	LookupPlatformAuthorization(context.Context, AccessTokenClaims, string, []string) (PlatformAuthorizationState, error)
	RecordPlatformAuthorization(context.Context, SecurityAuditEvent) error
}
type PlatformAuthorizationUsecase struct {
	registry AuthorizationPolicyRegistry
	verifier AccessCredentialVerifier
	reader   PlatformAuthorizationReader
	ids      IDGenerator
	clock    Clock
}

func NewPlatformAuthorizationUsecase(registry AuthorizationPolicyRegistry, verifier AccessCredentialVerifier, reader PlatformAuthorizationReader, ids IDGenerator, clock Clock) (*PlatformAuthorizationUsecase, error) {
	if registry == nil || verifier == nil || reader == nil || ids == nil || clock == nil {
		return nil, ErrAuthorizationDependency
	}
	return &PlatformAuthorizationUsecase{registry: registry, verifier: verifier, reader: reader, ids: ids, clock: clock}, nil
}
func (u *PlatformAuthorizationUsecase) policy(operation, revision string) (AuthorizationPolicy, error) {
	if strings.TrimSpace(revision) == "" {
		return AuthorizationPolicy{}, ErrAuthorizationPolicyRevisionRequired
	}
	if revision != u.registry.Revision() {
		return AuthorizationPolicy{}, &AuthorizationPolicyMismatchError{Expected: u.registry.Revision(), Actual: revision}
	}
	if strings.TrimSpace(operation) == "" {
		return AuthorizationPolicy{}, ErrAuthorizationOperationRequired
	}
	p, ok := u.registry.Lookup(operation)
	if !ok || p.OperationID != operation || p.Scope != PermissionScopePlatform || p.Resource == "" || len(p.Actions) == 0 || len(p.Obligations) != 0 {
		return AuthorizationPolicy{}, ErrAuthorizationOperationUnregistered
	}
	if !credentialKindAllowed(p.CredentialKinds, CredentialKindAccessToken) || !principalKindAllowed(p.PrincipalKinds, PrincipalTypeHuman) {
		return AuthorizationPolicy{}, ErrAuthenticationCredentialKindDenied
	}
	return p, nil
}
func (u *PlatformAuthorizationUsecase) lookup(ctx context.Context, raw string, p AuthorizationPolicy) (AccessTokenClaims, PlatformAuthorizationState, error) {
	if strings.TrimSpace(raw) == "" {
		return AccessTokenClaims{}, PlatformAuthorizationState{}, ErrInvalidCredential
	}
	c, err := u.verifier.Verify(ctx, raw)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return c, PlatformAuthorizationState{}, err
		}
		return AccessTokenClaims{}, PlatformAuthorizationState{}, ErrInvalidCredential
	}
	if c.Boundary != AccessBoundaryPlatform || c.Audience != AudienceBoss || c.TenantID != uuid.Nil || c.Subject == uuid.Nil || c.SessionID == uuid.Nil || c.GrantID == uuid.Nil || c.GrantVersion <= 0 || !u.clock.Now().UTC().Before(c.ExpiresAt) || !containsHumanAuthenticationMethod(c.AuthnMethods) {
		return AccessTokenClaims{}, PlatformAuthorizationState{}, ErrInvalidCredential
	}
	s, err := u.reader.LookupPlatformAuthorization(ctx, c, p.Resource, append([]string(nil), p.Actions...))
	return c, s, err
}
func platformAuthorizationDenial(s PlatformAuthorizationState, c AccessTokenClaims, now time.Time) AuthorizationReason {
	switch {
	case s.PrincipalStatus != PrincipalStatusActive:
		return AuthorizationReasonPrincipalInactive
	case s.MembershipID == uuid.Nil || s.MembershipStatus != MembershipStatusActive:
		return AuthorizationReasonMembershipInactive
	case s.SessionStatus != SessionStatusActive || !now.Before(s.IdleExpiresAt) || !now.Before(s.AbsoluteExpiresAt):
		return AuthorizationReasonSessionInactive
	case s.GrantStatus != GrantStatusActive:
		return AuthorizationReasonGrantInactive
	case s.GrantVersion != c.GrantVersion:
		return AuthorizationReasonGrantVersionMismatch
	default:
		return ""
	}
}
func (u *PlatformAuthorizationUsecase) CheckPermission(ctx context.Context, c CheckPermissionCommand) (AuthorizationDecision, error) {
	p, err := u.policy(c.OperationID, c.PolicyRevision)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	claims, state, err := u.lookup(ctx, c.RawCredential, p)
	if err != nil {
		return AuthorizationDecision{}, u.failure(ctx, claims, c.RequestID, c.CorrelationID, err)
	}
	reason := platformAuthorizationDenial(state, claims, u.clock.Now().UTC())
	if reason == "" && !state.PermissionAllowed {
		reason = AuthorizationReasonPermissionDenied
	}
	decisionID, err := u.ids.NewID()
	if err != nil || decisionID.Version() != 7 {
		return AuthorizationDecision{}, ErrAuthorizationDependency
	}
	principal := platformTrustedPrincipal(claims, state)
	if reason != "" {
		if err = u.audit(ctx, claims, c.RequestID, c.CorrelationID, decisionID, AuditActionAuthorizationDenied, AuditResultDenied, AuditReason(reason)); err != nil {
			return AuthorizationDecision{}, err
		}
		return AuthorizationDecision{Allowed: false, Reason: reason, DecisionID: decisionID, Principal: principal, PolicyRevision: c.PolicyRevision}, nil
	}
	return AuthorizationDecision{Allowed: true, Reason: AuthorizationReasonAllowed, DecisionID: decisionID, Principal: principal, PolicyRevision: c.PolicyRevision}, nil
}
func (u *PlatformAuthorizationUsecase) ValidatePrincipal(ctx context.Context, c ValidatePrincipalCommand) (ValidatePrincipalResult, error) {
	p, err := u.policy(c.OperationID, c.PolicyRevision)
	if err != nil {
		return ValidatePrincipalResult{}, err
	}
	claims, state, err := u.lookup(ctx, c.RawCredential, p)
	if err != nil {
		return ValidatePrincipalResult{}, u.failure(ctx, claims, c.RequestID, c.CorrelationID, err)
	}
	if reason := platformAuthorizationDenial(state, claims, u.clock.Now().UTC()); reason != "" {
		failure := ErrInvalidCredential
		if reason == AuthorizationReasonPrincipalInactive {
			failure = ErrPrincipalInactive
		}
		if reason == AuthorizationReasonMembershipInactive {
			failure = ErrMembershipInactive
		}
		return ValidatePrincipalResult{}, u.failure(ctx, claims, c.RequestID, c.CorrelationID, failure)
	}
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return ValidatePrincipalResult{}, ErrAuthenticationDependency
	}
	if err = u.audit(ctx, claims, c.RequestID, c.CorrelationID, id, AuditActionPrincipalValidationSucceeded, AuditResultSucceeded, AuditReasonPrincipalValidation); err != nil {
		return ValidatePrincipalResult{}, err
	}
	return ValidatePrincipalResult{Principal: platformTrustedPrincipal(claims, state), DecisionID: id, PolicyRevision: c.PolicyRevision}, nil
}
func platformTrustedPrincipal(c AccessTokenClaims, s PlatformAuthorizationState) TrustedPrincipalContext {
	return TrustedPrincipalContext{Boundary: AccessBoundaryPlatform, ID: c.Subject, Type: PrincipalTypeHuman, Status: s.PrincipalStatus, SessionID: c.SessionID, GrantID: c.GrantID, AuthnMethods: append([]AuditAuthenticationMethod(nil), c.AuthnMethods...)}
}
func (u *PlatformAuthorizationUsecase) failure(ctx context.Context, c AccessTokenClaims, request, correlation string, cause error) error {
	if errors.Is(cause, ErrPersistenceUnavailable) || errors.Is(cause, ErrAuthorizationDependency) || errors.Is(cause, ErrAuthenticationDependency) {
		return cause
	}
	id, err := u.ids.NewID()
	if err != nil {
		return ErrAuthenticationDependency
	}
	if err = u.audit(ctx, c, request, correlation, id, AuditActionPrincipalValidationFailed, AuditResultFailed, AuditReasonInvalidCredential); err != nil {
		return err
	}
	return cause
}
func (u *PlatformAuthorizationUsecase) audit(ctx context.Context, c AccessTokenClaims, request, correlation string, decision uuid.UUID, action AuditAction, result AuditResult, reason AuditReason) error {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return ErrAuthorizationDependency
	}
	if request == "" {
		request = decision.String()
	}
	if correlation == "" {
		correlation = request
	}
	actor, method, target := c.Subject, firstAuthenticationMethod(c.AuthnMethods), c.GrantID
	if actor == uuid.Nil || method == "" {
		actor = uuid.Nil
		method = AuditAuthenticationMethodAnonymous
	}
	if target == uuid.Nil {
		target = id
	}
	version := c.GrantVersion
	if version < 1 {
		version = 1
	}
	now := u.clock.Now().UTC()
	return u.reader.RecordPlatformAuthorization(ctx, SecurityAuditEvent{ID: id, ActorID: actor, AuthenticationMethod: method, Boundary: AuditBoundaryPlatform, Action: action, TargetType: AuditTargetTypeSessionGrant, TargetID: target, TargetVersion: version, Result: result, Reason: reason, RequestID: request, CorrelationID: correlation, DecisionID: decision.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now})
}
