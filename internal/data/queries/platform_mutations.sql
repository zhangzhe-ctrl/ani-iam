-- name: FindPlatformMutation :one
SELECT request_hash,result,created_at,expires_at FROM platform_mutation_results
WHERE actor_principal_id=sqlc.arg(actor_id) AND operation=sqlc.arg(operation) AND idempotency_key=sqlc.arg(idempotency_key);

-- name: SavePlatformMutation :exec
INSERT INTO platform_mutation_results(actor_principal_id,operation,idempotency_key,request_hash,result,created_at,expires_at)
VALUES(sqlc.arg(actor_id),sqlc.arg(operation),sqlc.arg(idempotency_key),sqlc.arg(request_hash),sqlc.arg(result),sqlc.arg(created_at),sqlc.arg(expires_at));

-- name: CreateCustomPlatformRole :exec
INSERT INTO platform_roles(id,code,display_name,system_role,system_definition_version,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(code),sqlc.arg(display_name),false,1,1,sqlc.arg(now),sqlc.arg(now));

-- name: UpdateCustomPlatformRole :execrows
UPDATE platform_roles SET display_name=sqlc.arg(display_name),version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND NOT system_role;

-- name: DeleteCustomPlatformRole :execrows
DELETE FROM platform_roles WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND NOT system_role;

-- name: DeleteCustomPlatformRolePermissions :exec
DELETE FROM platform_role_permissions p USING platform_roles r WHERE p.role_id=r.id AND r.id=sqlc.arg(role_id) AND NOT r.system_role;

-- name: PlatformRoleReferenced :one
SELECT EXISTS(SELECT 1 FROM platform_role_bindings b WHERE b.role_id=sqlc.arg(role_id)
UNION ALL SELECT 1 FROM platform_invitation_roles i WHERE i.role_id=sqlc.arg(role_id)) AS referenced;

-- name: UpdatePlatformTargetTenantAccess :one
UPDATE tenant_access SET status=sqlc.arg(status),version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND version=sqlc.arg(expected_version) RETURNING *;
