-- name: ListPlatformAdministratorCandidates :many
SELECT DISTINCT m.id AS membership_id,
(EXISTS(SELECT 1 FROM verified_emails e WHERE e.principal_id=p.id)
 AND EXISTS(SELECT 1 FROM identities i WHERE i.principal_id=p.id AND i.status='active'
 AND ((sqlc.arg(oidc_provider)::text<>'' AND sqlc.arg(oidc_issuer)::text<>'' AND i.provider=sqlc.arg(oidc_provider) AND i.issuer=sqlc.arg(oidc_issuer))
 OR (sqlc.arg(password_enabled)::boolean AND i.provider='password' AND EXISTS(SELECT 1 FROM password_credentials c WHERE c.principal_id=p.id AND c.identity_id=i.id AND (c.locked_until IS NULL OR c.locked_until<=statement_timestamp()))))))::boolean AS login_capable
FROM platform_memberships m JOIN principals p ON p.id=m.principal_id
JOIN platform_role_bindings b ON b.membership_id=m.id JOIN platform_roles r ON r.id=b.role_id
WHERE m.status='active' AND p.status='active' AND p.principal_type='human' AND r.code='platform-admin' AND r.system_role;

-- name: UpdatePlatformMembershipStatus :one
UPDATE platform_memberships SET status=sqlc.arg(status),version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status<>'removed' RETURNING *;

-- name: AdvancePlatformMembershipVersion :one
UPDATE platform_memberships SET version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='active' RETURNING *;

-- name: RemovePlatformMembershipRoleBinding :one
DELETE FROM platform_role_bindings WHERE membership_id=sqlc.arg(membership_id) AND role_id=sqlc.arg(role_id) RETURNING *;

-- name: AdvancePlatformMembershipVersionForUnbind :one
UPDATE platform_memberships SET version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) RETURNING *;
