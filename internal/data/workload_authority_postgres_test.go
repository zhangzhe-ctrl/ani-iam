package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

// Restricted real PostgreSQL component, called inside the fresh WR33 database.
// SQL changes below are isolated authority fault fixtures, not management API evidence.
func runWR33AuthorityPostgresComponent(t *testing.T, ctx context.Context, migrator, provisioner, runtime *pgxpool.Pool, previous *workloadregistry.Registry) {
	t.Helper()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	const audience = "authority-component"
	const begin = "snapshot.begin"
	const page = "snapshot.page"
	const receive = "snapshot.receive"
	records := previous.Targets()
	records = append(records, workloadregistry.Target{Audience: audience, Operation: receive, Mechanism: workloadregistry.Receiver, GrantScope: "receiver", Enabled: true})
	for _, op := range []string{begin, page} {
		records = append(records, workloadregistry.Target{Audience: audience, Operation: op, HTTPMethod: "POST", HTTPPath: "/api/" + op, Mechanism: workloadregistry.WorkloadOnly, GrantScope: "read", Enabled: true, ReceiverOperation: receive, AuthorityOperations: []string{begin, page}})
	}
	raw, _ := json.Marshal(workloadregistry.Document{Schema: workloadregistry.Schema, Targets: records})
	sum := sha256.Sum256(raw)
	registry, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if err = InstallWorkloadRegistry(ctx, NewData(migrator), registry, previous.Digest()); err != nil {
		t.Fatal("install component authority targets", err)
	}
	pq := sqlcgen.New(provisioner)
	makeIdentity := func(name string) biz.WorkloadIdentity {
		t.Helper()
		i := biz.WorkloadIdentity{PrincipalID: id(), BindingID: id(), PrincipalVersion: 1, BindingVersion: 1, Peer: biz.VerifiedWorkloadPeer{Environment: "wr33-authority", TrustDomain: "wr33-authority.test", IdentityKind: "x509_dns", IdentityValue: name + ".wr33-authority.test"}}
		tx, e := provisioner.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		seed := sqlcgen.New(tx)
		now := requiredTimestamptz(time.Now())
		if e := seed.InsertBootstrapPrincipal(ctx, sqlcgen.InsertBootstrapPrincipalParams{PrincipalID: i.PrincipalID, Now: now}); e != nil {
			t.Fatal(e)
		}
		if e := seed.InsertBootstrapProfile(ctx, sqlcgen.InsertBootstrapProfileParams{PrincipalID: i.PrincipalID, Name: name, Environment: pgtype.Text{String: i.Peer.Environment, Valid: true}, TrustDomain: pgtype.Text{String: i.Peer.TrustDomain, Valid: true}, Now: now}); e != nil {
			t.Fatal(e)
		}
		if e := seed.InsertBootstrapBinding(ctx, sqlcgen.InsertBootstrapBindingParams{BindingID: i.BindingID, PrincipalID: i.PrincipalID, Environment: i.Peer.Environment, TrustDomain: i.Peer.TrustDomain, IdentityValue: i.Peer.IdentityValue, Now: now}); e != nil {
			t.Fatal(e)
		}
		if e := tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		return i
	}
	callerID, receiverID := makeIdentity("authority-reader"), makeIdentity("authority-receiver")
	grant := func(identity biz.WorkloadIdentity, a, op string) uuid.UUID {
		t.Helper()
		g := id()
		target, ok := registry.Lookup(a, op)
		if !ok {
			t.Fatal("missing fixture target", op)
		}
		if e := pq.InsertBootstrapGrant(ctx, sqlcgen.InsertBootstrapGrantParams{GrantID: g, PrincipalID: identity.PrincipalID, Environment: identity.Peer.Environment, TrustDomain: identity.Peer.TrustDomain, Audience: a, Operation: op, Scope: target.GrantScope, Now: requiredTimestamptz(time.Now())}); e != nil {
			t.Fatal(e)
		}
		return g
	}
	ingressGrant := grant(receiverID, "ani-iam", biz.VerifyWorkloadCallerRPC)
	receiverGrant := grant(receiverID, audience, receive)
	beginGrant := grant(callerID, audience, begin)
	pageGrant := grant(callerID, audience, page)
	caller := biz.DirectCaller{Identity: callerID, Target: biz.WorkloadTarget{Audience: audience, Operation: begin}, GrantVersion: 1, TargetRevision: registry.Revision(audience, begin)}
	receiver := biz.DirectCaller{Identity: receiverID, Target: biz.WorkloadTarget{Audience: "ani-iam", Operation: biz.VerifyWorkloadCallerRPC}, GrantVersion: 1, TargetRevision: registry.Revision("ani-iam", biz.VerifyWorkloadCallerRPC)}
	reader := NewWorkloadAuthorityReader(NewData(runtime), registry)
	verify := func() (biz.WorkloadAuthoritySnapshot, error) {
		return reader.ReadCurrentAuthority(ctx, receiver, caller, caller.Identity.Peer)
	}
	got, err := verify()
	if err != nil || len(got.Grants) != 2 || got.Grants[0].ID != beginGrant || got.Grants[1].ID != pageGrant {
		t.Fatal("current pair", err)
	}
	if _, err = runtime.Exec(ctx, "UPDATE workload_grants SET version=version+1 WHERE id=$1", pageGrant); err == nil {
		t.Fatal("runtime wrote authority")
	}
	changeGrant := func(g uuid.UUID, status string) {
		t.Helper()
		if _, e := migrator.Exec(ctx, "UPDATE workload_grants SET status=$2,version=version+1 WHERE id=$1", g, status); e != nil {
			t.Fatal(e)
		}
	}
	for _, g := range []uuid.UUID{pageGrant, beginGrant, ingressGrant, receiverGrant} {
		changeGrant(g, "revoked")
		if _, e := verify(); !errors.Is(e, biz.ErrWorkloadPermissionDenied) {
			t.Fatal("revoked current Grant admitted", e)
		}
		changeGrant(g, "active")
		if g == beginGrant {
			caller.GrantVersion += 2
		}
		if g == ingressGrant {
			receiver.GrantVersion += 2
		}
		if _, e := verify(); e != nil {
			t.Fatal("restored current authority", e)
		}
	}
	// A newly issued selected WAT cannot hide a version change of the other Grant.
	got, err = verify()
	if err != nil || got.Grants[1].Version != 3 {
		t.Fatal("other Grant version missing", err)
	}
	badPeer := caller.Identity.Peer
	badPeer.IdentityValue = "different.wr33-authority.test"
	if _, err = reader.ReadCurrentAuthority(ctx, receiver, caller, badPeer); err == nil {
		t.Fatal("wrong observed peer admitted")
	}
	for _, identity := range []biz.WorkloadIdentity{callerID, receiverID} {
		if _, err = migrator.Exec(ctx, "UPDATE workload_identity_bindings SET status='revoked',version=version+1 WHERE id=$1", identity.BindingID); err != nil {
			t.Fatal(err)
		}
		if _, err = verify(); !errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
			t.Fatal("revoked Binding admitted", err)
		}
		if _, err = migrator.Exec(ctx, "UPDATE workload_identity_bindings SET status='active',version=version+1 WHERE id=$1", identity.BindingID); err != nil {
			t.Fatal(err)
		}
		if _, err = verify(); !errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
			t.Fatal("stale Binding version admitted", err)
		}
		if identity.PrincipalID == caller.Identity.PrincipalID {
			caller.Identity.BindingVersion += 2
		} else {
			receiver.Identity.BindingVersion += 2
		}
		if _, err = verify(); err != nil {
			t.Fatal("fresh Binding version rejected", err)
		}
	}
	expected, _ := hex.DecodeString(registry.Revision(audience, page))
	wrong := sha256.Sum256([]byte("different reviewed registration"))
	if _, err = migrator.Exec(ctx, "UPDATE workload_target_registrations SET target_sha256=$3 WHERE audience=$1 AND operation=$2", audience, page, wrong[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = verify(); !errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		t.Fatal("changed registration admitted", err)
	}
	if _, err = migrator.Exec(ctx, "UPDATE workload_target_registrations SET target_sha256=$3 WHERE audience=$1 AND operation=$2", audience, page, expected); err != nil {
		t.Fatal(err)
	}
	// At no committed instant are both Grants active: first Begin only, then Page
	// only. Pause after the first caller Grant read, commit the swap, then continue.
	changeGrant(pageGrant, "revoked")
	barrier := &authorityReadBarrier{reached: make(chan struct{}), resume: make(chan struct{})}
	defer barrier.release()
	cfg := runtime.Config().Copy()
	cfg.ConnConfig.Tracer = barrier
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { barrier.release(); pool.Close() }()
	consistent := NewWorkloadAuthorityReader(NewData(pool), registry)
	result := make(chan error, 1)
	go func() {
		_, e := consistent.ReadCurrentAuthority(ctx, receiver, caller, caller.Identity.Peer)
		result <- e
	}()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal("authority query barrier not reached")
	}
	tx, e := migrator.Begin(ctx)
	if e != nil {
		barrier.release()
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE workload_grants SET status=CASE WHEN id=$1 THEN 'revoked' ELSE 'active' END,version=version+1 WHERE id=$1 OR id=$2", beginGrant, pageGrant); e != nil {
		tx.Rollback(ctx)
		barrier.release()
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		barrier.release()
		t.Fatal(e)
	}
	barrier.release()
	select {
	case e = <-result:
		if !errors.Is(e, biz.ErrWorkloadPermissionDenied) {
			t.Fatal("combined Grants that were never simultaneously active", e)
		}
	case <-ctx.Done():
		t.Fatal("authority read did not finish")
	}
	if _, e = verify(); !errors.Is(e, biz.ErrWorkloadPermissionDenied) {
		t.Fatal("post-commit revocation admitted", e)
	}
	changeGrant(beginGrant, "active")
	caller.GrantVersion += 2
	if _, e = verify(); e != nil {
		t.Fatal("final authority restoration", e)
	}
	t.Log("paired authority: current Grant/Binding/registry denial, unchanged runtime write restrictions, and real concurrent REPEATABLE READ consistency passed")
}

type authorityTraceKey struct{}
type authorityReadBarrier struct {
	count           atomic.Int32
	reached, resume chan struct{}
	once            sync.Once
}

func (b *authorityReadBarrier) release() { b.once.Do(func() { close(b.resume) }) }
func (b *authorityReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, authorityTraceKey{}, strings.Contains(d.SQL, "-- name: ReadWorkloadAuthorityGrant"))
}
func (b *authorityReadBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if yes, _ := ctx.Value(authorityTraceKey{}).(bool); yes && b.count.Add(1) == 3 {
		close(b.reached)
		select {
		case <-b.resume:
		case <-ctx.Done():
		}
	}
}
