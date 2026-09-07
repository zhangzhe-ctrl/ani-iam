package data

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
)

func TestGRPCPasswordActionNotificationSubmitterMapsFrozenContract(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	requestID := uuid.MustParse("0198f062-b76d-7001-9000-000000000101")
	operationID := uuid.MustParse("0198f062-b76d-7001-9000-000000000102")
	principalID := uuid.MustParse("0198f062-b76d-7001-9000-000000000103")
	notificationID := "0198f062-b76d-7001-9000-000000000104"
	client := &recordingNotificationServiceClient{response: &notificationv1.SubmitNotificationResponse{
		Receipt: &notificationv1.NotificationReceipt{
			NotificationId: notificationID,
			RequestId:      requestID.String(),
			StoredAt:       timestamppb.New(now.Add(time.Second)),
		},
	}}
	submitter, err := NewGRPCPasswordActionNotificationSubmitter(client, PasswordActionNotificationSubmitterConfig{
		ConsoleActionURLBase: "https://console.example.test/password-action",
		Locale:               "en-US",
	})
	if err != nil {
		t.Fatalf("NewGRPCPasswordActionNotificationSubmitter() error = %v", err)
	}

	got, err := submitter.SubmitPasswordActionNotification(context.Background(), biz.PasswordActionNotificationSubmission{
		RequestID:        requestID,
		SourceID:         operationID,
		SourceVersion:    1,
		CorrelationID:    operationID,
		PrincipalID:      principalID,
		DestinationEmail: "user@example.com",
		Purpose:          biz.PasswordActionPurposeReset,
		Audience:         biz.AudienceConsole,
		ActionToken:      "test-only+/=action-token",
		OccurredAt:       now,
		DeliverBefore:    now.Add(30 * time.Minute),
	})
	if err != nil || got != notificationID {
		t.Fatalf("SubmitPasswordActionNotification() = %q, %v", got, err)
	}
	parsedActionURL, err := url.Parse(client.request.GetIamPasswordAction().GetActionUrl())
	if err != nil {
		t.Fatalf("parse action URL: %v", err)
	}
	if parsedActionURL.Scheme != "https" || parsedActionURL.Host != "console.example.test" ||
		parsedActionURL.Path != "/password-action" || parsedActionURL.Query().Get("token") != "test-only+/=action-token" {
		t.Fatalf("action URL = %q", parsedActionURL.String())
	}
	client.request.GetIamPasswordAction().ActionUrl = "https://console.example.test/password-action?token=redacted"
	want := &notificationv1.SubmitNotificationRequest{
		RequestId: requestID.String(),
		Source: &notificationv1.SourceReference{
			ResourceId: operationID.String(), ResourceVersion: 1,
		},
		CorrelationId: operationID.String(),
		OccurredAt:    timestamppb.New(now),
		DeliverBefore: timestamppb.New(now.Add(30 * time.Minute)),
		Locale:        "en-US",
		Scope: &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_HumanPrincipal{
			HumanPrincipal: &notificationv1.HumanPrincipalScope{PrincipalId: principalID.String()},
		}},
		Recipient: &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_HumanPrincipal{
			HumanPrincipal: &notificationv1.HumanPrincipalRecipient{PrincipalId: principalID.String()},
		}},
		Destination: &notificationv1.DestinationSnapshot{Destination: &notificationv1.DestinationSnapshot_Email{
			Email: &notificationv1.EmailDestinationSnapshot{NormalizedEmail: "user@example.com"},
		}},
		Notification: &notificationv1.SubmitNotificationRequest_IamPasswordAction{IamPasswordAction: &notificationv1.IamPasswordAction{
			ActionUrl:       "https://console.example.test/password-action?token=redacted",
			Purpose:         notificationv1.IamPasswordActionPurpose_IAM_PASSWORD_ACTION_PURPOSE_RESET,
			Audience:        notificationv1.IamPasswordActionAudience_IAM_PASSWORD_ACTION_AUDIENCE_CONSOLE,
			ActionExpiresAt: timestamppb.New(now.Add(30 * time.Minute)),
		}},
	}
	if !reflect.DeepEqual(client.request, want) {
		t.Fatalf("SubmitNotification request = %#v, want %#v", client.request, want)
	}
}

func TestGRPCPasswordActionNotificationSubmitterClassifiesFrozenErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "storage unavailable", err: notificationStatusError(t, codes.Unavailable, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_STORAGE_UNAVAILABLE), want: biz.ErrPasswordActionNotificationRetryable},
		{name: "intake key unavailable", err: notificationStatusError(t, codes.Unavailable, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_INTAKE_KEY_UNAVAILABLE), want: biz.ErrPasswordActionNotificationRetryable},
		{name: "ambiguous deadline", err: status.Error(codes.DeadlineExceeded, "deadline"), want: biz.ErrPasswordActionNotificationRetryable},
		{name: "ambiguous transport", err: status.Error(codes.Unknown, "transport"), want: biz.ErrPasswordActionNotificationRetryable},
		{name: "identity missing", err: notificationStatusError(t, codes.Unauthenticated, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_WORKLOAD_IDENTITY_MISSING), want: biz.ErrPasswordActionNotificationPermanent},
		{name: "capability forbidden", err: notificationStatusError(t, codes.PermissionDenied, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_PRODUCER_TYPE_FORBIDDEN), want: biz.ErrPasswordActionNotificationPermanent},
		{name: "request invalid", err: notificationStatusError(t, codes.InvalidArgument, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_REQUEST_INVALID), want: biz.ErrPasswordActionNotificationPermanent},
		{name: "action URL rejected", err: notificationStatusError(t, codes.InvalidArgument, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_ACTION_URL_NOT_ALLOWED), want: biz.ErrPasswordActionNotificationPermanent},
		{name: "idempotency conflict", err: notificationStatusError(t, codes.AlreadyExists, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_IDEMPOTENCY_CONFLICT), want: biz.ErrPasswordActionNotificationPermanent},
		{name: "template unavailable", err: notificationStatusError(t, codes.FailedPrecondition, notificationv1.NotificationErrorReason_NOTIFICATION_ERROR_REASON_TEMPLATE_UNAVAILABLE), want: biz.ErrPasswordActionNotificationPermanent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingNotificationServiceClient{err: test.err}
			submitter := newTestGRPCPasswordActionNotificationSubmitter(t, client)
			_, err := submitter.SubmitPasswordActionNotification(context.Background(), validPasswordActionNotificationSubmission())
			if !errors.Is(err, test.want) {
				t.Fatalf("SubmitPasswordActionNotification() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestGRPCPasswordActionNotificationSubmitterDoesNotExposeRemoteErrorText(t *testing.T) {
	const sensitiveRemoteText = "test-only-action-token-must-not-escape"
	client := &recordingNotificationServiceClient{err: status.Error(codes.Unavailable, sensitiveRemoteText)}
	submitter := newTestGRPCPasswordActionNotificationSubmitter(t, client)
	_, err := submitter.SubmitPasswordActionNotification(context.Background(), validPasswordActionNotificationSubmission())
	if !errors.Is(err, biz.ErrPasswordActionNotificationRetryable) {
		t.Fatalf("SubmitPasswordActionNotification() error = %v", err)
	}
	if strings.Contains(err.Error(), sensitiveRemoteText) {
		t.Fatalf("SubmitPasswordActionNotification() exposed remote error text: %v", err)
	}
}

func TestGRPCPasswordActionNotificationSubmitterFailsClosedBeforeTransport(t *testing.T) {
	client := &recordingNotificationServiceClient{}
	submitter := newTestGRPCPasswordActionNotificationSubmitter(t, client)
	tests := []struct {
		name   string
		mutate func(*biz.PasswordActionNotificationSubmission)
	}{
		{name: "empty token", mutate: func(value *biz.PasswordActionNotificationSubmission) { value.ActionToken = "" }},
		{name: "boss unsupported", mutate: func(value *biz.PasswordActionNotificationSubmission) { value.Audience = biz.AudienceBoss }},
		{name: "destination not normalized", mutate: func(value *biz.PasswordActionNotificationSubmission) { value.DestinationEmail = "User@example.com" }},
		{name: "non-v7 principal", mutate: func(value *biz.PasswordActionNotificationSubmission) {
			value.PrincipalID = uuid.MustParse("cf8867ce-3314-4c84-98d0-6e9fd985d62a")
		}},
		{name: "invalid lifetime", mutate: func(value *biz.PasswordActionNotificationSubmission) { value.DeliverBefore = value.OccurredAt }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client.request = nil
			value := validPasswordActionNotificationSubmission()
			test.mutate(&value)
			_, err := submitter.SubmitPasswordActionNotification(context.Background(), value)
			if !errors.Is(err, biz.ErrPasswordActionNotificationPermanent) {
				t.Fatalf("SubmitPasswordActionNotification() error = %v", err)
			}
			if client.request != nil {
				t.Fatal("invalid submission reached Notification transport")
			}
		})
	}
}

func TestNewGRPCPasswordActionNotificationSubmitterRejectsUnsafeActionURL(t *testing.T) {
	client := &recordingNotificationServiceClient{}
	for _, actionURL := range []string{
		"http://console.example.test/password-action",
		"https://user@console.example.test/password-action",
		"https://console.example.test/password-action?token=preconfigured",
		"https://console.example.test/password-action#fragment",
	} {
		if _, err := NewGRPCPasswordActionNotificationSubmitter(client, PasswordActionNotificationSubmitterConfig{
			ConsoleActionURLBase: actionURL,
			Locale:               "en-US",
		}); err == nil {
			t.Fatalf("NewGRPCPasswordActionNotificationSubmitter(%q) succeeded", actionURL)
		}
	}
}

func newTestGRPCPasswordActionNotificationSubmitter(t *testing.T, client notificationv1.NotificationServiceClient) biz.PasswordActionNotificationSubmitter {
	t.Helper()
	submitter, err := NewGRPCPasswordActionNotificationSubmitter(client, PasswordActionNotificationSubmitterConfig{
		ConsoleActionURLBase: "https://console.example.test/password-action",
		Locale:               "en-US",
	})
	if err != nil {
		t.Fatal(err)
	}
	return submitter
}

func validPasswordActionNotificationSubmission() biz.PasswordActionNotificationSubmission {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	return biz.PasswordActionNotificationSubmission{
		RequestID:        uuid.MustParse("0198f062-b76d-7001-9000-000000000111"),
		SourceID:         uuid.MustParse("0198f062-b76d-7001-9000-000000000112"),
		SourceVersion:    1,
		CorrelationID:    uuid.MustParse("0198f062-b76d-7001-9000-000000000112"),
		PrincipalID:      uuid.MustParse("0198f062-b76d-7001-9000-000000000113"),
		DestinationEmail: "user@example.com",
		Purpose:          biz.PasswordActionPurposeSetup,
		Audience:         biz.AudienceConsole,
		ActionToken:      "test-only-action-token",
		OccurredAt:       now,
		DeliverBefore:    now.Add(30 * time.Minute),
	}
}

func notificationStatusError(t *testing.T, code codes.Code, reason notificationv1.NotificationErrorReason) error {
	t.Helper()
	value := status.New(code, "notification request failed")
	value, err := value.WithDetails(&notificationv1.NotificationErrorDetail{Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	return value.Err()
}

type recordingNotificationServiceClient struct {
	request  *notificationv1.SubmitNotificationRequest
	response *notificationv1.SubmitNotificationResponse
	err      error
}

func (c *recordingNotificationServiceClient) SubmitNotification(
	_ context.Context,
	request *notificationv1.SubmitNotificationRequest,
	_ ...grpc.CallOption,
) (*notificationv1.SubmitNotificationResponse, error) {
	c.request = request
	return c.response, c.err
}

func (*recordingNotificationServiceClient) GetSubmissionStatus(
	context.Context,
	*notificationv1.GetSubmissionStatusRequest,
	...grpc.CallOption,
) (*notificationv1.GetSubmissionStatusResponse, error) {
	panic("GetSubmissionStatus must not be called by the password-action submitter")
}
