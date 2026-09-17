package biz

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrTenantLifecycleInvalid  = errors.New("invalid Tenant lifecycle fact")
	ErrTenantLifecycleConflict = errors.New("conflicting Tenant lifecycle fact")
)

const TenantLifecycleStaleAfter = 30 * time.Second

// TenantLifecycleEvent is a domain fact, not producer authentication. The
// receiver binds it to one immutable receipt and current broker authority in
// the same transaction that advances the projection.
type TenantLifecycleEvent struct {
	EventID, Epoch, TenantID uuid.UUID
	Sequence, TenantVersion  int64
	Status, Reason           string
	OccurredAt, EffectiveAt  time.Time
}

// TenantBrokerDelivery distinguishes stream facts, independent observations,
// and identity intent. Sequence on a Bootstrap delivery is a reference to its
// creating Lifecycle fact, never a new stream position.
type TenantBrokerDelivery struct {
	EventID, Epoch, TenantID uuid.UUID
	Producer, Kind           string
	Sequence                 int64
	OccurredAt               time.Time
	RawPayload               []byte
	Lifecycle                *TenantLifecycleEvent
	Heartbeat                *TenantLifecycleHeartbeat
	Bootstrap                *CoreBootstrapDelivery
}

func (d TenantBrokerDelivery) Validate() error {
	if !lifecycleID(d.EventID) || !lifecycleID(d.Epoch) || !ValidCoreProjectionProducer(d.Producer) || d.OccurredAt.IsZero() || len(d.RawPayload) == 0 || len(d.RawPayload) > 65536 || !json.Valid(d.RawPayload) {
		return ErrTenantLifecycleInvalid
	}
	switch d.Kind {
	case "lifecycle":
		e := d.Lifecycle
		if e == nil || d.Heartbeat != nil || d.Bootstrap != nil || e.Validate() != nil || e.EventID != d.EventID || e.Epoch != d.Epoch || e.TenantID != d.TenantID || e.Sequence != d.Sequence || !e.OccurredAt.Equal(d.OccurredAt) {
			return ErrTenantLifecycleInvalid
		}
	case "heartbeat":
		h := d.Heartbeat
		if h == nil || d.Lifecycle != nil || d.Bootstrap != nil || h.Validate() != nil || h.ID != d.EventID || h.Epoch != d.Epoch || d.TenantID != uuid.Nil || d.Sequence != 0 || !h.ObservedAt.Equal(d.OccurredAt) {
			return ErrTenantLifecycleInvalid
		}
	case "bootstrap":
		b := d.Bootstrap
		if b == nil || d.Lifecycle != nil || d.Heartbeat != nil || b.Validate() != nil || b.EventID != d.EventID || b.Intent.TenantID != d.TenantID || b.SourceSequence != d.Sequence || b.Producer != d.Producer || !b.OccurredAt.Equal(d.OccurredAt) || !bytes.Equal(b.RawPayload, d.RawPayload) {
			return ErrTenantLifecycleInvalid
		}
	default:
		return ErrTenantLifecycleInvalid
	}
	return nil
}

type TenantBrokerReceipt struct {
	Duplicate        bool
	Outcome          string
	Applied, Highest int64
}

func (e TenantLifecycleEvent) Validate() error {
	if !lifecycleID(e.EventID) || !lifecycleID(e.Epoch) || !lifecycleID(e.TenantID) || e.Sequence < 1 || e.TenantVersion < 1 ||
		!lifecycleStatus(e.Status) || strings.TrimSpace(e.Reason) == "" || !utf8.ValidString(e.Reason) || utf8.RuneCountInString(e.Reason) > 1024 ||
		e.OccurredAt.IsZero() || !e.EffectiveAt.Equal(e.OccurredAt) {
		return ErrTenantLifecycleInvalid
	}
	return nil
}

// Heartbeats have their own immutable identity. Their watermarks observe the
// Lifecycle stream; receiving one never allocates or applies a Lifecycle slot.
type TenantLifecycleHeartbeat struct {
	ID, Epoch            uuid.UUID
	Committed, Published int64
	ObservedAt           time.Time
}

func (h TenantLifecycleHeartbeat) Validate() error {
	if !lifecycleID(h.ID) || !lifecycleID(h.Epoch) || h.Committed < 0 || h.Published < 0 || h.Published > h.Committed || h.ObservedAt.IsZero() {
		return ErrTenantLifecycleInvalid
	}
	return nil
}

type TenantLifecycleFact struct {
	Version        int64
	Status, Reason string
	EffectiveAt    time.Time
	SnapshotBase   bool
}

// TenantLifecyclePipeline contains only lifecycle continuity and freshness.
// Bootstrap receipts, invitations and identities never advance this state.
type TenantLifecyclePipeline struct {
	Epoch               uuid.UUID
	Applied, Highest    int64
	SnapshotRequired    bool
	Heartbeat           TenantLifecycleHeartbeat
	HeartbeatReceivedAt time.Time
}

type TenantLifecycleUpdate struct {
	Pipeline TenantLifecyclePipeline
	Fact     TenantLifecycleFact
	Outcome  string
}

// ProjectTenantLifecycle is called under the pipeline transaction lock after
// exact event-ID/epoch/sequence conflict checks against durable receipts.
// An out-of-order receipt is retained for Snapshot catch-up, never applied over
// a hole. A snapshot is mandatory when first discovering or changing an epoch.
func ProjectTenantLifecycle(p TenantLifecyclePipeline, previous TenantLifecycleFact, e TenantLifecycleEvent) (TenantLifecycleUpdate, error) {
	result := TenantLifecycleUpdate{Pipeline: p, Fact: previous}
	if err := e.Validate(); err != nil {
		return result, err
	}
	if p.Epoch != e.Epoch {
		result.Pipeline.SnapshotRequired = true
		result.Outcome = "epoch_requires_snapshot"
		return result, nil
	}
	result.Pipeline.Highest = max(p.Highest, e.Sequence)
	if p.SnapshotRequired {
		result.Outcome = "awaiting_snapshot"
		return result, nil
	}
	if e.Sequence <= p.Applied {
		result.Outcome = "covered"
		return result, nil
	}
	if e.Sequence != p.Applied+1 {
		result.Pipeline.SnapshotRequired = true
		result.Outcome = "sequence_gap"
		return result, nil
	}
	if e.TenantVersion <= previous.Version {
		return result, ErrTenantLifecycleConflict
	}
	if e.TenantVersion != previous.Version+1 {
		result.Pipeline.SnapshotRequired = true
		result.Outcome = "tenant_version_gap"
		return result, nil
	}
	if !lifecycleTransition(previous.Status, e.Status, previous.Version == 0) {
		return result, ErrTenantLifecycleConflict
	}
	result.Pipeline.Applied = e.Sequence
	result.Fact = TenantLifecycleFact{Version: e.TenantVersion, Status: e.Status, Reason: e.Reason, EffectiveAt: e.EffectiveAt}
	result.Outcome = "applied"
	return result, nil
}

// ObserveTenantHeartbeat runs only after current producer/consumer authority
// and immutable heartbeat receipt checks. A replay or older source timestamp
// cannot renew the observation window, even when received over a healthy link.
func ObserveTenantHeartbeat(p TenantLifecyclePipeline, h TenantLifecycleHeartbeat, receivedAt time.Time) (TenantLifecyclePipeline, string, error) {
	if h.Validate() != nil || receivedAt.IsZero() || h.ObservedAt.After(receivedAt) {
		return p, "", ErrTenantLifecycleInvalid
	}
	if p.Epoch != h.Epoch {
		p.SnapshotRequired = true
		return p, "epoch_requires_snapshot", nil
	}
	if h.ID == p.Heartbeat.ID || !h.ObservedAt.After(p.Heartbeat.ObservedAt) {
		return p, "older_heartbeat", nil
	}
	if h.Committed < p.Heartbeat.Committed || h.Published < p.Heartbeat.Published {
		return p, "", ErrTenantLifecycleConflict
	}
	p.Heartbeat, p.HeartbeatReceivedAt = h, receivedAt
	p.Highest = max(p.Highest, h.Committed)
	return p, "observed", nil
}

func (p TenantLifecyclePipeline) Fresh(now time.Time) bool {
	return lifecycleID(p.Epoch) && !p.SnapshotRequired && p.Applied >= 0 && p.Applied == p.Highest &&
		p.Heartbeat.Validate() == nil && p.Heartbeat.Epoch == p.Epoch && p.Applied >= p.Heartbeat.Committed &&
		!p.HeartbeatReceivedAt.IsZero() && !p.HeartbeatReceivedAt.After(now) && !p.Heartbeat.ObservedAt.After(now) &&
		now.Before(p.HeartbeatReceivedAt.Add(TenantLifecycleStaleAfter)) && now.Before(p.Heartbeat.ObservedAt.Add(TenantLifecycleStaleAfter))
}

// ActivateTenantLifecycleSnapshot is used only after all immutable pages have
// been validated and stored in one shadow generation. highestRetained is the
// maximum retained Lifecycle sequence for this exact epoch, not a broker
// sequence or a local wall-clock cut. The swap and catch-up cursor are atomic.
// A new authenticated heartbeat is required after activation.
func ActivateTenantLifecycleSnapshot(epoch uuid.UUID, watermark, highestRetained int64) (TenantLifecyclePipeline, error) {
	if !lifecycleID(epoch) || watermark < 0 || highestRetained < 0 {
		return TenantLifecyclePipeline{}, ErrTenantLifecycleInvalid
	}
	return TenantLifecyclePipeline{Epoch: epoch, Applied: watermark, Highest: max(watermark, highestRetained)}, nil
}

func lifecycleID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func lifecycleStatus(status string) bool {
	return status == "active" || status == "frozen" || status == "disabled"
}
func lifecycleTransition(from, to string, initial bool) bool {
	if initial {
		return to == "active"
	}
	return (from == "active" && (to == "frozen" || to == "disabled")) || (from == "frozen" && (to == "active" || to == "disabled"))
}
