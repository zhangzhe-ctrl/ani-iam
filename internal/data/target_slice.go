package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresPasswordLoginReader struct {
	data *Data
}

func NewPostgresPasswordLoginReader(data *Data) biz.PasswordLoginReader {
	return &postgresPasswordLoginReader{data: data}
}

func (r *postgresPasswordLoginReader) LookupPasswordLogin(
	ctx context.Context,
	scope biz.TenantScope,
	normalizedAccount string,
) (biz.PasswordLoginState, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.PasswordLoginState{}, err
	}
	row, err := sqlcgen.New(r.data.pool).LookupPasswordLogin(ctx, sqlcgen.LookupPasswordLoginParams{
		TenantID:          tenantID,
		NormalizedAccount: normalizedAccount,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PasswordLoginState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.PasswordLoginState{}, mapPostgresError("lookup password login", err, nil)
	}
	return biz.PasswordLoginState{
		PrincipalID:      row.PrincipalID,
		PrincipalStatus:  biz.PrincipalStatus(row.PrincipalStatus),
		MembershipID:     row.MembershipID,
		MembershipStatus: biz.MembershipStatus(row.MembershipStatus),
		TenantAccess:     biz.TenantAccessStatus(row.TenantAccessStatus),
		Lifecycle:        biz.TenantLifecycleStatus(row.LifecycleStatus),
		LifecycleFresh:   row.LifecycleFresh,
		PasswordHash:     row.PasswordHash,
	}, nil
}

type postgresLoginUnitOfWork struct {
	data *Data
}

func NewPostgresLoginUnitOfWork(data *Data) biz.LoginUnitOfWork {
	return &postgresLoginUnitOfWork{data: data}
}

func (u *postgresLoginUnitOfWork) CommitLogin(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.LoginMutation,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin login unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	queries := sqlcgen.New(tx)
	if err := queries.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID:                mutation.Session.ID,
		PrincipalID:       mutation.Session.PrincipalID,
		Audience:          string(mutation.Session.Audience),
		Status:            string(mutation.Session.Status),
		DeviceName:        mutation.Session.DeviceName,
		IdleExpiresAt:     requiredTimestamptz(mutation.Session.IdleExpiresAt),
		AbsoluteExpiresAt: requiredTimestamptz(mutation.Session.AbsoluteExpiry),
		CreatedAt:         requiredTimestamptz(mutation.Session.CreatedAt),
		UpdatedAt:         requiredTimestamptz(mutation.Session.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create login session", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateSessionGrant(ctx, sqlcgen.CreateSessionGrantParams{
		TenantID:     tenantID,
		ID:           mutation.Grant.ID,
		SessionID:    mutation.Grant.SessionID,
		MembershipID: mutation.Grant.MembershipID,
		Status:       string(mutation.Grant.Status),
		Version:      mutation.Grant.Version,
		CreatedAt:    requiredTimestamptz(mutation.Grant.CreatedAt),
		UpdatedAt:    requiredTimestamptz(mutation.Grant.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create login grant", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateRefreshTokenFamily(ctx, sqlcgen.CreateRefreshTokenFamilyParams{
		TenantID:  tenantID,
		ID:        mutation.RefreshFamily.ID,
		GrantID:   mutation.RefreshFamily.GrantID,
		Status:    string(mutation.RefreshFamily.Status),
		CreatedAt: requiredTimestamptz(mutation.RefreshFamily.CreatedAt),
		UpdatedAt: requiredTimestamptz(mutation.RefreshFamily.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create refresh-token family", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		TenantID:  tenantID,
		ID:        mutation.RefreshToken.ID,
		FamilyID:  mutation.RefreshToken.FamilyID,
		Digest:    mutation.RefreshToken.Digest[:],
		IssuedAt:  requiredTimestamptz(mutation.RefreshToken.IssuedAt),
		ExpiresAt: requiredTimestamptz(mutation.RefreshToken.ExpiresAt),
	}); err != nil {
		return mapPostgresError("create refresh token", err, biz.ErrInvalidPersistenceState)
	}
	audit := securityAuditRepository{queries: queries, tenantID: tenantID}
	if err := audit.Append(ctx, scope, mutation.Audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit login unit of work", err, nil)
	}
	committed = true
	return nil
}

type postgresAuthorizationReader struct {
	data *Data
}

func NewPostgresAuthorizationReader(data *Data) biz.AuthorizationReader {
	return &postgresAuthorizationReader{data: data}
}

func (r *postgresAuthorizationReader) LookupAuthorization(
	ctx context.Context,
	scope biz.TenantScope,
	lookup biz.AuthorizationLookup,
) (biz.AuthorizationState, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.AuthorizationState{}, err
	}
	if len(lookup.Actions) == 0 {
		return biz.AuthorizationState{}, fmt.Errorf("lookup authorization: %w", biz.ErrInvalidPersistenceState)
	}
	row, err := sqlcgen.New(r.data.pool).LookupAuthorization(ctx, sqlcgen.LookupAuthorizationParams{
		Actions:     append([]string(nil), lookup.Actions...),
		TenantID:    tenantID,
		Resource:    lookup.Resource,
		SessionID:   lookup.SessionID,
		GrantID:     lookup.GrantID,
		PrincipalID: lookup.PrincipalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.AuthorizationState{}, nil
	}
	if err != nil {
		return biz.AuthorizationState{}, mapPostgresError("lookup authorization", err, nil)
	}
	return biz.AuthorizationState{
		PrincipalStatus:   biz.PrincipalStatus(row.PrincipalStatus),
		MembershipStatus:  biz.MembershipStatus(row.MembershipStatus),
		TenantAccess:      biz.TenantAccessStatus(row.TenantAccessStatus),
		Lifecycle:         biz.TenantLifecycleStatus(row.LifecycleStatus),
		LifecycleFresh:    row.LifecycleFresh,
		SessionStatus:     biz.SessionStatus(row.SessionStatus),
		GrantStatus:       biz.GrantStatus(row.GrantStatus),
		GrantVersion:      row.GrantVersion,
		PermissionAllowed: row.PermissionAllowed,
	}, nil
}

var (
	_ biz.PasswordLoginReader = (*postgresPasswordLoginReader)(nil)
	_ biz.LoginUnitOfWork     = (*postgresLoginUnitOfWork)(nil)
	_ biz.AuthorizationReader = (*postgresAuthorizationReader)(nil)
)
