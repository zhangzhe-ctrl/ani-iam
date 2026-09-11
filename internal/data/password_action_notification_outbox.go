package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresPasswordActionNotificationOutbox struct {
	data *Data
}

func NewPostgresPasswordActionNotificationOutbox(data *Data) biz.PasswordActionNotificationOutbox {
	return &postgresPasswordActionNotificationOutbox{data: data}
}

func (o *postgresPasswordActionNotificationOutbox) ClaimPasswordActionNotification(
	ctx context.Context,
	now time.Time,
	leaseDuration time.Duration,
) (biz.PasswordActionNotificationClaim, bool, error) {
	if now.IsZero() || leaseDuration <= 0 {
		return biz.PasswordActionNotificationClaim{}, false, biz.ErrInvalidPersistenceState
	}
	row, err := sqlcgen.New(o.data.pool).ClaimPasswordActionNotification(ctx, sqlcgen.ClaimPasswordActionNotificationParams{
		Now:                requiredTimestamptz(now),
		LeaseExpiredBefore: requiredTimestamptz(now.Add(-leaseDuration)),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PasswordActionNotificationClaim{}, false, nil
	}
	if err != nil {
		return biz.PasswordActionNotificationClaim{}, false, mapPostgresError("claim password-action notification", err, nil)
	}
	var purpose biz.PasswordActionPurpose
	switch row.Intent {
	case "password_setup":
		purpose = biz.PasswordActionPurposeSetup
	case "password_reset":
		purpose = biz.PasswordActionPurposeReset
	default:
		return biz.PasswordActionNotificationClaim{}, false, fmt.Errorf("%w: password-action notification intent", biz.ErrInvalidPersistenceState)
	}
	if row.ID == uuid.Nil || row.ID.Version() != 7 ||
		row.OperationID == uuid.Nil || row.OperationID.Version() != 7 ||
		row.PrincipalID == uuid.Nil || row.PrincipalID.Version() != 7 {
		return biz.PasswordActionNotificationClaim{}, false, fmt.Errorf("%w: password-action notification identifiers", biz.ErrInvalidPersistenceState)
	}
	email, err := o.data.outbox.open(row.DestinationKeyVersion.String, row.DestinationCiphertext, row.ID, row.OperationID, row.PrincipalID)
	if err != nil {
		return biz.PasswordActionNotificationClaim{}, false, biz.ErrPersistenceUnavailable
	}
	if strings.TrimSpace(email) == "" || strings.ToLower(strings.TrimSpace(email)) != email {
		return biz.PasswordActionNotificationClaim{}, false, fmt.Errorf("%w: password-action notification destination", biz.ErrInvalidPersistenceState)
	}
	if !row.IssuedAt.Valid || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(row.IssuedAt.Time) {
		return biz.PasswordActionNotificationClaim{}, false, fmt.Errorf("%w: password-action notification lifetime", biz.ErrInvalidPersistenceState)
	}
	if row.AttemptCount <= 0 || row.Version <= 1 {
		return biz.PasswordActionNotificationClaim{}, false, fmt.Errorf("%w: password-action notification claim version", biz.ErrInvalidPersistenceState)
	}
	return biz.PasswordActionNotificationClaim{
		ID:               row.ID,
		OperationID:      row.OperationID,
		PrincipalID:      row.PrincipalID,
		Purpose:          purpose,
		DestinationEmail: email,
		IssuedAt:         row.IssuedAt.Time.UTC(),
		ExpiresAt:        row.ExpiresAt.Time.UTC(),
		AttemptCount:     int(row.AttemptCount),
		Version:          row.Version,
	}, true, nil
}

func (o *postgresPasswordActionNotificationOutbox) ReschedulePasswordActionNotification(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	availableAt time.Time,
	updatedAt time.Time,
) error {
	if id == uuid.Nil || expectedVersion <= 0 || availableAt.IsZero() || updatedAt.IsZero() || availableAt.Before(updatedAt) {
		return biz.ErrInvalidPersistenceState
	}
	_, err := sqlcgen.New(o.data.pool).ReschedulePasswordActionNotification(ctx, sqlcgen.ReschedulePasswordActionNotificationParams{
		AvailableAt:     requiredTimestamptz(availableAt),
		UpdatedAt:       requiredTimestamptz(updatedAt),
		ID:              id,
		ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrVersionConflict
	}
	if err != nil {
		return mapPostgresError("reschedule password-action notification", err, nil)
	}
	return nil
}

func (o *postgresPasswordActionNotificationOutbox) MarkPasswordActionNotificationDelivered(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	notificationID string,
	deliveredAt time.Time,
) error {
	if id == uuid.Nil || expectedVersion <= 0 || strings.TrimSpace(notificationID) == "" || deliveredAt.IsZero() {
		return biz.ErrInvalidPersistenceState
	}
	_, err := sqlcgen.New(o.data.pool).MarkPasswordActionNotificationDelivered(ctx, sqlcgen.MarkPasswordActionNotificationDeliveredParams{
		DeliveredAt:     requiredTimestamptz(deliveredAt),
		NotificationID:  requiredPGText(notificationID),
		ID:              id,
		ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrVersionConflict
	}
	if err != nil {
		return mapPostgresError("mark password-action notification delivered", err, nil)
	}
	return nil
}

func (o *postgresPasswordActionNotificationOutbox) MarkPasswordActionNotificationAttentionRequired(
	ctx context.Context,
	id uuid.UUID,
	expectedVersion int64,
	updatedAt time.Time,
) error {
	if id == uuid.Nil || expectedVersion <= 0 || updatedAt.IsZero() {
		return biz.ErrInvalidPersistenceState
	}
	_, err := sqlcgen.New(o.data.pool).MarkPasswordActionNotificationAttentionRequired(ctx, sqlcgen.MarkPasswordActionNotificationAttentionRequiredParams{
		UpdatedAt:       requiredTimestamptz(updatedAt),
		ID:              id,
		ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrVersionConflict
	}
	if err != nil {
		return mapPostgresError("mark password-action notification attention required", err, nil)
	}
	return nil
}

var _ biz.PasswordActionNotificationOutbox = (*postgresPasswordActionNotificationOutbox)(nil)
