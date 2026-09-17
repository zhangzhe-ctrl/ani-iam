package data

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"slices"
	"time"
)

type invitationAcceptanceUOW struct{ data *Data }
type invitationAcceptanceTransaction struct {
	q   *sqlcgen.Queries
	cap biz.InvitationAcceptanceCapability
}

func NewInvitationAcceptanceUnitOfWork(d *Data) biz.InvitationAcceptanceUnitOfWork {
	return &invitationAcceptanceUOW{d}
}
func (u *invitationAcceptanceUOW) WithinInvitationAcceptance(ctx context.Context, cap biz.InvitationAcceptanceCapability, fn func(biz.InvitationAcceptanceTransaction) error) error {
	claims, target, _, err := cap.Binding()
	if err != nil {
		return err
	}
	if u == nil || u.data == nil || u.data.pool == nil || fn == nil {
		return biz.ErrPersistenceUnavailable
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin invitation acceptance", err, nil)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return mapPostgresError("lock invitation Platform boundary", err, nil)
	}
	// Use the established global-to-Tenant lock order. Sort and deduplicate both
	// real boundaries so two invitation acceptances cannot invert their locks.
	tenants := []uuid.UUID{}
	if claims.Password == nil && claims.Session.Boundary == biz.AccessBoundaryTenant {
		tenants = append(tenants, claims.Session.TenantID)
	}
	if target.Boundary == biz.AccessBoundaryTenant {
		tenants = append(tenants, target.TenantID)
	}
	slices.SortFunc(tenants, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	tenants = slices.Compact(tenants)
	for _, tenant := range tenants {
		if err = q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
			return mapPostgresError("lock invitation Tenant boundary", err, nil)
		}
	}
	if err = fn(&invitationAcceptanceTransaction{q: q, cap: cap}); err != nil {
		return err
	}
	return mapPostgresError("commit invitation acceptance", tx.Commit(ctx), nil)
}
func (t *invitationAcceptanceTransaction) binding(cap biz.InvitationAcceptanceCapability) (biz.InvitationAcceptanceBinding, biz.InvitationAcceptanceTarget, error) {
	c, target, caller, err := cap.Binding()
	if err != nil {
		return c, target, err
	}
	if !cap.Matches(t.cap) {
		return c, target, biz.ErrInvitationDenied
	}
	_ = caller
	return c, target, nil
}
func (t *invitationAcceptanceTransaction) Actor(ctx context.Context, cap biz.InvitationAcceptanceCapability) (biz.InvitationAcceptanceActor, error) {
	c, _, err := t.binding(cap)
	if err != nil {
		return biz.InvitationAcceptanceActor{}, err
	}
	if c.Password != nil {
		p, err := lockInvitationPassword(ctx, t.q, c.Password.NormalizedEmail)
		if err != nil {
			return biz.InvitationAcceptanceActor{}, err
		}
		return biz.InvitationAcceptanceActor{NormalizedEmail: p.NormalizedEmail, PrincipalStatus: p.PrincipalStatus, Password: p}, nil
	}
	if c.Session.Boundary == biz.AccessBoundaryPlatform {
		r, err := t.q.LockInvitationPlatformActor(ctx, sqlcgen.LockInvitationPlatformActorParams{PrincipalID: c.Subject, SessionID: c.Session.SessionID, GrantID: c.Session.GrantID})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.InvitationAcceptanceActor{}, biz.ErrInvalidCredential
		}
		if err != nil {
			return biz.InvitationAcceptanceActor{}, mapPostgresError("authenticate Platform invitation recipient", err, nil)
		}
		if !r.IdleExpiresAt.Valid || !r.AbsoluteExpiresAt.Valid {
			return biz.InvitationAcceptanceActor{}, biz.ErrInvalidPersistenceState
		}
		return biz.InvitationAcceptanceActor{NormalizedEmail: r.NormalizedEmail, PrincipalStatus: biz.PrincipalStatus(r.PrincipalStatus), MembershipStatus: biz.MembershipStatus(r.MembershipStatus), SessionStatus: biz.SessionStatus(r.SessionStatus), GrantStatus: biz.GrantStatus(r.GrantStatus), GrantVersion: r.GrantVersion, IdleExpiresAt: r.IdleExpiresAt.Time.UTC(), AbsoluteExpiresAt: r.AbsoluteExpiresAt.Time.UTC()}, nil
	}
	r, err := t.q.LockInvitationTenantActor(ctx, sqlcgen.LockInvitationTenantActorParams{TenantID: c.Session.TenantID, PrincipalID: c.Subject, SessionID: c.Session.SessionID, GrantID: c.Session.GrantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.InvitationAcceptanceActor{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.InvitationAcceptanceActor{}, mapPostgresError("authenticate Tenant invitation recipient", err, nil)
	}
	if !r.IdleExpiresAt.Valid || !r.AbsoluteExpiresAt.Valid {
		return biz.InvitationAcceptanceActor{}, biz.ErrInvalidPersistenceState
	}
	return biz.InvitationAcceptanceActor{NormalizedEmail: r.NormalizedEmail, PrincipalStatus: biz.PrincipalStatus(r.PrincipalStatus), MembershipStatus: biz.MembershipStatus(r.MembershipStatus), SessionStatus: biz.SessionStatus(r.SessionStatus), GrantStatus: biz.GrantStatus(r.GrantStatus), GrantVersion: r.GrantVersion, IdleExpiresAt: r.IdleExpiresAt.Time.UTC(), AbsoluteExpiresAt: r.AbsoluteExpiresAt.Time.UTC(), SourceAccess: biz.TenantAccessStatus(r.AccessStatus), SourceLifecycle: biz.TenantLifecycleStatus(r.LifecycleStatus), SourceLifecycleFresh: r.LifecycleFresh}, nil
}
func (t *invitationAcceptanceTransaction) Invitation(ctx context.Context, cap biz.InvitationAcceptanceCapability) (biz.TenantInvitationState, error) {
	_, target, err := t.binding(cap)
	if err != nil {
		return biz.TenantInvitationState{}, err
	}
	var state biz.TenantInvitationState
	if target.Boundary == biz.AccessBoundaryPlatform {
		r, err := t.q.GetPlatformInvitation(ctx, sqlcgen.GetPlatformInvitationParams{ID: target.InvitationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return state, biz.ErrInvitationNotFound
		}
		if err != nil {
			return state, mapPostgresError("read target Platform invitation", err, nil)
		}
		if len(r.TokenDigest) != 32 || !r.ExpiresAt.Valid || !r.CreatedAt.Valid || !r.UpdatedAt.Valid {
			return state, biz.ErrInvalidPersistenceState
		}
		state.Invitation = biz.TenantInvitation{ID: r.ID, NormalizedEmail: r.NormalizedEmail, RoleIDs: r.RoleIds, Status: biz.InvitationStatus(r.Status), Version: r.Version, ExpiresAt: r.ExpiresAt.Time.UTC(), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC()}
		copy(state.TokenDigest[:], r.TokenDigest)
	} else {
		r, err := t.q.GetTenantInvitation(ctx, sqlcgen.GetTenantInvitationParams{TenantID: target.TenantID, ID: target.InvitationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return state, biz.ErrInvitationNotFound
		}
		if err != nil {
			return state, mapPostgresError("read target Tenant invitation", err, nil)
		}
		if r.TenantID != target.TenantID || len(r.TokenDigest) != 32 || !r.ExpiresAt.Valid || !r.CreatedAt.Valid || !r.UpdatedAt.Valid {
			return state, biz.ErrInvalidPersistenceState
		}
		state.Invitation = biz.TenantInvitation{ID: r.ID, NormalizedEmail: r.NormalizedEmail, RoleIDs: r.RoleIds, Status: biz.InvitationStatus(r.Status), Version: r.Version, ExpiresAt: r.ExpiresAt.Time.UTC(), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC()}
		copy(state.TokenDigest[:], r.TokenDigest)
	}
	return state, nil
}
func (t *invitationAcceptanceTransaction) TargetReady(ctx context.Context, cap biz.InvitationAcceptanceCapability, policy biz.TenantAdminLoginPolicy) (*biz.InvitationBootstrapState, error) {
	_, target, err := t.binding(cap)
	if err != nil {
		return nil, err
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		return nil, nil
	}
	a, err := t.q.GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: target.TenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrTenantIAMNotReady
	}
	if err != nil {
		return nil, mapPostgresError("read invitation target Access", err, nil)
	}
	bootstrap, err := t.bootstrap(ctx, cap, policy)
	if err != nil {
		return nil, err
	}
	if a.Status == "bootstrap_pending" && bootstrap == nil {
		return nil, biz.ErrTenantIAMNotReady
	}
	if bootstrap != nil {
		if a.Status != "bootstrap_pending" {
			return nil, biz.ErrInvitationConflict
		}
		bootstrap.AccessVersion = a.Version
	}
	if a.Status != "active" && a.Status != "bootstrap_pending" {
		return nil, biz.ErrTenantAccessInactive
	}
	l, err := t.q.InvitationTargetLifecycle(ctx, sqlcgen.InvitationTargetLifecycleParams{TenantID: target.TenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrTenantLifecycleStale
	}
	if err != nil {
		return nil, mapPostgresError("read invitation target Lifecycle", err, nil)
	}
	if !l.Fresh {
		return nil, biz.ErrTenantLifecycleStale
	}
	if l.Status != "active" {
		return nil, biz.ErrTenantLifecycleBlocked
	}
	return bootstrap, nil
}
func (t *invitationAcceptanceTransaction) ExistingMembership(ctx context.Context, cap biz.InvitationAcceptanceCapability) (biz.InvitationAcceptedMembership, bool, error) {
	c, target, err := t.binding(cap)
	if err != nil {
		return biz.InvitationAcceptedMembership{}, false, err
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		r, err := t.q.InvitationLatestPlatformMembership(ctx, sqlcgen.InvitationLatestPlatformMembershipParams{PrincipalID: c.Subject})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.InvitationAcceptedMembership{}, false, nil
		}
		if err != nil {
			return biz.InvitationAcceptedMembership{}, false, mapPostgresError("read existing invited Platform Membership", err, nil)
		}
		if !r.UpdatedAt.Valid {
			return biz.InvitationAcceptedMembership{}, false, biz.ErrInvalidPersistenceState
		}
		return biz.InvitationAcceptedMembership{ID: r.ID, PrincipalID: r.PrincipalID, Status: biz.MembershipStatus(r.Status), Version: r.Version, UpdatedAt: r.UpdatedAt.Time.UTC()}, true, nil
	}
	r, err := t.q.InvitationLatestTenantMembership(ctx, sqlcgen.InvitationLatestTenantMembershipParams{TenantID: target.TenantID, PrincipalID: c.Subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.InvitationAcceptedMembership{}, false, nil
	}
	if err != nil {
		return biz.InvitationAcceptedMembership{}, false, mapPostgresError("read existing invited Tenant Membership", err, nil)
	}
	if r.TenantID != target.TenantID || !r.UpdatedAt.Valid {
		return biz.InvitationAcceptedMembership{}, false, biz.ErrInvalidPersistenceState
	}
	return biz.InvitationAcceptedMembership{ID: r.ID, PrincipalID: r.PrincipalID, Status: biz.MembershipStatus(r.Status), Version: r.Version, UpdatedAt: r.UpdatedAt.Time.UTC()}, true, nil
}
func (t *invitationAcceptanceTransaction) ValidateRoles(ctx context.Context, cap biz.InvitationAcceptanceCapability, roles []uuid.UUID) error {
	_, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if len(roles) < 1 || len(roles) > 100 {
		return biz.ErrInvalidPersistenceState
	}
	for _, id := range roles {
		if target.Boundary == biz.AccessBoundaryPlatform {
			_, err = t.q.GetPlatformRole(ctx, sqlcgen.GetPlatformRoleParams{ID: id})
		} else {
			_, err = t.q.GetTenantAuthorizationRole(ctx, sqlcgen.GetTenantAuthorizationRoleParams{TenantID: target.TenantID, ID: id})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrInvitationConflict
		}
		if err != nil {
			return mapPostgresError("read invited target Role", err, nil)
		}
	}
	return nil
}
func (t *invitationAcceptanceTransaction) CreateMembership(ctx context.Context, cap biz.InvitationAcceptanceCapability, m biz.InvitationAcceptedMembership, bindings []uuid.UUID) error {
	c, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if m.PrincipalID != c.Subject || m.Status != biz.MembershipStatusActive || m.Version != 1 || len(bindings) != len(m.RoleIDs) {
		return biz.ErrInvalidPersistenceState
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		err = t.q.CreatePlatformMembership(ctx, sqlcgen.CreatePlatformMembershipParams{ID: m.ID, PrincipalID: c.Subject, Now: requiredTimestamptz(m.CreatedAt)})
	} else {
		err = t.q.CreateTenantMembership(ctx, sqlcgen.CreateTenantMembershipParams{TenantID: target.TenantID, ID: m.ID, PrincipalID: c.Subject, Status: "active", Version: 1, CreatedAt: requiredTimestamptz(m.CreatedAt), UpdatedAt: requiredTimestamptz(m.UpdatedAt)})
	}
	if err != nil {
		return mapPostgresError("create invited Membership", err, biz.ErrInvitationConflict)
	}
	for i, role := range m.RoleIDs {
		if target.Boundary == biz.AccessBoundaryPlatform {
			err = t.q.CreatePlatformRoleBinding(ctx, sqlcgen.CreatePlatformRoleBindingParams{ID: bindings[i], MembershipID: m.ID, RoleID: role, Now: requiredTimestamptz(m.CreatedAt)})
		} else {
			err = t.q.CreateTenantRoleBinding(ctx, sqlcgen.CreateTenantRoleBindingParams{TenantID: target.TenantID, ID: bindings[i], MembershipID: m.ID, RoleID: role, Version: 1, CreatedAt: requiredTimestamptz(m.CreatedAt), UpdatedAt: requiredTimestamptz(m.UpdatedAt)})
		}
		if err != nil {
			return mapPostgresError("bind invited Role", err, biz.ErrInvitationConflict)
		}
	}
	return nil
}
func (t *invitationAcceptanceTransaction) ConsumeInvitation(ctx context.Context, cap biz.InvitationAcceptanceCapability, state biz.TenantInvitationState, m biz.InvitationAcceptedMembership, now time.Time) error {
	c, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if state.Invitation.ID != target.InvitationID || m.PrincipalID != c.Subject {
		return biz.ErrInvalidPersistenceState
	}
	var n int64
	if target.Boundary == biz.AccessBoundaryPlatform {
		n, err = t.q.AcceptPlatformInvitation(ctx, sqlcgen.AcceptPlatformInvitationParams{ID: target.InvitationID, ExpectedVersion: state.Invitation.Version, TokenDigest: state.TokenDigest[:], MembershipID: requiredPGUUID(m.ID), PrincipalID: requiredPGUUID(c.Subject), Now: requiredTimestamptz(now)})
	} else {
		n, err = t.q.AcceptTenantInvitation(ctx, sqlcgen.AcceptTenantInvitationParams{TenantID: target.TenantID, ID: target.InvitationID, ExpectedVersion: state.Invitation.Version, TokenDigest: state.TokenDigest[:], MembershipID: requiredPGUUID(m.ID), PrincipalID: requiredPGUUID(c.Subject), Now: requiredTimestamptz(now)})
	}
	if err != nil {
		return mapPostgresError("consume invitation", err, nil)
	}
	if n != 1 {
		return biz.ErrInvitationConflict
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		if err = t.q.ClearPlatformInvitationRoles(ctx, sqlcgen.ClearPlatformInvitationRolesParams{InvitationID: target.InvitationID}); err == nil {
			err = t.q.CancelPlatformInvitationDeliveries(ctx, sqlcgen.CancelPlatformInvitationDeliveriesParams{InvitationID: target.InvitationID, UpdatedAt: requiredTimestamptz(now)})
		}
	} else {
		if err = t.q.ClearTenantInvitationRoles(ctx, sqlcgen.ClearTenantInvitationRolesParams{TenantID: target.TenantID, InvitationID: target.InvitationID}); err == nil {
			err = t.q.CancelTenantInvitationDeliveries(ctx, sqlcgen.CancelTenantInvitationDeliveriesParams{TenantID: target.TenantID, InvitationID: target.InvitationID, UpdatedAt: requiredTimestamptz(now)})
		}
	}
	return mapPostgresError("release consumed invitation delivery and references", err, nil)
}
func (t *invitationAcceptanceTransaction) FindReceipt(ctx context.Context, cap biz.InvitationAcceptanceCapability, id biz.MutationIdentity) (biz.StoredMutation, bool, error) {
	c, target, err := t.binding(cap)
	if err != nil {
		return biz.StoredMutation{}, false, err
	}
	if c.Subject != id.ActorID {
		return biz.StoredMutation{}, false, biz.ErrInvitationDenied
	}
	var result []byte
	var digest []byte
	var created, expires time.Time
	if target.Boundary == biz.AccessBoundaryPlatform {
		r, e := t.q.FindPlatformMutation(ctx, sqlcgen.FindPlatformMutationParams{ActorID: id.ActorID, Operation: id.Operation, IdempotencyKey: id.Key})
		err = e
		result, digest, created, expires = r.Result, r.RequestHash, r.CreatedAt.Time, r.ExpiresAt.Time
	} else {
		r, e := t.q.FindTenantMutationResult(ctx, sqlcgen.FindTenantMutationResultParams{TenantID: target.TenantID, ActorID: id.ActorID, Operation: id.Operation, IdempotencyKey: id.Key})
		err = e
		result, digest, created, expires = r.Result, r.IntentDigest, r.CreatedAt.Time, r.ExpiresAt.Time
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.StoredMutation{}, false, nil
	}
	if err != nil {
		return biz.StoredMutation{}, false, mapPostgresError("read invitation acceptance receipt", err, nil)
	}
	if len(digest) != 32 || created.IsZero() || expires.IsZero() {
		return biz.StoredMutation{}, false, biz.ErrInvalidPersistenceState
	}
	copy(id.Intent[:], digest)
	return biz.StoredMutation{Identity: id, Result: result, CreatedAt: created.UTC(), ExpiresAt: expires.UTC()}, true, nil
}
func (t *invitationAcceptanceTransaction) SaveReceipt(ctx context.Context, cap biz.InvitationAcceptanceCapability, r biz.StoredMutation) error {
	c, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if r.Identity.ActorID != c.Subject {
		return biz.ErrInvitationDenied
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		err = t.q.SavePlatformMutation(ctx, sqlcgen.SavePlatformMutationParams{ActorID: r.Identity.ActorID, Operation: r.Identity.Operation, IdempotencyKey: r.Identity.Key, RequestHash: r.Identity.Intent[:], Result: r.Result, CreatedAt: requiredTimestamptz(r.CreatedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)})
	} else {
		err = t.q.SaveTenantMutationResult(ctx, sqlcgen.SaveTenantMutationResultParams{TenantID: target.TenantID, ActorID: r.Identity.ActorID, Operation: r.Identity.Operation, IdempotencyKey: r.Identity.Key, IntentDigest: r.Identity.Intent[:], CallerPrincipalID: requiredPGUUID(r.Identity.CallerID), Result: r.Result, CreatedAt: requiredTimestamptz(r.CreatedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)})
	}
	return mapPostgresError("save invitation acceptance receipt", err, nil)
}
func (t *invitationAcceptanceTransaction) AppendAcceptanceAudit(ctx context.Context, cap biz.InvitationAcceptanceCapability, a biz.SecurityAuditEvent) error {
	c, target, err := t.binding(cap)
	if err != nil {
		return err
	}
	if a.ActorID != c.Subject || a.TargetID != target.InvitationID {
		return biz.ErrInvalidPersistenceState
	}
	if target.Boundary == biz.AccessBoundaryPlatform {
		if a.Boundary != biz.AuditBoundaryPlatform {
			return biz.ErrInvalidPersistenceState
		}
		return appendPlatformAudit(ctx, t.q, a)
	}
	if a.Boundary != biz.AuditBoundaryTenant {
		return biz.ErrInvalidPersistenceState
	}
	scope, err := biz.NewTenantScope(target.TenantID)
	if err != nil {
		return err
	}
	return (securityAuditRepository{queries: t.q, tenantID: target.TenantID}).Append(ctx, scope, a)
}
