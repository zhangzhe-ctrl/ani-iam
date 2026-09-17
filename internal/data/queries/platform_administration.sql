-- name: GetPlatformMembership :one
SELECT * FROM platform_memberships WHERE id=sqlc.arg(id);

-- name: ListPlatformMemberships :many
SELECT * FROM platform_memberships
WHERE (sqlc.arg(status)::text='' OR status=sqlc.arg(status)) AND id>sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: GetPlatformMembershipRoles :many
SELECT role_id FROM platform_role_bindings WHERE membership_id=sqlc.arg(membership_id) ORDER BY role_id;

-- name: GetPlatformRole :one
SELECT * FROM platform_roles WHERE id=sqlc.arg(id);

-- name: ListPlatformRoles :many
SELECT * FROM platform_roles WHERE id>sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: GetPlatformTargetTenantAccess :one
SELECT * FROM tenant_access WHERE tenant_id=sqlc.arg(tenant_id);
