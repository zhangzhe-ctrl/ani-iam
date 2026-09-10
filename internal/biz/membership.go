package biz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPrincipalRequired                 = errors.New("principal is required")
	ErrAuditActorRequired                = errors.New("audit actor is required")
	ErrAuditAuthenticationMethodRequired = errors.New("audit authentication method is required")
	ErrAuditReasonRequired               = errors.New("audit reason is required")
	ErrAuditRequestIDRequired            = errors.New("audit request identity is required")
	ErrAuditCorrelationIDRequired        = errors.New("audit correlation identity is required")
	ErrAuditDecisionIDRequired           = errors.New("audit decision identity is required")
	ErrInvalidGeneratedID                = errors.New("generated ID must be UUIDv7")
	ErrMembershipNotFound                = errors.New("tenant membership not found")
	ErrMembershipConflict                = errors.New("tenant membership conflicts with existing state")
	ErrVersionConflict                   = errors.New("aggregate version conflict")
	ErrTenantRelationConflict            = errors.New("tenant relationship violates its boundary")
	ErrAuditConflict                     = errors.New("security audit identity conflicts with existing state")
	ErrInvalidPersistenceState           = errors.New("persistence state violates a domain constraint")
	ErrPersistencePermissionDenied       = errors.New("persistence operation is not permitted")
	ErrPersistenceUnavailable            = errors.New("persistence is unavailable")
)

type MembershipStatus string

const (
	MembershipStatusActive    MembershipStatus = "active"
	MembershipStatusSuspended MembershipStatus = "suspended"
	MembershipStatusRemoved   MembershipStatus = "removed"
)

type AuditAction string

const AuditActionMembershipCreated AuditAction = "iam.membership.created"

type AuditResult string

const AuditResultSucceeded AuditResult = "succeeded"

const AuditResultFailed AuditResult = "failed"

type AuditAuthenticationMethod string

const (
	AuditAuthenticationMethodPassword      AuditAuthenticationMethod = "password"
	AuditAuthenticationMethodOIDC          AuditAuthenticationMethod = "oidc"
	AuditAuthenticationMethodAPIKey        AuditAuthenticationMethod = "api_key"
	AuditAuthenticationMethodWorkloadToken AuditAuthenticationMethod = "workload_token"
	AuditAuthenticationMethodInternal      AuditAuthenticationMethod = "internal"
)

type AuditBoundary string

const AuditBoundaryTenant AuditBoundary = "tenant"

type AuditTargetType string

const AuditTargetTypeTenantMembership AuditTargetType = "tenant_membership"

type AuditReason string

const AuditReasonTenantBootstrap AuditReason = "TENANT_BOOTSTRAP"

type AuditSourceService string

const AuditSourceServiceIAM AuditSourceService = "iam-service"

// TenantMembership is a framework- and persistence-independent domain object.
// Tenant identity is carried by the mandatory TenantScope at every repository
// boundary instead of being accepted as an untrusted field on this object.
type TenantMembership struct {
	ID          uuid.UUID
	PrincipalID uuid.UUID
	Status      MembershipStatus
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SecurityAuditEvent is the allowlisted domain shape written atomically with a
// security mutation. It intentionally excludes arbitrary payloads and secrets.
type SecurityAuditEvent struct {
	DirectCaller         DirectCaller
	ID                   uuid.UUID
	ActorID              uuid.UUID
	AuthenticationMethod AuditAuthenticationMethod
	Boundary             AuditBoundary
	Action               AuditAction
	TargetType           AuditTargetType
	TargetID             uuid.UUID
	TargetVersion        int64
	Result               AuditResult
	Reason               AuditReason
	RequestID            string
	CorrelationID        string
	DecisionID           string
	SourceService        AuditSourceService
	OccurredAt           time.Time
	RecordedAt           time.Time
}

// TenantMembershipRepository is declared beside the use case that consumes it.
// Every method requires a TenantScope; no unscoped alternative exists.
type TenantMembershipRepository interface {
	Create(context.Context, TenantScope, TenantMembership) error
	Get(context.Context, TenantScope, uuid.UUID) (TenantMembership, error)
	UpdateStatus(context.Context, TenantScope, uuid.UUID, MembershipStatus, int64, time.Time) (TenantMembership, error)
}

type SecurityAuditRepository interface {
	Append(context.Context, TenantScope, SecurityAuditEvent) error
}

// TenantTransaction exposes only domain repositories backed by one transaction.
type TenantTransaction interface {
	Memberships() TenantMembershipRepository
	AuditEvents() SecurityAuditRepository
}

// TenantUnitOfWork owns commit and rollback; use cases own transaction scope.
type TenantUnitOfWork interface {
	WithinTenant(context.Context, TenantScope, func(context.Context, TenantTransaction) error) error
}

type IDGenerator interface {
	NewID() (uuid.UUID, error)
}

type Clock interface {
	Now() time.Time
}

type CreateMembershipCommand struct {
	PrincipalID          uuid.UUID
	ActorID              uuid.UUID
	AuthenticationMethod AuditAuthenticationMethod
	RequestID            string
	CorrelationID        string
	DecisionID           string
	Reason               AuditReason
}

type CreateMembershipResult struct {
	Membership   TenantMembership
	AuditEventID uuid.UUID
}

type MembershipUsecase struct {
	uow   TenantUnitOfWork
	ids   IDGenerator
	clock Clock
}

func NewMembershipUsecase(uow TenantUnitOfWork, ids IDGenerator, clock Clock) *MembershipUsecase {
	return &MembershipUsecase{uow: uow, ids: ids, clock: clock}
}

func (u *MembershipUsecase) Create(ctx context.Context, scope TenantScope, command CreateMembershipCommand) (CreateMembershipResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return CreateMembershipResult{}, err
	}
	if command.PrincipalID == uuid.Nil {
		return CreateMembershipResult{}, ErrPrincipalRequired
	}
	if command.ActorID == uuid.Nil {
		return CreateMembershipResult{}, ErrAuditActorRequired
	}
	if command.AuthenticationMethod == "" {
		return CreateMembershipResult{}, ErrAuditAuthenticationMethodRequired
	}
	if command.Reason == "" {
		return CreateMembershipResult{}, ErrAuditReasonRequired
	}
	if command.RequestID == "" {
		return CreateMembershipResult{}, ErrAuditRequestIDRequired
	}
	if command.CorrelationID == "" {
		return CreateMembershipResult{}, ErrAuditCorrelationIDRequired
	}
	if command.DecisionID == "" {
		return CreateMembershipResult{}, ErrAuditDecisionIDRequired
	}

	membershipID, err := u.newID()
	if err != nil {
		return CreateMembershipResult{}, fmt.Errorf("generate membership ID: %w", err)
	}
	auditID, err := u.newID()
	if err != nil {
		return CreateMembershipResult{}, fmt.Errorf("generate audit event ID: %w", err)
	}
	now := u.clock.Now().UTC()
	membership := TenantMembership{
		ID:          membershipID,
		PrincipalID: command.PrincipalID,
		Status:      MembershipStatusActive,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	audit := SecurityAuditEvent{
		ID:                   auditID,
		ActorID:              command.ActorID,
		AuthenticationMethod: command.AuthenticationMethod,
		Boundary:             AuditBoundaryTenant,
		Action:               AuditActionMembershipCreated,
		TargetType:           AuditTargetTypeTenantMembership,
		TargetID:             membershipID,
		TargetVersion:        membership.Version,
		Result:               AuditResultSucceeded,
		Reason:               command.Reason,
		RequestID:            command.RequestID,
		CorrelationID:        command.CorrelationID,
		DecisionID:           command.DecisionID,
		SourceService:        AuditSourceServiceIAM,
		OccurredAt:           now,
		RecordedAt:           now,
	}

	err = u.uow.WithinTenant(ctx, scope, func(txContext context.Context, tx TenantTransaction) error {
		if err := tx.Memberships().Create(txContext, scope, membership); err != nil {
			return err
		}
		if err := tx.AuditEvents().Append(txContext, scope, audit); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return CreateMembershipResult{}, err
	}
	return CreateMembershipResult{Membership: membership, AuditEventID: auditID}, nil
}

func (u *MembershipUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrInvalidGeneratedID
	}
	return id, nil
}
