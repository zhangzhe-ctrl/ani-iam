package biz

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type IdentityNotificationKind string

const (
	IdentityNotificationTenantInvitation   IdentityNotificationKind = "tenant_invitation"
	IdentityNotificationPlatformInvitation IdentityNotificationKind = "platform_invitation"
	IdentityNotificationEmailVerification  IdentityNotificationKind = "email_verification"
	identityNotificationLease                                       = 5 * time.Minute
	identityNotificationMaxAttempts                                 = 20
)

var (
	ErrIdentityNotificationRetryable  = errors.New("identity notification submission is retryable")
	ErrIdentityNotificationPermanent  = errors.New("identity notification requires attention")
	ErrIdentityNotificationSuperseded = errors.New("identity notification claim is no longer current")
)

// Secret is only for the in-memory submitter. Never log or serialize this value
// into a receipt, Audit, diagnostic or user-visible management response.
type IdentityNotificationClaim struct {
	Kind                   IdentityNotificationKind
	TenantID, ID, SourceID uuid.UUID
	Generation, Version    int64
	AttemptCount           int
	OccurredAt, ExpiresAt  time.Time
	Email, Secret, Locale  string
	PayloadValid           bool
}
type IdentityNotificationOutcome string

const (
	IdentityNotificationDelivered IdentityNotificationOutcome = "delivered"
	IdentityNotificationRetry     IdentityNotificationOutcome = "pending"
	IdentityNotificationCancelled IdentityNotificationOutcome = "cancelled"
	IdentityNotificationAttention IdentityNotificationOutcome = "attention_required"
)

type IdentityNotificationOutbox interface {
	ClaimIdentityNotification(context.Context, IdentityNotificationKind, time.Time, time.Duration) (IdentityNotificationClaim, bool, error)
	CurrentIdentityNotification(context.Context, IdentityNotificationClaim, time.Time) (bool, error)
	FinishIdentityNotification(context.Context, IdentityNotificationClaim, IdentityNotificationOutcome, string, time.Time, time.Time) error
}
type IdentityNotificationSubmission struct {
	Kind                          IdentityNotificationKind
	TenantID, RequestID, SourceID uuid.UUID
	SourceVersion                 int64
	Email, Secret, Locale         string
	OccurredAt, DeliverBefore     time.Time
}
type IdentityNotificationSubmitter interface {
	SubmitIdentityNotification(context.Context, IdentityNotificationSubmission) (string, error)
}
type IdentityNotificationDispatcher struct {
	outbox    IdentityNotificationOutbox
	submitter IdentityNotificationSubmitter
	clock     Clock
}

func NewIdentityNotificationDispatcher(outbox IdentityNotificationOutbox, submitter IdentityNotificationSubmitter, clock Clock) (*IdentityNotificationDispatcher, error) {
	if outbox == nil || submitter == nil || clock == nil {
		return nil, ErrAuthenticationDependency
	}
	return &IdentityNotificationDispatcher{outbox: outbox, submitter: submitter, clock: clock}, nil
}
func validIdentityNotificationKind(kind IdentityNotificationKind) bool {
	return kind == IdentityNotificationTenantInvitation || kind == IdentityNotificationPlatformInvitation || kind == IdentityNotificationEmailVerification
}
func validateIdentityNotificationClaim(c IdentityNotificationClaim) error {
	if !validIdentityNotificationKind(c.Kind) || c.ID.Version() != 7 || c.SourceID.Version() != 7 || c.Version < 2 || c.Generation < 1 || c.AttemptCount < 1 || c.OccurredAt.IsZero() || !c.ExpiresAt.After(c.OccurredAt) {
		return ErrIdentityNotificationPermanent
	}
	if (c.Kind == IdentityNotificationTenantInvitation && c.TenantID.Version() != 7) || (c.Kind != IdentityNotificationTenantInvitation && c.TenantID != uuid.Nil) {
		return ErrIdentityNotificationPermanent
	}
	return nil
}
func validateIdentityNotificationPayload(c IdentityNotificationClaim) error {
	email, err := normalizeInvitationEmail(c.Email)
	if !c.PayloadValid || err != nil || email != c.Email || (c.Locale != "en-US" && c.Locale != "zh-CN") {
		return ErrIdentityNotificationPermanent
	}
	if c.Kind == IdentityNotificationEmailVerification {
		if !validEmailVerificationCode(c.Secret) || c.Generation != 1 {
			return ErrIdentityNotificationPermanent
		}
		return nil
	}
	target := InvitationAcceptanceTarget{Boundary: AccessBoundaryPlatform, InvitationID: c.SourceID}
	if c.Kind == IdentityNotificationTenantInvitation {
		target.Boundary = AccessBoundaryTenant
		target.TenantID = c.TenantID
	}
	if _, err = invitationAcceptanceDigest(target, c.Secret); err != nil {
		return ErrIdentityNotificationPermanent
	}
	return nil
}
func (d *IdentityNotificationDispatcher) finish(ctx context.Context, c IdentityNotificationClaim, outcome IdentityNotificationOutcome, receipt string, next time.Time) error {
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := d.outbox.FinishIdentityNotification(finish, c, outcome, receipt, next, d.clock.Now().UTC())
	return err
}
func (d *IdentityNotificationDispatcher) failure(ctx context.Context, c IdentityNotificationClaim, cause error) error {
	now := d.clock.Now().UTC()
	outcome := IdentityNotificationRetry
	next := now
	if errors.Is(cause, ErrIdentityNotificationPermanent) || c.AttemptCount >= identityNotificationMaxAttempts {
		outcome = IdentityNotificationAttention
	} else {
		delay := time.Second << min(c.AttemptCount-1, 9)
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		next = now.Add(delay)
		if !next.Before(c.ExpiresAt) {
			outcome = IdentityNotificationCancelled
			next = now
		}
	}
	if err := d.finish(ctx, c, outcome, "", next); err != nil {
		return err
	}
	// Only a stable classification escapes; transport text can contain a URL or
	// response detail. The worker records metadata, never the submitted payload.
	if outcome == IdentityNotificationAttention {
		return ErrIdentityNotificationPermanent
	}
	if outcome == IdentityNotificationCancelled {
		return nil
	}
	return ErrIdentityNotificationRetryable
}
func (d *IdentityNotificationDispatcher) DispatchNext(ctx context.Context, kind IdentityNotificationKind) (worked bool, err error) {
	defer func() {
		if errors.Is(err, ErrIdentityNotificationSuperseded) {
			err = nil
		}
	}()
	if d == nil || d.outbox == nil || d.submitter == nil || d.clock == nil || !validIdentityNotificationKind(kind) {
		return false, ErrAuthenticationDependency
	}
	now := d.clock.Now().UTC()
	c, found, err := d.outbox.ClaimIdentityNotification(ctx, kind, now, identityNotificationLease)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if c.Kind != kind || validateIdentityNotificationClaim(c) != nil {
		return true, d.failure(ctx, c, ErrIdentityNotificationPermanent)
	}
	current, err := d.outbox.CurrentIdentityNotification(ctx, c, now)
	if err != nil {
		return true, err
	}
	if !current || !now.Before(c.ExpiresAt) {
		return true, d.finish(ctx, c, IdentityNotificationCancelled, "", now)
	}
	if c.AttemptCount > identityNotificationMaxAttempts || validateIdentityNotificationPayload(c) != nil {
		return true, d.failure(ctx, c, ErrIdentityNotificationPermanent)
	}
	receipt, err := d.submitter.SubmitIdentityNotification(ctx, IdentityNotificationSubmission{Kind: c.Kind, TenantID: c.TenantID, RequestID: c.ID, SourceID: c.SourceID, SourceVersion: c.Generation, Email: c.Email, Secret: c.Secret, Locale: c.Locale, OccurredAt: c.OccurredAt, DeliverBefore: c.ExpiresAt})
	if err != nil {
		return true, d.failure(ctx, c, err)
	}
	id, err := uuid.Parse(receipt)
	// Notification owns this external receipt UUID. IAM's UUIDv7 rule applies
	// to its request/source IDs, not to the producer-independent receipt.
	if err != nil || id == uuid.Nil || id.String() != receipt || strings.TrimSpace(receipt) != receipt {
		return true, d.failure(ctx, c, ErrIdentityNotificationRetryable)
	}
	if err = d.finish(ctx, c, IdentityNotificationDelivered, receipt, now); err != nil {
		return true, fmt.Errorf("persist identity notification receipt: %w", err)
	}
	return true, nil
}
