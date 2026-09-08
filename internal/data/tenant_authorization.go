package data

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresTenantAuthorizationUnitOfWork struct {
	data *Data
}

type postgresTenantAuthorizationReader struct {
	data *Data
}

func NewPostgresTenantAuthorizationReader(data *Data) biz.TenantAuthorizationReader {
	return &postgresTenantAuthorizationReader{data: data}
}

func (r *postgresTenantAuthorizationReader) GetAccess(ctx context.Context, scope biz.TenantScope) (biz.TenantAccess, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantAccess{}, err
	}
	row, err := sqlcgen.New(r.data.pool).GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAccess{}, biz.ErrTenantAccessNotFound
	}
	if err != nil {
		return biz.TenantAccess{}, mapPostgresError("get tenant authorization access", err, nil)
	}
	return tenantAccessFromValues(row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}

func (r *postgresTenantAuthorizationReader) GetMembership(ctx context.Context, scope biz.TenantScope, membershipID uuid.UUID) (biz.TenantMembershipRecord, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantMembershipRecord{}, err
	}
	queries := sqlcgen.New(r.data.pool)
	row, err := queries.GetTenantAuthorizationMembership(ctx, sqlcgen.GetTenantAuthorizationMembershipParams{TenantID: tenantID, ID: membershipID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembershipRecord{}, biz.ErrMembershipNotFound
	}
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("get tenant authorization membership", err, nil)
	}
	return membershipRecord(ctx, queries, tenantID, row.ID, row.PrincipalID, row.PrincipalType, row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}

func (r *postgresTenantAuthorizationReader) ListMemberships(ctx context.Context, scope biz.TenantScope, status biz.MembershipStatus, cursor uuid.UUID, pageSize int32) (biz.TenantMembershipPage, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantMembershipPage{}, err
	}
	if pageSize <= 0 || pageSize > 100 {
		return biz.TenantMembershipPage{}, biz.ErrInvalidPersistenceState
	}
	queries := sqlcgen.New(r.data.pool)
	rows, err := queries.ListTenantAuthorizationMemberships(ctx, sqlcgen.ListTenantAuthorizationMembershipsParams{TenantID: tenantID, CursorID: cursor, Status: string(status), PageLimit: pageSize + 1})
	if err != nil {
		return biz.TenantMembershipPage{}, mapPostgresError("list tenant authorization memberships", err, nil)
	}
	page := biz.TenantMembershipPage{Items: make([]biz.TenantMembershipRecord, 0, min(len(rows), int(pageSize)))}
	for index, row := range rows {
		if index == int(pageSize) {
			page.NextCursor = page.Items[len(page.Items)-1].Membership.ID
			break
		}
		record, err := membershipRecord(ctx, queries, tenantID, row.ID, row.PrincipalID, row.PrincipalType, row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
		if err != nil {
			return biz.TenantMembershipPage{}, err
		}
		page.Items = append(page.Items, record)
	}
	return page, nil
}

func (r *postgresTenantAuthorizationReader) GetRole(ctx context.Context, scope biz.TenantScope, roleID uuid.UUID) (biz.TenantRole, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantRole{}, err
	}
	return (postgresTenantAuthorizationTransaction{queries: sqlcgen.New(r.data.pool), tenantID: tenantID}).GetRole(ctx, scope, roleID)
}

func (r *postgresTenantAuthorizationReader) ListRoles(ctx context.Context, scope biz.TenantScope, cursor uuid.UUID, pageSize int32) (biz.TenantRolePage, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.TenantRolePage{}, err
	}
	if pageSize <= 0 || pageSize > 100 {
		return biz.TenantRolePage{}, biz.ErrInvalidPersistenceState
	}
	queries := sqlcgen.New(r.data.pool)
	rows, err := queries.ListTenantAuthorizationRoles(ctx, sqlcgen.ListTenantAuthorizationRolesParams{TenantID: tenantID, CursorID: cursor, PageLimit: pageSize + 1})
	if err != nil {
		return biz.TenantRolePage{}, mapPostgresError("list tenant authorization roles", err, nil)
	}
	transaction := postgresTenantAuthorizationTransaction{queries: queries, tenantID: tenantID}
	page := biz.TenantRolePage{Items: make([]biz.TenantRole, 0, min(len(rows), int(pageSize)))}
	for index, row := range rows {
		if index == int(pageSize) {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		role, err := transaction.GetRole(ctx, scope, row.ID)
		if err != nil {
			return biz.TenantRolePage{}, err
		}
		page.Items = append(page.Items, role)
	}
	return page, nil
}

func membershipRecord(ctx context.Context, queries *sqlcgen.Queries, tenantID, membershipID, principalID uuid.UUID, principalType, status string, version int64, createdValid bool, created time.Time, updatedValid bool, updated time.Time) (biz.TenantMembershipRecord, error) {
	if !createdValid || !updatedValid || version <= 0 {
		return biz.TenantMembershipRecord{}, biz.ErrInvalidPersistenceState
	}
	roleIDs, err := queries.ListTenantAuthorizationMembershipRoleIDs(ctx, sqlcgen.ListTenantAuthorizationMembershipRoleIDsParams{TenantID: tenantID, MembershipID: membershipID})
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("list tenant authorization membership roles", err, nil)
	}
	return biz.TenantMembershipRecord{
		Membership:    biz.TenantMembership{ID: membershipID, PrincipalID: principalID, Status: biz.MembershipStatus(status), Version: version, CreatedAt: created.UTC(), UpdatedAt: updated.UTC()},
		PrincipalType: biz.PrincipalType(principalType), RoleIDs: append([]uuid.UUID(nil), roleIDs...),
	}, nil
}

func NewPostgresTenantAuthorizationUnitOfWork(data *Data) biz.TenantAuthorizationUnitOfWork {
	return &postgresTenantAuthorizationUnitOfWork{data: data}
}

func (u *postgresTenantAuthorizationUnitOfWork) WithinTenantAuthorization(
	ctx context.Context,
	scope biz.TenantScope,
	fn func(context.Context, biz.TenantAuthorizationTransaction) error,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin tenant authorization unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	transaction := postgresTenantAuthorizationTransaction{queries: sqlcgen.New(tx), tenantID: tenantID}
	if err := fn(ctx, transaction); err != nil {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, mapPostgresError("rollback tenant authorization unit of work", rollbackErr, nil))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit tenant authorization unit of work", err, nil)
	}
	committed = true
	return nil
}

type postgresTenantAuthorizationTransaction struct {
	queries  *sqlcgen.Queries
	tenantID uuid.UUID
}

func (tx postgresTenantAuthorizationTransaction) LockAdministrationGuard(ctx context.Context, scope biz.TenantScope) error {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	if err := tx.queries.LockTenantAdministrationGuard(ctx, sqlcgen.LockTenantAdministrationGuardParams{TenantID: tenantID}); err != nil {
		return mapPostgresError("lock tenant administration guard", err, nil)
	}
	return nil
}

func (tx postgresTenantAuthorizationTransaction) GetAccess(ctx context.Context, scope biz.TenantScope) (biz.TenantAccess, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantAccess{}, err
	}
	row, err := tx.queries.GetTenantAuthorizationAccess(ctx, sqlcgen.GetTenantAuthorizationAccessParams{TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAccess{}, biz.ErrTenantAccessNotFound
	}
	if err != nil {
		return biz.TenantAccess{}, mapPostgresError("get tenant authorization access", err, nil)
	}
	return tenantAccessFromValues(row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}

func (tx postgresTenantAuthorizationTransaction) UpdateAccessStatus(ctx context.Context, scope biz.TenantScope, status biz.TenantAccessStatus, expectedVersion int64, updatedAt time.Time) (biz.TenantAccess, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantAccess{}, err
	}
	row, err := tx.queries.UpdateTenantAuthorizationAccessStatus(ctx, sqlcgen.UpdateTenantAuthorizationAccessStatusParams{
		Status: string(status), UpdatedAt: requiredTimestamptz(updatedAt), TenantID: tenantID, ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantAccess{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantAccess{}, mapPostgresError("update tenant authorization access", err, nil)
	}
	return tenantAccessFromValues(row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}

func (tx postgresTenantAuthorizationTransaction) GetMembership(ctx context.Context, scope biz.TenantScope, membershipID uuid.UUID) (biz.TenantMembership, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, err
	}
	row, err := tx.queries.GetTenantMembership(ctx, sqlcgen.GetTenantMembershipParams{TenantID: tenantID, ID: membershipID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrMembershipNotFound
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("get tenant authorization membership", err, nil)
	}
	return membershipFromGetRow(row)
}

func (tx postgresTenantAuthorizationTransaction) GetMembershipRecord(ctx context.Context, scope biz.TenantScope, membershipID uuid.UUID) (biz.TenantMembershipRecord, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantMembershipRecord{}, err
	}
	row, err := tx.queries.GetTenantAuthorizationMembership(ctx, sqlcgen.GetTenantAuthorizationMembershipParams{TenantID: tenantID, ID: membershipID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembershipRecord{}, biz.ErrMembershipNotFound
	}
	if err != nil {
		return biz.TenantMembershipRecord{}, mapPostgresError("get tenant authorization membership record", err, nil)
	}
	return membershipRecord(ctx, tx.queries, tenantID, row.ID, row.PrincipalID, row.PrincipalType, row.Status, row.Version, row.CreatedAt.Valid, row.CreatedAt.Time, row.UpdatedAt.Valid, row.UpdatedAt.Time)
}

func (tx postgresTenantAuthorizationTransaction) UpdateMembershipStatus(
	ctx context.Context,
	scope biz.TenantScope,
	membershipID uuid.UUID,
	status biz.MembershipStatus,
	expectedVersion int64,
	updatedAt time.Time,
) (biz.TenantMembership, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, err
	}
	row, err := tx.queries.UpdateTenantMembershipStatus(ctx, sqlcgen.UpdateTenantMembershipStatusParams{
		Status: string(status), UpdatedAt: requiredTimestamptz(updatedAt), TenantID: tenantID,
		ID: membershipID, ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("update tenant authorization membership", err, nil)
	}
	return membershipFromUpdateRow(row)
}

func (tx postgresTenantAuthorizationTransaction) GetRole(ctx context.Context, scope biz.TenantScope, roleID uuid.UUID) (biz.TenantRole, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantRole{}, err
	}
	row, err := tx.queries.GetTenantAuthorizationRole(ctx, sqlcgen.GetTenantAuthorizationRoleParams{TenantID: tenantID, ID: roleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantRole{}, biz.ErrRoleNotFound
	}
	if err != nil {
		return biz.TenantRole{}, mapPostgresError("get tenant authorization role", err, nil)
	}
	permissionRows, err := tx.queries.ListTenantAuthorizationRolePermissions(ctx, sqlcgen.ListTenantAuthorizationRolePermissionsParams{TenantID: tenantID, RoleID: roleID})
	if err != nil {
		return biz.TenantRole{}, mapPostgresError("list tenant authorization role permissions", err, nil)
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return biz.TenantRole{}, biz.ErrInvalidPersistenceState
	}
	permissions := make([]biz.Permission, 0, len(permissionRows))
	for _, permission := range permissionRows {
		permissions = append(permissions, biz.Permission{Scope: biz.PermissionScopeTenant, Resource: permission.Resource, Action: permission.Action})
	}
	return biz.TenantRole{
		ID: row.ID, Code: row.Code, DisplayName: row.Code, System: row.SystemRole,
		SystemDefinitionVersion: row.SystemDefinitionVersion, Permissions: permissions,
		Version: row.Version, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

func (tx postgresTenantAuthorizationTransaction) ActiveHumanAdministratorCount(ctx context.Context, scope biz.TenantScope) (int64, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return 0, err
	}
	count, err := tx.queries.CountActiveHumanTenantAdministrators(ctx, sqlcgen.CountActiveHumanTenantAdministratorsParams{TenantID: tenantID})
	if err != nil {
		return 0, mapPostgresError("count active human tenant administrators", err, nil)
	}
	return count, nil
}

func (tx postgresTenantAuthorizationTransaction) IsActiveHumanAdministrator(ctx context.Context, scope biz.TenantScope, membershipID uuid.UUID) (bool, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return false, err
	}
	active, err := tx.queries.IsActiveHumanTenantAdministrator(ctx, sqlcgen.IsActiveHumanTenantAdministratorParams{TenantID: tenantID, MembershipID: membershipID})
	if err != nil {
		return false, mapPostgresError("check active human tenant administrator", err, nil)
	}
	return active, nil
}

func (tx postgresTenantAuthorizationTransaction) BindRole(
	ctx context.Context,
	scope biz.TenantScope,
	binding biz.TenantRoleBinding,
	expectedMembershipVersion int64,
	updatedAt time.Time,
) (biz.TenantMembership, biz.TenantRoleBinding, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, err
	}
	if _, err := tx.lockMembership(ctx, tenantID, binding.MembershipID, expectedMembershipVersion); err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, err
	}
	if err := tx.queries.CreateTenantRoleBinding(ctx, sqlcgen.CreateTenantRoleBindingParams{
		TenantID: tenantID, ID: binding.ID, MembershipID: binding.MembershipID, RoleID: binding.RoleID,
		Version: binding.Version, CreatedAt: requiredTimestamptz(binding.CreatedAt), UpdatedAt: requiredTimestamptz(binding.UpdatedAt),
	}); err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, mapPostgresError("create tenant role binding", err, biz.ErrRoleBindingConflict)
	}
	membership, err := tx.bumpMembershipVersion(ctx, tenantID, binding.MembershipID, expectedMembershipVersion, updatedAt)
	return membership, binding, err
}

func (tx postgresTenantAuthorizationTransaction) UnbindRole(
	ctx context.Context,
	scope biz.TenantScope,
	membershipID uuid.UUID,
	roleID uuid.UUID,
	expectedMembershipVersion int64,
	updatedAt time.Time,
) (biz.TenantMembership, biz.TenantRoleBinding, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, err
	}
	if _, err := tx.lockMembership(ctx, tenantID, membershipID, expectedMembershipVersion); err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, err
	}
	bindingRow, err := tx.queries.DeleteTenantRoleBinding(ctx, sqlcgen.DeleteTenantRoleBindingParams{TenantID: tenantID, MembershipID: membershipID, RoleID: roleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, biz.ErrRoleBindingNotFound
	}
	if err != nil {
		return biz.TenantMembership{}, biz.TenantRoleBinding{}, mapPostgresError("delete tenant role binding", err, nil)
	}
	membership, err := tx.bumpMembershipVersion(ctx, tenantID, membershipID, expectedMembershipVersion, updatedAt)
	return membership, biz.TenantRoleBinding{
		ID: bindingRow.ID, MembershipID: bindingRow.MembershipID, RoleID: bindingRow.RoleID,
		Version: bindingRow.Version, CreatedAt: bindingRow.CreatedAt.Time.UTC(), UpdatedAt: bindingRow.UpdatedAt.Time.UTC(),
	}, err
}

func (tx postgresTenantAuthorizationTransaction) AppendAudit(ctx context.Context, scope biz.TenantScope, event biz.SecurityAuditEvent) error {
	return (securityAuditRepository{queries: tx.queries, tenantID: tx.tenantID}).Append(ctx, scope, event)
}

func (tx postgresTenantAuthorizationTransaction) lockMembership(ctx context.Context, tenantID, membershipID uuid.UUID, expectedVersion int64) (biz.TenantMembership, error) {
	row, err := tx.queries.LockTenantAuthorizationMembership(ctx, sqlcgen.LockTenantAuthorizationMembershipParams{
		TenantID: tenantID, ID: membershipID, ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("lock tenant authorization membership", err, nil)
	}
	return newTenantMembership(row.ID, row.PrincipalID, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
}

func (tx postgresTenantAuthorizationTransaction) bumpMembershipVersion(ctx context.Context, tenantID, membershipID uuid.UUID, expectedVersion int64, updatedAt time.Time) (biz.TenantMembership, error) {
	row, err := tx.queries.BumpTenantMembershipVersion(ctx, sqlcgen.BumpTenantMembershipVersionParams{
		UpdatedAt: requiredTimestamptz(updatedAt), TenantID: tenantID, ID: membershipID, ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("bump tenant membership version", err, nil)
	}
	return newTenantMembership(row.ID, row.PrincipalID, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
}

func tenantAccessFromValues(status string, version int64, createdValid bool, created time.Time, updatedValid bool, updated time.Time) (biz.TenantAccess, error) {
	if !createdValid || !updatedValid || version <= 0 {
		return biz.TenantAccess{}, biz.ErrInvalidPersistenceState
	}
	return biz.TenantAccess{Status: biz.TenantAccessStatus(status), Version: version, CreatedAt: created.UTC(), UpdatedAt: updated.UTC()}, nil
}

var (
	_ biz.TenantAuthorizationUnitOfWork  = (*postgresTenantAuthorizationUnitOfWork)(nil)
	_ biz.TenantAuthorizationTransaction = postgresTenantAuthorizationTransaction{}
	_ biz.TenantAuthorizationReader      = (*postgresTenantAuthorizationReader)(nil)
)
