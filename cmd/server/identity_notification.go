package main

import (
	"context"
	"errors"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"net/url"
)

// The single worker visits all four purpose-specific outboxes fairly. A busy
// Tenant queue cannot starve Platform invitations or account verification.
type notificationDispatchers struct {
	password passwordActionNotificationDispatcher
	identity *biz.IdentityNotificationDispatcher
	next     int
}

func (d *notificationDispatchers) DispatchNext(ctx context.Context) (bool, error) {
	index := d.next
	d.next = (d.next + 1) % 4
	if index == 0 {
		return d.password.DispatchNext(ctx)
	}
	kind := []biz.IdentityNotificationKind{biz.IdentityNotificationTenantInvitation, biz.IdentityNotificationPlatformInvitation, biz.IdentityNotificationEmailVerification}[index-1]
	worked, err := d.identity.DispatchNext(ctx, kind)
	if errors.Is(err, biz.ErrIdentityNotificationRetryable) {
		return worked, biz.ErrPasswordActionNotificationRetryable
	}
	return worked, err
}
func attachIdentityNotificationRuntime(w *passwordActionNotificationWorker, client *data.NotificationGRPCClient, postgres *data.Data, c *conf.Notification, clock biz.Clock) error {
	if c.PauseIdentityDelivery {
		return nil
	}
	// Older process configurations retain their controlled Console/BOSS origins;
	// explicit invitation URL settings can choose the actual activation page.
	base := func(explicit, action string) string {
		if explicit != "" {
			return explicit
		}
		if action == "" {
			return ""
		}
		u, err := url.Parse(action)
		if err != nil {
			return ""
		}
		u.Path = "/invitation"
		u.RawPath = ""
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	submitter, err := data.NewGRPCIdentityNotificationSubmitter(client, data.IdentityNotificationSubmitterConfig{TenantInvitationURLBase: base(c.TenantInvitationUrlBase, c.ConsoleActionUrlBase), PlatformInvitationURLBase: base(c.PlatformInvitationUrlBase, c.BossActionUrlBase)})
	if err != nil {
		return err
	}
	outbox, err := data.NewIdentityNotificationOutbox(postgres, c.Locale)
	if err != nil {
		return err
	}
	dispatcher, err := biz.NewIdentityNotificationDispatcher(outbox, submitter, clock)
	if err != nil {
		return err
	}
	w.dispatcher = &notificationDispatchers{password: w.dispatcher, identity: dispatcher}
	return nil
}
