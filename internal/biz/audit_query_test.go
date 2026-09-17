package biz

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type auditQueryFixture struct{ calls int }

func (r *auditQueryFixture) Get(context.Context, TenantScope, uuid.UUID) (AuditRecord, error) {
	return AuditRecord{}, ErrAuditEventNotFound
}
func (r *auditQueryFixture) List(_ context.Context, _ TenantScope, q AuditQuery) ([]AuditRecord, error) {
	r.calls++
	id, _ := uuid.NewV7()
	if q.Before != uuid.Nil {
		return nil, nil
	}
	return []AuditRecord{{SecurityAuditEvent: SecurityAuditEvent{ID: id}}, {SecurityAuditEvent: SecurityAuditEvent{ID: id}}}, nil
}
func TestAuditCursorCannotChangeTenantOrFilters(t *testing.T) {
	r := &auditQueryFixture{}
	u := NewAuditQueryUsecase(r)
	a, _ := NewTenantScope(uuid.New())
	b, _ := NewTenantScope(uuid.New())
	p, err := u.List(context.Background(), a, "iam.role.created", AuditResultSucceeded, "", 1)
	if err != nil || p.NextCursor == "" {
		t.Fatal("first page invalid")
	}
	for _, c := range []struct {
		scope  TenantScope
		action string
		result AuditResult
	}{{b, "iam.role.created", AuditResultSucceeded}, {a, "iam.role.updated", AuditResultSucceeded}, {a, "iam.role.created", AuditResultDenied}} {
		if _, err := u.List(context.Background(), c.scope, c.action, c.result, p.NextCursor, 1); !errors.Is(err, ErrAuditQueryInvalid) {
			t.Fatal("cursor escaped query scope")
		}
	}
	if r.calls != 1 {
		t.Fatal("invalid cursor reached repository")
	}
	if _, err := u.List(context.Background(), a, "iam.role.created", AuditResultSucceeded, p.NextCursor, 1); err != nil {
		t.Fatal(err)
	}
}
