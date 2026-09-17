-- name: GetTenantInvitation :one
SELECT * FROM tenant_invitations WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id);

-- name: FindPendingTenantInvitation :one
SELECT * FROM tenant_invitations WHERE tenant_id=sqlc.arg(tenant_id) AND normalized_email=sqlc.arg(normalized_email) AND status='pending';

-- name: ListTenantInvitations :many
SELECT * FROM tenant_invitations
WHERE tenant_id=sqlc.arg(tenant_id) AND (sqlc.arg(status)::text='' OR
  CASE WHEN status='pending' AND expires_at<=sqlc.arg(now) THEN 'expired' ELSE status END=sqlc.arg(status))
AND (sqlc.arg(cursor_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR id>sqlc.arg(cursor_id))
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: CreateTenantInvitation :exec
INSERT INTO tenant_invitations(tenant_id,id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(normalized_email),sqlc.arg(role_ids),sqlc.arg(locale),'pending',sqlc.arg(token_digest),1,sqlc.arg(expires_at),1,sqlc.arg(created_by),sqlc.arg(created_at),sqlc.arg(created_at));

-- name: AddTenantInvitationRole :exec
INSERT INTO tenant_invitation_roles(tenant_id,invitation_id,role_id) VALUES(sqlc.arg(tenant_id),sqlc.arg(invitation_id),sqlc.arg(role_id));

-- name: ClearTenantInvitationRoles :exec
DELETE FROM tenant_invitation_roles WHERE tenant_id=sqlc.arg(tenant_id) AND invitation_id=sqlc.arg(invitation_id);

-- name: TransitionTenantInvitationTerminal :execrows
UPDATE tenant_invitations SET status=sqlc.arg(status),version=version+1,updated_at=sqlc.arg(updated_at)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='pending';

-- name: ResendTenantInvitation :execrows
UPDATE tenant_invitations SET status='pending',token_digest=sqlc.arg(token_digest),delivery_generation=delivery_generation+1,
    expires_at=sqlc.arg(expires_at),version=version+1,updated_at=sqlc.arg(updated_at)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status IN ('pending','expired');

-- name: CreateTenantInvitationDelivery :exec
INSERT INTO tenant_invitation_outbox(tenant_id,id,invitation_id,delivery_generation,payload_key_version,payload_ciphertext,status,available_at,version,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(invitation_id),sqlc.arg(delivery_generation),sqlc.arg(payload_key_version),sqlc.arg(payload_ciphertext),'pending',sqlc.arg(created_at),1,sqlc.arg(created_at),sqlc.arg(created_at));

-- name: CancelTenantInvitationDeliveries :exec
UPDATE tenant_invitation_outbox SET status='cancelled',payload_key_version=NULL,payload_ciphertext=NULL,
    claimed_at=NULL,delivered_at=NULL,notification_id=NULL,version=version+1,updated_at=sqlc.arg(updated_at)
WHERE tenant_id=sqlc.arg(tenant_id) AND invitation_id=sqlc.arg(invitation_id) AND status IN ('pending','claimed','attention_required');

-- name: GetTenantInvitationDelivery :one
SELECT status,attempt_count FROM tenant_invitation_outbox
WHERE tenant_id=sqlc.arg(tenant_id) AND invitation_id=sqlc.arg(invitation_id) AND delivery_generation=sqlc.arg(delivery_generation);

-- name: ListExpiredTenantInvitationsForRole :many
SELECT i.* FROM tenant_invitations i JOIN tenant_invitation_roles r ON r.tenant_id=i.tenant_id AND r.invitation_id=i.id
WHERE i.tenant_id=sqlc.arg(tenant_id) AND r.role_id=sqlc.arg(role_id) AND i.status='pending' AND i.expires_at<=sqlc.arg(now)
ORDER BY i.id;
