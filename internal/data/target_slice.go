package data

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresPasswordLoginReader struct {
	data *Data
}

func NewPostgresPasswordLoginReader(data *Data) biz.AuthenticationReader {
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
	var lockedUntil time.Time
	if row.LockedUntil.Valid {
		lockedUntil = row.LockedUntil.Time.UTC()
	}
	return biz.PasswordLoginState{
		PrincipalID:       row.PrincipalID,
		PrincipalStatus:   biz.PrincipalStatus(row.PrincipalStatus),
		MembershipID:      row.MembershipID,
		MembershipStatus:  biz.MembershipStatus(row.MembershipStatus),
		TenantAccess:      biz.TenantAccessStatus(row.TenantAccessStatus),
		Lifecycle:         biz.TenantLifecycleStatus(row.LifecycleStatus),
		LifecycleFresh:    row.LifecycleFresh,
		PasswordHash:      row.PasswordHash,
		FailedAttempts:    int(row.FailedAttempts),
		LockedUntil:       lockedUntil,
		CredentialVersion: row.CredentialVersion,
	}, nil
}

func (r *postgresPasswordLoginReader) LookupPasswordActionTarget(
	ctx context.Context,
	normalizedAccount string,
	audience biz.Audience,
) (biz.PasswordActionTarget, bool, error) {
	if audience != biz.AudienceConsole {
		return biz.PasswordActionTarget{}, false, biz.ErrAuthenticationDependency
	}
	row, err := sqlcgen.New(r.data.pool).LookupPasswordActionTarget(ctx, sqlcgen.LookupPasswordActionTargetParams{
		NormalizedAccount: normalizedAccount,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PasswordActionTarget{}, false, nil
	}
	if err != nil {
		return biz.PasswordActionTarget{}, false, mapPostgresError("lookup password-action target", err, nil)
	}
	hasPassword, ok := row.HasPassword.(bool)
	if !ok {
		return biz.PasswordActionTarget{}, false, biz.ErrInvalidPersistenceState
	}
	if strings.TrimSpace(row.NormalizedEmail) == "" || strings.ToLower(strings.TrimSpace(row.NormalizedEmail)) != row.NormalizedEmail {
		return biz.PasswordActionTarget{}, false, biz.ErrInvalidPersistenceState
	}
	return biz.PasswordActionTarget{
		PrincipalID:   row.PrincipalID,
		HasPassword:   hasPassword,
		VerifiedEmail: row.NormalizedEmail,
	}, true, nil
}

type postgresLoginUnitOfWork struct {
	data *Data
}

func NewPostgresLoginUnitOfWork(data *Data) biz.AuthenticationUnitOfWork {
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
	if len(mutation.Session.AuthnMethods) != 1 || mutation.Session.AuthnMethods[0] != biz.AuditAuthenticationMethodPassword {
		return biz.ErrInvalidPersistenceState
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
	if _, err := queries.ResetPasswordLoginFailures(ctx, sqlcgen.ResetPasswordLoginFailuresParams{
		UpdatedAt:       requiredTimestamptz(mutation.Session.UpdatedAt),
		PrincipalID:     mutation.Session.PrincipalID,
		ExpectedVersion: mutation.CredentialVersion,
	}); errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrVersionConflict
	} else if err != nil {
		return mapPostgresError("reset password login failures", err, nil)
	}
	if err := queries.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID:                mutation.Session.ID,
		PrincipalID:       mutation.Session.PrincipalID,
		Audience:          string(mutation.Session.Audience),
		Status:            string(mutation.Session.Status),
		AuthnMethods:      []string{string(mutation.Session.AuthnMethods[0])},
		DeviceName:        mutation.Session.DeviceName,
		IdleExpiresAt:     requiredTimestamptz(mutation.Session.IdleExpiresAt),
		AbsoluteExpiresAt: requiredTimestamptz(mutation.Session.AbsoluteExpiry),
		ReauthenticatedAt: requiredTimestamptz(mutation.Session.ReauthenticatedAt),
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

func (u *postgresLoginUnitOfWork) RecordLoginFailure(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.LoginFailureMutation,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	if mutation.FailedAt.IsZero() || !mutation.LockUntil.After(mutation.FailedAt) ||
		mutation.Audit.ID == uuid.Nil || mutation.Audit.ActorID != uuid.Nil ||
		mutation.Audit.AuthenticationMethod != biz.AuditAuthenticationMethodAnonymous {
		return biz.ErrInvalidPersistenceState
	}
	if mutation.PrincipalID == uuid.Nil {
		if mutation.Audit.Boundary != biz.AuditBoundaryPrincipal || mutation.Audit.TargetID != mutation.Audit.ID {
			return biz.ErrInvalidPersistenceState
		}
	} else if mutation.Audit.Boundary != biz.AuditBoundaryTenant || mutation.Audit.TargetID != mutation.PrincipalID {
		return biz.ErrInvalidPersistenceState
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin login failure unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if mutation.PrincipalID == uuid.Nil {
		if err := appendAnonymousPrincipalAudit(ctx, queries, mutation.Audit); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return mapPostgresError("commit unknown login failure unit of work", err, nil)
		}
		committed = true
		return nil
	}
	state, err := queries.RecordPasswordLoginFailure(ctx, sqlcgen.RecordPasswordLoginFailureParams{
		FailedAt:    requiredTimestamptz(mutation.FailedAt),
		LockUntil:   requiredTimestamptz(mutation.LockUntil),
		PrincipalID: mutation.PrincipalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrInvalidPersistenceState
	}
	if err != nil {
		return mapPostgresError("record password login failure", err, nil)
	}
	mutation.Audit.TargetVersion = state.Version
	if err := appendAnonymousTenantAudit(ctx, queries, tenantID, mutation.Audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit login failure unit of work", err, nil)
	}
	committed = true
	return nil
}

func (u *postgresLoginUnitOfWork) RequestPasswordAction(
	ctx context.Context,
	mutation biz.PasswordActionRequestMutation,
) (biz.RequestPasswordActionResult, error) {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.RequestPasswordActionResult{}, mapPostgresError("begin password-action request", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockPasswordAuthenticationIdempotencyKey(ctx, sqlcgen.LockPasswordAuthenticationIdempotencyKeyParams{
		IdempotencyKey: mutation.IdempotencyKey,
	}); err != nil {
		return biz.RequestPasswordActionResult{}, mapPostgresError("lock password-action request idempotency", err, nil)
	}
	existing, err := queries.GetPasswordActionRequestByIdempotencyKey(ctx, sqlcgen.GetPasswordActionRequestByIdempotencyKeyParams{
		IdempotencyKey: mutation.IdempotencyKey,
	})
	if err == nil {
		if !bytes.Equal(existing.AccountDigest, mutation.AccountDigest[:]) || existing.Audience != string(mutation.Audience) {
			return biz.RequestPasswordActionResult{}, biz.ErrIdempotencyConflict
		}
		if !existing.ExpiresAt.Valid {
			return biz.RequestPasswordActionResult{}, biz.ErrInvalidPersistenceState
		}
		if err := tx.Commit(ctx); err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("commit idempotent password-action request", err, nil)
		}
		committed = true
		return biz.RequestPasswordActionResult{OperationID: existing.OperationID, ExpiresAt: existing.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return biz.RequestPasswordActionResult{}, mapPostgresError("lookup password-action idempotency", err, nil)
	}
	if mutation.Audit == nil || mutation.Audit.TargetID != mutation.OperationID {
		return biz.RequestPasswordActionResult{}, biz.ErrInvalidPersistenceState
	}
	if mutation.Target == nil {
		if mutation.Notification != nil || mutation.Purpose != "" {
			return biz.RequestPasswordActionResult{}, biz.ErrInvalidPersistenceState
		}
		err = queries.CreateUnknownPasswordActionRequest(ctx, sqlcgen.CreateUnknownPasswordActionRequestParams{
			OperationID:    mutation.OperationID,
			AccountDigest:  mutation.AccountDigest[:],
			Audience:       string(mutation.Audience),
			ExpiresAt:      requiredTimestamptz(mutation.ExpiresAt),
			IdempotencyKey: mutation.IdempotencyKey,
			CreatedAt:      requiredTimestamptz(mutation.CreatedAt),
		})
		if err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("create unknown password-action request", err, biz.ErrIdempotencyConflict)
		}
	} else {
		if mutation.Notification == nil || mutation.Purpose == "" ||
			mutation.Target.PrincipalID != mutation.Notification.PrincipalID ||
			mutation.OperationID != mutation.Notification.OperationID ||
			mutation.Purpose != mutation.Notification.Purpose ||
			mutation.Target.VerifiedEmail != mutation.Notification.DestinationEmail ||
			strings.TrimSpace(mutation.Notification.DestinationEmail) == "" ||
			strings.ToLower(strings.TrimSpace(mutation.Notification.DestinationEmail)) != mutation.Notification.DestinationEmail {
			return biz.RequestPasswordActionResult{}, biz.ErrInvalidPersistenceState
		}
		if err := queries.LockPasswordActionPrincipal(ctx, sqlcgen.LockPasswordActionPrincipalParams{
			PrincipalID: mutation.Target.PrincipalID,
		}); err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("lock password-action principal", err, nil)
		}
		err = queries.CreateKnownPasswordActionRequest(ctx, sqlcgen.CreateKnownPasswordActionRequestParams{
			OperationID:    mutation.OperationID,
			AccountDigest:  mutation.AccountDigest[:],
			Audience:       string(mutation.Audience),
			PrincipalID:    requiredPGUUID(mutation.Target.PrincipalID),
			Purpose:        requiredPGText(string(mutation.Purpose)),
			ExpiresAt:      requiredTimestamptz(mutation.ExpiresAt),
			IdempotencyKey: mutation.IdempotencyKey,
			CreatedAt:      requiredTimestamptz(mutation.CreatedAt),
		})
		if err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("create known password-action request", err, biz.ErrIdempotencyConflict)
		}
		_, err = queries.ReplaceActivePasswordActions(ctx, sqlcgen.ReplaceActivePasswordActionsParams{
			ReplacedBy:  requiredPGUUID(mutation.OperationID),
			UpdatedAt:   requiredTimestamptz(mutation.CreatedAt),
			PrincipalID: mutation.Target.PrincipalID,
		})
		if err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("replace active password actions", err, nil)
		}
		if err := queries.CancelReplacedPasswordActionNotifications(ctx, sqlcgen.CancelReplacedPasswordActionNotificationsParams{
			UpdatedAt:   requiredTimestamptz(mutation.CreatedAt),
			PrincipalID: mutation.Target.PrincipalID,
			ReplacedBy:  requiredPGUUID(mutation.OperationID),
		}); err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("cancel replaced password-action notifications", err, nil)
		}
		if err := queries.CreatePasswordAction(ctx, sqlcgen.CreatePasswordActionParams{
			OperationID: mutation.OperationID,
			PrincipalID: mutation.Target.PrincipalID,
			Purpose:     string(mutation.Purpose),
			ExpiresAt:   requiredTimestamptz(mutation.ExpiresAt),
			CreatedAt:   requiredTimestamptz(mutation.CreatedAt),
		}); err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("create password action", err, nil)
		}
		intent := "password_setup"
		if mutation.Purpose == biz.PasswordActionPurposeReset {
			intent = "password_reset"
		}
		if err := queries.CreatePasswordActionNotification(ctx, sqlcgen.CreatePasswordActionNotificationParams{
			ID:               mutation.Notification.ID,
			OperationID:      mutation.OperationID,
			PrincipalID:      mutation.Target.PrincipalID,
			Intent:           intent,
			DestinationEmail: mutation.Notification.DestinationEmail,
			AvailableAt:      requiredTimestamptz(mutation.CreatedAt),
			CreatedAt:        requiredTimestamptz(mutation.CreatedAt),
		}); err != nil {
			return biz.RequestPasswordActionResult{}, mapPostgresError("create password-action notification", err, nil)
		}
	}
	if err := appendAnonymousPrincipalAudit(ctx, queries, *mutation.Audit); err != nil {
		return biz.RequestPasswordActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.RequestPasswordActionResult{}, mapPostgresError("commit password-action request", err, nil)
	}
	committed = true
	return biz.RequestPasswordActionResult{OperationID: mutation.OperationID, ExpiresAt: mutation.ExpiresAt.UTC()}, nil
}

func (u *postgresLoginUnitOfWork) CompletePasswordAction(
	ctx context.Context,
	completion biz.PasswordActionCompletion,
) (biz.CompletePasswordActionResult, error) {
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("begin password-action completion", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	if err := queries.LockPasswordAuthenticationIdempotencyKey(ctx, sqlcgen.LockPasswordAuthenticationIdempotencyKeyParams{
		IdempotencyKey: completion.IdempotencyKey,
	}); err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("lock password-action completion idempotency", err, nil)
	}
	existing, err := queries.GetPasswordActionCompletionByIdempotencyKey(ctx, sqlcgen.GetPasswordActionCompletionByIdempotencyKeyParams{
		IdempotencyKey: completion.IdempotencyKey,
	})
	if err == nil {
		if existing.OperationID != completion.Claims.OperationID || existing.PrincipalID != completion.Claims.PrincipalID {
			return biz.CompletePasswordActionResult{}, biz.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return biz.CompletePasswordActionResult{}, mapPostgresError("commit idempotent password-action completion", err, nil)
		}
		committed = true
		return biz.CompletePasswordActionResult{
			PrincipalID:       existing.PrincipalID,
			CredentialVersion: existing.CredentialVersion,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return biz.CompletePasswordActionResult{}, mapPostgresError("lookup password-action completion idempotency", err, nil)
	}
	action, err := queries.LockPasswordAction(ctx, sqlcgen.LockPasswordActionParams{OperationID: completion.Claims.OperationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.CompletePasswordActionResult{}, biz.ErrPasswordActionInvalid
	}
	if err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("lock password action", err, nil)
	}
	if action.PrincipalID != completion.Claims.PrincipalID ||
		action.Purpose != string(completion.Claims.Purpose) ||
		action.Status != "active" || action.Version != 1 || !action.ExpiresAt.Valid ||
		!action.ExpiresAt.Time.Equal(completion.Claims.ExpiresAt) ||
		!completion.CompletedAt.Before(action.ExpiresAt.Time) {
		return biz.CompletePasswordActionResult{}, biz.ErrPasswordActionInvalid
	}
	actionVersion, err := queries.ConsumePasswordAction(ctx, sqlcgen.ConsumePasswordActionParams{
		CompletedAt:     requiredTimestamptz(completion.CompletedAt),
		OperationID:     completion.Claims.OperationID,
		PrincipalID:     completion.Claims.PrincipalID,
		Purpose:         string(completion.Claims.Purpose),
		ExpiresAt:       requiredTimestamptz(completion.Claims.ExpiresAt),
		ExpectedVersion: action.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.CompletePasswordActionResult{}, biz.ErrPasswordActionInvalid
	}
	if err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("consume password action", err, nil)
	}
	var credentialVersion int64
	switch completion.Claims.Purpose {
	case biz.PasswordActionPurposeReset:
		credentialVersion, err = queries.UpdatePasswordCredentialForReset(ctx, sqlcgen.UpdatePasswordCredentialForResetParams{
			PasswordHash: completion.PasswordHash,
			UpdatedAt:    requiredTimestamptz(completion.CompletedAt),
			PrincipalID:  completion.Claims.PrincipalID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.CompletePasswordActionResult{}, biz.ErrPasswordActionInvalid
		}
		if err != nil {
			return biz.CompletePasswordActionResult{}, mapPostgresError("update reset password credential", err, nil)
		}
		if err := revokePrincipalSessions(ctx, queries, completion.Claims.PrincipalID, completion.CompletedAt); err != nil {
			return biz.CompletePasswordActionResult{}, err
		}
	case biz.PasswordActionPurposeSetup:
		account, lookupErr := queries.GetVerifiedAccountForPrincipal(ctx, sqlcgen.GetVerifiedAccountForPrincipalParams{
			PrincipalID: completion.Claims.PrincipalID,
		})
		if lookupErr != nil {
			return biz.CompletePasswordActionResult{}, mapPostgresError("lookup verified account for password setup", lookupErr, nil)
		}
		if err := queries.CreatePasswordIdentity(ctx, sqlcgen.CreatePasswordIdentityParams{
			ID:          completion.IdentityID,
			PrincipalID: completion.Claims.PrincipalID,
			Subject:     account,
			CreatedAt:   requiredTimestamptz(completion.CompletedAt),
		}); err != nil {
			return biz.CompletePasswordActionResult{}, mapPostgresError("create password identity", err, biz.ErrPasswordActionInvalid)
		}
		credentialVersion, err = queries.CreatePasswordCredential(ctx, sqlcgen.CreatePasswordCredentialParams{
			PrincipalID:  completion.Claims.PrincipalID,
			IdentityID:   completion.IdentityID,
			PasswordHash: completion.PasswordHash,
			CreatedAt:    requiredTimestamptz(completion.CompletedAt),
		})
		if err != nil {
			return biz.CompletePasswordActionResult{}, mapPostgresError("create password credential", err, biz.ErrPasswordActionInvalid)
		}
	default:
		return biz.CompletePasswordActionResult{}, biz.ErrPasswordActionInvalid
	}
	if err := queries.CancelPasswordActionNotification(ctx, sqlcgen.CancelPasswordActionNotificationParams{
		UpdatedAt:   requiredTimestamptz(completion.CompletedAt),
		OperationID: completion.Claims.OperationID,
	}); err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("cancel completed password-action notification", err, nil)
	}
	if completion.Audit.TargetVersion != actionVersion {
		return biz.CompletePasswordActionResult{}, biz.ErrInvalidPersistenceState
	}
	if err := appendPrincipalAudit(ctx, queries, completion.Audit); err != nil {
		return biz.CompletePasswordActionResult{}, err
	}
	if err := queries.CreatePasswordActionCompletion(ctx, sqlcgen.CreatePasswordActionCompletionParams{
		IdempotencyKey:    completion.IdempotencyKey,
		OperationID:       completion.Claims.OperationID,
		PrincipalID:       completion.Claims.PrincipalID,
		CredentialVersion: credentialVersion,
		CompletedAt:       requiredTimestamptz(completion.CompletedAt),
	}); err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("create password-action completion", err, biz.ErrIdempotencyConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.CompletePasswordActionResult{}, mapPostgresError("commit password-action completion", err, nil)
	}
	committed = true
	return biz.CompletePasswordActionResult{
		PrincipalID:       completion.Claims.PrincipalID,
		CredentialVersion: credentialVersion,
	}, nil
}

func revokePrincipalSessions(ctx context.Context, queries *sqlcgen.Queries, principalID uuid.UUID, updatedAt time.Time) error {
	if err := queries.RevokeRefreshTokensForPrincipal(ctx, sqlcgen.RevokeRefreshTokensForPrincipalParams{PrincipalID: principalID}); err != nil {
		return mapPostgresError("revoke principal refresh tokens", err, nil)
	}
	if err := queries.RevokeRefreshTokenFamiliesForPrincipal(ctx, sqlcgen.RevokeRefreshTokenFamiliesForPrincipalParams{
		UpdatedAt:   requiredTimestamptz(updatedAt),
		PrincipalID: principalID,
	}); err != nil {
		return mapPostgresError("revoke principal refresh-token families", err, nil)
	}
	if err := queries.RevokeSessionGrantsForPrincipal(ctx, sqlcgen.RevokeSessionGrantsForPrincipalParams{
		UpdatedAt:   requiredTimestamptz(updatedAt),
		PrincipalID: principalID,
	}); err != nil {
		return mapPostgresError("revoke principal session grants", err, nil)
	}
	if err := queries.RevokeSessionsForPrincipal(ctx, sqlcgen.RevokeSessionsForPrincipalParams{
		UpdatedAt:   requiredTimestamptz(updatedAt),
		PrincipalID: principalID,
	}); err != nil {
		return mapPostgresError("revoke principal sessions", err, nil)
	}
	return nil
}

func appendAnonymousPrincipalAudit(ctx context.Context, queries *sqlcgen.Queries, event biz.SecurityAuditEvent) error {
	if event.ActorID != uuid.Nil || event.AuthenticationMethod != biz.AuditAuthenticationMethodAnonymous || event.Boundary != biz.AuditBoundaryPrincipal {
		return biz.ErrInvalidPersistenceState
	}
	err := queries.AppendAnonymousPrincipalSecurityAuditEvent(ctx, sqlcgen.AppendAnonymousPrincipalSecurityAuditEventParams{
		EventID:       event.ID,
		Action:        string(event.Action),
		TargetType:    string(event.TargetType),
		TargetID:      event.TargetID,
		TargetVersion: event.TargetVersion,
		Result:        string(event.Result),
		Reason:        string(event.Reason),
		RequestID:     event.RequestID,
		CorrelationID: event.CorrelationID,
		DecisionID:    event.DecisionID,
		SourceService: string(event.SourceService),
		OccurredAt:    requiredTimestamptz(event.OccurredAt),
		RecordedAt:    requiredTimestamptz(event.RecordedAt),
	})
	if err != nil {
		return mapPostgresError("append anonymous principal audit", err, biz.ErrAuditConflict)
	}
	return nil
}

func appendAnonymousTenantAudit(ctx context.Context, queries *sqlcgen.Queries, tenantID uuid.UUID, event biz.SecurityAuditEvent) error {
	if tenantID == uuid.Nil || event.ActorID != uuid.Nil ||
		event.AuthenticationMethod != biz.AuditAuthenticationMethodAnonymous || event.Boundary != biz.AuditBoundaryTenant {
		return biz.ErrInvalidPersistenceState
	}
	err := queries.AppendAnonymousTenantSecurityAuditEvent(ctx, sqlcgen.AppendAnonymousTenantSecurityAuditEventParams{
		TenantID:      requiredPGUUID(tenantID),
		EventID:       event.ID,
		Action:        string(event.Action),
		TargetType:    string(event.TargetType),
		TargetID:      event.TargetID,
		TargetVersion: event.TargetVersion,
		Result:        string(event.Result),
		Reason:        string(event.Reason),
		RequestID:     event.RequestID,
		CorrelationID: event.CorrelationID,
		DecisionID:    event.DecisionID,
		SourceService: string(event.SourceService),
		OccurredAt:    requiredTimestamptz(event.OccurredAt),
		RecordedAt:    requiredTimestamptz(event.RecordedAt),
	})
	if err != nil {
		return mapPostgresError("append anonymous tenant audit", err, biz.ErrAuditConflict)
	}
	return nil
}

func appendPrincipalAudit(ctx context.Context, queries *sqlcgen.Queries, event biz.SecurityAuditEvent) error {
	if event.ActorID == uuid.Nil || event.AuthenticationMethod != biz.AuditAuthenticationMethodAction || event.Boundary != biz.AuditBoundaryPrincipal {
		return biz.ErrInvalidPersistenceState
	}
	err := queries.AppendPrincipalSecurityAuditEvent(ctx, sqlcgen.AppendPrincipalSecurityAuditEventParams{
		EventID:       event.ID,
		ActorID:       requiredPGUUID(event.ActorID),
		Action:        string(event.Action),
		TargetType:    string(event.TargetType),
		TargetID:      event.TargetID,
		TargetVersion: event.TargetVersion,
		Result:        string(event.Result),
		Reason:        string(event.Reason),
		RequestID:     event.RequestID,
		CorrelationID: event.CorrelationID,
		DecisionID:    event.DecisionID,
		SourceService: string(event.SourceService),
		OccurredAt:    requiredTimestamptz(event.OccurredAt),
		RecordedAt:    requiredTimestamptz(event.RecordedAt),
	})
	if err != nil {
		return mapPostgresError("append principal audit", err, biz.ErrAuditConflict)
	}
	return nil
}

func requiredPGUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

func requiredPGText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
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
	queries := sqlcgen.New(r.data.pool)
	if _, err := queries.GetTenantAccessStatusForAuthorization(ctx, sqlcgen.GetTenantAccessStatusForAuthorizationParams{TenantID: tenantID}); errors.Is(err, pgx.ErrNoRows) {
		return biz.AuthorizationState{}, biz.ErrTenantIAMNotReady
	} else if err != nil {
		return biz.AuthorizationState{}, mapPostgresError("get tenant access for authorization", err, nil)
	}
	lifecycleFresh, err := queries.GetTenantLifecycleFreshnessForAuthorization(ctx, sqlcgen.GetTenantLifecycleFreshnessForAuthorizationParams{TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !lifecycleFresh) {
		return biz.AuthorizationState{}, biz.ErrTenantLifecycleStale
	}
	if err != nil {
		return biz.AuthorizationState{}, mapPostgresError("get tenant lifecycle for authorization", err, nil)
	}
	row, err := queries.LookupAuthorization(ctx, sqlcgen.LookupAuthorizationParams{
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
	if !row.LifecycleFresh {
		return biz.AuthorizationState{}, biz.ErrTenantLifecycleStale
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
	_ biz.AuthenticationReader     = (*postgresPasswordLoginReader)(nil)
	_ biz.AuthenticationUnitOfWork = (*postgresLoginUnitOfWork)(nil)
	_ biz.AuthorizationReader      = (*postgresAuthorizationReader)(nil)
)
