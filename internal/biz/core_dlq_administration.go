package biz

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCoreDLQInvalid          = errors.New("DLQ request is invalid")
	ErrCoreDLQNotFound         = errors.New("DLQ entry was not found")
	ErrCoreDLQConflict         = errors.New("DLQ original or attempt precondition changed")
	ErrCoreDLQProvenance       = errors.New("DLQ original receiver attribution is unavailable")
	ErrCoreDLQAuditUnavailable = errors.New("DLQ failure evidence could not be persisted")
)

type CoreDLQEntry struct {
	ID, ConsumerID      uuid.UUID
	Message             CoreBrokerMessage
	RawSHA256           string
	LastError           string
	LastAttempt         int64
	QuarantinedAt       time.Time
	ProvenanceAvailable bool
}
type CoreDLQAttempt struct {
	ID, AuditID                                                        uuid.UUID
	Number                                                             int64
	Outcome, ProjectionOutcome, ErrorCode, ReasonCode, AuthoritySHA256 string
	CreatedAt                                                          time.Time
}
type CoreDLQEntryPage struct {
	Entries    []CoreDLQEntry
	NextCursor string
}
type CoreDLQEntryDetail struct {
	Entry      CoreDLQEntry
	Attempts   []CoreDLQAttempt
	NextCursor string
}
type ReplayCoreDLQCommand struct {
	ConsumerID, EntryID        uuid.UUID
	ExpectedRawSHA256          string
	ExpectedAttempt            int64
	ReasonCode, IdempotencyKey string
}
type CoreDLQReplayResult struct {
	ConsumerID, EntryID, AttemptID, AuditID, EventID, TenantID, OperationID uuid.UUID
	AttemptNumber, SourceSequence                                           int64
	RawSHA256, Outcome, ProjectionOutcome, AuthoritySHA256                  string
	Replayed                                                                bool
}

type CoreDLQAdministrationRepository interface {
	ListPlatformCoreDLQ(context.Context, PlatformCapability, uuid.UUID, uuid.UUID, int32) ([]CoreDLQEntry, error)
	LoadPlatformCoreDLQ(context.Context, PlatformCapability, uuid.UUID, uuid.UUID) (CoreDLQEntry, error)
	ListPlatformCoreDLQAttempts(context.Context, PlatformCapability, uuid.UUID, uuid.UUID, int64, int32) ([]CoreDLQAttempt, error)
	CheckPlatformCoreDLQSource(context.Context, PlatformCapability, CoreDLQEntry) (string, error)
	ReceivePlatformCoreDLQ(context.Context, PlatformCapability, ReplayCoreDLQCommand, CoreBrokerDecoded) (CoreDLQReplayResult, error)
	AppendPlatformCoreDLQAttempt(context.Context, PlatformCapability, ReplayCoreDLQCommand, MutationIdentity, CoreDLQReplayResult, string) error
}

// A failure recorder can only retain a completed failed request after rollback.
// It cannot queue work or retain a Human authorization for future execution.
type CoreDLQFailureRecorder interface {
	RecordCoreDLQFailure(context.Context, PlatformCapability, ReplayCoreDLQCommand, MutationIdentity, SecurityAuditEvent, uuid.UUID, string) error
}

func (u *PlatformAdministrationUsecase) WithCoreDLQAdministration(decoder CoreBrokerDecoder, failures CoreDLQFailureRecorder) *PlatformAdministrationUsecase {
	u.dlqDecoder, u.dlqFailures = decoder, failures
	return u
}

type coreDLQCursor struct {
	Operation, Revision           string
	Actor, Consumer, Entry, After uuid.UUID
	Attempt                       int64
}

func coreDLQPage(cap PlatformCapability, operation string, consumer, entry uuid.UUID, raw string, limit uint32) (coreDLQCursor, int32, error) {
	want := coreDLQCursor{Operation: operation, Revision: cap.revision, Actor: cap.claims.Subject, Consumer: consumer, Entry: entry}
	if consumer.Version() != 7 || limit > 100 || len(raw) > 2048 {
		return want, 0, ErrCoreDLQInvalid
	}
	if limit == 0 {
		limit = 20
	}
	if raw != "" {
		value, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return want, 0, ErrCoreDLQInvalid
		}
		var cursor coreDLQCursor
		if json.Unmarshal(value, &cursor) != nil || cursor.Operation != want.Operation || cursor.Revision != want.Revision || cursor.Actor != want.Actor || cursor.Consumer != consumer || cursor.Entry != entry || cursor.Attempt < 0 {
			return want, 0, ErrCoreDLQInvalid
		}
		want = cursor
	}
	return want, int32(limit), nil
}
func coreDLQNext(c coreDLQCursor) string {
	value, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(value)
}

func (u *PlatformAdministrationUsecase) ListCoreDLQEntries(ctx context.Context, cap PlatformCapability, consumer uuid.UUID, cursor string, limit uint32) (CoreDLQEntryPage, error) {
	const op = "listCoreIAMDLQEntries"
	page, n, err := coreDLQPage(cap, op, consumer, uuid.Nil, cursor, limit)
	if err != nil {
		return CoreDLQEntryPage{}, err
	}
	result := CoreDLQEntryPage{Entries: []CoreDLQEntry{}}
	err = u.within(ctx, cap, op, consumer, nil, func(tx PlatformAdministrationTransaction) error {
		values, err := tx.ListPlatformCoreDLQ(ctx, cap, consumer, page.After, n+1)
		if err != nil {
			return err
		}
		if len(values) > int(n) {
			values = values[:n]
			page.After = values[len(values)-1].ID
			result.NextCursor = coreDLQNext(page)
		}
		for i := range values {
			values[i].Message.Payload = nil
			values[i].Message.Headers = nil
		}
		result.Entries = values
		return nil
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) GetCoreDLQEntry(ctx context.Context, cap PlatformCapability, consumer, entry uuid.UUID, cursor string, limit uint32) (CoreDLQEntryDetail, error) {
	const op = "getCoreIAMDLQEntry"
	var result CoreDLQEntryDetail
	if entry.Version() != 7 {
		return result, ErrCoreDLQInvalid
	}
	page, n, err := coreDLQPage(cap, op, consumer, entry, cursor, limit)
	if err != nil {
		return result, err
	}
	err = u.within(ctx, cap, op, entry, nil, func(tx PlatformAdministrationTransaction) error {
		result.Entry, err = tx.LoadPlatformCoreDLQ(ctx, cap, consumer, entry)
		if err != nil {
			return err
		}
		result.Attempts, err = tx.ListPlatformCoreDLQAttempts(ctx, cap, consumer, entry, page.Attempt, n+1)
		if err != nil {
			return err
		}
		if len(result.Attempts) > int(n) {
			result.Attempts = result.Attempts[:n]
			page.Attempt = result.Attempts[len(result.Attempts)-1].Number
			result.NextCursor = coreDLQNext(page)
		}
		return nil
	})
	return result, err
}

func (u *PlatformAdministrationUsecase) recheckCoreDLQHuman(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability) error {
	now := u.clock.Now().UTC()
	if !now.Before(cap.claims.ExpiresAt) {
		return ErrInvalidCredential
	}
	state, err := tx.LookupAuthority(ctx, cap, "iam.dlq", []string{"replay"})
	if err != nil {
		return err
	}
	if platformAuthorizationDenial(state, cap.claims, now) != "" || !state.PermissionAllowed {
		return ErrPlatformAdministrationDenied
	}
	return nil
}

func (u *PlatformAdministrationUsecase) ReplayCoreDLQEntry(ctx context.Context, cap PlatformCapability, c ReplayCoreDLQCommand) (CoreDLQReplayResult, error) {
	const op = "replayCoreIAMDLQEntry"
	var result CoreDLQReplayResult
	rawHash, err := hex.DecodeString(c.ExpectedRawSHA256)
	if err != nil || len(rawHash) != 32 || hex.EncodeToString(rawHash) != c.ExpectedRawSHA256 || c.ConsumerID.Version() != 7 || c.EntryID.Version() != 7 || c.ExpectedAttempt < 0 || c.ExpectedAttempt == math.MaxInt64 || !validBootstrapReissueReason(c.ReasonCode) || cap.reason != c.ReasonCode {
		return result, ErrCoreDLQInvalid
	}
	if u == nil || u.dlqDecoder == nil || u.dlqFailures == nil || u.clock == nil || u.ids == nil || u.uow == nil {
		return result, ErrCoreBrokerAuthority
	}
	identity, err := platformMutationIdentity(cap, op, c.IdempotencyKey, c)
	if err != nil {
		return result, err
	}
	attemptID, err := u.newAdministrationID()
	if err != nil {
		return result, err
	}
	auditID, err := u.newAdministrationID()
	if err != nil || auditID == attemptID {
		return result, ErrInvalidGeneratedID
	}
	err = u.withinAuthorized(ctx, cap, op, func(tx PlatformAdministrationTransaction, now time.Time) error {
		entry, err := tx.LoadPlatformCoreDLQ(ctx, cap, c.ConsumerID, c.EntryID)
		if err != nil {
			return err
		}
		if entry.RawSHA256 != c.ExpectedRawSHA256 {
			return ErrCoreDLQConflict
		}
		authority, err := tx.CheckPlatformCoreDLQSource(ctx, cap, entry)
		if err != nil {
			return err
		}
		if err = u.recheckCoreDLQHuman(ctx, tx, cap); err != nil {
			return err
		}
		previous, found, err := tx.FindPlatformMutation(ctx, cap, identity)
		if err != nil {
			return err
		}
		if found {
			if previous.Identity.Intent != identity.Intent {
				return ErrIdempotencyConflict
			}
			if !u.clock.Now().Before(previous.ExpiresAt) {
				return ErrIdempotencyExpired
			}
			d := json.NewDecoder(bytes.NewReader(previous.Result))
			d.DisallowUnknownFields()
			if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.ConsumerID != c.ConsumerID || result.EntryID != c.EntryID || result.RawSHA256 != c.ExpectedRawSHA256 || result.AttemptID.Version() != 7 || result.AuditID.Version() != 7 || result.AttemptNumber < 1 {
				return ErrInvalidPersistenceState
			}
			result.Replayed = true
			return u.recheckCoreDLQHuman(ctx, tx, cap)
		}
		if entry.LastAttempt != c.ExpectedAttempt {
			return ErrCoreDLQConflict
		}
		decoded, err := u.dlqDecoder.Decode(entry.Message)
		if err != nil {
			return err
		}
		result, err = tx.ReceivePlatformCoreDLQ(ctx, cap, c, decoded)
		if err != nil {
			return err
		}
		if result.AuthoritySHA256 != authority {
			return ErrCoreBrokerAuthority
		}
		result.AttemptID, result.AuditID, result.AttemptNumber = attemptID, auditID, entry.LastAttempt+1
		if err = u.recheckCoreDLQHuman(ctx, tx, cap); err != nil {
			return err
		}
		now = u.clock.Now().UTC()
		if err = tx.AppendAudit(ctx, cap, newPlatformAdministrationAudit(cap, "iam.dlq", auditID, c.EntryID, result.AttemptNumber, now)); err != nil {
			return err
		}
		if err = tx.AppendPlatformCoreDLQAttempt(ctx, cap, c, identity, result, ""); err != nil {
			return err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return ErrInvalidPersistenceState
		}
		if err = tx.SavePlatformMutation(ctx, cap, StoredMutation{Identity: identity, Result: encoded, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
			return err
		}
		if _, err = tx.CheckPlatformCoreDLQSource(ctx, cap, entry); err != nil {
			return err
		}
		return u.recheckCoreDLQHuman(ctx, tx, cap)
	})
	if err == nil {
		return result, nil
	}
	// The business transaction has rolled back (or its commit was uncertain).
	// A separate bounded audit-only transaction checks committed receipts before
	// retaining a failed attempt; it can never apply an event or grant authority.
	failure := newPlatformAdministrationAudit(cap, "iam.dlq", auditID, c.EntryID, 1, u.clock.Now().UTC())
	failure.Result = AuditResultFailed
	code := CoreDLQFailureCode(err)
	if code == "human_authority_denied" || code == "broker_authority_denied" {
		failure.Result = AuditResultDenied
	}
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if e := u.dlqFailures.RecordCoreDLQFailure(record, cap, c, identity, failure, attemptID, code); e != nil {
		return CoreDLQReplayResult{}, errors.Join(err, ErrCoreDLQAuditUnavailable)
	}
	return CoreDLQReplayResult{}, err
}

func CoreDLQFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrCoreDLQProvenance):
		return "provenance_unavailable"
	case errors.Is(err, ErrCoreDLQNotFound):
		return "entry_not_found"
	case errors.Is(err, ErrCoreDLQConflict), errors.Is(err, ErrIdempotencyConflict):
		return "request_conflict"
	case errors.Is(err, ErrIdempotencyExpired):
		return "idempotency_expired"
	case errors.Is(err, ErrPlatformAdministrationDenied), errors.Is(err, ErrInvalidCredential):
		return "human_authority_denied"
	case errors.Is(err, ErrCoreBrokerAuthority):
		return "broker_authority_denied"
	case errors.Is(err, ErrCoreProjectionInvalid), errors.Is(err, ErrCoreBootstrapInvalid):
		return "invalid_event"
	case errors.Is(err, ErrCoreProjectionConflict), errors.Is(err, ErrCoreBootstrapConflict):
		return "event_conflict"
	default:
		return "dependency_unavailable"
	}
}
