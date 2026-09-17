package data

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

func (t *platformAdministrationTransaction) GetInvitation(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.PlatformInvitationState, error) {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return biz.PlatformInvitationState{}, err
	}
	row, err := t.q.GetPlatformInvitation(ctx, sqlcgen.GetPlatformInvitationParams{ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformInvitationState{}, biz.ErrInvitationNotFound
	}
	if err != nil {
		return biz.PlatformInvitationState{}, mapPostgresError("get platform invitation", err, nil)
	}
	return t.invitation(ctx, row)
}
func (t *platformAdministrationTransaction) FindPendingInvitation(ctx context.Context, cap biz.PlatformCapability, email string) (biz.PlatformInvitationState, bool, error) {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return biz.PlatformInvitationState{}, false, err
	}
	row, err := t.q.FindPendingPlatformInvitation(ctx, sqlcgen.FindPendingPlatformInvitationParams{NormalizedEmail: email})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformInvitationState{}, false, nil
	}
	if err != nil {
		return biz.PlatformInvitationState{}, false, mapPostgresError("find pending platform invitation", err, nil)
	}
	state, err := t.invitation(ctx, row)
	return state, err == nil, err
}
func (t *platformAdministrationTransaction) ListInvitations(ctx context.Context, cap biz.PlatformCapability, status biz.InvitationStatus, cursor uuid.UUID, limit int32, now time.Time) ([]biz.PlatformInvitationState, error) {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 101 {
		return nil, biz.ErrInvalidPersistenceState
	}
	rows, err := t.q.ListPlatformInvitations(ctx, sqlcgen.ListPlatformInvitationsParams{Status: string(status), CursorID: cursor, PageLimit: limit, Now: requiredTimestamptz(now)})
	if err != nil {
		return nil, mapPostgresError("list platform invitations", err, nil)
	}
	result := make([]biz.PlatformInvitationState, 0, len(rows))
	for _, row := range rows {
		state, err := t.invitation(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, nil
}
func (t *platformAdministrationTransaction) ExpiredInvitationsForRole(ctx context.Context, cap biz.PlatformCapability, role uuid.UUID, now time.Time) ([]biz.PlatformInvitationState, error) {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return nil, err
	}
	rows, err := t.q.ListExpiredPlatformInvitationsForRole(ctx, sqlcgen.ListExpiredPlatformInvitationsForRoleParams{RoleID: role, Now: requiredTimestamptz(now)})
	if err != nil {
		return nil, mapPostgresError("list expired role invitation references", err, nil)
	}
	result := make([]biz.PlatformInvitationState, 0, len(rows))
	for _, row := range rows {
		state, err := t.invitation(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, nil
}
func (t *platformAdministrationTransaction) invitation(ctx context.Context, r sqlcgen.PlatformInvitation) (biz.PlatformInvitationState, error) {
	if len(r.TokenDigest) != 32 || !r.ExpiresAt.Valid || !r.CreatedAt.Valid || !r.UpdatedAt.Valid {
		return biz.PlatformInvitationState{}, biz.ErrInvalidPersistenceState
	}
	delivery, err := t.q.GetPlatformInvitationDelivery(ctx, sqlcgen.GetPlatformInvitationDeliveryParams{InvitationID: r.ID, DeliveryGeneration: r.DeliveryGeneration})
	if err != nil {
		return biz.PlatformInvitationState{}, mapPostgresError("get platform invitation delivery state", err, nil)
	}
	value := biz.PlatformInvitation{ID: r.ID, NormalizedEmail: r.NormalizedEmail, RoleIDs: r.RoleIds, Locale: r.Locale, Status: biz.InvitationStatus(r.Status), ExpiresAt: r.ExpiresAt.Time.UTC(), Version: r.Version, DeliveryGeneration: r.DeliveryGeneration, DeliveryStatus: delivery.Status, DeliveryAttemptCount: delivery.AttemptCount, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC()}
	if r.AcceptedMembershipID.Valid {
		value.AcceptedMembershipID = uuid.UUID(r.AcceptedMembershipID.Bytes)
	}
	if r.AcceptedPrincipalID.Valid {
		value.AcceptedPrincipalID = uuid.UUID(r.AcceptedPrincipalID.Bytes)
	}
	result := biz.PlatformInvitationState{Invitation: value}
	copy(result.TokenDigest[:], r.TokenDigest)
	return result, nil
}
func (t *platformAdministrationTransaction) CreateInvitation(ctx context.Context, cap biz.PlatformCapability, state biz.PlatformInvitationState, delivery uuid.UUID, token string) error {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	inv := state.Invitation
	if inv.Version != 1 || inv.DeliveryGeneration != 1 || inv.Status != biz.InvitationPending || sha256.Sum256([]byte(token)) != state.TokenDigest {
		return biz.ErrInvalidPersistenceState
	}
	err = t.q.CreatePlatformInvitation(ctx, sqlcgen.CreatePlatformInvitationParams{ID: inv.ID, NormalizedEmail: inv.NormalizedEmail, RoleIds: inv.RoleIDs, Locale: inv.Locale, TokenDigest: state.TokenDigest[:], ExpiresAt: requiredTimestamptz(inv.ExpiresAt), CreatedBy: inv.CreatedBy, CreatedAt: requiredTimestamptz(inv.CreatedAt)})
	if err != nil {
		return mapPostgresError("create platform invitation", err, biz.ErrInvitationConflict)
	}
	if err = t.addInvitationRoles(ctx, inv); err != nil {
		return err
	}
	return t.addInvitationDelivery(ctx, inv, delivery, token)
}
func (t *platformAdministrationTransaction) ResendInvitation(ctx context.Context, cap biz.PlatformCapability, state biz.PlatformInvitationState, version int64, delivery uuid.UUID, token string) error {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	inv := state.Invitation
	if sha256.Sum256([]byte(token)) != state.TokenDigest || inv.Version != version+1 || inv.Status != biz.InvitationPending {
		return biz.ErrInvalidPersistenceState
	}
	count, err := t.q.ResendPlatformInvitation(ctx, sqlcgen.ResendPlatformInvitationParams{ID: inv.ID, ExpectedVersion: version, TokenDigest: state.TokenDigest[:], ExpiresAt: requiredTimestamptz(inv.ExpiresAt), UpdatedAt: requiredTimestamptz(inv.UpdatedAt)})
	if err != nil {
		return mapPostgresError("resend platform invitation", err, biz.ErrInvitationConflict)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	if err = t.clearInvitationReferences(ctx, inv.ID, inv.UpdatedAt); err != nil {
		return err
	}
	if err = t.addInvitationRoles(ctx, inv); err != nil {
		return err
	}
	return t.addInvitationDelivery(ctx, inv, delivery, token)
}
func (t *platformAdministrationTransaction) EndInvitation(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID, version int64, status biz.InvitationStatus, now time.Time) error {
	_, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	if status != biz.InvitationExpired && status != biz.InvitationCancelled {
		return biz.ErrInvalidPersistenceState
	}
	count, err := t.q.TransitionPlatformInvitationTerminal(ctx, sqlcgen.TransitionPlatformInvitationTerminalParams{ID: id, ExpectedVersion: version, Status: string(status), UpdatedAt: requiredTimestamptz(now)})
	if err != nil {
		return mapPostgresError("end platform invitation", err, nil)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	return t.clearInvitationReferences(ctx, id, now)
}
func (t *platformAdministrationTransaction) clearInvitationReferences(ctx context.Context, id uuid.UUID, now time.Time) error {
	if err := t.q.ClearPlatformInvitationRoles(ctx, sqlcgen.ClearPlatformInvitationRolesParams{InvitationID: id}); err != nil {
		return mapPostgresError("release terminal invitation references", err, nil)
	}
	return mapPostgresError("cancel obsolete invitation deliveries", t.q.CancelPlatformInvitationDeliveries(ctx, sqlcgen.CancelPlatformInvitationDeliveriesParams{InvitationID: id, UpdatedAt: requiredTimestamptz(now)}), nil)
}
func (t *platformAdministrationTransaction) addInvitationRoles(ctx context.Context, inv biz.PlatformInvitation) error {
	for _, role := range inv.RoleIDs {
		if err := t.q.AddPlatformInvitationRole(ctx, sqlcgen.AddPlatformInvitationRoleParams{InvitationID: inv.ID, RoleID: role}); err != nil {
			return mapPostgresError("add same-platform invitation role", err, biz.ErrInvitationConflict)
		}
	}
	return nil
}
func (t *platformAdministrationTransaction) addInvitationDelivery(ctx context.Context, inv biz.PlatformInvitation, delivery uuid.UUID, token string) error {
	version, payload, err := t.protector.sealPlatformInvitation(inv.ID, delivery, inv.DeliveryGeneration, platformInvitationDeliveryPayload{Email: inv.NormalizedEmail, Token: token})
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	return mapPostgresError("enqueue platform invitation delivery", t.q.CreatePlatformInvitationDelivery(ctx, sqlcgen.CreatePlatformInvitationDeliveryParams{ID: delivery, InvitationID: inv.ID, DeliveryGeneration: inv.DeliveryGeneration, PayloadKeyVersion: pgtype.Text{String: version, Valid: true}, PayloadCiphertext: payload, CreatedAt: requiredTimestamptz(inv.UpdatedAt)}), nil)
}
