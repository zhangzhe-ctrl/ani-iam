package biz

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type changingWorkloadTrust struct {
	identity       WorkloadIdentity
	grant          int64
	bindingRevoked bool
	failure        error
	target         WorkloadTarget
}

func (r *changingWorkloadTrust) ResolveWorkloadIdentity(_ context.Context, p VerifiedWorkloadPeer) (WorkloadIdentity, error) {
	if r.failure != nil {
		return WorkloadIdentity{}, r.failure
	}
	if r.bindingRevoked || p != r.identity.Peer {
		return WorkloadIdentity{}, ErrWorkloadIdentityInvalid
	}
	return r.identity, nil
}
func (r *changingWorkloadTrust) CheckWorkloadGrant(_ context.Context, i WorkloadIdentity, target WorkloadTarget) (int64, error) {
	if r.failure != nil {
		return 0, r.failure
	}
	if r.bindingRevoked || i != r.identity || target != r.target || r.grant == 0 {
		return 0, ErrWorkloadPermissionDenied
	}
	return r.grant, nil
}

func TestDirectWorkloadCallerRequiresCurrentIdentityAndSeparateGrant(t *testing.T) {
	ctx := context.Background()
	identity := WorkloadIdentity{PrincipalID: uuid.MustParse("01993000-0000-7000-8000-000000000001"), BindingID: uuid.MustParse("01993000-0000-7000-8000-000000000002"), PrincipalVersion: 1, BindingVersion: 1, Peer: VerifiedWorkloadPeer{Environment: "wr17-18-isolated", TrustDomain: "iam.wr17-18.test", IdentityKind: "x509_dns", IdentityValue: "ani-gateway"}}
	target := WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/PasswordLogin"}
	trust := &changingWorkloadTrust{identity: identity, target: target, grant: 1}
	authentication, authorization := NewWorkloadAuthentication(trust), NewWorkloadAuthorization(trust)
	current, err := authentication.Authenticate(ctx, identity.Peer)
	if err != nil {
		t.Fatal(err)
	}
	caller, err := authorization.Authorize(ctx, current, target)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := DirectCallerFromContext(WithDirectCaller(ctx, caller))
	if !ok || got != caller {
		t.Fatal("verified caller provenance lost")
	}
	if _, ok := DirectCallerFromContext(ctx); ok {
		t.Fatal("no transport proof produced a caller")
	}
	trust.grant = 0
	if _, err := authorization.Authorize(ctx, current, target); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatalf("revoked Grant accepted=%v", err)
	}
	trust.grant = 2
	if _, err := authorization.Authorize(ctx, current, WorkloadTarget{Audience: "other-service", Operation: target.Operation}); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatalf("foreign target accepted=%v", err)
	}
	trust.bindingRevoked = true
	if _, err := authentication.Authenticate(ctx, identity.Peer); !errors.Is(err, ErrWorkloadIdentityInvalid) {
		t.Fatalf("revoked binding accepted=%v", err)
	}
	if _, err := authorization.Authorize(ctx, current, target); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatalf("old resolved identity accepted=%v", err)
	}
	trust.failure = ErrPersistenceUnavailable
	if _, err := authentication.Authenticate(ctx, identity.Peer); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("dependency failure fell back=%v", err)
	}
	if _, err := authorization.Authorize(ctx, current, target); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("grant dependency failure fell back=%v", err)
	}
}
