-- Internal queue discovery exposes only Tenant IDs. Every claim/read/write
-- after discovery carries the exact Tenant boundary.
-- name: DiscoverIdentityNotificationTenants :many
SELECT tenant_id FROM tenant_invitation_outbox
WHERE (status='pending' AND available_at<=sqlc.arg(now)) OR (status='claimed' AND claimed_at<=sqlc.arg(lease_expired_before))
GROUP BY tenant_id ORDER BY min(available_at),tenant_id LIMIT 32;

-- name: ClaimTenantInvitationNotification :one
WITH candidate AS (
 SELECT o.tenant_id,o.id FROM tenant_invitation_outbox o WHERE o.tenant_id=sqlc.arg(tenant_id) AND 
 ((o.status='pending' AND o.available_at<=sqlc.arg(now)) OR (o.status='claimed' AND o.claimed_at<=sqlc.arg(lease_expired_before)))
 ORDER BY o.available_at,o.id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE tenant_invitation_outbox o SET status='claimed',attempt_count=o.attempt_count+1,version=o.version+1,claimed_at=sqlc.arg(now),updated_at=sqlc.arg(now)
FROM candidate c WHERE o.tenant_id=c.tenant_id AND o.id=c.id RETURNING o.*;

-- name: CurrentTenantInvitationNotification :one
SELECT EXISTS(SELECT 1 FROM tenant_invitation_outbox o JOIN tenant_invitations p ON p.tenant_id=o.tenant_id AND p.id=o.invitation_id
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(id) AND o.invitation_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed'
AND o.delivery_generation=p.delivery_generation AND o.delivery_generation=sqlc.arg(generation) AND p.status='pending' AND p.expires_at>sqlc.arg(now) AND p.expires_at>statement_timestamp()) AS current;

-- name: FinishTenantInvitationNotification :execrows
UPDATE tenant_invitation_outbox o SET status=sqlc.arg(outcome),version=o.version+1,updated_at=sqlc.arg(now),available_at=sqlc.arg(next_attempt),
 claimed_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN o.claimed_at ELSE NULL END,
 delivered_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(now)::timestamptz ELSE NULL END,
 notification_id=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(notification_id)::text ELSE NULL END,
 payload_key_version=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_key_version END,
 payload_ciphertext=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_ciphertext END
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(id) AND o.invitation_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed';

-- name: ClaimPlatformInvitationNotification :one
WITH candidate AS (
 SELECT o.id FROM platform_invitation_outbox o WHERE 
 ((o.status='pending' AND o.available_at<=sqlc.arg(now)) OR (o.status='claimed' AND o.claimed_at<=sqlc.arg(lease_expired_before)))
 ORDER BY o.available_at,o.id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE platform_invitation_outbox o SET status='claimed',attempt_count=o.attempt_count+1,version=o.version+1,claimed_at=sqlc.arg(now),updated_at=sqlc.arg(now)
FROM candidate c WHERE o.id=c.id RETURNING o.*;

-- name: CurrentPlatformInvitationNotification :one
SELECT EXISTS(SELECT 1 FROM platform_invitation_outbox o JOIN platform_invitations p ON p.id=o.invitation_id
WHERE o.id=sqlc.arg(id) AND o.invitation_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed'
AND o.delivery_generation=p.delivery_generation AND o.delivery_generation=sqlc.arg(generation) AND p.status='pending' AND p.expires_at>sqlc.arg(now) AND p.expires_at>statement_timestamp()) AS current;

-- name: FinishPlatformInvitationNotification :execrows
UPDATE platform_invitation_outbox o SET status=sqlc.arg(outcome),version=o.version+1,updated_at=sqlc.arg(now),available_at=sqlc.arg(next_attempt),
 claimed_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN o.claimed_at ELSE NULL END,
 delivered_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(now)::timestamptz ELSE NULL END,
 notification_id=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(notification_id)::text ELSE NULL END,
 payload_key_version=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_key_version END,
 payload_ciphertext=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_ciphertext END
WHERE o.id=sqlc.arg(id) AND o.invitation_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed';

-- name: ClaimAccountVerificationNotification :one
WITH candidate AS (
 SELECT o.id FROM iam_invited_account_outbox o WHERE 
 ((o.status='pending' AND o.available_at<=sqlc.arg(now)) OR (o.status='claimed' AND o.claimed_at<=sqlc.arg(lease_expired_before)))
 ORDER BY o.available_at,o.id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE iam_invited_account_outbox o SET status='claimed',attempt_count=o.attempt_count+1,version=o.version+1,claimed_at=sqlc.arg(now),updated_at=sqlc.arg(now)
FROM candidate c WHERE o.id=c.id RETURNING o.*;

-- name: CurrentAccountVerificationNotification :one
SELECT EXISTS(SELECT 1 FROM iam_invited_account_outbox o JOIN iam_invited_account_verifications p ON p.id=o.challenge_id
WHERE o.id=sqlc.arg(id) AND o.challenge_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed'
AND p.failed_attempts<5 AND p.status='pending' AND p.expires_at>sqlc.arg(now) AND p.expires_at>statement_timestamp()) AS current;

-- name: FinishAccountVerificationNotification :execrows
UPDATE iam_invited_account_outbox o SET status=sqlc.arg(outcome),version=o.version+1,updated_at=sqlc.arg(now),available_at=sqlc.arg(next_attempt),
 claimed_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN o.claimed_at ELSE NULL END,
 delivered_at=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(now)::timestamptz ELSE NULL END,
 notification_id=CASE WHEN sqlc.arg(outcome)::text='delivered' THEN sqlc.arg(notification_id)::text ELSE NULL END,
 payload_key_version=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_key_version END,
 payload_ciphertext=CASE WHEN sqlc.arg(outcome)::text IN ('delivered','cancelled') THEN NULL ELSE o.payload_ciphertext END
WHERE o.id=sqlc.arg(id) AND o.challenge_id=sqlc.arg(source_id) AND o.version=sqlc.arg(expected_version) AND o.status='claimed';

