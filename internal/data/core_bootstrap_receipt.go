package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type coreBootstrapReceiptRepository struct{ data *Data }

func NewCoreBootstrapReceiptRepository(d *Data) biz.CoreBootstrapReceiptRepository {
	return &coreBootstrapReceiptRepository{data: d}
}

func (r *coreBootstrapReceiptRepository) Receive(ctx context.Context, d biz.CoreBootstrapDelivery) (biz.CoreBootstrapReceipt, error) {
	if err := d.Validate(); err != nil {
		return biz.CoreBootstrapReceipt{}, err
	}
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.CoreBootstrapReceipt{}, biz.ErrPersistenceUnavailable
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.CoreBootstrapReceipt{}, mapPostgresError("begin Core Bootstrap receipt", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	result, err := receiveCoreBootstrap(ctx, sqlcgen.New(tx), d, uuid.Nil)
	if err != nil {
		return biz.CoreBootstrapReceipt{}, err
	}
	return result, mapPostgresError("commit Core Bootstrap receipt", tx.Commit(ctx), nil)
}

// The broker receiver calls this helper inside the same authority/receipt/
// projection transaction. The original component port retains its own UoW.
func receiveCoreBootstrap(ctx context.Context, q *sqlcgen.Queries, d biz.CoreBootstrapDelivery, epoch uuid.UUID) (biz.CoreBootstrapReceipt, error) {
	result := biz.CoreBootstrapReceipt{EventID: d.EventID, OperationID: d.Intent.OperationID}
	if err := d.Validate(); err != nil {
		return result, err
	}
	var err error
	sourceEpoch := pgtype.UUID{}
	if epoch != uuid.Nil {
		sourceEpoch = requiredPGUUID(epoch)
	}
	// Share the existing Tenant administration lock with recovery. An absent
	// Access row must not force creation just to serialize Bootstrap ingress.
	if err = q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: d.Intent.TenantID}); err != nil {
		return result, mapPostgresError("lock Core Bootstrap Tenant", err, nil)
	}
	sum := sha256.Sum256(d.RawPayload)
	existing, err := q.GetCoreBootstrapReceipt(ctx, sqlcgen.GetCoreBootstrapReceiptParams{TenantID: d.Intent.TenantID, EventID: d.EventID})
	if err == nil {
		if existing.OperationID != d.Intent.OperationID || existing.Producer != d.Producer || existing.SourceSequence != d.SourceSequence || existing.SourceEpoch != sourceEpoch || existing.PayloadFingerprint != d.Intent.Fingerprint || !bytes.Equal(existing.RawPayload, d.RawPayload) || !bytes.Equal(existing.RawSha256, sum[:]) || !existing.OccurredAt.Time.Equal(d.OccurredAt.Round(time.Microsecond)) {
			return result, biz.ErrCoreBootstrapConflict
		}
		result.DuplicateEvent = true
		result.ReceivedAt = existing.ReceivedAt.Time
	} else if errors.Is(err, pgx.ErrNoRows) {
		payload, _ := d.Intent.CanonicalPayload()
		n, insertErr := q.CreateCoreBootstrapOperation(ctx, sqlcgen.CreateCoreBootstrapOperationParams{TenantID: d.Intent.TenantID, ID: d.Intent.OperationID, IntendedEmail: d.Intent.NormalizedEmail, PayloadFingerprint: d.Intent.Fingerprint, Payload: payload})
		if insertErr != nil {
			return result, mapPostgresError("retain Core Bootstrap operation", insertErr, biz.ErrCoreBootstrapConflict)
		}
		op, readErr := q.GetCoreBootstrapOperation(ctx, sqlcgen.GetCoreBootstrapOperationParams{TenantID: d.Intent.TenantID, ID: d.Intent.OperationID})
		if errors.Is(readErr, pgx.ErrNoRows) {
			return result, biz.ErrCoreBootstrapConflict
		}
		if readErr != nil {
			return result, mapPostgresError("read Core Bootstrap operation", readErr, nil)
		}
		if op.SourceKind != "core" || op.IntendedEmail != d.Intent.NormalizedEmail || op.PayloadFingerprint != d.Intent.Fingerprint {
			return result, biz.ErrCoreBootstrapConflict
		}
		received, appendErr := q.AppendCoreBootstrapReceipt(ctx, sqlcgen.AppendCoreBootstrapReceiptParams{TenantID: d.Intent.TenantID, EventID: d.EventID, OperationID: d.Intent.OperationID, Producer: d.Producer, SourceSequence: d.SourceSequence, SourceEpoch: sourceEpoch, PayloadFingerprint: d.Intent.Fingerprint, RawPayload: d.RawPayload, RawSha256: sum[:], OccurredAt: tenantFactTime(d.OccurredAt)})
		if appendErr != nil {
			return result, mapPostgresError("append immutable Core Bootstrap receipt", appendErr, biz.ErrCoreBootstrapConflict)
		}
		result.OperationCreated = n == 1
		result.ReceivedAt = received.Time
	} else {
		return result, mapPostgresError("read Core Bootstrap receipt", err, nil)
	}
	return result, nil
}
