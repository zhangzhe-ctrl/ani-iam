-- name: GetTenantBootstrapRecovery :one
SELECT * FROM tenant_bootstrap_recovery_operations WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: CreateTenantBootstrapRecovery :exec
INSERT INTO tenant_bootstrap_recovery_operations(tenant_id,id,target_principal_id,requester_principal_id,reason_code,payload_fingerprint,status,version,created_at,updated_at,expires_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(target_principal_id),sqlc.arg(requester_principal_id),sqlc.arg(reason_code),sqlc.arg(payload_fingerprint),'pending_approval',1,sqlc.arg(now),sqlc.arg(now),sqlc.arg(expires_at));

-- name: ApproveTenantBootstrapRecovery :execrows
UPDATE tenant_bootstrap_recovery_operations SET status='approved',approver_principal_id=sqlc.arg(approver_principal_id),
 approval_reference=sqlc.arg(approval_reference),approved_at=sqlc.arg(now),updated_at=sqlc.arg(now),expires_at=sqlc.arg(expires_at),version=version+1
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='pending_approval' AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp() AND requester_principal_id<>sqlc.arg(approver_principal_id);

-- name: ExecuteTenantBootstrapRecovery :execrows
UPDATE tenant_bootstrap_recovery_operations SET status='executed',executed_at=sqlc.arg(now),updated_at=sqlc.arg(now),membership_id=sqlc.arg(membership_id),version=version+1
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='approved' AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp() AND approval_reference=sqlc.arg(approval_reference);

-- name: ReserveBootstrapRecoveryApproval :exec
INSERT INTO iam_recovery_approval_references(approval_reference,tenant_id,bootstrap_operation_id,created_at)
VALUES(sqlc.arg(approval_reference),sqlc.arg(tenant_id),sqlc.arg(operation_id),sqlc.arg(now));

-- name: BootstrapRecoveryVerifiedEmail :one
SELECT normalized_email FROM verified_emails WHERE principal_id=sqlc.arg(principal_id);

-- name: GetCurrentTenantBootstrap :one
SELECT * FROM tenant_bootstrap_operations WHERE tenant_id=sqlc.arg(tenant_id) AND superseded_by IS NULL FOR UPDATE;

-- name: CreateBootstrapPendingAccess :exec
INSERT INTO tenant_access(tenant_id,status,version,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),'bootstrap_pending',1,sqlc.arg(now),sqlc.arg(now)) ON CONFLICT(tenant_id) DO NOTHING;

-- name: CreateBootstrapAdministratorRole :exec
INSERT INTO tenant_roles(tenant_id,id,code,display_name,system_role,system_definition_version,version,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),'tenant-admin','Tenant administrator',true,1,1,sqlc.arg(now),sqlc.arg(now));

-- name: ActivateRecoveryBootstrapAccess :one
UPDATE tenant_access SET status='active',version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND status='bootstrap_pending'
RETURNING status,version,created_at,updated_at;

-- name: SupersedeTenantBootstrap :execrows
UPDATE tenant_bootstrap_operations SET status='superseded',superseded_by=sqlc.arg(recovery_id),version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
AND status IN ('pending','waiting_for_principal_verification','attention_required') AND superseded_by IS NULL;

-- name: CancelSupersededBootstrapInvitations :many
UPDATE tenant_invitations SET status='cancelled',version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND bootstrap_operation_id=sqlc.arg(operation_id) AND status IN ('pending','expired')
RETURNING id,version;

-- name: CreateRecoveredTenantBootstrap :exec
INSERT INTO tenant_bootstrap_operations(tenant_id,id,source_kind,intended_email,intended_principal_id,payload_fingerprint,payload,status,version,
 recovery_request_id,supersedes,membership_id,principal_id,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),'recovery',sqlc.arg(intended_email),sqlc.arg(principal_id),sqlc.arg(payload_fingerprint),sqlc.arg(payload),'succeeded',1,
 sqlc.arg(id),sqlc.narg(supersedes),sqlc.arg(membership_id),sqlc.arg(principal_id),sqlc.arg(now),sqlc.arg(now));

-- name: RecoveryCancelledBootstrapInvitation :one
SELECT EXISTS(SELECT 1 FROM tenant_invitations i JOIN tenant_bootstrap_operations o ON o.tenant_id=i.tenant_id AND o.id=i.bootstrap_operation_id
WHERE i.tenant_id=sqlc.arg(tenant_id) AND i.id=sqlc.arg(invitation_id) AND i.status='cancelled' AND i.version=sqlc.arg(version)
AND o.superseded_by=sqlc.arg(recovery_id)) AS valid;
