-- name: GetTenantAdminRecovery :one
SELECT * FROM tenant_admin_recovery_operations WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: CreateTenantAdminRecovery :exec
INSERT INTO tenant_admin_recovery_operations(tenant_id,id,target_principal_id,requester_principal_id,reason_code,payload_fingerprint,status,version,created_at,updated_at,expires_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(target_principal_id),sqlc.arg(requester_principal_id),sqlc.arg(reason_code),sqlc.arg(payload_fingerprint),'pending_approval',1,sqlc.arg(now),sqlc.arg(now),sqlc.arg(expires_at));

-- name: ApproveTenantAdminRecovery :execrows
UPDATE tenant_admin_recovery_operations SET status='approved',approver_principal_id=sqlc.arg(approver_principal_id),
 approval_reference=sqlc.arg(approval_reference),approved_at=sqlc.arg(now),updated_at=sqlc.arg(now),expires_at=sqlc.arg(expires_at),version=version+1
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='pending_approval' AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp() AND requester_principal_id<>sqlc.arg(approver_principal_id);

-- name: ExecuteTenantAdminRecovery :execrows
UPDATE tenant_admin_recovery_operations SET status='executed',executed_at=sqlc.arg(now),updated_at=sqlc.arg(now),membership_id=sqlc.arg(membership_id),version=version+1
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='approved' AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp() AND approval_reference=sqlc.arg(approval_reference);

-- name: TenantAdminRecoveryLoginTarget :one
SELECT p.id, EXISTS (
 SELECT 1 FROM verified_emails e JOIN identities i ON i.principal_id=e.principal_id
 WHERE e.principal_id=p.id AND i.status='active'
 AND ((sqlc.arg(oidc_provider)::text<>'' AND sqlc.arg(oidc_issuer)::text<>''
       AND i.provider=sqlc.arg(oidc_provider) AND i.issuer=sqlc.arg(oidc_issuer))
 OR (sqlc.arg(password_enabled)::boolean AND i.provider='password'
     AND EXISTS (SELECT 1 FROM password_credentials c WHERE c.principal_id=p.id AND c.identity_id=i.id
       AND (c.locked_until IS NULL OR c.locked_until<=sqlc.arg(now)))))
) AS login_capable
FROM principals p WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='human' AND p.status='active' FOR UPDATE OF p;

-- name: TenantAdminRecoveryMembership :one
SELECT * FROM tenant_memberships WHERE tenant_id=sqlc.arg(tenant_id) AND principal_id=sqlc.arg(principal_id) AND status<>'removed' FOR UPDATE;

-- name: TenantAdminRecoveryRole :one
SELECT * FROM tenant_roles WHERE tenant_id=sqlc.arg(tenant_id) AND code='tenant-admin' AND system_role FOR SHARE;

-- name: ReserveRestoreRecoveryApproval :exec
INSERT INTO iam_recovery_approval_references(approval_reference,tenant_id,restore_operation_id,created_at)
VALUES(sqlc.arg(approval_reference),sqlc.arg(tenant_id),sqlc.arg(operation_id),sqlc.arg(now));
