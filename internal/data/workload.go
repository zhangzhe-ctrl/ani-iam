package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type workloadIdentityReader struct{ data *Data }
type workloadGrantReader struct{ data *Data }

func NewWorkloadIdentityReader(data *Data) biz.WorkloadIdentityReader {
	return &workloadIdentityReader{data: data}
}
func NewWorkloadGrantReader(data *Data) biz.WorkloadGrantReader {
	return &workloadGrantReader{data: data}
}

func (r *workloadIdentityReader) ResolveWorkloadIdentity(ctx context.Context, peer biz.VerifiedWorkloadPeer) (biz.WorkloadIdentity, error) {
	row, err := sqlcgen.New(r.data.pool).ResolveWorkloadIdentity(ctx, sqlcgen.ResolveWorkloadIdentityParams{
		Environment: peer.Environment, TrustDomain: peer.TrustDomain, IdentityKind: peer.IdentityKind, IdentityValue: peer.IdentityValue,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.WorkloadIdentity{}, biz.ErrWorkloadIdentityInvalid
	}
	if err != nil {
		return biz.WorkloadIdentity{}, mapPostgresError("resolve Workload identity", err, nil)
	}
	return biz.WorkloadIdentity{PrincipalID: row.PrincipalID, BindingID: row.BindingID,
		PrincipalVersion: row.PrincipalVersion, BindingVersion: row.BindingVersion, Peer: peer}, nil
}

func (r *workloadGrantReader) CheckWorkloadGrant(ctx context.Context, identity biz.WorkloadIdentity, target biz.WorkloadTarget) (int64, error) {
	version, err := sqlcgen.New(r.data.pool).CheckWorkloadGrant(ctx, sqlcgen.CheckWorkloadGrantParams{
		PrincipalID: identity.PrincipalID, BindingID: identity.BindingID,
		PrincipalVersion: identity.PrincipalVersion, BindingVersion: identity.BindingVersion,
		Environment: identity.Peer.Environment, TrustDomain: identity.Peer.TrustDomain,
		Audience: target.Audience, Operation: target.Operation,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, biz.ErrWorkloadPermissionDenied
	}
	if err != nil {
		return 0, mapPostgresError("authorize Workload target", err, nil)
	}
	return version, nil
}

// ValidateRuntimeFoundation inspects only schema/role metadata. Business SQL
// uses sqlc; the runtime never migrates or grants itself privileges.
func ValidateRuntimeFoundation(ctx context.Context, data *Data) error {
	var revision, role string
	var privileged, owns, unsafePrivileges, guardsMissing bool
	err := data.pool.QueryRow(ctx, `SELECT (SELECT revision FROM iam_schema_revision WHERE singleton), current_user,
        r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolbypassrls OR r.rolreplication,
        EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
                WHERE n.nspname='public' AND c.relowner=r.oid),
        has_schema_privilege(current_user, 'public', 'CREATE')
        OR has_database_privilege(current_user, current_database(), 'TEMPORARY')
        OR has_table_privilege(current_user, 'workload_identity_bindings', 'INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'workload_grants', 'INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'workload_bootstrap_receipts', 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_audit_events', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_schema_revision', 'INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_parameter_privilege(current_user, 'session_replication_role', 'SET')
        OR EXISTS (SELECT 1 FROM pg_auth_members WHERE member=r.oid)
        OR NOT has_schema_privilege(current_user, 'public', 'USAGE')
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['verified_emails','tenant_lifecycle_projections','tenant_role_permissions',
                'workload_identity_bindings','workload_grants','iam_schema_revision']) AS required_table(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, 'SELECT'))
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['principals','tenant_access','tenant_memberships','tenant_roles',
                'tenant_role_bindings','identities','password_credentials','sessions','session_grants',
                'refresh_token_families','refresh_tokens','password_action_requests','password_actions',
                'notification_outbox','password_action_completions','workload_principals','api_keys']) AS required_table(name)
            CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE']) AS required_privilege(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, required_privilege.name))
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['iam_audit_events','tenant_mutation_results']) AS required_table(name)
            CROSS JOIN unnest(ARRAY['SELECT','INSERT']) AS required_privilege(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, required_privilege.name))
        OR NOT has_table_privilege(current_user, 'tenant_role_bindings', 'DELETE'),
        (SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
          JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND t.tgenabled='O'
          AND t.tgname IN ('principal_identity_write_guard','workload_profile_write_guard',
              'principals_workload_relations','workload_profile_relations','workload_membership_relations',
              'verified_email_human','identity_human','password_credential_human','session_human',
              'api_key_identity_write_guard','membership_principal_lock',
              'bootstrap_principal_guard','bootstrap_profile_guard','bootstrap_audit_guard')) <> 14
        FROM pg_roles r WHERE rolname=current_user`).Scan(&revision, &role, &privileged, &owns, &unsafePrivileges, &guardsMissing)
	if err != nil {
		return fmt.Errorf("validate runtime schema and role: %w", err)
	}
	if revision != "202609100003" || role != "ani_iam_runtime" || privileged || owns || unsafePrivileges || guardsMissing {
		return errors.New("runtime schema revision, role or guards do not match WR-19 foundation")
	}
	return nil
}
