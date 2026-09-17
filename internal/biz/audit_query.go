package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

const AuditResultDenied AuditResult = "denied"

var (
	ErrAuditEventNotFound = errors.New("audit event was not found")
	ErrAuditQueryInvalid  = errors.New("audit query is invalid")
)

type AuditQuery struct {
	Action string
	Result AuditResult
	Before uuid.UUID
	Limit  int32
}
type AuditRecord struct {
	SecurityAuditEvent
	ActorType string
}
type TenantAuditReader interface {
	Get(context.Context, TenantScope, uuid.UUID) (AuditRecord, error)
	List(context.Context, TenantScope, AuditQuery) ([]AuditRecord, error)
}
type AuditPage struct {
	Events     []AuditRecord
	NextCursor string
}
type AuditQueryUsecase struct{ reader TenantAuditReader }

func NewAuditQueryUsecase(reader TenantAuditReader) *AuditQueryUsecase {
	return &AuditQueryUsecase{reader: reader}
}

type auditQueryCursor struct {
	Tenant uuid.UUID   `json:"tenant"`
	Action string      `json:"action"`
	Result AuditResult `json:"result"`
	Before uuid.UUID   `json:"before"`
}

func (u *AuditQueryUsecase) Get(ctx context.Context, scope TenantScope, id uuid.UUID) (AuditRecord, error) {
	if _, err := scope.TenantID(); err != nil {
		return AuditRecord{}, err
	}
	if id == uuid.Nil {
		return AuditRecord{}, ErrAuditQueryInvalid
	}
	if u == nil || u.reader == nil {
		return AuditRecord{}, ErrAuthenticationDependency
	}
	return u.reader.Get(ctx, scope, id)
}

func (u *AuditQueryUsecase) List(ctx context.Context, scope TenantScope, action string, result AuditResult, cursor string, limit uint32) (AuditPage, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return AuditPage{}, err
	}
	if limit > 100 || len(action) > 128 || strings.TrimSpace(action) != action || len(cursor) > 2048 || (result != "" && result != AuditResultSucceeded && result != AuditResultDenied && result != AuditResultFailed) {
		return AuditPage{}, ErrAuditQueryInvalid
	}
	if u == nil || u.reader == nil {
		return AuditPage{}, ErrAuthenticationDependency
	}
	if limit == 0 {
		limit = 50
	}
	query := AuditQuery{Action: action, Result: result, Limit: int32(limit) + 1}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return AuditPage{}, ErrAuditQueryInvalid
		}
		var c auditQueryCursor
		if json.Unmarshal(raw, &c) != nil || c.Tenant != tenant || c.Action != action || c.Result != result || c.Before == uuid.Nil {
			return AuditPage{}, ErrAuditQueryInvalid
		}
		query.Before = c.Before
	}
	rows, err := u.reader.List(ctx, scope, query)
	if err != nil {
		return AuditPage{}, err
	}
	page := AuditPage{Events: rows}
	if len(rows) > int(limit) {
		page.Events = rows[:limit]
		raw, _ := json.Marshal(auditQueryCursor{Tenant: tenant, Action: action, Result: result, Before: page.Events[len(page.Events)-1].ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
