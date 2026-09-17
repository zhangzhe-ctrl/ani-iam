package data

import (
	"bytes"
	"context"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

// ValidateWorkloadRegistry keeps configuration mistakes closed at startup.
// The Grant query separately checks the current target digest on every call.
func ValidateWorkloadRegistry(ctx context.Context, d *Data, r *workloadregistry.Registry) error {
	if d == nil || d.pool == nil || r == nil {
		return biz.ErrPersistenceUnavailable
	}
	return validateWorkloadRegistry(ctx, sqlcgen.New(d.pool), r)
}

func validateWorkloadRegistry(ctx context.Context, q *sqlcgen.Queries, r *workloadregistry.Registry) error {
	if r == nil {
		return biz.ErrWorkloadBootstrapInvalid
	}
	rows, err := q.ListWorkloadTargetRegistrations(ctx)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if len(rows) != len(r.Targets()) {
		return biz.ErrWorkloadBootstrapConflict
	}
	for _, row := range rows {
		t, ok := r.Lookup(row.Audience, row.Operation)
		if !ok || t.GrantScope != row.Scope || t.Enabled != row.Enabled || hex.EncodeToString(row.TargetSha256) != r.Revision(row.Audience, row.Operation) || hex.EncodeToString(row.BundleSha256) != r.Digest() {
			return biz.ErrWorkloadBootstrapConflict
		}
	}
	return nil
}

// InstallWorkloadRegistry is an offline deployment-owner operation. It requires
// the existing restricted migrator, an exact predecessor, and one local commit.
// It never creates a Principal, Credential, Membership, Grant, Role or Binding.
func InstallWorkloadRegistry(ctx context.Context, d *Data, r *workloadregistry.Registry, previous string) error {
	if d == nil || d.pool == nil || r == nil {
		return biz.ErrWorkloadBootstrapInvalid
	}
	if previous != "empty" {
		v, e := hex.DecodeString(previous)
		if e != nil || len(v) != 32 || hex.EncodeToString(v) != previous {
			return biz.ErrWorkloadBootstrapInvalid
		}
	}
	if _, _, err := workloadOperationPolicies(r); err != nil {
		return err
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer tx.Rollback(ctx)
	var owner bool
	err = tx.QueryRow(ctx, `SELECT current_user='ani_iam_migrator'
        AND NOT (rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls OR rolreplication)
        AND NOT EXISTS (SELECT 1 FROM pg_auth_members WHERE member=r.oid)
        AND (SELECT relowner=r.oid FROM pg_class WHERE oid='workload_target_registrations'::regclass)
        FROM pg_roles r WHERE rolname=current_user`).Scan(&owner)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if !owner {
		return biz.ErrWorkloadBootstrapDenied
	}
	q := sqlcgen.New(tx)
	if q.LockWorkloadRegistryInstallation(ctx) != nil {
		return biz.ErrPersistenceUnavailable
	}
	rows, err := q.ListWorkloadTargetRegistrations(ctx)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	current := "empty"
	for _, row := range rows {
		digest := hex.EncodeToString(row.BundleSha256)
		if current != "empty" && current != digest {
			return biz.ErrWorkloadBootstrapConflict
		}
		current = digest
		t, ok := r.Lookup(row.Audience, row.Operation)
		// A target with grants must be explicitly retained (possibly disabled).
		// Omission or repurposing a scope cannot silently change granted meaning.
		if !ok || t.GrantScope != row.Scope {
			return biz.ErrWorkloadBootstrapConflict
		}
	}
	if current == r.Digest() {
		if err := validateWorkloadRegistry(ctx, q, r); err != nil {
			return err
		}
		if tx.Commit(ctx) != nil {
			return biz.ErrPersistenceUnavailable
		}
		return nil
	}
	if current != previous {
		return biz.ErrWorkloadBootstrapConflict
	}
	bundle, _ := hex.DecodeString(r.Digest())
	for _, t := range r.Targets() {
		revision, _ := hex.DecodeString(r.Revision(t.Audience, t.Operation))
		if q.UpsertWorkloadTargetRegistration(ctx, sqlcgen.UpsertWorkloadTargetRegistrationParams{Audience: t.Audience, Operation: t.Operation, Scope: t.GrantScope, TargetSha256: revision, BundleSha256: bundle, Enabled: t.Enabled}) != nil {
			return biz.ErrPersistenceUnavailable
		}
		for _, s := range t.Sources {
			if q.InsertWorkloadRegistryPermission(ctx, sqlcgen.InsertWorkloadRegistryPermissionParams{Resource: s.Resource, Action: s.Action}) != nil {
				return biz.ErrPersistenceUnavailable
			}
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	var predecessor []byte
	if previous != "empty" {
		predecessor, _ = hex.DecodeString(previous)
	}
	if q.InsertWorkloadRegistryInstallation(ctx, sqlcgen.InsertWorkloadRegistryInstallationParams{ID: id, PreviousSha256: bytes.Clone(predecessor), InstalledSha256: bundle, InstalledAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}) != nil {
		return biz.ErrPersistenceUnavailable
	}
	if tx.Commit(ctx) != nil {
		return biz.ErrPersistenceUnavailable
	}
	return nil
}
