-- name: LockInvitationTenantActor :one
SELECT e.normalized_email,p.status AS principal_status,m.status AS membership_status,
 s.status AS session_status,g.status AS grant_status,g.version AS grant_version,
 s.idle_expires_at,s.absolute_expires_at,a.status AS access_status,l.status AS lifecycle_status,
 l.fresh_until>statement_timestamp() AS lifecycle_fresh
FROM principals p JOIN verified_emails e ON e.principal_id=p.id
JOIN sessions s ON s.principal_id=p.id AND s.id=sqlc.arg(session_id)
JOIN session_grants g ON g.tenant_id=sqlc.arg(tenant_id) AND g.session_id=s.id AND g.id=sqlc.arg(grant_id)
JOIN tenant_memberships m ON m.tenant_id=g.tenant_id AND m.id=g.membership_id AND m.principal_id=p.id
JOIN tenant_access a ON a.tenant_id=m.tenant_id JOIN current_tenant_lifecycle l ON l.tenant_id=m.tenant_id
WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='human' AND s.audience='console'
FOR UPDATE OF p,s,g,m;

-- name: LockInvitationPlatformActor :one
SELECT e.normalized_email,p.status AS principal_status,m.status AS membership_status,
 s.status AS session_status,g.status AS grant_status,g.version AS grant_version,s.idle_expires_at,s.absolute_expires_at
FROM principals p JOIN verified_emails e ON e.principal_id=p.id
JOIN sessions s ON s.principal_id=p.id AND s.id=sqlc.arg(session_id)
JOIN platform_session_grants g ON g.principal_id=p.id AND g.session_id=s.id AND g.id=sqlc.arg(grant_id)
JOIN platform_memberships m ON m.id=g.membership_id AND m.principal_id=p.id
WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='human' AND s.audience='boss'
FOR UPDATE OF p,s,g,m;

-- name: InvitationTargetLifecycle :one
SELECT status,fresh_until>statement_timestamp() AS fresh FROM current_tenant_lifecycle
WHERE tenant_id=sqlc.arg(tenant_id);

-- name: InvitationLatestTenantMembership :one
SELECT * FROM tenant_memberships WHERE tenant_id=sqlc.arg(tenant_id) AND principal_id=sqlc.arg(principal_id)
ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE;

-- name: InvitationLatestPlatformMembership :one
SELECT * FROM platform_memberships WHERE principal_id=sqlc.arg(principal_id)
ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE;

-- name: AcceptTenantInvitation :execrows
UPDATE tenant_invitations SET status='accepted',accepted_membership_id=sqlc.arg(membership_id),accepted_principal_id=sqlc.arg(principal_id),
 version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='pending' AND token_digest=sqlc.arg(token_digest) AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp();

-- name: AcceptPlatformInvitation :execrows
UPDATE platform_invitations SET status='accepted',accepted_membership_id=sqlc.arg(membership_id),accepted_principal_id=sqlc.arg(principal_id),
 version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version)
 AND status='pending' AND token_digest=sqlc.arg(token_digest) AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp();
