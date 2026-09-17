package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestGRPCIdentityNotificationPreservesScopeAndPurpose(t *testing.T) {
	now := time.Now().UTC()
	source, request, tenant := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	client := &recordingNotificationServiceClient{response: &notificationv1.SubmitNotificationResponse{Receipt: &notificationv1.NotificationReceipt{NotificationId: uuid.NewString(), RequestId: request.String(), StoredAt: timestamppb.New(now)}}}
	submitter, err := NewGRPCIdentityNotificationSubmitter(client, IdentityNotificationSubmitterConfig{TenantInvitationURLBase: "https://console.example.test/invitation", PlatformInvitationURLBase: "https://boss.example.test/invitation"})
	if err != nil {
		t.Fatal("identity submitter configuration")
	}
	for _, kind := range []biz.IdentityNotificationKind{biz.IdentityNotificationTenantInvitation, biz.IdentityNotificationPlatformInvitation, biz.IdentityNotificationEmailVerification} {
		value := biz.IdentityNotificationSubmission{Kind: kind, RequestID: request, SourceID: source, SourceVersion: 1, Email: "invited@example.test", Secret: "123456", Locale: "en-US", OccurredAt: now, DeliverBefore: now.Add(time.Minute)}
		if kind == biz.IdentityNotificationTenantInvitation {
			value.TenantID = tenant
		}
		if _, err = submitter.SubmitIdentityNotification(context.Background(), value); err != nil {
			t.Fatal("typed identity delivery rejected")
		}
		r := client.request
		if r.Recipient.GetDirect() == nil || r.Recipient.GetHumanPrincipal() != nil || r.Source.ResourceId != source.String() {
			t.Fatal("identity projection invented Human recipient")
		}
		switch kind {
		case biz.IdentityNotificationTenantInvitation:
			if r.Scope.GetTenant().GetTenantId() != tenant.String() || r.GetIamTenantInvitation() == nil {
				t.Fatal("Tenant scope lost")
			}
		case biz.IdentityNotificationPlatformInvitation:
			if r.Scope.GetPlatformInvitation().GetInvitationId() != source.String() || r.Scope.GetTenant() != nil || r.GetIamPlatformInvitation() == nil {
				t.Fatal("Platform invitation acquired a Tenant")
			}
		case biz.IdentityNotificationEmailVerification:
			if r.Scope.GetEmailVerification().GetVerificationId() != source.String() || r.GetIamEmailVerification().GetCode() != value.Secret || r.Scope.GetTenant() != nil {
				t.Fatal("verification object or code lost")
			}
		}
		client.err = biz.ErrWorkloadPermissionDenied
		if _, err = submitter.SubmitIdentityNotification(context.Background(), value); !errors.Is(err, biz.ErrIdentityNotificationPermanent) {
			t.Fatal("local Workload denial was treated as a transport retry")
		}
		client.err = nil
	}
}
