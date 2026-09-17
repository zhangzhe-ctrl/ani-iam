package data

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"time"
)

func (t *platformAdministrationTransaction) AdministratorCandidates(ctx context.Context, cap biz.PlatformCapability, policy biz.PlatformAdminLoginPolicy) ([]biz.PlatformAdministratorCandidate, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return nil, err
	}
	rows, err := t.q.ListPlatformAdministratorCandidates(ctx, sqlcgen.ListPlatformAdministratorCandidatesParams{OidcProvider: policy.OIDCProvider, OidcIssuer: policy.OIDCIssuer, PasswordEnabled: policy.PasswordEnabled})
	if err != nil {
		return nil, platformLoginPersistenceError(err)
	}
	result := make([]biz.PlatformAdministratorCandidate, 0, len(rows))
	for _, row := range rows {
		result = append(result, biz.PlatformAdministratorCandidate{MembershipID: row.MembershipID, LoginCapable: row.LoginCapable})
	}
	return result, nil
}
func (t *platformAdministrationTransaction) UpdateMembership(ctx context.Context, cap biz.PlatformCapability, id uuid.UUID, state biz.MembershipStatus, version int64, now time.Time) (biz.PlatformMembership, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformMembership{}, err
	}
	r, err := t.q.UpdatePlatformMembershipStatus(ctx, sqlcgen.UpdatePlatformMembershipStatusParams{ID: id, Status: string(state), ExpectedVersion: version, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.PlatformMembership{}, platformLoginPersistenceError(err)
	}
	return t.membership(ctx, r)
}
func (t *platformAdministrationTransaction) BindMembershipRole(ctx context.Context, cap biz.PlatformCapability, b biz.PlatformRoleBinding, version int64, now time.Time) (biz.PlatformMembership, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformMembership{}, err
	}
	err := t.q.CreatePlatformRoleBinding(ctx, sqlcgen.CreatePlatformRoleBindingParams{ID: b.ID, MembershipID: b.MembershipID, RoleID: b.RoleID, Now: requiredTimestamptz(now)})
	if err != nil {
		var p *pgconn.PgError
		if errors.As(err, &p) && p.Code == "23505" {
			return biz.PlatformMembership{}, biz.ErrRoleBindingConflict
		}
		return biz.PlatformMembership{}, platformLoginPersistenceError(err)
	}
	r, err := t.q.AdvancePlatformMembershipVersion(ctx, sqlcgen.AdvancePlatformMembershipVersionParams{ID: b.MembershipID, ExpectedVersion: version, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.PlatformMembership{}, platformLoginPersistenceError(err)
	}
	return t.membership(ctx, r)
}
func (t *platformAdministrationTransaction) UnbindMembershipRole(ctx context.Context, cap biz.PlatformCapability, member, role uuid.UUID, version int64, now time.Time) (biz.PlatformMembership, biz.PlatformRoleBinding, error) {
	if _, _, err := cap.CredentialBinding(); err != nil {
		return biz.PlatformMembership{}, biz.PlatformRoleBinding{}, err
	}
	b, err := t.q.RemovePlatformMembershipRoleBinding(ctx, sqlcgen.RemovePlatformMembershipRoleBindingParams{MembershipID: member, RoleID: role})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformMembership{}, biz.PlatformRoleBinding{}, biz.ErrRoleBindingNotFound
	}
	if err != nil {
		return biz.PlatformMembership{}, biz.PlatformRoleBinding{}, platformLoginPersistenceError(err)
	}
	r, err := t.q.AdvancePlatformMembershipVersionForUnbind(ctx, sqlcgen.AdvancePlatformMembershipVersionForUnbindParams{ID: member, ExpectedVersion: version, Now: requiredTimestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.PlatformMembership{}, biz.PlatformRoleBinding{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.PlatformMembership{}, biz.PlatformRoleBinding{}, platformLoginPersistenceError(err)
	}
	m, err := t.membership(ctx, r)
	return m, biz.PlatformRoleBinding{ID: b.ID, MembershipID: b.MembershipID, RoleID: b.RoleID, Version: b.Version}, err
}
