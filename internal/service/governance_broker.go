package service

import (
	"github.com/google/uuid"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
)

type governanceBrokerDecoder struct{}

// NewGovernanceBrokerDecoder consumes exactly the two Governance-owned message
// DTOs and IAM's Bootstrap DTO. It has no legacy subject or decoder fallback.
// Payload producer fields never substitute for broker provenance checks.
func NewGovernanceBrokerDecoder() biz.CoreBrokerDecoder { return governanceBrokerDecoder{} }

func (governanceBrokerDecoder) Decode(message biz.CoreBrokerMessage) (biz.TenantBrokerDelivery, error) {
	result := biz.TenantBrokerDelivery{RawPayload: append([]byte(nil), message.Payload...)}
	invalid := func() (biz.TenantBrokerDelivery, error) {
		return biz.TenantBrokerDelivery{}, biz.ErrTenantLifecycleInvalid
	}
	switch message.Subject {
	case governancev1.LifecycleSubject:
		dto, err := governancev1.ParseLifecycle(message.Payload)
		if err != nil {
			return invalid()
		}
		event := biz.TenantLifecycleEvent{EventID: uuid.MustParse(dto.EventId), Epoch: uuid.MustParse(dto.Epoch), TenantID: uuid.MustParse(dto.TenantId), Sequence: dto.Sequence, TenantVersion: dto.TenantVersion, Status: string(dto.BusinessStatus), Reason: dto.Reason, OccurredAt: dto.OccurredAt, EffectiveAt: dto.EffectiveAt}
		if event.Validate() != nil {
			return invalid()
		}
		result.EventID, result.Epoch, result.TenantID, result.Sequence = event.EventID, event.Epoch, event.TenantID, event.Sequence
		result.Producer, result.Kind, result.OccurredAt, result.Lifecycle = string(dto.Producer), "lifecycle", event.OccurredAt, &event
	case governancev1.HeartbeatSubject:
		dto, err := governancev1.ParseHeartbeat(message.Payload)
		if err != nil {
			return invalid()
		}
		heartbeat := biz.TenantLifecycleHeartbeat{ID: uuid.MustParse(dto.HeartbeatId), Epoch: uuid.MustParse(dto.Epoch), Committed: dto.CommittedSequence, Published: dto.PublishedSequence, ObservedAt: dto.ObservedAt}
		if heartbeat.Validate() != nil {
			return invalid()
		}
		result.EventID, result.Epoch = heartbeat.ID, heartbeat.Epoch
		result.Producer, result.Kind, result.OccurredAt, result.Heartbeat = string(dto.Producer), "heartbeat", heartbeat.ObservedAt, &heartbeat
	case tenantbootstrap.Subject:
		dto, err := tenantbootstrap.Parse(message.Payload)
		if err != nil || dto.GetProducer() != governancev1.Producer {
			return invalid()
		}
		bootstrap := biz.CoreBootstrapDelivery{
			Intent:  biz.CoreBootstrapIntent{TenantID: uuid.MustParse(dto.GetTenantId()), OperationID: uuid.MustParse(dto.GetOperationId()), NormalizedEmail: dto.GetIntendedAdministrator().GetNormalizedEmail(), Locale: dto.GetIntendedAdministrator().GetLocale(), Fingerprint: dto.GetPayloadFingerprint()},
			EventID: uuid.MustParse(dto.GetEventId()), Producer: dto.GetProducer(), SourceSequence: dto.GetLifecycleSequence(), OccurredAt: dto.GetOccurredAt().AsTime(), RawPayload: append([]byte(nil), message.Payload...),
		}
		if bootstrap.Validate() != nil {
			return invalid()
		}
		result.EventID, result.Epoch, result.TenantID, result.Sequence = bootstrap.EventID, uuid.MustParse(dto.GetSourceEpoch()), bootstrap.Intent.TenantID, bootstrap.SourceSequence
		result.Producer, result.Kind, result.OccurredAt, result.Bootstrap = bootstrap.Producer, "bootstrap", bootstrap.OccurredAt, &bootstrap
	default:
		return invalid()
	}
	// Exact headers are provenance-neutral protocol checks. Current publisher
	// identity/Grant is established separately from the registered NKey route.
	for key, values := range message.Headers {
		if len(values) != 1 {
			return invalid()
		}
		switch key {
		case "Content-Type":
			if values[0] != "application/json" {
				return invalid()
			}
		case "Nats-Msg-Id":
			if values[0] != result.EventID.String() {
				return biz.TenantBrokerDelivery{}, biz.ErrTenantLifecycleConflict
			}
		case "Nats-Expected-Stream":
			if values[0] != message.Stream {
				return biz.TenantBrokerDelivery{}, biz.ErrTenantLifecycleConflict
			}
		default:
			return invalid()
		}
	}
	return result, nil
}
