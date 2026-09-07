package data

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresOIDC struct {
	data *Data
}

func NewPostgresOIDCReader(data *Data) biz.OIDCReader {
	return &postgresOIDC{data: data}
}

func NewPostgresOIDCUnitOfWork(data *Data) biz.OIDCUnitOfWork {
	return &postgresOIDC{data: data}
}

func (p *postgresOIDC) LookupOIDCLogin(
	ctx context.Context,
	scope biz.TenantScope,
	provider string,
	issuer string,
	subject string,
) (biz.OIDCLoginState, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.OIDCLoginState{}, err
	}
	row, err := sqlcgen.New(p.data.pool).LookupOIDCLogin(ctx, sqlcgen.LookupOIDCLoginParams{
		TenantID: tenantID, Provider: provider, Issuer: issuer, Subject: subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.OIDCLoginState{}, biz.ErrOIDCIdentityNotFound
	}
	if err != nil {
		return biz.OIDCLoginState{}, mapPostgresError("lookup OIDC login", err, nil)
	}
	return biz.OIDCLoginState{
		IdentityID:  row.IdentityID,
		PrincipalID: row.PrincipalID, PrincipalStatus: biz.PrincipalStatus(row.PrincipalStatus),
		MembershipID: row.MembershipID, MembershipStatus: biz.MembershipStatus(row.MembershipStatus),
		TenantAccess: biz.TenantAccessStatus(row.TenantAccessStatus), Lifecycle: biz.TenantLifecycleStatus(row.LifecycleStatus),
		LifecycleFresh: row.LifecycleFresh, NormalizedEmail: row.NormalizedEmail,
	}, nil
}

func (p *postgresOIDC) LookupOIDCReauthentication(
	ctx context.Context,
	scope biz.TenantScope,
	claims biz.AccessTokenClaims,
) (biz.OIDCReauthenticationState, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.OIDCReauthenticationState{}, err
	}
	row, err := sqlcgen.New(p.data.pool).LookupOIDCReauthentication(ctx, sqlcgen.LookupOIDCReauthenticationParams{
		SessionID: claims.SessionID, TenantID: tenantID, GrantID: claims.GrantID, PrincipalID: claims.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.OIDCReauthenticationState{}, biz.ErrOIDCReauthenticationRequired
	}
	if err != nil {
		return biz.OIDCReauthenticationState{}, mapPostgresError("lookup OIDC reauthentication", err, nil)
	}
	return biz.OIDCReauthenticationState{
		PrincipalID: row.PrincipalID, PrincipalStatus: biz.PrincipalStatus(row.PrincipalStatus),
		SessionID: row.SessionID, SessionStatus: biz.SessionStatus(row.SessionStatus),
		GrantID: row.GrantID, GrantStatus: biz.GrantStatus(row.GrantStatus), GrantVersion: row.GrantVersion,
		TenantID: row.TenantID, ReauthenticatedAt: row.ReauthenticatedAt.Time.UTC(),
	}, nil
}

func (p *postgresOIDC) CommitOIDCLogin(ctx context.Context, scope biz.TenantScope, mutation biz.OIDCLoginMutation) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if mutation.IdentityID == uuid.Nil || mutation.Session.PrincipalID == uuid.Nil || mutation.Grant.MembershipID == uuid.Nil ||
		len(mutation.Session.AuthnMethods) != 1 || mutation.Session.AuthnMethods[0] != biz.AuditAuthenticationMethodOIDC ||
		strings.TrimSpace(mutation.Provider) != "dex" || strings.TrimSpace(mutation.Issuer) == "" ||
		strings.TrimSpace(mutation.Subject) == "" || strings.TrimSpace(mutation.NormalizedEmail) == "" {
		return biz.ErrInvalidPersistenceState
	}
	tx, err := p.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin OIDC login unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	authentication, err := queries.LockOIDCLoginAuthentication(ctx, sqlcgen.LockOIDCLoginAuthenticationParams{
		TenantID: tenantID, MembershipID: mutation.Grant.MembershipID, IdentityID: mutation.IdentityID,
		Provider: mutation.Provider, Issuer: mutation.Issuer, Subject: mutation.Subject,
		PrincipalID: mutation.Session.PrincipalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrInvalidCredential
	}
	if err != nil {
		return mapPostgresError("lock OIDC login authentication", err, nil)
	}
	if authentication.IdentityStatus != "active" || authentication.NormalizedEmail != mutation.NormalizedEmail {
		return biz.ErrInvalidCredential
	}
	if biz.PrincipalStatus(authentication.PrincipalStatus) != biz.PrincipalStatusActive {
		return biz.ErrPrincipalInactive
	}
	if biz.MembershipStatus(authentication.MembershipStatus) != biz.MembershipStatusActive {
		return biz.ErrMembershipInactive
	}
	if biz.TenantAccessStatus(authentication.TenantAccessStatus) != biz.TenantAccessStatusActive {
		return biz.ErrTenantAccessInactive
	}
	if !authentication.LifecycleFresh {
		return biz.ErrTenantLifecycleStale
	}
	if biz.TenantLifecycleStatus(authentication.LifecycleStatus) != biz.TenantLifecycleStatusActive {
		return biz.ErrTenantLifecycleBlocked
	}
	if err := createOIDCSessionGraph(ctx, queries, tenantID, mutation); err != nil {
		return err
	}
	audit := securityAuditRepository{queries: queries, tenantID: tenantID}
	if err := audit.Append(ctx, scope, mutation.Audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit OIDC login unit of work", err, nil)
	}
	committed = true
	return nil
}

func (p *postgresOIDC) RecordOIDCLoginFailure(ctx context.Context, scope biz.TenantScope, event biz.SecurityAuditEvent) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if event.ID == uuid.Nil || event.Action != biz.AuditActionOIDCLoginFailed ||
		event.TargetType != biz.AuditTargetTypeOIDCOperation || event.TargetID != event.ID ||
		event.TargetVersion != 1 || event.Result != biz.AuditResultFailed || event.Reason != biz.AuditReasonOIDCLoginFailed {
		return biz.ErrInvalidPersistenceState
	}
	return appendAnonymousTenantAudit(ctx, sqlcgen.New(p.data.pool), tenantID, event)
}

func (p *postgresOIDC) RecordOIDCIdentityLinkFailure(ctx context.Context, scope biz.TenantScope, event biz.SecurityAuditEvent) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if event.ID == uuid.Nil || event.ActorID == uuid.Nil ||
		(event.AuthenticationMethod != biz.AuditAuthenticationMethodPassword && event.AuthenticationMethod != biz.AuditAuthenticationMethodOIDC) ||
		event.Action != biz.AuditActionOIDCIdentityLinkFailed || event.TargetType != biz.AuditTargetTypeOIDCOperation ||
		event.TargetID != event.ID || event.TargetVersion != 1 || event.Result != biz.AuditResultFailed ||
		event.Reason != biz.AuditReasonOIDCIdentityLinkFailed {
		return biz.ErrInvalidPersistenceState
	}
	return (securityAuditRepository{queries: sqlcgen.New(p.data.pool), tenantID: tenantID}).Append(ctx, scope, event)
}

func (p *postgresOIDC) LinkOIDCIdentity(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.OIDCIdentityLinkMutation,
) (biz.OIDCIdentityLinkResult, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.OIDCIdentityLinkResult{}, err
	}
	if mutation.IdentityID == uuid.Nil || mutation.PrincipalID == uuid.Nil || mutation.SessionID == uuid.Nil || mutation.GrantID == uuid.Nil || mutation.ExpectedGrantVersion <= 0 ||
		mutation.ReauthenticatedAfter.IsZero() || strings.TrimSpace(mutation.Provider) != "dex" ||
		strings.TrimSpace(mutation.Issuer) == "" || strings.TrimSpace(mutation.Subject) == "" ||
		strings.TrimSpace(mutation.NormalizedEmail) == "" || mutation.LinkedAt.IsZero() || mutation.Audit.ID == uuid.Nil {
		return biz.OIDCIdentityLinkResult{}, biz.ErrInvalidPersistenceState
	}
	tx, err := p.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("begin OIDC identity-link unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	reauthenticatedAt, err := queries.LockOIDCLinkAuthentication(ctx, sqlcgen.LockOIDCLinkAuthenticationParams{
		SessionID: mutation.SessionID, TenantID: tenantID, GrantID: mutation.GrantID,
		PrincipalID: mutation.PrincipalID, ExpectedGrantVersion: mutation.ExpectedGrantVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && reauthenticatedAt.Time.Before(mutation.ReauthenticatedAfter.UTC())) {
		return biz.OIDCIdentityLinkResult{}, biz.ErrOIDCReauthenticationRequired
	}
	if err != nil {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("lock OIDC link authentication", err, nil)
	}
	emailOwner, err := queries.LookupVerifiedEmailOwner(ctx, sqlcgen.LookupVerifiedEmailOwnerParams{NormalizedEmail: mutation.NormalizedEmail})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && emailOwner != mutation.PrincipalID) {
		return biz.OIDCIdentityLinkResult{}, biz.ErrOIDCEmailConflict
	}
	if err != nil {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("lookup OIDC verified-email owner", err, nil)
	}
	identityOwner, err := queries.LookupOIDCIdentityOwner(ctx, sqlcgen.LookupOIDCIdentityOwnerParams{
		Issuer: mutation.Issuer, Subject: mutation.Subject,
	})
	if err == nil {
		_ = identityOwner
		return biz.OIDCIdentityLinkResult{}, biz.ErrOIDCIdentityConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("lookup OIDC identity owner", err, nil)
	}
	if err := queries.CreateOIDCIdentity(ctx, sqlcgen.CreateOIDCIdentityParams{
		ID: mutation.IdentityID, PrincipalID: mutation.PrincipalID, Provider: mutation.Provider,
		Issuer: mutation.Issuer, Subject: mutation.Subject,
		CreatedAt: requiredTimestamptz(mutation.LinkedAt), UpdatedAt: requiredTimestamptz(mutation.LinkedAt),
	}); err != nil {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("create OIDC identity", err, biz.ErrOIDCIdentityConflict)
	}
	audit := securityAuditRepository{queries: queries, tenantID: tenantID}
	if err := audit.Append(ctx, scope, mutation.Audit); err != nil {
		return biz.OIDCIdentityLinkResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.OIDCIdentityLinkResult{}, mapPostgresError("commit OIDC identity-link unit of work", err, nil)
	}
	committed = true
	return biz.OIDCIdentityLinkResult{IdentityID: mutation.IdentityID, PrincipalID: mutation.PrincipalID}, nil
}

func createOIDCSessionGraph(ctx context.Context, queries *sqlcgen.Queries, tenantID uuid.UUID, mutation biz.OIDCLoginMutation) error {
	if err := queries.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID: mutation.Session.ID, PrincipalID: mutation.Session.PrincipalID, Audience: string(mutation.Session.Audience),
		Status: string(mutation.Session.Status), AuthnMethods: []string{string(mutation.Session.AuthnMethods[0])},
		DeviceName:    mutation.Session.DeviceName,
		IdleExpiresAt: requiredTimestamptz(mutation.Session.IdleExpiresAt), AbsoluteExpiresAt: requiredTimestamptz(mutation.Session.AbsoluteExpiry),
		ReauthenticatedAt: requiredTimestamptz(mutation.Session.ReauthenticatedAt), CreatedAt: requiredTimestamptz(mutation.Session.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Session.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create OIDC login session", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateSessionGrant(ctx, sqlcgen.CreateSessionGrantParams{
		TenantID: tenantID, ID: mutation.Grant.ID, SessionID: mutation.Grant.SessionID, MembershipID: mutation.Grant.MembershipID,
		Status: string(mutation.Grant.Status), Version: mutation.Grant.Version,
		CreatedAt: requiredTimestamptz(mutation.Grant.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Grant.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create OIDC login grant", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateRefreshTokenFamily(ctx, sqlcgen.CreateRefreshTokenFamilyParams{
		TenantID: tenantID, ID: mutation.RefreshFamily.ID, GrantID: mutation.RefreshFamily.GrantID,
		Status: string(mutation.RefreshFamily.Status), CreatedAt: requiredTimestamptz(mutation.RefreshFamily.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.RefreshFamily.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create OIDC refresh-token family", err, biz.ErrInvalidPersistenceState)
	}
	if err := queries.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		TenantID: tenantID, ID: mutation.RefreshToken.ID, FamilyID: mutation.RefreshToken.FamilyID,
		Digest: mutation.RefreshToken.Digest[:], IssuedAt: requiredTimestamptz(mutation.RefreshToken.IssuedAt), ExpiresAt: requiredTimestamptz(mutation.RefreshToken.ExpiresAt),
	}); err != nil {
		return mapPostgresError("create OIDC refresh token", err, biz.ErrInvalidPersistenceState)
	}
	return nil
}

var (
	_ biz.OIDCReader     = (*postgresOIDC)(nil)
	_ biz.OIDCUnitOfWork = (*postgresOIDC)(nil)
)
