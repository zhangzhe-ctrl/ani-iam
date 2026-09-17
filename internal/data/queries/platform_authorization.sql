-- name: LookupPlatformAuthorization :one
SELECT p.status AS principal_status,m.id AS membership_id,m.status AS membership_status,
s.status AS session_status,s.idle_expires_at,s.absolute_expires_at,s.reauthenticated_at,
g.status AS grant_status,g.version AS grant_version,
(NOT EXISTS(SELECT 1 FROM unnest(sqlc.arg(actions)::text[]) AS requested(action)
WHERE NOT EXISTS(SELECT 1 FROM platform_role_bindings b JOIN platform_role_permissions rp ON rp.role_id=b.role_id
WHERE b.membership_id=m.id AND rp.scope='platform' AND rp.resource=sqlc.arg(resource) AND rp.action=requested.action)))::boolean AS permission_allowed
FROM platform_session_grants g
JOIN sessions s ON s.id=g.session_id AND s.principal_id=g.principal_id AND s.audience='boss'
JOIN platform_memberships m ON m.id=g.membership_id AND m.principal_id=g.principal_id
JOIN principals p ON p.id=g.principal_id AND p.principal_type='human'
WHERE p.id=sqlc.arg(principal_id) AND s.id=sqlc.arg(session_id) AND g.id=sqlc.arg(grant_id);
