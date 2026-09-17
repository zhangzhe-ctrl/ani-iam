// Package tenantbootstrap owns the sole IAM Bootstrap request encoding and
// identity-intent fingerprint consumed by reliable producers and IAM adapters.
package tenantbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"golang.org/x/net/idna"
	"google.golang.org/protobuf/encoding/protojson"
)

const Revision = "iam-tenant-bootstrap-v1"
const Subject = "iam.tenant.bootstrap.v1"
const MaxPayloadBytes = 65536

var ErrInvalid = errors.New("invalid tenant Bootstrap request")

// NormalizeEmail follows the existing IAM Invitation normalization: lowercase
// local part, lowercase IDNA Lookup ASCII domain, and no display-name syntax.
// Normalization does not verify identity or establish any membership.
func NormalizeEmail(value string) (string, error) {
	email := strings.TrimSpace(value)
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.Contains(email[:at], "@") || strings.ContainsAny(email[:at], " \t\r\n") {
		return "", ErrInvalid
	}
	domain, err := idna.Lookup.ToASCII(strings.ToLower(email[at+1:]))
	if err != nil || domain == "" {
		return "", ErrInvalid
	}
	email = strings.ToLower(email[:at]) + "@" + strings.ToLower(domain)
	if !utf8.ValidString(email) || len(email) > 320 {
		return "", ErrInvalid
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" {
		return "", ErrInvalid
	}
	return email, nil
}

func validID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id.Version() == 7 && id.String() == raw
}

// Fingerprint binds only immutable identity intent. Event/retry metadata never
// changes the intended administrator. JSON keys are sorted by encoding/json;
// its escaping, including HTML-sensitive characters, is part of the contract.
func Fingerprint(tenantID, operationID, normalizedEmail, locale string) (string, error) {
	email, err := NormalizeEmail(normalizedEmail)
	if err != nil || email != normalizedEmail || !validID(tenantID) || !validID(operationID) || (locale != "en-US" && locale != "zh-CN") {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(map[string]string{"locale": locale, "normalized_email": email, "operation_id": operationID, "tenant_id": tenantID})
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func Validate(r *iamv1.TenantBootstrapRequested) error {
	if r == nil || r.SchemaRevision != Revision || !validID(r.EventId) || !validID(r.SourceEpoch) || r.LifecycleSequence < 1 || r.OccurredAt == nil || r.OccurredAt.CheckValid() != nil || r.OccurredAt.AsTime().IsZero() {
		return ErrInvalid
	}
	if r.Producer == "" || len(r.Producer) > 128 || r.Producer != strings.TrimSpace(r.Producer) || !utf8.ValidString(r.Producer) || strings.ContainsAny(r.Producer, "\x00\r\n\t") {
		return ErrInvalid
	}
	a := r.GetIntendedAdministrator()
	expected, err := Fingerprint(r.TenantId, r.OperationId, a.GetNormalizedEmail(), a.GetLocale())
	if err != nil || r.PayloadFingerprint != expected {
		return ErrInvalid
	}
	return nil
}

func Marshal(r *iamv1.TenantBootstrapRequested) ([]byte, error) {
	if err := Validate(r); err != nil {
		return nil, err
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(r)
	if err != nil || len(raw) > MaxPayloadBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}

// Parse accepts only the target schema's snake_case JSON names and string int64.
// ProtoJSON rejects duplicate keys, unknown fields, malformed UTF-8 and trailing
// documents. It is not a second legacy decoder or a broker authentication step.
func Parse(raw []byte) (*iamv1.TenantBootstrapRequested, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes {
		return nil, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, ErrInvalid
	}
	allowed := map[string]bool{"schema_revision": true, "event_id": true, "producer": true, "tenant_id": true, "operation_id": true, "occurred_at": true, "source_epoch": true, "lifecycle_sequence": true, "intended_administrator": true, "payload_fingerprint": true}
	for key := range fields {
		if !allowed[key] {
			return nil, ErrInvalid
		}
	}
	var sequence string
	if json.Unmarshal(fields["lifecycle_sequence"], &sequence) != nil || sequence == "" {
		return nil, ErrInvalid
	}
	var administrator map[string]json.RawMessage
	if json.Unmarshal(fields["intended_administrator"], &administrator) != nil {
		return nil, ErrInvalid
	}
	for key := range administrator {
		if key != "normalized_email" && key != "locale" {
			return nil, ErrInvalid
		}
	}
	result := new(iamv1.TenantBootstrapRequested)
	if (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, result) != nil || Validate(result) != nil {
		return nil, ErrInvalid
	}
	return result, nil
}
