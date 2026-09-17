package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"slices"
	"time"
)

func recoveryCapability(cap biz.PlatformCapability, allowed ...string) error {
	_, op, err := cap.CredentialBinding()
	if err != nil || !slices.Contains(allowed, op) {
		return biz.ErrPlatformAdministrationDenied
	}
	return nil
}
func (t *platformAdministrationTransaction) GetRecovery(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.TenantAdminRecoveryOperation, error) {
	if err := recoveryCapability(cap, "requestRestoreTenantAdmin", "approveRestoreTenantAdmin", "executeRestoreTenantAdmin"); err != nil {
		return biz.TenantAdminRecoveryOperation{}, err
	}
	row, err := t.q.GetTenantAdminRecovery(ctx, sqlcgen.GetTenantAdminRecoveryParams{ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAdminRecoveryOperation{}, biz.ErrRecoveryNotFound
	}
	if err != nil {
		return biz.TenantAdminRecoveryOperation{}, mapPostgresError("read administrator recovery", err, nil)
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid || !row.ExpiresAt.Valid {
		return biz.TenantAdminRecoveryOperation{}, biz.ErrInvalidPersistenceState
	}
	r := biz.TenantAdminRecoveryOperation{ID: row.ID, TenantID: row.TenantID, TargetPrincipalID: row.TargetPrincipalID, RequesterPrincipalID: row.RequesterPrincipalID, ReasonCode: row.ReasonCode, PayloadFingerprint: row.PayloadFingerprint, Status: biz.RecoveryStatus(row.Status), Version: row.Version, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC()}
	if row.ApproverPrincipalID.Valid {
		r.ApproverPrincipalID = uuid.UUID(row.ApproverPrincipalID.Bytes)
	}
	if row.ApprovalReference.Valid {
		r.ApprovalReference = row.ApprovalReference.String
	}
	if row.ApprovedAt.Valid {
		r.ApprovedAt = row.ApprovedAt.Time.UTC()
	}
	if row.ExecutedAt.Valid {
		r.ExecutedAt = row.ExecutedAt.Time.UTC()
	}
	if row.MembershipID.Valid {
		r.MembershipID = uuid.UUID(row.MembershipID.Bytes)
	}
	return r, nil
}
func (t *platformAdministrationTransaction) CreateRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation) error {
	if err := recoveryCapability(cap, "requestRestoreTenantAdmin"); err != nil {
		return err
	}
	claims, _, _ := cap.CredentialBinding()
	if r.RequesterPrincipalID != claims.Subject || r.Status != biz.RecoveryPendingApproval || r.Version != 1 {
		return biz.ErrInvalidPersistenceState
	}
	return mapPostgresError("create administrator recovery", t.q.CreateTenantAdminRecovery(ctx, sqlcgen.CreateTenantAdminRecoveryParams{TenantID: r.TenantID, ID: r.ID, TargetPrincipalID: r.TargetPrincipalID, RequesterPrincipalID: r.RequesterPrincipalID, ReasonCode: r.ReasonCode, PayloadFingerprint: r.PayloadFingerprint, Now: requiredTimestamptz(r.CreatedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)}), biz.ErrRecoveryConflict)
}
func (t *platformAdministrationTransaction) ApproveRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, version int64) error {
	if err := recoveryCapability(cap, "approveRestoreTenantAdmin"); err != nil {
		return err
	}
	claims, _, _ := cap.CredentialBinding()
	if r.ApproverPrincipalID != claims.Subject || r.Status != biz.RecoveryApproved || r.Version != version+1 {
		return biz.ErrInvalidPersistenceState
	}
	if err := t.q.ReserveRestoreRecoveryApproval(ctx, sqlcgen.ReserveRestoreRecoveryApprovalParams{ApprovalReference: r.ApprovalReference, TenantID: r.TenantID, OperationID: requiredPGUUID(r.ID), Now: requiredTimestamptz(r.ApprovedAt)}); err != nil {
		return mapPostgresError("reserve exact Restore approval reference", err, biz.ErrRecoveryConflict)
	}
	n, err := t.q.ApproveTenantAdminRecovery(ctx, sqlcgen.ApproveTenantAdminRecoveryParams{TenantID: r.TenantID, ID: r.ID, ExpectedVersion: version, ApproverPrincipalID: requiredPGUUID(r.ApproverPrincipalID), ApprovalReference: pgtype.Text{String: r.ApprovalReference, Valid: true}, Now: requiredTimestamptz(r.ApprovedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)})
	if err != nil {
		return mapPostgresError("approve administrator recovery", err, biz.ErrRecoveryConflict)
	}
	if n != 1 {
		return biz.ErrRecoveryConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) CompleteRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, version int64) error {
	if err := recoveryCapability(cap, "executeRestoreTenantAdmin"); err != nil {
		return err
	}
	if r.Status != biz.RecoveryExecuted || r.Version != version+1 || r.MembershipID == uuid.Nil {
		return biz.ErrInvalidPersistenceState
	}
	n, err := t.q.ExecuteTenantAdminRecovery(ctx, sqlcgen.ExecuteTenantAdminRecoveryParams{TenantID: r.TenantID, ID: r.ID, ExpectedVersion: version, ApprovalReference: pgtype.Text{String: r.ApprovalReference, Valid: true}, MembershipID: requiredPGUUID(r.MembershipID), Now: requiredTimestamptz(r.ExecutedAt)})
	if err != nil {
		return mapPostgresError("consume administrator recovery approval", err, nil)
	}
	if n != 1 {
		return biz.ErrRecoveryConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) ValidateRecoveryTarget(ctx context.Context, cap biz.PlatformCapability, tenant, target uuid.UUID, policy biz.TenantAdminLoginPolicy, now time.Time) error {
	if err := recoveryCapability(cap, "requestRestoreTenantAdmin", "approveRestoreTenantAdmin", "executeRestoreTenantAdmin"); err != nil {
		return err
	}
	if tenant == uuid.Nil || target == uuid.Nil {
		return biz.ErrRecoveryInvalid
	}
	if err := t.q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
		return mapPostgresError("lock recovery Tenant guard", err, nil)
	}
	access, err := t.q.GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrTenantAccessNotFound
	}
	if err != nil {
		return mapPostgresError("read recovery Tenant access", err, nil)
	}
	if access.Status == "bootstrap_pending" {
		return biz.ErrRecoveryConflict
	}
	if _, err = t.q.TenantAdminRecoveryRole(ctx, sqlcgen.TenantAdminRecoveryRoleParams{TenantID: tenant}); errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrRecoveryConflict
	} else if err != nil {
		return mapPostgresError("read recovery builtin role", err, nil)
	}
	count, err := t.q.CountActiveHumanTenantAdministrators(ctx, sqlcgen.CountActiveHumanTenantAdministratorsParams{TenantID: tenant, OidcProvider: policy.OIDCProvider, OidcIssuer: policy.OIDCIssuer, PasswordEnabled: policy.PasswordEnabled})
	if err != nil {
		return mapPostgresError("check lost administrator condition", err, nil)
	}
	if count != 0 {
		return biz.ErrRecoveryConflict
	}
	candidate, err := t.q.TenantAdminRecoveryLoginTarget(ctx, sqlcgen.TenantAdminRecoveryLoginTargetParams{PrincipalID: target, OidcProvider: policy.OIDCProvider, OidcIssuer: policy.OIDCIssuer, PasswordEnabled: policy.PasswordEnabled, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrRecoveryConflict
	}
	if err != nil {
		return mapPostgresError("check recovery target identity", err, nil)
	}
	if !candidate.LoginCapable {
		return biz.ErrRecoveryConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) RecoveryReauthentication(ctx context.Context, cap biz.PlatformCapability, proof biz.AccessTokenClaims) (time.Time, error) {
	if err := recoveryCapability(cap, "executeRestoreTenantAdmin", "executeRecoveryBootstrap"); err != nil {
		return time.Time{}, err
	}
	claims, _, _ := cap.CredentialBinding()
	if claims.Subject != proof.Subject || proof.Boundary != biz.AccessBoundaryPlatform || proof.Audience != biz.AudienceBoss || proof.TenantID != uuid.Nil {
		return time.Time{}, biz.ErrPlatformAdministrationDenied
	}
	authenticatedAt, err := t.q.LockPlatformIdentityLinkAuthentication(ctx, sqlcgen.LockPlatformIdentityLinkAuthenticationParams{PrincipalID: proof.Subject, GrantID: proof.GrantID, GrantVersion: proof.GrantVersion, SessionID: proof.SessionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return time.Time{}, mapPostgresError("check recovery current reauthentication", err, nil)
	}
	if !authenticatedAt.Valid {
		return time.Time{}, biz.ErrInvalidPersistenceState
	}
	return authenticatedAt.Time.UTC(), nil
}
func (t *platformAdministrationTransaction) RestoreRecoveryAdministrator(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, newMemberID, newBindingID uuid.UUID, now time.Time) (biz.TenantMembershipRecord, error) {
	if err := recoveryCapability(cap, "executeRestoreTenantAdmin"); err != nil {
		return biz.TenantMembershipRecord{}, err
	}
	stored, err := t.GetRecovery(ctx, cap, r.ID)
	if err != nil {
		return biz.TenantMembershipRecord{}, err
	}
	if stored.TenantID != r.TenantID || stored.TargetPrincipalID != r.TargetPrincipalID || stored.Status != biz.RecoveryApproved || stored.Version != r.Version || !now.Before(stored.ExpiresAt) {
		return biz.TenantMembershipRecord{}, biz.ErrRecoveryConflict
	}
	role, err := t.q.TenantAdminRecoveryRole(ctx, sqlcgen.TenantAdminRecoveryRoleParams{TenantID: r.TenantID})
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("load approved administrator role", err, nil)
	}
	member, err := t.q.TenantAdminRecoveryMembership(ctx, sqlcgen.TenantAdminRecoveryMembershipParams{TenantID: r.TenantID, PrincipalID: r.TargetPrincipalID})
	memberID := member.ID
	if errors.Is(err, pgx.ErrNoRows) {
		memberID = newMemberID
		err = t.q.CreateTenantMembership(ctx, sqlcgen.CreateTenantMembershipParams{TenantID: r.TenantID, ID: memberID, PrincipalID: r.TargetPrincipalID, Status: "active", Version: 1, CreatedAt: requiredTimestamptz(now), UpdatedAt: requiredTimestamptz(now)})
	} else if err == nil {
		_, err = t.q.UpdateTenantMembershipStatus(ctx, sqlcgen.UpdateTenantMembershipStatusParams{TenantID: r.TenantID, ID: memberID, ExpectedVersion: member.Version, Status: "active", UpdatedAt: requiredTimestamptz(now)})
	}
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("establish recovered membership", err, nil)
	}
	roleIDs, err := t.q.ListTenantAuthorizationMembershipRoleIDs(ctx, sqlcgen.ListTenantAuthorizationMembershipRoleIDsParams{TenantID: r.TenantID, MembershipID: memberID})
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("read recovered membership roles", err, nil)
	}
	if !slices.Contains(roleIDs, role.ID) {
		err = t.q.CreateTenantRoleBinding(ctx, sqlcgen.CreateTenantRoleBindingParams{TenantID: r.TenantID, ID: newBindingID, MembershipID: memberID, RoleID: role.ID, Version: 1, CreatedAt: requiredTimestamptz(now), UpdatedAt: requiredTimestamptz(now)})
		if err != nil {
			return biz.TenantMembershipRecord{}, mapPostgresError("bind recovered administrator", err, nil)
		}
	}
	row, err := t.q.GetTenantAuthorizationMembership(ctx, sqlcgen.GetTenantAuthorizationMembershipParams{TenantID: r.TenantID, ID: memberID})
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("read recovered membership", err, nil)
	}
	return membershipRecord(ctx, t.q, r.TenantID, row.ID, row.PrincipalID, row.PrincipalType, row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}
func (t *platformAdministrationTransaction) AppendRecoveryEffectAudit(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, audit biz.SecurityAuditEvent) error {
	if err := recoveryCapability(cap, "executeRestoreTenantAdmin"); err != nil {
		return err
	}
	stored, err := t.GetRecovery(ctx, cap, r.ID)
	if err != nil {
		return err
	}
	if stored.TenantID != r.TenantID || stored.TargetPrincipalID != r.TargetPrincipalID || stored.Status != biz.RecoveryExecuted || stored.MembershipID != audit.TargetID || audit.Boundary != biz.AuditBoundaryTenant {
		return biz.ErrInvalidPersistenceState
	}
	// Exact Tenant came from the locked, approved persistent operation, never
	// from a path/body field or an ordinary Platform administrator Boolean.
	scope, err := biz.NewTenantScope(stored.TenantID)
	if err != nil {
		return err
	}
	return (securityAuditRepository{queries: t.q, tenantID: stored.TenantID}).Append(ctx, scope, audit)
}
