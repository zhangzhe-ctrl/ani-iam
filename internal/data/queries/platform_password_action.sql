-- Password reset is a global Human action. These statements run with the
-- existing Console revocations and Credential/Audit in one local transaction.
-- The Platform administrator guard is acquired before any Credential lock,
-- matching BOSS login, refresh and explicit identity-link transactions.

-- name: RevokePlatformRefreshTokensForPrincipal :exec
UPDATE platform_refresh_tokens t SET status='revoked'
FROM platform_refresh_token_families f JOIN platform_session_grants g ON g.id=f.grant_id
WHERE t.family_id=f.id AND g.principal_id=sqlc.arg(principal_id) AND t.status='active';

-- name: RevokePlatformRefreshFamiliesForPrincipal :exec
UPDATE platform_refresh_token_families f SET status='revoked',version=f.version+1,updated_at=sqlc.arg(updated_at)
FROM platform_session_grants g
WHERE f.grant_id=g.id AND g.principal_id=sqlc.arg(principal_id) AND f.status='active';

-- name: RevokePlatformGrantsForPrincipal :exec
UPDATE platform_session_grants SET status='revoked',version=version+1,updated_at=sqlc.arg(updated_at)
WHERE principal_id=sqlc.arg(principal_id) AND status='active';
