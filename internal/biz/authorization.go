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
	ErrAuthorizationTargetRequired         = errors.New("authorization target resource is required")
	ErrTenantIAMNotReady                   = errors.New("tenant IAM access is not ready")
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
	Scope       PermissionScope
	Obligations []AuthorizationObligation
}

type PermissionScope string

const (
	PermissionScopeTenant   PermissionScope = "tenant"
	PermissionScopePlatform PermissionScope = "platform"
	PermissionScopeOwn      PermissionScope = "own"
)

type Permission struct {
	Scope    PermissionScope
	Resource string
	Action   string
}

type PermissionCatalog interface {
	Contains(Permission) bool
	Permissions(PermissionScope) []Permission
}

type AuthorizationObligationType string

const AuthorizationObligationResourceTenantMatch AuthorizationObligationType = "resource_tenant_match"

type AuthorizationObligation struct {
	Type             AuthorizationObligationType
	Handler          string
	ResourceID       string
	ExpectedTenantID uuid.UUID
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
	Obligations    []AuthorizationObligation
	PolicyRevision string
}

type CheckPermissionCommand struct {
	RawCredential    string
	OperationID      string
	PolicyRevision   string
	TargetTenantID   uuid.UUID
	TargetResourceID string
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
	// This use case is intentionally the ordinary Tenant boundary evaluator.
	// Platform and own-scope operations require their distinct DP2 slices and
	// must never be interpreted through tenant role bindings.
	if policy.Scope != PermissionScopeTenant {
		return AuthorizationDecision{}, ErrAuthorizationOperationUnregistered
	}
	if len(policy.Obligations) > 0 && strings.TrimSpace(command.TargetResourceID) == "" {
		return AuthorizationDecision{}, ErrAuthorizationTargetRequired
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
		Obligations:    authorizationObligations(policy.Obligations, command.TargetResourceID, command.TargetTenantID),
		PolicyRevision: command.PolicyRevision,
	}, nil
}

func authorizationObligations(policies []AuthorizationObligation, resourceID string, tenantID uuid.UUID) []AuthorizationObligation {
	if len(policies) == 0 {
		return nil
	}
	obligations := make([]AuthorizationObligation, len(policies))
	for index, policy := range policies {
		obligations[index] = AuthorizationObligation{
			Type: policy.Type, Handler: policy.Handler,
			ResourceID: strings.TrimSpace(resourceID), ExpectedTenantID: tenantID,
		}
	}
	return obligations
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
