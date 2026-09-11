package biz

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrWorkloadIdentityInvalid  = errors.New("Workload identity is invalid")
	ErrWorkloadPermissionDenied = errors.New("Workload target is not authorized")
)

// VerifiedWorkloadPeer is produced only after transport verifies the peer's
// certificate chain, usage and unambiguous identity. It grants no authority.
type VerifiedWorkloadPeer struct {
	Environment   string
	TrustDomain   string
	IdentityKind  string
	IdentityValue string
}

type WorkloadIdentity struct {
	PrincipalID      uuid.UUID
	BindingID        uuid.UUID
	PrincipalVersion int64
	BindingVersion   int64
	Peer             VerifiedWorkloadPeer
}

type WorkloadTarget struct {
	Audience  string
	Operation string
}

// DirectCaller is transport-independent provenance. Subject authority remains
// a separate IAM evaluation; an ingress Grant never makes the caller an admin.
type DirectCaller struct {
	Identity     WorkloadIdentity
	Target       WorkloadTarget
	GrantVersion int64
}

type WorkloadIdentityReader interface {
	ResolveWorkloadIdentity(context.Context, VerifiedWorkloadPeer) (WorkloadIdentity, error)
}

type WorkloadGrantReader interface {
	CheckWorkloadGrant(context.Context, WorkloadIdentity, WorkloadTarget) (int64, error)
}

type WorkloadAuthentication struct{ identities WorkloadIdentityReader }

func NewWorkloadAuthentication(identities WorkloadIdentityReader) *WorkloadAuthentication {
	return &WorkloadAuthentication{identities: identities}
}

func (a *WorkloadAuthentication) Authenticate(ctx context.Context, peer VerifiedWorkloadPeer) (WorkloadIdentity, error) {
	if peer.Environment == "" || peer.TrustDomain == "" || peer.IdentityKind != "x509_dns" ||
		peer.IdentityValue == "" || peer.IdentityValue != strings.ToLower(strings.TrimSpace(peer.IdentityValue)) ||
		strings.ContainsAny(peer.IdentityValue, " */\t\r\n") {
		return WorkloadIdentity{}, ErrWorkloadIdentityInvalid
	}
	if a == nil || a.identities == nil {
		return WorkloadIdentity{}, ErrPersistenceUnavailable
	}
	return a.identities.ResolveWorkloadIdentity(ctx, peer)
}

type WorkloadAuthorization struct{ grants WorkloadGrantReader }

func NewWorkloadAuthorization(grants WorkloadGrantReader) *WorkloadAuthorization {
	return &WorkloadAuthorization{grants: grants}
}

func (a *WorkloadAuthorization) Authorize(ctx context.Context, identity WorkloadIdentity, target WorkloadTarget) (DirectCaller, error) {
	if identity.PrincipalID == uuid.Nil || identity.BindingID == uuid.Nil || identity.BindingVersion <= 0 || identity.PrincipalVersion <= 0 {
		return DirectCaller{}, ErrWorkloadIdentityInvalid
	}
	if (target.Audience != "ani-iam" && target != (WorkloadTarget{Audience: SessionInvocationAudience, Operation: SessionInvocationOperation}) && !notificationTarget(target)) || target.Operation == "" {
		return DirectCaller{}, ErrWorkloadPermissionDenied
	}
	if a == nil || a.grants == nil {
		return DirectCaller{}, ErrPersistenceUnavailable
	}
	version, err := a.grants.CheckWorkloadGrant(ctx, identity, target)
	if err != nil {
		return DirectCaller{}, err
	}
	if version <= 0 {
		return DirectCaller{}, ErrWorkloadPermissionDenied
	}
	return DirectCaller{Identity: identity, Target: target, GrantVersion: version}, nil
}

type directCallerContextKey struct{}

func WithDirectCaller(ctx context.Context, caller DirectCaller) context.Context {
	return context.WithValue(ctx, directCallerContextKey{}, caller)
}

func DirectCallerFromContext(ctx context.Context) (DirectCaller, bool) {
	caller, ok := ctx.Value(directCallerContextKey{}).(DirectCaller)
	return caller, ok && caller.Identity.PrincipalID != uuid.Nil && caller.Identity.BindingID != uuid.Nil && caller.GrantVersion > 0
}
