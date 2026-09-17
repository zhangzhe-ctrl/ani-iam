package biz

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrCoreProjectionInvalid  = errors.New("invalid Core projection input")
	ErrCoreProjectionConflict = errors.New("Core projection source conflicts")
	ErrCoreProjectionMissing  = errors.New("Core projection is not initialized")
)

const CoreProjectionStaleAfter = 30 * time.Second

// CoreProjectionMessage is decoded input to the persistence port, never an
// authenticated broker identity. The transport receiver must validate authority
// and bind these fields to RawPayload before invoking the port.
type CoreProjectionMessage struct {
	EventID, TenantID uuid.UUID
	Producer, Kind    string
	SourceSequence    int64
	OccurredAt        time.Time
	RawPayload        []byte
	LifecycleVersion  int64
	Status, Reason    string
	EffectiveAt       time.Time
}

func ValidCoreProjectionProducer(p string) bool {
	return p != "" && len(p) <= 128 && p == strings.TrimSpace(p) && utf8.ValidString(p) && !strings.ContainsAny(p, "\x00\r\n\t")
}

func (m CoreProjectionMessage) Validate() error {
	if m.EventID.Version() != 7 || !ValidCoreProjectionProducer(m.Producer) || m.SourceSequence < 1 || m.OccurredAt.IsZero() || len(m.RawPayload) == 0 || len(m.RawPayload) > 65536 || !json.Valid(m.RawPayload) {
		return ErrCoreProjectionInvalid
	}
	switch m.Kind {
	case "heartbeat":
		if m.TenantID != uuid.Nil || m.LifecycleVersion != 0 || m.Status != "" || m.Reason != "" || !m.EffectiveAt.IsZero() {
			return ErrCoreProjectionInvalid
		}
	case "bootstrap":
		if m.TenantID.Version() != 7 || m.LifecycleVersion != 0 || m.Status != "" || m.Reason != "" || !m.EffectiveAt.IsZero() {
			return ErrCoreProjectionInvalid
		}
	case "lifecycle":
		if m.TenantID.Version() != 7 || m.LifecycleVersion < 1 || m.EffectiveAt.IsZero() {
			return ErrCoreProjectionInvalid
		}
		valid := false
		switch m.Status {
		case "active":
			valid = m.Reason == "created" || m.Reason == "restored"
		case "frozen":
			valid = m.Reason == "administrative_freeze" || m.Reason == "billing_suspension"
		case "disabled":
			valid = m.Reason == "user_requested_deactivation"
		}
		if !valid {
			return ErrCoreProjectionInvalid
		}
	default:
		return ErrCoreProjectionInvalid
	}
	return nil
}

// CoreTenantProjection preserves Core vocabulary (active/frozen/disabled),
// distinct from IAM Access. Version zero is only a placeholder for a missing
// initial fact; it can never be fresh or grant authority.
type CoreTenantProjection struct {
	Version, RequiredVersion int64
	Status, Reason           string
	EffectiveAt              time.Time
	RepairRequired           bool
	SnapshotBase             bool
}

// ProjectCoreLifecycle never repairs a gap by applying later deliveries. A
// consistent Snapshot must repair/replace that generation first.
func ProjectCoreLifecycle(previous CoreTenantProjection, m CoreProjectionMessage) (CoreTenantProjection, string, error) {
	if err := m.Validate(); err != nil || m.Kind != "lifecycle" {
		return previous, "", ErrCoreProjectionInvalid
	}
	if previous.RepairRequired {
		previous.RequiredVersion = max(previous.RequiredVersion, m.LifecycleVersion)
		return previous, "awaiting_snapshot", nil
	}
	if m.LifecycleVersion < previous.Version {
		return previous, "older", nil
	}
	if m.LifecycleVersion == previous.Version {
		if m.Status != previous.Status || (!previous.SnapshotBase && (m.Reason != previous.Reason || !m.EffectiveAt.Truncate(time.Microsecond).Equal(previous.EffectiveAt.Truncate(time.Microsecond)))) {
			return previous, "", ErrCoreProjectionConflict
		}
		if previous.SnapshotBase {
			return CoreTenantProjection{Version: m.LifecycleVersion, RequiredVersion: m.LifecycleVersion, Status: m.Status, Reason: m.Reason, EffectiveAt: m.EffectiveAt.Truncate(time.Microsecond)}, "applied", nil
		}
		return previous, "same_version", nil
	}
	if m.LifecycleVersion != previous.Version+1 {
		previous.RepairRequired = true
		previous.RequiredVersion = m.LifecycleVersion
		return previous, "version_gap", nil
	}
	if previous.Status == "disabled" || (previous.Version == 0 && (m.Status != "active" || m.Reason != "created")) {
		return previous, "", ErrCoreProjectionConflict
	}
	return CoreTenantProjection{Version: m.LifecycleVersion, RequiredVersion: m.LifecycleVersion, Status: m.Status, Reason: m.Reason, EffectiveAt: m.EffectiveAt.Truncate(time.Microsecond)}, "applied", nil
}

type CoreProjectionReceipt struct {
	Duplicate                           bool
	Outcome                             string
	ContiguousSequence, HighestSequence int64
}

type CoreShadowTenant struct {
	CoreTenantProjection
	GenerationID                        uuid.UUID
	ContiguousSequence, HighestSequence int64
	Fresh                               bool
}

// CoreLifecycleProjectionRepository is an observation path until authenticated
// delivery, rebuild and the real shadow-exit gate have all been established.
// It is deliberately not registered as the current authorization reader.
type CoreLifecycleProjectionRepository interface {
	InitializeShadow(context.Context) error
	Apply(context.Context, CoreProjectionMessage) (CoreProjectionReceipt, error)
	ReadShadowTenant(context.Context, TenantScope) (CoreShadowTenant, error)
}
