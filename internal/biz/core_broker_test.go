package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type brokerConsumerFixture struct {
	ack, retry int
	message    CoreBrokerMessage
}

func (f *brokerConsumerFixture) Next(context.Context) (CoreBrokerMessage, error) {
	return f.message, nil
}
func (f *brokerConsumerFixture) Ack(context.Context, uuid.UUID) error   { f.ack++; return nil }
func (f *brokerConsumerFixture) Retry(context.Context, uuid.UUID) error { f.retry++; return nil }
func (f *brokerConsumerFixture) Check(context.Context) error            { return nil }
func (f *brokerConsumerFixture) Close() error                           { return nil }

type brokerRepositoryFixture struct {
	receiveErr, quarantineErr error
	quarantine                int
	reason                    string
}

func (f *brokerRepositoryFixture) Receive(context.Context, CoreBrokerMessage, CoreBrokerDecoded) error {
	return f.receiveErr
}
func (f *brokerRepositoryFixture) Quarantine(_ context.Context, _ CoreBrokerMessage, reason string) error {
	f.quarantine++
	f.reason = reason
	return f.quarantineErr
}
func (f *brokerRepositoryFixture) Check(context.Context) error { return nil }

type brokerDecoderFixture struct{ err error }

func (f brokerDecoderFixture) Decode(CoreBrokerMessage) (CoreBrokerDecoded, error) {
	return CoreBrokerDecoded{}, f.err
}
func TestCoreBrokerDurableACK(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		decode, receive, quarantine error
		ack, retry, count           int
		reason                      string
	}{
		{name: "durable_fact", ack: 1},
		{name: "uncertain_receive", receive: ErrPersistenceUnavailable, retry: 1},
		{name: "durable_denial", receive: ErrCoreBrokerAuthority, ack: 1, count: 1, reason: "authority_denied"},
		{name: "failed_denial_persistence", receive: ErrCoreBrokerAuthority, quarantine: ErrPersistenceUnavailable, retry: 1, count: 1, reason: "authority_denied"},
		{name: "durable_poison", decode: ErrCoreProjectionInvalid, ack: 1, count: 1, reason: "invalid_event"},
		{name: "failed_poison_persistence", decode: ErrCoreProjectionInvalid, quarantine: ErrPersistenceUnavailable, retry: 1, count: 1, reason: "invalid_event"},
		{name: "conflict_quarantine", receive: ErrCoreProjectionConflict, ack: 1, count: 1, reason: "event_conflict"},
		{name: "existing_quarantine", receive: ErrCoreBrokerQuarantined, ack: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &brokerConsumerFixture{}
			r := &brokerRepositoryFixture{receiveErr: tc.receive, quarantineErr: tc.quarantine}
			receiver, err := NewCoreBrokerReceiver(c, brokerDecoderFixture{tc.decode}, r)
			if err != nil {
				t.Fatal(err)
			}
			err = receiver.ReceiveNext(context.Background())
			if c.ack != tc.ack || c.retry != tc.retry || r.quarantine != tc.count || r.reason != tc.reason {
				t.Fatalf("ack=%d retry=%d quarantine=%d reason=%s", c.ack, c.retry, r.quarantine, r.reason)
			}
			if tc.retry > 0 && !errors.Is(err, ErrPersistenceUnavailable) {
				t.Fatalf("lost failure: %v", err)
			}
		})
	}
}
