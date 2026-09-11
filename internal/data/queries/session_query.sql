-- name: ListOwnedHumanSessions :many
SELECT s.* FROM sessions s
WHERE s.principal_id = sqlc.arg(principal_id)
  AND s.id > sqlc.arg(after_id)
ORDER BY s.id ASC LIMIT sqlc.arg(row_limit);

-- name: ListOwnedSessionGrants :many
SELECT g.* FROM session_grants g
JOIN sessions s ON s.id = g.session_id
WHERE g.tenant_id = sqlc.arg(tenant_id)
  AND g.session_id = sqlc.arg(session_id)
  AND s.principal_id = sqlc.arg(principal_id)
ORDER BY g.id ASC;
