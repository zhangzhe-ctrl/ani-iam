package biz

import (
	"context"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"time"
)

const (
	NotificationAudience        = "ani-notification-service"
	NotificationSubmitOperation = "notification.submit"
	NotificationGetOwnOperation = "notification.get_own"
	NotificationSubmitRPC       = "/notification.v1.NotificationService/SubmitNotification"
	NotificationGetOwnRPC       = "/notification.v1.NotificationService/GetSubmissionStatus"
	VerifyWorkloadCallerRPC     = "/iam.v1.AuthorizationService/VerifyWorkloadCaller"
)

type VerifiedWorkloadCaller struct {
	Caller            DirectCaller
	ExpiresAt         time.Time
	AuthorityRevision string
}

// IssueLocalWorkloadToken uses the same current identity, ingress and target
// authorization as the public issuer. Only composition supplies the local
// certificate identity; no caller-provided business field enters this path.
func (u *WorkloadInvocation) IssueLocalWorkloadToken(ctx context.Context, peer VerifiedWorkloadPeer, target WorkloadTarget) (IssuedWorkloadCredential, error) {
	registration, ok := u.grants.registry.Lookup(target.Audience, target.Operation)
	if !ok || !registration.Enabled || registration.Mechanism != workloadregistry.WorkloadOnly {
		return IssuedWorkloadCredential{}, ErrWorkloadPermissionDenied
	}
	identity, err := u.identities.Authenticate(ctx, peer)
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	ingress, err := u.grants.Authorize(ctx, identity, WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/IssueWorkloadToken"})
	if err != nil {
		return IssuedWorkloadCredential{}, err
	}
	return u.IssueWorkloadToken(WithDirectCaller(ctx, ingress), target, u.grants.registry.Revision(target.Audience, target.Operation))
}

// VerifyCaller requires both the IAM verifier ingress and the exact target receiver Grant.
func (u *WorkloadInvocation) VerifyCaller(ctx context.Context, raw string, target WorkloadTarget, method string, observed VerifiedWorkloadPeer, expectedRevision ...string) (VerifiedWorkloadCaller, error) {
	if method == "" {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	return u.verifyCaller(ctx, raw, target, method, "", "", observed, expectedRevision...)
}

// VerifyHTTPCaller is the same online mechanism with an explicit HTTP endpoint.
// The receiver supplies a peer from its actual verified TLS connection.
func (u *WorkloadInvocation) VerifyHTTPCaller(ctx context.Context, raw string, target WorkloadTarget, method, path string, observed VerifiedWorkloadPeer, revision string) (VerifiedWorkloadCaller, error) {
	if method == "" || path == "" {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	return u.verifyCaller(ctx, raw, target, "", method, path, observed, revision)
}

func (u *WorkloadInvocation) verifyCaller(ctx context.Context, raw string, target WorkloadTarget, method, httpMethod, httpPath string, observed VerifiedWorkloadPeer, expectedRevision ...string) (VerifiedWorkloadCaller, error) {
	receiver, ok := DirectCallerFromContext(ctx)
	if !ok || receiver.Target != (WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}) {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	registration, registered := u.grants.registry.Lookup(target.Audience, target.Operation)
	if !registered || !registration.Enabled || len(expectedRevision) != 1 || expectedRevision[0] != u.grants.registry.Revision(target.Audience, target.Operation) || registration.Mechanism != workloadregistry.WorkloadOnly || registration.RPC != method || registration.HTTPMethod != httpMethod || registration.HTTPPath != httpPath {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	if len(registration.AuthorityOperations) == 0 {
		if _, err := u.grants.Authorize(ctx, receiver.Identity, WorkloadTarget{Audience: target.Audience, Operation: registration.ReceiverOperation}); err != nil {
			return VerifiedWorkloadCaller{}, err
		}
	}
	if observed.Environment != receiver.Identity.Peer.Environment || observed.TrustDomain != receiver.Identity.Peer.TrustDomain {
		return VerifiedWorkloadCaller{}, ErrWorkloadIdentityInvalid
	}
	wat, err := u.codec.VerifyWorkload(ctx, raw)
	if err != nil {
		return VerifiedWorkloadCaller{}, ErrInvocationCredentialInvalid
	}
	now := u.clock.Now()
	if wat.ID == [16]byte{} || !wat.ExpiresAt.After(now) || wat.IssuedAt.After(now) || wat.ExpiresAt.Sub(wat.IssuedAt) > WorkloadTokenMaxTTL {
		return VerifiedWorkloadCaller{}, ErrInvocationCredentialInvalid
	}
	if wat.Caller.Target != target {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	if len(registration.AuthorityOperations) != 0 {
		if wat.Caller.Identity.Peer != observed {
			return VerifiedWorkloadCaller{}, ErrWorkloadIdentityInvalid
		}
		if u.authority == nil {
			return VerifiedWorkloadCaller{}, ErrPersistenceUnavailable
		}
		current, err := u.authority.ReadCurrentAuthority(ctx, receiver, wat.Caller, observed)
		if err != nil {
			return VerifiedWorkloadCaller{}, err
		}
		revision, err := workloadAuthorityRevision(u.grants.registry, wat.Caller, current)
		if err != nil {
			return VerifiedWorkloadCaller{}, err
		}
		if !u.clock.Now().Before(wat.ExpiresAt) {
			return VerifiedWorkloadCaller{}, ErrInvocationCredentialInvalid
		}
		return VerifiedWorkloadCaller{Caller: wat.Caller, ExpiresAt: wat.ExpiresAt, AuthorityRevision: revision}, nil
	}
	caller, err := u.currentCaller(ctx, wat, observed)
	if err != nil {
		return VerifiedWorkloadCaller{}, err
	}
	return VerifiedWorkloadCaller{Caller: caller, ExpiresAt: wat.ExpiresAt}, nil
}
