package biz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestMembershipUsecaseCreateCommitsMembershipAndAuditTogether(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70")
	principalID := uuid.MustParse("0198f062-b76d-7c0e-b701-9c8bc9421f5d")
	actorID := uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17")
	membershipID := uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01")
	auditID := uuid.MustParse("0198f062-b76d-7653-8d33-55e7ddf4094b")
	now := time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC)
	scope, err := biz.NewTenantScope(tenantID)
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}

	memberships := &fakeMembershipRepository{}
	audits := &fakeAuditRepository{}
	uow := &fakeUnitOfWork{tx: fakeTenantTransaction{memberships: memberships, audits: audits}}
	usecase := biz.NewMembershipUsecase(uow, &sequenceIDGenerator{ids: []uuid.UUID{membershipID, auditID}}, fixedClock{now: now})

	got, err := usecase.Create(context.Background(), scope, biz.CreateMembershipCommand{
		PrincipalID:          principalID,
		ActorID:              actorID,
		AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID:            "req-dp2-04-create-membership",
		CorrelationID:        "corr-dp2-04-create-membership",
		DecisionID:           "decision-dp2-04-create-membership",
		Reason:               biz.AuditReasonTenantBootstrap,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !uow.committed {
		t.Fatal("Create() did not commit the UnitOfWork")
	}
	if memberships.created.ID != membershipID || memberships.created.PrincipalID != principalID {
		t.Fatalf("created membership = %#v", memberships.created)
	}
	if memberships.created.Status != biz.MembershipStatusActive || memberships.created.Version != 1 {
		t.Fatalf("created membership status/version = %q/%d", memberships.created.Status, memberships.created.Version)
	}
	if audits.appended.ID != auditID || audits.appended.TargetID != membershipID {
		t.Fatalf("appended audit = %#v", audits.appended)
	}
	if audits.appended.Action != biz.AuditActionMembershipCreated || audits.appended.Result != biz.AuditResultSucceeded {
		t.Fatalf("appended audit action/result = %q/%q", audits.appended.Action, audits.appended.Result)
	}
	if audits.appended.AuthenticationMethod != biz.AuditAuthenticationMethodPassword || audits.appended.Boundary != biz.AuditBoundaryTenant {
		t.Fatalf("appended audit authn/boundary = %q/%q", audits.appended.AuthenticationMethod, audits.appended.Boundary)
	}
	if audits.appended.TargetType != biz.AuditTargetTypeTenantMembership || audits.appended.Reason != biz.AuditReasonTenantBootstrap {
		t.Fatalf("appended audit target/reason = %q/%q", audits.appended.TargetType, audits.appended.Reason)
	}
	if audits.appended.DecisionID != "decision-dp2-04-create-membership" || audits.appended.SourceService != biz.AuditSourceServiceIAM {
		t.Fatalf("appended audit decision/source = %q/%q", audits.appended.DecisionID, audits.appended.SourceService)
	}
	if got.Membership.ID != membershipID || got.AuditEventID != auditID {
		t.Fatalf("Create() result = %#v", got)
	}
}

func TestMembershipUsecaseCreateFailsWhenAuditFails(t *testing.T) {
	t.Parallel()

	scope, err := biz.NewTenantScope(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"))
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}
	auditErr := errors.New("audit unavailable")
	memberships := &fakeMembershipRepository{}
	audits := &fakeAuditRepository{appendErr: auditErr}
	uow := &fakeUnitOfWork{tx: fakeTenantTransaction{memberships: memberships, audits: audits}}
	usecase := biz.NewMembershipUsecase(
		uow,
		&sequenceIDGenerator{ids: []uuid.UUID{
			uuid.MustParse("0198f062-b76d-704f-ac04-ab231ee78f01"),
			uuid.MustParse("0198f062-b76d-7653-8d33-55e7ddf4094b"),
		}},
		fixedClock{now: time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC)},
	)

	_, err = usecase.Create(context.Background(), scope, biz.CreateMembershipCommand{
		PrincipalID:          uuid.MustParse("0198f062-b76d-7c0e-b701-9c8bc9421f5d"),
		ActorID:              uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID:            "req-audit-failure",
		CorrelationID:        "corr-audit-failure",
		DecisionID:           "decision-audit-failure",
		Reason:               biz.AuditReasonTenantBootstrap,
	})
	if !errors.Is(err, auditErr) {
		t.Fatalf("Create() error = %v, want %v", err, auditErr)
	}
	if uow.committed {
		t.Fatal("Create() committed after the required audit failed")
	}
	if memberships.created.ID == uuid.Nil {
		t.Fatal("test did not reach membership mutation before the audit failure")
	}
}

func TestMembershipUsecaseCreateRejectsIncompleteAuditIdentity(t *testing.T) {
	t.Parallel()

	scope, err := biz.NewTenantScope(uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"))
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}
	valid := biz.CreateMembershipCommand{
		PrincipalID:          uuid.MustParse("0198f062-b76d-7c0e-b701-9c8bc9421f5d"),
		ActorID:              uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17"),
		AuthenticationMethod: biz.AuditAuthenticationMethodPassword,
		RequestID:            "req-audit-identity",
		CorrelationID:        "corr-audit-identity",
		DecisionID:           "decision-audit-identity",
		Reason:               biz.AuditReasonTenantBootstrap,
	}
	tests := []struct {
		name   string
		mutate func(*biz.CreateMembershipCommand)
		want   error
	}{
		{name: "authentication method", mutate: func(command *biz.CreateMembershipCommand) { command.AuthenticationMethod = "" }, want: biz.ErrAuditAuthenticationMethodRequired},
		{name: "request identity", mutate: func(command *biz.CreateMembershipCommand) { command.RequestID = "" }, want: biz.ErrAuditRequestIDRequired},
		{name: "correlation identity", mutate: func(command *biz.CreateMembershipCommand) { command.CorrelationID = "" }, want: biz.ErrAuditCorrelationIDRequired},
		{name: "decision identity", mutate: func(command *biz.CreateMembershipCommand) { command.DecisionID = "" }, want: biz.ErrAuditDecisionIDRequired},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := valid
			test.mutate(&command)
			usecase := biz.NewMembershipUsecase(
				&fakeUnitOfWork{},
				&sequenceIDGenerator{},
				fixedClock{now: time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC)},
			)
			_, gotErr := usecase.Create(context.Background(), scope, command)
			if !errors.Is(gotErr, test.want) {
				t.Fatalf("Create() error = %v, want %v", gotErr, test.want)
			}
		})
	}
}

type fakeUnitOfWork struct {
	tx        fakeTenantTransaction
	committed bool
}

func (u *fakeUnitOfWork) WithinTenant(ctx context.Context, scope biz.TenantScope, fn func(context.Context, biz.TenantTransaction) error) error {
	if _, err := scope.TenantID(); err != nil {
		return err
	}
	if err := fn(ctx, u.tx); err != nil {
		return err
	}
	u.committed = true
	return nil
}

type fakeTenantTransaction struct {
	memberships biz.TenantMembershipRepository
	audits      biz.SecurityAuditRepository
}

func (tx fakeTenantTransaction) Memberships() biz.TenantMembershipRepository {
	return tx.memberships
}

func (tx fakeTenantTransaction) AuditEvents() biz.SecurityAuditRepository {
	return tx.audits
}

type fakeMembershipRepository struct {
	created biz.TenantMembership
}

func (r *fakeMembershipRepository) Create(_ context.Context, _ biz.TenantScope, membership biz.TenantMembership) error {
	r.created = membership
	return nil
}

func (*fakeMembershipRepository) Get(context.Context, biz.TenantScope, uuid.UUID) (biz.TenantMembership, error) {
	return biz.TenantMembership{}, errors.New("not implemented by this use-case test")
}

func (*fakeMembershipRepository) UpdateStatus(context.Context, biz.TenantScope, uuid.UUID, biz.MembershipStatus, int64, time.Time) (biz.TenantMembership, error) {
	return biz.TenantMembership{}, errors.New("not implemented by this use-case test")
}

type fakeAuditRepository struct {
	appended  biz.SecurityAuditEvent
	appendErr error
}

func (r *fakeAuditRepository) Append(_ context.Context, _ biz.TenantScope, event biz.SecurityAuditEvent) error {
	r.appended = event
	return r.appendErr
}

type sequenceIDGenerator struct {
	ids []uuid.UUID
}

func (g *sequenceIDGenerator) NewID() (uuid.UUID, error) {
	if len(g.ids) == 0 {
		return uuid.Nil, errors.New("no test ID available")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}
