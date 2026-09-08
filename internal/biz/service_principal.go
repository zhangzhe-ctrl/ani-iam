package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrServicePrincipalNameRequired  = errors.New("service principal name is required")
	ErrServicePrincipalRolesRequired = errors.New("service principal requires at least one role")
	ErrServicePrincipalNotFound      = errors.New("service principal not found")
	ErrServicePrincipalConflict      = errors.New("service principal conflicts with existing state")
	ErrServicePrincipalDisabled      = errors.New("service principal is disabled")
	ErrAPIKeyNotFound                = errors.New("API key not found")
	ErrAPIKeyConflict                = errors.New("API key conflicts with existing state")
	ErrAPIKeyExpiryInvalid           = errors.New("API key expiry mode is invalid")
)

const (
	AuditActionServicePrincipalCreated  AuditAction     = "iam.service-principal.created"
	AuditActionServicePrincipalUpdated  AuditAction     = "iam.service-principal.updated"
	AuditActionServicePrincipalDisabled AuditAction     = "iam.service-principal.disabled"
	AuditActionAPIKeyCreated            AuditAction     = "iam.api-key.created"
	AuditActionAPIKeyRevoked            AuditAction     = "iam.api-key.revoked"
	AuditTargetTypeServicePrincipal     AuditTargetType = "service_principal"
	AuditTargetTypeAPIKey               AuditTargetType = "api_key"
	AuditReasonServicePrincipalMutation AuditReason     = "SERVICE_PRINCIPAL_MUTATION"
)

const (
	APIKeyCreationRateLimit           int64 = 30
	APIKeyCreationRateWindow                = time.Minute
	APIKeyUsageMaxLag                       = 15 * time.Minute
	APIKeyStaleAfter                        = 90 * 24 * time.Hour
	APIKeyUnusualActiveCountThreshold int64 = 10
)

type APIKeyCreationLimiter interface {
	Acquire(context.Context, TenantScope, uuid.UUID) error
}

type APIKeyStatus string

const (
	APIKeyStatusActive  APIKeyStatus = "active"
	APIKeyStatusRevoked APIKeyStatus = "revoked"
	APIKeyStatusExpired APIKeyStatus = "expired"
)

type ServicePrincipal struct {
	ID             uuid.UUID
	Name           string
	NormalizedName string
	Status         PrincipalStatus
	MembershipID   uuid.UUID
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ServicePrincipalCreateMutation struct {
	Principal  ServicePrincipal
	Membership TenantMembership
	Bindings   []TenantRoleBinding
	Audit      SecurityAuditEvent
}

type APIKey struct {
	ID            uuid.UUID
	PrincipalID   uuid.UUID
	Status        APIKeyStatus
	DisplayPrefix string
	Digest        [sha256.Size]byte
	NeverExpires  bool
	ExpiresAt     time.Time
	CreatedAt     time.Time
	LastUsedAt    time.Time
	RevokedAt     time.Time
	Version       int64
}

func (k APIKey) EffectiveStatus(now time.Time) APIKeyStatus {
	if k.Status == APIKeyStatusActive && !k.NeverExpires && !k.ExpiresAt.After(now.UTC()) {
		return APIKeyStatusExpired
	}
	return k.Status
}

type APIKeyCreateMutation struct {
	APIKey APIKey
	Audit  SecurityAuditEvent
}

// APIKeyCredential is produced by an infrastructure adapter. Business logic
// receives only the one-time presentation value and the persistence-safe
// material needed for the new credential; it never implements randomness or
// hashing itself.
type APIKeyCredential struct {
	Raw           string
	DisplayPrefix string
	Digest        [sha256.Size]byte
}

type ServicePrincipalRecord struct {
	TenantID  uuid.UUID
	Principal ServicePrincipal
}

type ServicePrincipalPage struct {
	Items      []ServicePrincipalRecord
	NextCursor uuid.UUID
}

type APIKeyPage struct {
	Items      []APIKey
	NextCursor uuid.UUID
}

type APIKeyOperationalSignals struct {
	ActiveCount           int64
	StaleNonExpiringCount int64
	UnusualActiveCount    bool
	ObservedAt            time.Time
}

type APIKeyOperationalReader interface {
	GetAPIKeyOperationalSignals(context.Context, TenantScope, uuid.UUID, time.Time) (APIKeyOperationalSignals, error)
}

// APIKeyOperationalSnapshot contains only aggregate, credential-free values
// suitable for operator observability. Tenant and Principal labels are
// intentionally absent to avoid unbounded metric cardinality.
type APIKeyOperationalSnapshot struct {
	StaleNonExpiringCount        int64
	UnusualServicePrincipalCount int64
	ObservedAt                   time.Time
}

type APIKeyOperationalSnapshotReader interface {
	GetAPIKeyOperationalSnapshot(context.Context, time.Time) (APIKeyOperationalSnapshot, error)
}

type ServicePrincipalReader interface {
	GetServicePrincipal(context.Context, TenantScope, uuid.UUID) (ServicePrincipal, error)
	ListServicePrincipals(context.Context, TenantScope, PrincipalStatus, uuid.UUID, int32) (ServicePrincipalPage, error)
	ListAPIKeys(context.Context, TenantScope, uuid.UUID, uuid.UUID, int32) (APIKeyPage, error)
}

type TenantAdminReader interface {
	TenantAuthorizationReader
	ServicePrincipalReader
}

type ServicePrincipalTransaction interface {
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	GetServicePrincipal(context.Context, TenantScope, uuid.UUID) (ServicePrincipal, error)
	IssueAPIKeyCredential(context.Context, uuid.UUID) (APIKeyCredential, error)
	CreateServicePrincipal(context.Context, TenantScope, ServicePrincipalCreateMutation) error
	CreateAPIKey(context.Context, TenantScope, APIKeyCreateMutation) error
	UpdateServicePrincipal(context.Context, TenantScope, ServicePrincipal, int64) (ServicePrincipal, int64, error)
	GetAPIKey(context.Context, TenantScope, uuid.UUID) (APIKey, error)
	RevokeAPIKey(context.Context, TenantScope, APIKey, int64) (APIKey, error)
	AppendAudit(context.Context, TenantScope, SecurityAuditEvent) error
}

type ServicePrincipalUnitOfWork interface {
	WithinServicePrincipal(context.Context, TenantScope, func(context.Context, ServicePrincipalTransaction) error) error
}

type CreateServicePrincipalCommand struct {
	Name    string
	RoleIDs []uuid.UUID
	Actor   TenantAuthorizationActor
}

type CreateServicePrincipalResult struct {
	Principal    ServicePrincipal
	Membership   TenantMembershipRecord
	AuditEventID uuid.UUID
}

type CreateAPIKeyCommand struct {
	PrincipalID    uuid.UUID
	NeverExpires   bool
	ExpiresAt      time.Time
	IdempotencyKey string
	Actor          TenantAuthorizationActor
}

type CreateAPIKeyResult struct {
	APIKey       APIKey
	Secret       string
	AuditEventID uuid.UUID
}

type UpdateServicePrincipalCommand struct {
	PrincipalID     uuid.UUID
	Name            string
	Status          PrincipalStatus
	ExpectedVersion int64
	IdempotencyKey  string
	Actor           TenantAuthorizationActor
}

type UpdateServicePrincipalResult struct {
	Principal      ServicePrincipal
	RevokedAPIKeys int64
	AuditEventID   uuid.UUID
}

type RevokeAPIKeyCommand struct {
	KeyID          uuid.UUID
	IdempotencyKey string
	Actor          TenantAuthorizationActor
}

type RevokeAPIKeyResult struct {
	APIKey       APIKey
	AuditEventID uuid.UUID
}

type ServicePrincipalUsecase struct {
	uow     ServicePrincipalUnitOfWork
	limiter APIKeyCreationLimiter
	ids     IDGenerator
	clock   Clock
}

func NewServicePrincipalUsecase(
	uow ServicePrincipalUnitOfWork,
	ids IDGenerator,
	clock Clock,
	limiter APIKeyCreationLimiter,
) *ServicePrincipalUsecase {
	return &ServicePrincipalUsecase{uow: uow, limiter: limiter, ids: ids, clock: clock}
}

func (u *ServicePrincipalUsecase) CreateServicePrincipal(ctx context.Context, scope TenantScope, command CreateServicePrincipalCommand) (CreateServicePrincipalResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return CreateServicePrincipalResult{}, err
	}
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return CreateServicePrincipalResult{}, ErrServicePrincipalNameRequired
	}
	if len(command.RoleIDs) == 0 {
		return CreateServicePrincipalResult{}, ErrServicePrincipalRolesRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return CreateServicePrincipalResult{}, err
	}

	ids, err := u.newIDs(3 + len(command.RoleIDs))
	if err != nil {
		return CreateServicePrincipalResult{}, err
	}
	now := u.clock.Now().UTC()
	principal := ServicePrincipal{
		ID: ids[0], Name: name, NormalizedName: strings.ToLower(name), Status: PrincipalStatusActive,
		MembershipID: ids[1], Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	membership := TenantMembership{
		ID: ids[1], PrincipalID: principal.ID, Status: MembershipStatusActive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	bindings := make([]TenantRoleBinding, len(command.RoleIDs))
	seenRoles := make(map[uuid.UUID]struct{}, len(command.RoleIDs))
	for index, roleID := range command.RoleIDs {
		if roleID == uuid.Nil {
			return CreateServicePrincipalResult{}, ErrRoleNotFound
		}
		if _, exists := seenRoles[roleID]; exists {
			return CreateServicePrincipalResult{}, ErrRoleBindingConflict
		}
		seenRoles[roleID] = struct{}{}
		bindings[index] = TenantRoleBinding{
			ID: ids[2+index], MembershipID: membership.ID, RoleID: roleID,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	auditID := ids[len(ids)-1]
	audit := newTenantAuthorizationAudit(
		auditID, command.Actor, AuditActionServicePrincipalCreated,
		AuditTargetTypeServicePrincipal, principal.ID, principal.Version, now,
	)

	mutation := ServicePrincipalCreateMutation{Principal: principal, Membership: membership, Bindings: bindings, Audit: audit}
	err = u.uow.WithinServicePrincipal(ctx, scope, func(txContext context.Context, tx ServicePrincipalTransaction) error {
		for _, roleID := range command.RoleIDs {
			if _, err := tx.GetRole(txContext, scope, roleID); err != nil {
				return err
			}
		}
		return tx.CreateServicePrincipal(txContext, scope, mutation)
	})
	if err != nil {
		return CreateServicePrincipalResult{}, err
	}
	return CreateServicePrincipalResult{
		Principal:    principal,
		Membership:   TenantMembershipRecord{Membership: membership, PrincipalType: PrincipalTypeService, RoleIDs: append([]uuid.UUID(nil), command.RoleIDs...)},
		AuditEventID: auditID,
	}, nil
}

func (u *ServicePrincipalUsecase) CreateAPIKey(ctx context.Context, scope TenantScope, command CreateAPIKeyCommand) (CreateAPIKeyResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return CreateAPIKeyResult{}, err
	}
	if command.PrincipalID == uuid.Nil {
		return CreateAPIKeyResult{}, ErrServicePrincipalNotFound
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return CreateAPIKeyResult{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return CreateAPIKeyResult{}, err
	}
	now := u.clock.Now().UTC()
	if command.NeverExpires == !command.ExpiresAt.IsZero() || (!command.NeverExpires && !command.ExpiresAt.After(now)) {
		return CreateAPIKeyResult{}, ErrAPIKeyExpiryInvalid
	}
	if u.limiter == nil {
		return CreateAPIKeyResult{}, ErrAuthenticationDependency
	}
	if err := u.limiter.Acquire(ctx, scope, command.PrincipalID); err != nil {
		if errors.Is(err, ErrAuthenticationRateLimited) {
			return CreateAPIKeyResult{}, err
		}
		return CreateAPIKeyResult{}, fmt.Errorf("acquire API key creation limit: %w", errors.Join(ErrAuthenticationDependency, err))
	}
	var result CreateAPIKeyResult
	err := u.uow.WithinServicePrincipal(ctx, scope, func(txContext context.Context, tx ServicePrincipalTransaction) error {
		principal, err := tx.GetServicePrincipal(txContext, scope, command.PrincipalID)
		if err != nil {
			return err
		}
		if principal.Status != PrincipalStatusActive {
			return ErrServicePrincipalDisabled
		}
		ids, err := u.newIDs(2)
		if err != nil {
			return err
		}
		credential, err := tx.IssueAPIKeyCredential(txContext, ids[0])
		if err != nil {
			return fmt.Errorf("generate API key secret: %w", err)
		}
		if strings.TrimSpace(credential.Raw) == "" || strings.TrimSpace(credential.DisplayPrefix) == "" || credential.Digest == ([sha256.Size]byte{}) {
			return ErrAuthenticationDependency
		}
		apiKey := APIKey{
			ID: ids[0], PrincipalID: command.PrincipalID, Status: APIKeyStatusActive,
			DisplayPrefix: credential.DisplayPrefix, Digest: credential.Digest,
			NeverExpires: command.NeverExpires, ExpiresAt: command.ExpiresAt.UTC(), CreatedAt: now, Version: 1,
		}
		audit := newTenantAuthorizationAudit(
			ids[1], command.Actor, AuditActionAPIKeyCreated, AuditTargetTypeAPIKey,
			apiKey.ID, apiKey.Version, now,
		)
		if err := tx.CreateAPIKey(txContext, scope, APIKeyCreateMutation{APIKey: apiKey, Audit: audit}); err != nil {
			return err
		}
		result = CreateAPIKeyResult{APIKey: apiKey, Secret: credential.Raw, AuditEventID: ids[1]}
		return nil
	})
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	return result, nil
}

func (u *ServicePrincipalUsecase) UpdateServicePrincipal(ctx context.Context, scope TenantScope, command UpdateServicePrincipalCommand) (UpdateServicePrincipalResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return UpdateServicePrincipalResult{}, err
	}
	name := strings.TrimSpace(command.Name)
	if command.PrincipalID == uuid.Nil {
		return UpdateServicePrincipalResult{}, ErrServicePrincipalNotFound
	}
	if name == "" {
		return UpdateServicePrincipalResult{}, ErrServicePrincipalNameRequired
	}
	if command.Status != PrincipalStatusActive && command.Status != PrincipalStatusDisabled {
		return UpdateServicePrincipalResult{}, ErrMembershipStatusInvalid
	}
	if command.ExpectedVersion <= 0 {
		return UpdateServicePrincipalResult{}, ErrExpectedVersionRequired
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return UpdateServicePrincipalResult{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return UpdateServicePrincipalResult{}, err
	}
	now := u.clock.Now().UTC()
	var result UpdateServicePrincipalResult
	err := u.uow.WithinServicePrincipal(ctx, scope, func(txContext context.Context, tx ServicePrincipalTransaction) error {
		current, err := tx.GetServicePrincipal(txContext, scope, command.PrincipalID)
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return ErrVersionConflict
		}
		current.Name = name
		current.NormalizedName = strings.ToLower(name)
		current.Status = command.Status
		current.UpdatedAt = now
		updated, revoked, err := tx.UpdateServicePrincipal(txContext, scope, current, command.ExpectedVersion)
		if err != nil {
			return err
		}
		auditID, err := u.newID()
		if err != nil {
			return err
		}
		action := AuditActionServicePrincipalUpdated
		if updated.Status == PrincipalStatusDisabled {
			action = AuditActionServicePrincipalDisabled
		}
		audit := newTenantAuthorizationAudit(auditID, command.Actor, action, AuditTargetTypeServicePrincipal, updated.ID, updated.Version, now)
		if err := tx.AppendAudit(txContext, scope, audit); err != nil {
			return err
		}
		result = UpdateServicePrincipalResult{Principal: updated, RevokedAPIKeys: revoked, AuditEventID: auditID}
		return nil
	})
	return result, err
}

func (u *ServicePrincipalUsecase) RevokeAPIKey(ctx context.Context, scope TenantScope, command RevokeAPIKeyCommand) (RevokeAPIKeyResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return RevokeAPIKeyResult{}, err
	}
	if command.KeyID == uuid.Nil {
		return RevokeAPIKeyResult{}, ErrAPIKeyNotFound
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return RevokeAPIKeyResult{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return RevokeAPIKeyResult{}, err
	}
	now := u.clock.Now().UTC()
	var result RevokeAPIKeyResult
	err := u.uow.WithinServicePrincipal(ctx, scope, func(txContext context.Context, tx ServicePrincipalTransaction) error {
		current, err := tx.GetAPIKey(txContext, scope, command.KeyID)
		if err != nil {
			return err
		}
		if current.Status == APIKeyStatusRevoked {
			result.APIKey = current
			return nil
		}
		current.Status = APIKeyStatusRevoked
		current.RevokedAt = now
		updated, err := tx.RevokeAPIKey(txContext, scope, current, current.Version)
		if err != nil {
			return err
		}
		auditID, err := u.newID()
		if err != nil {
			return err
		}
		audit := newTenantAuthorizationAudit(auditID, command.Actor, AuditActionAPIKeyRevoked, AuditTargetTypeAPIKey, updated.ID, updated.Version, now)
		if err := tx.AppendAudit(txContext, scope, audit); err != nil {
			return err
		}
		result = RevokeAPIKeyResult{APIKey: updated, AuditEventID: auditID}
		return nil
	})
	return result, err
}

func (u *ServicePrincipalUsecase) newIDs(count int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		id, err := u.ids.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate service principal identity: %w", err)
		}
		if id == uuid.Nil || id.Version() != 7 {
			return nil, ErrInvalidGeneratedID
		}
		ids[index] = id
	}
	return ids, nil
}

func (u *ServicePrincipalUsecase) newID() (uuid.UUID, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return uuid.Nil, err
	}
	return ids[0], nil
}
