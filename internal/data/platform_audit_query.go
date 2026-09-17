package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (t *platformAdministrationTransaction) GetPlatformAuditEvent(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.PlatformAuditRecord, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformAuditRecord{}, err
	}
	v, err := t.q.GetPlatformAuditEvent(ctx, sqlcgen.GetPlatformAuditEventParams{EventID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformAuditRecord{}, biz.ErrAuditEventNotFound
	}
	if err != nil {
		return biz.PlatformAuditRecord{}, platformLoginPersistenceError(err)
	}
	return platformAuditRecord(v), nil
}
func (t *platformAdministrationTransaction) ListPlatformAuditEvents(ctx context.Context, cap biz.PlatformCapability, q biz.PlatformAuditQuery) ([]biz.PlatformAuditRecord, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 101 {
		return nil, biz.ErrAuditQueryInvalid
	}
	rows, err := t.q.ListPlatformAuditEvents(ctx, sqlcgen.ListPlatformAuditEventsParams{TenantID: q.TenantID, Action: q.Action, Result: string(q.Result), BeforeID: q.Before, PageLimit: q.Limit})
	if err != nil {
		return nil, platformLoginPersistenceError(err)
	}
	result := make([]biz.PlatformAuditRecord, 0, len(rows))
	for _, v := range rows {
		result = append(result, platformAuditRecord(sqlcgen.GetPlatformAuditEventRow(v)))
	}
	return result, nil
}
func platformAuditRecord(v sqlcgen.GetPlatformAuditEventRow) biz.PlatformAuditRecord {
	tenant := uuid.Nil
	if v.TenantID.Valid {
		tenant = uuid.UUID(v.TenantID.Bytes)
	}
	return biz.PlatformAuditRecord{Record: auditRecord(sqlcgen.GetTenantAuditEventRow(v)), TenantID: tenant}
}
