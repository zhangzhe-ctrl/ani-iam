package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrAuthorizationCredentialRequired     = errors.New("authorization credential is required")
	ErrAuthorizationCredentialInvalid      = errors.New("authorization credential is invalid")
	ErrAuthorizationOperationRequired      = errors.New("authorization operation is required")
	ErrAuthorizationOperationUnregistered  = errors.New("authorization operation is unregistered")
	ErrAuthorizationPolicyRevisionRequired = errors.New("authorization policy revision is required")
	ErrAuthorizationPolicyMismatch         = errors.New("authorization policy revision mismatches registry")
	ErrAuthorizationDependency             = errors.New("authorization dependency is unavailable")
)

type AuthorizationReason string

const (
	AuthorizationReasonAllowed              AuthorizationReason = "ALLOWED"
	AuthorizationReasonPermissionDenied     AuthorizationReason = "PERMISSION_DENIED"
	AuthorizationReasonPrincipalInactive    AuthorizationReason = "PRINCIPAL_INACTIVE"
	AuthorizationReasonMembershipInactive   AuthorizationReason = "MEMBERSHIP_INACTIVE"
	AuthorizationReasonTenantAccessInactive AuthorizationReason = "TENANT_ACCESS_INACTIVE"
	AuthorizationReasonLifecycleBlocked     AuthorizationReason = "TENANT_LIFECYCLE_BLOCKED"
	AuthorizationReasonLifecycleStale       AuthorizationReason = "TENANT_LIFECYCLE_STALE"
	AuthorizationReasonSessionInactive      AuthorizationReason = "SESSION_INACTIVE"
	AuthorizationReasonGrantInactive        AuthorizationReason = "GRANT_INACTIVE"
	AuthorizationReasonGrantVersionMismatch AuthorizationReason = "GRANT_VERSION_MISMATCH"
	AuthorizationReasonTenantMismatch       AuthorizationReason = "TENANT_MISMATCH"
)

type AuthorizationPolicyMismatchError struct {
	Expected string
	Actual   string
}

func (e *AuthorizationPolicyMismatchError) Error() string {
	return fmt.Sprintf("%v: expected %s, got %s", ErrAuthorizationPolicyMismatch, e.Expected, e.Actual)
}

func (e *AuthorizationPolicyMismatchError) Unwrap() error {
	return ErrAuthorizationPolicyMismatch
}

type AuthorizationPolicy struct {
	OperationID string
	Resource    string
	Actions     []string
}

type AuthorizationPolicyRegistry interface {
	Revision() string
	Lookup(operationID string) (AuthorizationPolicy, bool)
}

type AccessCredentialVerifier interface {
	Verify(context.Context, string) (AccessTokenClaims, error)
}

type AuthorizationLookup struct {
	PrincipalID          uuid.UUID
	SessionID            uuid.UUID
	GrantID              uuid.UUID
	ExpectedGrantVersion int64
	Resource             string
	Actions              []string
}

type AuthorizationState struct {
	PrincipalStatus   PrincipalStatus
	MembershipStatus  MembershipStatus
	TenantAccess      TenantAccessStatus
	Lifecycle         TenantLifecycleStatus
	LifecycleFresh    bool
	SessionStatus     SessionStatus
	GrantStatus       GrantStatus
	GrantVersion      int64
	PermissionAllowed bool
}

type AuthorizationReader interface {
	LookupAuthorization(context.Context, TenantScope, AuthorizationLookup) (AuthorizationState, error)
}

type TrustedPrincipalContext struct {
	ID           uuid.UUID
	Status       PrincipalStatus
	TenantID     uuid.UUID
	SessionID    uuid.UUID
	GrantID      uuid.UUID
	AuthnMethods []AuditAuthenticationMethod
}

type AuthorizationDecision struct {
	Allowed        bool
	Reason         AuthorizationReason
	DecisionID     uuid.UUID
	Principal      TrustedPrincipalContext
	PolicyRevision string
}

type CheckPermissionCommand struct {
	RawCredential  string
	OperationID    string
	PolicyRevision string
	TargetTenantID uuid.UUID
}

type AuthorizationUsecase struct {
	registry AuthorizationPolicyRegistry
	verifier AccessCredentialVerifier
	reader   AuthorizationReader
	ids      IDGenerator
	clock    Clock
}

func NewAuthorizationUsecase(
	registry AuthorizationPolicyRegistry,
	verifier AccessCredentialVerifier,
	reader AuthorizationReader,
	ids IDGenerator,
	clock Clock,
) *AuthorizationUsecase {
	return &AuthorizationUsecase{registry: registry, verifier: verifier, reader: reader, ids: ids, clock: clock}
}

func (u *AuthorizationUsecase) CheckPermission(ctx context.Context, command CheckPermissionCommand) (AuthorizationDecision, error) {
	if strings.TrimSpace(command.PolicyRevision) == "" {
		return AuthorizationDecision{}, ErrAuthorizationPolicyRevisionRequired
	}
	if command.PolicyRevision != u.registry.Revision() {
		return AuthorizationDecision{}, &AuthorizationPolicyMismatchError{
			Expected: u.registry.Revision(),
			Actual:   command.PolicyRevision,
		}
	}
	operationID := strings.TrimSpace(command.OperationID)
	if operationID == "" {
		return AuthorizationDecision{}, ErrAuthorizationOperationRequired
	}
	policy, ok := u.registry.Lookup(operationID)
	if !ok || policy.OperationID != operationID || strings.TrimSpace(policy.Resource) == "" || len(policy.Actions) == 0 {
		return AuthorizationDecision{}, ErrAuthorizationOperationUnregistered
	}
	credential := strings.TrimSpace(command.RawCredential)
	if credential == "" {
		return AuthorizationDecision{}, ErrAuthorizationCredentialRequired
	}
	scope, err := NewTenantScope(command.TargetTenantID)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	claims, err := u.verifier.Verify(ctx, credential)
	if err != nil {
		if errors.Is(err, ErrAuthorizationDependency) {
			return AuthorizationDecision{}, err
		}
		return AuthorizationDecision{}, errors.Join(ErrAuthorizationCredentialInvalid, err)
	}
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.GrantVersion <= 0 || claims.ExpiresAt.IsZero() || !u.clock.Now().UTC().Before(claims.ExpiresAt.UTC()) {
		return AuthorizationDecision{}, ErrAuthorizationCredentialInvalid
	}

	principal := TrustedPrincipalContext{
		ID:           claims.Subject,
		Status:       PrincipalStatusActive,
		TenantID:     claims.TenantID,
		SessionID:    claims.SessionID,
		GrantID:      claims.GrantID,
		AuthnMethods: append([]AuditAuthenticationMethod(nil), claims.AuthnMethods...),
	}
	if claims.TenantID != command.TargetTenantID {
		return u.deny(principal, command.PolicyRevision, AuthorizationReasonTenantMismatch)
	}

	state, err := u.reader.LookupAuthorization(ctx, scope, AuthorizationLookup{
		PrincipalID:          claims.Subject,
		SessionID:            claims.SessionID,
		GrantID:              claims.GrantID,
		ExpectedGrantVersion: claims.GrantVersion,
		Resource:             policy.Resource,
		Actions:              append([]string(nil), policy.Actions...),
	})
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("lookup authorization: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	principal.Status = state.PrincipalStatus
	if reason := authorizationDenialReason(state, claims.GrantVersion); reason != "" {
		return u.deny(principal, command.PolicyRevision, reason)
	}
	decisionID, err := u.newDecisionID()
	if err != nil {
		return AuthorizationDecision{}, err
	}
	return AuthorizationDecision{
		Allowed:        true,
		Reason:         AuthorizationReasonAllowed,
		DecisionID:     decisionID,
		Principal:      principal,
		PolicyRevision: command.PolicyRevision,
	}, nil
}

func authorizationDenialReason(state AuthorizationState, expectedGrantVersion int64) AuthorizationReason {
	switch {
	case state.PrincipalStatus != PrincipalStatusActive:
		return AuthorizationReasonPrincipalInactive
	case state.MembershipStatus != MembershipStatusActive:
		return AuthorizationReasonMembershipInactive
	case state.TenantAccess != TenantAccessStatusActive:
		return AuthorizationReasonTenantAccessInactive
	case !state.LifecycleFresh:
		return AuthorizationReasonLifecycleStale
	case state.Lifecycle != TenantLifecycleStatusActive:
		return AuthorizationReasonLifecycleBlocked
	case state.SessionStatus != SessionStatusActive:
		return AuthorizationReasonSessionInactive
	case state.GrantStatus != GrantStatusActive:
		return AuthorizationReasonGrantInactive
	case state.GrantVersion != expectedGrantVersion:
		return AuthorizationReasonGrantVersionMismatch
	case !state.PermissionAllowed:
		return AuthorizationReasonPermissionDenied
	default:
		return ""
	}
}

func (u *AuthorizationUsecase) deny(principal TrustedPrincipalContext, revision string, reason AuthorizationReason) (AuthorizationDecision, error) {
	decisionID, err := u.newDecisionID()
	if err != nil {
		return AuthorizationDecision{}, err
	}
	return AuthorizationDecision{
		Allowed:        false,
		Reason:         reason,
		DecisionID:     decisionID,
		Principal:      principal,
		PolicyRevision: revision,
	}, nil
}

func (u *AuthorizationUsecase) newDecisionID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate authorization decision ID: %w", err)
	}
	if id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}
