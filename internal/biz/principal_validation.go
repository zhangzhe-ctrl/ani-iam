package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrAuthenticationCredentialKindDenied = errors.New("credential kind is not allowed for the operation")

const (
	AuditActionPrincipalValidationSucceeded AuditAction     = "iam.principal.validation.succeeded"
	AuditActionPrincipalValidationFailed    AuditAction     = "iam.principal.validation.failed"
	AuditTargetTypeCredential               AuditTargetType = "credential"
	AuditReasonPrincipalValidation          AuditReason     = "PRINCIPAL_VALIDATION"
	AuditReasonPrincipalInactive            AuditReason     = "PRINCIPAL_INACTIVE"
	AuditReasonMembershipInactive           AuditReason     = "MEMBERSHIP_INACTIVE"
	AuditReasonTenantAccessInactive         AuditReason     = "TENANT_ACCESS_INACTIVE"
	AuditReasonTenantLifecycleBlocked       AuditReason     = "TENANT_LIFECYCLE_BLOCKED"
	AuditReasonTenantLifecycleStale         AuditReason     = "TENANT_LIFECYCLE_STALE"
)

type TenantBoundAuthenticationError struct {
	TenantID uuid.UUID
	Cause    error
}

func (e *TenantBoundAuthenticationError) Error() string {
	return fmt.Sprintf("tenant %s authentication failed: %v", e.TenantID, e.Cause)
}

func (e *TenantBoundAuthenticationError) Unwrap() error {
	return e.Cause
}

func TenantIDFromAuthenticationError(err error) (uuid.UUID, bool) {
	var tenantError *TenantBoundAuthenticationError
	if !errors.As(err, &tenantError) || tenantError.TenantID == uuid.Nil {
		return uuid.Nil, false
	}
	return tenantError.TenantID, true
}

func tenantBoundAuthenticationError(tenantID uuid.UUID, cause error) error {
	return &TenantBoundAuthenticationError{TenantID: tenantID, Cause: cause}
}

type ValidatePrincipalCommand struct {
	RawCredential  string
	OperationID    string
	PolicyRevision string
	RequestID      string
	CorrelationID  string
}

type ValidatePrincipalResult struct {
	Principal      TrustedPrincipalContext
	DecisionID     uuid.UUID
	PolicyRevision string
}

// PrincipalValidationReader is the narrow read seam used by the authenticated-
// only Gateway path. The operation registry is checked before either token
// verification or database access, and an API Key boundary is derived only
// from its globally unique, non-secret key identity.
type PrincipalValidationReader interface {
	AuthorizationPolicyRegistry
	AuthorizationReader
}

type PrincipalValidationAuditWriter interface {
	RecordTenantPrincipalValidation(context.Context, TenantScope, SecurityAuditEvent) error
	RecordUnboundPrincipalValidation(context.Context, SecurityAuditEvent) error
}

func (u *AuthenticationUsecase) ValidatePrincipal(ctx context.Context, command ValidatePrincipalCommand) (ValidatePrincipalResult, error) {
	policy, err := validatePrincipalPolicy(u.reader, command)
	if err != nil {
		return ValidatePrincipalResult{}, err
	}
	rawCredential := strings.TrimSpace(command.RawCredential)
	if rawCredential == "" {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrInvalidCredential,
		)
	}
	if strings.HasPrefix(rawCredential, "ani_") {
		return u.validateAPIKeyPrincipal(ctx, u.reader, policy, rawCredential, command)
	}
	return u.validateAccessTokenPrincipal(ctx, u.reader, policy, rawCredential, command)
}

func validatePrincipalPolicy(reader PrincipalValidationReader, command ValidatePrincipalCommand) (AuthorizationPolicy, error) {
	policyRevision := strings.TrimSpace(command.PolicyRevision)
	if policyRevision == "" {
		return AuthorizationPolicy{}, ErrAuthorizationPolicyRevisionRequired
	}
	if policyRevision != reader.Revision() {
		return AuthorizationPolicy{}, &AuthorizationPolicyMismatchError{Expected: reader.Revision(), Actual: policyRevision}
	}
	operationID := strings.TrimSpace(command.OperationID)
	if operationID == "" {
		return AuthorizationPolicy{}, ErrAuthorizationOperationRequired
	}
	policy, ok := reader.Lookup(operationID)
	if !ok || policy.OperationID != operationID || strings.TrimSpace(policy.Resource) == "" || len(policy.Actions) == 0 || policy.Scope != PermissionScopeTenant {
		return AuthorizationPolicy{}, ErrAuthorizationOperationUnregistered
	}
	return policy, nil
}

func (u *AuthenticationUsecase) validateAPIKeyPrincipal(
	ctx context.Context,
	reader PrincipalValidationReader,
	policy AuthorizationPolicy,
	rawCredential string,
	command ValidatePrincipalCommand,
) (ValidatePrincipalResult, error) {
	if !credentialKindAllowed(policy.CredentialKinds, CredentialKindAPIKey) || !principalKindAllowed(policy.PrincipalKinds, PrincipalTypeWorkload) {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, uuid.Nil, 1, ErrAuthenticationCredentialKindDenied,
		)
	}
	keyID, err := ParseAPIKeyCredential(rawCredential)
	if err != nil {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil,
			AuditAuthenticationMethodAnonymous, AuditTargetTypeAPIKey, uuid.Nil, 1, ErrInvalidCredential,
		)
	}
	tenantID, err := reader.GetAPIKeyBoundary(ctx, keyID)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, 1, ErrInvalidCredential,
			)
		}
		return ValidatePrincipalResult{}, fmt.Errorf("lookup API key boundary: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	scope, err := NewTenantScope(tenantID)
	if err != nil {
		return ValidatePrincipalResult{}, fmt.Errorf("build API key tenant scope: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	now := u.clock.Now().UTC()
	credential, err := reader.LookupAPIKeyCredential(ctx, scope, keyID, rawCredential)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, 1, ErrInvalidCredential,
			)
		}
		return ValidatePrincipalResult{}, fmt.Errorf("lookup API key credential: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if credential.ID != keyID || credential.PrincipalID == uuid.Nil {
		return ValidatePrincipalResult{}, fmt.Errorf("validate API key credential: %w", errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState))
	}
	if credential.Status != APIKeyStatusActive ||
		(!credential.NeverExpires && !now.Before(credential.ExpiresAt.UTC())) {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, keyID, credential.Version, ErrInvalidCredential,
		)
	}
	state, err := reader.LookupAPIKeyAuthorization(ctx, scope, keyID, policy.Resource, append([]string(nil), policy.Actions...))
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
				ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeAPIKey, keyID, credential.Version, ErrInvalidCredential,
			)
		}
		if errors.Is(err, ErrTenantIAMNotReady) || errors.Is(err, ErrTenantLifecycleStale) {
			failure := tenantBoundAuthenticationError(tenantID, err)
			return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
				ctx, &scope, command, credential.PrincipalID, AuditAuthenticationMethodAPIKey,
				AuditTargetTypeAPIKey, keyID, credential.Version, failure,
			)
		}
		return ValidatePrincipalResult{}, fmt.Errorf("lookup API key principal: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if state.APIKey.Status != APIKeyStatusActive ||
		(!state.APIKey.NeverExpires && !now.Before(state.APIKey.ExpiresAt.UTC())) {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeAPIKey, keyID, state.APIKey.Version, ErrInvalidCredential,
		)
	}
	if state.TenantID != tenantID || state.APIKey.ID != keyID ||
		state.APIKey.PrincipalID == uuid.Nil || state.APIKey.PrincipalID != credential.PrincipalID {
		return ValidatePrincipalResult{}, fmt.Errorf("validate API key boundary: %w", errors.Join(ErrAuthenticationDependency, ErrInvalidPersistenceState))
	}
	if err := validateAPIKeyPrincipalState(state); err != nil {
		failure := tenantBoundAuthenticationError(tenantID, err)
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, &scope, command, state.APIKey.PrincipalID, AuditAuthenticationMethodAPIKey,
			AuditTargetTypeAPIKey, keyID, state.APIKey.Version, failure,
		)
	}
	if u.usage == nil {
		return ValidatePrincipalResult{}, ErrAuthenticationDependency
	}
	if err := u.usage.ObserveAPIKeyUse(ctx, scope, keyID, now); err != nil {
		return ValidatePrincipalResult{}, fmt.Errorf("observe API key authentication: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return u.validatedPrincipalResult(ctx, scope, command, TrustedPrincipalContext{
		ID: state.APIKey.PrincipalID, Type: PrincipalTypeWorkload, Status: state.PrincipalStatus,
		TenantID: tenantID, AuthnMethods: []AuditAuthenticationMethod{AuditAuthenticationMethodAPIKey},
	}, AuditTargetTypeAPIKey, keyID, state.APIKey.Version)
}

func validateAPIKeyPrincipalState(state APIKeyAuthorizationState) error {
	if state.PrincipalStatus != PrincipalStatusActive {
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
	return nil
}

func (u *AuthenticationUsecase) validateAccessTokenPrincipal(
	ctx context.Context,
	reader PrincipalValidationReader,
	policy AuthorizationPolicy,
	rawCredential string,
	command ValidatePrincipalCommand,
) (ValidatePrincipalResult, error) {
	if !credentialKindAllowed(policy.CredentialKinds, CredentialKindAccessToken) || !principalKindAllowed(policy.PrincipalKinds, PrincipalTypeHuman) {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrAuthenticationCredentialKindDenied,
		)
	}
	claims, err := u.tokens.Verify(ctx, rawCredential)
	if err != nil {
		if errors.Is(err, ErrAuthenticationDependency) || errors.Is(err, ErrAuthorizationDependency) {
			return ValidatePrincipalResult{}, err
		}
		failure := errors.Join(ErrInvalidCredential, err)
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, failure,
		)
	}
	now := u.clock.Now().UTC()
	if claims.Subject == uuid.Nil || claims.SessionID == uuid.Nil || claims.GrantID == uuid.Nil || claims.TenantID == uuid.Nil ||
		claims.GrantVersion <= 0 || claims.ExpiresAt.IsZero() || !now.Before(claims.ExpiresAt.UTC()) {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrInvalidCredential,
		)
	}
	scope, err := NewTenantScope(claims.TenantID)
	if err != nil {
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, nil, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
			AuditTargetTypeCredential, uuid.Nil, 1, ErrInvalidCredential,
		)
	}
	state, err := reader.LookupAuthorization(ctx, scope, AuthorizationLookup{
		PrincipalID: claims.Subject, SessionID: claims.SessionID, GrantID: claims.GrantID,
		ExpectedGrantVersion: claims.GrantVersion, Resource: policy.Resource, Actions: append([]string(nil), policy.Actions...),
	})
	if err != nil {
		if errors.Is(err, ErrTenantIAMNotReady) || errors.Is(err, ErrTenantLifecycleStale) {
			failure := tenantBoundAuthenticationError(claims.TenantID, err)
			return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
				ctx, &scope, command, uuid.Nil, AuditAuthenticationMethodAnonymous,
				AuditTargetTypeSession, claims.SessionID, claims.GrantVersion, failure,
			)
		}
		return ValidatePrincipalResult{}, fmt.Errorf("lookup access-token principal: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	if err := validateAccessTokenPrincipalState(state, claims.GrantVersion); err != nil {
		failure := tenantBoundAuthenticationError(claims.TenantID, err)
		method := firstAuthenticationMethod(claims.AuthnMethods)
		return ValidatePrincipalResult{}, u.recordPrincipalValidationFailure(
			ctx, &scope, command, claims.Subject, method,
			AuditTargetTypeSession, claims.SessionID, claims.GrantVersion, failure,
		)
	}
	return u.validatedPrincipalResult(ctx, scope, command, TrustedPrincipalContext{
		ID: claims.Subject, Type: PrincipalTypeHuman, Status: state.PrincipalStatus,
		TenantID: claims.TenantID, SessionID: claims.SessionID, GrantID: claims.GrantID,
		AuthnMethods: append([]AuditAuthenticationMethod(nil), claims.AuthnMethods...),
	}, AuditTargetTypeSession, claims.SessionID, claims.GrantVersion)
}

func validateAccessTokenPrincipalState(state AuthorizationState, expectedGrantVersion int64) error {
	if state.PrincipalStatus != PrincipalStatusActive {
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
	if state.SessionStatus != SessionStatusActive || state.GrantStatus != GrantStatusActive || state.GrantVersion != expectedGrantVersion {
		return ErrInvalidCredential
	}
	return nil
}

func (u *AuthenticationUsecase) validatedPrincipalResult(
	ctx context.Context,
	scope TenantScope,
	command ValidatePrincipalCommand,
	principal TrustedPrincipalContext,
	targetType AuditTargetType,
	targetID uuid.UUID,
	targetVersion int64,
) (ValidatePrincipalResult, error) {
	ids, err := u.newIDs(2)
	if err != nil {
		return ValidatePrincipalResult{}, err
	}
	method := firstAuthenticationMethod(principal.AuthnMethods)
	if principal.ID == uuid.Nil || method == "" || method == AuditAuthenticationMethodAnonymous || targetID == uuid.Nil || targetVersion <= 0 {
		return ValidatePrincipalResult{}, ErrInvalidPersistenceState
	}
	now := u.clock.Now().UTC()
	event := principalValidationAuditEvent(
		ids[1], ids[0].String(), command, principal.ID, method, AuditBoundaryTenant,
		AuditActionPrincipalValidationSucceeded, targetType, targetID, targetVersion,
		AuditResultSucceeded, AuditReasonPrincipalValidation, now,
	)
	if err := u.uow.RecordTenantPrincipalValidation(ctx, scope, event); err != nil {
		return ValidatePrincipalResult{}, fmt.Errorf("record principal validation success: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return ValidatePrincipalResult{Principal: principal, DecisionID: ids[0], PolicyRevision: command.PolicyRevision}, nil
}

func (u *AuthenticationUsecase) recordPrincipalValidationFailure(
	ctx context.Context,
	scope *TenantScope,
	command ValidatePrincipalCommand,
	actorID uuid.UUID,
	method AuditAuthenticationMethod,
	targetType AuditTargetType,
	targetID uuid.UUID,
	targetVersion int64,
	cause error,
) error {
	ids, err := u.newIDs(1)
	if err != nil {
		return err
	}
	if targetID == uuid.Nil {
		targetID = ids[0]
	}
	if targetVersion <= 0 {
		targetVersion = 1
	}
	boundary := AuditBoundaryPrincipal
	if scope == nil {
		actorID = uuid.Nil
		method = AuditAuthenticationMethodAnonymous
	} else {
		boundary = AuditBoundaryTenant
		if actorID == uuid.Nil {
			method = AuditAuthenticationMethodAnonymous
		}
	}
	event := principalValidationAuditEvent(
		ids[0], ids[0].String(), command, actorID, method, boundary,
		AuditActionPrincipalValidationFailed, targetType, targetID, targetVersion,
		AuditResultFailed, principalValidationFailureReason(cause), u.clock.Now().UTC(),
	)
	if scope == nil {
		err = u.uow.RecordUnboundPrincipalValidation(ctx, event)
	} else {
		err = u.uow.RecordTenantPrincipalValidation(ctx, *scope, event)
	}
	if err != nil {
		return fmt.Errorf("record principal validation failure: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	return cause
}

func principalValidationAuditEvent(
	eventID uuid.UUID,
	decisionID string,
	command ValidatePrincipalCommand,
	actorID uuid.UUID,
	method AuditAuthenticationMethod,
	boundary AuditBoundary,
	action AuditAction,
	targetType AuditTargetType,
	targetID uuid.UUID,
	targetVersion int64,
	result AuditResult,
	reason AuditReason,
	now time.Time,
) SecurityAuditEvent {
	requestID := strings.TrimSpace(command.RequestID)
	if requestID == "" {
		requestID = eventID.String()
	}
	correlationID := strings.TrimSpace(command.CorrelationID)
	if correlationID == "" {
		correlationID = requestID
	}
	return SecurityAuditEvent{
		ID: eventID, ActorID: actorID, AuthenticationMethod: method, Boundary: boundary,
		Action: action, TargetType: targetType, TargetID: targetID, TargetVersion: targetVersion,
		Result: result, Reason: reason, RequestID: requestID, CorrelationID: correlationID,
		DecisionID: decisionID, SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now,
	}
}

func principalValidationFailureReason(err error) AuditReason {
	switch {
	case errors.Is(err, ErrPrincipalInactive):
		return AuditReasonPrincipalInactive
	case errors.Is(err, ErrMembershipInactive):
		return AuditReasonMembershipInactive
	case errors.Is(err, ErrTenantAccessInactive), errors.Is(err, ErrTenantIAMNotReady):
		return AuditReasonTenantAccessInactive
	case errors.Is(err, ErrTenantLifecycleStale):
		return AuditReasonTenantLifecycleStale
	case errors.Is(err, ErrTenantLifecycleBlocked):
		return AuditReasonTenantLifecycleBlocked
	default:
		return AuditReasonInvalidCredential
	}
}

func firstAuthenticationMethod(methods []AuditAuthenticationMethod) AuditAuthenticationMethod {
	if len(methods) == 0 {
		return ""
	}
	return methods[0]
}
