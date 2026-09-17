package data

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type workloadIdentityReader struct{ data *Data }
type workloadGrantReader struct {
	data     *Data
	registry *workloadregistry.Registry
}

func NewWorkloadIdentityReader(data *Data) biz.WorkloadIdentityReader {
	return &workloadIdentityReader{data: data}
}
func NewWorkloadGrantReader(data *Data, registries ...*workloadregistry.Registry) biz.WorkloadGrantReader {
	var registry *workloadregistry.Registry
	if len(registries) == 1 {
		registry = registries[0]
	}
	return &workloadGrantReader{data: data, registry: registry}
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
	registration, ok := r.registry.Lookup(target.Audience, target.Operation)
	if !ok || !registration.Enabled {
		return 0, biz.ErrWorkloadPermissionDenied
	}
	revision, _ := hex.DecodeString(r.registry.Revision(target.Audience, target.Operation))
	version, err := sqlcgen.New(r.data.pool).CheckWorkloadGrant(ctx, sqlcgen.CheckWorkloadGrantParams{
		PrincipalID: identity.PrincipalID, BindingID: identity.BindingID,
		PrincipalVersion: identity.PrincipalVersion, BindingVersion: identity.BindingVersion,
		Environment: identity.Peer.Environment, TrustDomain: identity.Peer.TrustDomain,
		Audience: target.Audience, Operation: target.Operation, TargetSha256: revision,
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
 OR has_table_privilege(current_user,'workload_target_registrations','INSERT,UPDATE,DELETE,TRUNCATE')
 OR has_table_privilege(current_user,'workload_registry_installations','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'workload_bootstrap_receipts', 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'first_administrator_intents', 'INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'first_administrator_completions', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'platform_mutation_results', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_audit_events', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_schema_revision', 'INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'tenant_admin_recovery_operations', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'tenant_bootstrap_recovery_operations', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'tenant_bootstrap_operations', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_bootstrap_receipts', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_integration_receipts', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_bootstrap_worker_results', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_lifecycle_generations', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_lifecycle_rebuild_pages', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_lifecycle_rebuilds', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_bootstrap_jobs', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_bootstrap_attempts', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_lifecycle_pipelines', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'core_lifecycle_projection_rows', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'tenant_lifecycle_projections','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'current_tenant_lifecycle','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_current_lifecycle_facts','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_bindings','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_routes','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_grants','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_authority_receipts','UPDATE,DELETE,TRUNCATE')
 OR has_table_privilege(current_user,'tenant_integration_receipts','UPDATE,DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_lifecycle_generations','UPDATE,DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_broker_authority_receipts','UPDATE,DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_bootstrap_broker_approvals','UPDATE,DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_snapshot_pages','UPDATE,DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_lifecycle_pipelines','DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_lifecycle_facts','DELETE,TRUNCATE')
OR has_table_privilege(current_user,'tenant_snapshot_rebuilds','DELETE,TRUNCATE')
 OR has_table_privilege(current_user,'tenant_current_lifecycle_facts','INSERT,UPDATE,DELETE,TRUNCATE')
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['producer','snapshot_id','generation_id','base_generation_id','epoch','reader_id','reader_binding_id','watermark','total_count','page_size','captured_at','expires_at','first_token','authority_sha256','created_at']) AS immutable_column(name) WHERE has_column_privilege(current_user,'tenant_snapshot_rebuilds',immutable_column.name,'UPDATE'))
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['state','next_token','last_tenant_id','loaded_items','applied_through','abandoned_reason','updated_at']) AS progress_column(name) WHERE NOT has_column_privilege(current_user,'tenant_snapshot_rebuilds',progress_column.name,'UPDATE'))
        OR has_table_privilege(current_user,'core_broker_dlq','UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_dlq_context','UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_dlq_attempts','UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user,'core_broker_administration_receipts','INSERT,UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_recovery_approval_references', 'UPDATE,DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'iam_invited_account_verifications', 'DELETE,TRUNCATE')
 OR has_table_privilege(current_user, 'iam_invited_account_outbox', 'DELETE,TRUNCATE')
 OR has_table_privilege(current_user, 'platform_invitations', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'platform_invitation_outbox', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'tenant_invitations', 'DELETE,TRUNCATE')
        OR has_table_privilege(current_user, 'tenant_invitation_outbox', 'DELETE,TRUNCATE')
        OR has_parameter_privilege(current_user, 'session_replication_role', 'SET')
        OR EXISTS (SELECT 1 FROM pg_auth_members WHERE member=r.oid)
        OR NOT has_schema_privilege(current_user, 'public', 'USAGE')
        OR NOT has_table_privilege(current_user, 'verified_emails', 'INSERT')
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['tenant_current_lifecycle_facts','verified_emails','core_current_lifecycle_facts','current_tenant_lifecycle','tenant_role_permissions',
                'workload_identity_bindings','workload_grants','iam_schema_revision','first_administrator_intents',
                'platform_role_permissions','platform_administrator_guard','core_broker_bindings','core_broker_routes','core_broker_grants','core_broker_administration_receipts']) AS required_table(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, 'SELECT'))
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['principals','tenant_access','tenant_memberships','tenant_roles',
                'tenant_role_bindings','identities','password_credentials','sessions','session_grants',
                'refresh_token_families','refresh_tokens','password_action_requests','password_actions',
                'notification_outbox','password_action_completions','workload_principals','api_keys',
                'platform_memberships','platform_roles','platform_role_bindings','platform_session_grants',
                'platform_refresh_token_families','platform_refresh_tokens','tenant_invitations','tenant_invitation_outbox','platform_invitations','platform_invitation_outbox','tenant_admin_recovery_operations','tenant_bootstrap_recovery_operations','tenant_bootstrap_operations','iam_invited_account_verifications','iam_invited_account_outbox','core_lifecycle_pipelines','core_lifecycle_projection_rows','core_lifecycle_rebuilds','core_bootstrap_jobs','tenant_lifecycle_pipelines','tenant_lifecycle_facts']) AS required_table(name)
            CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE']) AS required_privilege(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, required_privilege.name))
        OR EXISTS (
            SELECT 1 FROM unnest(ARRAY['iam_audit_events','tenant_mutation_results','first_administrator_completions','platform_mutation_results','iam_recovery_approval_references','core_bootstrap_receipts','core_integration_receipts','core_lifecycle_generations','core_bootstrap_worker_results','core_lifecycle_rebuild_pages','core_bootstrap_attempts','core_broker_authority_receipts','core_broker_dlq','core_broker_dlq_context','core_broker_dlq_attempts','core_bootstrap_broker_approvals','tenant_integration_receipts','tenant_lifecycle_generations','tenant_broker_authority_receipts','tenant_bootstrap_broker_approvals','tenant_snapshot_pages','tenant_snapshot_rebuilds']) AS required_table(name)
            CROSS JOIN unnest(ARRAY['SELECT','INSERT']) AS required_privilege(name)
            WHERE NOT has_table_privilege(current_user, required_table.name, required_privilege.name))
        OR NOT has_table_privilege(current_user, 'tenant_role_bindings', 'DELETE')
        OR NOT has_table_privilege(current_user, 'platform_invitation_roles', 'SELECT')
        OR NOT has_table_privilege(current_user, 'platform_invitation_roles', 'INSERT')
        OR NOT has_table_privilege(current_user, 'platform_invitation_roles', 'DELETE')
        OR NOT has_table_privilege(current_user, 'tenant_invitation_roles', 'SELECT')
        OR NOT has_table_privilege(current_user, 'tenant_invitation_roles', 'INSERT')
        OR NOT has_table_privilege(current_user, 'tenant_invitation_roles', 'DELETE'),
        (SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
          JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND t.tgenabled='O'
          AND t.tgname IN ('principal_identity_write_guard','workload_profile_write_guard',
              'principals_workload_relations','workload_profile_relations','workload_membership_relations',
              'verified_email_human','identity_human','password_credential_human','session_human',
              'api_key_identity_write_guard','membership_principal_lock',
              'bootstrap_principal_guard','bootstrap_profile_guard','bootstrap_audit_guard',
              'platform_membership_human','first_administrator_completion_human','platform_membership_identity',
              'platform_grant_identity','platform_family_identity','platform_token_identity','platform_binding_identity','tenant_invitation_identity','platform_invitation_identity','tenant_admin_recovery_identity','tenant_bootstrap_recovery_identity','tenant_bootstrap_operation_identity','tenant_admin_recovery_approval_reference','tenant_bootstrap_recovery_approval_reference','tenant_invitation_bootstrap_identity','invited_account_verification_identity','invited_account_delivery_identity','core_bootstrap_receipt_identity','core_integration_receipt_identity','core_projection_identity','core_bootstrap_worker_result_identity','core_bootstrap_worker_audit','core_receipt_lifecycle_fields','core_generation_immutable','core_rebuild_page_immutable','core_rebuild_identity','core_bootstrap_job_identity','core_bootstrap_attempt_identity','core_broker_binding_change','core_broker_route_change','core_broker_grant_change','core_broker_principal_change','core_broker_profile_change','core_broker_binding_identity','core_broker_route_identity','core_broker_grant_identity','core_broker_receipt_identity','core_broker_dlq_identity','core_broker_administration_identity','core_bootstrap_broker_approval_identity','core_rebuild_authority_identity','core_broker_dlq_context_identity','core_broker_dlq_attempt_identity','tenant_lifecycle_generation_immutable','tenant_integration_receipt_immutable','tenant_integration_fact_binding','tenant_broker_route_contract','tenant_bootstrap_epoch','tenant_broker_receipt_identity','tenant_bootstrap_broker_approval_identity','tenant_snapshot_identity_change','tenant_snapshot_grant_change','tenant_snapshot_target_change','tenant_snapshot_page_immutable','tenant_snapshot_cut_immutable','tenant_bootstrap_receipt_epoch')) <> 70
        FROM pg_roles r WHERE rolname=current_user`).Scan(&revision, &role, &privileged, &owns, &unsafePrivileges, &guardsMissing)
	if err != nil {
		return fmt.Errorf("validate runtime schema and role: %w", err)
	}
	if revision != "202609160005" || role != "ani_iam_runtime" || privileged || owns || unsafePrivileges || guardsMissing {
		return errors.New("runtime schema revision, role or guards do not match Tenant integration foundation")
	}
	return nil
}
