-- The global operation identity is used only by the separately authorized
-- Platform entry. All subsequent locks and relations carry its stored Tenant.
-- name: LookupPlatformCoreBootstrapTenant :one
SELECT tenant_id FROM tenant_bootstrap_operations WHERE id=sqlc.arg(operation_id) AND source_kind='core';

-- name: ResumeReissuedCoreBootstrap :execrows
UPDATE tenant_bootstrap_operations o SET status='waiting_for_principal_verification',version=version+1,updated_at=sqlc.arg(now)
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(operation_id) AND o.source_kind='core'
AND o.status IN ('waiting_for_principal_verification','attention_required') AND o.version=sqlc.arg(expected_version) AND o.superseded_by IS NULL
AND EXISTS(SELECT 1 FROM tenant_access a WHERE a.tenant_id=o.tenant_id AND a.status='bootstrap_pending')
AND EXISTS(SELECT 1 FROM tenant_invitations i WHERE i.tenant_id=o.tenant_id AND i.id=sqlc.arg(invitation_id) AND i.bootstrap_operation_id=o.id
 AND i.normalized_email=o.intended_email AND i.status='pending' AND i.delivery_generation=sqlc.arg(generation) AND i.expires_at>clock_timestamp());
