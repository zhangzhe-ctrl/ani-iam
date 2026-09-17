package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestPlatformAuditCursorBindsActorTenantAndFilters(t *testing.T) {
	u := &PlatformAdministrationUsecase{}
	cap := PlatformCapability{claims: AccessTokenClaims{Subject: uuid.Must(uuid.NewV7())}, revision: "unit-policy"}
	tenant := uuid.Must(uuid.NewV7())
	base := platformAuditCursor{Actor: cap.claims.Subject, Tenant: tenant, Before: uuid.Must(uuid.NewV7()), Revision: cap.revision, Action: "iam.test", Result: AuditResultSucceeded}
	for _, mutate := range []func(*platformAuditCursor){func(c *platformAuditCursor) { c.Actor = uuid.Must(uuid.NewV7()) }, func(c *platformAuditCursor) { c.Tenant = uuid.Must(uuid.NewV7()) }, func(c *platformAuditCursor) { c.Action = "other" }, func(c *platformAuditCursor) { c.Result = AuditResultDenied }, func(c *platformAuditCursor) { c.Revision = "other" }} {
		c := base
		mutate(&c)
		raw, _ := json.Marshal(c)
		if _, err := u.ListAuditEvents(context.Background(), cap, tenant, base.Action, base.Result, base64.RawURLEncoding.EncodeToString(raw), 1); !errors.Is(err, ErrAuditQueryInvalid) {
			t.Fatal("cursor changed query identity")
		}
	}
}
