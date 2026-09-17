package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"slices"
	"strings"
	"time"
)

var ErrInvitationDenied = errors.New("invitation acceptance is denied")

// Independent Human authentication identifies the recipient; the Invitation
// controls only its own target Membership creation.
type InvitationAcceptanceTarget struct {
	Boundary               AccessBoundary
	TenantID, InvitationID uuid.UUID
}
type InvitationAcceptanceCommand struct {
	Target                                                                InvitationAcceptanceTarget
	Credential, InvitationToken, IdempotencyKey, RequestID, CorrelationID string
}
type InvitationAcceptanceCapability struct {
	claims                              AccessTokenClaims
	password                            *InvitationPasswordState
	target                              InvitationAcceptanceTarget
	caller                              DirectCaller
	digest                              [sha256.Size]byte
	operation, requestID, correlationID string
}

// The alternatives are explicit. Password proof contains no Session or AT claims.
type InvitationAcceptanceBinding struct {
	Subject  uuid.UUID
	Session  AccessTokenClaims
	Password *InvitationPasswordState
}

func (c InvitationAcceptanceCapability) Binding() (InvitationAcceptanceBinding, InvitationAcceptanceTarget, DirectCaller, error) {
	op, rpc, err := invitationAcceptanceOperation(c.target)
	targetRPC := "/iam.v1.IAMAdminService/" + rpc
	b := InvitationAcceptanceBinding{Subject: c.claims.Subject, Session: c.claims}
	b.Session.AuthnMethods = slices.Clone(c.claims.AuthnMethods)
	if c.password != nil {
		proof := *c.password
		b = InvitationAcceptanceBinding{Subject: proof.PrincipalID, Password: &proof}
		targetRPC = invitationPasswordRPC
		if c.claims.Subject != uuid.Nil {
			return b, c.target, c.caller, ErrInvitationDenied
		}
	}
	if err != nil || op != c.operation || b.Subject == uuid.Nil || c.caller.Target != (WorkloadTarget{Audience: "ani-iam", Operation: targetRPC}) {
		return b, c.target, c.caller, ErrInvitationDenied
	}
	return b, c.target, c.caller, nil
}

// Match compares the private authority sealed by the usecase, including proof and intent.
func (c InvitationAcceptanceCapability) Matches(other InvitationAcceptanceCapability) bool {
	if c.target != other.target || c.caller != other.caller || c.digest != other.digest || c.operation != other.operation || c.requestID != other.requestID || c.correlationID != other.correlationID {
		return false
	}
	if c.password != nil || other.password != nil {
		return c.password != nil && other.password != nil && *c.password == *other.password
	}
	a, b := c.claims, other.claims
	return a.Subject == b.Subject && a.Boundary == b.Boundary && a.TenantID == b.TenantID && a.SessionID == b.SessionID && a.GrantID == b.GrantID && a.GrantVersion == b.GrantVersion && a.ExpiresAt.Equal(b.ExpiresAt) && slices.Equal(a.AuthnMethods, b.AuthnMethods)
}
func (c InvitationAcceptanceCapability) validateActor(a InvitationAcceptanceActor, now time.Time) error {
	if c.password != nil {
		return validateInvitationPassword(*c.password, a.Password, now)
	}
	if err := validateInvitationAcceptanceActor(c.claims, a, now); err != nil {
		return invitationSourceAuthenticationError(c.claims, err)
	}
	return nil
}
func invitationAcceptanceOperation(t InvitationAcceptanceTarget) (string, string, error) {
	if t.InvitationID == uuid.Nil || t.InvitationID.Version() != 7 {
		return "", "", ErrInvitationInvalid
	}
	if t.Boundary == AccessBoundaryPlatform && t.TenantID == uuid.Nil {
		return "acceptPlatformIAMInvitation", "AcceptPlatformInvitation", nil
	}
	if t.Boundary == AccessBoundaryTenant && t.TenantID != uuid.Nil && t.TenantID.Version() == 7 {
		return "acceptTenantIAMInvitation", "AcceptTenantInvitation", nil
	}
	return "", "", ErrInvitationInvalid
}
func invitationAcceptanceDigest(target InvitationAcceptanceTarget, raw string) ([sha256.Size]byte, error) {
	parts := strings.Split(raw, ".")
	var secret string
	if target.Boundary == AccessBoundaryTenant {
		if len(parts) != 4 || parts[0] != "ani_inv_t" || parts[1] != target.TenantID.String() || parts[2] != target.InvitationID.String() {
			return [sha256.Size]byte{}, ErrInvitationDenied
		}
		secret = parts[3]
	} else if target.Boundary == AccessBoundaryPlatform {
		if len(parts) != 3 || parts[0] != "ani_inv_p" || parts[1] != target.InvitationID.String() {
			return [sha256.Size]byte{}, ErrInvitationDenied
		}
		secret = parts[2]
	} else {
		return [sha256.Size]byte{}, ErrInvitationInvalid
	}
	if len(secret) < 32 || len(secret) > 128 {
		return [sha256.Size]byte{}, ErrInvitationDenied
	}
	for _, r := range secret {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return [sha256.Size]byte{}, ErrInvitationDenied
		}
	}
	return sha256.Sum256([]byte(raw)), nil
}

type InvitationAcceptanceActor struct {
	Password                         InvitationPasswordState
	NormalizedEmail                  string
	PrincipalStatus                  PrincipalStatus
	MembershipStatus                 MembershipStatus
	SessionStatus                    SessionStatus
	GrantStatus                      GrantStatus
	GrantVersion                     int64
	IdleExpiresAt, AbsoluteExpiresAt time.Time
	SourceAccess                     TenantAccessStatus
	SourceLifecycle                  TenantLifecycleStatus
	SourceLifecycleFresh             bool
}
type InvitationAcceptedMembership struct {
	ID, PrincipalID      uuid.UUID
	Status               MembershipStatus
	Version              int64
	RoleIDs              []uuid.UUID
	CreatedAt, UpdatedAt time.Time
}
type InvitationAcceptanceResult struct {
	Target       InvitationAcceptanceTarget
	Membership   InvitationAcceptedMembership
	AuditEventID uuid.UUID
}
type InvitationAcceptanceTransaction interface {
	ResetAcceptancePasswordFailures(context.Context, InvitationAcceptanceCapability, int64, time.Time) error
	Actor(context.Context, InvitationAcceptanceCapability) (InvitationAcceptanceActor, error)
	Invitation(context.Context, InvitationAcceptanceCapability) (TenantInvitationState, error)
	TargetReady(context.Context, InvitationAcceptanceCapability, TenantAdminLoginPolicy) (*InvitationBootstrapState, error)
	CompleteBootstrap(context.Context, InvitationAcceptanceCapability, InvitationBootstrapState, InvitationAcceptedMembership, SecurityAuditEvent, time.Time) error
	ExistingMembership(context.Context, InvitationAcceptanceCapability) (InvitationAcceptedMembership, bool, error)
	ValidateRoles(context.Context, InvitationAcceptanceCapability, []uuid.UUID) error
	CreateMembership(context.Context, InvitationAcceptanceCapability, InvitationAcceptedMembership, []uuid.UUID) error
	ConsumeInvitation(context.Context, InvitationAcceptanceCapability, TenantInvitationState, InvitationAcceptedMembership, time.Time) error
	FindReceipt(context.Context, InvitationAcceptanceCapability, MutationIdentity) (StoredMutation, bool, error)
	SaveReceipt(context.Context, InvitationAcceptanceCapability, StoredMutation) error
	AppendAcceptanceAudit(context.Context, InvitationAcceptanceCapability, SecurityAuditEvent) error
}
type InvitationAcceptanceUnitOfWork interface {
	WithinInvitationAcceptance(context.Context, InvitationAcceptanceCapability, func(InvitationAcceptanceTransaction) error) error
}
type InvitationAcceptanceUsecase struct {
	uow                  InvitationAcceptanceUnitOfWork
	verifier             AccessCredentialVerifier
	ids                  IDGenerator
	clock                Clock
	bootstrapLoginPolicy TenantAdminLoginPolicy
	bootstrapCatalog     PermissionCatalog
}

func NewInvitationAcceptanceUsecase(uow InvitationAcceptanceUnitOfWork, verifier AccessCredentialVerifier, ids IDGenerator, clock Clock) *InvitationAcceptanceUsecase {
	return &InvitationAcceptanceUsecase{uow: uow, verifier: verifier, ids: ids, clock: clock}
}
func (u *InvitationAcceptanceUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}
func (u *InvitationAcceptanceUsecase) Accept(ctx context.Context, c InvitationAcceptanceCommand) (InvitationAcceptanceResult, error) {
	if u == nil || u.uow == nil || u.verifier == nil || u.ids == nil || u.clock == nil {
		return InvitationAcceptanceResult{}, ErrAuthenticationDependency
	}
	operation, rpc, err := invitationAcceptanceOperation(c.Target)
	if err != nil {
		return InvitationAcceptanceResult{}, err
	}
	caller, ok := DirectCallerFromContext(ctx)
	if !ok || caller.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + rpc}) {
		return InvitationAcceptanceResult{}, ErrInvitationDenied
	}
	if c.IdempotencyKey == "" || strings.TrimSpace(c.IdempotencyKey) != c.IdempotencyKey || len(c.IdempotencyKey) > 128 {
		return InvitationAcceptanceResult{}, ErrIdempotencyKeyRequired
	}
	claims, err := u.verifier.Verify(ctx, c.Credential)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return InvitationAcceptanceResult{}, err
		}
		return InvitationAcceptanceResult{}, ErrInvalidCredential
	}
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.GrantVersion < 1 || !u.clock.Now().UTC().Before(claims.ExpiresAt) || !containsHumanAuthenticationMethod(claims.AuthnMethods) {
		return InvitationAcceptanceResult{}, ErrInvalidCredential
	}
	if !((claims.Boundary == AccessBoundaryTenant && claims.Audience == AudienceConsole && claims.TenantID != uuid.Nil) || (claims.Boundary == AccessBoundaryPlatform && claims.Audience == AudienceBoss && claims.TenantID == uuid.Nil)) {
		return InvitationAcceptanceResult{}, ErrInvalidCredential
	}
	digest, err := invitationAcceptanceDigest(c.Target, c.InvitationToken)
	if err != nil {
		return InvitationAcceptanceResult{}, err
	}
	request, correlation := c.RequestID, c.CorrelationID
	if request == "" {
		id, err := u.newID()
		if err != nil {
			return InvitationAcceptanceResult{}, err
		}
		request = id.String()
	}
	if correlation == "" {
		correlation = request
	}
	cap := InvitationAcceptanceCapability{claims: claims, target: c.Target, caller: caller, digest: digest, operation: operation, requestID: request, correlationID: correlation}
	return u.accept(ctx, cap, c.IdempotencyKey)
}
func (u *InvitationAcceptanceUsecase) accept(ctx context.Context, cap InvitationAcceptanceCapability, key string) (InvitationAcceptanceResult, error) {
	binding, target, caller, err := cap.Binding()
	if err != nil {
		return InvitationAcceptanceResult{}, err
	}
	digest, operation, request, correlation := cap.digest, cap.operation, cap.requestID, cap.correlationID
	reset := func(tx InvitationAcceptanceTransaction, a InvitationAcceptanceActor, now time.Time) error {
		if cap.password == nil {
			return nil
		}
		return tx.ResetAcceptancePasswordFailures(ctx, cap, a.Password.CredentialVersion, now)
	}
	raw, _ := json.Marshal(struct {
		Target        InvitationAcceptanceTarget
		Actor, Caller uuid.UUID
		Digest        [32]byte
	}{target, binding.Subject, caller.Identity.PrincipalID, digest})
	identity := MutationIdentity{ActorID: binding.Subject, CallerID: caller.Identity.PrincipalID, Operation: operation, Key: key, Intent: sha256.Sum256(raw)}
	var result InvitationAcceptanceResult
	err = u.uow.WithinInvitationAcceptance(ctx, cap, func(tx InvitationAcceptanceTransaction) error {
		actor, err := tx.Actor(ctx, cap)
		if err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		if err = cap.validateActor(actor, now); err != nil {
			return err
		}
		state, err := tx.Invitation(ctx, cap)
		if err != nil {
			return err
		}
		if state.Invitation.ID != target.InvitationID || subtle.ConstantTimeCompare(state.TokenDigest[:], digest[:]) != 1 || state.Invitation.NormalizedEmail != actor.NormalizedEmail {
			return ErrInvitationDenied
		}
		previous, found, err := tx.FindReceipt(ctx, cap, identity)
		if err != nil {
			return err
		}
		if found {
			if previous.Identity.Intent != identity.Intent {
				return ErrIdempotencyConflict
			}
			if !now.Before(previous.ExpiresAt) {
				return ErrIdempotencyExpired
			}
			d := json.NewDecoder(bytes.NewReader(previous.Result))
			d.DisallowUnknownFields()
			if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.Target != target || result.Membership.PrincipalID != binding.Subject || result.Membership.ID == uuid.Nil || result.AuditEventID.Version() != 7 {
				return ErrInvalidPersistenceState
			}
			return reset(tx, actor, now)
		}
		if state.Invitation.Status != InvitationPending || !now.Before(state.Invitation.ExpiresAt) {
			return ErrInvitationConflict
		}
		bootstrap, err := tx.TargetReady(ctx, cap, u.bootstrapLoginPolicy)
		if err != nil {
			return err
		}
		if err = validateInvitationBootstrap(bootstrap, actor.NormalizedEmail, state.Invitation.RoleIDs, u.bootstrapCatalog); err != nil {
			return err
		}
		previousMember, exists, err := tx.ExistingMembership(ctx, cap)
		if err != nil {
			return err
		}
		if exists && (previousMember.Status != MembershipStatusRemoved || !state.Invitation.CreatedAt.After(previousMember.UpdatedAt)) {
			return ErrInvitationConflict
		}
		if err = tx.ValidateRoles(ctx, cap, state.Invitation.RoleIDs); err != nil {
			return err
		}
		// Check the actual clock after state/role locks; expiration cannot be extended
		// by queueing behind another operation.
		now = u.clock.Now().UTC()
		if !now.Before(state.Invitation.ExpiresAt) {
			return ErrInvitationConflict
		}
		if err = cap.validateActor(actor, now); err != nil {
			return err
		}
		memberID, err := u.newID()
		if err != nil {
			return err
		}
		bindings := make([]uuid.UUID, len(state.Invitation.RoleIDs))
		for i := range bindings {
			bindings[i], err = u.newID()
			if err != nil {
				return err
			}
		}
		member := InvitationAcceptedMembership{ID: memberID, PrincipalID: binding.Subject, Status: MembershipStatusActive, Version: 1, RoleIDs: slices.Clone(state.Invitation.RoleIDs), CreatedAt: now, UpdatedAt: now}
		if err = tx.CreateMembership(ctx, cap, member, bindings); err != nil {
			return err
		}
		if err = tx.ConsumeInvitation(ctx, cap, state, member, now); err != nil {
			return err
		}
		auditID, err := u.newID()
		if err != nil {
			return err
		}
		audit := SecurityAuditEvent{ID: auditID, ActorID: binding.Subject, DirectCaller: caller, AuthenticationMethod: invitationAcceptanceAuthenticationMethod(cap), Boundary: AuditBoundaryTenant, Action: "iam.invitation.accepted", TargetType: "invitation", TargetID: state.Invitation.ID, TargetVersion: state.Invitation.Version + 1, Result: AuditResultSucceeded, Reason: "INVITATION_RECIPIENT_VERIFIED", RequestID: request, CorrelationID: correlation, DecisionID: auditID.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
		if target.Boundary == AccessBoundaryPlatform {
			audit.Boundary = AuditBoundaryPlatform
			audit.Action = "iam.platform.invitation.accepted"
		}
		if err = tx.AppendAcceptanceAudit(ctx, cap, audit); err != nil {
			return err
		}
		if err = u.completeBootstrap(ctx, tx, cap, bootstrap, member, audit, now); err != nil {
			return err
		}
		result = InvitationAcceptanceResult{Target: target, Membership: member, AuditEventID: auditID}
		encoded, err := json.Marshal(result)
		if err != nil {
			return ErrInvalidPersistenceState
		}
		if err = reset(tx, actor, now); err != nil {
			return err
		}
		return tx.SaveReceipt(ctx, cap, StoredMutation{Identity: identity, Result: encoded, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
func invitationSourceAuthenticationError(c AccessTokenClaims, err error) error {
	if c.Boundary == AccessBoundaryTenant {
		return tenantBoundAuthenticationError(c.TenantID, err)
	}
	return err
}
func validateInvitationAcceptanceActor(c AccessTokenClaims, a InvitationAcceptanceActor, now time.Time) error {
	if !now.Before(c.ExpiresAt) || a.SessionStatus != SessionStatusActive || a.GrantStatus != GrantStatusActive || a.GrantVersion != c.GrantVersion || !now.Before(a.IdleExpiresAt) || !now.Before(a.AbsoluteExpiresAt) {
		return ErrInvalidCredential
	}
	if a.PrincipalStatus != PrincipalStatusActive || a.NormalizedEmail == "" {
		return ErrPrincipalInactive
	}
	if a.MembershipStatus != MembershipStatusActive {
		return ErrMembershipInactive
	}
	if c.Boundary == AccessBoundaryTenant {
		if a.SourceAccess != TenantAccessStatusActive {
			return ErrTenantAccessInactive
		}
		if !a.SourceLifecycleFresh {
			return ErrTenantLifecycleStale
		}
		if a.SourceLifecycle != TenantLifecycleStatusActive {
			return ErrTenantLifecycleBlocked
		}
	}
	return nil
}
