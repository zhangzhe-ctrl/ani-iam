-- name: CreateCustomTenantRole :exec
INSERT INTO tenant_roles (tenant_id,id,code,display_name,system_role,system_definition_version,version,created_at,updated_at)
VALUES (sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(code),sqlc.arg(display_name),false,1,1,sqlc.arg(created_at),sqlc.arg(updated_at));

-- name: UpdateCustomTenantRole :execrows
UPDATE tenant_roles SET display_name=sqlc.arg(display_name),version=version+1,updated_at=sqlc.arg(updated_at)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND NOT system_role;

-- name: DeleteCustomTenantRole :execrows
DELETE FROM tenant_roles
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND NOT system_role;

-- name: ClearCustomTenantRolePermissions :exec
DELETE FROM tenant_role_permissions
WHERE tenant_id=sqlc.arg(tenant_id) AND role_id=sqlc.arg(role_id);

-- name: AddCustomTenantRolePermission :exec
INSERT INTO tenant_role_permissions(tenant_id,role_id,scope,resource,action,created_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(role_id),'tenant',sqlc.arg(resource),sqlc.arg(action),sqlc.arg(created_at));

-- name: CustomTenantRoleReferenced :one
SELECT EXISTS(SELECT 1 FROM tenant_role_bindings b
WHERE b.tenant_id=sqlc.arg(tenant_id) AND b.role_id=sqlc.arg(role_id)
UNION ALL SELECT 1 FROM tenant_invitation_roles i
WHERE i.tenant_id=sqlc.arg(tenant_id) AND i.role_id=sqlc.arg(role_id)) AS referenced;
