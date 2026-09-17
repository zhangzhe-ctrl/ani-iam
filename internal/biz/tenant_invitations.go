package biz

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type InvitationStatus string

const (
	InvitationPending   InvitationStatus = "pending"
	InvitationAccepted  InvitationStatus = "accepted"
	InvitationCancelled InvitationStatus = "cancelled"
	InvitationExpired   InvitationStatus = "expired"
	invitationLifetime                   = 7 * 24 * time.Hour
)

var (
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrInvitationInvalid  = errors.New("invitation input is invalid")
	ErrInvitationConflict = errors.New("invitation intent conflicts with current state")
)

// Metadata is the complete public/ledger-safe outcome: no token or digest.
type TenantInvitation struct {
	ID                                        uuid.UUID
	NormalizedEmail                           string
	RoleIDs                                   []uuid.UUID
	Locale                                    string
	Status                                    InvitationStatus
	ExpiresAt                                 time.Time
	Version, DeliveryGeneration               int64
	DeliveryStatus                            string
	DeliveryAttemptCount                      int32
	CreatedBy                                 uuid.UUID
	CreatedAt, UpdatedAt                      time.Time
	AcceptedMembershipID, AcceptedPrincipalID uuid.UUID
}
type TenantInvitationState struct {
	Invitation  TenantInvitation
	TokenDigest [sha256.Size]byte
}
type TenantInvitationPage struct {
	Items      []TenantInvitation
	NextCursor string
}
type TenantInvitationTransaction interface {
	TenantRoleAdministrationTransaction
	MutationResultTransaction
	GetRole(context.Context, TenantScope, uuid.UUID) (TenantRole, error)
	GetInvitation(context.Context, TenantScope, uuid.UUID) (TenantInvitationState, error)
	FindPendingInvitation(context.Context, TenantScope, string) (TenantInvitationState, bool, error)
	ListInvitations(context.Context, TenantScope, InvitationStatus, uuid.UUID, int32, time.Time) ([]TenantInvitationState, error)
	ExpiredInvitationsForRole(context.Context, TenantScope, uuid.UUID, time.Time) ([]TenantInvitationState, error)
	CreateInvitation(context.Context, TenantScope, TenantInvitationState, uuid.UUID, string) error
	ResendInvitation(context.Context, TenantScope, TenantInvitationState, int64, uuid.UUID, string) error
	EndInvitation(context.Context, TenantScope, uuid.UUID, int64, InvitationStatus, time.Time) error
	AppendAudit(context.Context, TenantScope, SecurityAuditEvent) error
}
type TenantInvitationUnitOfWork interface {
	WithinTenantInvitations(context.Context, TenantScope, func(context.Context, TenantInvitationTransaction) error) error
}
type CreateTenantInvitationCommand struct {
	Email                  string
	RoleIDs                []uuid.UUID
	Locale, IdempotencyKey string
	Actor                  TenantAuthorizationActor
}
type ChangeTenantInvitationCommand struct {
	ID              uuid.UUID
	ExpectedVersion int64
	IdempotencyKey  string
	Actor           TenantAuthorizationActor
}
type TenantInvitationMutationResult struct {
	Invitation   TenantInvitation
	AuditEventID uuid.UUID
}
type TenantInvitationUsecase struct {
	uow     TenantInvitationUnitOfWork
	ids     IDGenerator
	secrets SecretGenerator
	clock   Clock
}

func NewTenantInvitationUsecase(u TenantInvitationUnitOfWork, ids IDGenerator, secrets SecretGenerator, c Clock) *TenantInvitationUsecase {
	return &TenantInvitationUsecase{u, ids, secrets, c}
}

func invitationRoleIDs(values []uuid.UUID) ([]uuid.UUID, error) {
	if len(values) < 1 || len(values) > 100 {
		return nil, ErrInvitationInvalid
	}
	result := slices.Clone(values)
	for _, id := range result {
		if id == uuid.Nil || id.Version() != 7 {
			return nil, ErrInvitationInvalid
		}
	}
	slices.SortFunc(result, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(result), nil
}

func normalizeInvitationEmail(value string) (string, error) {
	email, err := normalizeOIDCEmail(value)
	if err != nil || !utf8.ValidString(email) || len(email) > 320 {
		return "", ErrInvitationInvalid
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" {
		return "", ErrInvitationInvalid
	}
	return email, nil
}
func (u *TenantInvitationUsecase) newID() (uuid.UUID, error) {
	id, err := u.ids.NewID()
	if err != nil || id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrAuthenticationDependency
	}
	return id, nil
}
func (u *TenantInvitationUsecase) token(scope TenantScope, id uuid.UUID) (string, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return "", err
	}
	secret, err := u.secrets.NewSecret()
	if err != nil || len(secret) < 32 || len(secret) > 128 {
		return "", ErrAuthenticationDependency
	}
	for _, c := range secret {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", ErrAuthenticationDependency
		}
	}
	return "ani_inv_t." + tenant.String() + "." + id.String() + "." + secret, nil
}
func (u *TenantInvitationUsecase) roles(ctx context.Context, tx TenantInvitationTransaction, scope TenantScope, ids []uuid.UUID) error {
	for _, id := range ids {
		if _, err := tx.GetRole(ctx, scope, id); err != nil {
			return err
		}
	}
	return nil
}
func (u *TenantInvitationUsecase) audit(ctx context.Context, tx TenantInvitationTransaction, scope TenantScope, actor TenantAuthorizationActor, inv TenantInvitation, action AuditAction, now time.Time) (uuid.UUID, error) {
	id, err := u.newID()
	if err != nil {
		return uuid.Nil, err
	}
	event := newTenantAuthorizationAudit(id, actor, action, "tenant_invitation", inv.ID, inv.Version, now)
	event.Reason = "TENANT_INVITATION_TRANSITION"
	return id, tx.AppendAudit(ctx, scope, event)
}
func (u *TenantInvitationUsecase) expire(ctx context.Context, tx TenantInvitationTransaction, scope TenantScope, actor TenantAuthorizationActor, state TenantInvitationState, now time.Time) (TenantInvitationState, error) {
	inv := state.Invitation
	if inv.Status != InvitationPending || now.Before(inv.ExpiresAt) {
		return state, nil
	}
	if err := tx.EndInvitation(ctx, scope, inv.ID, inv.Version, InvitationExpired, now); err != nil {
		return TenantInvitationState{}, err
	}
	inv.Status = InvitationExpired
	inv.Version++
	inv.UpdatedAt = now
	if inv.DeliveryStatus != "delivered" {
		inv.DeliveryStatus = "cancelled"
	}
	state.Invitation = inv
	_, err := u.audit(ctx, tx, scope, actor, inv, "iam.invitation.expired", now)
	return state, err
}
func (u *TenantInvitationUsecase) mutate(ctx context.Context, scope TenantScope, actor TenantAuthorizationActor, operation, key string, fields any, fn func(context.Context, TenantInvitationTransaction, time.Time) (TenantInvitation, AuditAction, error)) (TenantInvitationMutationResult, error) {
	if u == nil || u.uow == nil || u.ids == nil || u.secrets == nil || u.clock == nil {
		return TenantInvitationMutationResult{}, ErrAuthenticationDependency
	}
	identity, err := mutationIdentity(actor, operation, key, fields)
	if err != nil {
		return TenantInvitationMutationResult{}, err
	}
	var result TenantInvitationMutationResult
	err = u.uow.WithinTenantInvitations(ctx, scope, func(ctx context.Context, tx TenantInvitationTransaction) error {
		if err := requireTenantRoleAdministrator(ctx, tx, scope, actor); err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		return executeMutation(ctx, tx, scope, identity, now, &result, func() error {
			inv, action, err := fn(ctx, tx, now)
			if err != nil {
				return err
			}
			audit, err := u.audit(ctx, tx, scope, actor, inv, action, now)
			if err != nil {
				return err
			}
			result = TenantInvitationMutationResult{inv, audit}
			return nil
		})
	})
	return result, err
}
func (u *TenantInvitationUsecase) Create(ctx context.Context, scope TenantScope, c CreateTenantInvitationCommand) (TenantInvitationMutationResult, error) {
	email, err := normalizeInvitationEmail(c.Email)
	if err != nil {
		return TenantInvitationMutationResult{}, ErrInvitationInvalid
	}
	roles, err := invitationRoleIDs(c.RoleIDs)
	if err != nil {
		return TenantInvitationMutationResult{}, err
	}
	locale := c.Locale
	if locale == "" {
		locale = "en-US"
	}
	if locale != "en-US" && locale != "zh-CN" {
		return TenantInvitationMutationResult{}, ErrInvitationInvalid
	}
	fields := struct {
		Email  string
		Roles  []uuid.UUID
		Locale string
	}{email, roles, locale}
	return u.mutate(ctx, scope, c.Actor, "createTenantIAMInvitation", c.IdempotencyKey, fields, func(ctx context.Context, tx TenantInvitationTransaction, now time.Time) (TenantInvitation, AuditAction, error) {
		old, found, err := tx.FindPendingInvitation(ctx, scope, email)
		if err != nil {
			return TenantInvitation{}, "", err
		}
		if found {
			old, err = u.expire(ctx, tx, scope, c.Actor, old, now)
			if err != nil {
				return TenantInvitation{}, "", err
			}
			if old.Invitation.Status == InvitationPending {
				if !slices.Equal(old.Invitation.RoleIDs, roles) {
					return TenantInvitation{}, "", ErrInvitationConflict
				}
				return old.Invitation, "iam.invitation.create.reused", nil
			}
		}
		if err = u.roles(ctx, tx, scope, roles); err != nil {
			return TenantInvitation{}, "", err
		}
		id, err := u.newID()
		if err != nil {
			return TenantInvitation{}, "", err
		}
		delivery, err := u.newID()
		if err != nil || delivery == id {
			return TenantInvitation{}, "", ErrAuthenticationDependency
		}
		token, err := u.token(scope, id)
		if err != nil {
			return TenantInvitation{}, "", err
		}
		inv := TenantInvitation{ID: id, NormalizedEmail: email, RoleIDs: roles, Locale: locale, Status: InvitationPending, ExpiresAt: now.Add(invitationLifetime), Version: 1, DeliveryGeneration: 1, DeliveryStatus: "pending", CreatedBy: c.Actor.PrincipalID, CreatedAt: now, UpdatedAt: now}
		err = tx.CreateInvitation(ctx, scope, TenantInvitationState{inv, sha256.Sum256([]byte(token))}, delivery, token)
		return inv, "iam.invitation.created", err
	})
}
func (u *TenantInvitationUsecase) Resend(ctx context.Context, scope TenantScope, c ChangeTenantInvitationCommand) (TenantInvitationMutationResult, error) {
	if c.ID == uuid.Nil || c.ExpectedVersion < 1 {
		return TenantInvitationMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, scope, c.Actor, "resendTenantIAMInvitation", c.IdempotencyKey, struct {
		ID      uuid.UUID
		Version int64
	}{c.ID, c.ExpectedVersion}, func(ctx context.Context, tx TenantInvitationTransaction, now time.Time) (TenantInvitation, AuditAction, error) {
		old, err := tx.GetInvitation(ctx, scope, c.ID)
		if err != nil {
			return TenantInvitation{}, "", err
		}
		inv := old.Invitation
		if inv.Version != c.ExpectedVersion {
			return TenantInvitation{}, "", ErrVersionConflict
		}
		if inv.Status != InvitationPending && inv.Status != InvitationExpired {
			return TenantInvitation{}, "", ErrInvitationConflict
		}
		if err = u.roles(ctx, tx, scope, inv.RoleIDs); err != nil {
			return TenantInvitation{}, "", err
		}
		delivery, err := u.newID()
		if err != nil {
			return TenantInvitation{}, "", err
		}
		token, err := u.token(scope, inv.ID)
		if err != nil {
			return TenantInvitation{}, "", err
		}
		digest := sha256.Sum256([]byte(token))
		if digest == old.TokenDigest {
			return TenantInvitation{}, "", ErrAuthenticationDependency
		}
		inv.Status = InvitationPending
		inv.Version++
		inv.DeliveryGeneration++
		inv.ExpiresAt = now.Add(invitationLifetime)
		inv.UpdatedAt = now
		inv.DeliveryStatus = "pending"
		inv.DeliveryAttemptCount = 0
		err = tx.ResendInvitation(ctx, scope, TenantInvitationState{inv, digest}, c.ExpectedVersion, delivery, token)
		return inv, "iam.invitation.resent", err
	})
}
func (u *TenantInvitationUsecase) Cancel(ctx context.Context, scope TenantScope, c ChangeTenantInvitationCommand) (TenantInvitationMutationResult, error) {
	if c.ID == uuid.Nil || c.ExpectedVersion < 1 {
		return TenantInvitationMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, scope, c.Actor, "cancelTenantIAMInvitation", c.IdempotencyKey, struct {
		ID      uuid.UUID
		Version int64
	}{c.ID, c.ExpectedVersion}, func(ctx context.Context, tx TenantInvitationTransaction, now time.Time) (TenantInvitation, AuditAction, error) {
		old, err := tx.GetInvitation(ctx, scope, c.ID)
		if err != nil {
			return TenantInvitation{}, "", err
		}
		inv := old.Invitation
		if inv.Version != c.ExpectedVersion {
			return TenantInvitation{}, "", ErrVersionConflict
		}
		if inv.Status != InvitationPending || !now.Before(inv.ExpiresAt) {
			return TenantInvitation{}, "", ErrInvitationConflict
		}
		if err = tx.EndInvitation(ctx, scope, inv.ID, inv.Version, InvitationCancelled, now); err != nil {
			return TenantInvitation{}, "", err
		}
		inv.Status = InvitationCancelled
		inv.Version++
		inv.UpdatedAt = now
		if inv.DeliveryStatus != "delivered" {
			inv.DeliveryStatus = "cancelled"
		}
		return inv, "iam.invitation.cancelled", nil
	})
}
func (u *TenantInvitationUsecase) Get(ctx context.Context, scope TenantScope, actor TenantAuthorizationActor, id uuid.UUID) (TenantInvitation, error) {
	if err := validateTenantActor(actor); err != nil {
		return TenantInvitation{}, err
	}
	var result TenantInvitation
	err := u.uow.WithinTenantInvitations(ctx, scope, func(ctx context.Context, tx TenantInvitationTransaction) error {
		if err := requireTenantRoleAdministrator(ctx, tx, scope, actor); err != nil {
			return err
		}
		state, err := tx.GetInvitation(ctx, scope, id)
		if err != nil {
			return err
		}
		state, err = u.expire(ctx, tx, scope, actor, state, u.clock.Now().UTC())
		result = state.Invitation
		return err
	})
	return result, err
}
func (u *TenantInvitationUsecase) ExpireForRole(ctx context.Context, scope TenantScope, actor TenantAuthorizationActor, roleID uuid.UUID) error {
	if err := validateTenantActor(actor); err != nil {
		return err
	}
	return u.uow.WithinTenantInvitations(ctx, scope, func(ctx context.Context, tx TenantInvitationTransaction) error {
		if err := requireTenantRoleAdministrator(ctx, tx, scope, actor); err != nil {
			return err
		}
		now := u.clock.Now().UTC()
		rows, err := tx.ExpiredInvitationsForRole(ctx, scope, roleID, now)
		if err != nil {
			return err
		}
		for _, state := range rows {
			if _, err = u.expire(ctx, tx, scope, actor, state, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// The cursor carries no authority. Every page independently checks current
// Tenant membership and administrator status under the transaction guard.
type tenantInvitationCursor struct {
	Tenant, Actor, After uuid.UUID
	Status               InvitationStatus
}

func (u *TenantInvitationUsecase) List(ctx context.Context, scope TenantScope, actor TenantAuthorizationActor, status InvitationStatus, cursor string, limit uint32) (TenantInvitationPage, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return TenantInvitationPage{}, err
	}
	if err = validateTenantActor(actor); err != nil {
		return TenantInvitationPage{}, err
	}
	if limit > 100 || len(cursor) > 2048 || (status != "" && status != InvitationPending && status != InvitationAccepted && status != InvitationCancelled && status != InvitationExpired) {
		return TenantInvitationPage{}, ErrInvitationInvalid
	}
	if limit == 0 {
		limit = 50
	}
	c := tenantInvitationCursor{Tenant: tenant, Actor: actor.PrincipalID, Status: status}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return TenantInvitationPage{}, ErrInvitationInvalid
		}
		if json.Unmarshal(raw, &c) != nil || c.Tenant != tenant || c.Actor != actor.PrincipalID || c.Status != status || c.After == uuid.Nil || c.After.Version() != 7 {
			return TenantInvitationPage{}, ErrInvitationInvalid
		}
	}
	result := TenantInvitationPage{Items: []TenantInvitation{}}
	err = u.uow.WithinTenantInvitations(ctx, scope, func(ctx context.Context, tx TenantInvitationTransaction) error {
		if e := requireTenantRoleAdministrator(ctx, tx, scope, actor); e != nil {
			return e
		}
		now := u.clock.Now().UTC()
		rows, e := tx.ListInvitations(ctx, scope, status, c.After, int32(limit)+1, now)
		if e != nil {
			return e
		}
		more := len(rows) > int(limit)
		if more {
			rows = rows[:limit]
		}
		for _, row := range rows {
			state, e := u.expire(ctx, tx, scope, actor, row, now)
			if e != nil {
				return e
			}
			result.Items = append(result.Items, state.Invitation)
		}
		if more {
			c.After = result.Items[len(result.Items)-1].ID
			raw, _ := json.Marshal(c)
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return nil
	})
	return result, err
}
