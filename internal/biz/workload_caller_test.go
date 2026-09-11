package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type callerCodec struct {
	WorkloadCredentialCodec
	wat WorkloadTokenClaims
}

func (c *callerCodec) VerifyWorkload(_ context.Context, raw string) (WorkloadTokenClaims, error) {
	if raw != "valid" {
		return WorkloadTokenClaims{}, ErrInvocationCredentialInvalid
	}
	return c.wat, nil
}
func TestWorkloadOnlyVerificationRechecksAuthorityAndExactReceiverTarget(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	target := WorkloadTarget{Audience: NotificationAudience, Operation: NotificationSubmitOperation}
	identity := WorkloadIdentity{PrincipalID: uuid.New(), BindingID: uuid.New(), PrincipalVersion: 1, BindingVersion: 1, Peer: VerifiedWorkloadPeer{Environment: "wr20", TrustDomain: "wr20.test", IdentityKind: "x509_dns", IdentityValue: "ani-iam"}}
	trust := &changingWorkloadTrust{identity: identity, target: target, grant: 1}
	codec := &callerCodec{wat: WorkloadTokenClaims{ID: uuid.Must(uuid.NewV7()), Caller: DirectCaller{Identity: identity, Target: target, GrantVersion: 1}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}}
	u := NewWorkloadInvocation(NewWorkloadAuthentication(trust), NewWorkloadAuthorization(trust), nil, codec, nil, nil, fixedAuthClock{now})
	receiver := DirectCaller{Identity: identity, Target: WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}, GrantVersion: 1}
	ctx := WithDirectCaller(context.Background(), receiver)
	result, err := u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer)
	if err != nil || result.Caller != codec.wat.Caller {
		t.Fatal("valid Workload-only call rejected")
	}
	if _, err = u.VerifyCaller(context.Background(), "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("receiver authority missing accepted")
	}
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationGetOwnRPC, identity.Peer); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("RPC mismatch accepted")
	}
	trust.grant = 2
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("changed Grant accepted")
	}
	trust.grant = 1
	trust.bindingRevoked = true
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrWorkloadIdentityInvalid) {
		t.Fatal("revoked binding accepted")
	}
	trust.bindingRevoked = false
	trust.identity.PrincipalVersion++
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrWorkloadIdentityInvalid) {
		t.Fatal("changed Principal accepted")
	}
	trust.identity = identity
	trust.failure = ErrPersistenceUnavailable
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal("database failure admitted")
	}
	trust.failure = nil
	codec.wat.ExpiresAt = now
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer); !errors.Is(err, ErrInvocationCredentialInvalid) {
		t.Fatal("expiry accepted")
	}
}
