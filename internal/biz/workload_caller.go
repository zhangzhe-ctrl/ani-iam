package biz

import (
	"context"
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

func notificationTarget(target WorkloadTarget) bool {
	return target.Audience == NotificationAudience && (target.Operation == NotificationSubmitOperation || target.Operation == NotificationGetOwnOperation)
}
func notificationRPC(target WorkloadTarget, method string) bool {
	return notificationTarget(target) && ((target.Operation == NotificationSubmitOperation && method == NotificationSubmitRPC) || (target.Operation == NotificationGetOwnOperation && method == NotificationGetOwnRPC))
}

type VerifiedWorkloadCaller struct {
	Caller    DirectCaller
	ExpiresAt time.Time
}

// IssueLocalWorkloadToken uses the same current identity, ingress and target
// authorization as the public issuer. Only composition supplies the local
// certificate identity; no caller-provided business field enters this path.
func (u *WorkloadInvocation) IssueLocalWorkloadToken(ctx context.Context, peer VerifiedWorkloadPeer, target WorkloadTarget) (IssuedWorkloadCredential, error) {
	if peer.IdentityValue != "ani-iam."+peer.TrustDomain || !notificationTarget(target) {
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
	return u.IssueWorkloadToken(WithDirectCaller(ctx, ingress), target)
}

// VerifyCaller is intentionally limited to Notification's two registered
// operations. The receiver's exact IAM ingress Grant authorizes verification
// for this sole audience; it conveys no submission or Human authority.
func (u *WorkloadInvocation) VerifyCaller(ctx context.Context, raw string, target WorkloadTarget, method string, observed VerifiedWorkloadPeer) (VerifiedWorkloadCaller, error) {
	receiver, ok := DirectCallerFromContext(ctx)
	if !ok || receiver.Target != (WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}) {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
	}
	if !notificationRPC(target, method) {
		return VerifiedWorkloadCaller{}, ErrWorkloadPermissionDenied
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
	caller, err := u.currentCaller(ctx, wat, observed)
	if err != nil {
		return VerifiedWorkloadCaller{}, err
	}
	return VerifiedWorkloadCaller{Caller: caller, ExpiresAt: wat.ExpiresAt}, nil
}
