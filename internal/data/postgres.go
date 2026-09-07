package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type postgresUnitOfWork struct {
	data *Data
}

// NewPostgresUnitOfWork returns the biz port, not the data implementation.
func NewPostgresUnitOfWork(data *Data) biz.TenantUnitOfWork {
	return &postgresUnitOfWork{data: data}
}

func (u *postgresUnitOfWork) WithinTenant(ctx context.Context, scope biz.TenantScope, fn func(context.Context, biz.TenantTransaction) error) error {
	tenantID, err := scope.TenantID()
	if err != nil {
		return err
	}
	tx, err := u.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapPostgresError("begin tenant unit of work", err, nil)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	queries := sqlcgen.New(tx)
	transaction := postgresTenantTransaction{
		memberships: membershipRepository{queries: queries, tenantID: tenantID},
		audits:      securityAuditRepository{queries: queries, tenantID: tenantID},
	}
	if err := fn(ctx, transaction); err != nil {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, mapPostgresError("rollback tenant unit of work", rollbackErr, nil))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPostgresError("commit tenant unit of work", err, nil)
	}
	committed = true
	return nil
}

type postgresTenantTransaction struct {
	memberships membershipRepository
	audits      securityAuditRepository
}

func (tx postgresTenantTransaction) Memberships() biz.TenantMembershipRepository {
	return tx.memberships
}

func (tx postgresTenantTransaction) AuditEvents() biz.SecurityAuditRepository {
	return tx.audits
}

type membershipRepository struct {
	queries  *sqlcgen.Queries
	tenantID uuid.UUID
}

func (r membershipRepository) Create(ctx context.Context, scope biz.TenantScope, membership biz.TenantMembership) error {
	tenantID, err := tenantIDForScope(r.tenantID, scope)
	if err != nil {
		return err
	}
	err = r.queries.CreateTenantMembership(ctx, sqlcgen.CreateTenantMembershipParams{
		TenantID:    tenantID,
		ID:          membership.ID,
		PrincipalID: membership.PrincipalID,
		Status:      string(membership.Status),
		Version:     membership.Version,
		CreatedAt:   requiredTimestamptz(membership.CreatedAt),
		UpdatedAt:   requiredTimestamptz(membership.UpdatedAt),
	})
	if err != nil {
		return mapPostgresError("create tenant membership", err, biz.ErrMembershipConflict)
	}
	return nil
}

func (r membershipRepository) Get(ctx context.Context, scope biz.TenantScope, membershipID uuid.UUID) (biz.TenantMembership, error) {
	tenantID, err := tenantIDForScope(r.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, err
	}
	row, err := r.queries.GetTenantMembership(ctx, sqlcgen.GetTenantMembershipParams{
		TenantID: tenantID,
		ID:       membershipID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrMembershipNotFound
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("get tenant membership", err, nil)
	}
	return membershipFromGetRow(row)
}

func (r membershipRepository) UpdateStatus(
	ctx context.Context,
	scope biz.TenantScope,
	membershipID uuid.UUID,
	status biz.MembershipStatus,
	expectedVersion int64,
	updatedAt time.Time,
) (biz.TenantMembership, error) {
	tenantID, err := tenantIDForScope(r.tenantID, scope)
	if err != nil {
		return biz.TenantMembership{}, err
	}
	row, err := r.queries.UpdateTenantMembershipStatus(ctx, sqlcgen.UpdateTenantMembershipStatusParams{
		Status:          string(status),
		UpdatedAt:       requiredTimestamptz(updatedAt),
		TenantID:        tenantID,
		ID:              membershipID,
		ExpectedVersion: expectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.TenantMembership{}, biz.ErrVersionConflict
	}
	if err != nil {
		return biz.TenantMembership{}, mapPostgresError("update tenant membership status", err, nil)
	}
	return membershipFromUpdateRow(row)
}

type securityAuditRepository struct {
	queries  *sqlcgen.Queries
	tenantID uuid.UUID
}

func (r securityAuditRepository) Append(ctx context.Context, scope biz.TenantScope, event biz.SecurityAuditEvent) error {
	tenantID, err := tenantIDForScope(r.tenantID, scope)
	if err != nil {
		return err
	}
	err = r.queries.AppendSecurityAuditEvent(ctx, sqlcgen.AppendSecurityAuditEventParams{
		TenantID:             requiredPGUUID(tenantID),
		EventID:              event.ID,
		ActorID:              requiredPGUUID(event.ActorID),
		AuthenticationMethod: string(event.AuthenticationMethod),
		Boundary:             string(event.Boundary),
		Action:               string(event.Action),
		TargetType:           string(event.TargetType),
		TargetID:             event.TargetID,
		TargetVersion:        event.TargetVersion,
		Result:               string(event.Result),
		Reason:               string(event.Reason),
		RequestID:            event.RequestID,
		CorrelationID:        event.CorrelationID,
		DecisionID:           event.DecisionID,
		SourceService:        string(event.SourceService),
		OccurredAt:           requiredTimestamptz(event.OccurredAt),
		RecordedAt:           requiredTimestamptz(event.RecordedAt),
	})
	if err != nil {
		return mapPostgresError("append security audit event", err, biz.ErrAuditConflict)
	}
	return nil
}

func tenantIDForScope(transactionTenantID uuid.UUID, scope biz.TenantScope) (uuid.UUID, error) {
	tenantID, err := scope.TenantID()
	if err != nil {
		return uuid.Nil, err
	}
	if tenantID != transactionTenantID {
		return uuid.Nil, biz.ErrTenantScopeMismatch
	}
	return tenantID, nil
}

func mapPostgresError(operation string, err error, uniqueConflict error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}

	mapped := biz.ErrPersistenceUnavailable
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			switch postgresError.ConstraintName {
			case "tenant_role_bindings_membership_fk", "tenant_role_bindings_role_fk":
				mapped = biz.ErrTenantRelationConflict
			default:
				mapped = biz.ErrInvalidPersistenceState
			}
		case "23505":
			if uniqueConflict != nil {
				mapped = uniqueConflict
			} else {
				mapped = biz.ErrInvalidPersistenceState
			}
		case "23502", "23514":
			mapped = biz.ErrInvalidPersistenceState
		case "40001", "40P01":
			mapped = biz.ErrVersionConflict
		case "42501":
			mapped = biz.ErrPersistencePermissionDenied
		}
	}
	return fmt.Errorf("%s: %w", operation, mapped)
}

func requiredTimestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func membershipFromGetRow(row sqlcgen.GetTenantMembershipRow) (biz.TenantMembership, error) {
	return newTenantMembership(row.ID, row.PrincipalID, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
}

func membershipFromUpdateRow(row sqlcgen.UpdateTenantMembershipStatusRow) (biz.TenantMembership, error) {
	return newTenantMembership(row.ID, row.PrincipalID, row.Status, row.Version, row.CreatedAt, row.UpdatedAt)
}

func newTenantMembership(
	id uuid.UUID,
	principalID uuid.UUID,
	status string,
	version int64,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) (biz.TenantMembership, error) {
	if !createdAt.Valid || !updatedAt.Valid {
		return biz.TenantMembership{}, biz.ErrInvalidPersistenceState
	}
	return biz.TenantMembership{
		ID:          id,
		PrincipalID: principalID,
		Status:      biz.MembershipStatus(status),
		Version:     version,
		CreatedAt:   createdAt.Time.UTC(),
		UpdatedAt:   updatedAt.Time.UTC(),
	}, nil
}

var (
	_ biz.TenantUnitOfWork           = (*postgresUnitOfWork)(nil)
	_ biz.TenantMembershipRepository = membershipRepository{}
	_ biz.SecurityAuditRepository    = securityAuditRepository{}
)
