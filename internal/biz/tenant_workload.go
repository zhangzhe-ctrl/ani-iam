package biz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTenantWorkloadNameRequired  = errors.New("tenant workload name is required")
	ErrTenantWorkloadRolesRequired = errors.New("tenant workload requires at least one role")
	ErrTenantWorkloadNotFound      = errors.New("tenant workload not found")
	ErrTenantWorkloadConflict      = errors.New("tenant workload conflicts with existing state")
	ErrTenantWorkloadDisabled      = errors.New("tenant workload is disabled")
	ErrAPIKeyNotFound              = errors.New("API key not found")
	ErrAPIKeyConflict              = errors.New("API key conflicts with existing state")
	ErrAPIKeyExpiryInvalid         = errors.New("API key expiry mode is invalid")
)

const (
	AuditActionTenantWorkloadCreated  AuditAction     = "iam.tenant-workload.created"
	AuditActionTenantWorkloadUpdated  AuditAction     = "iam.tenant-workload.updated"
	AuditActionTenantWorkloadDisabled AuditAction     = "iam.tenant-workload.disabled"
	AuditActionAPIKeyCreated          AuditAction     = "iam.api-key.created"
	AuditActionAPIKeyRevoked          AuditAction     = "iam.api-key.revoked"
	AuditTargetTypeTenantWorkload     AuditTargetType = "tenant_workload"
	AuditTargetTypeAPIKey             AuditTargetType = "api_key"
	AuditReasonTenantWorkloadMutation AuditReason     = "TENANT_WORKLOAD_MUTATION"
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

type TenantWorkload struct {
	ID             uuid.UUID
	Name           string
	NormalizedName string
	Status         PrincipalStatus
	MembershipID   uuid.UUID
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TenantWorkloadCreateMutation struct {
	Principal  TenantWorkload
	Membership TenantMembership
	Bindings   []TenantRoleBinding
	Audit      SecurityAuditEvent
}

type APIKey struct {
	ID            uuid.UUID
	PrincipalID   uuid.UUID
	Status        APIKeyStatus
	DisplayPrefix string
	Digest        [sha256.Size]byte `json:"-"`
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
	Digest        [sha256.Size]byte `json:"-"`
}

type TenantWorkloadRecord struct {
	TenantID  uuid.UUID
	Principal TenantWorkload
}

type TenantWorkloadPage struct {
	Items      []TenantWorkloadRecord
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
	StaleNonExpiringCount      int64
	UnusualTenantWorkloadCount int64
	ObservedAt                 time.Time
}

type APIKeyOperationalSnapshotReader interface {
	GetAPIKeyOperationalSnapshot(context.Context, time.Time) (APIKeyOperationalSnapshot, error)
}

type TenantWorkloadReader interface {
	GetTenantWorkload(context.Context, TenantScope, uuid.UUID) (TenantWorkload, error)
	ListTenantWorkloads(context.Context, TenantScope, PrincipalStatus, uuid.UUID, int32) (TenantWorkloadPage, error)
	ListAPIKeys(context.Context, TenantScope, uuid.UUID, uuid.UUID, int32) (APIKeyPage, error)
}

type TenantAdminReader interface {
	TenantAuthorizationReader
	TenantWorkloadReader
}

type TenantWorkloadTransaction interface {
	MutationResultTransaction
	CreateCurrentWorkloadMembership(context.Context, TenantScope, TenantMembership) error
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	GetTenantWorkload(context.Context, TenantScope, uuid.UUID) (TenantWorkload, error)
	IssueAPIKeyCredential(context.Context, uuid.UUID) (APIKeyCredential, error)
	CreateTenantWorkload(context.Context, TenantScope, TenantWorkloadCreateMutation) error
	CreateAPIKey(context.Context, TenantScope, APIKeyCreateMutation) error
	UpdateTenantWorkload(context.Context, TenantScope, TenantWorkload, int64) (TenantWorkload, int64, error)
	GetAPIKey(context.Context, TenantScope, uuid.UUID) (APIKey, error)
	RevokeAPIKey(context.Context, TenantScope, APIKey, int64) (APIKey, error)
	AppendAudit(context.Context, TenantScope, SecurityAuditEvent) error
}

type TenantWorkloadUnitOfWork interface {
	WithinTenantWorkload(context.Context, TenantScope, func(context.Context, TenantWorkloadTransaction) error) error
}

type CreateTenantWorkloadCommand struct {
	IdempotencyKey string
	Name           string
	RoleIDs        []uuid.UUID
	Actor          TenantAuthorizationActor
}

type CreateTenantWorkloadResult struct {
	Principal    TenantWorkload
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
	Replayed     bool
	APIKey       APIKey
	Secret       string `json:"-"`
	AuditEventID uuid.UUID
}

type UpdateTenantWorkloadCommand struct {
	PrincipalID     uuid.UUID
	Status          PrincipalStatus
	ExpectedVersion int64
	IdempotencyKey  string
	Actor           TenantAuthorizationActor
}

type UpdateTenantWorkloadResult struct {
	Principal      TenantWorkload
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

type TenantWorkloadUsecase struct {
	uow     TenantWorkloadUnitOfWork
	limiter APIKeyCreationLimiter
	ids     IDGenerator
	clock   Clock
}

func NewTenantWorkloadUsecase(
	uow TenantWorkloadUnitOfWork,
	ids IDGenerator,
	clock Clock,
	limiter APIKeyCreationLimiter,
) *TenantWorkloadUsecase {
	return &TenantWorkloadUsecase{uow: uow, limiter: limiter, ids: ids, clock: clock}
}

func (u *TenantWorkloadUsecase) CreateTenantWorkload(ctx context.Context, scope TenantScope, command CreateTenantWorkloadCommand) (CreateTenantWorkloadResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return CreateTenantWorkloadResult{}, err
	}
	name := strings.ToLower(strings.TrimSpace(command.Name))
	if name == "" {
		return CreateTenantWorkloadResult{}, ErrTenantWorkloadNameRequired
	}
	if len(command.RoleIDs) == 0 {
		return CreateTenantWorkloadResult{}, ErrTenantWorkloadRolesRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return CreateTenantWorkloadResult{}, err
	}

	roles := append([]uuid.UUID(nil), command.RoleIDs...)
	sort.Slice(roles, func(i, j int) bool { return roles[i].String() < roles[j].String() })
	identity, err := mutationIdentity(command.Actor, "createTenantWorkload", command.IdempotencyKey, struct {
		Name  string
		Roles []uuid.UUID
	}{name, roles})
	if err != nil {
		return CreateTenantWorkloadResult{}, err
	}
	now := u.clock.Now().UTC()
	var result CreateTenantWorkloadResult
	err = u.uow.WithinTenantWorkload(ctx, scope, func(txContext context.Context, tx TenantWorkloadTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			ids, err := u.newIDs(3 + len(command.RoleIDs))
			if err != nil {
				return err
			}
			principal := TenantWorkload{
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
					return ErrRoleNotFound
				}
				if _, exists := seenRoles[roleID]; exists {
					return ErrRoleBindingConflict
				}
				seenRoles[roleID] = struct{}{}
				bindings[index] = TenantRoleBinding{
					ID: ids[2+index], MembershipID: membership.ID, RoleID: roleID,
					Version: 1, CreatedAt: now, UpdatedAt: now,
				}
			}
			auditID := ids[len(ids)-1]
			audit := newTenantAuthorizationAudit(
				auditID, command.Actor, AuditActionTenantWorkloadCreated,
				AuditTargetTypeTenantWorkload, principal.ID, principal.Version, now,
			)

			mutation := TenantWorkloadCreateMutation{Principal: principal, Membership: membership, Bindings: bindings, Audit: audit}
			for _, roleID := range command.RoleIDs {
				if _, err := tx.GetRole(txContext, scope, roleID); err != nil {
					return err
				}
			}
			if err := tx.CreateTenantWorkload(txContext, scope, mutation); err != nil {
				return err
			}
			result = CreateTenantWorkloadResult{Principal: principal,
				Membership:   TenantMembershipRecord{Membership: membership, PrincipalType: PrincipalTypeWorkload, RoleIDs: append([]uuid.UUID(nil), command.RoleIDs...)},
				AuditEventID: auditID}
			return nil
		})
	})
	if err != nil {
		return CreateTenantWorkloadResult{}, err
	}
	return result, nil
}

func (u *TenantWorkloadUsecase) CreateAPIKey(ctx context.Context, scope TenantScope, command CreateAPIKeyCommand) (CreateAPIKeyResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return CreateAPIKeyResult{}, err
	}
	if command.PrincipalID == uuid.Nil {
		return CreateAPIKeyResult{}, ErrTenantWorkloadNotFound
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return CreateAPIKeyResult{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return CreateAPIKeyResult{}, err
	}
	identity, err := mutationIdentity(command.Actor, "createIAMAPIKey", command.IdempotencyKey, struct {
		Principal uuid.UUID
		Never     bool
		Expires   time.Time
	}{command.PrincipalID, command.NeverExpires, command.ExpiresAt.UTC()})
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	now := u.clock.Now().UTC()
	var result CreateAPIKeyResult
	err = u.uow.WithinTenantWorkload(ctx, scope, func(txContext context.Context, tx TenantWorkloadTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			if command.NeverExpires == !command.ExpiresAt.IsZero() || (!command.NeverExpires && !command.ExpiresAt.After(now)) {
				return ErrAPIKeyExpiryInvalid
			}

			if u.limiter == nil {
				return ErrAuthenticationDependency
			}
			if err := u.limiter.Acquire(ctx, scope, command.PrincipalID); err != nil {
				if errors.Is(err, ErrAuthenticationRateLimited) {
					return err
				}
				return fmt.Errorf("acquire API key creation limit: %w", errors.Join(ErrAuthenticationDependency, err))
			}
			principal, err := tx.GetTenantWorkload(txContext, scope, command.PrincipalID)
			if err != nil {
				return err
			}
			if principal.Status != PrincipalStatusActive {
				return ErrTenantWorkloadDisabled
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
	})
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	return result, nil
}

func (u *TenantWorkloadUsecase) UpdateTenantWorkload(ctx context.Context, scope TenantScope, command UpdateTenantWorkloadCommand) (UpdateTenantWorkloadResult, error) {
	if _, err := scope.TenantID(); err != nil {
		return UpdateTenantWorkloadResult{}, err
	}
	if command.PrincipalID == uuid.Nil {
		return UpdateTenantWorkloadResult{}, ErrTenantWorkloadNotFound
	}
	if command.Status != PrincipalStatusActive && command.Status != PrincipalStatusDisabled {
		return UpdateTenantWorkloadResult{}, ErrMembershipStatusInvalid
	}
	if command.ExpectedVersion <= 0 {
		return UpdateTenantWorkloadResult{}, ErrExpectedVersionRequired
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return UpdateTenantWorkloadResult{}, ErrIdempotencyKeyRequired
	}
	if err := validateTenantActor(command.Actor); err != nil {
		return UpdateTenantWorkloadResult{}, err
	}
	identity, err := mutationIdentity(command.Actor, "updateTenantWorkload", command.IdempotencyKey, struct {
		Principal uuid.UUID
		Status    PrincipalStatus
		Version   int64
	}{command.PrincipalID, command.Status, command.ExpectedVersion})
	if err != nil {
		return UpdateTenantWorkloadResult{}, err
	}
	now := u.clock.Now().UTC()
	var result UpdateTenantWorkloadResult
	err = u.uow.WithinTenantWorkload(ctx, scope, func(txContext context.Context, tx TenantWorkloadTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
			current, err := tx.GetTenantWorkload(txContext, scope, command.PrincipalID)
			if err != nil {
				return err
			}
			if current.Version != command.ExpectedVersion {
				return ErrVersionConflict
			}
			if command.Status == PrincipalStatusActive && current.MembershipID == uuid.Nil {
				membershipID, err := u.newID()
				if err != nil {
					return err
				}
				membership := TenantMembership{ID: membershipID, PrincipalID: current.ID,
					Status: MembershipStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
				if err := tx.CreateCurrentWorkloadMembership(txContext, scope, membership); err != nil {
					return err
				}
				current.MembershipID = membershipID
			}
			current.Status = command.Status
			current.UpdatedAt = now
			updated, revoked, err := tx.UpdateTenantWorkload(txContext, scope, current, command.ExpectedVersion)
			if err != nil {
				return err
			}
			auditID, err := u.newID()
			if err != nil {
				return err
			}
			action := AuditActionTenantWorkloadUpdated
			if updated.Status == PrincipalStatusDisabled {
				action = AuditActionTenantWorkloadDisabled
			}
			audit := newTenantAuthorizationAudit(auditID, command.Actor, action, AuditTargetTypeTenantWorkload, updated.ID, updated.Version, now)
			if err := tx.AppendAudit(txContext, scope, audit); err != nil {
				return err
			}
			result = UpdateTenantWorkloadResult{Principal: updated, RevokedAPIKeys: revoked, AuditEventID: auditID}
			return nil
		})
	})
	return result, err
}

func (u *TenantWorkloadUsecase) RevokeAPIKey(ctx context.Context, scope TenantScope, command RevokeAPIKeyCommand) (RevokeAPIKeyResult, error) {
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
	identity, err := mutationIdentity(command.Actor, "revokeIAMAPIKey", command.IdempotencyKey, command.KeyID)
	if err != nil {
		return RevokeAPIKeyResult{}, err
	}
	now := u.clock.Now().UTC()
	var result RevokeAPIKeyResult
	err = u.uow.WithinTenantWorkload(ctx, scope, func(txContext context.Context, tx TenantWorkloadTransaction) error {
		return executeMutation(txContext, tx, scope, identity, now, &result, func() error {
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
	})
	return result, err
}

func (u *TenantWorkloadUsecase) newIDs(count int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, count)
	for index := range ids {
		id, err := u.ids.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate tenant workload identity: %w", err)
		}
		if id == uuid.Nil || id.Version() != 7 {
			return nil, ErrInvalidGeneratedID
		}
		ids[index] = id
	}
	return ids, nil
}

func (u *TenantWorkloadUsecase) newID() (uuid.UUID, error) {
	ids, err := u.newIDs(1)
	if err != nil {
		return uuid.Nil, err
	}
	return ids[0], nil
}
