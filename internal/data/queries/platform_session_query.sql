-- name: ListOwnedPlatformSessionGrants :many
SELECT g.* FROM platform_session_grants g JOIN sessions s ON s.id=g.session_id AND s.principal_id=g.principal_id AND s.audience='boss'
WHERE s.principal_id=sqlc.arg(principal_id) AND g.session_id=sqlc.arg(session_id) ORDER BY g.id;
