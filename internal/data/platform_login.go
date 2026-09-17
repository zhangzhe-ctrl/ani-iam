package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type platformLoginUnitOfWork struct{ data *Data }
type platformLoginTransaction struct{ q *sqlcgen.Queries }

func NewPlatformLoginUnitOfWork(d *Data) biz.PlatformLoginUnitOfWork {
	return &platformLoginUnitOfWork{data: d}
}

func NewPlatformLoginAuditWriter(d *Data) biz.PlatformLoginAuditWriter {
	return &platformLoginUnitOfWork{data: d}
}
func (u *platformLoginUnitOfWork) RecordPlatformLoginFailure(ctx context.Context, a biz.SecurityAuditEvent) error {
	if u == nil || u.data == nil || u.data.pool == nil {
		return biz.ErrPersistenceUnavailable
	}
	return appendPlatformAudit(ctx, sqlcgen.New(u.data.pool), a)
}

func (u *platformLoginUnitOfWork) WithinPlatformLogin(ctx context.Context, environment string, fn func(biz.PlatformLoginTransaction) error) error {
	if u == nil || u.data == nil || u.data.pool == nil || fn == nil {
		return biz.ErrPersistenceUnavailable
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return platformLoginPersistenceError(err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollback)
	}()
	q := sqlcgen.New(tx)
	if err = q.LockPlatformAdministrator(ctx); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err = q.LockFirstAdministrator(ctx, sqlcgen.LockFirstAdministratorParams{Environment: environment}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err = fn(&platformLoginTransaction{q: q}); err != nil {
		return err
	}
	return platformLoginPersistenceError(tx.Commit(ctx))
}

func (t *platformLoginTransaction) LookupOIDC(ctx context.Context, provider, issuer, subject string) (biz.PlatformLoginState, error) {
	r, err := t.q.LookupPlatformOIDC(ctx, sqlcgen.LookupPlatformOIDCParams{Provider: provider, Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformLoginState{}, biz.ErrOIDCIdentityNotFound
	}
	if err != nil {
		return biz.PlatformLoginState{}, platformLoginPersistenceError(err)
	}
	return biz.PlatformLoginState{IdentityID: r.IdentityID, IdentityActive: r.IdentityActive, Principal: biz.Principal{ID: r.PrincipalID, Status: biz.PrincipalStatus(r.PrincipalStatus)}, MembershipID: r.MembershipID, MembershipStatus: biz.MembershipStatus(r.MembershipStatus), NormalizedEmail: r.NormalizedEmail}, nil
}

func (t *platformLoginTransaction) FirstAdministrator(ctx context.Context, environment string) (biz.PlatformFirstAdministratorState, error) {
	r, err := t.q.LookupFirstAdministratorCandidate(ctx, sqlcgen.LookupFirstAdministratorCandidateParams{Environment: environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformFirstAdministratorState{}, biz.ErrInvalidCredential
	}
	if err != nil {
		return biz.PlatformFirstAdministratorState{}, platformLoginPersistenceError(err)
	}
	return biz.PlatformFirstAdministratorState{Manifest: biz.FirstAdministratorManifest{Version: 1, IntentID: r.IntentID, Environment: r.Environment, Email: r.NormalizedEmail, Issuer: r.Issuer, Subject: r.Subject, ExpiresAt: r.ExpiresAt.Time}, Completed: r.Completed, AdministratorPresent: r.AdministratorPresent}, nil
}

func (t *platformLoginTransaction) IdentityAvailable(ctx context.Context, email, issuer, subject string) (bool, error) {
	available, err := t.q.PlatformBootstrapIdentityAvailable(ctx, sqlcgen.PlatformBootstrapIdentityAvailableParams{NormalizedEmail: email, Issuer: issuer, Subject: subject})
	return available, platformLoginPersistenceError(err)
}

func (t *platformLoginTransaction) AdministratorRole(ctx context.Context) (biz.PlatformRole, error) {
	r, err := t.q.GetPlatformAdministratorRole(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformRole{}, biz.ErrRoleNotFound
	}
	if err != nil {
		return biz.PlatformRole{}, platformLoginPersistenceError(err)
	}
	rows, err := t.q.GetPlatformRolePermissionSet(ctx, sqlcgen.GetPlatformRolePermissionSetParams{RoleID: r.ID})
	if err != nil {
		return biz.PlatformRole{}, platformLoginPersistenceError(err)
	}
	role := biz.PlatformRole{ID: r.ID, Code: r.Code, DisplayName: r.DisplayName, System: r.SystemRole, SystemDefinitionVersion: r.SystemDefinitionVersion, Version: r.Version}
	for _, p := range rows {
		role.Permissions = append(role.Permissions, biz.Permission{Scope: biz.PermissionScope(p.Scope), Resource: p.Resource, Action: p.Action})
	}
	return role, nil
}

func (t *platformLoginTransaction) InsertAdministratorRole(ctx context.Context, role biz.PlatformRole, now time.Time) error {
	if err := t.q.InsertPlatformAdministratorRole(ctx, sqlcgen.InsertPlatformAdministratorRoleParams{ID: role.ID, DisplayName: role.DisplayName, Now: requiredTimestamptz(now)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	for _, p := range role.Permissions {
		if err := t.q.InsertPlatformRolePermission(ctx, sqlcgen.InsertPlatformRolePermissionParams{RoleID: role.ID, Resource: p.Resource, Action: p.Action, Now: requiredTimestamptz(now)}); err != nil {
			return platformLoginPersistenceError(err)
		}
	}
	return nil
}

func (t *platformLoginTransaction) CreateFirstAdministrator(ctx context.Context, c biz.PlatformFirstAdministratorCreation) error {
	now := requiredTimestamptz(c.Now)
	if err := t.q.CreatePlatformHuman(ctx, sqlcgen.CreatePlatformHumanParams{ID: c.PrincipalID, Now: now}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err := t.q.CreatePlatformVerifiedEmail(ctx, sqlcgen.CreatePlatformVerifiedEmailParams{PrincipalID: c.PrincipalID, NormalizedEmail: c.Identity.Email, Now: now}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err := t.q.CreateOIDCIdentity(ctx, sqlcgen.CreateOIDCIdentityParams{ID: c.IdentityID, PrincipalID: c.PrincipalID, Provider: c.Provider, Issuer: c.Identity.Issuer, Subject: c.Identity.Subject, CreatedAt: now, UpdatedAt: now}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err := t.q.CreatePlatformMembership(ctx, sqlcgen.CreatePlatformMembershipParams{ID: c.MembershipID, PrincipalID: c.PrincipalID, Now: now}); err != nil {
		return platformLoginPersistenceError(err)
	}
	if err := t.q.CreatePlatformRoleBinding(ctx, sqlcgen.CreatePlatformRoleBindingParams{ID: c.BindingID, MembershipID: c.MembershipID, RoleID: c.RoleID, Now: now}); err != nil {
		return platformLoginPersistenceError(err)
	}
	return platformLoginPersistenceError(t.q.CompleteFirstAdministratorIntent(ctx, sqlcgen.CompleteFirstAdministratorIntentParams{Environment: c.Environment, IntentID: c.IntentID, PrincipalID: c.PrincipalID, AuditEventID: c.AuditEventID, Now: now}))
}

func (t *platformLoginTransaction) SaveLogin(ctx context.Context, m biz.PlatformLoginMutation) error {
	s := m.Session
	methods := make([]string, len(s.AuthnMethods))
	for i, a := range s.AuthnMethods {
		methods[i] = string(a)
	}
	if err := t.q.CreateSession(ctx, sqlcgen.CreateSessionParams{ID: s.ID, PrincipalID: s.PrincipalID, Audience: string(s.Audience), Status: string(s.Status), AuthnMethods: methods, DeviceName: s.DeviceName, IdleExpiresAt: requiredTimestamptz(s.IdleExpiresAt), AbsoluteExpiresAt: requiredTimestamptz(s.AbsoluteExpiry), ReauthenticatedAt: requiredTimestamptz(s.ReauthenticatedAt), CreatedAt: requiredTimestamptz(s.CreatedAt), UpdatedAt: requiredTimestamptz(s.UpdatedAt)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	g := m.Grant
	if err := t.q.CreatePlatformSessionGrant(ctx, sqlcgen.CreatePlatformSessionGrantParams{ID: g.ID, SessionID: s.ID, PrincipalID: s.PrincipalID, MembershipID: g.MembershipID, Now: requiredTimestamptz(g.CreatedAt)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	f := m.Family
	if err := t.q.CreatePlatformRefreshFamily(ctx, sqlcgen.CreatePlatformRefreshFamilyParams{ID: f.ID, GrantID: g.ID, Now: requiredTimestamptz(f.CreatedAt)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	r := m.Refresh
	if err := t.q.CreatePlatformRefreshToken(ctx, sqlcgen.CreatePlatformRefreshTokenParams{ID: r.ID, FamilyID: f.ID, Digest: r.Digest[:], IssuedAt: requiredTimestamptz(r.IssuedAt), ExpiresAt: requiredTimestamptz(r.ExpiresAt)}); err != nil {
		return platformLoginPersistenceError(err)
	}
	return appendPlatformAudit(ctx, t.q, m.Audit)
}

func appendPlatformAudit(ctx context.Context, q *sqlcgen.Queries, a biz.SecurityAuditEvent) error {
	return platformLoginPersistenceError(q.AppendPlatformHumanAudit(ctx, sqlcgen.AppendPlatformHumanAuditParams{EventID: a.ID, ActorID: optionalPGUUID(a.ActorID), AuthenticationMethod: string(a.AuthenticationMethod), Action: string(a.Action), TargetType: string(a.TargetType), TargetID: a.TargetID, TargetVersion: a.TargetVersion, Result: string(a.Result), Reason: string(a.Reason), RequestID: a.RequestID, CorrelationID: a.CorrelationID, DecisionID: a.DecisionID, OccurredAt: requiredTimestamptz(a.OccurredAt), RecordedAt: requiredTimestamptz(a.RecordedAt), CallerPrincipalID: optionalPGUUID(a.DirectCaller.Identity.PrincipalID), CallerBindingID: optionalPGUUID(a.DirectCaller.Identity.BindingID), CallerBindingVersion: optionalPositiveInt64(a.DirectCaller.Identity.BindingVersion), CallerGrantVersion: optionalPositiveInt64(a.DirectCaller.GrantVersion)}))
}

func platformLoginPersistenceError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return biz.ErrOIDCIdentityConflict
		}
		// Keep only PostgreSQL's fixed five-character class code. Never expose
		// statement text, parameters, DETAIL, connection or other driver fields.
		return fmt.Errorf("%w: SQLSTATE=%s", biz.ErrPersistenceUnavailable, pgErr.Code)
	}
	return biz.ErrPersistenceUnavailable
}
