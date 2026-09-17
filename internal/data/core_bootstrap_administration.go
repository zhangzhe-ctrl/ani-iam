package data

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

func (t *platformAdministrationTransaction) LoadPlatformBootstrap(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID, producer string) (biz.CoreBootstrapWork, error) {
	var empty biz.CoreBootstrapWork
	if err := recoveryCapability(cap, "getTenantIAMBootstrap", "reissueTenantIAMBootstrapInvitation", "retryTenantIAMBootstrapJob"); err != nil {
		return empty, err
	}
	if t.data == nil || !biz.ValidCoreProjectionProducer(producer) || (t.broker != nil && producer != t.broker.config.Producer) {
		return empty, biz.ErrCoreBootstrapAuthority
	}
	tenant, err := t.q.LookupPlatformCoreBootstrapTenant(ctx, sqlcgen.LookupPlatformCoreBootstrapTenantParams{OperationID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, biz.ErrCoreBootstrapNotFound
	}
	if err != nil {
		return empty, mapPostgresError("find Platform Bootstrap target", err, nil)
	}
	if _, err = t.q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: producer}); errors.Is(err, pgx.ErrNoRows) {
		return empty, biz.ErrTenantLifecycleStale
	} else if err != nil {
		return empty, mapPostgresError("lock Platform Bootstrap source", err, nil)
	}
	if err = t.q.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenant}); err != nil {
		return empty, mapPostgresError("lock stored Bootstrap Tenant", err, nil)
	}
	t.bootstrap = &coreBootstrapWorkTransaction{q: t.q, data: t.data, producer: producer, tenant: tenant, broker: t.broker}
	// Tenant was obtained from the immutable original operation after the
	// dedicated Platform authorization; it is never a promoted request scope.
	scope, err := biz.NewTenantScope(tenant)
	if err != nil {
		return empty, err
	}
	return t.bootstrap.LoadCoreBootstrapWork(ctx, scope, id)
}

func (t *platformAdministrationTransaction) AuthorizePlatformBootstrapExecution(ctx context.Context, cap biz.PlatformCapability, s biz.CoreBootstrapSource, authorizer biz.CoreBootstrapAuthorizer) (biz.CoreBootstrapExecutionAuthorization, error) {
	if err := recoveryCapability(cap, "reissueTenantIAMBootstrapInvitation", "retryTenantIAMBootstrapJob"); err != nil {
		return biz.CoreBootstrapExecutionAuthorization{}, err
	}
	if t.bootstrap == nil || t.bootstrap.work == nil || s != t.bootstrap.work.Source || authorizer == nil {
		return biz.CoreBootstrapExecutionAuthorization{}, biz.ErrCoreBootstrapAuthority
	}
	if t.broker != nil {
		t.bootstrap.platformRecovery = true
		ctx = context.WithValue(ctx, coreBrokerTransactionKey{}, coreBrokerTransaction{data: t.data, consumer: t.broker.config.ConsumerID, q: t.q, work: t.bootstrap})
	}
	return authorizer.AuthorizeCoreBootstrap(ctx, s)
}

func (t *platformAdministrationTransaction) RecheckPlatformBootstrapExecution(ctx context.Context, cap biz.PlatformCapability, a biz.CoreBootstrapExecutionAuthorization) error {
	if err := recoveryCapability(cap, "reissueTenantIAMBootstrapInvitation", "retryTenantIAMBootstrapJob"); err != nil {
		return err
	}
	if t.bootstrap == nil {
		return biz.ErrCoreBootstrapAuthority
	}
	scope, err := biz.NewTenantScope(t.bootstrap.tenant)
	if err != nil {
		return err
	}
	return t.bootstrap.RecheckBootstrapExecution(ctx, scope, a)
}

func (t *platformAdministrationTransaction) ReissuePlatformBootstrapInvitation(ctx context.Context, cap biz.PlatformCapability, w biz.CoreBootstrapWork, d biz.CoreBootstrapInvitationDraft, a biz.CoreBootstrapExecutionAuthorization) error {
	if err := recoveryCapability(cap, "reissueTenantIAMBootstrapInvitation"); err != nil {
		return err
	}
	b := t.bootstrap
	if b == nil || b.work == nil || b.authority == nil || *b.authority != a || w.Source != b.work.Source || w.Version != b.work.Version || b.work.Invitation == nil || b.work.Result == nil {
		return biz.ErrCoreBootstrapConflict
	}
	old := b.work.Invitation.Invitation
	inv := d.Invitation.Invitation
	if inv.ID != old.ID || inv.NormalizedEmail != w.Intent.NormalizedEmail || inv.Locale != w.Intent.Locale || !slices.Equal(inv.RoleIDs, old.RoleIDs) || len(inv.RoleIDs) != 1 || inv.RoleIDs[0] != b.work.Result.RoleID || d.RoleID != b.work.Result.RoleID || inv.CreatedBy != old.CreatedBy || !inv.CreatedAt.Equal(old.CreatedAt) || inv.Version != old.Version+1 || inv.DeliveryGeneration != old.DeliveryGeneration+1 || inv.Status != biz.InvitationPending || d.Invitation.TokenDigest == b.work.Invitation.TokenDigest || sha256.Sum256([]byte(d.Token)) != d.Invitation.TokenDigest || d.AuditID.Version() != 7 || d.DeliveryID.Version() != 7 {
		return biz.ErrCoreBootstrapConflict
	}
	role, err := t.q.TenantAdminRecoveryRole(ctx, sqlcgen.TenantAdminRecoveryRoleParams{TenantID: b.tenant})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrRoleNotFound
	}
	if err != nil {
		return mapPostgresError("read Bootstrap administrator role", err, nil)
	}
	if role.ID != d.RoleID || !role.SystemRole || role.SystemDefinitionVersion != 1 {
		return biz.ErrCoreBootstrapConflict
	}
	permissions, err := t.q.ListTenantAuthorizationRolePermissions(ctx, sqlcgen.ListTenantAuthorizationRolePermissionsParams{TenantID: b.tenant, RoleID: role.ID})
	if err != nil {
		return mapPostgresError("read Bootstrap role definition", err, nil)
	}
	wanted := map[biz.Permission]bool{}
	for _, p := range d.Permissions {
		if p.Scope != biz.PermissionScopeTenant || wanted[p] {
			return biz.ErrCoreBootstrapConflict
		}
		wanted[p] = true
	}
	if len(wanted) == 0 || len(wanted) != len(permissions) {
		return biz.ErrCoreBootstrapConflict
	}
	for _, p := range permissions {
		if !wanted[biz.Permission{Scope: biz.PermissionScopeTenant, Resource: p.Resource, Action: p.Action}] {
			return biz.ErrCoreBootstrapConflict
		}
	}
	// Source authorization can take time while the pipeline lock is held.
	// Evaluate freshness with database time again immediately before effects.
	if err := b.checkBootstrapLifecycle(ctx); err != nil {
		return err
	}
	scope, err := biz.NewTenantScope(b.tenant)
	if err != nil {
		return err
	}
	i := tenantInvitationTransaction{postgresTenantAuthorizationTransaction: postgresTenantAuthorizationTransaction{queries: t.q, tenantID: b.tenant}, protector: t.protector}
	if err = i.ResendInvitation(ctx, scope, d.Invitation, old.Version, d.DeliveryID, d.Token); err != nil {
		return err
	}
	n, err := t.q.ResumeReissuedCoreBootstrap(ctx, sqlcgen.ResumeReissuedCoreBootstrapParams{TenantID: b.tenant, OperationID: w.Source.OperationID, ExpectedVersion: w.Version, InvitationID: inv.ID, Generation: inv.DeliveryGeneration, Now: requiredTimestamptz(inv.UpdatedAt)})
	if err != nil {
		return mapPostgresError("resume exact reissued Bootstrap", err, nil)
	}
	if n != 1 {
		return biz.ErrCoreBootstrapConflict
	}
	b.auditID = d.AuditID
	return nil
}

func (t *platformAdministrationTransaction) AppendPlatformBootstrapEffectAudit(ctx context.Context, cap biz.PlatformCapability, a biz.SecurityAuditEvent) error {
	if err := recoveryCapability(cap, "reissueTenantIAMBootstrapInvitation"); err != nil {
		return err
	}
	claims, _, err := cap.CredentialBinding()
	if err != nil {
		return err
	}
	b := t.bootstrap
	if b == nil || b.work == nil || b.work.Invitation == nil || a.ID != b.auditID || a.ActorID != claims.Subject || a.Boundary != biz.AuditBoundaryTenant || a.Action != "iam.bootstrap.invitation.reissued" || a.TargetID != b.work.Invitation.Invitation.ID || a.TargetVersion != b.work.Invitation.Invitation.Version+1 {
		return biz.ErrInvalidPersistenceState
	}
	scope, err := biz.NewTenantScope(b.tenant)
	if err != nil {
		return err
	}
	if err = (securityAuditRepository{queries: t.q, tenantID: b.tenant}).Append(ctx, scope, a); err != nil {
		return err
	}
	return t.appendBootstrapBrokerApproval(ctx, cap, biz.CoreBootstrapExpire, b.work.Invitation.Invitation.DeliveryGeneration+1, a.ID, true, string(a.Reason))
}
