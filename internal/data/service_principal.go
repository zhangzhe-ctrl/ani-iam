package data

import (
	"context"
	"crypto/sha256"
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

func NewPostgresServicePrincipalUnitOfWork(data *Data) biz.ServicePrincipalUnitOfWork {
	return &postgresTenantAuthorizationUnitOfWork{data: data}
}

func (r *postgresTenantAuthorizationReader) GetServicePrincipalBoundary(ctx context.Context, principalID uuid.UUID) (uuid.UUID, error) {
	tenantID, err := sqlcgen.New(r.data.pool).GetServicePrincipalBoundary(ctx, sqlcgen.GetServicePrincipalBoundaryParams{PrincipalID: principalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, biz.ErrServicePrincipalNotFound
	}
	if err != nil {
		return uuid.Nil, mapPostgresError("get service principal boundary", err, nil)
	}
	if tenantID == uuid.Nil {
		return uuid.Nil, biz.ErrInvalidPersistenceState
	}
	return tenantID, nil
}

func (r *postgresTenantAuthorizationReader) GetServicePrincipal(ctx context.Context, scope biz.TenantScope, principalID uuid.UUID) (biz.ServicePrincipal, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.ServicePrincipal{}, err
	}
	row, err := sqlcgen.New(r.data.pool).GetServicePrincipal(ctx, sqlcgen.GetServicePrincipalParams{TenantID: tenantID, PrincipalID: principalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ServicePrincipal{}, biz.ErrServicePrincipalNotFound
	}
	if err != nil {
		return biz.ServicePrincipal{}, mapPostgresError("get service principal", err, nil)
	}
	principal, err := servicePrincipalFromValues(row.PrincipalID, row.MembershipID, row.Name, row.NormalizedName, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
	if err != nil {
		return biz.ServicePrincipal{}, err
	}
	return principal, nil
}

func (r *postgresTenantAuthorizationReader) ListServicePrincipals(ctx context.Context, scope biz.TenantScope, status biz.PrincipalStatus, cursor uuid.UUID, pageSize int32) (biz.ServicePrincipalPage, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.ServicePrincipalPage{}, err
	}
	if pageSize <= 0 || pageSize > 100 {
		return biz.ServicePrincipalPage{}, biz.ErrInvalidPersistenceState
	}
	rows, err := sqlcgen.New(r.data.pool).ListServicePrincipals(ctx, sqlcgen.ListServicePrincipalsParams{
		TenantID: tenantID, CursorID: cursor, Status: string(status), PageLimit: pageSize + 1,
	})
	if err != nil {
		return biz.ServicePrincipalPage{}, mapPostgresError("list service principals", err, nil)
	}
	page := biz.ServicePrincipalPage{Items: make([]biz.ServicePrincipalRecord, 0, min(len(rows), int(pageSize)))}
	for index, row := range rows {
		if index == int(pageSize) {
			page.NextCursor = page.Items[len(page.Items)-1].Principal.ID
			break
		}
		principal, err := servicePrincipalFromValues(row.PrincipalID, row.MembershipID, row.Name, row.NormalizedName, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
		if err != nil {
			return biz.ServicePrincipalPage{}, err
		}
		page.Items = append(page.Items, biz.ServicePrincipalRecord{TenantID: row.TenantID, Principal: principal})
	}
	return page, nil
}

func (r *postgresTenantAuthorizationReader) ListAPIKeys(ctx context.Context, scope biz.TenantScope, principalID, cursor uuid.UUID, pageSize int32) (biz.APIKeyPage, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.APIKeyPage{}, err
	}
	if pageSize <= 0 || pageSize > 100 {
		return biz.APIKeyPage{}, biz.ErrInvalidPersistenceState
	}
	rows, err := sqlcgen.New(r.data.pool).ListAPIKeys(ctx, sqlcgen.ListAPIKeysParams{
		TenantID: tenantID, PrincipalID: principalID, CursorID: cursor, PageLimit: pageSize + 1,
	})
	if err != nil {
		return biz.APIKeyPage{}, mapPostgresError("list API keys", err, nil)
	}
	page := biz.APIKeyPage{Items: make([]biz.APIKey, 0, min(len(rows), int(pageSize)))}
	for index, row := range rows {
		if index == int(pageSize) {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		apiKey, err := apiKeyFromValues(row.KeyID, row.PrincipalID, row.Status, row.DisplayPrefix, row.SecretDigest, row.NeverExpires, row.ExpiresAt, row.CreatedAt, row.LastUsedAt, row.RevokedAt, row.Version)
		if err != nil {
			return biz.APIKeyPage{}, err
		}
		page.Items = append(page.Items, apiKey)
	}
	return page, nil
}

func (r *postgresTenantAuthorizationReader) GetAPIKeyBoundary(ctx context.Context, keyID uuid.UUID) (uuid.UUID, error) {
	tenantID, err := sqlcgen.New(r.data.pool).GetAPIKeyBoundary(ctx, sqlcgen.GetAPIKeyBoundaryParams{KeyID: keyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, biz.ErrAPIKeyNotFound
	}
	if err != nil {
		return uuid.Nil, mapPostgresError("get API key boundary", err, nil)
	}
	if tenantID == uuid.Nil {
		return uuid.Nil, biz.ErrInvalidPersistenceState
	}
	return tenantID, nil
}

func (r *postgresTenantAuthorizationReader) GetAPIKeyOperationalSignals(
	ctx context.Context,
	scope biz.TenantScope,
	principalID uuid.UUID,
	observedAt time.Time,
) (biz.APIKeyOperationalSignals, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return biz.APIKeyOperationalSignals{}, err
	}
	if principalID == uuid.Nil || observedAt.IsZero() {
		return biz.APIKeyOperationalSignals{}, biz.ErrInvalidPersistenceState
	}
	row, err := sqlcgen.New(r.data.pool).GetAPIKeyOperationalSignals(ctx, sqlcgen.GetAPIKeyOperationalSignalsParams{
		ObservedAt: requiredTimestamptz(observedAt), StaleBefore: requiredTimestamptz(observedAt.Add(-biz.APIKeyStaleAfter)),
		TenantID: tenantID, PrincipalID: principalID,
	})
	if err != nil {
		return biz.APIKeyOperationalSignals{}, mapPostgresError("get API key operational signals", err, nil)
	}
	return biz.APIKeyOperationalSignals{
		ActiveCount: row.ActiveCount, StaleNonExpiringCount: row.StaleNonExpiringCount,
		UnusualActiveCount: row.ActiveCount >= biz.APIKeyUnusualActiveCountThreshold, ObservedAt: observedAt.UTC(),
	}, nil
}

type postgresAPIKeyOperationalSnapshotReader struct {
	data *Data
}

func NewPostgresAPIKeyOperationalSnapshotReader(data *Data) biz.APIKeyOperationalSnapshotReader {
	return &postgresAPIKeyOperationalSnapshotReader{data: data}
}

func (r *postgresAPIKeyOperationalSnapshotReader) GetAPIKeyOperationalSnapshot(
	ctx context.Context,
	observedAt time.Time,
) (biz.APIKeyOperationalSnapshot, error) {
	if observedAt.IsZero() {
		return biz.APIKeyOperationalSnapshot{}, biz.ErrInvalidPersistenceState
	}
	row, err := sqlcgen.New(r.data.pool).GetAPIKeyOperationalSnapshot(ctx, sqlcgen.GetAPIKeyOperationalSnapshotParams{
		ObservedAt:                  requiredTimestamptz(observedAt),
		StaleBefore:                 requiredTimestamptz(observedAt.Add(-biz.APIKeyStaleAfter)),
		UnusualActiveCountThreshold: biz.APIKeyUnusualActiveCountThreshold,
	})
	if err != nil {
		return biz.APIKeyOperationalSnapshot{}, mapPostgresError("get API key operational snapshot", err, nil)
	}
	return biz.APIKeyOperationalSnapshot{
		StaleNonExpiringCount:        row.StaleNonExpiringCount,
		UnusualServicePrincipalCount: row.UnusualServicePrincipalCount,
		ObservedAt:                   observedAt.UTC(),
	}, nil
}

func servicePrincipalFromValues(
	principalID, membershipID uuid.UUID,
	name, normalizedName, status string,
	version int64,
	createdAt, updatedAt pgtype.Timestamptz,
) (biz.ServicePrincipal, error) {
	if principalID == uuid.Nil || membershipID == uuid.Nil || !createdAt.Valid || !updatedAt.Valid || version <= 0 {
		return biz.ServicePrincipal{}, biz.ErrInvalidPersistenceState
	}
	return biz.ServicePrincipal{
		ID: principalID, MembershipID: membershipID, Name: name, NormalizedName: normalizedName,
		Status: biz.PrincipalStatus(status), Version: version,
		CreatedAt: createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
	}, nil
}

func (u *postgresTenantAuthorizationUnitOfWork) WithinServicePrincipal(
	ctx context.Context,
	scope biz.TenantScope,
	fn func(context.Context, biz.ServicePrincipalTransaction) error,
) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin service principal unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	queries := sqlcgen.New(tx)
	transaction := postgresServicePrincipalTransaction{
		postgresTenantAuthorizationTransaction: postgresTenantAuthorizationTransaction{queries: queries, tenantID: tenantID},
	}
	if err := fn(ctx, transaction); err != nil {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, mapPostgresError("rollback service principal unit of work", rollbackErr, nil))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit service principal unit of work", err, biz.ErrServicePrincipalConflict)
	}
	committed = true
	return nil
}

type postgresServicePrincipalTransaction struct {
	postgresTenantAuthorizationTransaction
}

func (postgresServicePrincipalTransaction) IssueAPIKeyCredential(ctx context.Context, keyID uuid.UUID) (biz.APIKeyCredential, error) {
	if keyID == uuid.Nil {
		return biz.APIKeyCredential{}, biz.ErrInvalidPersistenceState
	}
	if err := ctx.Err(); err != nil {
		return biz.APIKeyCredential{}, err
	}
	opaqueSecret, err := NewSecretGenerator().NewSecret()
	if err != nil {
		return biz.APIKeyCredential{}, fmt.Errorf("generate API key credential: %w", err)
	}
	if strings.TrimSpace(opaqueSecret) == "" {
		return biz.APIKeyCredential{}, biz.ErrAuthenticationDependency
	}
	raw := "ani_" + keyID.String() + "_" + opaqueSecret
	return biz.APIKeyCredential{
		Raw: raw, DisplayPrefix: "ani_" + keyID.String()[:8], Digest: sha256.Sum256([]byte(raw)),
	}, nil
}

func (tx postgresTenantAuthorizationTransaction) DisableServicePrincipalForMembership(
	ctx context.Context,
	scope biz.TenantScope,
	membershipID uuid.UUID,
	updatedAt time.Time,
) (biz.ServicePrincipal, int64, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.ServicePrincipal{}, 0, err
	}
	row, err := tx.queries.GetServicePrincipalByMembershipForUpdate(ctx, sqlcgen.GetServicePrincipalByMembershipForUpdateParams{
		TenantID: tenantID, MembershipID: membershipID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ServicePrincipal{}, 0, biz.ErrServicePrincipalNotFound
	}
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("get service principal by membership", err, nil)
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid || row.Version <= 0 {
		return biz.ServicePrincipal{}, 0, biz.ErrInvalidPersistenceState
	}
	updatedAt = updatedAt.UTC()
	rows, err := tx.queries.UpdateServicePrincipalBaseStatus(ctx, sqlcgen.UpdateServicePrincipalBaseStatusParams{
		Status: string(biz.PrincipalStatusDisabled), UpdatedAt: requiredTimestamptz(updatedAt),
		PrincipalID: row.PrincipalID, ExpectedVersion: row.Version,
	})
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("disable service principal base", err, biz.ErrServicePrincipalConflict)
	}
	if rows != 1 {
		return biz.ServicePrincipal{}, 0, biz.ErrVersionConflict
	}
	profile, err := tx.queries.UpdateServicePrincipalProfile(ctx, sqlcgen.UpdateServicePrincipalProfileParams{
		Name: row.Name, NormalizedName: row.NormalizedName, UpdatedAt: requiredTimestamptz(updatedAt),
		TenantID: tenantID, PrincipalID: row.PrincipalID, ExpectedVersion: row.Version,
	})
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("disable service principal profile", err, biz.ErrServicePrincipalConflict)
	}
	revoked, err := tx.queries.RevokeActiveAPIKeysForPrincipal(ctx, sqlcgen.RevokeActiveAPIKeysForPrincipalParams{
		RevokedAt: requiredTimestamptz(updatedAt), TenantID: tenantID, PrincipalID: row.PrincipalID,
	})
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("revoke service principal API keys", err, biz.ErrAPIKeyConflict)
	}
	return biz.ServicePrincipal{
		ID: profile.PrincipalID, MembershipID: profile.MembershipID, Name: profile.Name,
		NormalizedName: profile.NormalizedName, Status: biz.PrincipalStatusDisabled,
		Version: profile.Version, CreatedAt: profile.CreatedAt.Time.UTC(), UpdatedAt: profile.UpdatedAt.Time.UTC(),
	}, int64(len(revoked)), nil
}

func (tx postgresServicePrincipalTransaction) GetServicePrincipal(
	ctx context.Context,
	scope biz.TenantScope,
	principalID uuid.UUID,
) (biz.ServicePrincipal, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.ServicePrincipal{}, err
	}
	row, err := tx.queries.GetServicePrincipalForUpdate(ctx, sqlcgen.GetServicePrincipalForUpdateParams{
		TenantID: tenantID, PrincipalID: principalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ServicePrincipal{}, biz.ErrServicePrincipalNotFound
	}
	if err != nil {
		return biz.ServicePrincipal{}, mapPostgresError("get service principal", err, nil)
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return biz.ServicePrincipal{}, biz.ErrInvalidPersistenceState
	}
	return biz.ServicePrincipal{
		ID: row.PrincipalID, MembershipID: row.MembershipID, Name: row.Name,
		NormalizedName: row.NormalizedName, Status: biz.PrincipalStatus(row.Status),
		Version: row.Version, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

func (tx postgresServicePrincipalTransaction) CreateServicePrincipal(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.ServicePrincipalCreateMutation,
) error {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	if mutation.Principal.ID == uuid.Nil || mutation.Membership.PrincipalID != mutation.Principal.ID || mutation.Membership.ID != mutation.Principal.MembershipID {
		return biz.ErrInvalidPersistenceState
	}
	if err := tx.queries.CreateServicePrincipalBase(ctx, sqlcgen.CreateServicePrincipalBaseParams{
		ID: mutation.Principal.ID, Status: string(mutation.Principal.Status), Version: mutation.Principal.Version,
		CreatedAt: requiredTimestamptz(mutation.Principal.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Principal.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create service principal base", err, biz.ErrServicePrincipalConflict)
	}
	if err := tx.queries.CreateTenantMembership(ctx, sqlcgen.CreateTenantMembershipParams{
		TenantID: tenantID, ID: mutation.Membership.ID, PrincipalID: mutation.Membership.PrincipalID,
		Status: string(mutation.Membership.Status), Version: mutation.Membership.Version,
		CreatedAt: requiredTimestamptz(mutation.Membership.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Membership.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create service principal membership", err, biz.ErrServicePrincipalConflict)
	}
	if err := tx.queries.CreateServicePrincipalProfile(ctx, sqlcgen.CreateServicePrincipalProfileParams{
		PrincipalID: mutation.Principal.ID, TenantID: tenantID, MembershipID: mutation.Principal.MembershipID,
		Name: mutation.Principal.Name, NormalizedName: mutation.Principal.NormalizedName, Version: mutation.Principal.Version,
		CreatedAt: requiredTimestamptz(mutation.Principal.CreatedAt), UpdatedAt: requiredTimestamptz(mutation.Principal.UpdatedAt),
	}); err != nil {
		return mapPostgresError("create service principal profile", err, biz.ErrServicePrincipalConflict)
	}
	for _, binding := range mutation.Bindings {
		if binding.MembershipID != mutation.Membership.ID {
			return biz.ErrTenantRelationConflict
		}
		if err := tx.queries.CreateTenantRoleBinding(ctx, sqlcgen.CreateTenantRoleBindingParams{
			TenantID: tenantID, ID: binding.ID, MembershipID: binding.MembershipID, RoleID: binding.RoleID,
			Version: binding.Version, CreatedAt: requiredTimestamptz(binding.CreatedAt), UpdatedAt: requiredTimestamptz(binding.UpdatedAt),
		}); err != nil {
			return mapPostgresError("create service principal role binding", err, biz.ErrServicePrincipalConflict)
		}
	}
	if err := tx.AppendAudit(ctx, scope, mutation.Audit); err != nil {
		return err
	}
	return nil
}

func (tx postgresServicePrincipalTransaction) CreateAPIKey(
	ctx context.Context,
	scope biz.TenantScope,
	mutation biz.APIKeyCreateMutation,
) error {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return err
	}
	apiKey := mutation.APIKey
	if apiKey.ID == uuid.Nil || apiKey.PrincipalID == uuid.Nil || apiKey.Version <= 0 || apiKey.Digest == ([32]byte{}) {
		return biz.ErrInvalidPersistenceState
	}
	expiresAt := pgtype.Timestamptz{}
	if !apiKey.ExpiresAt.IsZero() {
		expiresAt = requiredTimestamptz(apiKey.ExpiresAt)
	}
	lastUsedAt := pgtype.Timestamptz{}
	if !apiKey.LastUsedAt.IsZero() {
		lastUsedAt = requiredTimestamptz(apiKey.LastUsedAt)
	}
	revokedAt := pgtype.Timestamptz{}
	if !apiKey.RevokedAt.IsZero() {
		revokedAt = requiredTimestamptz(apiKey.RevokedAt)
	}
	if err := tx.queries.CreateAPIKey(ctx, sqlcgen.CreateAPIKeyParams{
		TenantID: tenantID, KeyID: apiKey.ID, PrincipalID: apiKey.PrincipalID,
		Status: string(apiKey.Status), DisplayPrefix: apiKey.DisplayPrefix, SecretDigest: apiKey.Digest[:],
		NeverExpires: apiKey.NeverExpires, ExpiresAt: expiresAt, CreatedAt: requiredTimestamptz(apiKey.CreatedAt),
		LastUsedAt: lastUsedAt, RevokedAt: revokedAt, Version: apiKey.Version,
	}); err != nil {
		return mapPostgresError("create API key", err, biz.ErrAPIKeyConflict)
	}
	if err := tx.AppendAudit(ctx, scope, mutation.Audit); err != nil {
		return err
	}
	return nil
}

func (tx postgresServicePrincipalTransaction) UpdateServicePrincipal(
	ctx context.Context,
	scope biz.TenantScope,
	principal biz.ServicePrincipal,
	expectedVersion int64,
) (biz.ServicePrincipal, int64, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.ServicePrincipal{}, 0, err
	}
	rows, err := tx.queries.UpdateServicePrincipalBaseStatus(ctx, sqlcgen.UpdateServicePrincipalBaseStatusParams{
		Status: string(principal.Status), UpdatedAt: requiredTimestamptz(principal.UpdatedAt),
		PrincipalID: principal.ID, ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("update service principal base", err, biz.ErrServicePrincipalConflict)
	}
	if rows != 1 {
		return biz.ServicePrincipal{}, 0, biz.ErrVersionConflict
	}
	row, err := tx.queries.UpdateServicePrincipalProfile(ctx, sqlcgen.UpdateServicePrincipalProfileParams{
		Name: principal.Name, NormalizedName: principal.NormalizedName, UpdatedAt: requiredTimestamptz(principal.UpdatedAt),
		TenantID: tenantID, PrincipalID: principal.ID, ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return biz.ServicePrincipal{}, 0, mapPostgresError("update service principal profile", err, biz.ErrServicePrincipalConflict)
	}
	var revokedCount int64
	if principal.Status == biz.PrincipalStatusDisabled {
		revoked, err := tx.queries.RevokeActiveAPIKeysForPrincipal(ctx, sqlcgen.RevokeActiveAPIKeysForPrincipalParams{
			RevokedAt: requiredTimestamptz(principal.UpdatedAt), TenantID: tenantID, PrincipalID: principal.ID,
		})
		if err != nil {
			return biz.ServicePrincipal{}, 0, mapPostgresError("revoke disabled service principal API keys", err, biz.ErrAPIKeyConflict)
		}
		revokedCount = int64(len(revoked))
	}
	return biz.ServicePrincipal{
		ID: row.PrincipalID, MembershipID: row.MembershipID, Name: row.Name, NormalizedName: row.NormalizedName,
		Status: principal.Status, Version: row.Version, CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, revokedCount, nil
}

func (tx postgresServicePrincipalTransaction) GetAPIKey(ctx context.Context, scope biz.TenantScope, keyID uuid.UUID) (biz.APIKey, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.APIKey{}, err
	}
	row, err := tx.queries.GetAPIKeyForUpdate(ctx, sqlcgen.GetAPIKeyForUpdateParams{TenantID: tenantID, KeyID: keyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.APIKey{}, biz.ErrAPIKeyNotFound
	}
	if err != nil {
		return biz.APIKey{}, mapPostgresError("get API key", err, nil)
	}
	return apiKeyFromValues(row.KeyID, row.PrincipalID, row.Status, row.DisplayPrefix, row.SecretDigest, row.NeverExpires, row.ExpiresAt, row.CreatedAt, row.LastUsedAt, row.RevokedAt, row.Version)
}

func (tx postgresServicePrincipalTransaction) RevokeAPIKey(ctx context.Context, scope biz.TenantScope, apiKey biz.APIKey, expectedVersion int64) (biz.APIKey, error) {
	tenantID, err := tenantIDForScope(tx.tenantID, scope)
	if err != nil {
		return biz.APIKey{}, err
	}
	row, err := tx.queries.RevokeAPIKey(ctx, sqlcgen.RevokeAPIKeyParams{
		RevokedAt: requiredTimestamptz(apiKey.RevokedAt), TenantID: tenantID,
		KeyID: apiKey.ID, ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return biz.APIKey{}, mapPostgresError("revoke API key", err, biz.ErrVersionConflict)
	}
	return apiKeyFromValues(row.KeyID, row.PrincipalID, row.Status, row.DisplayPrefix, row.SecretDigest, row.NeverExpires, row.ExpiresAt, row.CreatedAt, row.LastUsedAt, row.RevokedAt, row.Version)
}

func apiKeyFromValues(
	keyID, principalID uuid.UUID,
	status, displayPrefix string,
	digest []byte,
	neverExpires bool,
	expiresAt, createdAt, lastUsedAt, revokedAt pgtype.Timestamptz,
	version int64,
) (biz.APIKey, error) {
	if len(digest) != 32 || !createdAt.Valid || version <= 0 {
		return biz.APIKey{}, biz.ErrInvalidPersistenceState
	}
	apiKey := biz.APIKey{
		ID: keyID, PrincipalID: principalID, Status: biz.APIKeyStatus(status), DisplayPrefix: displayPrefix,
		NeverExpires: neverExpires, CreatedAt: createdAt.Time.UTC(), Version: version,
	}
	copy(apiKey.Digest[:], digest)
	if expiresAt.Valid {
		apiKey.ExpiresAt = expiresAt.Time.UTC()
	}
	if lastUsedAt.Valid {
		apiKey.LastUsedAt = lastUsedAt.Time.UTC()
	}
	if revokedAt.Valid {
		apiKey.RevokedAt = revokedAt.Time.UTC()
	}
	return apiKey, nil
}

var (
	_ biz.ServicePrincipalUnitOfWork      = (*postgresTenantAuthorizationUnitOfWork)(nil)
	_ biz.ServicePrincipalTransaction     = postgresServicePrincipalTransaction{}
	_ biz.ServicePrincipalReader          = (*postgresTenantAuthorizationReader)(nil)
	_ biz.APIKeyOperationalReader         = (*postgresTenantAuthorizationReader)(nil)
	_ biz.APIKeyOperationalSnapshotReader = (*postgresAPIKeyOperationalSnapshotReader)(nil)
)
