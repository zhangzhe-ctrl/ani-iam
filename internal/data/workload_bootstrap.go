package data

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type workloadBootstrapRepository struct{ data *Data }

func NewWorkloadBootstrapRepository(data *Data) biz.WorkloadBootstrapRepository {
	return &workloadBootstrapRepository{data: data}
}

func (r *workloadBootstrapRepository) Provision(ctx context.Context, intent biz.WorkloadBootstrapIntent) (biz.WorkloadBootstrapReceipt, error) {
	if r == nil || r.data == nil || r.data.pool == nil {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrPersistenceUnavailable
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	// This metadata gate is deliberately independent of caller-supplied names.
	// Only the separately authenticated, restricted provisioner role can enter.
	var permitted bool
	err = tx.QueryRow(ctx, `SELECT current_user='ani_iam_provisioner'
        AND NOT (rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls OR rolreplication)
        AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
        AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relowner=r.oid)
        AND NOT has_schema_privilege(current_user,'public','CREATE')
        AND NOT has_database_privilege(current_user,current_database(),'TEMPORARY')
        AND NOT has_parameter_privilege(current_user,'session_replication_role','SET')
        AND NOT EXISTS(SELECT 1 FROM information_schema.role_table_grants WHERE grantee=current_user AND table_schema='public' AND privilege_type NOT IN ('SELECT','INSERT'))
        AND NOT has_table_privilege(current_user,'password_credentials','SELECT')
        AND NOT has_table_privilege(current_user,'tenant_memberships','INSERT')
        FROM pg_roles r WHERE rolname=current_user`).Scan(&permitted)
	if err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	if !permitted {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrWorkloadBootstrapDenied
	}
	q := sqlcgen.New(tx)
	m := intent.Manifest
	if err = q.LockWorkloadBootstrap(ctx, sqlcgen.LockWorkloadBootstrapParams{Environment: m.Environment}); err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	previous, err := q.GetWorkloadBootstrapReceipt(ctx, sqlcgen.GetWorkloadBootstrapReceiptParams{ManifestID: m.ManifestID})
	if err == nil {
		if !bytes.Equal(previous.IntentSha256, intent.Digest[:]) {
			return biz.WorkloadBootstrapReceipt{}, biz.ErrWorkloadBootstrapConflict
		}
		var receipt biz.WorkloadBootstrapReceipt
		if json.Unmarshal(previous.Receipt, &receipt) != nil {
			return biz.WorkloadBootstrapReceipt{}, biz.ErrPersistenceUnavailable
		}
		return receipt, nil // read-only retry; deferred rollback releases the lock
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	if _, err := q.GetWorkloadBootstrapEnvironmentReceipt(ctx, sqlcgen.GetWorkloadBootstrapEnvironmentReceiptParams{Environment: m.Environment}); err == nil {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrWorkloadBootstrapConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	if !m.ExpiresAt.After(intent.Now) {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrWorkloadBootstrapExpired
	}
	now := requiredTimestamptz(intent.Now)
	receipt := biz.WorkloadBootstrapReceipt{ManifestID: m.ManifestID, IntentSHA256: hex.EncodeToString(intent.Digest[:]), CompletedAt: intent.Now, PrincipalIDs: make([]uuid.UUID, 0, len(m.Workloads))}
	for _, w := range m.Workloads {
		if err = q.InsertBootstrapPrincipal(ctx, sqlcgen.InsertBootstrapPrincipalParams{PrincipalID: w.PrincipalID, Now: now}); err != nil {
			return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
		}
		if err = q.InsertBootstrapProfile(ctx, sqlcgen.InsertBootstrapProfileParams{PrincipalID: w.PrincipalID, Name: w.Name, Environment: pgtype.Text{String: m.Environment, Valid: true}, TrustDomain: pgtype.Text{String: m.TrustDomain, Valid: true}, Now: now}); err != nil {
			return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
		}
		if err = q.InsertBootstrapBinding(ctx, sqlcgen.InsertBootstrapBindingParams{BindingID: w.BindingID, PrincipalID: w.PrincipalID, Environment: m.Environment, TrustDomain: m.TrustDomain, IdentityValue: w.DNSIdentity, Now: now}); err != nil {
			return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
		}
		for _, g := range w.Grants {
			scope := "iam_ingress"
			if g.Audience == "ani-session-gateway" {
				scope = "delegated_session"
			}
			if err = q.InsertBootstrapGrant(ctx, sqlcgen.InsertBootstrapGrantParams{GrantID: g.ID, PrincipalID: w.PrincipalID, Environment: m.Environment, TrustDomain: m.TrustDomain, Audience: g.Audience, Operation: g.Operation, Scope: scope, Now: now}); err != nil {
				return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
			}
		}
		receipt.PrincipalIDs = append(receipt.PrincipalIDs, w.PrincipalID)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrPersistenceUnavailable
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return biz.WorkloadBootstrapReceipt{}, biz.ErrPersistenceUnavailable
	}
	ca, _ := hex.DecodeString(m.CASHA256)
	if err = q.InsertWorkloadBootstrapReceipt(ctx, sqlcgen.InsertWorkloadBootstrapReceiptParams{ManifestID: m.ManifestID, Environment: m.Environment, TrustDomain: m.TrustDomain, CaSha256: ca, IntentSha256: intent.Digest[:], Receipt: encoded, AuditEventID: auditID, Now: now}); err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	if err = q.InsertWorkloadBootstrapAudit(ctx, sqlcgen.InsertWorkloadBootstrapAuditParams{EventID: auditID, ManifestID: m.ManifestID, RequestID: m.ManifestID.String(), IntentDigest: receipt.IntentSHA256, Now: now}); err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return biz.WorkloadBootstrapReceipt{}, bootstrapPersistenceError(err)
	}
	return receipt, nil
}

func bootstrapPersistenceError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return biz.ErrWorkloadBootstrapConflict
		case "42501":
			return biz.ErrWorkloadBootstrapDenied
		}
	}
	// Raw driver details may contain values from a rejected statement. Keep
	// owner-facing errors stable and non-sensitive, including uncertain commit.
	return biz.ErrPersistenceUnavailable
}
