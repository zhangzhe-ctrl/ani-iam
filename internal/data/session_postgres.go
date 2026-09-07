package data

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (r *postgresPasswordLoginReader) LookupRefreshSession(
	ctx context.Context,
	digest [sha256.Size]byte,
) (biz.RefreshSessionState, error) {
	row, err := sqlcgen.New(r.data.pool).LookupRefreshSession(ctx, sqlcgen.LookupRefreshSessionParams{
		RefreshDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.RefreshSessionState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.RefreshSessionState{}, mapPostgresError("lookup refresh session", err, nil)
	}
	return refreshStateFromLookup(row)
}

func (r *postgresPasswordLoginReader) LookupLogoutSession(
	ctx context.Context,
	digest [sha256.Size]byte,
) (biz.LogoutSessionState, bool, error) {
	row, err := sqlcgen.New(r.data.pool).LookupLogoutSession(ctx, sqlcgen.LookupLogoutSessionParams{
		RefreshDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.LogoutSessionState{}, false, nil
	}
	if err != nil {
		return biz.LogoutSessionState{}, false, mapPostgresError("lookup logout session", err, nil)
	}
	state, err := logoutStateFromLookup(row)
	if err != nil {
		return biz.LogoutSessionState{}, false, err
	}
	return state, true, nil
}

func (r *postgresPasswordLoginReader) LookupTenantSwitch(
	ctx context.Context,
	scope biz.TenantScope,
	claims biz.AccessTokenClaims,
) (biz.TenantSwitchState, error) {
	targetTenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantSwitchState{}, err
	}
	row, err := sqlcgen.New(r.data.pool).LookupTenantSwitch(ctx, sqlcgen.LookupTenantSwitchParams{
		SessionID: claims.SessionID, SourceTenantID: claims.TenantID, SourceGrantID: claims.GrantID,
		TargetTenantID: targetTenantID, PrincipalID: claims.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantSwitchState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.TenantSwitchState{}, mapPostgresError("lookup tenant switch", err, nil)
	}
	return tenantSwitchStateFromLookup(row)
}

func (u *postgresLoginUnitOfWork) RotateRefreshSession(
	ctx context.Context,
	mutation biz.RefreshSessionMutation,
) (biz.RefreshSessionMutationResult, error) {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("begin refresh rotation", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockSessionContinuity(ctx, sqlcgen.LockSessionContinuityParams{SessionID: mutation.Expected.Session.ID}); err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("serialize refresh rotation", err, nil)
	}
	row, err := queries.LockRefreshSession(ctx, sqlcgen.LockRefreshSessionParams{
		RefreshDigest: mutation.Expected.Token.Digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.RefreshSessionMutationResult{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("lock refresh session", err, nil)
	}
	current, err := refreshStateFromLock(row)
	if err != nil {
		return biz.RefreshSessionMutationResult{}, err
	}
	if !sameRefreshCredential(current, mutation.Expected) {
		return biz.RefreshSessionMutationResult{}, biz.ErrInvalidCredential
	}
	if current.Token.Status == biz.RefreshTokenStatusConsumed {
		if mutation.ReuseAudit.TargetVersion != mutation.Expected.Grant.Version+1 {
			return biz.RefreshSessionMutationResult{}, biz.ErrInvalidPersistenceState
		}
		reuseAudit := mutation.ReuseAudit
		reuseAudit.TargetVersion = current.Grant.Version + 1
		_, err := applyRefreshReuse(ctx, queries, current, reuseAudit, mutation.RotatedAt)
		if err != nil {
			return biz.RefreshSessionMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return biz.RefreshSessionMutationResult{}, mapPostgresError("commit concurrent refresh reuse", err, nil)
		}
		committed = true
		return biz.RefreshSessionMutationResult{Reused: true}, nil
	}
	if err := validateLockedActiveRefresh(current, mutation); err != nil {
		return biz.RefreshSessionMutationResult{}, err
	}
	if _, err := queries.ConsumeRefreshToken(ctx, sqlcgen.ConsumeRefreshTokenParams{
		ConsumedAt: requiredTimestamptz(mutation.RotatedAt), ReplacedBy: requiredPGUUID(mutation.ReplacementToken.ID),
		TenantID: current.TenantID, TokenID: current.Token.ID, FamilyID: current.Family.ID,
		RefreshDigest: current.Token.Digest[:],
	}); errors.Is(err, pgx.ErrNoRows) {
		return biz.RefreshSessionMutationResult{}, biz.ErrInvalidCredential
	} else if err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("consume refresh token", err, nil)
	}
	if err := queries.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		TenantID: current.TenantID, ID: mutation.ReplacementToken.ID, FamilyID: current.Family.ID,
		Digest: mutation.ReplacementToken.Digest[:], IssuedAt: requiredTimestamptz(mutation.ReplacementToken.IssuedAt),
		ExpiresAt: requiredTimestamptz(mutation.ReplacementToken.ExpiresAt),
	}); err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("create replacement refresh token", err, biz.ErrInvalidPersistenceState)
	}
	sessionVersion, err := queries.UpdateSessionIdleExpiry(ctx, sqlcgen.UpdateSessionIdleExpiryParams{
		IdleExpiresAt: requiredTimestamptz(mutation.NewIdleExpiresAt), UpdatedAt: requiredTimestamptz(mutation.RotatedAt),
		SessionID: current.Session.ID, ExpectedVersion: current.Session.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.RefreshSessionMutationResult{}, biz.ErrInvalidCredential
	} else if err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("slide refresh session idle expiry", err, nil)
	}
	scope, err := biz.NewTenantScope(current.TenantID)
	if err != nil {
		return biz.RefreshSessionMutationResult{}, err
	}
	if err := (securityAuditRepository{queries: queries, tenantID: current.TenantID}).Append(ctx, scope, mutation.Audit); err != nil {
		return biz.RefreshSessionMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.RefreshSessionMutationResult{}, mapPostgresError("commit refresh rotation", err, nil)
	}
	committed = true
	return biz.RefreshSessionMutationResult{SessionVersion: sessionVersion}, nil
}

func (u *postgresLoginUnitOfWork) RevokeRefreshReuse(
	ctx context.Context,
	mutation biz.RefreshReuseMutation,
) (bool, error) {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, mapPostgresError("begin refresh reuse", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockSessionContinuity(ctx, sqlcgen.LockSessionContinuityParams{SessionID: mutation.Expected.Session.ID}); err != nil {
		return false, mapPostgresError("serialize refresh reuse", err, nil)
	}
	row, err := queries.LockRefreshSession(ctx, sqlcgen.LockRefreshSessionParams{
		RefreshDigest: mutation.Expected.Token.Digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, biz.ErrInvalidCredential
	}
	if err != nil {
		return false, mapPostgresError("lock refresh reuse", err, nil)
	}
	current, err := refreshStateFromLock(row)
	if err != nil {
		return false, err
	}
	if !sameRefreshCredential(current, mutation.Expected) || current.Token.Status != biz.RefreshTokenStatusConsumed {
		return false, biz.ErrInvalidCredential
	}
	if mutation.Audit.TargetVersion != mutation.Expected.Grant.Version+1 {
		return false, biz.ErrInvalidPersistenceState
	}
	audit := mutation.Audit
	audit.TargetVersion = current.Grant.Version + 1
	changed, err := applyRefreshReuse(ctx, queries, current, audit, mutation.ReusedAt)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, mapPostgresError("commit refresh reuse", err, nil)
	}
	committed = true
	return changed, nil
}

func applyRefreshReuse(
	ctx context.Context,
	queries *sqlcgen.Queries,
	current biz.RefreshSessionState,
	audit biz.SecurityAuditEvent,
	reusedAt time.Time,
) (bool, error) {
	if current.Family.Status != biz.GrantStatusActive || current.Grant.Status != biz.GrantStatusActive {
		return false, nil
	}
	if audit.Action != biz.AuditActionRefreshTokenReused || audit.Boundary != biz.AuditBoundaryTenant ||
		audit.TargetType != biz.AuditTargetTypeSessionGrant || audit.TargetID != current.Grant.ID ||
		audit.TargetVersion != current.Grant.Version+1 || audit.ActorID != current.Principal.ID {
		return false, biz.ErrInvalidPersistenceState
	}
	if _, err := queries.RevokeRefreshFamilyForReuse(ctx, sqlcgen.RevokeRefreshFamilyForReuseParams{
		UpdatedAt: requiredTimestamptz(reusedAt), TenantID: current.TenantID,
		FamilyID: current.Family.ID, GrantID: current.Grant.ID, ExpectedVersion: current.Family.Version,
	}); errors.Is(err, pgx.ErrNoRows) {
		return false, biz.ErrVersionConflict
	} else if err != nil {
		return false, mapPostgresError("revoke reused refresh family", err, nil)
	}
	if err := queries.RevokeActiveRefreshTokensForFamily(ctx, sqlcgen.RevokeActiveRefreshTokensForFamilyParams{
		TenantID: current.TenantID, FamilyID: current.Family.ID,
	}); err != nil {
		return false, mapPostgresError("revoke active refresh tokens in reused family", err, nil)
	}
	version, err := queries.IncrementSessionGrantVersionForReuse(ctx, sqlcgen.IncrementSessionGrantVersionForReuseParams{
		UpdatedAt: requiredTimestamptz(reusedAt), TenantID: current.TenantID,
		GrantID: current.Grant.ID, ExpectedVersion: current.Grant.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, biz.ErrVersionConflict
	}
	if err != nil {
		return false, mapPostgresError("increment reused refresh grant version", err, nil)
	}
	if version != audit.TargetVersion {
		return false, biz.ErrInvalidPersistenceState
	}
	scope, err := biz.NewTenantScope(current.TenantID)
	if err != nil {
		return false, err
	}
	if err := (securityAuditRepository{queries: queries, tenantID: current.TenantID}).Append(ctx, scope, audit); err != nil {
		return false, err
	}
	return true, nil
}

func (u *postgresLoginUnitOfWork) LogoutSession(
	ctx context.Context,
	mutation biz.LogoutSessionMutation,
) (biz.LogoutSessionMutationResult, error) {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("begin session logout", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockSessionContinuity(ctx, sqlcgen.LockSessionContinuityParams{SessionID: mutation.Expected.Session.ID}); err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("serialize session logout", err, nil)
	}
	row, err := queries.LockLogoutSession(ctx, sqlcgen.LockLogoutSessionParams{RefreshDigest: mutation.RefreshDigest[:]})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return biz.LogoutSessionMutationResult{}, mapPostgresError("commit unknown session logout", err, nil)
		}
		committed = true
		return biz.LogoutSessionMutationResult{}, nil
	}
	if err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("lock session logout", err, nil)
	}
	current, err := logoutStateFromLock(row)
	if err != nil {
		return biz.LogoutSessionMutationResult{}, err
	}
	if current.Session.ID != mutation.Expected.Session.ID || current.Principal.ID != mutation.Expected.Principal.ID {
		return biz.LogoutSessionMutationResult{}, biz.ErrInvalidCredential
	}
	if current.Session.Status != biz.SessionStatusActive {
		if err := tx.Commit(ctx); err != nil {
			return biz.LogoutSessionMutationResult{}, mapPostgresError("commit idempotent session logout", err, nil)
		}
		committed = true
		return biz.LogoutSessionMutationResult{}, nil
	}
	if mutation.Audit.Action != biz.AuditActionSessionLoggedOut || mutation.Audit.Boundary != biz.AuditBoundaryPrincipal ||
		mutation.Audit.TargetType != biz.AuditTargetTypeSession || mutation.Audit.TargetID != current.Session.ID ||
		mutation.Audit.TargetVersion != current.Session.Version+1 || mutation.Audit.ActorID != current.Principal.ID {
		return biz.LogoutSessionMutationResult{}, biz.ErrInvalidPersistenceState
	}
	if err := queries.RevokeCurrentSessionRefreshTokens(ctx, sqlcgen.RevokeCurrentSessionRefreshTokensParams{SessionID: current.Session.ID}); err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("revoke current session refresh tokens", err, nil)
	}
	if err := queries.RevokeCurrentSessionFamilies(ctx, sqlcgen.RevokeCurrentSessionFamiliesParams{
		UpdatedAt: requiredTimestamptz(mutation.LoggedOutAt), SessionID: current.Session.ID,
	}); err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("revoke current session refresh families", err, nil)
	}
	if err := queries.RevokeCurrentSessionGrants(ctx, sqlcgen.RevokeCurrentSessionGrantsParams{
		UpdatedAt: requiredTimestamptz(mutation.LoggedOutAt), SessionID: current.Session.ID,
	}); err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("revoke current session grants", err, nil)
	}
	version, err := queries.RevokeCurrentSession(ctx, sqlcgen.RevokeCurrentSessionParams{
		UpdatedAt: requiredTimestamptz(mutation.LoggedOutAt), SessionID: current.Session.ID,
		ExpectedVersion: current.Session.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.LogoutSessionMutationResult{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("revoke current session", err, nil)
	}
	if version != mutation.Audit.TargetVersion {
		return biz.LogoutSessionMutationResult{}, biz.ErrInvalidPersistenceState
	}
	if err := appendPrincipalSessionAudit(ctx, queries, mutation.Audit); err != nil {
		return biz.LogoutSessionMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.LogoutSessionMutationResult{}, mapPostgresError("commit session logout", err, nil)
	}
	committed = true
	return biz.LogoutSessionMutationResult{Changed: true}, nil
}

func (u *postgresLoginUnitOfWork) SwitchTenant(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.TenantSwitchMutation,
) error {
	targetTenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin tenant switch", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockSessionContinuity(ctx, sqlcgen.LockSessionContinuityParams{SessionID: mutation.Expected.Session.ID}); err != nil {
		return mapPostgresError("serialize tenant switch", err, nil)
	}
	row, err := queries.LockTenantSwitchBoundary(ctx, sqlcgen.LockTenantSwitchBoundaryParams{
		SessionID: mutation.Expected.Session.ID, SourceTenantID: mutation.Expected.SourceTenantID,
		SourceGrantID: mutation.Expected.SourceGrant.ID, TargetTenantID: targetTenantID,
		PrincipalID: mutation.Expected.Principal.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrInvalidCredential
	}
	if err != nil {
		return mapPostgresError("lock tenant-switch boundary", err, nil)
	}
	current, err := tenantSwitchStateFromBoundary(row)
	if err != nil {
		return err
	}
	grantRow, grantErr := queries.LockActiveTargetGrant(ctx, sqlcgen.LockActiveTargetGrantParams{
		TenantID: targetTenantID, SessionID: current.Session.ID,
	})
	if grantErr == nil {
		if !grantRow.CreatedAt.Valid || !grantRow.UpdatedAt.Valid {
			return biz.ErrInvalidPersistenceState
		}
		current.TargetGrant = &biz.SessionGrant{
			ID: grantRow.ID, SessionID: current.Session.ID, MembershipID: grantRow.MembershipID,
			Status: biz.GrantStatus(grantRow.Status), Version: grantRow.Version,
			CreatedAt: grantRow.CreatedAt.Time.UTC(), UpdatedAt: grantRow.UpdatedAt.Time.UTC(),
		}
		familyRow, familyErr := queries.LockActiveRefreshFamily(ctx, sqlcgen.LockActiveRefreshFamilyParams{
			TenantID: targetTenantID, GrantID: grantRow.ID,
		})
		if familyErr == nil {
			if !familyRow.CreatedAt.Valid || !familyRow.UpdatedAt.Valid {
				return biz.ErrInvalidPersistenceState
			}
			current.TargetFamily = &biz.RefreshTokenFamily{
				ID: familyRow.ID, GrantID: familyRow.GrantID, Status: biz.GrantStatus(familyRow.Status),
				Version: familyRow.Version, CreatedAt: familyRow.CreatedAt.Time.UTC(), UpdatedAt: familyRow.UpdatedAt.Time.UTC(),
			}
		} else if !errors.Is(familyErr, pgx.ErrNoRows) {
			return mapPostgresError("lock active target refresh family", familyErr, nil)
		}
	} else if !errors.Is(grantErr, pgx.ErrNoRows) {
		return mapPostgresError("lock active target grant", grantErr, nil)
	}
	if !sameTenantSwitchState(current, mutation.Expected) {
		return biz.ErrVersionConflict
	}
	if err := validateLockedTenantSwitch(current, targetTenantID, mutation); err != nil {
		return err
	}

	if current.TargetGrant == nil {
		if err := queries.CreateSessionGrant(ctx, sqlcgen.CreateSessionGrantParams{
			TenantID: targetTenantID, ID: mutation.Grant.ID, SessionID: current.Session.ID,
			MembershipID: current.MembershipID, Status: string(mutation.Grant.Status), Version: mutation.Grant.Version,
			CreatedAt: requiredTimestamptz(mutation.Grant.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Grant.UpdatedAt),
		}); err != nil {
			return mapPostgresError("create tenant-switch grant", err, biz.ErrInvalidPersistenceState)
		}
		if err := createTenantSwitchFamily(ctx, queries, targetTenantID, mutation); err != nil {
			return err
		}
	} else if current.TargetFamily == nil {
		if err := createTenantSwitchFamily(ctx, queries, targetTenantID, mutation); err != nil {
			return err
		}
	} else {
		activeToken, err := queries.LockActiveRefreshToken(ctx, sqlcgen.LockActiveRefreshTokenParams{
			TenantID: targetTenantID, FamilyID: current.TargetFamily.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrInvalidPersistenceState
		}
		if err != nil {
			return mapPostgresError("lock active target refresh token", err, nil)
		}
		if len(activeToken.Digest) != sha256.Size || !activeToken.IssuedAt.Valid || !activeToken.ExpiresAt.Valid ||
			activeToken.Status != string(biz.RefreshTokenStatusActive) {
			return biz.ErrInvalidPersistenceState
		}
		if _, err := queries.ConsumeRefreshToken(ctx, sqlcgen.ConsumeRefreshTokenParams{
			ConsumedAt: requiredTimestamptz(mutation.SwitchedAt), ReplacedBy: requiredPGUUID(mutation.RefreshToken.ID),
			TenantID: targetTenantID, TokenID: activeToken.ID, FamilyID: current.TargetFamily.ID,
			RefreshDigest: activeToken.Digest,
		}); errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrVersionConflict
		} else if err != nil {
			return mapPostgresError("consume target refresh token", err, nil)
		}
		version, err := queries.IncrementSessionGrantVersionForSwitch(ctx, sqlcgen.IncrementSessionGrantVersionForSwitchParams{
			UpdatedAt: requiredTimestamptz(mutation.SwitchedAt), TenantID: targetTenantID,
			GrantID: current.TargetGrant.ID, SessionID: current.Session.ID, MembershipID: current.MembershipID,
			ExpectedVersion: current.TargetGrant.Version,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrVersionConflict
		}
		if err != nil {
			return mapPostgresError("increment target grant version", err, nil)
		}
		if version != mutation.Grant.Version {
			return biz.ErrInvalidPersistenceState
		}
		if err := createTenantSwitchToken(ctx, queries, targetTenantID, mutation); err != nil {
			return err
		}
	}
	if _, err := queries.UpdateSessionIdleExpiry(ctx, sqlcgen.UpdateSessionIdleExpiryParams{
		IdleExpiresAt: requiredTimestamptz(mutation.NewIdleExpiresAt), UpdatedAt: requiredTimestamptz(mutation.SwitchedAt),
		SessionID: current.Session.ID, ExpectedVersion: current.Session.Version,
	}); errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrVersionConflict
	} else if err != nil {
		return mapPostgresError("slide tenant-switch session idle expiry", err, nil)
	}
	if err := (securityAuditRepository{queries: queries, tenantID: targetTenantID}).Append(ctx, scope, mutation.Audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit tenant switch", err, nil)
	}
	committed = true
	return nil
}

func createTenantSwitchFamily(ctx context.Context, queries *sqlcgen.Queries, tenantID uuid.UUID, mutation biz.TenantSwitchMutation) error {
	if err := queries.CreateRefreshTokenFamily(ctx, sqlcgen.CreateRefreshTokenFamilyParams{
		TenantID: tenantID, ID: mutation.Family.ID, GrantID: mutation.Grant.ID,
		Status: string(mutation.Family.Status), CreatedAt: requiredTimestamptz(mutation.Family.CreatedAt),
		UpdatedAt: requiredTimestamptz(mutation.Family.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create tenant-switch refresh family", err, biz.ErrInvalidPersistenceState)
	}
	return createTenantSwitchToken(ctx, queries, tenantID, mutation)
}

func createTenantSwitchToken(ctx context.Context, queries *sqlcgen.Queries, tenantID uuid.UUID, mutation biz.TenantSwitchMutation) error {
	if err := queries.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		TenantID: tenantID, ID: mutation.RefreshToken.ID, FamilyID: mutation.Family.ID,
		Digest: mutation.RefreshToken.Digest[:], IssuedAt: requiredTimestamptz(mutation.RefreshToken.IssuedAt),
		ExpiresAt: requiredTimestamptz(mutation.RefreshToken.ExpiresAt),
	}); err != nil {
		return mapPostgresError("create tenant-switch refresh token", err, biz.ErrInvalidPersistenceState)
	}
	return nil
}

func appendPrincipalSessionAudit(ctx context.Context, queries *sqlcgen.Queries, event biz.SecurityAuditEvent) error {
	if event.ActorID == uuid.Nil || event.Boundary != biz.AuditBoundaryPrincipal {
		return biz.ErrInvalidPersistenceState
	}
	err := queries.AppendPrincipalSessionSecurityAuditEvent(ctx, sqlcgen.AppendPrincipalSessionSecurityAuditEventParams{
		EventID: event.ID, ActorID: requiredPGUUID(event.ActorID), AuthenticationMethod: string(event.AuthenticationMethod),
		Action: string(event.Action), TargetType: string(event.TargetType), TargetID: event.TargetID,
		TargetVersion: event.TargetVersion, Result: string(event.Result), Reason: string(event.Reason),
		RequestID: event.RequestID, CorrelationID: event.CorrelationID, DecisionID: event.DecisionID,
		SourceService: string(event.SourceService), OccurredAt: requiredTimestamptz(event.OccurredAt),
		RecordedAt: requiredTimestamptz(event.RecordedAt),
	})
	if err != nil {
		return mapPostgresError("append principal session audit", err, biz.ErrAuditConflict)
	}
	return nil
}

func validateLockedActiveRefresh(current biz.RefreshSessionState, mutation biz.RefreshSessionMutation) error {
	if !sameRefreshAggregate(current, mutation.Expected) || current.Token.Status != biz.RefreshTokenStatusActive ||
		current.Family.Status != biz.GrantStatusActive || current.Grant.Status != biz.GrantStatusActive ||
		current.Session.Status != biz.SessionStatusActive || !mutation.RotatedAt.Before(current.Token.ExpiresAt) ||
		!mutation.RotatedAt.Before(current.Session.IdleExpiresAt) || !mutation.RotatedAt.Before(current.Session.AbsoluteExpiry) {
		return biz.ErrInvalidCredential
	}
	if current.Principal.Status != biz.PrincipalStatusActive {
		return biz.ErrPrincipalInactive
	}
	if current.MembershipStatus != biz.MembershipStatusActive {
		return biz.ErrMembershipInactive
	}
	if current.TenantAccess != biz.TenantAccessStatusActive {
		return biz.ErrTenantAccessInactive
	}
	if !current.LifecycleFresh {
		return biz.ErrTenantLifecycleStale
	}
	if current.Lifecycle != biz.TenantLifecycleStatusActive {
		return biz.ErrTenantLifecycleBlocked
	}
	replacement := mutation.ReplacementToken
	if replacement.ID == uuid.Nil || replacement.ID.Version() != 7 || replacement.FamilyID != current.Family.ID ||
		replacement.Digest == ([sha256.Size]byte{}) || replacement.Digest == current.Token.Digest ||
		replacement.Status != biz.RefreshTokenStatusActive || !replacement.IssuedAt.Equal(mutation.RotatedAt) ||
		!replacement.ExpiresAt.Equal(current.Session.AbsoluteExpiry) || !mutation.NewIdleExpiresAt.After(mutation.RotatedAt) ||
		mutation.NewIdleExpiresAt.After(current.Session.AbsoluteExpiry) {
		return biz.ErrInvalidPersistenceState
	}
	if mutation.Audit.Action != biz.AuditActionSessionRefreshed || mutation.Audit.TargetID != current.Grant.ID ||
		mutation.Audit.TargetVersion != current.Grant.Version || mutation.Audit.ActorID != current.Principal.ID ||
		mutation.Audit.Boundary != biz.AuditBoundaryTenant {
		return biz.ErrInvalidPersistenceState
	}
	return nil
}

func sameRefreshCredential(left, right biz.RefreshSessionState) bool {
	return left.TenantID == right.TenantID && left.Principal.ID == right.Principal.ID &&
		left.Session.ID == right.Session.ID && left.Grant.ID == right.Grant.ID &&
		left.Family.ID == right.Family.ID && left.Token.ID == right.Token.ID && left.Token.Digest == right.Token.Digest
}

func sameRefreshAggregate(left, right biz.RefreshSessionState) bool {
	return sameRefreshCredential(left, right) && left.Grant.Version == right.Grant.Version && left.Family.Version == right.Family.Version &&
		left.Token.Status == right.Token.Status
}

type refreshColumns struct {
	tenantID           uuid.UUID
	principalID        uuid.UUID
	principalStatus    string
	membershipStatus   string
	tenantAccessStatus string
	lifecycleStatus    string
	lifecycleFresh     bool
	sessionID          uuid.UUID
	audience           string
	sessionStatus      string
	sessionVersion     int64
	authnMethods       []string
	deviceName         string
	idleExpiresAt      pgtype.Timestamptz
	absoluteExpiresAt  pgtype.Timestamptz
	reauthenticatedAt  pgtype.Timestamptz
	sessionCreatedAt   pgtype.Timestamptz
	sessionUpdatedAt   pgtype.Timestamptz
	grantID            uuid.UUID
	membershipID       uuid.UUID
	grantStatus        string
	grantVersion       int64
	grantCreatedAt     pgtype.Timestamptz
	grantUpdatedAt     pgtype.Timestamptz
	familyID           uuid.UUID
	familyStatus       string
	familyVersion      int64
	familyCreatedAt    pgtype.Timestamptz
	familyUpdatedAt    pgtype.Timestamptz
	tokenID            uuid.UUID
	digest             []byte
	tokenStatus        string
	issuedAt           pgtype.Timestamptz
	expiresAt          pgtype.Timestamptz
	consumedAt         pgtype.Timestamptz
	replacedBy         pgtype.UUID
}

func refreshStateFromLookup(row sqlcgen.LookupRefreshSessionRow) (biz.RefreshSessionState, error) {
	return refreshStateFromColumns(refreshColumns{
		tenantID: row.TenantID, principalID: row.PrincipalID, principalStatus: row.PrincipalStatus,
		membershipStatus: row.MembershipStatus, tenantAccessStatus: row.TenantAccessStatus,
		lifecycleStatus: row.LifecycleStatus, lifecycleFresh: row.LifecycleFresh,
		sessionID: row.SessionID, audience: row.Audience, sessionStatus: row.SessionStatus, sessionVersion: row.SessionVersion,
		authnMethods: row.AuthnMethods, deviceName: row.DeviceName, idleExpiresAt: row.IdleExpiresAt,
		absoluteExpiresAt: row.AbsoluteExpiresAt, reauthenticatedAt: row.ReauthenticatedAt,
		sessionCreatedAt: row.SessionCreatedAt, sessionUpdatedAt: row.SessionUpdatedAt,
		grantID: row.GrantID, membershipID: row.MembershipID, grantStatus: row.GrantStatus,
		grantVersion: row.GrantVersion, grantCreatedAt: row.GrantCreatedAt, grantUpdatedAt: row.GrantUpdatedAt,
		familyID: row.FamilyID, familyStatus: row.FamilyStatus, familyVersion: row.FamilyVersion,
		familyCreatedAt: row.FamilyCreatedAt, familyUpdatedAt: row.FamilyUpdatedAt,
		tokenID: row.TokenID, digest: row.Digest, tokenStatus: row.TokenStatus,
		issuedAt: row.IssuedAt, expiresAt: row.ExpiresAt, consumedAt: row.ConsumedAt, replacedBy: row.ReplacedBy,
	})
}

func refreshStateFromLock(row sqlcgen.LockRefreshSessionRow) (biz.RefreshSessionState, error) {
	return refreshStateFromColumns(refreshColumns{
		tenantID: row.TenantID, principalID: row.PrincipalID, principalStatus: row.PrincipalStatus,
		membershipStatus: row.MembershipStatus, tenantAccessStatus: row.TenantAccessStatus,
		lifecycleStatus: row.LifecycleStatus, lifecycleFresh: row.LifecycleFresh,
		sessionID: row.SessionID, audience: row.Audience, sessionStatus: row.SessionStatus, sessionVersion: row.SessionVersion,
		authnMethods: row.AuthnMethods, deviceName: row.DeviceName, idleExpiresAt: row.IdleExpiresAt,
		absoluteExpiresAt: row.AbsoluteExpiresAt, reauthenticatedAt: row.ReauthenticatedAt,
		sessionCreatedAt: row.SessionCreatedAt, sessionUpdatedAt: row.SessionUpdatedAt,
		grantID: row.GrantID, membershipID: row.MembershipID, grantStatus: row.GrantStatus,
		grantVersion: row.GrantVersion, grantCreatedAt: row.GrantCreatedAt, grantUpdatedAt: row.GrantUpdatedAt,
		familyID: row.FamilyID, familyStatus: row.FamilyStatus, familyVersion: row.FamilyVersion,
		familyCreatedAt: row.FamilyCreatedAt, familyUpdatedAt: row.FamilyUpdatedAt,
		tokenID: row.TokenID, digest: row.Digest, tokenStatus: row.TokenStatus,
		issuedAt: row.IssuedAt, expiresAt: row.ExpiresAt, consumedAt: row.ConsumedAt, replacedBy: row.ReplacedBy,
	})
}

func refreshStateFromColumns(row refreshColumns) (biz.RefreshSessionState, error) {
	if len(row.digest) != sha256.Size || !row.idleExpiresAt.Valid || !row.absoluteExpiresAt.Valid ||
		!row.reauthenticatedAt.Valid || !row.sessionCreatedAt.Valid || !row.sessionUpdatedAt.Valid ||
		!row.grantCreatedAt.Valid || !row.grantUpdatedAt.Valid || !row.familyCreatedAt.Valid ||
		!row.familyUpdatedAt.Valid || !row.issuedAt.Valid || !row.expiresAt.Valid {
		return biz.RefreshSessionState{}, biz.ErrInvalidPersistenceState
	}
	var digest [sha256.Size]byte
	copy(digest[:], row.digest)
	authnMethods := make([]biz.AuditAuthenticationMethod, len(row.authnMethods))
	for index, method := range row.authnMethods {
		authnMethods[index] = biz.AuditAuthenticationMethod(method)
	}
	token := biz.RefreshToken{
		ID: row.tokenID, FamilyID: row.familyID, Digest: digest,
		Status: biz.RefreshTokenStatus(row.tokenStatus), IssuedAt: row.issuedAt.Time.UTC(), ExpiresAt: row.expiresAt.Time.UTC(),
	}
	if row.consumedAt.Valid {
		token.ConsumedAt = row.consumedAt.Time.UTC()
	}
	if row.replacedBy.Valid {
		token.ReplacedBy = row.replacedBy.Bytes
	}
	return biz.RefreshSessionState{
		TenantID: row.tenantID, Principal: biz.Principal{ID: row.principalID, Status: biz.PrincipalStatus(row.principalStatus)},
		MembershipStatus: biz.MembershipStatus(row.membershipStatus), TenantAccess: biz.TenantAccessStatus(row.tenantAccessStatus),
		Lifecycle: biz.TenantLifecycleStatus(row.lifecycleStatus), LifecycleFresh: row.lifecycleFresh,
		Session: biz.Session{
			ID: row.sessionID, PrincipalID: row.principalID, Audience: biz.Audience(row.audience),
			Status: biz.SessionStatus(row.sessionStatus), Version: row.sessionVersion,
			AuthnMethods: authnMethods, DeviceName: row.deviceName,
			IdleExpiresAt: row.idleExpiresAt.Time.UTC(), AbsoluteExpiry: row.absoluteExpiresAt.Time.UTC(),
			ReauthenticatedAt: row.reauthenticatedAt.Time.UTC(), CreatedAt: row.sessionCreatedAt.Time.UTC(),
			UpdatedAt: row.sessionUpdatedAt.Time.UTC(),
		},
		Grant: biz.SessionGrant{
			ID: row.grantID, SessionID: row.sessionID, MembershipID: row.membershipID,
			Status: biz.GrantStatus(row.grantStatus), Version: row.grantVersion,
			CreatedAt: row.grantCreatedAt.Time.UTC(), UpdatedAt: row.grantUpdatedAt.Time.UTC(),
		},
		Family: biz.RefreshTokenFamily{
			ID: row.familyID, GrantID: row.grantID, Status: biz.GrantStatus(row.familyStatus), Version: row.familyVersion,
			CreatedAt: row.familyCreatedAt.Time.UTC(), UpdatedAt: row.familyUpdatedAt.Time.UTC(),
		},
		Token: token,
	}, nil
}

func logoutStateFromLookup(row sqlcgen.LookupLogoutSessionRow) (biz.LogoutSessionState, error) {
	return logoutState(row.PrincipalID, row.PrincipalStatus, row.SessionID, row.Audience, row.SessionStatus,
		row.SessionVersion, row.AuthnMethods, row.DeviceName, row.IdleExpiresAt, row.AbsoluteExpiresAt,
		row.ReauthenticatedAt, row.CreatedAt, row.UpdatedAt)
}

func logoutStateFromLock(row sqlcgen.LockLogoutSessionRow) (biz.LogoutSessionState, error) {
	return logoutState(row.PrincipalID, row.PrincipalStatus, row.SessionID, row.Audience, row.SessionStatus,
		row.SessionVersion, row.AuthnMethods, row.DeviceName, row.IdleExpiresAt, row.AbsoluteExpiresAt,
		row.ReauthenticatedAt, row.CreatedAt, row.UpdatedAt)
}

func logoutState(
	principalID uuid.UUID,
	principalStatus string,
	sessionID uuid.UUID,
	audience string,
	sessionStatus string,
	sessionVersion int64,
	rawAuthnMethods []string,
	deviceName string,
	idleExpiresAt, absoluteExpiresAt, reauthenticatedAt, createdAt, updatedAt pgtype.Timestamptz,
) (biz.LogoutSessionState, error) {
	if principalID == uuid.Nil || sessionID == uuid.Nil || sessionVersion <= 0 || !idleExpiresAt.Valid ||
		!absoluteExpiresAt.Valid || !reauthenticatedAt.Valid || !createdAt.Valid || !updatedAt.Valid {
		return biz.LogoutSessionState{}, biz.ErrInvalidPersistenceState
	}
	authnMethods := make([]biz.AuditAuthenticationMethod, len(rawAuthnMethods))
	for index, method := range rawAuthnMethods {
		authnMethods[index] = biz.AuditAuthenticationMethod(method)
	}
	return biz.LogoutSessionState{
		Principal: biz.Principal{ID: principalID, Status: biz.PrincipalStatus(principalStatus)},
		Session: biz.Session{
			ID: sessionID, PrincipalID: principalID, Audience: biz.Audience(audience),
			Status: biz.SessionStatus(sessionStatus), Version: sessionVersion,
			AuthnMethods: authnMethods, DeviceName: deviceName,
			IdleExpiresAt: idleExpiresAt.Time.UTC(), AbsoluteExpiry: absoluteExpiresAt.Time.UTC(),
			ReauthenticatedAt: reauthenticatedAt.Time.UTC(), CreatedAt: createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
		},
	}, nil
}

type tenantSwitchBase struct {
	sourceTenantID         uuid.UUID
	targetTenantID         uuid.UUID
	principalID            uuid.UUID
	principalStatus        string
	targetMembershipID     uuid.UUID
	targetMembershipStatus string
	targetAccessStatus     string
	targetLifecycleStatus  string
	targetLifecycleFresh   bool
	sessionID              uuid.UUID
	audience               string
	sessionStatus          string
	sessionVersion         int64
	authnMethods           []string
	deviceName             string
	idleExpiresAt          pgtype.Timestamptz
	absoluteExpiresAt      pgtype.Timestamptz
	reauthenticatedAt      pgtype.Timestamptz
	sessionCreatedAt       pgtype.Timestamptz
	sessionUpdatedAt       pgtype.Timestamptz
	sourceGrantID          uuid.UUID
	sourceMembershipID     uuid.UUID
	sourceGrantStatus      string
	sourceGrantVersion     int64
	sourceGrantCreatedAt   pgtype.Timestamptz
	sourceGrantUpdatedAt   pgtype.Timestamptz
}

func tenantSwitchStateFromLookup(row sqlcgen.LookupTenantSwitchRow) (biz.TenantSwitchState, error) {
	state, err := tenantSwitchStateFromBase(tenantSwitchBase{
		sourceTenantID: row.SourceTenantID, targetTenantID: row.TargetTenantID,
		principalID: row.PrincipalID, principalStatus: row.PrincipalStatus,
		targetMembershipID: row.TargetMembershipID, targetMembershipStatus: row.TargetMembershipStatus,
		targetAccessStatus: row.TargetAccessStatus, targetLifecycleStatus: row.TargetLifecycleStatus,
		targetLifecycleFresh: row.TargetLifecycleFresh, sessionID: row.SessionID, audience: row.Audience,
		sessionStatus: row.SessionStatus, sessionVersion: row.SessionVersion, authnMethods: row.AuthnMethods,
		deviceName: row.DeviceName, idleExpiresAt: row.IdleExpiresAt, absoluteExpiresAt: row.AbsoluteExpiresAt,
		reauthenticatedAt: row.ReauthenticatedAt, sessionCreatedAt: row.SessionCreatedAt,
		sessionUpdatedAt: row.SessionUpdatedAt, sourceGrantID: row.SourceGrantID,
		sourceMembershipID: row.SourceMembershipID, sourceGrantStatus: row.SourceGrantStatus,
		sourceGrantVersion: row.SourceGrantVersion, sourceGrantCreatedAt: row.SourceGrantCreatedAt,
		sourceGrantUpdatedAt: row.SourceGrantUpdatedAt,
	})
	if err != nil {
		return biz.TenantSwitchState{}, err
	}
	if row.TargetGrantID.Valid {
		if !row.TargetGrantStatus.Valid || !row.TargetGrantVersion.Valid || !row.TargetGrantCreatedAt.Valid || !row.TargetGrantUpdatedAt.Valid {
			return biz.TenantSwitchState{}, biz.ErrInvalidPersistenceState
		}
		state.TargetGrant = &biz.SessionGrant{
			ID: row.TargetGrantID.Bytes, SessionID: state.Session.ID, MembershipID: state.MembershipID,
			Status: biz.GrantStatus(row.TargetGrantStatus.String), Version: row.TargetGrantVersion.Int64,
			CreatedAt: row.TargetGrantCreatedAt.Time.UTC(), UpdatedAt: row.TargetGrantUpdatedAt.Time.UTC(),
		}
	}
	if row.TargetFamilyID.Valid {
		if state.TargetGrant == nil || !row.TargetFamilyStatus.Valid || !row.TargetFamilyVersion.Valid ||
			!row.TargetFamilyCreatedAt.Valid || !row.TargetFamilyUpdatedAt.Valid {
			return biz.TenantSwitchState{}, biz.ErrInvalidPersistenceState
		}
		state.TargetFamily = &biz.RefreshTokenFamily{
			ID: row.TargetFamilyID.Bytes, GrantID: state.TargetGrant.ID,
			Status: biz.GrantStatus(row.TargetFamilyStatus.String), Version: row.TargetFamilyVersion.Int64,
			CreatedAt: row.TargetFamilyCreatedAt.Time.UTC(), UpdatedAt: row.TargetFamilyUpdatedAt.Time.UTC(),
		}
	}
	return state, nil
}

func tenantSwitchStateFromBoundary(row sqlcgen.LockTenantSwitchBoundaryRow) (biz.TenantSwitchState, error) {
	return tenantSwitchStateFromBase(tenantSwitchBase{
		sourceTenantID: row.SourceTenantID, targetTenantID: row.TargetTenantID,
		principalID: row.PrincipalID, principalStatus: row.PrincipalStatus,
		targetMembershipID: row.TargetMembershipID, targetMembershipStatus: row.TargetMembershipStatus,
		targetAccessStatus: row.TargetAccessStatus, targetLifecycleStatus: row.TargetLifecycleStatus,
		targetLifecycleFresh: row.TargetLifecycleFresh, sessionID: row.SessionID, audience: row.Audience,
		sessionStatus: row.SessionStatus, sessionVersion: row.SessionVersion, authnMethods: row.AuthnMethods,
		deviceName: row.DeviceName, idleExpiresAt: row.IdleExpiresAt, absoluteExpiresAt: row.AbsoluteExpiresAt,
		reauthenticatedAt: row.ReauthenticatedAt, sessionCreatedAt: row.SessionCreatedAt,
		sessionUpdatedAt: row.SessionUpdatedAt, sourceGrantID: row.SourceGrantID,
		sourceMembershipID: row.SourceMembershipID, sourceGrantStatus: row.SourceGrantStatus,
		sourceGrantVersion: row.SourceGrantVersion, sourceGrantCreatedAt: row.SourceGrantCreatedAt,
		sourceGrantUpdatedAt: row.SourceGrantUpdatedAt,
	})
}

func tenantSwitchStateFromBase(row tenantSwitchBase) (biz.TenantSwitchState, error) {
	if row.sourceTenantID == uuid.Nil || row.targetTenantID == uuid.Nil || row.principalID == uuid.Nil ||
		row.targetMembershipID == uuid.Nil || row.sessionID == uuid.Nil || row.sessionVersion <= 0 ||
		row.sourceGrantID == uuid.Nil || row.sourceMembershipID == uuid.Nil || row.sourceGrantVersion <= 0 ||
		!row.idleExpiresAt.Valid || !row.absoluteExpiresAt.Valid || !row.reauthenticatedAt.Valid ||
		!row.sessionCreatedAt.Valid || !row.sessionUpdatedAt.Valid || !row.sourceGrantCreatedAt.Valid || !row.sourceGrantUpdatedAt.Valid {
		return biz.TenantSwitchState{}, biz.ErrInvalidPersistenceState
	}
	authnMethods := make([]biz.AuditAuthenticationMethod, len(row.authnMethods))
	for index, method := range row.authnMethods {
		authnMethods[index] = biz.AuditAuthenticationMethod(method)
	}
	return biz.TenantSwitchState{
		SourceTenantID: row.sourceTenantID, TargetTenantID: row.targetTenantID,
		Principal:    biz.Principal{ID: row.principalID, Status: biz.PrincipalStatus(row.principalStatus)},
		MembershipID: row.targetMembershipID, MembershipStatus: biz.MembershipStatus(row.targetMembershipStatus),
		TenantAccess: biz.TenantAccessStatus(row.targetAccessStatus), Lifecycle: biz.TenantLifecycleStatus(row.targetLifecycleStatus),
		LifecycleFresh: row.targetLifecycleFresh,
		Session: biz.Session{
			ID: row.sessionID, PrincipalID: row.principalID, Audience: biz.Audience(row.audience),
			Status: biz.SessionStatus(row.sessionStatus), Version: row.sessionVersion,
			AuthnMethods: authnMethods, DeviceName: row.deviceName,
			IdleExpiresAt: row.idleExpiresAt.Time.UTC(), AbsoluteExpiry: row.absoluteExpiresAt.Time.UTC(),
			ReauthenticatedAt: row.reauthenticatedAt.Time.UTC(), CreatedAt: row.sessionCreatedAt.Time.UTC(),
			UpdatedAt: row.sessionUpdatedAt.Time.UTC(),
		},
		SourceGrant: biz.SessionGrant{
			ID: row.sourceGrantID, SessionID: row.sessionID, MembershipID: row.sourceMembershipID,
			Status: biz.GrantStatus(row.sourceGrantStatus), Version: row.sourceGrantVersion,
			CreatedAt: row.sourceGrantCreatedAt.Time.UTC(), UpdatedAt: row.sourceGrantUpdatedAt.Time.UTC(),
		},
	}, nil
}

func sameTenantSwitchState(left, right biz.TenantSwitchState) bool {
	if left.SourceTenantID != right.SourceTenantID || left.TargetTenantID != right.TargetTenantID ||
		left.Principal.ID != right.Principal.ID || left.MembershipID != right.MembershipID ||
		left.Session.ID != right.Session.ID || left.Session.Version != right.Session.Version ||
		left.SourceGrant.ID != right.SourceGrant.ID || left.SourceGrant.Version != right.SourceGrant.Version ||
		(left.TargetGrant == nil) != (right.TargetGrant == nil) || (left.TargetFamily == nil) != (right.TargetFamily == nil) {
		return false
	}
	if left.TargetGrant != nil && (left.TargetGrant.ID != right.TargetGrant.ID || left.TargetGrant.Version != right.TargetGrant.Version) {
		return false
	}
	if left.TargetFamily != nil && (left.TargetFamily.ID != right.TargetFamily.ID || left.TargetFamily.Version != right.TargetFamily.Version) {
		return false
	}
	return true
}

func validateLockedTenantSwitch(current biz.TenantSwitchState, tenantID uuid.UUID, mutation biz.TenantSwitchMutation) error {
	now := mutation.SwitchedAt.UTC()
	if current.TargetTenantID != tenantID || current.Principal.Status != biz.PrincipalStatusActive ||
		current.MembershipStatus != biz.MembershipStatusActive || current.TenantAccess != biz.TenantAccessStatusActive ||
		current.Lifecycle != biz.TenantLifecycleStatusActive || !current.LifecycleFresh ||
		current.Session.Status != biz.SessionStatusActive || current.SourceGrant.Status != biz.GrantStatusActive ||
		!now.Before(current.Session.IdleExpiresAt) || !now.Before(current.Session.AbsoluteExpiry) {
		return biz.ErrInvalidCredential
	}
	if mutation.Grant.ID == uuid.Nil || mutation.Grant.SessionID != current.Session.ID ||
		mutation.Grant.MembershipID != current.MembershipID || mutation.Grant.Status != biz.GrantStatusActive ||
		mutation.Family.ID == uuid.Nil || mutation.Family.GrantID != mutation.Grant.ID ||
		mutation.Family.Status != biz.GrantStatusActive || mutation.RefreshToken.ID == uuid.Nil ||
		mutation.RefreshToken.FamilyID != mutation.Family.ID || mutation.RefreshToken.Status != biz.RefreshTokenStatusActive ||
		mutation.RefreshToken.Digest == ([sha256.Size]byte{}) || !mutation.RefreshToken.IssuedAt.Equal(now) ||
		!mutation.RefreshToken.ExpiresAt.Equal(current.Session.AbsoluteExpiry) ||
		!mutation.NewIdleExpiresAt.After(now) || mutation.NewIdleExpiresAt.After(current.Session.AbsoluteExpiry) {
		return biz.ErrInvalidPersistenceState
	}
	switch {
	case current.TargetGrant == nil:
		if mutation.Grant.Version != 1 || mutation.Family.Version != 1 || mutation.Grant.ID.Version() != 7 || mutation.Family.ID.Version() != 7 {
			return biz.ErrInvalidPersistenceState
		}
	case current.TargetFamily == nil:
		if mutation.Grant.ID != current.TargetGrant.ID || mutation.Grant.Version != current.TargetGrant.Version ||
			mutation.Family.Version != 1 || mutation.Family.ID.Version() != 7 {
			return biz.ErrInvalidPersistenceState
		}
	default:
		if mutation.Grant.ID != current.TargetGrant.ID || mutation.Grant.Version != current.TargetGrant.Version+1 ||
			mutation.Family.ID != current.TargetFamily.ID || mutation.Family.Version != current.TargetFamily.Version {
			return biz.ErrInvalidPersistenceState
		}
	}
	if mutation.Audit.Action != biz.AuditActionTenantSwitched || mutation.Audit.Boundary != biz.AuditBoundaryTenant ||
		mutation.Audit.TargetID != mutation.Grant.ID || mutation.Audit.TargetVersion != mutation.Grant.Version ||
		mutation.Audit.ActorID != current.Principal.ID {
		return biz.ErrInvalidPersistenceState
	}
	return nil
}
