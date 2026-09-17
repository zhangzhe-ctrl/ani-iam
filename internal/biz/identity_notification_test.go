package biz

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"reflect"
	"strings"
	"testing"
	"time"
)

type identityNotificationTestOutbox struct {
	claims            []IdentityNotificationClaim
	current           bool
	outcome           IdentityNotificationOutcome
	receipt           string
	next              time.Time
	finishErr         error
	finishContextLive bool
}

func (o *identityNotificationTestOutbox) ClaimIdentityNotification(_ context.Context, _ IdentityNotificationKind, _ time.Time, lease time.Duration) (IdentityNotificationClaim, bool, error) {
	if lease != 5*time.Minute {
		return IdentityNotificationClaim{}, false, errors.New("lease mismatch")
	}
	if len(o.claims) == 0 {
		return IdentityNotificationClaim{}, false, nil
	}
	c := o.claims[0]
	o.claims = o.claims[1:]
	return c, true, nil
}
func (o *identityNotificationTestOutbox) CurrentIdentityNotification(context.Context, IdentityNotificationClaim, time.Time) (bool, error) {
	return o.current, nil
}
func (o *identityNotificationTestOutbox) FinishIdentityNotification(ctx context.Context, _ IdentityNotificationClaim, outcome IdentityNotificationOutcome, receipt string, next, _ time.Time) error {
	o.outcome, o.receipt, o.next = outcome, receipt, next
	deadline, ok := ctx.Deadline()
	o.finishContextLive = ctx.Err() == nil && ok && time.Until(deadline) <= time.Second
	return o.finishErr
}

type identityNotificationTestSubmitter struct {
	send func(context.Context, IdentityNotificationSubmission) (string, error)
}

func (s identityNotificationTestSubmitter) SubmitIdentityNotification(ctx context.Context, v IdentityNotificationSubmission) (string, error) {
	return s.send(ctx, v)
}
func identityNotificationUnitClaim(now time.Time, kind IdentityNotificationKind) IdentityNotificationClaim {
	c := IdentityNotificationClaim{Kind: kind, ID: uuid.Must(uuid.NewV7()), SourceID: uuid.Must(uuid.NewV7()), Generation: 1, Version: 2, AttemptCount: 1, OccurredAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute), Email: "invite@example.test", Locale: "en-US", PayloadValid: true}
	switch kind {
	case IdentityNotificationTenantInvitation:
		c.TenantID = uuid.Must(uuid.NewV7())
		c.Secret = "ani_inv_t." + c.TenantID.String() + "." + c.SourceID.String() + "." + strings.Repeat("a", 43)
	case IdentityNotificationPlatformInvitation:
		c.Secret = "ani_inv_p." + c.SourceID.String() + "." + strings.Repeat("b", 43)
	case IdentityNotificationEmailVerification:
		c.Secret = "012345"
	}
	return c
}
func TestIdentityNotificationDispatcherStableRetryAndIndependentReceipt(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []IdentityNotificationKind{IdentityNotificationTenantInvitation, IdentityNotificationPlatformInvitation, IdentityNotificationEmailVerification} {
		t.Run(string(kind), func(t *testing.T) {
			c := identityNotificationUnitClaim(now, kind)
			second := c
			second.Version = 4
			second.AttemptCount = 2
			o := &identityNotificationTestOutbox{claims: []IdentityNotificationClaim{c, second}, current: true}
			clock := &mutableNotificationClock{now: now}
			var submissions []IdentityNotificationSubmission
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			receipt := uuid.NewString() // the fixed Notification owner emits UUIDv4
			s := identityNotificationTestSubmitter{send: func(_ context.Context, v IdentityNotificationSubmission) (string, error) {
				submissions = append(submissions, v)
				if len(submissions) == 1 {
					return "", errors.New("private transport detail must not escape")
				}
				cancel()
				return receipt, nil
			}}
			d, _ := NewIdentityNotificationDispatcher(o, s, clock)
			if worked, err := d.DispatchNext(ctx, kind); !worked || err != ErrIdentityNotificationRetryable || o.outcome != IdentityNotificationRetry || !o.next.Equal(now.Add(time.Second)) {
				t.Fatal("retry classification or schedule")
			}
			clock.now = now.Add(time.Second)
			if worked, err := d.DispatchNext(ctx, kind); !worked || err != nil || o.outcome != IdentityNotificationDelivered || o.receipt != receipt || !o.finishContextLive {
				t.Fatal("durable receipt after request cancellation")
			}
			if len(submissions) != 2 || !reflect.DeepEqual(submissions[0], submissions[1]) {
				t.Fatal("retry changed protected request identity or intent")
			}
		})
	}
}
func TestIdentityNotificationDispatcherTerminalAndCurrentGuards(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		mutate  func(*IdentityNotificationClaim)
		current bool
		sendErr error
		receipt string
		outcome IdentityNotificationOutcome
		want    error
		sends   int
	}{
		{name: "cancelled parent", current: false, outcome: IdentityNotificationCancelled},
		{name: "expired", current: true, mutate: func(c *IdentityNotificationClaim) { c.ExpiresAt = now }, outcome: IdentityNotificationCancelled},
		{name: "wrong purpose token", current: true, mutate: func(c *IdentityNotificationClaim) { c.Secret = "000000" }, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent},
		{name: "ciphertext rejected", current: true, mutate: func(c *IdentityNotificationClaim) { c.PayloadValid = false }, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent},
		{name: "invalid boundary", current: true, mutate: func(c *IdentityNotificationClaim) { c.TenantID = uuid.Nil }, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent},
		{name: "retry budget exhausted", current: true, mutate: func(c *IdentityNotificationClaim) { c.AttemptCount = 20 }, sendErr: ErrIdentityNotificationRetryable, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent, sends: 1},
		{name: "exhausted claim", current: true, mutate: func(c *IdentityNotificationClaim) { c.AttemptCount = 21 }, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent},
		{name: "permanent", current: true, sendErr: ErrIdentityNotificationPermanent, outcome: IdentityNotificationAttention, want: ErrIdentityNotificationPermanent, sends: 1},
		{name: "no useful retry lifetime", current: true, mutate: func(c *IdentityNotificationClaim) { c.ExpiresAt = now.Add(time.Millisecond) }, sendErr: ErrIdentityNotificationRetryable, outcome: IdentityNotificationCancelled, sends: 1},
		{name: "invalid receipt", current: true, receipt: "bad-receipt", outcome: IdentityNotificationRetry, want: ErrIdentityNotificationRetryable, sends: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := identityNotificationUnitClaim(now, IdentityNotificationTenantInvitation)
			if tc.mutate != nil {
				tc.mutate(&c)
			}
			o := &identityNotificationTestOutbox{claims: []IdentityNotificationClaim{c}, current: tc.current}
			sent := 0
			d, _ := NewIdentityNotificationDispatcher(o, identityNotificationTestSubmitter{send: func(context.Context, IdentityNotificationSubmission) (string, error) {
				sent++
				return tc.receipt, tc.sendErr
			}}, fixedNotificationClock{now: now})
			worked, err := d.DispatchNext(context.Background(), c.Kind)
			if !worked || !errors.Is(err, tc.want) || o.outcome != tc.outcome || sent != tc.sends {
				t.Fatal("dispatch terminal/current classification")
			}
		})
	}
}
func TestIdentityNotificationDispatcherSupersededReceiptAndCappedRetry(t *testing.T) {
	now := time.Now().UTC()
	c := identityNotificationUnitClaim(now, IdentityNotificationEmailVerification)
	c.AttemptCount = 12
	o := &identityNotificationTestOutbox{claims: []IdentityNotificationClaim{c}, current: true}
	s := identityNotificationTestSubmitter{send: func(context.Context, IdentityNotificationSubmission) (string, error) {
		return "", ErrIdentityNotificationRetryable
	}}
	d, _ := NewIdentityNotificationDispatcher(o, s, fixedNotificationClock{now: now})
	if _, err := d.DispatchNext(context.Background(), c.Kind); err != ErrIdentityNotificationRetryable || !o.next.Equal(now.Add(5*time.Minute)) {
		t.Fatal("retry cap")
	}
	o.claims = []IdentityNotificationClaim{c}
	o.finishErr = ErrIdentityNotificationSuperseded
	d, _ = NewIdentityNotificationDispatcher(o, identityNotificationTestSubmitter{send: func(context.Context, IdentityNotificationSubmission) (string, error) {
		return uuid.Must(uuid.NewV7()).String(), nil
	}}, fixedNotificationClock{now: now})
	if worked, err := d.DispatchNext(context.Background(), c.Kind); !worked || err != nil {
		t.Fatal("supersession is benign without overriding current row")
	}
}
