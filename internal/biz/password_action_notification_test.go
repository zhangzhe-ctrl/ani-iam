package biz

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPasswordActionNotificationDispatcherSubmitsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC)
	claim := passwordActionNotificationTestClaim(now, 1, 2)
	outbox := &passwordActionNotificationTestOutbox{claims: []PasswordActionNotificationClaim{claim}}
	tokens := &passwordActionNotificationTestTokens{token: "test-only-action-capability"}
	submitter := &passwordActionNotificationTestSubmitter{notificationID: "submission-1"}
	dispatcher, err := NewPasswordActionNotificationDispatcher(outbox, tokens, submitter, fixedNotificationClock{now: now})
	if err != nil {
		t.Fatalf("NewPasswordActionNotificationDispatcher() error = %v", err)
	}

	processed, err := dispatcher.DispatchNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("DispatchNext() = %t, %v", processed, err)
	}
	wantClaims := PasswordActionTokenClaims{
		Issuer:      "ani-iam",
		PrincipalID: claim.PrincipalID,
		OperationID: claim.OperationID,
		Purpose:     claim.Purpose,
		IssuedAt:    claim.IssuedAt,
		ExpiresAt:   claim.ExpiresAt,
	}
	if tokens.claims != wantClaims {
		t.Fatalf("issued claims = %#v, want %#v", tokens.claims, wantClaims)
	}
	wantSubmission := PasswordActionNotificationSubmission{
		RequestID:        claim.ID,
		SourceID:         claim.OperationID,
		SourceVersion:    1,
		CorrelationID:    claim.OperationID,
		PrincipalID:      claim.PrincipalID,
		DestinationEmail: claim.DestinationEmail,
		Purpose:          claim.Purpose,
		Audience:         AudienceConsole,
		ActionToken:      "test-only-action-capability",
		OccurredAt:       claim.IssuedAt,
		DeliverBefore:    claim.ExpiresAt,
	}
	if !reflect.DeepEqual(submitter.submissions, []PasswordActionNotificationSubmission{wantSubmission}) {
		t.Fatalf("submissions = %#v, want %#v", submitter.submissions, wantSubmission)
	}
	if outbox.deliveredID != claim.ID || outbox.deliveredVersion != claim.Version || outbox.notificationID != "submission-1" || !outbox.deliveredAt.Equal(now) {
		t.Fatalf("delivery mark = %#v", outbox)
	}
}

func TestPasswordActionNotificationDispatcherRetriesIdenticalSubmissionAfterAmbiguousFailure(t *testing.T) {
	now := time.Date(2026, 9, 7, 5, 15, 0, 0, time.UTC)
	first := passwordActionNotificationTestClaim(now, 1, 2)
	second := first
	second.AttemptCount = 2
	second.Version = 4
	outbox := &passwordActionNotificationTestOutbox{claims: []PasswordActionNotificationClaim{first, second}}
	tokens := &passwordActionNotificationTestTokens{token: "test-only-stable-capability"}
	submitter := &passwordActionNotificationTestSubmitter{
		errors:         []error{ErrPasswordActionNotificationRetryable, nil},
		notificationID: "submission-after-retry",
	}
	clock := &mutableNotificationClock{now: now}
	dispatcher, err := NewPasswordActionNotificationDispatcher(outbox, tokens, submitter, clock)
	if err != nil {
		t.Fatalf("NewPasswordActionNotificationDispatcher() error = %v", err)
	}

	if processed, err := dispatcher.DispatchNext(context.Background()); !processed || !errors.Is(err, ErrPasswordActionNotificationRetryable) {
		t.Fatalf("first DispatchNext() = %t, %v", processed, err)
	}
	if outbox.rescheduledID != first.ID || outbox.rescheduledVersion != first.Version || !outbox.availableAt.Equal(now.Add(time.Second)) {
		t.Fatalf("retry schedule = %#v", outbox)
	}
	clock.now = now.Add(time.Second)
	if processed, err := dispatcher.DispatchNext(context.Background()); err != nil || !processed {
		t.Fatalf("retry DispatchNext() = %t, %v", processed, err)
	}
	if len(submitter.submissions) != 2 || !reflect.DeepEqual(submitter.submissions[0], submitter.submissions[1]) {
		t.Fatalf("retry submissions changed = %#v", submitter.submissions)
	}
}

func TestPasswordActionNotificationDispatcherTerminatesPermanentOrExhaustedFailure(t *testing.T) {
	now := time.Date(2026, 9, 7, 5, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		attempts int
		sendErr  error
	}{
		{name: "permanent contract rejection", attempts: 1, sendErr: ErrPasswordActionNotificationPermanent},
		{name: "retry budget exhausted", attempts: 20, sendErr: ErrPasswordActionNotificationRetryable},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := passwordActionNotificationTestClaim(now, test.attempts, 21)
			outbox := &passwordActionNotificationTestOutbox{claims: []PasswordActionNotificationClaim{claim}}
			dispatcher, err := NewPasswordActionNotificationDispatcher(
				outbox,
				&passwordActionNotificationTestTokens{token: "test-only-action-capability"},
				&passwordActionNotificationTestSubmitter{errors: []error{test.sendErr}},
				fixedNotificationClock{now: now},
			)
			if err != nil {
				t.Fatalf("NewPasswordActionNotificationDispatcher() error = %v", err)
			}
			if processed, err := dispatcher.DispatchNext(context.Background()); !processed || !errors.Is(err, test.sendErr) {
				t.Fatalf("DispatchNext() = %t, %v", processed, err)
			}
			if outbox.attentionID != claim.ID || outbox.attentionVersion != claim.Version || !outbox.attentionAt.Equal(now) {
				t.Fatalf("attention mark = %#v", outbox)
			}
			if outbox.rescheduledID != uuid.Nil {
				t.Fatalf("terminal failure was rescheduled: %#v", outbox)
			}
		})
	}
}

func TestPasswordActionNotificationDispatcherRejectsNonV7DurableIdentityBeforeSubmission(t *testing.T) {
	now := time.Date(2026, 9, 7, 5, 45, 0, 0, time.UTC)
	claim := passwordActionNotificationTestClaim(now, 1, 2)
	claim.OperationID = uuid.MustParse("d57a4e66-746d-4ca7-bb07-8d809f0cf5cb")
	outbox := &passwordActionNotificationTestOutbox{claims: []PasswordActionNotificationClaim{claim}}
	submitter := &passwordActionNotificationTestSubmitter{notificationID: "must-not-submit"}
	dispatcher, err := NewPasswordActionNotificationDispatcher(
		outbox,
		&passwordActionNotificationTestTokens{token: "must-not-issue"},
		submitter,
		fixedNotificationClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewPasswordActionNotificationDispatcher() error = %v", err)
	}
	processed, err := dispatcher.DispatchNext(context.Background())
	if !processed || !errors.Is(err, ErrPasswordActionNotificationPermanent) {
		t.Fatalf("DispatchNext(non-v7 identity) = %t, %v", processed, err)
	}
	if len(submitter.submissions) != 0 {
		t.Fatalf("invalid durable identity reached submitter: %#v", submitter.submissions)
	}
	if outbox.attentionID != claim.ID || outbox.attentionVersion != claim.Version {
		t.Fatalf("invalid durable identity attention mark = %#v", outbox)
	}
}

func passwordActionNotificationTestClaim(now time.Time, attempts int, version int64) PasswordActionNotificationClaim {
	return PasswordActionNotificationClaim{
		ID:               uuid.MustParse("0198f062-b76d-7001-9000-000000000301"),
		OperationID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000302"),
		PrincipalID:      uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01303"),
		Purpose:          PasswordActionPurposeReset,
		DestinationEmail: "dispatch@example.com",
		IssuedAt:         now.Add(-time.Minute),
		ExpiresAt:        now.Add(29 * time.Minute),
		AttemptCount:     attempts,
		Version:          version,
	}
}

type passwordActionNotificationTestOutbox struct {
	claims             []PasswordActionNotificationClaim
	deliveredID        uuid.UUID
	deliveredVersion   int64
	notificationID     string
	deliveredAt        time.Time
	rescheduledID      uuid.UUID
	rescheduledVersion int64
	availableAt        time.Time
	attentionID        uuid.UUID
	attentionVersion   int64
	attentionAt        time.Time
}

func (o *passwordActionNotificationTestOutbox) ClaimPasswordActionNotification(context.Context, time.Time, time.Duration) (PasswordActionNotificationClaim, bool, error) {
	if len(o.claims) == 0 {
		return PasswordActionNotificationClaim{}, false, nil
	}
	claim := o.claims[0]
	o.claims = o.claims[1:]
	return claim, true, nil
}

func (o *passwordActionNotificationTestOutbox) ReschedulePasswordActionNotification(_ context.Context, id uuid.UUID, version int64, availableAt, _ time.Time) error {
	o.rescheduledID, o.rescheduledVersion, o.availableAt = id, version, availableAt
	return nil
}

func (o *passwordActionNotificationTestOutbox) MarkPasswordActionNotificationDelivered(_ context.Context, id uuid.UUID, version int64, notificationID string, deliveredAt time.Time) error {
	o.deliveredID, o.deliveredVersion, o.notificationID, o.deliveredAt = id, version, notificationID, deliveredAt
	return nil
}

func (o *passwordActionNotificationTestOutbox) MarkPasswordActionNotificationAttentionRequired(_ context.Context, id uuid.UUID, version int64, attentionAt time.Time) error {
	o.attentionID, o.attentionVersion, o.attentionAt = id, version, attentionAt
	return nil
}

type passwordActionNotificationTestTokens struct {
	claims PasswordActionTokenClaims
	token  string
	err    error
}

func (t *passwordActionNotificationTestTokens) IssuePasswordAction(_ context.Context, claims PasswordActionTokenClaims) (string, error) {
	t.claims = claims
	return t.token, t.err
}

func (*passwordActionNotificationTestTokens) VerifyPasswordAction(context.Context, string) (PasswordActionTokenClaims, error) {
	return PasswordActionTokenClaims{}, errors.New("not used")
}

type passwordActionNotificationTestSubmitter struct {
	submissions    []PasswordActionNotificationSubmission
	errors         []error
	notificationID string
}

func (s *passwordActionNotificationTestSubmitter) SubmitPasswordActionNotification(_ context.Context, submission PasswordActionNotificationSubmission) (string, error) {
	s.submissions = append(s.submissions, submission)
	if len(s.errors) > 0 {
		err := s.errors[0]
		s.errors = s.errors[1:]
		if err != nil {
			return "", err
		}
	}
	return s.notificationID, nil
}

type fixedNotificationClock struct{ now time.Time }

func (c fixedNotificationClock) Now() time.Time { return c.now }

type mutableNotificationClock struct{ now time.Time }

func (c *mutableNotificationClock) Now() time.Time { return c.now }
