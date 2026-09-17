-- name: ListWorkloadTargetRegistrations :many
SELECT audience, operation, scope, target_sha256, bundle_sha256, enabled
FROM workload_target_registrations ORDER BY audience, operation;

-- name: LockWorkloadRegistryInstallation :exec
SELECT pg_advisory_xact_lock(hashtextextended('iam.workload-target-registration.v1',0));

-- name: UpsertWorkloadTargetRegistration :exec
INSERT INTO workload_target_registrations
    (audience, operation, scope, target_sha256, bundle_sha256, enabled)
VALUES (sqlc.arg(audience), sqlc.arg(operation), sqlc.arg(scope),
        sqlc.arg(target_sha256), sqlc.arg(bundle_sha256), sqlc.arg(enabled))
ON CONFLICT (audience, operation) DO UPDATE SET
    scope = EXCLUDED.scope,
    target_sha256 = EXCLUDED.target_sha256,
    bundle_sha256 = EXCLUDED.bundle_sha256,
    enabled = EXCLUDED.enabled;

-- name: InsertWorkloadRegistryPermission :exec
INSERT INTO permission_catalog (scope, resource, action)
VALUES ('tenant', sqlc.arg(resource), sqlc.arg(action))
ON CONFLICT DO NOTHING;

-- name: InsertWorkloadRegistryInstallation :exec
INSERT INTO workload_registry_installations
    (id, previous_sha256, installed_sha256, installed_by, installed_at)
VALUES (sqlc.arg(id), sqlc.narg(previous_sha256), sqlc.arg(installed_sha256), current_user, sqlc.arg(installed_at));
