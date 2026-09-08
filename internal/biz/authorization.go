package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

const (
	AuditActionAuthorizationDenied       AuditAction     = "iam.authorization.denied"
	AuditTargetTypeAuthorizationDecision AuditTargetType = "authorization_decision"
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
	AuthorizationReasonCredentialKindDenied AuthorizationReason = "CREDENTIAL_KIND_DENIED"
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
	OperationID     string
	Resource        string
	Actions         []string
	Scope           PermissionScope
	Obligations     []AuthorizationObligation
	CredentialKinds []CredentialKind
	PrincipalKinds  []PrincipalType
}

type CredentialKind string

const (
	CredentialKindAccessToken  CredentialKind = "access_token"
	CredentialKindAPIKey       CredentialKind = "api_key"
	CredentialKindServiceToken CredentialKind = "service_token"
)

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
	APIKeyAuthorizationReader
	LookupAuthorization(context.Context, TenantScope, AuthorizationLookup) (AuthorizationState, error)
	RecordDeniedAuthorization(context.Context, TenantScope, SecurityAuditEvent) error
	RecordUnboundAuthorization(context.Context, SecurityAuditEvent) error
}

type APIKeyAuthorizationState struct {
	APIKey            APIKey
	TenantID          uuid.UUID
	PrincipalStatus   PrincipalStatus
	MembershipStatus  MembershipStatus
	TenantAccess      TenantAccessStatus
	Lifecycle         TenantLifecycleStatus
	LifecycleFresh    bool
	PermissionAllowed bool
}

type APIKeyAuthorizationReader interface {
	GetAPIKeyBoundary(context.Context, uuid.UUID) (uuid.UUID, error)
	LookupAPIKeyCredential(context.Context, TenantScope, uuid.UUID, string) (APIKey, error)
	LookupAPIKeyAuthorization(context.Context, TenantScope, uuid.UUID, string, []string) (APIKeyAuthorizationState, error)
}

type APIKeyUsageObserver interface {
	ObserveAPIKeyUse(context.Context, TenantScope, uuid.UUID, time.Time) error
}

type TrustedPrincipalContext struct {
	ID           uuid.UUID
	Type         PrincipalType
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
	RequestID        string
	CorrelationID    string
}

type AuthorizationUsecase struct {
	registry AuthorizationPolicyRegistry
	verifier AccessCredentialVerifier
	reader   AuthorizationReader
	usage    APIKeyUsageObserver
	ids      IDGenerator
	clock    Clock
}

func NewAuthorizationUsecase(
	registry AuthorizationPolicyRegistry,
	verifier AccessCredentialVerifier,
	reader AuthorizationReader,
	ids IDGenerator,
	clock Clock,
	observer APIKeyUsageObserver,
) *AuthorizationUsecase {
	return &AuthorizationUsecase{registry: registry, verifier: verifier, reader: reader, usage: observer, ids: ids, clock: clock}
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
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrAuthorizationCredentialRequired,
		)
	}
	if strings.HasPrefix(credential, "ani_") {
		return u.checkAPIKey(ctx, credential, command, policy)
	}
	if !credentialKindAllowed(policy.CredentialKinds, CredentialKindAccessToken) || !principalKindAllowed(policy.PrincipalKinds, PrincipalTypeHuman) {
		return u.denyUnbound(ctx, command, AuthorizationReasonCredentialKindDenied)
	}
	claims, err := u.verifier.Verify(ctx, credential)
	if err != nil {
		if errors.Is(err, ErrAuthorizationDependency) {
			return AuthorizationDecision{}, err
		}
		failure := errors.Join(ErrAuthorizationCredentialInvalid, err)
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, failure,
		)
	}
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.GrantVersion <= 0 || claims.ExpiresAt.IsZero() || !u.clock.Now().UTC().Before(claims.ExpiresAt.UTC()) {
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrAuthorizationCredentialInvalid,
		)
	}

	principal := TrustedPrincipalContext{
		ID:           claims.Subject,
		Type:         PrincipalTypeHuman,
		Status:       PrincipalStatusActive,
		TenantID:     claims.TenantID,
		SessionID:    claims.SessionID,
		GrantID:      claims.GrantID,
		AuthnMethods: append([]AuditAuthenticationMethod(nil), claims.AuthnMethods...),
	}
	scope, err := NewTenantScope(claims.TenantID)
	if err != nil {
		return AuthorizationDecision{}, ErrAuthorizationCredentialInvalid
	}
	if claims.TenantID != command.TargetTenantID {
		return u.deny(ctx, scope, principal, command, AuthorizationReasonTenantMismatch)
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
		return u.deny(ctx, scope, principal, command, reason)
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

func (u *AuthorizationUsecase) checkAPIKey(ctx context.Context, rawCredential string, command CheckPermissionCommand, policy AuthorizationPolicy) (AuthorizationDecision, error) {
	if !credentialKindAllowed(policy.CredentialKinds, CredentialKindAPIKey) || !principalKindAllowed(policy.PrincipalKinds, PrincipalTypeService) {
		return u.denyUnbound(ctx, command, AuthorizationReasonCredentialKindDenied)
	}
	keyID, err := ParseAPIKeyCredential(rawCredential)
	if err != nil {
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, uuid.Nil, 1, ErrAuthorizationCredentialInvalid,
		)
	}
	credentialTenantID, err := u.reader.GetAPIKeyBoundary(ctx, keyID)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, 1, ErrAuthorizationCredentialInvalid,
			)
		}
		return AuthorizationDecision{}, fmt.Errorf("lookup API key boundary: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	scope, err := NewTenantScope(credentialTenantID)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("build API key tenant scope: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	now := u.clock.Now().UTC()
	credential, err := u.reader.LookupAPIKeyCredential(ctx, scope, keyID, rawCredential)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, 1, ErrAuthorizationCredentialInvalid,
			)
		}
		return AuthorizationDecision{}, fmt.Errorf("lookup API key credential: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	if credential.ID != keyID || credential.PrincipalID == uuid.Nil {
		return AuthorizationDecision{}, fmt.Errorf("validate API key credential: %w", errors.Join(ErrAuthorizationDependency, ErrInvalidPersistenceState))
	}
	if credential.Status != APIKeyStatusActive || (!credential.NeverExpires && !now.Before(credential.ExpiresAt.UTC())) {
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, keyID, credential.Version, ErrAuthorizationCredentialInvalid,
		)
	}
	state, err := u.reader.LookupAPIKeyAuthorization(ctx, scope, keyID, policy.Resource, append([]string(nil), policy.Actions...))
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, credential.Version, ErrAuthorizationCredentialInvalid,
			)
		}
		return AuthorizationDecision{}, fmt.Errorf("lookup API key authorization: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	if state.APIKey.ID != credential.ID || state.APIKey.PrincipalID != credential.PrincipalID || state.TenantID != credentialTenantID {
		return AuthorizationDecision{}, fmt.Errorf("validate API key boundary: %w", errors.Join(ErrAuthorizationDependency, ErrInvalidPersistenceState))
	}
	if state.APIKey.Status != APIKeyStatusActive || (!state.APIKey.NeverExpires && !now.Before(state.APIKey.ExpiresAt.UTC())) {
		return AuthorizationDecision{}, u.recordAuthorizationAuthenticationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, keyID, state.APIKey.Version, ErrAuthorizationCredentialInvalid,
		)
	}
	principal := TrustedPrincipalContext{
		ID: state.APIKey.PrincipalID, Type: PrincipalTypeService, Status: state.PrincipalStatus,
		TenantID: state.TenantID, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodAPIKey},
	}
	if credentialTenantID != command.TargetTenantID {
		return u.deny(ctx, scope, principal, command, AuthorizationReasonTenantMismatch)
	}
	if reason := apiKeyAuthorizationDenialReason(state); reason != "" {
		return u.deny(ctx, scope, principal, command, reason)
	}
	if u.usage == nil {
		return AuthorizationDecision{}, ErrAuthorizationDependency
	}
	if err := u.usage.ObserveAPIKeyUse(ctx, scope, keyID, now); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("observe API key use: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	decisionID, err := u.newDecisionID()
	if err != nil {
		return AuthorizationDecision{}, err
	}
	return AuthorizationDecision{
		Allowed: true, Reason: AuthorizationReasonAllowed, DecisionID: decisionID, Principal: principal,
		Obligations:    authorizationObligations(policy.Obligations, command.TargetResourceID, command.TargetTenantID),
		PolicyRevision: command.PolicyRevision,
	}, nil
}

func ParseAPIKeyCredential(raw string) (uuid.UUID, error) {
	parts := strings.SplitN(strings.TrimPrefix(raw, "ani_"), "_", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return uuid.Nil, ErrAuthorizationCredentialInvalid
	}
	keyID, err := uuid.Parse(parts[0])
	if err != nil || keyID == uuid.Nil {
		return uuid.Nil, ErrAuthorizationCredentialInvalid
	}
	return keyID, nil
}

func credentialKindAllowed(values []CredentialKind, expected CredentialKind) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func principalKindAllowed(values []PrincipalType, expected PrincipalType) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func apiKeyAuthorizationDenialReason(state APIKeyAuthorizationState) AuthorizationReason {
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
	case !state.PermissionAllowed:
		return AuthorizationReasonPermissionDenied
	default:
		return ""
	}
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

func (u *AuthorizationUsecase) deny(
	ctx context.Context,
	scope TenantScope,
	principal TrustedPrincipalContext,
	command CheckPermissionCommand,
	reason AuthorizationReason,
) (AuthorizationDecision, error) {
	ids, err := u.newDecisionIDs(2)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	decisionID := ids[0]
	method := firstAuthenticationMethod(principal.AuthnMethods)
	actorID := principal.ID
	if actorID == uuid.Nil || method == "" {
		actorID = uuid.Nil
		method = AuditAuthenticationMethodAnonymous
	}
	now := u.clock.Now().UTC()
	event := principalValidationAuditEvent(
		ids[1], decisionID.String(), ValidatePrincipalCommand{
			RequestID: command.RequestID, CorrelationID: command.CorrelationID,
		}, actorID, method, AuditBoundaryTenant, AuditActionAuthorizationDenied,
		AuditTargetTypeAuthorizationDecision, decisionID, 1, AuditResultFailed, AuditReason(reason), now,
	)
	if err := u.reader.RecordDeniedAuthorization(ctx, scope, event); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("record denied authorization: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	return AuthorizationDecision{
		Allowed:        false,
		Reason:         reason,
		DecisionID:     decisionID,
		Principal:      principal,
		PolicyRevision: command.PolicyRevision,
	}, nil
}

func (u *AuthorizationUsecase) denyUnbound(
	ctx context.Context,
	command CheckPermissionCommand,
	reason AuthorizationReason,
) (AuthorizationDecision, error) {
	ids, err := u.newDecisionIDs(2)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	decisionID := ids[0]
	now := u.clock.Now().UTC()
	event := principalValidationAuditEvent(
		ids[1], decisionID.String(), ValidatePrincipalCommand{
			RequestID: command.RequestID, CorrelationID: command.CorrelationID,
		}, uuid.Nil, AuditAuthenticationMethodAnonymous, AuditBoundaryPrincipal, AuditActionAuthorizationDenied,
		AuditTargetTypeAuthorizationDecision, decisionID, 1, AuditResultFailed, AuditReason(reason), now,
	)
	if err := u.reader.RecordUnboundAuthorization(ctx, event); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("record unbound denied authorization: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	return AuthorizationDecision{
		Allowed: false, Reason: reason, DecisionID: decisionID, PolicyRevision: command.PolicyRevision,
	}, nil
}

func (u *AuthorizationUsecase) newDecisionIDs(count int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		id, err := u.newDecisionID()
		if err != nil {
			return nil, err
		}
		ids[index] = id
	}
	return ids, nil
}

func (u *AuthorizationUsecase) recordAuthorizationAuthenticationFailure(
	ctx context.Context,
	scope *TenantScope,
	command CheckPermissionCommand,
	actorID uuid.UUID,
	method AuditAuthenticationMethod,
	targetType AuditTargetType,
	targetID uuid.UUID,
	targetVersion int64,
	cause error,
) error {
	eventID, err := u.newDecisionID()
	if err != nil {
		return err
	}
	if actorID == uuid.Nil || method == "" {
		actorID = uuid.Nil
		method = AuditAuthenticationMethodAnonymous
	}
	if targetID == uuid.Nil {
		targetID = eventID
	}
	if targetVersion <= 0 {
		targetVersion = 1
	}
	boundary := AuditBoundaryPrincipal
	if scope != nil {
		boundary = AuditBoundaryTenant
	}
	event := principalValidationAuditEvent(
		eventID, eventID.String(), ValidatePrincipalCommand{
			RequestID: command.RequestID, CorrelationID: command.CorrelationID,
		}, actorID, method, boundary, AuditActionPrincipalValidationFailed,
		targetType, targetID, targetVersion, AuditResultFailed,
		principalValidationFailureReason(cause), u.clock.Now().UTC(),
	)
	if scope == nil {
		err = u.reader.RecordUnboundAuthorization(ctx, event)
	} else {
		err = u.reader.RecordDeniedAuthorization(ctx, *scope, event)
	}
	if err != nil {
		return fmt.Errorf("record authorization authentication failure: %w", errors.Join(ErrAuthorizationDependency, err))
	}
	return cause
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
