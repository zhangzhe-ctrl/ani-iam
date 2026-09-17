package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrCoreBootstrapInvalid  = errors.New("invalid Core Bootstrap receipt")
	ErrCoreBootstrapConflict = errors.New("Core Bootstrap intent or receipt conflicts")
)

// CoreBootstrapIntent is the immutable identity intent owned by Core. Receipt
// neither verifies an email nor creates Access, Membership, Credential or Session.
type CoreBootstrapIntent struct {
	TenantID, OperationID                uuid.UUID
	NormalizedEmail, Locale, Fingerprint string
}

// CanonicalPayload matches the frozen Core v1 fingerprint preimage: sorted JSON
// object keys, no whitespace, and Go encoding/json string escaping (including
// HTML-sensitive characters). The receiver rejects non-normalized input instead
// of changing an identity after the producer has committed it.
func (i CoreBootstrapIntent) CanonicalPayload() ([]byte, error) {
	email, err := normalizeInvitationEmail(i.NormalizedEmail)
	if err != nil || email != i.NormalizedEmail || i.TenantID.Version() != 7 || i.OperationID.Version() != 7 || (i.Locale != "en-US" && i.Locale != "zh-CN") {
		return nil, ErrCoreBootstrapInvalid
	}
	raw, err := json.Marshal(map[string]string{"locale": i.Locale, "normalized_email": email, "operation_id": i.OperationID.String(), "tenant_id": i.TenantID.String()})
	if err != nil {
		return nil, ErrCoreBootstrapInvalid
	}
	sum := sha256.Sum256(raw)
	if i.Fingerprint != "sha256:"+hex.EncodeToString(sum[:]) {
		return nil, ErrCoreBootstrapInvalid
	}
	return raw, nil
}

// CoreBootstrapDelivery contains decoded domain fields and the original wire
// bytes for durable evidence. Producer is a logical name, never authentication.
// A future transport receiver MUST verify current broker authority and the
// raw-message/domain binding before calling this persistence port. No public
// transport or composition-root registration is provided by this component.
type CoreBootstrapDelivery struct {
	Intent         CoreBootstrapIntent
	EventID        uuid.UUID
	Producer       string
	SourceSequence int64
	OccurredAt     time.Time
	RawPayload     []byte
}

func (d CoreBootstrapDelivery) Validate() error {
	if _, err := d.Intent.CanonicalPayload(); err != nil {
		return err
	}
	if d.EventID.Version() != 7 || d.Producer == "" || d.Producer != strings.TrimSpace(d.Producer) || len(d.Producer) > 128 || !utf8.ValidString(d.Producer) || strings.ContainsAny(d.Producer, "\x00\r\n\t") || d.SourceSequence < 1 || d.OccurredAt.IsZero() || len(d.RawPayload) == 0 || len(d.RawPayload) > 65536 || !json.Valid(d.RawPayload) {
		return ErrCoreBootstrapInvalid
	}
	return nil
}

type CoreBootstrapReceipt struct {
	EventID, OperationID             uuid.UUID
	ReceivedAt                       time.Time
	OperationCreated, DuplicateEvent bool
}

// CoreBootstrapReceiptRepository atomically retains an operation and immutable
// receipt. A successful return is a durability condition, not an authorization
// decision or permission to ACK a message without the receiver's other checks.
type CoreBootstrapReceiptRepository interface {
	Receive(context.Context, CoreBootstrapDelivery) (CoreBootstrapReceipt, error)
}
