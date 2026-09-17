-- name: LockPlatformRefreshSession :one
SELECT sqlc.embed(p),sqlc.embed(m),sqlc.embed(s),sqlc.embed(g),sqlc.embed(f),sqlc.embed(t)
FROM platform_refresh_tokens t
JOIN platform_refresh_token_families f ON f.id=t.family_id
JOIN platform_session_grants g ON g.id=f.grant_id
JOIN sessions s ON s.id=g.session_id AND s.principal_id=g.principal_id AND s.audience='boss'
JOIN platform_memberships m ON m.id=g.membership_id AND m.principal_id=g.principal_id
JOIN principals p ON p.id=g.principal_id AND p.principal_type='human'
WHERE t.digest=sqlc.arg(digest)
FOR UPDATE OF s,g,f,t FOR SHARE OF p,m;

-- name: ConsumePlatformRefreshToken :execrows
UPDATE platform_refresh_tokens SET status='consumed',consumed_at=sqlc.arg(now),replaced_by=sqlc.arg(replacement_id)
WHERE id=sqlc.arg(id) AND family_id=sqlc.arg(family_id) AND status='active';

-- name: TouchPlatformSession :one
UPDATE sessions SET version=version+1,idle_expires_at=sqlc.arg(idle_expires_at),updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND principal_id=sqlc.arg(principal_id) AND audience='boss' AND status='active'
RETURNING version;

-- name: RevokePlatformRefreshFamily :execrows
UPDATE platform_refresh_token_families SET status='revoked',version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND grant_id=sqlc.arg(grant_id) AND status='active';

-- name: RevokePlatformFamilyTokens :exec
UPDATE platform_refresh_tokens SET status='revoked'
WHERE family_id=sqlc.arg(family_id) AND status='active';

-- name: InvalidatePlatformGrantVersion :execrows
UPDATE platform_session_grants SET version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND session_id=sqlc.arg(session_id);

-- name: RevokePlatformSession :execrows
UPDATE sessions SET status='revoked',version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND principal_id=sqlc.arg(principal_id) AND audience='boss' AND status='active';

-- name: RevokePlatformSessionGrants :exec
UPDATE platform_session_grants SET status='revoked',version=version+1,updated_at=sqlc.arg(now)
WHERE session_id=sqlc.arg(session_id) AND principal_id=sqlc.arg(principal_id) AND status='active';

-- name: RevokePlatformSessionFamilies :exec
UPDATE platform_refresh_token_families f SET status='revoked',version=f.version+1,updated_at=sqlc.arg(now)
FROM platform_session_grants g
WHERE f.grant_id=g.id AND g.session_id=sqlc.arg(session_id) AND g.principal_id=sqlc.arg(principal_id) AND f.status='active';

-- name: RevokePlatformSessionTokens :exec
UPDATE platform_refresh_tokens t SET status='revoked'
FROM platform_refresh_token_families f JOIN platform_session_grants g ON g.id=f.grant_id
WHERE t.family_id=f.id AND g.session_id=sqlc.arg(session_id) AND g.principal_id=sqlc.arg(principal_id) AND t.status='active';
