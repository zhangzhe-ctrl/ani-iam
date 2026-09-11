package biz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SessionInvocationAudience  = "ani-session-gateway"
	SessionInvocationOperation = "session.create"
	SessionInvocationRPC       = "/ani.session.v1.SessionService/CreateSession"
	WorkloadTokenMaxTTL        = 5 * time.Minute
	DelegationMaxTTL           = time.Minute
)

var (
	ErrInvocationInvalid           = errors.New("Workload invocation binding is invalid")
	ErrInvocationCredentialInvalid = errors.New("Workload invocation credential is invalid")
)

// InvocationBinding includes only authorization dimensions and the digest of
// the owner's actual normalized DTO. IAM never owns the resource's state.
const SessionContinuationMaxTTL = 16 * time.Minute
const VerifySessionContinuationRPC = "/iam.v1.AuthorizationService/VerifySessionContinuation"

type InvocationBinding struct {
	Audience        string
	Operation       string
	RPCMethod       string
	SourceOperation string
	TenantID        uuid.UUID
	SubjectID       uuid.UUID
	ResourceID      string
	Mode            string
	RequestSHA256   [32]byte
	PolicyRevision  string
}

func (b InvocationBinding) Target() WorkloadTarget {
	return WorkloadTarget{Audience: b.Audience, Operation: b.Operation}
}

func (b InvocationBinding) Validate() error {
	if b.Audience != SessionInvocationAudience || b.Operation != SessionInvocationOperation || b.RPCMethod != SessionInvocationRPC ||
		b.TenantID == uuid.Nil || b.SubjectID == uuid.Nil || b.ResourceID == "" || b.ResourceID != strings.TrimSpace(b.ResourceID) ||
		len(b.ResourceID) > 256 || b.RequestSHA256 == ([32]byte{}) || b.PolicyRevision == "" {
		return ErrInvocationInvalid
	}
	if (b.SourceOperation == "createInstanceExecSession" && b.Mode == "exec") || (b.SourceOperation == "createInstanceConsoleSession" && b.Mode == "vm_console") {
		return nil
	}
	return ErrInvocationInvalid
}

// WorkloadTokenClaims contain a directly authenticated caller and one grant.
// Neither the profile's platform owner nor this token implies Tenant authority.
type WorkloadTokenClaims struct {
	ID        uuid.UUID
	Caller    DirectCaller
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type DelegatedSubject struct {
	CredentialExpiresAt time.Time
	Principal           TrustedPrincipalContext
	GrantVersion        int64
	APIKeyID            uuid.UUID
	APIKeyVersion       int64
}

type DelegationClaims struct {
	ID              uuid.UUID
	WorkloadTokenID uuid.UUID
	Caller          DirectCaller
	Subject         DelegatedSubject
	Binding         InvocationBinding
	IssuedAt        time.Time
	ExpiresAt       time.Time
}

type SessionContinuationClaims struct {
	ID                  uuid.UUID
	Caller              DirectCaller
	Receiver            DirectCaller
	Subject             DelegatedSubject
	Binding             InvocationBinding
	IssuedAt, ExpiresAt time.Time
}

type WorkloadCredentialCodec interface {
	IssueContinuation(context.Context, SessionContinuationClaims) (string, error)
	VerifyContinuation(context.Context, string) (SessionContinuationClaims, error)
	IssueWorkload(context.Context, WorkloadTokenClaims) (string, error)
	VerifyWorkload(context.Context, string) (WorkloadTokenClaims, error)
	IssueDelegation(context.Context, DelegationClaims) (string, error)
	VerifyDelegation(context.Context, string) (DelegationClaims, error)
}

type WorkloadCredentialAudit interface {
	AppendCredentialIssue(context.Context, uuid.UUID, SecurityAuditEvent) error
}

type IssuedWorkloadCredential struct {
	Value       string
	ExpiresAt   time.Time
	PrincipalID uuid.UUID
}

type VerifiedInvocation struct {
	Continuation          string
	ContinuationExpiresAt time.Time
	Caller                DirectCaller
	Subject               TrustedPrincipalContext
	Binding               InvocationBinding
	ExpiresAt             time.Time
}

type WorkloadInvocation struct {
	identities *WorkloadAuthentication
	grants     *WorkloadAuthorization
	subjects   *AuthorizationUsecase
	codec      WorkloadCredentialCodec
	audit      WorkloadCredentialAudit
	ids        IDGenerator
	clock      Clock
}

func NewWorkloadInvocation(identities *WorkloadAuthentication, grants *WorkloadAuthorization, subjects *AuthorizationUsecase, codec WorkloadCredentialCodec, audit WorkloadCredentialAudit, ids IDGenerator, clock Clock) *WorkloadInvocation {
	return &WorkloadInvocation{identities: identities, grants: grants, subjects: subjects, codec: codec, audit: audit, ids: ids, clock: clock}
}

func (u *WorkloadInvocation) IssueWorkloadToken(ctx context.Context, target WorkloadTarget) (IssuedWorkloadCredential, error) {
	ingress, ok := DirectCallerFromContext(ctx)
	if !ok || ingress.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/IssueWorkloadToken"}) {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	if target != (WorkloadTarget{Audience: SessionInvocationAudience, Operation: SessionInvocationOperation}) && !notificationTarget(target) {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	caller, err := u.grants.Authorize(ctx, ingress.Identity, target)
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	id, err := u.ids.NewID()
	if err != nil {
		return IssuedWorkloadCredential{}, ErrPersistenceUnavailable
	}
	now := u.clock.Now().UTC().Truncate(time.Second)
	claims := WorkloadTokenClaims{ID: id, Caller: caller, IssuedAt: now, ExpiresAt: now.Add(WorkloadTokenMaxTTL)}
	token, err := u.codec.IssueWorkload(ctx, claims)
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	if err = u.recordIssue(ctx, ingress, claims.ID, uuid.Nil, ingress.Identity.PrincipalID, ingress.Identity.PrincipalVersion, "workload.token.issue", "workload_principal", now); err != nil {
		return IssuedWorkloadCredential{}, err
	}
	return IssuedWorkloadCredential{Value: token, ExpiresAt: claims.ExpiresAt, PrincipalID: caller.Identity.PrincipalID}, nil
}

func (u *WorkloadInvocation) IssueDelegation(ctx context.Context, rawWAT, rawSubject string, binding InvocationBinding) (IssuedWorkloadCredential, error) {
	ingress, ok := DirectCallerFromContext(ctx)
	if !ok || ingress.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/IssueDelegation"}) {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	if err := binding.Validate(); err != nil {
		return IssuedWorkloadCredential{}, err
	}
	wat, err := u.codec.VerifyWorkload(ctx, rawWAT)
	if err != nil {
		return IssuedWorkloadCredential{}, ErrInvocationCredentialInvalid
	}
	if _, err = u.currentCaller(ctx, wat, ingress.Identity.Peer); err != nil {
		return IssuedWorkloadCredential{}, err
	}
	if wat.Caller.Target != binding.Target() {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	decision, err := u.subjects.CheckPermission(ctx, CheckPermissionCommand{RawCredential: rawSubject, OperationID: binding.SourceOperation, PolicyRevision: binding.PolicyRevision, TargetTenantID: binding.TenantID, TargetResourceID: binding.ResourceID, RequestID: wat.ID.String(), CorrelationID: wat.ID.String()})
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	if !decision.Allowed || decision.Principal.ID != binding.SubjectID || decision.Principal.TenantID != binding.TenantID {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	subject, subjectExpiry, err := u.subjectReference(ctx, rawSubject, decision.Principal)
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	now := u.clock.Now().UTC().Truncate(time.Second)
	expires := now.Add(DelegationMaxTTL)
	for _, limit := range []time.Time{wat.ExpiresAt, subjectExpiry} {
		if !limit.IsZero() && limit.Before(expires) {
			expires = limit
		}
	}
	expires = expires.UTC().Truncate(time.Second)
	if !expires.After(now) {
		return IssuedWorkloadCredential{}, ErrInvocationCredentialInvalid
	}
	id, err := u.ids.NewID()
	if err != nil {
		return IssuedWorkloadCredential{}, ErrPersistenceUnavailable
	}
	claims := DelegationClaims{ID: id, WorkloadTokenID: wat.ID, Caller: wat.Caller, Subject: subject, Binding: binding, IssuedAt: now, ExpiresAt: expires}
	token, err := u.codec.IssueDelegation(ctx, claims)
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	targetID, version, targetType := subject.Principal.GrantID, subject.GrantVersion, AuditTargetType("session_grant")
	if subject.APIKeyID != uuid.Nil {
		targetID, version, targetType = subject.APIKeyID, subject.APIKeyVersion, AuditTargetType("api_key")
	}
	if err = u.recordIssue(ctx, ingress, id, binding.TenantID, targetID, version, "workload.delegation.issue", targetType, now); err != nil {
		return IssuedWorkloadCredential{}, err
	}
	return IssuedWorkloadCredential{Value: token, ExpiresAt: expires, PrincipalID: subject.Principal.ID}, nil
}

func (u *WorkloadInvocation) Verify(ctx context.Context, rawWAT, rawDelegation string, binding InvocationBinding, observed VerifiedWorkloadPeer) (VerifiedInvocation, error) {
	receiver, ok := DirectCallerFromContext(ctx)
	if !ok || receiver.Target != (WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}) {
		return VerifiedInvocation{}, ErrWorkloadPermissionDenied
	}
	if err := binding.Validate(); err != nil {
		return VerifiedInvocation{}, err
	}
	if observed.Environment != receiver.Identity.Peer.Environment || observed.TrustDomain != receiver.Identity.Peer.TrustDomain {
		return VerifiedInvocation{}, ErrWorkloadIdentityInvalid
	}
	wat, err := u.codec.VerifyWorkload(ctx, rawWAT)
	if err != nil {
		return VerifiedInvocation{}, ErrInvocationCredentialInvalid
	}
	caller, err := u.currentCaller(ctx, wat, observed)
	if err != nil {
		return VerifiedInvocation{}, err
	}
	delegation, err := u.codec.VerifyDelegation(ctx, rawDelegation)
	if err != nil {
		return VerifiedInvocation{}, ErrInvocationCredentialInvalid
	}
	if delegation.WorkloadTokenID != wat.ID || delegation.Caller != caller || delegation.Binding != binding || caller.Target != binding.Target() || delegation.ExpiresAt.After(wat.ExpiresAt) {
		return VerifiedInvocation{}, ErrWorkloadPermissionDenied
	}
	if err = u.currentSubject(ctx, delegation.Subject, binding); err != nil {
		return VerifiedInvocation{}, err
	}
	now := u.clock.Now().UTC().Truncate(time.Second)
	expires := now.Add(SessionContinuationMaxTTL)
	if !delegation.Subject.CredentialExpiresAt.IsZero() && delegation.Subject.CredentialExpiresAt.Before(expires) {
		expires = delegation.Subject.CredentialExpiresAt.UTC().Truncate(time.Second)
	}
	if !expires.After(now) {
		return VerifiedInvocation{}, ErrInvocationCredentialInvalid
	}
	id, err := u.ids.NewID()
	if err != nil {
		return VerifiedInvocation{}, ErrPersistenceUnavailable
	}
	continuation, err := u.codec.IssueContinuation(ctx, SessionContinuationClaims{ID: id, Caller: caller, Receiver: receiver, Subject: delegation.Subject, Binding: binding, IssuedAt: now, ExpiresAt: expires})
	if err != nil {
		return VerifiedInvocation{}, err
	}
	return VerifiedInvocation{Caller: caller, Subject: delegation.Subject.Principal, Binding: binding, ExpiresAt: delegation.ExpiresAt, Continuation: continuation, ContinuationExpiresAt: expires}, nil
}

func (u *WorkloadInvocation) currentCaller(ctx context.Context, wat WorkloadTokenClaims, peer VerifiedWorkloadPeer) (DirectCaller, error) {
	identity, err := u.identities.Authenticate(ctx, peer)
	if err != nil {
		return DirectCaller{}, err
	}
	if identity != wat.Caller.Identity {
		return DirectCaller{}, ErrWorkloadIdentityInvalid
	}
	caller, err := u.grants.Authorize(ctx, identity, wat.Caller.Target)
	if err != nil {
		return DirectCaller{}, err
	}
	if caller != wat.Caller {
		return DirectCaller{}, ErrWorkloadPermissionDenied
	}
	return caller, nil
}

func (u *WorkloadInvocation) subjectReference(ctx context.Context, raw string, p TrustedPrincipalContext) (DelegatedSubject, time.Time, error) {
	if p.Type == PrincipalTypeHuman {
		c, err := u.subjects.verifier.Verify(ctx, raw)
		if err != nil {
			return DelegatedSubject{}, time.Time{}, ErrInvocationCredentialInvalid
		}
		if c.Subject != p.ID || c.TenantID != p.TenantID || c.SessionID != p.SessionID || c.GrantID != p.GrantID {
			return DelegatedSubject{}, time.Time{}, ErrInvocationCredentialInvalid
		}
		return DelegatedSubject{Principal: p, GrantVersion: c.GrantVersion, CredentialExpiresAt: c.ExpiresAt}, c.ExpiresAt, nil
	}
	if p.Type != PrincipalTypeWorkload {
		return DelegatedSubject{}, time.Time{}, ErrInvocationCredentialInvalid
	}
	keyID, err := ParseAPIKeyCredential(raw)
	if err != nil {
		return DelegatedSubject{}, time.Time{}, ErrInvocationCredentialInvalid
	}
	scope, err := NewTenantScope(p.TenantID)
	if err != nil {
		return DelegatedSubject{}, time.Time{}, err
	}
	key, err := u.subjects.reader.LookupAPIKeyCredential(ctx, scope, keyID, raw)
	if err != nil {
		return DelegatedSubject{}, time.Time{}, err
	}
	if key.PrincipalID != p.ID || key.Status != APIKeyStatusActive {
		return DelegatedSubject{}, time.Time{}, ErrInvocationCredentialInvalid
	}
	expires := key.ExpiresAt
	if key.NeverExpires {
		expires = time.Time{}
	}
	return DelegatedSubject{Principal: p, APIKeyID: keyID, APIKeyVersion: key.Version, CredentialExpiresAt: expires}, expires, nil
}

func (u *WorkloadInvocation) currentSubject(ctx context.Context, s DelegatedSubject, b InvocationBinding) error {
	if s.Principal.ID != b.SubjectID || s.Principal.TenantID != b.TenantID {
		return ErrWorkloadPermissionDenied
	}
	if b.PolicyRevision != u.subjects.registry.Revision() {
		return ErrAuthorizationPolicyMismatch
	}
	policy, ok := u.subjects.registry.Lookup(b.SourceOperation)
	if !ok || policy.Scope != PermissionScopeTenant || policy.Resource != "instances" || len(policy.Actions) != 1 || policy.Actions[0] != "create" {
		return ErrAuthorizationOperationUnregistered
	}
	scope, err := NewTenantScope(b.TenantID)
	if err != nil {
		return err
	}
	if s.Principal.Type == PrincipalTypeHuman {
		if s.APIKeyID != uuid.Nil || s.GrantVersion <= 0 || s.Principal.SessionID == uuid.Nil || s.Principal.GrantID == uuid.Nil {
			return ErrInvocationCredentialInvalid
		}
		state, err := u.subjects.reader.LookupAuthorization(ctx, scope, AuthorizationLookup{PrincipalID: s.Principal.ID, SessionID: s.Principal.SessionID, GrantID: s.Principal.GrantID, ExpectedGrantVersion: s.GrantVersion, Resource: policy.Resource, Actions: policy.Actions})
		if err != nil {
			return ErrAuthorizationDependency
		}
		if authorizationDenialReason(state, s.GrantVersion) != "" {
			return ErrWorkloadPermissionDenied
		}
		return nil
	}
	if s.Principal.Type != PrincipalTypeWorkload || s.APIKeyID == uuid.Nil || s.APIKeyVersion <= 0 || s.Principal.SessionID != uuid.Nil || s.Principal.GrantID != uuid.Nil {
		return ErrInvocationCredentialInvalid
	}
	state, err := u.subjects.reader.LookupAPIKeyAuthorization(ctx, scope, s.APIKeyID, policy.Resource, policy.Actions)
	if err != nil {
		return ErrAuthorizationDependency
	}
	if state.APIKey.PrincipalID != s.Principal.ID || state.APIKey.Version != s.APIKeyVersion || state.TenantID != b.TenantID || state.APIKey.Status != APIKeyStatusActive || (!state.APIKey.NeverExpires && !u.clock.Now().Before(state.APIKey.ExpiresAt)) || apiKeyAuthorizationDenialReason(state) != "" {
		return ErrWorkloadPermissionDenied
	}
	return nil
}

func (u *WorkloadInvocation) recordIssue(ctx context.Context, caller DirectCaller, id, tenant, target uuid.UUID, version int64, action AuditAction, targetType AuditTargetType, now time.Time) error {
	if u.audit == nil {
		return ErrPersistenceUnavailable
	}
	boundary := AuditBoundaryTenant
	if tenant == uuid.Nil {
		boundary = AuditBoundaryPrincipal
	}
	event := SecurityAuditEvent{ID: id, ActorID: caller.Identity.PrincipalID, DirectCaller: caller, AuthenticationMethod: AuditAuthenticationMethodWorkloadToken, Boundary: boundary, Action: action, TargetType: targetType, TargetID: target, TargetVersion: version, Result: AuditResultSucceeded, Reason: "CURRENT_AUTHORITY_VERIFIED", RequestID: id.String(), CorrelationID: id.String(), DecisionID: id.String(), SourceService: AuditSourceServiceIAM, OccurredAt: now, RecordedAt: now}
	if err := u.audit.AppendCredentialIssue(ctx, tenant, event); err != nil {
		return ErrPersistenceUnavailable
	}
	return nil
}

// VerifyContinuation can only recheck the admitted request for its receiver.
// It does not accept the reference as a creation credential or renew its life.
func (u *WorkloadInvocation) VerifyContinuation(ctx context.Context, raw string, binding InvocationBinding) (VerifiedInvocation, error) {
	receiver, ok := DirectCallerFromContext(ctx)
	if !ok || receiver.Target != (WorkloadTarget{Audience: "ani-iam", Operation: VerifySessionContinuationRPC}) {
		return VerifiedInvocation{}, ErrWorkloadPermissionDenied
	}
	if err := binding.Validate(); err != nil {
		return VerifiedInvocation{}, err
	}
	proof, err := u.codec.VerifyContinuation(ctx, raw)
	if err != nil {
		return VerifiedInvocation{}, err
	}
	if proof.Binding != binding || proof.Receiver.Identity != receiver.Identity {
		return VerifiedInvocation{}, ErrWorkloadPermissionDenied
	}
	originalVerification, err := u.grants.Authorize(ctx, receiver.Identity, proof.Receiver.Target)
	if err != nil {
		return VerifiedInvocation{}, err
	}
	if originalVerification != proof.Receiver {
		return VerifiedInvocation{}, ErrWorkloadPermissionDenied
	}
	caller, err := u.currentCaller(ctx, WorkloadTokenClaims{Caller: proof.Caller}, proof.Caller.Identity.Peer)
	if err != nil {
		return VerifiedInvocation{}, err
	}
	if err := u.currentSubject(ctx, proof.Subject, binding); err != nil {
		return VerifiedInvocation{}, err
	}
	return VerifiedInvocation{Caller: caller, Subject: proof.Subject.Principal, Binding: binding, ExpiresAt: proof.ExpiresAt}, nil
}
