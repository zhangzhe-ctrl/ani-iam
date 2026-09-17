-- name: GetPlatformInvitation :one
SELECT * FROM platform_invitations WHERE id=sqlc.arg(id);

-- name: FindPendingPlatformInvitation :one
SELECT * FROM platform_invitations WHERE normalized_email=sqlc.arg(normalized_email) AND status='pending';

-- name: ListPlatformInvitations :many
SELECT * FROM platform_invitations
WHERE (sqlc.arg(status)::text='' OR
  CASE WHEN status='pending' AND expires_at<=sqlc.arg(now) THEN 'expired' ELSE status END=sqlc.arg(status))
AND (sqlc.arg(cursor_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR id>sqlc.arg(cursor_id))
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: CreatePlatformInvitation :exec
INSERT INTO platform_invitations(id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(normalized_email),sqlc.arg(role_ids),sqlc.arg(locale),'pending',sqlc.arg(token_digest),1,sqlc.arg(expires_at),1,sqlc.arg(created_by),sqlc.arg(created_at),sqlc.arg(created_at));

-- name: AddPlatformInvitationRole :exec
INSERT INTO platform_invitation_roles(invitation_id,role_id) VALUES(sqlc.arg(invitation_id),sqlc.arg(role_id));

-- name: ClearPlatformInvitationRoles :exec
DELETE FROM platform_invitation_roles WHERE invitation_id=sqlc.arg(invitation_id);

-- name: TransitionPlatformInvitationTerminal :execrows
UPDATE platform_invitations SET status=sqlc.arg(status),version=version+1,updated_at=sqlc.arg(updated_at)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='pending';

-- name: ResendPlatformInvitation :execrows
UPDATE platform_invitations SET status='pending',token_digest=sqlc.arg(token_digest),delivery_generation=delivery_generation+1,
    expires_at=sqlc.arg(expires_at),version=version+1,updated_at=sqlc.arg(updated_at)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status IN ('pending','expired');

-- name: CreatePlatformInvitationDelivery :exec
INSERT INTO platform_invitation_outbox(id,invitation_id,delivery_generation,payload_key_version,payload_ciphertext,status,available_at,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(invitation_id),sqlc.arg(delivery_generation),sqlc.arg(payload_key_version),sqlc.arg(payload_ciphertext),'pending',sqlc.arg(created_at),1,sqlc.arg(created_at),sqlc.arg(created_at));

-- name: CancelPlatformInvitationDeliveries :exec
UPDATE platform_invitation_outbox SET status='cancelled',payload_key_version=NULL,payload_ciphertext=NULL,
    claimed_at=NULL,delivered_at=NULL,notification_id=NULL,version=version+1,updated_at=sqlc.arg(updated_at)
WHERE invitation_id=sqlc.arg(invitation_id) AND status IN ('pending','claimed','attention_required');

-- name: GetPlatformInvitationDelivery :one
SELECT status,attempt_count FROM platform_invitation_outbox
WHERE invitation_id=sqlc.arg(invitation_id) AND delivery_generation=sqlc.arg(delivery_generation);

-- name: ListExpiredPlatformInvitationsForRole :many
SELECT i.* FROM platform_invitations i JOIN platform_invitation_roles r ON r.invitation_id=i.id
WHERE r.role_id=sqlc.arg(role_id) AND i.status='pending' AND i.expires_at<=sqlc.arg(now)
ORDER BY i.id;
