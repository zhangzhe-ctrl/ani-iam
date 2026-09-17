package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"time"
)

type PlatformAuditRecord struct {
	Record   AuditRecord
	TenantID uuid.UUID
}
type PlatformAuditQuery struct {
	TenantID uuid.UUID
	AuditQuery
}
type PlatformAuditPage struct {
	Events     []PlatformAuditRecord
	NextCursor string
}
type PlatformAuditRepository interface {
	GetPlatformAuditEvent(context.Context, PlatformCapability, uuid.UUID) (PlatformAuditRecord, error)
	ListPlatformAuditEvents(context.Context, PlatformCapability, PlatformAuditQuery) ([]PlatformAuditRecord, error)
}
type platformAuditCursor struct {
	Actor, Tenant, Before uuid.UUID
	Revision, Action      string
	Result                AuditResult
}

func (u *PlatformAdministrationUsecase) GetAuditEvent(ctx context.Context, cap PlatformCapability, id uuid.UUID) (PlatformAuditRecord, error) {
	if id == uuid.Nil {
		return PlatformAuditRecord{}, ErrAuditQueryInvalid
	}
	var result PlatformAuditRecord
	err := u.withinAuthorized(ctx, cap, "getPlatformIAMSecurityAuditEvent", func(tx PlatformAdministrationTransaction, now time.Time) error {
		var err error
		result, err = tx.GetPlatformAuditEvent(ctx, cap, id)
		if err != nil {
			return err
		}
		return u.auditPlatformQuery(ctx, tx, cap, id, 1, now)
	})
	return result, err
}
func (u *PlatformAdministrationUsecase) ListAuditEvents(ctx context.Context, cap PlatformCapability, tenant uuid.UUID, action string, result AuditResult, cursor string, limit uint32) (PlatformAuditPage, error) {
	if limit > 100 || len(action) > 128 || strings.TrimSpace(action) != action || len(cursor) > 2048 || (result != "" && result != AuditResultSucceeded && result != AuditResultDenied && result != AuditResultFailed) {
		return PlatformAuditPage{}, ErrAuditQueryInvalid
	}
	if limit == 0 {
		limit = 50
	}
	q := PlatformAuditQuery{TenantID: tenant, AuditQuery: AuditQuery{Action: action, Result: result, Limit: int32(limit) + 1}}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var c platformAuditCursor
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Actor != cap.claims.Subject || c.Tenant != tenant || c.Revision != cap.revision || c.Action != action || c.Result != result || c.Before == uuid.Nil {
			return PlatformAuditPage{}, ErrAuditQueryInvalid
		}
		q.Before = c.Before
	}
	var page PlatformAuditPage
	err := u.withinAuthorized(ctx, cap, "listPlatformIAMSecurityAuditEvents", func(tx PlatformAdministrationTransaction, now time.Time) error {
		rows, err := tx.ListPlatformAuditEvents(ctx, cap, q)
		if err != nil {
			return err
		}
		page.Events = rows
		if len(rows) > int(limit) {
			page.Events = rows[:limit]
			raw, _ := json.Marshal(platformAuditCursor{Actor: cap.claims.Subject, Tenant: tenant, Before: page.Events[len(page.Events)-1].Record.ID, Revision: cap.revision, Action: action, Result: result})
			page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return u.auditPlatformQuery(ctx, tx, cap, uuid.Nil, 1, now)
	})
	return page, err
}
func (u *PlatformAdministrationUsecase) auditPlatformQuery(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, target uuid.UUID, version int64, now time.Time) error {
	id, err := u.ids.NewID()
	if err != nil || id.Version() != 7 {
		return ErrInvalidGeneratedID
	}
	if target == uuid.Nil {
		target = id
	}
	return tx.AppendAudit(ctx, cap, newPlatformAdministrationAudit(cap, "iam.audit-events", id, target, version, now))
}
