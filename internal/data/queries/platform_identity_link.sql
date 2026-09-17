-- name: LockPlatformIdentityLinkAuthentication :one
SELECT s.reauthenticated_at
FROM principals p JOIN platform_memberships m ON m.principal_id=p.id
JOIN platform_session_grants g ON g.membership_id=m.id AND g.principal_id=p.id
JOIN sessions s ON s.id=g.session_id AND s.principal_id=p.id
WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='human' AND p.status='active'
AND m.status='active' AND g.id=sqlc.arg(grant_id) AND g.status='active' AND g.version=sqlc.arg(grant_version)
AND s.id=sqlc.arg(session_id) AND s.audience='boss' AND s.status='active'
AND s.idle_expires_at>statement_timestamp() AND s.absolute_expires_at>statement_timestamp()
FOR UPDATE OF p,m,g,s;
