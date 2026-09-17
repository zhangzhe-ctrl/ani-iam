package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (t *platformAdministrationTransaction) coreDLQScope(cap biz.PlatformCapability, consumer uuid.UUID) error {
	if err := recoveryCapability(cap, "listCoreIAMDLQEntries", "getCoreIAMDLQEntry", "replayCoreIAMDLQEntry"); err != nil {
		return err
	}
	if t.broker == nil || t.broker.config.ConsumerID != consumer {
		return biz.ErrCoreDLQNotFound
	}
	return nil
}
func (t *platformAdministrationTransaction) LoadPlatformCoreDLQ(ctx context.Context, cap biz.PlatformCapability, consumer, entry uuid.UUID) (biz.CoreDLQEntry, error) {
	var result biz.CoreDLQEntry
	if err := t.coreDLQScope(cap, consumer); err != nil {
		return result, err
	}
	row, err := t.q.ReadCoreDLQEntry(ctx, sqlcgen.ReadCoreDLQEntryParams{ConsumerID: consumer, EntryID: entry})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrCoreDLQNotFound
	}
	if err != nil {
		return result, mapPostgresError("read DLQ original", err, nil)
	}
	headers := map[string][]string{}
	if json.Unmarshal(row.Headers, &headers) != nil {
		return result, biz.ErrInvalidPersistenceState
	}
	result = biz.CoreDLQEntry{ID: row.ID, ConsumerID: consumer, RawSHA256: hex.EncodeToString(row.RawSha256), LastError: row.LastError, LastAttempt: row.LastAttempt, QuarantinedAt: row.QuarantinedAt.Time,
		Message: biz.CoreBrokerMessage{DeliveryID: row.ID, ConsumerID: consumer, Consumer: t.broker.config.Consumer, BrokerName: row.BrokerName, Account: row.AccountName, Stream: row.StreamName, Subject: row.Subject, BrokerSequence: row.BrokerSequence, DeliveryCount: uint64(row.DeliveryCount), PublishedAt: row.PublishedAt.Time, Payload: append([]byte(nil), row.RawPayload...), Headers: headers}}
	_, err = t.q.ReadCoreDLQContext(ctx, sqlcgen.ReadCoreDLQContextParams{ConsumerID: consumer, EntryID: entry})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return biz.CoreDLQEntry{}, mapPostgresError("read DLQ attribution presence", err, nil)
	}
	result.ProvenanceAvailable = err == nil
	return result, nil
}
func (t *platformAdministrationTransaction) ListPlatformCoreDLQ(ctx context.Context, cap biz.PlatformCapability, consumer, after uuid.UUID, limit int32) ([]biz.CoreDLQEntry, error) {
	if err := t.coreDLQScope(cap, consumer); err != nil {
		return nil, err
	}
	ids, err := t.q.ListCoreDLQEntries(ctx, sqlcgen.ListCoreDLQEntriesParams{ConsumerID: consumer, AfterID: after, PageLimit: limit})
	if err != nil {
		return nil, mapPostgresError("list DLQ original IDs", err, nil)
	}
	result := make([]biz.CoreDLQEntry, 0, len(ids))
	for _, id := range ids {
		v, err := t.LoadPlatformCoreDLQ(ctx, cap, consumer, id)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}
func (t *platformAdministrationTransaction) ListPlatformCoreDLQAttempts(ctx context.Context, cap biz.PlatformCapability, consumer, entry uuid.UUID, after int64, limit int32) ([]biz.CoreDLQAttempt, error) {
	if err := t.coreDLQScope(cap, consumer); err != nil {
		return nil, err
	}
	rows, err := t.q.ListCoreDLQAttempts(ctx, sqlcgen.ListCoreDLQAttemptsParams{ConsumerID: consumer, EntryID: entry, AfterAttempt: after, PageLimit: limit})
	if err != nil {
		return nil, mapPostgresError("list completed DLQ attempts", err, nil)
	}
	result := make([]biz.CoreDLQAttempt, 0, len(rows))
	for _, r := range rows {
		result = append(result, biz.CoreDLQAttempt{ID: r.ID, AuditID: r.AuditEventID, Number: r.AttemptNumber, Outcome: r.Outcome, ProjectionOutcome: r.ProjectionOutcome, ErrorCode: r.ErrorCode, ReasonCode: r.ReasonCode, AuthoritySHA256: r.AuthoritySha256, CreatedAt: r.CreatedAt.Time})
	}
	return result, nil
}
func (t *platformAdministrationTransaction) CheckPlatformCoreDLQSource(ctx context.Context, cap biz.PlatformCapability, entry biz.CoreDLQEntry) (string, error) {
	if err := recoveryCapability(cap, "replayCoreIAMDLQEntry"); err != nil {
		return "", err
	}
	if err := t.coreDLQScope(cap, entry.ConsumerID); err != nil {
		return "", err
	}
	original, err := t.LoadPlatformCoreDLQ(ctx, cap, entry.ConsumerID, entry.ID)
	if err != nil {
		return "", err
	}
	if original.RawSHA256 != entry.RawSHA256 {
		return "", biz.ErrCoreDLQConflict
	}
	proof, err := t.q.ReadCoreDLQContext(ctx, sqlcgen.ReadCoreDLQContextParams{ConsumerID: entry.ConsumerID, EntryID: entry.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", biz.ErrCoreDLQProvenance
	}
	if err != nil {
		return "", mapPostgresError("read immutable DLQ attribution", err, nil)
	}
	current, err := json.Marshal(t.broker.config)
	if err != nil {
		return "", biz.ErrCoreDLQProvenance
	}
	sum := sha256.Sum256(proof.Configuration)
	if !bytes.Equal(sum[:], proof.ConfigurationSha256) || !bytes.Equal(current, proof.Configuration) {
		return "", biz.ErrCoreDLQProvenance
	}
	if err = t.broker.config.message(original.Message); err != nil {
		return "", err
	}
	route, err := t.broker.config.route(original.Message.Subject)
	if err != nil {
		return "", err
	}
	authority, err := t.broker.current(ctx, t.q, route)
	if err != nil {
		return "", err
	}
	return authority.fingerprint(), nil
}

func (t *platformAdministrationTransaction) ReceivePlatformCoreDLQ(ctx context.Context, cap biz.PlatformCapability, c biz.ReplayCoreDLQCommand, decoded biz.CoreBrokerDecoded) (biz.CoreDLQReplayResult, error) {
	var result biz.CoreDLQReplayResult
	if err := recoveryCapability(cap, "replayCoreIAMDLQEntry"); err != nil {
		return result, err
	}
	entry, err := t.LoadPlatformCoreDLQ(ctx, cap, c.ConsumerID, c.EntryID)
	if err != nil {
		return result, err
	}
	if entry.LastAttempt != c.ExpectedAttempt || entry.RawSHA256 != c.ExpectedRawSHA256 {
		return result, biz.ErrCoreDLQConflict
	}
	fingerprint, err := t.CheckPlatformCoreDLQSource(ctx, cap, entry)
	if err != nil {
		return result, err
	}
	// Never use a request-supplied payload. The common helper checks the decoded
	// original against these immutable bytes and current exact route authority.
	receipt, err := t.broker.receiveInTransaction(ctx, t.q, entry.Message, decoded)
	if err != nil {
		return result, err
	}
	result = biz.CoreDLQReplayResult{ConsumerID: c.ConsumerID, EntryID: c.EntryID, EventID: decoded.EventID, TenantID: decoded.TenantID, SourceSequence: decoded.Sequence, RawSHA256: entry.RawSHA256, Outcome: "received", ProjectionOutcome: receipt.Outcome, AuthoritySHA256: fingerprint}
	if decoded.Bootstrap != nil {
		result.OperationID = decoded.Bootstrap.Intent.OperationID
	}
	if receipt.Duplicate {
		result.Outcome = "duplicate"
	} else if receipt.Outcome == "covered" {
		result.Outcome = "covered"
	}
	return result, nil
}
func (t *platformAdministrationTransaction) AppendPlatformCoreDLQAttempt(ctx context.Context, cap biz.PlatformCapability, c biz.ReplayCoreDLQCommand, identity biz.MutationIdentity, result biz.CoreDLQReplayResult, code string) error {
	if err := recoveryCapability(cap, "replayCoreIAMDLQEntry"); err != nil {
		return err
	}
	claims, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	if claims.Subject != identity.ActorID || result.ConsumerID != c.ConsumerID || result.EntryID != c.EntryID {
		return biz.ErrInvalidPersistenceState
	}
	rawHash, err := hex.DecodeString(result.RawSHA256)
	if err != nil || len(rawHash) != 32 {
		return biz.ErrInvalidPersistenceState
	}
	return mapPostgresError("append completed DLQ request attempt", t.q.AppendCoreDLQAttempt(ctx, sqlcgen.AppendCoreDLQAttemptParams{ConsumerID: c.ConsumerID, EntryID: c.EntryID, ID: result.AttemptID, AttemptNumber: result.AttemptNumber, RawSha256: rawHash, RequestHash: identity.Intent[:], ActorID: claims.Subject, ReasonCode: c.ReasonCode, Outcome: result.Outcome, ProjectionOutcome: result.ProjectionOutcome, ErrorCode: code, AuthoritySha256: result.AuthoritySHA256, AuditEventID: result.AuditID}), nil)
}

type coreDLQFailureRecorder struct{ uow *platformAdministrationUOW }

func NewCoreDLQFailureRecorder(d *Data, c CoreBrokerConfiguration) (biz.CoreDLQFailureRecorder, error) {
	broker, err := newCoreBrokerRepository(d, c)
	if err != nil {
		return nil, err
	}
	return &coreDLQFailureRecorder{uow: &platformAdministrationUOW{data: d, broker: broker}}, nil
}
func (r *coreDLQFailureRecorder) RecordCoreDLQFailure(ctx context.Context, cap biz.PlatformCapability, c biz.ReplayCoreDLQCommand, identity biz.MutationIdentity, audit biz.SecurityAuditEvent, attemptID uuid.UUID, code string) error {
	if err := recoveryCapability(cap, "replayCoreIAMDLQEntry"); err != nil {
		return err
	}
	claims, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	if audit.ActorID != claims.Subject || audit.TargetID != c.EntryID || audit.Action != "iam.platform.replayCoreIAMDLQEntry" || audit.Reason != biz.AuditReason(c.ReasonCode) || (audit.Result != biz.AuditResultFailed && audit.Result != biz.AuditResultDenied) {
		return biz.ErrInvalidPersistenceState
	}
	return r.uow.WithinPlatformAdministration(ctx, cap, func(transaction biz.PlatformAdministrationTransaction) error {
		// Serializing with Platform mutations resolves an uncertain original commit
		// before classifying it as a failed attempt. No receipt contents are exposed.
		previous, found, err := transaction.FindPlatformMutation(ctx, cap, identity)
		if err != nil {
			return err
		}
		if found && previous.Identity.Intent == identity.Intent {
			var committed biz.CoreDLQReplayResult
			d := json.NewDecoder(bytes.NewReader(previous.Result))
			d.DisallowUnknownFields()
			if d.Decode(&committed) != nil || d.Decode(new(any)) != io.EOF || committed.ConsumerID != c.ConsumerID || committed.EntryID != c.EntryID || committed.RawSHA256 != c.ExpectedRawSHA256 || committed.AttemptID.Version() != 7 || committed.AuditID.Version() != 7 || committed.AttemptNumber < 1 {
				return biz.ErrInvalidPersistenceState
			}
			// Only this exact attempt can resolve its uncertain commit. An older
			// success must not suppress a later denial or expired-receipt failure.
			if committed.AttemptID == attemptID && committed.AuditID == audit.ID {
				return nil
			}
		}
		entry, err := transaction.LoadPlatformCoreDLQ(ctx, cap, c.ConsumerID, c.EntryID)
		if errors.Is(err, biz.ErrCoreDLQNotFound) {
			return transaction.AppendAudit(ctx, cap, audit)
		}
		if err != nil {
			return err
		}
		audit.TargetVersion = entry.LastAttempt + 1
		if err = transaction.AppendAudit(ctx, cap, audit); err != nil {
			return err
		}
		result := biz.CoreDLQReplayResult{ConsumerID: c.ConsumerID, EntryID: c.EntryID, RawSHA256: entry.RawSHA256, AttemptID: attemptID, AuditID: audit.ID, AttemptNumber: audit.TargetVersion, Outcome: "failed"}
		return transaction.AppendPlatformCoreDLQAttempt(ctx, cap, c, identity, result, code)
	})
}
