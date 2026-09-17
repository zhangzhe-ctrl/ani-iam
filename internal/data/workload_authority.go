package data

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type workloadAuthorityReader struct {
	data     *Data
	registry *workloadregistry.Registry
}

func NewWorkloadAuthorityReader(data *Data, registry *workloadregistry.Registry) biz.WorkloadAuthorityReader {
	return &workloadAuthorityReader{data: data, registry: registry}
}

func (r *workloadAuthorityReader) ReadCurrentAuthority(ctx context.Context, receiver, caller biz.DirectCaller, observed biz.VerifiedWorkloadPeer) (biz.WorkloadAuthoritySnapshot, error) {
	empty := biz.WorkloadAuthoritySnapshot{}
	if r == nil || r.data == nil || r.data.pool == nil || r.registry == nil {
		return empty, biz.ErrPersistenceUnavailable
	}
	target, ok := r.registry.Lookup(caller.Target.Audience, caller.Target.Operation)
	if !ok || !target.Enabled || len(target.AuthorityOperations) != 2 || receiver.Target != (biz.WorkloadTarget{Audience: "ani-iam", Operation: biz.VerifyWorkloadCallerRPC}) || caller.Identity.Peer != observed || observed.Environment != receiver.Identity.Peer.Environment || observed.TrustDomain != receiver.Identity.Peer.TrustDomain {
		return empty, biz.ErrWorkloadPermissionDenied
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, mapPostgresError("begin current Workload authority", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// The first query fixes the MVCC snapshot and verification linearization point.
	// All identity, receiver and paired Grant reads use this same transaction.
	q := sqlcgen.New(tx)
	resolve := func(expected biz.WorkloadIdentity, peer biz.VerifiedWorkloadPeer) error {
		row, e := q.ResolveWorkloadIdentity(ctx, sqlcgen.ResolveWorkloadIdentityParams{Environment: peer.Environment, TrustDomain: peer.TrustDomain, IdentityKind: peer.IdentityKind, IdentityValue: peer.IdentityValue})
		if errors.Is(e, pgx.ErrNoRows) {
			return biz.ErrWorkloadIdentityInvalid
		}
		if e != nil {
			return mapPostgresError("read current Workload identity", e, nil)
		}
		current := biz.WorkloadIdentity{PrincipalID: row.PrincipalID, BindingID: row.BindingID, PrincipalVersion: row.PrincipalVersion, BindingVersion: row.BindingVersion, Peer: peer}
		if current != expected {
			return biz.ErrWorkloadIdentityInvalid
		}
		return nil
	}
	if err = resolve(receiver.Identity, receiver.Identity.Peer); err != nil {
		return empty, err
	}
	if err = resolve(caller.Identity, observed); err != nil {
		return empty, err
	}
	readGrant := func(identity biz.WorkloadIdentity, t biz.WorkloadTarget) (biz.WorkloadAuthorityGrant, error) {
		registration, registered := r.registry.Lookup(t.Audience, t.Operation)
		revision := r.registry.Revision(t.Audience, t.Operation)
		digest, e := hex.DecodeString(revision)
		if !registered || !registration.Enabled || e != nil || len(digest) != 32 {
			return biz.WorkloadAuthorityGrant{}, biz.ErrWorkloadPermissionDenied
		}
		row, e := q.ReadWorkloadAuthorityGrant(ctx, sqlcgen.ReadWorkloadAuthorityGrantParams{PrincipalID: identity.PrincipalID, BindingID: identity.BindingID, PrincipalVersion: identity.PrincipalVersion, BindingVersion: identity.BindingVersion, Environment: identity.Peer.Environment, TrustDomain: identity.Peer.TrustDomain, Audience: t.Audience, Operation: t.Operation, TargetSha256: digest})
		if errors.Is(e, pgx.ErrNoRows) {
			return biz.WorkloadAuthorityGrant{}, biz.ErrWorkloadPermissionDenied
		}
		if e != nil {
			return biz.WorkloadAuthorityGrant{}, mapPostgresError("read current Workload grant", e, nil)
		}
		return biz.WorkloadAuthorityGrant{Operation: t.Operation, TargetRevision: revision, ID: row.ID, Version: row.Version}, nil
	}
	ingress, err := readGrant(receiver.Identity, receiver.Target)
	if err != nil {
		return empty, err
	}
	if ingress.Version != receiver.GrantVersion || ingress.TargetRevision != receiver.TargetRevision {
		return empty, biz.ErrWorkloadPermissionDenied
	}
	if _, err = readGrant(receiver.Identity, biz.WorkloadTarget{Audience: target.Audience, Operation: target.ReceiverOperation}); err != nil {
		return empty, err
	}
	current := biz.WorkloadAuthoritySnapshot{Identity: caller.Identity}
	for _, operation := range target.AuthorityOperations {
		grant, e := readGrant(caller.Identity, biz.WorkloadTarget{Audience: target.Audience, Operation: operation})
		if e != nil {
			return empty, e
		}
		if operation == caller.Target.Operation && (grant.Version != caller.GrantVersion || grant.TargetRevision != caller.TargetRevision) {
			return empty, biz.ErrWorkloadPermissionDenied
		}
		current.Grants = append(current.Grants, grant)
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, mapPostgresError("complete current Workload authority", err, nil)
	}
	return current, nil
}
