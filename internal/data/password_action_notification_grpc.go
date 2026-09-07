package data

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
)

// PasswordActionNotificationSubmitterConfig contains only the stable
// producer-side projection required by the frozen notification.v1 contract.
// The action token is added to a copy of ConsoleActionURLBase at dispatch time.
type PasswordActionNotificationSubmitterConfig struct {
	ConsoleActionURLBase string
	Locale               string
}

type grpcPasswordActionNotificationSubmitter struct {
	client               notificationv1.NotificationServiceClient
	consoleActionURLBase url.URL
	locale               string
}

func NewGRPCPasswordActionNotificationSubmitter(
	client notificationv1.NotificationServiceClient,
	config PasswordActionNotificationSubmitterConfig,
) (biz.PasswordActionNotificationSubmitter, error) {
	if client == nil {
		return nil, errors.New("Notification client is required")
	}
	actionURL, err := url.Parse(config.ConsoleActionURLBase)
	if err != nil || actionURL.Scheme != "https" || actionURL.Host == "" || !actionURL.IsAbs() ||
		actionURL.User != nil || actionURL.RawQuery != "" || actionURL.Fragment != "" ||
		strings.ToLower(actionURL.Host) != actionURL.Host {
		return nil, errors.New("console password-action URL must be a canonical HTTPS base without userinfo, query, or fragment")
	}
	if config.Locale != "en-US" && config.Locale != "zh-CN" {
		return nil, errors.New("password-action notification locale must be en-US or zh-CN")
	}
	return &grpcPasswordActionNotificationSubmitter{
		client:               client,
		consoleActionURLBase: *actionURL,
		locale:               config.Locale,
	}, nil
}

func (s *grpcPasswordActionNotificationSubmitter) SubmitPasswordActionNotification(
	ctx context.Context,
	submission biz.PasswordActionNotificationSubmission,
) (string, error) {
	if err := validatePasswordActionNotificationSubmission(submission); err != nil {
		return "", errors.Join(biz.ErrPasswordActionNotificationPermanent, err)
	}
	purpose, ok := passwordActionNotificationPurpose(submission.Purpose)
	if !ok {
		return "", errors.Join(biz.ErrPasswordActionNotificationPermanent, errors.New("password-action purpose is unsupported"))
	}
	audience, ok := passwordActionNotificationAudience(submission.Audience)
	if !ok {
		return "", errors.Join(biz.ErrPasswordActionNotificationPermanent, errors.New("password-action audience is unsupported"))
	}
	actionURL := s.consoleActionURLBase
	query := actionURL.Query()
	query.Set("token", submission.ActionToken)
	actionURL.RawQuery = query.Encode()
	request := &notificationv1.SubmitNotificationRequest{
		RequestId: submission.RequestID.String(),
		Source: &notificationv1.SourceReference{
			ResourceId: submission.SourceID.String(), ResourceVersion: uint64(submission.SourceVersion),
		},
		CorrelationId: submission.CorrelationID.String(),
		OccurredAt:    timestamppb.New(submission.OccurredAt),
		DeliverBefore: timestamppb.New(submission.DeliverBefore),
		Locale:        s.locale,
		Scope: &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_HumanPrincipal{
			HumanPrincipal: &notificationv1.HumanPrincipalScope{PrincipalId: submission.PrincipalID.String()},
		}},
		Recipient: &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_HumanPrincipal{
			HumanPrincipal: &notificationv1.HumanPrincipalRecipient{PrincipalId: submission.PrincipalID.String()},
		}},
		Destination: &notificationv1.DestinationSnapshot{Destination: &notificationv1.DestinationSnapshot_Email{
			Email: &notificationv1.EmailDestinationSnapshot{NormalizedEmail: submission.DestinationEmail},
		}},
		Notification: &notificationv1.SubmitNotificationRequest_IamPasswordAction{IamPasswordAction: &notificationv1.IamPasswordAction{
			ActionUrl:       actionURL.String(),
			Purpose:         purpose,
			Audience:        audience,
			ActionExpiresAt: timestamppb.New(submission.DeliverBefore),
		}},
	}
	response, err := s.client.SubmitNotification(ctx, request)
	if err != nil {
		return "", classifyPasswordActionNotificationError(err)
	}
	if response == nil || response.Receipt == nil || response.Receipt.RequestId != submission.RequestID.String() ||
		!canonicalNotificationID(response.Receipt.NotificationId) || response.Receipt.StoredAt == nil ||
		response.Receipt.StoredAt.CheckValid() != nil {
		return "", errors.Join(biz.ErrPasswordActionNotificationRetryable, errors.New("Notification returned an invalid durable receipt"))
	}
	return response.Receipt.NotificationId, nil
}

func validatePasswordActionNotificationSubmission(submission biz.PasswordActionNotificationSubmission) error {
	if !validNotificationUUIDv7(submission.RequestID) || !validNotificationUUIDv7(submission.SourceID) ||
		!validNotificationUUIDv7(submission.CorrelationID) || !validNotificationUUIDv7(submission.PrincipalID) ||
		submission.SourceID != submission.CorrelationID || submission.SourceVersion <= 0 {
		return errors.New("password-action Notification identities are invalid")
	}
	if strings.TrimSpace(submission.DestinationEmail) == "" ||
		strings.TrimSpace(submission.DestinationEmail) != submission.DestinationEmail ||
		strings.ToLower(submission.DestinationEmail) != submission.DestinationEmail {
		return errors.New("password-action Notification destination is not normalized")
	}
	if strings.TrimSpace(submission.ActionToken) == "" || strings.TrimSpace(submission.ActionToken) != submission.ActionToken {
		return errors.New("password-action capability is invalid")
	}
	if submission.OccurredAt.IsZero() || submission.DeliverBefore.IsZero() ||
		!submission.DeliverBefore.After(submission.OccurredAt) ||
		timestamppb.New(submission.OccurredAt).CheckValid() != nil ||
		timestamppb.New(submission.DeliverBefore).CheckValid() != nil {
		return errors.New("password-action Notification lifetime is invalid")
	}
	return nil
}

func passwordActionNotificationPurpose(value biz.PasswordActionPurpose) (notificationv1.IamPasswordActionPurpose, bool) {
	switch value {
	case biz.PasswordActionPurposeSetup:
		return notificationv1.IamPasswordActionPurpose_IAM_PASSWORD_ACTION_PURPOSE_SETUP, true
	case biz.PasswordActionPurposeReset:
		return notificationv1.IamPasswordActionPurpose_IAM_PASSWORD_ACTION_PURPOSE_RESET, true
	default:
		return notificationv1.IamPasswordActionPurpose_IAM_PASSWORD_ACTION_PURPOSE_UNSPECIFIED, false
	}
}

func passwordActionNotificationAudience(value biz.Audience) (notificationv1.IamPasswordActionAudience, bool) {
	if value == biz.AudienceConsole {
		return notificationv1.IamPasswordActionAudience_IAM_PASSWORD_ACTION_AUDIENCE_CONSOLE, true
	}
	return notificationv1.IamPasswordActionAudience_IAM_PASSWORD_ACTION_AUDIENCE_UNSPECIFIED, false
}

func classifyPasswordActionNotificationError(err error) error {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		return errors.Join(biz.ErrPasswordActionNotificationRetryable, errors.New("Notification transport failed before a durable receipt"))
	}
	reason := notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_UNSPECIFIED
	for _, detail := range grpcStatus.Details() {
		if typed, ok := detail.(*notificationv1.NotificationErrorDetail); ok {
			reason = typed.Reason
			break
		}
	}
	switch reason {
	case notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_STORAGE_UNAVAILABLE,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_INTAKE_KEY_UNAVAILABLE:
		return fmt.Errorf("%w: Notification code=%s reason=%s", biz.ErrPasswordActionNotificationRetryable, grpcStatus.Code(), reason)
	case notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_WORKLOAD_IDENTITY_MISSING,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_PRODUCER_TYPE_FORBIDDEN,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_REQUEST_INVALID,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_ACTION_URL_NOT_ALLOWED,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_IDEMPOTENCY_CONFLICT,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_TEMPLATE_UNAVAILABLE,
		notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_NOTIFICATION_NOT_FOUND:
		return fmt.Errorf("%w: Notification code=%s reason=%s", biz.ErrPasswordActionNotificationPermanent, grpcStatus.Code(), reason)
	}
	switch grpcStatus.Code() {
	case codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument,
		codes.AlreadyExists, codes.FailedPrecondition, codes.NotFound:
		return fmt.Errorf("%w: Notification code=%s without a recognized reason", biz.ErrPasswordActionNotificationPermanent, grpcStatus.Code())
	default:
		return fmt.Errorf("%w: Notification code=%s without a recognized reason", biz.ErrPasswordActionNotificationRetryable, grpcStatus.Code())
	}
}

func canonicalNotificationID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func validNotificationUUIDv7(value uuid.UUID) bool {
	return value != uuid.Nil && value.Version() == 7
}

var _ biz.PasswordActionNotificationSubmitter = (*grpcPasswordActionNotificationSubmitter)(nil)
