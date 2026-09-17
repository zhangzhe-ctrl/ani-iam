package data

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func workloadCodecFixture(t *testing.T) (*JWXWorkloadCredentialCodec, biz.WorkloadTokenClaims, biz.DelegationClaims) {
	t.Helper()
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x69}, ed25519.SeedSize))
	base, err := NewJWXAccessTokenCodec("wr19-key", key, map[string]ed25519.PublicKey{"wr19-key": key.Public().(ed25519.PublicKey)}, "ani-iam", fixedDataClock{now})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewJWXWorkloadCredentialCodec(base, dataWorkloadRegistryFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	caller := biz.DirectCaller{Identity: biz.WorkloadIdentity{PrincipalID: id(), BindingID: id(), PrincipalVersion: 1, BindingVersion: 1, Peer: biz.VerifiedWorkloadPeer{Environment: "wr19", TrustDomain: "wr19.test", IdentityKind: "x509_dns", IdentityValue: "gateway.wr19.test"}}, Target: biz.WorkloadTarget{Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation}, GrantVersion: 1}
	caller.TargetRevision = c.registry.Revision(caller.Target.Audience, caller.Target.Operation)
	wat := biz.WorkloadTokenClaims{ID: id(), Caller: caller, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	subject, tenant := id(), id()
	d := biz.DelegationClaims{ID: id(), WorkloadTokenID: wat.ID, Caller: caller, IssuedAt: now, ExpiresAt: now.Add(time.Minute), Subject: biz.DelegatedSubject{Principal: biz.TrustedPrincipalContext{ID: subject, Type: biz.PrincipalTypeHuman, Status: biz.PrincipalStatusActive, TenantID: tenant, SessionID: id(), GrantID: id()}, GrantVersion: 1}, Binding: biz.InvocationBinding{Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation, RPCMethod: biz.SessionInvocationRPC, SourceOperation: "createInstanceExecSession", TenantID: tenant, SubjectID: subject, ResourceID: id().String(), Mode: "exec", PolicyRevision: "fixed-test-revision", RequestSHA256: [32]byte{1}}}
	d.Binding.TargetRevision = caller.TargetRevision
	return c, wat, d
}

func TestWorkloadAndDelegationJWXRoundTripAndTypeSeparation(t *testing.T) {
	c, w, d := workloadCodecFixture(t)
	ctx := context.Background()
	rawW, err := c.IssueWorkload(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	gotW, err := c.VerifyWorkload(ctx, rawW)
	if err != nil || !reflect.DeepEqual(w, gotW) {
		t.Fatalf("Workload round trip: %v", err)
	}
	rawD, err := c.IssueDelegation(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	gotD, err := c.VerifyDelegation(ctx, rawD)
	if err != nil || !reflect.DeepEqual(d, gotD) {
		t.Fatalf("delegation round trip: %v", err)
	}
	if _, err = c.Verify(ctx, rawW); err == nil {
		t.Fatal("WAT accepted as Human access token")
	}
	if _, err = c.VerifyWorkload(ctx, rawD); err == nil {
		t.Fatal("delegation accepted as WAT")
	}
	if _, err = c.VerifyDelegation(ctx, rawW); err == nil {
		t.Fatal("WAT accepted as delegation")
	}
	c.clock = fixedDataClock{d.ExpiresAt}
	if _, err = c.VerifyDelegation(ctx, rawD); err == nil {
		t.Fatal("expired delegation accepted")
	}
	c.clock = fixedDataClock{w.ExpiresAt}
	if _, err = c.VerifyWorkload(ctx, rawW); err == nil {
		t.Fatal("expired WAT accepted")
	}
}

func TestWorkloadJWXRejectsSignedWrongEnvelope(t *testing.T) {
	c, w, _ := workloadCodecFixture(t)
	payload, _ := json.Marshal(w)
	cases := map[string]func(jwt.Token, jws.Headers){
		"issuer":   func(t jwt.Token, h jws.Headers) { _ = t.Set(jwt.IssuerKey, "another-issuer") },
		"audience": func(t jwt.Token, h jws.Headers) { _ = t.Set(jwt.AudienceKey, []string{"another-service"}) },
		"multiple audiences": func(t jwt.Token, h jws.Headers) {
			_ = t.Set(jwt.AudienceKey, []string{biz.SessionInvocationAudience, "another-service"})
		},
		"subject":         func(t jwt.Token, h jws.Headers) { _ = t.Set(jwt.SubjectKey, uuid.NewString()) },
		"missing context": func(t jwt.Token, h jws.Headers) { _ = t.Remove(invocationContextClaim) },
		"future issued":   func(t jwt.Token, h jws.Headers) { _ = t.Set(jwt.IssuedAtKey, w.IssuedAt.Add(time.Minute)) },
		"excessive ttl":   func(t jwt.Token, h jws.Headers) { _ = t.Set(jwt.ExpirationKey, w.IssuedAt.Add(6*time.Minute)) },
		"unknown kid":     func(t jwt.Token, h jws.Headers) { _ = h.Set(jws.KeyIDKey, "unknown") },
		"wrong type":      func(t jwt.Token, h jws.Headers) { _ = h.Set(jws.TypeKey, "JWT") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			token, err := jwt.NewBuilder().Issuer(c.issuer).Subject(w.Caller.Identity.PrincipalID.String()).Audience([]string{biz.SessionInvocationAudience}).JwtID(w.ID.String()).IssuedAt(w.IssuedAt).Expiration(w.ExpiresAt).Claim(invocationContextClaim, string(payload)).Build()
			if err != nil {
				t.Fatal(err)
			}
			h := jws.NewHeaders()
			_ = h.Set(jws.KeyIDKey, c.activeKeyID)
			_ = h.Set(jws.TypeKey, workloadJWTType)
			change(token, h)
			raw, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), c.privateKey, jws.WithProtectedHeaders(h)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.VerifyWorkload(context.Background(), string(raw)); err == nil {
				t.Fatal("accepted signed but invalid envelope")
			}
		})
	}
}

func TestWorkloadJWXSigningKeyRotation(t *testing.T) {
	c, w, _ := workloadCodecFixture(t)
	ctx := context.Background()
	raw, err := c.IssueWorkload(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	next := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	rotatedBase, err := NewJWXAccessTokenCodec("new", next, map[string]ed25519.PublicKey{"new": next.Public().(ed25519.PublicKey), c.activeKeyID: c.privateKey.Public().(ed25519.PublicKey)}, c.issuer, c.clock)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewJWXWorkloadCredentialCodec(rotatedBase, c.registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.VerifyWorkload(ctx, raw); err != nil {
		t.Fatal("retained key cannot verify still-valid token")
	}
	delete(rotated.verificationKey, c.activeKeyID)
	if _, err = rotated.VerifyWorkload(ctx, raw); err == nil {
		t.Fatal("removed key still verifies token")
	}
}

func TestContinuationCannotCreateOrOutliveSubject(t *testing.T) {
	codec, wat, d := workloadCodecFixture(t)
	receiver := wat.Caller
	receiver.Identity.PrincipalID = uuid.Must(uuid.NewV7())
	receiver.Identity.BindingID = uuid.Must(uuid.NewV7())
	receiver.Identity.Peer.IdentityValue = "session.wr19.test"
	receiver.Target = biz.WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.AuthorizationService/VerifyWorkloadInvocation"}
	receiver.TargetRevision = codec.registry.Revision(receiver.Target.Audience, receiver.Target.Operation)
	d.Subject.CredentialExpiresAt = d.IssuedAt.Add(10 * time.Minute)
	proof := biz.SessionContinuationClaims{ID: uuid.Must(uuid.NewV7()), Caller: wat.Caller, Receiver: receiver, Subject: d.Subject, Binding: d.Binding, IssuedAt: d.IssuedAt, ExpiresAt: d.IssuedAt.Add(10 * time.Minute)}
	proof.ReceiverAuthority = receiver
	proof.ReceiverAuthority.Target = biz.WorkloadTarget{Audience: biz.SessionInvocationAudience, Operation: "session.receive"}
	proof.ReceiverAuthority.TargetRevision = codec.registry.Revision(proof.ReceiverAuthority.Target.Audience, proof.ReceiverAuthority.Target.Operation)
	ctx := context.Background()
	raw, err := codec.IssueContinuation(ctx, proof)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.VerifyContinuation(ctx, raw)
	if err != nil || !reflect.DeepEqual(got, proof) {
		t.Fatalf("continuation roundtrip: %v", err)
	}
	if _, err = codec.VerifyDelegation(ctx, raw); err == nil {
		t.Fatal("continuation accepted as creation delegation")
	}
	if _, err = codec.VerifyWorkload(ctx, raw); err == nil {
		t.Fatal("continuation accepted as WAT")
	}
	if _, err = codec.Verify(ctx, raw); err == nil {
		t.Fatal("continuation accepted as Human credential")
	}
	rawD, err := codec.IssueDelegation(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = codec.VerifyContinuation(ctx, rawD); err == nil {
		t.Fatal("creation delegation accepted as continuation")
	}
	codec.clock = fixedDataClock{d.ExpiresAt.Add(time.Second)}
	if _, err = codec.VerifyContinuation(ctx, raw); err != nil {
		t.Fatal("continuation cannot recheck after delegation expires")
	}
	if _, err = codec.VerifyDelegation(ctx, rawD); err == nil {
		t.Fatal("delegation expiry weakened")
	}
	proof.ExpiresAt = proof.Subject.CredentialExpiresAt.Add(time.Second)
	if _, err = codec.IssueContinuation(ctx, proof); err == nil {
		t.Fatal("continuation outlived subject credential")
	}
	proof.ExpiresAt = proof.IssuedAt.Add(17 * time.Minute)
	proof.Subject.CredentialExpiresAt = proof.ExpiresAt
	if _, err = codec.IssueContinuation(ctx, proof); err == nil {
		t.Fatal("continuation exceeded lifetime cap")
	}
	codec.clock = fixedDataClock{got.ExpiresAt}
	if _, err = codec.VerifyContinuation(ctx, raw); err == nil {
		t.Fatal("expired continuation accepted")
	}
}

func TestInferenceJWTUsesExactAudienceAndNeverContinuation(t *testing.T) {
	codec, w, d := workloadCodecFixture(t)
	ctx := context.Background()
	w.Caller.Target = biz.WorkloadTarget{Audience: biz.InferenceInvocationAudience, Operation: biz.InferenceInvocationOperation}
	w.Caller.TargetRevision = codec.registry.Revision(w.Caller.Target.Audience, w.Caller.Target.Operation)
	d.Caller = w.Caller
	d.Binding.Audience = biz.InferenceInvocationAudience
	d.Binding.Operation = biz.InferenceInvocationOperation
	d.Binding.TargetRevision = w.Caller.TargetRevision
	d.Binding.RPCMethod = biz.InferenceInvocationRPC
	d.Binding.SourceOperation = biz.InferenceSourceOperation
	d.Binding.Mode = "chat_completions"
	d.Subject = biz.DelegatedSubject{Principal: biz.TrustedPrincipalContext{ID: d.Binding.SubjectID, Type: biz.PrincipalTypeWorkload, TenantID: d.Binding.TenantID, Status: biz.PrincipalStatusActive}, APIKeyID: uuid.New(), APIKeyVersion: 1}
	raw, err := codec.IssueWorkload(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.VerifyWorkload(ctx, raw)
	if err != nil || got.Caller.Target != w.Caller.Target {
		t.Fatal("Inference WAT audience mismatch")
	}
	raw, err = codec.IssueDelegation(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := codec.VerifyDelegation(ctx, raw)
	if err != nil || proof.Binding != d.Binding || proof.Subject.APIKeyID != d.Subject.APIKeyID {
		t.Fatal("Inference delegation mismatch")
	}
	if _, err = codec.VerifyContinuation(ctx, raw); err == nil {
		t.Fatal("Inference delegation accepted as continuation")
	}
	changed := d
	changed.Caller.Target = biz.WorkloadTarget{Audience: biz.SessionInvocationAudience, Operation: biz.SessionInvocationOperation}
	if _, err = codec.IssueDelegation(ctx, changed); err == nil {
		t.Fatal("caller and binding target mismatch accepted")
	}
	changed = d
	changed.Subject.Principal.Type = biz.PrincipalTypeHuman
	if _, err = codec.IssueDelegation(ctx, changed); err == nil {
		t.Fatal("Human Inference invocation accepted")
	}
}
