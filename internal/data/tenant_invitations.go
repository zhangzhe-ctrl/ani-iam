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

// The protector belongs to Data; transaction adapters share the existing
// tenant administrator guard and never acquire their own storage connection.
type tenantInvitationTransaction struct {
	postgresTenantAuthorizationTransaction
	protector *OutboxProtector
}

func NewPostgresTenantInvitationUnitOfWork(d *Data) biz.TenantInvitationUnitOfWork {
	return &postgresTenantAuthorizationUnitOfWork{data: d}
}
func (u *postgresTenantAuthorizationUnitOfWork) WithinTenantInvitations(ctx context.Context, scope biz.TenantScope, fn func(context.Context, biz.TenantInvitationTransaction) error) error {
	return u.WithinTenantAuthorization(ctx, scope, func(ctx context.Context, tx biz.TenantAuthorizationTransaction) error {
		return fn(ctx, &tenantInvitationTransaction{tx.(postgresTenantAuthorizationTransaction), u.data.outbox})
	})
}
func (t *tenantInvitationTransaction) GetInvitation(ctx context.Context, scope biz.TenantScope, id uuid.UUID) (biz.TenantInvitationState, error) {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return biz.TenantInvitationState{}, err
	}
	row, err := t.queries.GetTenantInvitation(ctx, sqlcgen.GetTenantInvitationParams{TenantID: tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantInvitationState{}, biz.ErrInvitationNotFound
	}
	if err != nil {
		return biz.TenantInvitationState{}, mapPostgresError("get tenant invitation", err, nil)
	}
	return t.invitation(ctx, row)
}
func (t *tenantInvitationTransaction) FindPendingInvitation(ctx context.Context, scope biz.TenantScope, email string) (biz.TenantInvitationState, bool, error) {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return biz.TenantInvitationState{}, false, err
	}
	row, err := t.queries.FindPendingTenantInvitation(ctx, sqlcgen.FindPendingTenantInvitationParams{TenantID: tenant, NormalizedEmail: email})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantInvitationState{}, false, nil
	}
	if err != nil {
		return biz.TenantInvitationState{}, false, mapPostgresError("find pending tenant invitation", err, nil)
	}
	state, err := t.invitation(ctx, row)
	return state, err == nil, err
}
func (t *tenantInvitationTransaction) ListInvitations(ctx context.Context, scope biz.TenantScope, status biz.InvitationStatus, cursor uuid.UUID, limit int32, now time.Time) ([]biz.TenantInvitationState, error) {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 101 {
		return nil, biz.ErrInvalidPersistenceState
	}
	rows, err := t.queries.ListTenantInvitations(ctx, sqlcgen.ListTenantInvitationsParams{TenantID: tenant, Status: string(status), CursorID: cursor, PageLimit: limit, Now: requiredTimestamptz(now)})
	if err != nil {
		return nil, mapPostgresError("list tenant invitations", err, nil)
	}
	result := make([]biz.TenantInvitationState, 0, len(rows))
	for _, row := range rows {
		state, err := t.invitation(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, nil
}
func (t *tenantInvitationTransaction) ExpiredInvitationsForRole(ctx context.Context, scope biz.TenantScope, role uuid.UUID, now time.Time) ([]biz.TenantInvitationState, error) {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return nil, err
	}
	rows, err := t.queries.ListExpiredTenantInvitationsForRole(ctx, sqlcgen.ListExpiredTenantInvitationsForRoleParams{TenantID: tenant, RoleID: role, Now: requiredTimestamptz(now)})
	if err != nil {
		return nil, mapPostgresError("list expired role invitation references", err, nil)
	}
	result := make([]biz.TenantInvitationState, 0, len(rows))
	for _, row := range rows {
		state, err := t.invitation(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, nil
}
func (t *tenantInvitationTransaction) invitation(ctx context.Context, r sqlcgen.TenantInvitation) (biz.TenantInvitationState, error) {
	if r.TenantID != t.tenantID || len(r.TokenDigest) != 32 || !r.ExpiresAt.Valid || !r.CreatedAt.Valid || !r.UpdatedAt.Valid {
		return biz.TenantInvitationState{}, biz.ErrInvalidPersistenceState
	}
	delivery, err := t.queries.GetTenantInvitationDelivery(ctx, sqlcgen.GetTenantInvitationDeliveryParams{TenantID: r.TenantID, InvitationID: r.ID, DeliveryGeneration: r.DeliveryGeneration})
	if err != nil {
		return biz.TenantInvitationState{}, mapPostgresError("get tenant invitation delivery state", err, nil)
	}
	value := biz.TenantInvitation{ID: r.ID, NormalizedEmail: r.NormalizedEmail, RoleIDs: r.RoleIds, Locale: r.Locale, Status: biz.InvitationStatus(r.Status), ExpiresAt: r.ExpiresAt.Time.UTC(), Version: r.Version, DeliveryGeneration: r.DeliveryGeneration, DeliveryStatus: delivery.Status, DeliveryAttemptCount: delivery.AttemptCount, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC()}
	if r.AcceptedMembershipID.Valid {
		value.AcceptedMembershipID = uuid.UUID(r.AcceptedMembershipID.Bytes)
	}
	if r.AcceptedPrincipalID.Valid {
		value.AcceptedPrincipalID = uuid.UUID(r.AcceptedPrincipalID.Bytes)
	}
	result := biz.TenantInvitationState{Invitation: value}
	copy(result.TokenDigest[:], r.TokenDigest)
	return result, nil
}
func (t *tenantInvitationTransaction) CreateInvitation(ctx context.Context, scope biz.TenantScope, state biz.TenantInvitationState, delivery uuid.UUID, token string) error {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return err
	}
	inv := state.Invitation
	if inv.Version != 1 || inv.DeliveryGeneration != 1 || inv.Status != biz.InvitationPending || sha256.Sum256([]byte(token)) != state.TokenDigest {
		return biz.ErrInvalidPersistenceState
	}
	err = t.queries.CreateTenantInvitation(ctx, sqlcgen.CreateTenantInvitationParams{TenantID: tenant, ID: inv.ID, NormalizedEmail: inv.NormalizedEmail, RoleIds: inv.RoleIDs, Locale: inv.Locale, TokenDigest: state.TokenDigest[:], ExpiresAt: requiredTimestamptz(inv.ExpiresAt), CreatedBy: inv.CreatedBy, CreatedAt: requiredTimestamptz(inv.CreatedAt)})
	if err != nil {
		return mapPostgresError("create tenant invitation", err, biz.ErrInvitationConflict)
	}
	if err = t.addInvitationRoles(ctx, tenant, inv); err != nil {
		return err
	}
	return t.addInvitationDelivery(ctx, tenant, inv, delivery, token)
}
func (t *tenantInvitationTransaction) ResendInvitation(ctx context.Context, scope biz.TenantScope, state biz.TenantInvitationState, version int64, delivery uuid.UUID, token string) error {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return err
	}
	inv := state.Invitation
	if sha256.Sum256([]byte(token)) != state.TokenDigest || inv.Version != version+1 || inv.Status != biz.InvitationPending {
		return biz.ErrInvalidPersistenceState
	}
	count, err := t.queries.ResendTenantInvitation(ctx, sqlcgen.ResendTenantInvitationParams{TenantID: tenant, ID: inv.ID, ExpectedVersion: version, TokenDigest: state.TokenDigest[:], ExpiresAt: requiredTimestamptz(inv.ExpiresAt), UpdatedAt: requiredTimestamptz(inv.UpdatedAt)})
	if err != nil {
		return mapPostgresError("resend tenant invitation", err, biz.ErrInvitationConflict)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	if err = t.clearInvitationReferences(ctx, tenant, inv.ID, inv.UpdatedAt); err != nil {
		return err
	}
	if err = t.addInvitationRoles(ctx, tenant, inv); err != nil {
		return err
	}
	return t.addInvitationDelivery(ctx, tenant, inv, delivery, token)
}
func (t *tenantInvitationTransaction) EndInvitation(ctx context.Context, scope biz.TenantScope, id uuid.UUID, version int64, status biz.InvitationStatus, now time.Time) error {
	tenant, err := tenantIDForScope(t.tenantID, scope)
	if err != nil {
		return err
	}
	if status != biz.InvitationExpired && status != biz.InvitationCancelled {
		return biz.ErrInvalidPersistenceState
	}
	count, err := t.queries.TransitionTenantInvitationTerminal(ctx, sqlcgen.TransitionTenantInvitationTerminalParams{TenantID: tenant, ID: id, ExpectedVersion: version, Status: string(status), UpdatedAt: requiredTimestamptz(now)})
	if err != nil {
		return mapPostgresError("end tenant invitation", err, nil)
	}
	if count != 1 {
		return biz.ErrVersionConflict
	}
	return t.clearInvitationReferences(ctx, tenant, id, now)
}
func (t *tenantInvitationTransaction) clearInvitationReferences(ctx context.Context, tenant, id uuid.UUID, now time.Time) error {
	if err := t.queries.ClearTenantInvitationRoles(ctx, sqlcgen.ClearTenantInvitationRolesParams{TenantID: tenant, InvitationID: id}); err != nil {
		return mapPostgresError("release terminal invitation references", err, nil)
	}
	return mapPostgresError("cancel obsolete invitation deliveries", t.queries.CancelTenantInvitationDeliveries(ctx, sqlcgen.CancelTenantInvitationDeliveriesParams{TenantID: tenant, InvitationID: id, UpdatedAt: requiredTimestamptz(now)}), nil)
}
func (t *tenantInvitationTransaction) addInvitationRoles(ctx context.Context, tenant uuid.UUID, inv biz.TenantInvitation) error {
	for _, role := range inv.RoleIDs {
		if err := t.queries.AddTenantInvitationRole(ctx, sqlcgen.AddTenantInvitationRoleParams{TenantID: tenant, InvitationID: inv.ID, RoleID: role}); err != nil {
			return mapPostgresError("add same-tenant invitation role", err, biz.ErrInvitationConflict)
		}
	}
	return nil
}
func (t *tenantInvitationTransaction) addInvitationDelivery(ctx context.Context, tenant uuid.UUID, inv biz.TenantInvitation, delivery uuid.UUID, token string) error {
	version, payload, err := t.protector.sealTenantInvitation(tenant, inv.ID, delivery, inv.DeliveryGeneration, tenantInvitationDeliveryPayload{Email: inv.NormalizedEmail, Token: token})
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	return mapPostgresError("enqueue tenant invitation delivery", t.queries.CreateTenantInvitationDelivery(ctx, sqlcgen.CreateTenantInvitationDeliveryParams{TenantID: tenant, ID: delivery, InvitationID: inv.ID, DeliveryGeneration: inv.DeliveryGeneration, PayloadKeyVersion: pgtype.Text{String: version, Valid: true}, PayloadCiphertext: payload, CreatedAt: requiredTimestamptz(inv.UpdatedAt)}), nil)
}
