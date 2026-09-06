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

-- name: LookupPasswordLogin :one
SELECT
    principal.id AS principal_id,
    principal.status AS principal_status,
    membership.id AS membership_id,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    credential.password_hash
FROM verified_emails AS email
JOIN principals AS principal
  ON principal.id = email.principal_id
JOIN identities AS identity
  ON identity.principal_id = principal.id
 AND identity.provider = 'password'
 AND identity.status = 'active'
JOIN password_credentials AS credential
  ON credential.principal_id = principal.id
 AND credential.identity_id = identity.id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = sqlc.arg(tenant_id)
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE email.normalized_email = sqlc.arg(normalized_account);

-- name: CreateSession :exec
INSERT INTO sessions (
    id, principal_id, audience, status, device_name, idle_expires_at,
    absolute_expires_at, version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(principal_id), sqlc.arg(audience), sqlc.arg(status),
    sqlc.arg(device_name), sqlc.arg(idle_expires_at),
    sqlc.arg(absolute_expires_at), 1, sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: CreateSessionGrant :exec
INSERT INTO session_grants (
    tenant_id, id, session_id, membership_id, status, version, created_at,
    updated_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(id), sqlc.arg(session_id),
    sqlc.arg(membership_id), sqlc.arg(status), sqlc.arg(version),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: CreateRefreshTokenFamily :exec
INSERT INTO refresh_token_families (
    tenant_id, id, grant_id, status, version, created_at, updated_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(id), sqlc.arg(grant_id), sqlc.arg(status),
    1, sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (
    tenant_id, id, family_id, digest, status, issued_at, expires_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(id), sqlc.arg(family_id), sqlc.arg(digest),
    'active', sqlc.arg(issued_at), sqlc.arg(expires_at)
);

-- name: LookupAuthorization :one
SELECT
    principal.status AS principal_status,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    session.status AS session_status,
    session_grant.status AS grant_status,
    session_grant.version AS grant_version,
    NOT EXISTS (
        SELECT 1
        FROM unnest(sqlc.arg(actions)::text[]) AS required_action(action)
        WHERE NOT EXISTS (
            SELECT 1
            FROM tenant_role_bindings AS binding
            JOIN tenant_role_permissions AS permission
              ON permission.tenant_id = binding.tenant_id
             AND permission.role_id = binding.role_id
            WHERE binding.tenant_id = sqlc.arg(tenant_id)
              AND binding.membership_id = membership.id
              AND permission.resource = sqlc.arg(resource)
              AND permission.action = required_action.action
        )
    ) AS permission_allowed
FROM principals AS principal
JOIN sessions AS session
  ON session.id = sqlc.arg(session_id)
 AND session.principal_id = principal.id
JOIN session_grants AS session_grant
  ON session_grant.tenant_id = sqlc.arg(tenant_id)
 AND session_grant.id = sqlc.arg(grant_id)
 AND session_grant.session_id = session.id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = session_grant.tenant_id
 AND membership.id = session_grant.membership_id
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE principal.id = sqlc.arg(principal_id);
