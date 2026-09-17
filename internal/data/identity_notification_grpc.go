package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/url"
	"strings"
)

type IdentityNotificationSubmitterConfig struct{ TenantInvitationURLBase, PlatformInvitationURLBase string }
type grpcIdentityNotificationSubmitter struct {
	client   notificationv1.NotificationServiceClient
	tenant   url.URL
	platform *url.URL
}

func NewGRPCIdentityNotificationSubmitter(client notificationv1.NotificationServiceClient, c IdentityNotificationSubmitterConfig) (biz.IdentityNotificationSubmitter, error) {
	parse := func(raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ToLower(u.Host) != u.Host {
			return nil, errors.New("invitation URL requires a canonical HTTPS base")
		}
		return u, nil
	}
	if client == nil {
		return nil, biz.ErrAuthenticationDependency
	}
	tenant, err := parse(c.TenantInvitationURLBase)
	if err != nil {
		return nil, err
	}
	var platform *url.URL
	if c.PlatformInvitationURLBase != "" {
		platform, err = parse(c.PlatformInvitationURLBase)
		if err != nil || platform.Host == tenant.Host {
			return nil, errors.New("Platform invitation requires a distinct HTTPS origin")
		}
	}
	return &grpcIdentityNotificationSubmitter{client: client, tenant: *tenant, platform: platform}, nil
}
func (s *grpcIdentityNotificationSubmitter) SubmitIdentityNotification(ctx context.Context, v biz.IdentityNotificationSubmission) (string, error) {
	if v.RequestID.Version() != 7 || v.SourceID.Version() != 7 || v.SourceVersion < 1 || v.OccurredAt.IsZero() || !v.DeliverBefore.After(v.OccurredAt) || timestamppb.New(v.OccurredAt).CheckValid() != nil || timestamppb.New(v.DeliverBefore).CheckValid() != nil || v.Email == "" || v.Email != strings.ToLower(strings.TrimSpace(v.Email)) || v.Secret == "" || len(v.Secret) > 512 || (v.Locale != "en-US" && v.Locale != "zh-CN") {
		return "", biz.ErrIdentityNotificationPermanent
	}
	r := &notificationv1.SubmitNotificationRequest{RequestId: v.RequestID.String(), Source: &notificationv1.SourceReference{ResourceId: v.SourceID.String(), ResourceVersion: uint64(v.SourceVersion)}, CorrelationId: v.SourceID.String(), OccurredAt: timestamppb.New(v.OccurredAt), DeliverBefore: timestamppb.New(v.DeliverBefore), Locale: v.Locale,
		Recipient:   &notificationv1.NotificationRecipient{Recipient: &notificationv1.NotificationRecipient_Direct{Direct: &notificationv1.DirectRecipient{}}},
		Destination: &notificationv1.DestinationSnapshot{Destination: &notificationv1.DestinationSnapshot_Email{Email: &notificationv1.EmailDestinationSnapshot{NormalizedEmail: v.Email}}}}
	switch v.Kind {
	case biz.IdentityNotificationTenantInvitation:
		if v.TenantID.Version() != 7 {
			return "", biz.ErrIdentityNotificationPermanent
		}
		u := s.tenant
		q := u.Query()
		q.Set("token", v.Secret)
		u.RawQuery = q.Encode()
		r.Scope = &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_Tenant{Tenant: &notificationv1.TenantScope{TenantId: v.TenantID.String()}}}
		r.Notification = &notificationv1.SubmitNotificationRequest_IamTenantInvitation{IamTenantInvitation: &notificationv1.IamTenantInvitation{ActionUrl: u.String(), TenantDisplayName: v.TenantID.String(), InvitationExpiresAt: timestamppb.New(v.DeliverBefore)}}
	case biz.IdentityNotificationPlatformInvitation:
		if v.TenantID != uuid.Nil || s.platform == nil {
			return "", biz.ErrIdentityNotificationPermanent
		}
		u := *s.platform
		q := u.Query()
		q.Set("token", v.Secret)
		u.RawQuery = q.Encode()
		r.Scope = &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_PlatformInvitation{PlatformInvitation: &notificationv1.PlatformInvitationScope{InvitationId: v.SourceID.String()}}}
		r.Notification = &notificationv1.SubmitNotificationRequest_IamPlatformInvitation{IamPlatformInvitation: &notificationv1.IamPlatformInvitation{ActionUrl: u.String(), InvitationExpiresAt: timestamppb.New(v.DeliverBefore)}}
	case biz.IdentityNotificationEmailVerification:
		if v.TenantID != uuid.Nil || v.SourceVersion != 1 || len(v.Secret) != 6 {
			return "", biz.ErrIdentityNotificationPermanent
		}
		for _, c := range v.Secret {
			if c < '0' || c > '9' {
				return "", biz.ErrIdentityNotificationPermanent
			}
		}
		r.Scope = &notificationv1.NotificationScope{Scope: &notificationv1.NotificationScope_EmailVerification{EmailVerification: &notificationv1.EmailVerificationScope{VerificationId: v.SourceID.String()}}}
		r.Notification = &notificationv1.SubmitNotificationRequest_IamEmailVerification{IamEmailVerification: &notificationv1.IamEmailVerification{Code: v.Secret, VerificationExpiresAt: timestamppb.New(v.DeliverBefore)}}
	default:
		return "", biz.ErrIdentityNotificationPermanent
	}
	response, err := s.client.SubmitNotification(ctx, r)
	if err != nil {
		// Local Workload issuance returns domain errors before any gRPC hop.
		// A revoked identity/grant is an authority denial, not a transport outage.
		if errors.Is(err, biz.ErrWorkloadPermissionDenied) || errors.Is(err, biz.ErrWorkloadIdentityInvalid) {
			return "", biz.ErrIdentityNotificationPermanent
		}
		classified := classifyPasswordActionNotificationError(err)
		if errors.Is(classified, biz.ErrPasswordActionNotificationPermanent) {
			return "", biz.ErrIdentityNotificationPermanent
		}
		return "", biz.ErrIdentityNotificationRetryable
	}
	if response == nil || response.Receipt == nil || response.Receipt.RequestId != v.RequestID.String() || response.Receipt.StoredAt == nil || response.Receipt.StoredAt.CheckValid() != nil {
		return "", biz.ErrIdentityNotificationRetryable
	}
	id, err := uuid.Parse(response.Receipt.NotificationId)
	if err != nil || id == uuid.Nil || id.String() != response.Receipt.NotificationId {
		return "", biz.ErrIdentityNotificationRetryable
	}
	return id.String(), nil
}
