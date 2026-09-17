package data

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"slices"
	"time"
)

func (t *platformAdministrationTransaction) GetBootstrapRecovery(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID) (biz.TenantAdminRecoveryOperation, error) {
	if err := recoveryCapability(cap, "requestRecoveryBootstrap", "approveRecoveryBootstrap", "executeRecoveryBootstrap"); err != nil {
		return biz.TenantAdminRecoveryOperation{}, err
	}
	row, err := t.q.GetTenantBootstrapRecovery(ctx, sqlcgen.GetTenantBootstrapRecoveryParams{ID: id})
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
func (t *platformAdministrationTransaction) CreateBootstrapRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation) error {
	if err := recoveryCapability(cap, "requestRecoveryBootstrap"); err != nil {
		return err
	}
	claims, _, _ := cap.CredentialBinding()
	if r.RequesterPrincipalID != claims.Subject || r.Status != biz.RecoveryPendingApproval || r.Version != 1 {
		return biz.ErrInvalidPersistenceState
	}
	return mapPostgresError("create administrator recovery", t.q.CreateTenantBootstrapRecovery(ctx, sqlcgen.CreateTenantBootstrapRecoveryParams{TenantID: r.TenantID, ID: r.ID, TargetPrincipalID: r.TargetPrincipalID, RequesterPrincipalID: r.RequesterPrincipalID, ReasonCode: r.ReasonCode, PayloadFingerprint: r.PayloadFingerprint, Now: requiredTimestamptz(r.CreatedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)}), biz.ErrRecoveryConflict)
}
func (t *platformAdministrationTransaction) ApproveBootstrapRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, version int64) error {
	if err := recoveryCapability(cap, "approveRecoveryBootstrap"); err != nil {
		return err
	}
	claims, _, _ := cap.CredentialBinding()
	if r.ApproverPrincipalID != claims.Subject || r.Status != biz.RecoveryApproved || r.Version != version+1 {
		return biz.ErrInvalidPersistenceState
	}
	if err := t.q.ReserveBootstrapRecoveryApproval(ctx, sqlcgen.ReserveBootstrapRecoveryApprovalParams{ApprovalReference: r.ApprovalReference, TenantID: r.TenantID, OperationID: requiredPGUUID(r.ID), Now: requiredTimestamptz(r.ApprovedAt)}); err != nil {
		return mapPostgresError("reserve exact Bootstrap approval reference", err, biz.ErrRecoveryConflict)
	}
	n, err := t.q.ApproveTenantBootstrapRecovery(ctx, sqlcgen.ApproveTenantBootstrapRecoveryParams{TenantID: r.TenantID, ID: r.ID, ExpectedVersion: version, ApproverPrincipalID: requiredPGUUID(r.ApproverPrincipalID), ApprovalReference: pgtype.Text{String: r.ApprovalReference, Valid: true}, Now: requiredTimestamptz(r.ApprovedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)})
	if err != nil {
		return mapPostgresError("approve administrator recovery", err, biz.ErrRecoveryConflict)
	}
	if n != 1 {
		return biz.ErrRecoveryConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) CompleteBootstrapRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, version int64) error {
	if err := recoveryCapability(cap, "executeRecoveryBootstrap"); err != nil {
		return err
	}
	if r.Status != biz.RecoveryExecuted || r.Version != version+1 || r.MembershipID == uuid.Nil {
		return biz.ErrInvalidPersistenceState
	}
	n, err := t.q.ExecuteTenantBootstrapRecovery(ctx, sqlcgen.ExecuteTenantBootstrapRecoveryParams{TenantID: r.TenantID, ID: r.ID, ExpectedVersion: version, ApprovalReference: pgtype.Text{String: r.ApprovalReference, Valid: true}, MembershipID: requiredPGUUID(r.MembershipID), Now: requiredTimestamptz(r.ExecutedAt)})
	if err != nil {
		return mapPostgresError("consume administrator recovery approval", err, nil)
	}
	if n != 1 {
		return biz.ErrRecoveryConflict
	}
	return nil
}
func (t *platformAdministrationTransaction) ValidateBootstrapRecoveryTarget(ctx context.Context, cap biz.PlatformCapability, tenant, target uuid.UUID, policy biz.TenantAdminLoginPolicy, now time.Time) error {
	if err := recoveryCapability(cap, "requestRecoveryBootstrap", "approveRecoveryBootstrap", "executeRecoveryBootstrap"); err != nil {
		return err
	}
	if err := t.q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
		return mapPostgresError("lock Bootstrap target", err, nil)
	}
	access, err := t.q.GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: tenant})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return mapPostgresError("read Bootstrap Access", err, nil)
	}
	if err == nil && access.Status != "bootstrap_pending" {
		return biz.ErrRecoveryConflict
	}
	lifecycle, err := t.q.InvitationTargetLifecycle(ctx, sqlcgen.InvitationTargetLifecycleParams{TenantID: tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrTenantLifecycleStale
	}
	if err != nil {
		return mapPostgresError("read Bootstrap Core projection", err, nil)
	}
	if !lifecycle.Fresh {
		return biz.ErrTenantLifecycleStale
	}
	if lifecycle.Status != "active" {
		return biz.ErrTenantLifecycleBlocked
	}
	candidate, err := t.q.TenantAdminRecoveryLoginTarget(ctx, sqlcgen.TenantAdminRecoveryLoginTargetParams{PrincipalID: target, OidcProvider: policy.OIDCProvider, OidcIssuer: policy.OIDCIssuer, PasswordEnabled: policy.PasswordEnabled, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrRecoveryConflict
	}
	if err != nil {
		return mapPostgresError("check Bootstrap intended Human", err, nil)
	}
	if !candidate.LoginCapable {
		return biz.ErrRecoveryConflict
	}
	email, err := t.q.BootstrapRecoveryVerifiedEmail(ctx, sqlcgen.BootstrapRecoveryVerifiedEmailParams{PrincipalID: target})
	if err != nil {
		return mapPostgresError("read Bootstrap verified identity", err, nil)
	}
	original, err := t.q.GetCurrentTenantBootstrap(ctx, sqlcgen.GetCurrentTenantBootstrapParams{TenantID: tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return mapPostgresError("read original Bootstrap identity", err, nil)
	}
	// A known original payload for this identity must retain its own operation.
	if original.Status == "succeeded" || original.IntendedEmail == email {
		return biz.ErrRecoveryConflict
	}
	return nil
}

func (t *platformAdministrationTransaction) ApplyBootstrapRecovery(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, newRoleID, newMemberID, newBindingID uuid.UUID, permissions []biz.Permission, now time.Time) (biz.BootstrapRecoveryEffects, error) {
	var result biz.BootstrapRecoveryEffects
	if err := recoveryCapability(cap, "executeRecoveryBootstrap"); err != nil {
		return result, err
	}
	stored, err := t.GetBootstrapRecovery(ctx, cap, r.ID)
	if err != nil {
		return result, err
	}
	if stored.TenantID != r.TenantID || stored.TargetPrincipalID != r.TargetPrincipalID || stored.Status != biz.RecoveryApproved || stored.Version != r.Version || !now.Before(stored.ExpiresAt) {
		return result, biz.ErrRecoveryConflict
	}
	if len(permissions) == 0 {
		return result, biz.ErrInvalidPersistenceState
	}
	wanted := map[string]bool{}
	for _, p := range permissions {
		if p.Scope != biz.PermissionScopeTenant {
			return result, biz.ErrInvalidPersistenceState
		}
		wanted[p.Resource+"/"+p.Action] = true
	}
	email, err := t.q.BootstrapRecoveryVerifiedEmail(ctx, sqlcgen.BootstrapRecoveryVerifiedEmailParams{PrincipalID: r.TargetPrincipalID})
	if err != nil {
		return result, mapPostgresError("read approved Bootstrap identity", err, nil)
	}
	original, err := t.q.GetCurrentTenantBootstrap(ctx, sqlcgen.GetCurrentTenantBootstrapParams{TenantID: r.TenantID})
	var supersedes pgtype.UUID
	if err == nil {
		if original.Status == "succeeded" || original.IntendedEmail == email {
			return result, biz.ErrRecoveryConflict
		}
		n, e := t.q.SupersedeTenantBootstrap(ctx, sqlcgen.SupersedeTenantBootstrapParams{TenantID: r.TenantID, ID: original.ID, ExpectedVersion: original.Version, RecoveryID: requiredPGUUID(r.ID), Now: requiredTimestamptz(now)})
		if e != nil {
			return result, mapPostgresError("supersede original Bootstrap", e, nil)
		}
		if n != 1 {
			return result, biz.ErrRecoveryConflict
		}
		supersedes = requiredPGUUID(original.ID)
		cancelled, e := t.q.CancelSupersededBootstrapInvitations(ctx, sqlcgen.CancelSupersededBootstrapInvitationsParams{TenantID: r.TenantID, OperationID: requiredPGUUID(original.ID), Now: requiredTimestamptz(now)})
		if e != nil {
			return result, mapPostgresError("cancel replaced Bootstrap intent", e, nil)
		}
		for _, inv := range cancelled {
			if e = t.q.ClearTenantInvitationRoles(ctx, sqlcgen.ClearTenantInvitationRolesParams{TenantID: r.TenantID, InvitationID: inv.ID}); e != nil {
				return result, mapPostgresError("release replaced Bootstrap role references", e, nil)
			}
			if e = t.q.CancelTenantInvitationDeliveries(ctx, sqlcgen.CancelTenantInvitationDeliveriesParams{TenantID: r.TenantID, InvitationID: inv.ID, UpdatedAt: requiredTimestamptz(now)}); e != nil {
				return result, mapPostgresError("cancel replaced Bootstrap delivery", e, nil)
			}
			result.CancelledInvitations = append(result.CancelledInvitations, biz.BootstrapCancelledInvitation{ID: inv.ID, Version: inv.Version})
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("lock current Bootstrap", err, nil)
	}
	if err = t.q.CreateBootstrapPendingAccess(ctx, sqlcgen.CreateBootstrapPendingAccessParams{TenantID: r.TenantID, Now: requiredTimestamptz(now)}); err != nil {
		return result, mapPostgresError("initialize approved IAM Access", err, nil)
	}
	role, err := t.q.TenantAdminRecoveryRole(ctx, sqlcgen.TenantAdminRecoveryRoleParams{TenantID: r.TenantID})
	roleID := role.ID
	if errors.Is(err, pgx.ErrNoRows) {
		roleID = newRoleID
		if err = t.q.CreateBootstrapAdministratorRole(ctx, sqlcgen.CreateBootstrapAdministratorRoleParams{TenantID: r.TenantID, ID: roleID, Now: requiredTimestamptz(now)}); err != nil {
			return result, mapPostgresError("initialize Bootstrap builtin Role", err, nil)
		}
		for _, p := range permissions {
			if err = t.q.AddCustomTenantRolePermission(ctx, sqlcgen.AddCustomTenantRolePermissionParams{TenantID: r.TenantID, RoleID: roleID, Resource: p.Resource, Action: p.Action, CreatedAt: requiredTimestamptz(now)}); err != nil {
				return result, mapPostgresError("materialize Bootstrap catalog", err, nil)
			}
		}
	} else if err != nil {
		return result, mapPostgresError("read Bootstrap builtin Role", err, nil)
	} else {
		if !role.SystemRole || role.SystemDefinitionVersion != 1 {
			return result, biz.ErrRecoveryConflict
		}
		rows, e := t.q.ListTenantAuthorizationRolePermissions(ctx, sqlcgen.ListTenantAuthorizationRolePermissionsParams{TenantID: r.TenantID, RoleID: roleID})
		if e != nil {
			return result, mapPostgresError("read existing builtin definition", e, nil)
		}
		if len(rows) != len(wanted) {
			return result, biz.ErrRecoveryConflict
		}
		for _, p := range rows {
			if !wanted[p.Resource+"/"+p.Action] {
				return result, biz.ErrRecoveryConflict
			}
		}
	}
	member, err := t.q.TenantAdminRecoveryMembership(ctx, sqlcgen.TenantAdminRecoveryMembershipParams{TenantID: r.TenantID, PrincipalID: r.TargetPrincipalID})
	memberID := member.ID
	if errors.Is(err, pgx.ErrNoRows) {
		memberID = newMemberID
		err = t.q.CreateTenantMembership(ctx, sqlcgen.CreateTenantMembershipParams{TenantID: r.TenantID, ID: memberID, PrincipalID: r.TargetPrincipalID, Status: "active", Version: 1, CreatedAt: requiredTimestamptz(now), UpdatedAt: requiredTimestamptz(now)})
	} else if err == nil && member.Status != "active" {
		_, err = t.q.UpdateTenantMembershipStatus(ctx, sqlcgen.UpdateTenantMembershipStatusParams{TenantID: r.TenantID, ID: memberID, ExpectedVersion: member.Version, Status: "active", UpdatedAt: requiredTimestamptz(now)})
	}
	if err != nil {
		return result, mapPostgresError("establish Bootstrap Membership", err, nil)
	}
	roleIDs, err := t.q.ListTenantAuthorizationMembershipRoleIDs(ctx, sqlcgen.ListTenantAuthorizationMembershipRoleIDsParams{TenantID: r.TenantID, MembershipID: memberID})
	if err != nil {
		return result, mapPostgresError("read Bootstrap Membership roles", err, nil)
	}
	if !slices.Contains(roleIDs, roleID) {
		if err = t.q.CreateTenantRoleBinding(ctx, sqlcgen.CreateTenantRoleBindingParams{TenantID: r.TenantID, ID: newBindingID, MembershipID: memberID, RoleID: roleID, Version: 1, CreatedAt: requiredTimestamptz(now), UpdatedAt: requiredTimestamptz(now)}); err != nil {
			return result, mapPostgresError("bind Bootstrap administrator", err, nil)
		}
	}
	access, err := t.q.ActivateRecoveryBootstrapAccess(ctx, sqlcgen.ActivateRecoveryBootstrapAccessParams{TenantID: r.TenantID, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, biz.ErrRecoveryConflict
	}
	if err != nil {
		return result, mapPostgresError("activate approved IAM Access", err, nil)
	}
	if !access.CreatedAt.Valid || !access.UpdatedAt.Valid {
		return result, biz.ErrInvalidPersistenceState
	}
	result.Access = biz.TenantAccess{Status: biz.TenantAccessStatus(access.Status), Version: access.Version, CreatedAt: access.CreatedAt.Time.UTC(), UpdatedAt: access.UpdatedAt.Time.UTC()}
	payload, err := json.Marshal(struct {
		TenantID, IntendedPrincipalID uuid.UUID
		ReasonCode                    string
	}{r.TenantID, r.TargetPrincipalID, r.ReasonCode})
	if err != nil {
		return result, biz.ErrInvalidPersistenceState
	}
	if err = t.q.CreateRecoveredTenantBootstrap(ctx, sqlcgen.CreateRecoveredTenantBootstrapParams{TenantID: r.TenantID, ID: r.ID, IntendedEmail: email, PrincipalID: requiredPGUUID(r.TargetPrincipalID), PayloadFingerprint: r.PayloadFingerprint, Payload: payload, Supersedes: supersedes, MembershipID: requiredPGUUID(memberID), Now: requiredTimestamptz(now)}); err != nil {
		return result, mapPostgresError("record distinct recovered Bootstrap", err, nil)
	}
	row, err := t.q.GetTenantAuthorizationMembership(ctx, sqlcgen.GetTenantAuthorizationMembershipParams{TenantID: r.TenantID, ID: memberID})
	if err != nil {
		return result, mapPostgresError("read Bootstrap result", err, nil)
	}
	result.Membership, err = membershipRecord(ctx, t.q, r.TenantID, row.ID, row.PrincipalID, row.PrincipalType, row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
	return result, err
}
func (t *platformAdministrationTransaction) AppendBootstrapRecoveryEffectAudit(ctx context.Context, cap biz.PlatformCapability, r biz.TenantAdminRecoveryOperation, audit biz.SecurityAuditEvent) error {
	if err := recoveryCapability(cap, "executeRecoveryBootstrap"); err != nil {
		return err
	}
	stored, err := t.GetBootstrapRecovery(ctx, cap, r.ID)
	if err != nil {
		return err
	}
	if stored.TenantID != r.TenantID || stored.TargetPrincipalID != r.TargetPrincipalID || stored.Status != biz.RecoveryExecuted || audit.Boundary != biz.AuditBoundaryTenant {
		return biz.ErrInvalidPersistenceState
	}
	if audit.TargetType == "invitation" {
		valid, e := t.q.RecoveryCancelledBootstrapInvitation(ctx, sqlcgen.RecoveryCancelledBootstrapInvitationParams{TenantID: r.TenantID, InvitationID: audit.TargetID, Version: audit.TargetVersion, RecoveryID: requiredPGUUID(r.ID)})
		if e != nil {
			return mapPostgresError("validate replaced Bootstrap audit target", e, nil)
		}
		if !valid {
			return biz.ErrInvalidPersistenceState
		}
	} else if audit.TargetType != "tenant_membership" || stored.MembershipID != audit.TargetID {
		return biz.ErrInvalidPersistenceState
	}
	scope, err := biz.NewTenantScope(stored.TenantID)
	if err != nil {
		return err
	}
	return (securityAuditRepository{queries: t.q, tenantID: stored.TenantID}).Append(ctx, scope, audit)
}
