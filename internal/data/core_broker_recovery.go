package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

// The fingerprint includes every current Principal, Binding, Grant and exact
// route revision. It cannot outlive another revocation/restoration cycle.
func (a coreBrokerAuthority) fingerprint() string {
	raw, _ := json.Marshal(struct {
		Route                         CoreBrokerRoute
		Producer, Receiver, Execution sqlcgen.ReadCurrentCoreBrokerGrantRow
	}{a.route, a.producer, a.receiver, a.execution})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (t *coreBootstrapWorkTransaction) currentBrokerAuthority(ctx context.Context, s biz.CoreBootstrapSource) (coreBrokerAuthority, error) {
	current, previous, err := t.broker.bootstrapSourceAuthority(ctx, t.q, s)
	if err != nil || current.matches(previous) {
		return current, err
	}
	// This flag is set only by the already authorized Platform transaction.
	// The outer UOW still rechecks the Human before receipt/effects and audits.
	if t.platformRecovery {
		return current, nil
	}
	if t.work == nil || t.work.Source != s {
		return current, biz.ErrCoreBrokerAuthority
	}
	kind, generation := biz.CoreBootstrapInitialize, int64(1)
	if t.work.Result != nil {
		if t.work.Invitation == nil {
			return current, biz.ErrCoreBrokerAuthority
		}
		kind, generation = biz.CoreBootstrapExpire, t.work.Invitation.Invitation.DeliveryGeneration
	}
	var recovery pgtype.UUID
	found := false
	for _, job := range t.work.Jobs {
		if job.Kind == kind && job.Generation == generation {
			found = true
			if job.RecoveryID != uuid.Nil {
				recovery = requiredPGUUID(job.RecoveryID)
			}
		}
	}
	if !found {
		return current, biz.ErrCoreBrokerAuthority
	}
	approved, err := t.q.HasCoreBootstrapBrokerApproval(ctx, sqlcgen.HasCoreBootstrapBrokerApprovalParams{
		TenantID: s.TenantID, ConsumerID: t.broker.config.ConsumerID, BrokerSequence: previous.BrokerSequence,
		SourceEventID: s.EventID, OperationID: s.OperationID, Kind: string(kind), Generation: generation,
		AuthoritySha256: current.fingerprint(), RecoveryID: recovery,
	})
	if err != nil {
		return current, mapPostgresError("read exact Bootstrap authority approval", err, nil)
	}
	if !approved {
		return current, biz.ErrCoreBrokerAuthority
	}
	return current, nil
}

func (t *platformAdministrationTransaction) appendBootstrapBrokerApproval(ctx context.Context, cap biz.PlatformCapability, kind biz.CoreBootstrapJobKind, generation int64, id uuid.UUID, reissue bool, reason string) error {
	b := t.bootstrap
	if t.broker == nil {
		return nil
	}
	if b == nil || !b.platformRecovery || b.work == nil || b.authority == nil || id.Version() != 7 {
		return biz.ErrCoreBrokerAuthority
	}
	claims, operation, err := cap.CredentialBinding()
	if err != nil || (reissue && operation != "reissueTenantIAMBootstrapInvitation") || (!reissue && operation != "retryTenantIAMBootstrapJob") {
		return biz.ErrCoreBrokerAuthority
	}
	current, previous, err := t.broker.bootstrapSourceAuthority(ctx, t.q, b.work.Source)
	if err != nil {
		return err
	}
	var recovery, audit pgtype.UUID
	if reissue {
		audit = requiredPGUUID(id)
	} else {
		recovery = requiredPGUUID(id)
	}
	return mapPostgresError("append audited Bootstrap authority approval", t.q.AppendCoreBootstrapBrokerApproval(ctx, sqlcgen.AppendCoreBootstrapBrokerApprovalParams{
		TenantID: b.tenant, ID: id, ConsumerID: t.broker.config.ConsumerID, BrokerSequence: previous.BrokerSequence,
		SourceEventID: b.work.Source.EventID, OperationID: b.work.Source.OperationID, Kind: string(kind), Generation: generation,
		AuthoritySha256: current.fingerprint(), RecoveryID: recovery, EffectAuditID: audit, ActorID: claims.Subject, ReasonCode: reason,
	}), nil)
}
