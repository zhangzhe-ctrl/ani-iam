package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type authorityReaderFunc func(context.Context, DirectCaller, DirectCaller, VerifiedWorkloadPeer) (WorkloadAuthoritySnapshot, error)

func (f authorityReaderFunc) ReadCurrentAuthority(c context.Context, r, d DirectCaller, p VerifiedWorkloadPeer) (WorkloadAuthoritySnapshot, error) {
	return f(c, r, d, p)
}
func authorityRegistryFixture(t *testing.T) *workloadregistry.Registry {
	t.Helper()
	d := workloadregistry.Document{Schema: workloadregistry.Schema, Targets: workloadRegistryFixture(t).Targets()}
	d.Targets = append(d.Targets, workloadregistry.Target{Audience: "paired-owner", Operation: "snapshot.receive", Mechanism: workloadregistry.Receiver, GrantScope: "receiver", Enabled: true})
	for _, op := range []string{"snapshot.begin", "snapshot.page"} {
		d.Targets = append(d.Targets, workloadregistry.Target{Audience: "paired-owner", Operation: op, HTTPMethod: "POST", HTTPPath: "/api/" + op, Mechanism: workloadregistry.WorkloadOnly, GrantScope: "read", Enabled: true, ReceiverOperation: "snapshot.receive", AuthorityOperations: []string{"snapshot.begin", "snapshot.page"}})
	}
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	r, e := workloadregistry.Parse(raw, hex.EncodeToString(h[:]))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func authorityStateFixture(r *workloadregistry.Registry) (DirectCaller, WorkloadAuthoritySnapshot) {
	i := WorkloadIdentity{PrincipalID: uuid.MustParse("01900000-0000-7000-8000-000000000001"), BindingID: uuid.MustParse("01900000-0000-7000-8000-000000000002"), PrincipalVersion: 1, BindingVersion: 2, Peer: VerifiedWorkloadPeer{Environment: "test", TrustDomain: "test", IdentityKind: "x509_dns", IdentityValue: "reader.test"}}
	c := DirectCaller{Identity: i, Target: WorkloadTarget{Audience: "paired-owner", Operation: "snapshot.begin"}, GrantVersion: 3, TargetRevision: r.Revision("paired-owner", "snapshot.begin")}
	return c, WorkloadAuthoritySnapshot{Identity: i, Grants: []WorkloadAuthorityGrant{{Operation: "snapshot.begin", TargetRevision: c.TargetRevision, ID: uuid.MustParse("01900000-0000-7000-8000-000000000003"), Version: 3}, {Operation: "snapshot.page", TargetRevision: r.Revision("paired-owner", "snapshot.page"), ID: uuid.MustParse("01900000-0000-7000-8000-000000000004"), Version: 4}}}
}
func TestAuthorityRevisionBindsWholeCurrentPair(t *testing.T) {
	r := authorityRegistryFixture(t)
	c, s := authorityStateFixture(r)
	begin, e := workloadAuthorityRevision(r, c, s)
	if e != nil {
		t.Fatal(e)
	}
	page := c
	page.Target.Operation = "snapshot.page"
	page.TargetRevision = r.Revision(page.Target.Audience, page.Target.Operation)
	page.GrantVersion = 4
	if got, e := workloadAuthorityRevision(r, page, s); e != nil || got != begin {
		t.Fatal("request operation changed same authority", e)
	}
	// The proof changes even when only the other operation's Grant changed.
	s.Grants[1].Version++
	changed, e := workloadAuthorityRevision(r, c, s)
	if e != nil || changed == begin {
		t.Fatal("other Grant omitted", e)
	}
	s.Grants[1].Version++
	restored, e := workloadAuthorityRevision(r, c, s)
	if e != nil || restored == begin || restored == changed {
		t.Fatal("restored Grant revived old authority", e)
	}
	c, s = authorityStateFixture(r)
	c.Identity.BindingVersion++
	s.Identity = c.Identity
	if got, e := workloadAuthorityRevision(r, c, s); e != nil || got == begin {
		t.Fatal("Binding omitted", e)
	}
	for _, tc := range []struct {
		name   string
		change func(*DirectCaller, *WorkloadAuthoritySnapshot)
	}{
		{"missing grant", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Grants = s.Grants[:1] }},
		{"duplicate grant ID", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Grants[1].ID = s.Grants[0].ID }},
		{"duplicate operation", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Grants[1].Operation = s.Grants[0].Operation }},
		{"unsorted", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) {
			s.Grants[0], s.Grants[1] = s.Grants[1], s.Grants[0]
		}},
		{"stale selected WAT", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { c.GrantVersion++ }},
		{"wrong target SHA", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) {
			s.Grants[1].TargetRevision = s.Grants[0].TargetRevision
		}},
		{"stale identity", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Identity.BindingVersion++ }},
		{"invalid ID", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Grants[0].ID = uuid.Nil }},
		{"invalid version", func(c *DirectCaller, s *WorkloadAuthoritySnapshot) { s.Grants[1].Version = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := authorityStateFixture(r)
			tc.change(&c, &s)
			if _, e := workloadAuthorityRevision(r, c, s); e == nil {
				t.Fatal("invalid authority admitted")
			}
		})
	}
}
func TestGroupedHTTPVerifierFailsClosedAndChecksExpiryAfterRead(t *testing.T) {
	r := authorityRegistryFixture(t)
	caller, state := authorityStateFixture(r)
	now := time.Now().UTC()
	clock := &authorityClock{now: now}
	receiver := caller
	receiver.Identity.PrincipalID = uuid.Must(uuid.NewV7())
	receiver.Identity.BindingID = uuid.Must(uuid.NewV7())
	receiver.Identity.Peer.IdentityValue = "owner.test"
	receiver.Target = WorkloadTarget{Audience: "ani-iam", Operation: VerifyWorkloadCallerRPC}
	receiver.TargetRevision = r.Revision(receiver.Target.Audience, receiver.Target.Operation)
	codec := &callerCodec{wat: WorkloadTokenClaims{ID: uuid.Must(uuid.NewV7()), Caller: caller, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}}
	u := NewWorkloadInvocation(nil, NewWorkloadAuthorization(nil, r), nil, codec, nil, nil, clock)
	ctx := WithDirectCaller(context.Background(), receiver)
	verify := func(peer VerifiedWorkloadPeer) (VerifiedWorkloadCaller, error) {
		return u.VerifyHTTPCaller(ctx, "valid", caller.Target, "POST", "/api/snapshot.begin", peer, caller.TargetRevision)
	}
	if _, e := verify(caller.Identity.Peer); !errors.Is(e, ErrPersistenceUnavailable) {
		t.Fatal("missing authority reader did not fail closed", e)
	}
	calls := 0
	failure := error(nil)
	expire := false
	u.WithWorkloadAuthorityReader(authorityReaderFunc(func(_ context.Context, gotReceiver, gotCaller DirectCaller, peer VerifiedWorkloadPeer) (WorkloadAuthoritySnapshot, error) {
		calls++
		if gotReceiver != receiver || gotCaller != caller || peer != caller.Identity.Peer {
			t.Fatal("verification context lost")
		}
		if expire {
			clock.now = codec.wat.ExpiresAt
		}
		return state, failure
	}))
	got, e := verify(caller.Identity.Peer)
	if e != nil || got.AuthorityRevision == "" {
		t.Fatal("group verification failed", e)
	}
	other := caller.Identity.Peer
	other.IdentityValue = "other.test"
	if _, e = verify(other); !errors.Is(e, ErrWorkloadIdentityInvalid) || calls != 1 {
		t.Fatal("peer mismatch reached reader", e)
	}
	failure = ErrWorkloadPermissionDenied
	if _, e = verify(caller.Identity.Peer); !errors.Is(e, failure) {
		t.Fatal("current denial admitted", e)
	}
	failure = ErrPersistenceUnavailable
	if _, e = verify(caller.Identity.Peer); !errors.Is(e, failure) {
		t.Fatal("dependency failure admitted", e)
	}
	failure = nil
	expire = true
	if _, e = verify(caller.Identity.Peer); !errors.Is(e, ErrInvocationCredentialInvalid) {
		t.Fatal("WAT expired during read admitted", e)
	}
}

type authorityClock struct{ now time.Time }

func (c *authorityClock) Now() time.Time { return c.now }

// Fixed vector calculated independently from the written contract with Python
// on Fedora, retained as authority-golden-vector.json in WR33 evidence.
func TestAuthorityRevisionGoldenVector(t *testing.T) {
	r := authorityRegistryFixture(t)
	c, s := authorityStateFixture(r)
	got, err := workloadAuthorityRevision(r, c, s)
	if err != nil || got != "wa1:56362b89dc706647b7f03ff20248228a0b0c34268e3c5384e98ba4cdcc74181f" {
		t.Fatal("authority contract encoding drift", got, err)
	}
}
