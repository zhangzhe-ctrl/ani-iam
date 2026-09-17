-- name: GetInvitationBootstrapOperation :one
SELECT o.* FROM tenant_bootstrap_operations o
JOIN tenant_invitations i ON i.tenant_id=o.tenant_id AND i.bootstrap_operation_id=o.id
WHERE i.tenant_id=sqlc.arg(tenant_id) AND i.id=sqlc.arg(invitation_id)
FOR UPDATE OF o;

-- name: CompleteInvitationBootstrap :execrows
UPDATE tenant_bootstrap_operations o SET status='succeeded',membership_id=sqlc.arg(membership_id),principal_id=sqlc.arg(principal_id),version=version+1,updated_at=sqlc.arg(now)
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(id) AND o.version=sqlc.arg(expected_version)
AND o.source_kind='core' AND o.status='waiting_for_principal_verification' AND o.superseded_by IS NULL
AND EXISTS(SELECT 1 FROM tenant_invitations i JOIN verified_emails e ON e.principal_id=i.accepted_principal_id
 WHERE i.tenant_id=o.tenant_id AND i.bootstrap_operation_id=o.id AND i.id=sqlc.arg(invitation_id)
 AND i.status='accepted' AND i.accepted_membership_id=sqlc.arg(membership_id) AND i.accepted_principal_id=sqlc.arg(principal_id)
 AND i.normalized_email=o.intended_email AND e.normalized_email=o.intended_email);

-- name: ActivateInvitationBootstrapAccess :execrows
UPDATE tenant_access SET status='active',version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND status='bootstrap_pending' AND version=sqlc.arg(expected_version);
