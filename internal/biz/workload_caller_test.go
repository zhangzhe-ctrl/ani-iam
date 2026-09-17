package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
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
	registry := workloadRegistryFixture(t)
	target := WorkloadTarget{Audience: NotificationAudience, Operation: NotificationSubmitOperation}
	identity := WorkloadIdentity{PrincipalID: uuid.New(), BindingID: uuid.New(), PrincipalVersion: 1, BindingVersion: 1, Peer: VerifiedWorkloadPeer{Environment: "wr20", TrustDomain: "wr20.test", IdentityKind: "x509_dns", IdentityValue: "ani-iam"}}
	receiverIdentity := identity
	receiverIdentity.PrincipalID = uuid.New()
	receiverIdentity.BindingID = uuid.New()
	trust := &changingWorkloadTrust{receiverIdentity: receiverIdentity, identity: identity, target: target, grant: 1, receiverTarget: WorkloadTarget{Audience: NotificationAudience, Operation: "notification.receive"}, receiverGrant: 1}
	codec := &callerCodec{wat: WorkloadTokenClaims{ID: uuid.Must(uuid.NewV7()), Caller: DirectCaller{Identity: identity, Target: target, GrantVersion: 1, TargetRevision: registry.Revision(target.Audience, target.Operation)}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}}
	u := NewWorkloadInvocation(NewWorkloadAuthentication(trust), NewWorkloadAuthorization(trust, registry), nil, codec, nil, nil, fixedAuthClock{now})
	receiver := DirectCaller{Identity: receiverIdentity, Target: WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}, GrantVersion: 1}
	ctx := WithDirectCaller(context.Background(), receiver)
	result, err := u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation))
	if err != nil || result.Caller != codec.wat.Caller {
		t.Fatal("valid Workload-only call rejected")
	}
	if _, err = u.VerifyCaller(context.Background(), "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("receiver authority missing accepted")
	}
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationGetOwnRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("RPC mismatch accepted")
	}
	trust.receiverGrant = 0
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("verifier ingress alone accepted for receiver audience")
	}
	trust.receiverGrant = 1
	for _, revision := range []string{"", "wrong"} {
		if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, revision); !errors.Is(err, ErrWorkloadPermissionDenied) {
			t.Fatal("wrong target revision accepted")
		}
	}
	trust.grant = 2
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadPermissionDenied) {
		t.Fatal("changed Grant accepted")
	}
	trust.grant = 1
	trust.bindingRevoked = true
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadIdentityInvalid) {
		t.Fatal("revoked binding accepted")
	}
	trust.bindingRevoked = false
	trust.identity.PrincipalVersion++
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrWorkloadIdentityInvalid) {
		t.Fatal("changed Principal accepted")
	}
	trust.identity = identity
	trust.failure = ErrPersistenceUnavailable
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatal("database failure admitted")
	}
	trust.failure = nil
	codec.wat.ExpiresAt = now
	if _, err = u.VerifyCaller(ctx, "valid", target, NotificationSubmitRPC, identity.Peer, registry.Revision(target.Audience, target.Operation)); !errors.Is(err, ErrInvocationCredentialInvalid) {
		t.Fatal("expiry accepted")
	}
}

func TestHTTPWorkloadVerificationUsesExactTargetAndCurrentAuthority(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	records := workloadRegistryFixture(t).Targets()
	records = append(records, workloadregistry.Target{Audience: "http-owner", Operation: "snapshot.receive", Mechanism: workloadregistry.Receiver, GrantScope: "receiver", Enabled: true}, workloadregistry.Target{Audience: "http-owner", Operation: "snapshot.read", HTTPMethod: "GET", HTTPPath: "/api/v1/snapshot", Mechanism: workloadregistry.WorkloadOnly, GrantScope: "read", Enabled: true, ReceiverOperation: "snapshot.receive"})
	raw, _ := json.Marshal(workloadregistry.Document{Schema: workloadregistry.Schema, Targets: records})
	sum := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	target := WorkloadTarget{Audience: "http-owner", Operation: "snapshot.read"}
	identity := WorkloadIdentity{PrincipalID: uuid.New(), BindingID: uuid.New(), PrincipalVersion: 1, BindingVersion: 1, Peer: VerifiedWorkloadPeer{Environment: "isolated", TrustDomain: "test", IdentityKind: "x509_dns", IdentityValue: "reader.test"}}
	receiver := identity
	receiver.PrincipalID = uuid.New()
	receiver.BindingID = uuid.New()
	trust := &changingWorkloadTrust{identity: identity, receiverIdentity: receiver, target: target, grant: 1, receiverTarget: WorkloadTarget{Audience: "http-owner", Operation: "snapshot.receive"}, receiverGrant: 1}
	codec := &callerCodec{wat: WorkloadTokenClaims{ID: uuid.Must(uuid.NewV7()), Caller: DirectCaller{Identity: identity, Target: target, GrantVersion: 1, TargetRevision: registry.Revision(target.Audience, target.Operation)}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}}
	u := NewWorkloadInvocation(NewWorkloadAuthentication(trust), NewWorkloadAuthorization(trust, registry), nil, codec, nil, nil, fixedAuthClock{now})
	ctx := WithDirectCaller(context.Background(), DirectCaller{Identity: receiver, Target: WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}, GrantVersion: 1})
	verify := func(method, path, revision string) error {
		_, err := u.VerifyHTTPCaller(ctx, "valid", target, method, path, identity.Peer, revision)
		return err
	}
	revision := registry.Revision(target.Audience, target.Operation)
	if err = verify("GET", "/api/v1/snapshot", revision); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][3]string{{"POST", "/api/v1/snapshot", revision}, {"GET", "/api/v1/other", revision}, {"GET", "/api/v1/snapshot", ""}, {"GET", "/api/v1/snapshot", "wrong"}} {
		if verify(p[0], p[1], p[2]) == nil {
			t.Fatal("incorrect HTTP target accepted")
		}
	}
	if _, err = u.VerifyCaller(ctx, "valid", target, "/api/v1/snapshot", identity.Peer, revision); err == nil {
		t.Fatal("HTTP accepted as RPC")
	}
	trust.receiverGrant = 0
	if verify("GET", "/api/v1/snapshot", revision) == nil {
		t.Fatal("only Verify ingress accepted")
	}
	trust.receiverGrant = 1
	trust.grant = 0
	if verify("GET", "/api/v1/snapshot", revision) == nil {
		t.Fatal("revoked caller target accepted")
	}
	trust.grant = 1
	trust.bindingRevoked = true
	if verify("GET", "/api/v1/snapshot", revision) == nil {
		t.Fatal("revoked binding accepted")
	}
	trust.bindingRevoked = false
	trust.failure = ErrPersistenceUnavailable
	if !errors.Is(verify("GET", "/api/v1/snapshot", revision), ErrPersistenceUnavailable) {
		t.Fatal("dependency failure admitted")
	}
	trust.failure = nil
	if err = verify("GET", "/api/v1/snapshot", revision); err != nil {
		t.Fatal("current authority recovery failed", err)
	}
}
