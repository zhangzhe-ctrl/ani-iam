-- name: CreateTenantMembership :exec
INSERT INTO tenant_memberships (
    tenant_id,
    id,
    principal_id,
    status,
    version,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(id),
    sqlc.arg(principal_id),
    sqlc.arg(status),
    sqlc.arg(version),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: GetTenantMembership :one
SELECT id, principal_id, status, version, created_at, updated_at
FROM tenant_memberships
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id);

-- name: UpdateTenantMembershipStatus :one
UPDATE tenant_memberships
SET status = sqlc.arg(status),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
RETURNING id, principal_id, status, version, created_at, updated_at;

-- name: AppendSecurityAuditEvent :exec
INSERT INTO iam_audit_events (
    tenant_id,
    event_id,
    actor_id,
    authentication_method,
    boundary,
    action,
    target_type,
    target_id,
    target_version,
    result,
    reason,
    request_id,
    correlation_id,
    decision_id,
    source_service,
    occurred_at,
    recorded_at
) VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(event_id),
    sqlc.arg(actor_id),
    sqlc.arg(authentication_method),
    sqlc.arg(boundary),
    sqlc.arg(action),
    sqlc.arg(target_type),
    sqlc.arg(target_id),
    sqlc.arg(target_version),
    sqlc.arg(result),
    sqlc.arg(reason),
    sqlc.arg(request_id),
    sqlc.arg(correlation_id),
    sqlc.arg(decision_id),
    sqlc.arg(source_service),
    sqlc.arg(occurred_at),
    sqlc.arg(recorded_at)
);
