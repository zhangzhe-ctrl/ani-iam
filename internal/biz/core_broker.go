package biz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCoreBrokerAuthority   = errors.New("Core broker current authority is unavailable or denied")
	ErrCoreBrokerQuarantined = errors.New("Core broker message requires controlled recovery")
)

// These coordinates come from an authenticated broker connection and its
// consumer metadata, not from the event payload or arbitrary identity headers.
// The registered repository independently checks the exact fixed route.
type CoreBrokerMessage struct {
	DeliveryID, ConsumerID                         uuid.UUID
	BrokerName, Account, Stream, Consumer, Subject string
	BrokerSequence                                 int64
	DeliveryCount                                  uint64
	PublishedAt                                    time.Time
	Payload                                        []byte
	Headers                                        map[string][]string
}

// CoreBrokerDecoded retains the transport port name; its only accepted domain
// contract is the current Tenant owner delivery.
type CoreBrokerDecoded = TenantBrokerDelivery

type CoreBrokerConsumer interface {
	Next(context.Context) (CoreBrokerMessage, error)
	Ack(context.Context, uuid.UUID) error
	Retry(context.Context, uuid.UUID) error
	Check(context.Context) error
	Close() error
}
type CoreBrokerDecoder interface {
	Decode(CoreBrokerMessage) (CoreBrokerDecoded, error)
}
type CoreBrokerRepository interface {
	Receive(context.Context, CoreBrokerMessage, CoreBrokerDecoded) error
	Quarantine(context.Context, CoreBrokerMessage, string) error
	Check(context.Context) error
}

// A successful durable receive or quarantine must precede ACK. Persistence
// failure never becomes an ACK; redelivery remains with the durable consumer.
type CoreBrokerReceiver struct {
	consumer CoreBrokerConsumer
	decoder  CoreBrokerDecoder
	repo     CoreBrokerRepository
}

func NewCoreBrokerReceiver(c CoreBrokerConsumer, d CoreBrokerDecoder, r CoreBrokerRepository) (*CoreBrokerReceiver, error) {
	if c == nil || d == nil || r == nil {
		return nil, ErrCoreBrokerAuthority
	}
	return &CoreBrokerReceiver{c, d, r}, nil
}
func (r *CoreBrokerReceiver) ReceiveNext(ctx context.Context) error {
	message, err := r.consumer.Next(ctx)
	if err != nil {
		return err
	}
	decoded, err := r.decoder.Decode(message)
	reason := "invalid_event"
	if err == nil {
		err = r.repo.Receive(ctx, message, decoded)
		switch {
		case err == nil:
			return r.consumer.Ack(ctx, message.DeliveryID)
		case errors.Is(err, ErrCoreBrokerQuarantined):
			return r.consumer.Ack(ctx, message.DeliveryID)
		case errors.Is(err, ErrCoreBrokerAuthority):
			reason = "authority_denied"
		case errors.Is(err, ErrCoreProjectionConflict), errors.Is(err, ErrCoreBootstrapConflict), errors.Is(err, ErrTenantLifecycleConflict):
			reason = "event_conflict"
		case errors.Is(err, ErrCoreProjectionInvalid), errors.Is(err, ErrCoreBootstrapInvalid), errors.Is(err, ErrTenantLifecycleInvalid):
			reason = "invalid_event"
		default:
			_ = r.consumer.Retry(ctx, message.DeliveryID)
			return err
		}
	}
	if err = r.repo.Quarantine(ctx, message, reason); err != nil {
		_ = r.consumer.Retry(ctx, message.DeliveryID)
		return err
	}
	return r.consumer.Ack(ctx, message.DeliveryID)
}
func (r *CoreBrokerReceiver) Check(ctx context.Context) error {
	if err := r.consumer.Check(ctx); err != nil {
		return err
	}
	return r.repo.Check(ctx)
}
