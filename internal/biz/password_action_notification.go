package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	passwordActionNotificationLease       = 5 * time.Minute
	passwordActionNotificationMaxAttempts = 20
	passwordActionNotificationMaxBackoff  = 5 * time.Minute
)

var (
	ErrPasswordActionNotificationRetryable = errors.New("password-action notification submission is retryable")
	ErrPasswordActionNotificationPermanent = errors.New("password-action notification submission requires attention")
)

// PasswordActionNotificationClaim contains only durable notification metadata.
// The bearer action token is derived from these fields after the claim and is
// never persisted by the outbox.
type PasswordActionNotificationClaim struct {
	ID               uuid.UUID
	OperationID      uuid.UUID
	PrincipalID      uuid.UUID
	Purpose          PasswordActionPurpose
	DestinationEmail string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	AttemptCount     int
	Version          int64
}

// PasswordActionNotificationOutbox owns lease and compare-and-swap state. It
// deliberately knows nothing about a notification transport or its Proto.
type PasswordActionNotificationOutbox interface {
	ClaimPasswordActionNotification(context.Context, time.Time, time.Duration) (PasswordActionNotificationClaim, bool, error)
	ReschedulePasswordActionNotification(context.Context, uuid.UUID, int64, time.Time, time.Time) error
	MarkPasswordActionNotificationDelivered(context.Context, uuid.UUID, int64, string, time.Time) error
	MarkPasswordActionNotificationAttentionRequired(context.Context, uuid.UUID, int64, time.Time) error
}

// PasswordActionNotificationSubmission is the transport-neutral command for
// the Notification adapter. ActionToken exists only in memory for the duration
// of SubmitPasswordActionNotification.
type PasswordActionNotificationSubmission struct {
	RequestID        uuid.UUID
	SourceID         uuid.UUID
	SourceVersion    int64
	CorrelationID    uuid.UUID
	PrincipalID      uuid.UUID
	DestinationEmail string
	Purpose          PasswordActionPurpose
	Audience         Audience
	ActionToken      string
	OccurredAt       time.Time
	DeliverBefore    time.Time
}

type PasswordActionNotificationSubmitter interface {
	SubmitPasswordActionNotification(context.Context, PasswordActionNotificationSubmission) (string, error)
}

// PasswordActionNotificationDispatcher owns retry policy and token-at-dispatch.
// The outbox owns durable leasing while a thin adapter owns the external API.
type PasswordActionNotificationDispatcher struct {
	outbox    PasswordActionNotificationOutbox
	tokens    PasswordActionTokenCodec
	submitter PasswordActionNotificationSubmitter
	clock     Clock
}

func NewPasswordActionNotificationDispatcher(
	outbox PasswordActionNotificationOutbox,
	tokens PasswordActionTokenCodec,
	submitter PasswordActionNotificationSubmitter,
	clock Clock,
) (*PasswordActionNotificationDispatcher, error) {
	if outbox == nil || tokens == nil || submitter == nil || clock == nil {
		return nil, errors.New("password-action notification dispatcher dependencies are required")
	}
	return &PasswordActionNotificationDispatcher{
		outbox:    outbox,
		tokens:    tokens,
		submitter: submitter,
		clock:     clock,
	}, nil
}

// DispatchNext processes at most one durable outbox entry. The bool reports
// whether an entry was claimed, including entries that were rescheduled or
// moved to attention_required.
func (d *PasswordActionNotificationDispatcher) DispatchNext(ctx context.Context) (bool, error) {
	now := d.clock.Now().UTC()
	claim, found, err := d.outbox.ClaimPasswordActionNotification(ctx, now, passwordActionNotificationLease)
	if err != nil {
		return false, fmt.Errorf("claim password-action notification: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := validatePasswordActionNotificationClaim(claim, now); err != nil {
		return true, d.finishFailure(ctx, claim, now, errors.Join(ErrPasswordActionNotificationPermanent, err))
	}
	actionToken, err := d.tokens.IssuePasswordAction(ctx, PasswordActionTokenClaims{
		Issuer:      "ani-iam",
		PrincipalID: claim.PrincipalID,
		OperationID: claim.OperationID,
		Purpose:     claim.Purpose,
		IssuedAt:    claim.IssuedAt,
		ExpiresAt:   claim.ExpiresAt,
	})
	if err != nil {
		return true, d.finishFailure(ctx, claim, now, fmt.Errorf("%w: issue password-action capability: %v", ErrPasswordActionNotificationRetryable, err))
	}
	if strings.TrimSpace(actionToken) == "" {
		return true, d.finishFailure(ctx, claim, now, fmt.Errorf("%w: password-action capability is empty", ErrPasswordActionNotificationPermanent))
	}
	submission := PasswordActionNotificationSubmission{
		RequestID:        claim.ID,
		SourceID:         claim.OperationID,
		SourceVersion:    1,
		CorrelationID:    claim.OperationID,
		PrincipalID:      claim.PrincipalID,
		DestinationEmail: claim.DestinationEmail,
		Purpose:          claim.Purpose,
		Audience:         AudienceConsole,
		ActionToken:      actionToken,
		OccurredAt:       claim.IssuedAt,
		DeliverBefore:    claim.ExpiresAt,
	}
	notificationID, err := d.submitter.SubmitPasswordActionNotification(ctx, submission)
	if err != nil {
		return true, d.finishFailure(ctx, claim, now, err)
	}
	if strings.TrimSpace(notificationID) == "" {
		return true, d.finishFailure(ctx, claim, now, fmt.Errorf("%w: notification ID is empty", ErrPasswordActionNotificationPermanent))
	}
	// Submission may have consumed its deadline. Persist the receipt under an
	// independent bounded deadline so cancellation cannot strand a claimed row.
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := d.outbox.MarkPasswordActionNotificationDelivered(finish, claim.ID, claim.Version, notificationID, d.clock.Now().UTC()); err != nil {
		return true, fmt.Errorf("mark password-action notification delivered: %w", err)
	}
	return true, nil
}

func (d *PasswordActionNotificationDispatcher) finishFailure(
	ctx context.Context,
	claim PasswordActionNotificationClaim,
	now time.Time,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	now = d.clock.Now().UTC()
	terminal := errors.Is(cause, ErrPasswordActionNotificationPermanent) ||
		claim.AttemptCount >= passwordActionNotificationMaxAttempts
	availableAt := now.Add(passwordActionNotificationBackoff(claim.AttemptCount))
	if !availableAt.Before(claim.ExpiresAt) {
		terminal = true
	}
	if terminal {
		if err := d.outbox.MarkPasswordActionNotificationAttentionRequired(ctx, claim.ID, claim.Version, now); err != nil {
			return errors.Join(cause, fmt.Errorf("mark password-action notification attention required: %w", err))
		}
		return cause
	}
	if err := d.outbox.ReschedulePasswordActionNotification(ctx, claim.ID, claim.Version, availableAt, now); err != nil {
		return errors.Join(cause, fmt.Errorf("reschedule password-action notification: %w", err))
	}
	return cause
}

func passwordActionNotificationBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Second
	}
	delay := time.Second
	for current := 1; current < attempt && delay < passwordActionNotificationMaxBackoff; current++ {
		delay *= 2
	}
	if delay > passwordActionNotificationMaxBackoff {
		return passwordActionNotificationMaxBackoff
	}
	return delay
}

func validatePasswordActionNotificationClaim(claim PasswordActionNotificationClaim, now time.Time) error {
	if claim.ID == uuid.Nil || claim.ID.Version() != 7 ||
		claim.OperationID == uuid.Nil || claim.OperationID.Version() != 7 ||
		claim.PrincipalID == uuid.Nil || claim.PrincipalID.Version() != 7 ||
		claim.Version <= 1 || claim.AttemptCount <= 0 {
		return ErrInvalidPersistenceState
	}
	if claim.Purpose != PasswordActionPurposeSetup && claim.Purpose != PasswordActionPurposeReset {
		return ErrInvalidPersistenceState
	}
	if strings.TrimSpace(claim.DestinationEmail) == "" ||
		strings.ToLower(strings.TrimSpace(claim.DestinationEmail)) != claim.DestinationEmail {
		return ErrInvalidPersistenceState
	}
	if claim.IssuedAt.IsZero() || !claim.ExpiresAt.After(claim.IssuedAt) || !now.Before(claim.ExpiresAt) {
		return ErrInvalidPersistenceState
	}
	return nil
}
