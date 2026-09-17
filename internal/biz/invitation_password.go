package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/netip"
	"strings"
	"time"
)

const invitationPasswordRPC = "/iam.v1.AuthenticationService/AcceptInvitationWithPassword"

// Global authentication state; no Tenant or Platform Membership is required.
type InvitationPasswordState struct {
	PrincipalID, IdentityID       uuid.UUID
	PrincipalStatus               PrincipalStatus
	IdentityActive                bool
	NormalizedEmail, PasswordHash string
	CredentialVersion             int64
	LockedUntil                   time.Time
}
type InvitationPasswordRepository interface {
	ReadInvitationPassword(context.Context, string) (InvitationPasswordState, error)
	RecordInvitationPasswordFailure(context.Context, InvitationPasswordState, SecurityAuditEvent, bool) error
}
type InvitationPasswordCommand struct {
	Account, Password, InvitationToken, IdempotencyKey string
	SourceIP                                           netip.Addr
}
type InvitationPasswordUsecase struct {
	acceptance *InvitationAcceptanceUsecase
	repo       InvitationPasswordRepository
	verifier   PasswordVerifier
	throttle   LoginThrottle
}

func NewInvitationPasswordUsecase(a *InvitationAcceptanceUsecase, r InvitationPasswordRepository, v PasswordVerifier, t LoginThrottle) *InvitationPasswordUsecase {
	return &InvitationPasswordUsecase{a, r, v, t}
}
func invitationPasswordActive(s InvitationPasswordState, now time.Time) bool {
	return s.PrincipalID != uuid.Nil && s.IdentityID != uuid.Nil && s.PrincipalStatus == PrincipalStatusActive && s.IdentityActive && s.NormalizedEmail != "" && s.PasswordHash != "" && s.CredentialVersion > 0 && !now.Before(s.LockedUntil)
}
func validateInvitationPassword(observed, current InvitationPasswordState, now time.Time) error {
	// A counter reset/failure can advance version without rotating the password.
	// Current lockout still wins, and any changed identity/hash rejects the proof.
	if !invitationPasswordActive(current, now) || observed.PrincipalID != current.PrincipalID || observed.IdentityID != current.IdentityID || observed.NormalizedEmail != current.NormalizedEmail || observed.PasswordHash != current.PasswordHash || current.CredentialVersion < observed.CredentialVersion {
		return ErrInvalidCredential
	}
	return nil
}
func invitationAcceptanceAuthenticationMethod(c InvitationAcceptanceCapability) AuditAuthenticationMethod {
	if c.password != nil {
		return AuditAuthenticationMethodPassword
	}
	return firstAuthenticationMethod(c.claims.AuthnMethods)
}
func invitationTargetFromToken(raw string) (InvitationAcceptanceTarget, error) {
	p := strings.Split(raw, ".")
	var t InvitationAcceptanceTarget
	var err error
	switch {
	case len(p) == 4 && p[0] == "ani_inv_t":
		t.Boundary = AccessBoundaryTenant
		t.TenantID, err = uuid.Parse(p[1])
		if err != nil {
			return t, ErrInvitationDenied
		}
		t.InvitationID, err = uuid.Parse(p[2])
	case len(p) == 3 && p[0] == "ani_inv_p":
		t.Boundary = AccessBoundaryPlatform
		t.InvitationID, err = uuid.Parse(p[1])
	default:
		return t, ErrInvitationDenied
	}
	if err != nil {
		return t, ErrInvitationDenied
	}
	if _, _, err = invitationAcceptanceOperation(t); err != nil {
		return t, ErrInvitationDenied
	}
	if _, err = invitationAcceptanceDigest(t, raw); err != nil {
		return t, ErrInvitationDenied
	}
	return t, nil
}
func (u *InvitationPasswordUsecase) Accept(ctx context.Context, c InvitationPasswordCommand) (InvitationAcceptanceResult, error) {
	empty := InvitationAcceptanceResult{}
	if u == nil || u.acceptance == nil || u.acceptance.uow == nil || u.acceptance.ids == nil || u.acceptance.clock == nil || u.repo == nil || u.verifier == nil || u.throttle == nil {
		return empty, ErrAuthenticationDependency
	}
	caller, ok := DirectCallerFromContext(ctx)
	if !ok || caller.Target != (WorkloadTarget{Audience: "ani-iam", Operation: invitationPasswordRPC}) {
		return empty, ErrInvalidCredential
	}
	email, err := invitedAccountInput(c.Account, c.IdempotencyKey, c.SourceIP)
	if err != nil || c.Password == "" || len(c.Password) > 1024 || len(c.InvitationToken) > 512 {
		return empty, ErrInvitedAccountInvalid
	}
	attempt := LoginThrottleAttempt{NormalizedAccount: email, SourceIP: c.SourceIP.Unmap()}
	if err = u.throttle.Check(ctx, attempt); err != nil {
		return empty, invitationThrottleError(err)
	}
	observed, err := u.repo.ReadInvitationPassword(ctx, email)
	if err != nil && !errors.Is(err, ErrInvalidCredential) {
		return empty, err
	}
	active := err == nil && invitationPasswordActive(observed, u.acceptance.clock.Now().UTC())
	valid := false
	if active {
		valid, err = u.verifier.Verify(observed.PasswordHash, c.Password)
	} else {
		err = u.verifier.VerifyUnknown(c.Password)
	}
	if err != nil {
		return empty, ErrAuthenticationDependency
	}
	if !valid {
		return empty, u.denied(ctx, observed, caller, c.IdempotencyKey, attempt, active)
	}
	target, err := invitationTargetFromToken(c.InvitationToken)
	if err != nil {
		return empty, err
	}
	digest, _ := invitationAcceptanceDigest(target, c.InvitationToken)
	op, _, _ := invitationAcceptanceOperation(target)
	cap := InvitationAcceptanceCapability{password: &observed, target: target, caller: caller, digest: digest, operation: op, requestID: c.IdempotencyKey, correlationID: c.IdempotencyKey}
	result, err := u.acceptance.accept(ctx, cap, c.IdempotencyKey)
	if errors.Is(err, ErrInvalidCredential) {
		return empty, u.denied(ctx, observed, caller, c.IdempotencyKey, attempt, false)
	}
	if err != nil {
		return empty, err
	}
	if err = u.throttle.Reset(ctx, attempt); err != nil {
		return empty, ErrAuthenticationDependency
	}
	return result, nil
}
func invitationThrottleError(err error) error {
	if errors.Is(err, ErrAuthenticationRateLimited) {
		return err
	}
	return ErrAuthenticationDependency
}
func (u *InvitationPasswordUsecase) denied(ctx context.Context, s InvitationPasswordState, c DirectCaller, key string, attempt LoginThrottleAttempt, increment bool) error {
	id, err := u.acceptance.newID()
	if err != nil {
		return err
	}
	now := u.acceptance.clock.Now().UTC()
	target := s.PrincipalID
	if target == uuid.Nil {
		target = id
	}
	audit := SecurityAuditEvent{ID: id, DirectCaller: c, AuthenticationMethod: AuditAuthenticationMethodAnonymous, Boundary: AuditBoundaryPrincipal, Action: "iam.invitation.authentication.denied", TargetType: "principal", TargetID: target, TargetVersion: 1, Result: AuditResultDenied, Reason: "CREDENTIAL_INVALID", RequestID: key, CorrelationID: key, DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
	if err = u.repo.RecordInvitationPasswordFailure(ctx, s, audit, increment); err != nil {
		return err
	}
	// Every credential denial contributes once to the public account/IP limit.
	// Only an active credential's wrong password increments its durable counter.
	if err = u.throttle.RecordFailure(ctx, attempt); err != nil {
		return invitationThrottleError(err)
	}
	return ErrInvalidCredential
}
