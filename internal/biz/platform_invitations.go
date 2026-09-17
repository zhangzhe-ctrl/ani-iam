package biz

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"slices"
	"time"
)

// Platform and Tenant share metadata shape, never an authority or repository.
// This distinct type has no tenant identifier; its repository requires cap.
type PlatformInvitation TenantInvitation
type PlatformInvitationState struct {
	Invitation  PlatformInvitation
	TokenDigest [sha256.Size]byte
}
type PlatformInvitationPage struct {
	Items      []PlatformInvitation
	NextCursor string
}
type PlatformInvitationRepository interface {
	GetInvitation(context.Context, PlatformCapability, uuid.UUID) (PlatformInvitationState, error)
	FindPendingInvitation(context.Context, PlatformCapability, string) (PlatformInvitationState, bool, error)
	ListInvitations(context.Context, PlatformCapability, InvitationStatus, uuid.UUID, int32, time.Time) ([]PlatformInvitationState, error)
	ExpiredInvitationsForRole(context.Context, PlatformCapability, uuid.UUID, time.Time) ([]PlatformInvitationState, error)
	CreateInvitation(context.Context, PlatformCapability, PlatformInvitationState, uuid.UUID, string) error
	ResendInvitation(context.Context, PlatformCapability, PlatformInvitationState, int64, uuid.UUID, string) error
	EndInvitation(context.Context, PlatformCapability, uuid.UUID, int64, InvitationStatus, time.Time) error
}
type CreatePlatformInvitationCommand struct {
	Email                  string
	RoleIDs                []uuid.UUID
	Locale, IdempotencyKey string
}
type ChangePlatformInvitationCommand struct {
	ID              uuid.UUID
	ExpectedVersion int64
	IdempotencyKey  string
}

func (u *PlatformAdministrationUsecase) WithInvitationSecrets(s SecretGenerator) *PlatformAdministrationUsecase {
	u.invitationSecrets = s
	return u
}
func (u *PlatformAdministrationUsecase) newAdministrationID() (uuid.UUID, error) {
	if u == nil || u.ids == nil {
		return uuid.Nil, ErrAuthenticationDependency
	}
	id, err := u.ids.NewID()
	if err != nil || id == uuid.Nil || id.Version() != 7 {
		return uuid.Nil, ErrAuthenticationDependency
	}
	return id, nil
}
func (u *PlatformAdministrationUsecase) platformInvitationToken(id uuid.UUID) (string, error) {
	if u == nil || u.invitationSecrets == nil {
		return "", ErrAuthenticationDependency
	}
	secret, err := u.invitationSecrets.NewSecret()
	if err != nil || len(secret) < 32 || len(secret) > 128 {
		return "", ErrAuthenticationDependency
	}
	for _, c := range secret {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", ErrAuthenticationDependency
		}
	}
	return "ani_inv_p." + id.String() + "." + secret, nil
}
func (u *PlatformAdministrationUsecase) expirePlatformInvitation(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, state PlatformInvitationState, now time.Time) (PlatformInvitationState, error) {
	v := state.Invitation
	if v.Status != InvitationPending || now.Before(v.ExpiresAt) {
		return state, nil
	}
	if err := tx.EndInvitation(ctx, cap, v.ID, v.Version, InvitationExpired, now); err != nil {
		return PlatformInvitationState{}, err
	}
	v.Status = InvitationExpired
	v.Version++
	v.UpdatedAt = now
	if v.DeliveryStatus != "delivered" {
		v.DeliveryStatus = "cancelled"
	}
	state.Invitation = v
	id, err := u.newAdministrationID()
	if err != nil {
		return PlatformInvitationState{}, err
	}
	audit := newPlatformAdministrationAudit(cap, "platform_invitation", id, v.ID, v.Version, now)
	audit.Action = "iam.platform.invitation.expired"
	audit.Reason = "PLATFORM_INVITATION_EXPIRED"
	return state, tx.AppendAudit(ctx, cap, audit)
}
func (u *PlatformAdministrationUsecase) expirePlatformInvitationsForRole(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, role uuid.UUID, now time.Time) error {
	rows, err := tx.ExpiredInvitationsForRole(ctx, cap, role, now)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err = u.expirePlatformInvitation(ctx, tx, cap, row, now); err != nil {
			return err
		}
	}
	return nil
}
func platformInvitationResult(v PlatformInvitation) PlatformMutationResult {
	return PlatformMutationResult{Invitation: &v, TargetID: v.ID, TargetVersion: v.Version}
}
func (u *PlatformAdministrationUsecase) CreateInvitation(ctx context.Context, cap PlatformCapability, c CreatePlatformInvitationCommand) (PlatformMutationResult, error) {
	email, err := normalizeInvitationEmail(c.Email)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	roles, err := invitationRoleIDs(c.RoleIDs)
	if err != nil {
		return PlatformMutationResult{}, err
	}
	locale := c.Locale
	if locale == "" {
		locale = "en-US"
	}
	if locale != "en-US" && locale != "zh-CN" {
		return PlatformMutationResult{}, ErrInvitationInvalid
	}
	fields := struct {
		Email  string
		Roles  []uuid.UUID
		Locale string
	}{email, roles, locale}
	return u.mutate(ctx, cap, "createPlatformIAMInvitation", c.IdempotencyKey, fields, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		old, found, err := tx.FindPendingInvitation(ctx, cap, email)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		if found {
			old, err = u.expirePlatformInvitation(ctx, tx, cap, old, now)
			if err != nil {
				return PlatformMutationResult{}, err
			}
			if old.Invitation.Status == InvitationPending {
				if !slices.Equal(old.Invitation.RoleIDs, roles) {
					return PlatformMutationResult{}, ErrInvitationConflict
				}
				return platformInvitationResult(old.Invitation), nil
			}
		}
		for _, id := range roles {
			if _, err := tx.GetRole(ctx, cap, id); err != nil {
				return PlatformMutationResult{}, err
			}
		}
		id, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		delivery, err := u.newAdministrationID()
		if err != nil || delivery == id {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		token, err := u.platformInvitationToken(id)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		v := PlatformInvitation{ID: id, NormalizedEmail: email, RoleIDs: roles, Locale: locale, Status: InvitationPending, ExpiresAt: now.Add(invitationLifetime), Version: 1, DeliveryGeneration: 1, DeliveryStatus: "pending", CreatedBy: cap.claims.Subject, CreatedAt: now, UpdatedAt: now}
		return platformInvitationResult(v), tx.CreateInvitation(ctx, cap, PlatformInvitationState{v, sha256.Sum256([]byte(token))}, delivery, token)
	})
}
func (u *PlatformAdministrationUsecase) ResendInvitation(ctx context.Context, cap PlatformCapability, c ChangePlatformInvitationCommand) (PlatformMutationResult, error) {
	if c.ID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, cap, "resendPlatformIAMInvitation", c.IdempotencyKey, struct {
		ID      uuid.UUID
		Version int64
	}{c.ID, c.ExpectedVersion}, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		old, err := tx.GetInvitation(ctx, cap, c.ID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		v := old.Invitation
		if v.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		if v.Status != InvitationPending && v.Status != InvitationExpired {
			return PlatformMutationResult{}, ErrInvitationConflict
		}
		for _, id := range v.RoleIDs {
			if _, err := tx.GetRole(ctx, cap, id); err != nil {
				return PlatformMutationResult{}, err
			}
		}
		delivery, err := u.newAdministrationID()
		if err != nil {
			return PlatformMutationResult{}, err
		}
		token, err := u.platformInvitationToken(v.ID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		digest := sha256.Sum256([]byte(token))
		if digest == old.TokenDigest {
			return PlatformMutationResult{}, ErrAuthenticationDependency
		}
		v.Status = InvitationPending
		v.Version++
		v.DeliveryGeneration++
		v.DeliveryStatus = "pending"
		v.DeliveryAttemptCount = 0
		v.ExpiresAt = now.Add(invitationLifetime)
		v.UpdatedAt = now
		return platformInvitationResult(v), tx.ResendInvitation(ctx, cap, PlatformInvitationState{v, digest}, c.ExpectedVersion, delivery, token)
	})
}
func (u *PlatformAdministrationUsecase) CancelInvitation(ctx context.Context, cap PlatformCapability, c ChangePlatformInvitationCommand) (PlatformMutationResult, error) {
	if c.ID == uuid.Nil || c.ExpectedVersion < 1 {
		return PlatformMutationResult{}, ErrExpectedVersionRequired
	}
	return u.mutate(ctx, cap, "cancelPlatformIAMInvitation", c.IdempotencyKey, struct {
		ID      uuid.UUID
		Version int64
	}{c.ID, c.ExpectedVersion}, func(tx PlatformAdministrationTransaction, now time.Time) (PlatformMutationResult, error) {
		old, err := tx.GetInvitation(ctx, cap, c.ID)
		if err != nil {
			return PlatformMutationResult{}, err
		}
		v := old.Invitation
		if v.Version != c.ExpectedVersion {
			return PlatformMutationResult{}, ErrVersionConflict
		}
		if v.Status != InvitationPending || !now.Before(v.ExpiresAt) {
			return PlatformMutationResult{}, ErrInvitationConflict
		}
		if err = tx.EndInvitation(ctx, cap, v.ID, v.Version, InvitationCancelled, now); err != nil {
			return PlatformMutationResult{}, err
		}
		v.Status = InvitationCancelled
		v.Version++
		v.UpdatedAt = now
		if v.DeliveryStatus != "delivered" {
			v.DeliveryStatus = "cancelled"
		}
		return platformInvitationResult(v), nil
	})
}
func (u *PlatformAdministrationUsecase) platformInvitationReadAudit(ctx context.Context, tx PlatformAdministrationTransaction, cap PlatformCapability, v PlatformInvitation, now time.Time) error {
	id, err := u.newAdministrationID()
	if err != nil {
		return err
	}
	target, version := v.ID, v.Version
	if target == uuid.Nil {
		target = id
		version = 1
	}
	return tx.AppendAudit(ctx, cap, newPlatformAdministrationAudit(cap, "iam.platform-invitations", id, target, version, now))
}
func (u *PlatformAdministrationUsecase) GetInvitation(ctx context.Context, cap PlatformCapability, id uuid.UUID) (PlatformInvitation, error) {
	if id == uuid.Nil {
		return PlatformInvitation{}, ErrInvitationInvalid
	}
	var result PlatformInvitation
	err := u.withinAuthorized(ctx, cap, "getPlatformIAMInvitation", func(tx PlatformAdministrationTransaction, now time.Time) error {
		row, err := tx.GetInvitation(ctx, cap, id)
		if err != nil {
			return err
		}
		row, err = u.expirePlatformInvitation(ctx, tx, cap, row, now)
		if err != nil {
			return err
		}
		result = row.Invitation
		return u.platformInvitationReadAudit(ctx, tx, cap, result, now)
	})
	return result, err
}

type platformInvitationCursor struct {
	Actor, After uuid.UUID
	Status       InvitationStatus
	Revision     string
}

func (u *PlatformAdministrationUsecase) ListInvitations(ctx context.Context, cap PlatformCapability, status InvitationStatus, cursor string, limit uint32) (PlatformInvitationPage, error) {
	claims, _, err := cap.CredentialBinding()
	if err != nil {
		return PlatformInvitationPage{}, err
	}
	if limit > 100 || len(cursor) > 2048 || (status != "" && status != InvitationPending && status != InvitationAccepted && status != InvitationCancelled && status != InvitationExpired) {
		return PlatformInvitationPage{}, ErrInvitationInvalid
	}
	if limit == 0 {
		limit = 50
	}
	c := platformInvitationCursor{Actor: claims.Subject, Status: status, Revision: cap.revision}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &c) != nil || c.Actor != claims.Subject || c.Status != status || c.Revision != cap.revision || c.After == uuid.Nil || c.After.Version() != 7 {
			return PlatformInvitationPage{}, ErrInvitationInvalid
		}
	}
	result := PlatformInvitationPage{Items: []PlatformInvitation{}}
	err = u.withinAuthorized(ctx, cap, "listPlatformIAMInvitations", func(tx PlatformAdministrationTransaction, now time.Time) error {
		rows, e := tx.ListInvitations(ctx, cap, status, c.After, int32(limit)+1, now)
		if e != nil {
			return e
		}
		more := len(rows) > int(limit)
		if more {
			rows = rows[:limit]
		}
		for _, row := range rows {
			row, e = u.expirePlatformInvitation(ctx, tx, cap, row, now)
			if e != nil {
				return e
			}
			result.Items = append(result.Items, row.Invitation)
		}
		if more {
			c.After = result.Items[len(result.Items)-1].ID
			raw, _ := json.Marshal(c)
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return u.platformInvitationReadAudit(ctx, tx, cap, PlatformInvitation{}, now)
	})
	return result, err
}
