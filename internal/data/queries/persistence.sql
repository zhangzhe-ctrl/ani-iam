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

-- name: CreateTenantWorkloadBase :exec
INSERT INTO principals (
    id, principal_type, status, version, created_at, updated_at
) VALUES (
    sqlc.arg(id), 'workload', sqlc.arg(status), sqlc.arg(version),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: CreateTenantWorkloadProfile :exec
INSERT INTO workload_principals (
    principal_id, owner_type, tenant_id, membership_id, name, normalized_name,
    version, created_at, updated_at
) VALUES (
    sqlc.arg(principal_id), 'tenant', sqlc.arg(tenant_id), sqlc.narg(membership_id),
    sqlc.arg(name), sqlc.arg(normalized_name), sqlc.arg(version),
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: GetTenantWorkloadForUpdate :one
SELECT profile.principal_id, profile.membership_id, profile.name,
       profile.normalized_name, principal.status, profile.version,
       profile.created_at, profile.updated_at
FROM workload_principals AS profile
JOIN principals AS principal ON principal.id = profile.principal_id
WHERE profile.tenant_id = sqlc.arg(tenant_id)
  AND profile.principal_id = sqlc.arg(principal_id)
FOR UPDATE OF profile, principal;

-- name: GetTenantWorkloadBoundary :one
SELECT tenant_id
FROM workload_principals
WHERE principal_id = sqlc.arg(principal_id) AND owner_type = 'tenant';

-- name: GetTenantWorkload :one
SELECT profile.principal_id, profile.membership_id,
       profile.name, profile.normalized_name, principal.status,
       profile.version, profile.created_at, profile.updated_at
FROM workload_principals AS profile
JOIN principals AS principal ON principal.id = profile.principal_id
WHERE profile.tenant_id = sqlc.arg(tenant_id)
  AND profile.principal_id = sqlc.arg(principal_id);

-- name: ListTenantWorkloads :many
SELECT profile.tenant_id, profile.principal_id, profile.membership_id,
       profile.name, profile.normalized_name, principal.status,
       profile.version, profile.created_at, profile.updated_at
FROM workload_principals AS profile
JOIN principals AS principal ON principal.id = profile.principal_id
WHERE profile.tenant_id = sqlc.arg(tenant_id)
  AND profile.principal_id > sqlc.arg(cursor_id)
  AND (sqlc.arg(status)::text = '' OR principal.status = sqlc.arg(status)::text)
ORDER BY profile.principal_id
LIMIT sqlc.arg(page_limit);

-- name: ListAPIKeys :many
SELECT key_id, principal_id, status, display_prefix, secret_digest,
       never_expires, expires_at, created_at, last_used_at, revoked_at, version
FROM api_keys
WHERE tenant_id = sqlc.arg(tenant_id)
  AND principal_id = sqlc.arg(principal_id)
  AND key_id > sqlc.arg(cursor_id)
ORDER BY key_id
LIMIT sqlc.arg(page_limit);

-- name: GetAPIKeyBoundary :one
SELECT tenant_id
FROM api_keys
WHERE key_id = sqlc.arg(key_id);

-- name: LookupAPIKeyCredential :one
SELECT key_id, principal_id, status, display_prefix, secret_digest,
       never_expires, expires_at, created_at, last_used_at, revoked_at, version
FROM api_keys
WHERE tenant_id = sqlc.arg(tenant_id)
  AND key_id = sqlc.arg(key_id);

-- name: GetAPIKeyOperationalSignals :one
SELECT
    count(*) FILTER (
        WHERE status = 'active'
          AND (never_expires OR expires_at > sqlc.arg(observed_at))
    ) AS active_count,
    count(*) FILTER (
        WHERE status = 'active'
          AND never_expires
          AND COALESCE(last_used_at, created_at) <= sqlc.arg(stale_before)
    ) AS stale_non_expiring_count
FROM api_keys
WHERE tenant_id = sqlc.arg(tenant_id)
  AND principal_id = sqlc.arg(principal_id);

-- name: GetAPIKeyOperationalSnapshot :one
SELECT
    (
        SELECT count(*)
        FROM api_keys AS stale_key
        WHERE stale_key.status = 'active'
          AND stale_key.never_expires
          AND COALESCE(stale_key.last_used_at, stale_key.created_at) <= sqlc.arg(stale_before)
    ) AS stale_non_expiring_count,
    (
        SELECT count(*)
        FROM (
            SELECT active_key.tenant_id, active_key.principal_id
            FROM api_keys AS active_key
            WHERE active_key.status = 'active'
              AND (active_key.never_expires OR active_key.expires_at > sqlc.arg(observed_at))
            GROUP BY active_key.tenant_id, active_key.principal_id
            HAVING count(*) >= sqlc.arg(unusual_active_count_threshold)
        ) AS unusual_principals
    ) AS unusual_tenant_workload_count;

-- name: GetTenantWorkloadByMembershipForUpdate :one
SELECT profile.principal_id, profile.membership_id, profile.name,
       profile.normalized_name, principal.status, profile.version,
       profile.created_at, profile.updated_at
FROM workload_principals AS profile
JOIN principals AS principal ON principal.id = profile.principal_id
WHERE profile.tenant_id = sqlc.arg(tenant_id)
  AND profile.membership_id = sqlc.arg(membership_id)
FOR UPDATE OF profile, principal;

-- name: UpdateTenantWorkloadBaseStatus :execrows
UPDATE principals
SET status = sqlc.arg(status),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(principal_id)
  AND principal_type = 'workload'
  AND EXISTS (SELECT 1 FROM workload_principals WHERE principal_id = principals.id AND tenant_id = sqlc.arg(tenant_id) AND owner_type = 'tenant')
  AND principals.version = sqlc.arg(expected_version);

-- name: UpdateTenantWorkloadProfile :one
UPDATE workload_principals
SET membership_id = sqlc.narg(membership_id),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND principal_id = sqlc.arg(principal_id)
  AND version = sqlc.arg(expected_version)
RETURNING principal_id, membership_id, name, normalized_name,
          version, created_at, updated_at;

-- name: RevokeActiveAPIKeysForPrincipal :many
UPDATE api_keys
SET status = 'revoked',
    revoked_at = sqlc.arg(revoked_at),
    version = version + 1
WHERE tenant_id = sqlc.arg(tenant_id)
  AND principal_id = sqlc.arg(principal_id)
  AND status = 'active'
RETURNING key_id;

-- name: CreateAPIKey :exec
INSERT INTO api_keys (
    tenant_id, key_id, principal_id, status, display_prefix, secret_digest,
    never_expires, expires_at, created_at, last_used_at, revoked_at, version
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(key_id), sqlc.arg(principal_id), sqlc.arg(status),
    sqlc.arg(display_prefix), sqlc.arg(secret_digest), sqlc.arg(never_expires),
    sqlc.narg(expires_at), sqlc.arg(created_at), sqlc.narg(last_used_at),
    sqlc.narg(revoked_at), sqlc.arg(version)
);

-- name: GetAPIKeyForUpdate :one
SELECT key_id, principal_id, status, display_prefix, secret_digest,
       never_expires, expires_at, created_at, last_used_at, revoked_at, version
FROM api_keys
WHERE tenant_id = sqlc.arg(tenant_id)
  AND key_id = sqlc.arg(key_id)
FOR UPDATE;

-- name: RevokeAPIKey :one
UPDATE api_keys
SET status = 'revoked',
    revoked_at = sqlc.arg(revoked_at),
    version = version + 1
WHERE tenant_id = sqlc.arg(tenant_id)
  AND key_id = sqlc.arg(key_id)
  AND version = sqlc.arg(expected_version)
RETURNING key_id, principal_id, status, display_prefix, secret_digest,
          never_expires, expires_at, created_at, last_used_at, revoked_at, version;

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

-- name: LockTenantAdministrationGuard :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(tenant_id)::uuid::text, 9)
);

-- name: GetTenantAuthorizationAccess :one
SELECT status, version, created_at, updated_at
FROM tenant_access
WHERE tenant_id = sqlc.arg(tenant_id);

-- name: UpdateTenantAuthorizationAccessStatus :one
UPDATE tenant_access
SET status = sqlc.arg(status),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND version = sqlc.arg(expected_version)
RETURNING status, version, created_at, updated_at;

-- name: LockTenantAuthorizationMembership :one
SELECT id, principal_id, status, version, created_at, updated_at
FROM tenant_memberships
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
FOR UPDATE;

-- name: BumpTenantMembershipVersion :one
UPDATE tenant_memberships
SET version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id)
  AND version = sqlc.arg(expected_version)
RETURNING id, principal_id, status, version, created_at, updated_at;

-- name: GetTenantAuthorizationRole :one
SELECT id, code, system_role, system_definition_version,
       version, created_at, updated_at
FROM tenant_roles
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id);

-- name: ListTenantAuthorizationRolePermissions :many
SELECT resource, action
FROM tenant_role_permissions
WHERE tenant_id = sqlc.arg(tenant_id)
  AND role_id = sqlc.arg(role_id)
ORDER BY resource, action;

-- name: GetTenantAuthorizationMembership :one
SELECT membership.id, membership.principal_id, principal.principal_type,
       membership.status, membership.version, membership.created_at, membership.updated_at
FROM tenant_memberships AS membership
JOIN principals AS principal
  ON principal.id = membership.principal_id
WHERE membership.tenant_id = sqlc.arg(tenant_id)
  AND membership.id = sqlc.arg(id);

-- name: ListTenantAuthorizationMemberships :many
SELECT membership.id, membership.principal_id, principal.principal_type,
       membership.status, membership.version, membership.created_at, membership.updated_at
FROM tenant_memberships AS membership
JOIN principals AS principal
  ON principal.id = membership.principal_id
WHERE membership.tenant_id = sqlc.arg(tenant_id)
  AND membership.id > sqlc.arg(cursor_id)
  AND (sqlc.arg(status)::text = '' OR membership.status = sqlc.arg(status)::text)
ORDER BY membership.id
LIMIT sqlc.arg(page_limit);

-- name: ListTenantAuthorizationMembershipRoleIDs :many
SELECT role_id
FROM tenant_role_bindings
WHERE tenant_id = sqlc.arg(tenant_id)
  AND membership_id = sqlc.arg(membership_id)
ORDER BY role_id;

-- name: ListTenantAuthorizationRoles :many
SELECT id, code, system_role, system_definition_version,
       version, created_at, updated_at
FROM tenant_roles
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id > sqlc.arg(cursor_id)
ORDER BY id
LIMIT sqlc.arg(page_limit);

-- name: IsActiveHumanTenantAdministrator :one
SELECT EXISTS (
    SELECT 1
    FROM tenant_memberships AS membership
    JOIN principals AS principal
      ON principal.id = membership.principal_id
    JOIN tenant_role_bindings AS binding
      ON binding.tenant_id = membership.tenant_id
     AND binding.membership_id = membership.id
    JOIN tenant_roles AS role
      ON role.tenant_id = binding.tenant_id
     AND role.id = binding.role_id
    WHERE membership.tenant_id = sqlc.arg(tenant_id)
      AND membership.id = sqlc.arg(membership_id)
      AND membership.status = 'active'
      AND principal.principal_type = 'human'
      AND principal.status = 'active'
      AND role.system_role
      AND role.code = 'tenant-admin'
) AS is_active_human_administrator;

-- name: CountActiveHumanTenantAdministrators :one
SELECT count(DISTINCT membership.id)
FROM tenant_memberships AS membership
JOIN principals AS principal
  ON principal.id = membership.principal_id
JOIN tenant_role_bindings AS binding
  ON binding.tenant_id = membership.tenant_id
 AND binding.membership_id = membership.id
JOIN tenant_roles AS role
  ON role.tenant_id = binding.tenant_id
 AND role.id = binding.role_id
WHERE membership.tenant_id = sqlc.arg(tenant_id)
  AND membership.status = 'active'
  AND principal.principal_type = 'human'
  AND principal.status = 'active'
  AND role.system_role
  AND role.code = 'tenant-admin';

-- name: CreateTenantRoleBinding :exec
INSERT INTO tenant_role_bindings (
    tenant_id, id, membership_id, role_id, version, created_at, updated_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(id), sqlc.arg(membership_id),
    sqlc.arg(role_id), sqlc.arg(version), sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: DeleteTenantRoleBinding :one
DELETE FROM tenant_role_bindings
WHERE tenant_id = sqlc.arg(tenant_id)
  AND membership_id = sqlc.arg(membership_id)
  AND role_id = sqlc.arg(role_id)
RETURNING id, membership_id, role_id, version, created_at, updated_at;

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
    recorded_at, caller_principal_id, caller_binding_id, caller_binding_version, caller_grant_version
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
    sqlc.arg(recorded_at), sqlc.narg(caller_principal_id), sqlc.narg(caller_binding_id), sqlc.narg(caller_binding_version), sqlc.narg(caller_grant_version)
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
    credential.password_hash,
    credential.failed_attempts,
    credential.locked_until,
    credential.version AS credential_version
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

-- name: RecordPasswordLoginFailure :one
UPDATE password_credentials
SET failed_attempts = CASE
        WHEN locked_until IS NOT NULL AND locked_until <= sqlc.arg(failed_at) THEN 1
        ELSE failed_attempts + 1
    END,
    locked_until = CASE
        WHEN locked_until IS NOT NULL AND locked_until <= sqlc.arg(failed_at) THEN NULL
        WHEN failed_attempts + 1 >= 5 THEN sqlc.arg(lock_until)::timestamptz
        ELSE NULL
    END,
    version = version + 1,
    updated_at = sqlc.arg(failed_at)
WHERE principal_id = sqlc.arg(principal_id)
RETURNING failed_attempts, locked_until, version;

-- name: AppendAnonymousTenantSecurityAuditEvent :exec
INSERT INTO iam_audit_events (
    tenant_id, event_id, actor_id, authentication_method, boundary,
    action, target_type, target_id, target_version, result, reason,
    request_id, correlation_id, decision_id, source_service,
    occurred_at, recorded_at
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(event_id), NULL, 'anonymous', 'tenant',
    sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id),
    sqlc.arg(target_version), sqlc.arg(result), sqlc.arg(reason),
    sqlc.arg(request_id), sqlc.arg(correlation_id), sqlc.arg(decision_id),
    sqlc.arg(source_service), sqlc.arg(occurred_at), sqlc.arg(recorded_at)
);

-- name: ResetPasswordLoginFailures :one
UPDATE password_credentials
SET failed_attempts = 0,
    locked_until = NULL,
    version = CASE
        WHEN failed_attempts <> 0 OR locked_until IS NOT NULL THEN version + 1
        ELSE version
    END,
    updated_at = CASE
        WHEN failed_attempts <> 0 OR locked_until IS NOT NULL THEN sqlc.arg(updated_at)
        ELSE updated_at
    END
WHERE principal_id = sqlc.arg(principal_id)
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: LookupPasswordActionTarget :one
SELECT
    principal.id AS principal_id,
    credential.principal_id IS NOT NULL AS has_password,
    email.normalized_email
FROM verified_emails AS email
JOIN principals AS principal
  ON principal.id = email.principal_id
 AND principal.principal_type = 'human'
 AND principal.status = 'active'
LEFT JOIN password_credentials AS credential
  ON credential.principal_id = principal.id
WHERE email.normalized_email = sqlc.arg(normalized_account);

-- name: LockPasswordAuthenticationIdempotencyKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(idempotency_key)::text, 0)
);

-- name: LockPasswordActionPrincipal :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(principal_id)::uuid::text, 1)
);

-- name: GetPasswordActionRequestByIdempotencyKey :one
SELECT operation_id, account_digest, audience, expires_at
FROM password_action_requests
WHERE idempotency_key = sqlc.arg(idempotency_key)
FOR UPDATE;

-- name: CreateUnknownPasswordActionRequest :exec
INSERT INTO password_action_requests (
    operation_id, account_digest, audience, expires_at,
    idempotency_key, created_at
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(account_digest), sqlc.arg(audience),
    sqlc.arg(expires_at), sqlc.arg(idempotency_key), sqlc.arg(created_at)
);

-- name: CreateKnownPasswordActionRequest :exec
INSERT INTO password_action_requests (
    operation_id, account_digest, audience, principal_id, purpose,
    expires_at, idempotency_key, created_at
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(account_digest), sqlc.arg(audience),
    sqlc.arg(principal_id), sqlc.arg(purpose), sqlc.arg(expires_at),
    sqlc.arg(idempotency_key), sqlc.arg(created_at)
);

-- name: ReplaceActivePasswordActions :many
UPDATE password_actions
SET status = 'replaced',
    replaced_by = sqlc.arg(replaced_by),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE principal_id = sqlc.arg(principal_id)
  AND status = 'active'
RETURNING operation_id;

-- name: CancelReplacedPasswordActionNotifications :exec
UPDATE notification_outbox AS outbox
SET status = 'cancelled',
 destination_key_version = NULL, destination_ciphertext = NULL,
    claimed_at = NULL,
    version = outbox.version + 1,
    updated_at = sqlc.arg(updated_at)
FROM password_actions AS action
WHERE outbox.operation_id = action.operation_id
  AND action.principal_id = sqlc.arg(principal_id)
  AND action.status = 'replaced'
  AND action.replaced_by = sqlc.arg(replaced_by)
  AND outbox.status IN ('pending', 'claimed');

-- name: CreatePasswordAction :exec
INSERT INTO password_actions (
    operation_id, principal_id, purpose, status, expires_at,
    version, created_at, updated_at
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(principal_id), sqlc.arg(purpose),
    'active', sqlc.arg(expires_at), 1, sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: CreatePasswordActionNotification :exec
INSERT INTO notification_outbox (
    id, operation_id, principal_id, intent, destination_key_version, destination_ciphertext, status, available_at,
    version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(operation_id), sqlc.arg(principal_id),
    sqlc.arg(intent), sqlc.arg(destination_key_version), sqlc.arg(destination_ciphertext), 'pending', sqlc.arg(available_at), 1,
    sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: ClaimPasswordActionNotification :one
WITH candidate AS (
    SELECT outbox.id
    FROM notification_outbox AS outbox
    JOIN password_actions AS action
      ON action.operation_id = outbox.operation_id
    WHERE (
        (outbox.status = 'pending' AND outbox.available_at <= sqlc.arg(now))
        OR (
            outbox.status = 'claimed'
            AND outbox.claimed_at <= sqlc.arg(lease_expired_before)
        )
    )
      AND action.status = 'active'
      AND action.expires_at > sqlc.arg(now)
    ORDER BY outbox.available_at, outbox.id
    FOR UPDATE OF outbox SKIP LOCKED
    LIMIT 1
)
UPDATE notification_outbox AS outbox
SET status = 'claimed',
    attempt_count = outbox.attempt_count + 1,
    claimed_at = sqlc.arg(now),
    version = outbox.version + 1,
    updated_at = sqlc.arg(now)
FROM candidate, password_actions AS action
WHERE outbox.id = candidate.id
  AND action.operation_id = outbox.operation_id
RETURNING outbox.id, outbox.operation_id, outbox.principal_id,
          outbox.intent, outbox.destination_key_version, outbox.destination_ciphertext, outbox.attempt_count,
          outbox.version, action.created_at AS issued_at,
          action.expires_at;

-- name: ReschedulePasswordActionNotification :one
UPDATE notification_outbox
SET status = 'pending',
    available_at = sqlc.arg(available_at),
    claimed_at = NULL,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: MarkPasswordActionNotificationAttentionRequired :one
UPDATE notification_outbox
SET status = 'attention_required',
    claimed_at = NULL,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: MarkPasswordActionNotificationDelivered :one
UPDATE notification_outbox
SET status = 'delivered',
 destination_key_version = NULL, destination_ciphertext = NULL,
    delivered_at = sqlc.arg(delivered_at),
    notification_id = sqlc.arg(notification_id),
    version = version + 1,
    updated_at = sqlc.arg(delivered_at)
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: AppendAnonymousPrincipalSecurityAuditEvent :exec
INSERT INTO iam_audit_events (
    tenant_id, event_id, actor_id, authentication_method, boundary,
    action, target_type, target_id, target_version, result, reason,
    request_id, correlation_id, decision_id, source_service,
    occurred_at, recorded_at
) VALUES (
    NULL, sqlc.arg(event_id), NULL, 'anonymous', 'principal',
    sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id),
    sqlc.arg(target_version), sqlc.arg(result), sqlc.arg(reason),
    sqlc.arg(request_id), sqlc.arg(correlation_id), sqlc.arg(decision_id),
    sqlc.arg(source_service), sqlc.arg(occurred_at), sqlc.arg(recorded_at)
);

-- name: GetPasswordActionCompletionByIdempotencyKey :one
SELECT operation_id, principal_id, credential_version, request_fingerprint
FROM password_action_completions
WHERE idempotency_key = sqlc.arg(idempotency_key)
FOR UPDATE;

-- name: LockPasswordAction :one
SELECT operation_id, principal_id, purpose, status, expires_at, version
FROM password_actions
WHERE operation_id = sqlc.arg(operation_id)
FOR UPDATE;

-- name: ConsumePasswordAction :one
UPDATE password_actions
SET status = 'consumed',
    consumed_at = sqlc.arg(completed_at),
    version = version + 1,
    updated_at = sqlc.arg(completed_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND principal_id = sqlc.arg(principal_id)
  AND purpose = sqlc.arg(purpose)
  AND status = 'active'
  AND expires_at = sqlc.arg(expires_at)
  AND expires_at > sqlc.arg(completed_at)
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: UpdatePasswordCredentialForReset :one
UPDATE password_credentials
SET password_hash = sqlc.arg(password_hash),
    failed_attempts = 0,
    locked_until = NULL,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE principal_id = sqlc.arg(principal_id)
RETURNING version;

-- name: GetVerifiedAccountForPrincipal :one
SELECT normalized_email
FROM verified_emails
WHERE principal_id = sqlc.arg(principal_id);

-- name: CreatePasswordIdentity :exec
INSERT INTO identities (
    id, principal_id, provider, issuer, subject, status,
    version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(principal_id), 'password', 'ani-local',
    sqlc.arg(subject), 'active', 1, sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: CreatePasswordCredential :one
INSERT INTO password_credentials (
    principal_id, identity_id, password_hash, algorithm,
    failed_attempts, locked_until, version, created_at, updated_at
) VALUES (
    sqlc.arg(principal_id), sqlc.arg(identity_id), sqlc.arg(password_hash),
    'argon2id', 0, NULL, 1, sqlc.arg(created_at), sqlc.arg(created_at)
)
RETURNING version;

-- name: RevokeRefreshTokensForPrincipal :exec
UPDATE refresh_tokens AS token
SET status = 'revoked'
WHERE token.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM refresh_token_families AS family
    JOIN session_grants AS grant_row
      ON grant_row.tenant_id = family.tenant_id
     AND grant_row.id = family.grant_id
    JOIN sessions AS session_row
      ON session_row.id = grant_row.session_id
    WHERE family.tenant_id = token.tenant_id
      AND family.id = token.family_id
      AND session_row.principal_id = sqlc.arg(principal_id)
  );

-- name: RevokeRefreshTokenFamiliesForPrincipal :exec
UPDATE refresh_token_families AS family
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE family.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM session_grants AS grant_row
    JOIN sessions AS session_row
      ON session_row.id = grant_row.session_id
    WHERE grant_row.tenant_id = family.tenant_id
      AND grant_row.id = family.grant_id
      AND session_row.principal_id = sqlc.arg(principal_id)
  );

-- name: RevokeSessionGrantsForPrincipal :exec
UPDATE session_grants AS grant_row
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE grant_row.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM sessions AS session_row
    WHERE session_row.id = grant_row.session_id
      AND session_row.principal_id = sqlc.arg(principal_id)
  );

-- name: RevokeSessionsForPrincipal :exec
UPDATE sessions
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE principal_id = sqlc.arg(principal_id)
  AND status = 'active';

-- name: CancelPasswordActionNotification :exec
UPDATE notification_outbox
SET status = 'cancelled',
 destination_key_version = NULL, destination_ciphertext = NULL,
    claimed_at = NULL,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE operation_id = sqlc.arg(operation_id)
  AND status IN ('pending', 'claimed');

-- name: AppendPrincipalSecurityAuditEvent :exec
INSERT INTO iam_audit_events (
    tenant_id, event_id, actor_id, authentication_method, boundary,
    action, target_type, target_id, target_version, result, reason,
    request_id, correlation_id, decision_id, source_service,
    occurred_at, recorded_at
) VALUES (
    NULL, sqlc.arg(event_id), sqlc.arg(actor_id), 'password_action', 'principal',
    sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id),
    sqlc.arg(target_version), sqlc.arg(result), sqlc.arg(reason),
    sqlc.arg(request_id), sqlc.arg(correlation_id), sqlc.arg(decision_id),
    sqlc.arg(source_service), sqlc.arg(occurred_at), sqlc.arg(recorded_at)
);

-- name: CreatePasswordActionCompletion :exec
INSERT INTO password_action_completions (
    idempotency_key, operation_id, principal_id, request_fingerprint,
    credential_version, completed_at
) VALUES (
    sqlc.arg(idempotency_key), sqlc.arg(operation_id), sqlc.arg(principal_id), sqlc.arg(request_fingerprint),
    sqlc.arg(credential_version), sqlc.arg(completed_at)
);

-- name: CreateSession :exec
INSERT INTO sessions (
    id, principal_id, audience, status, authn_methods, device_name, idle_expires_at,
    absolute_expires_at, reauthenticated_at, version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(principal_id), sqlc.arg(audience), sqlc.arg(status), sqlc.arg(authn_methods),
    sqlc.arg(device_name), sqlc.arg(idle_expires_at),
    sqlc.arg(absolute_expires_at), sqlc.arg(reauthenticated_at), 1,
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: LookupOIDCLogin :one
SELECT
    identity.id AS identity_id,
    principal.id AS principal_id,
    principal.status AS principal_status,
    membership.id AS membership_id,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    email.normalized_email
FROM identities AS identity
JOIN principals AS principal
  ON principal.id = identity.principal_id
JOIN verified_emails AS email
  ON email.principal_id = principal.id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = sqlc.arg(tenant_id)
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE identity.provider = sqlc.arg(provider)
  AND identity.issuer = sqlc.arg(issuer)
  AND identity.subject = sqlc.arg(subject)
  AND identity.status = 'active';

-- name: LockOIDCLoginAuthentication :one
SELECT
    identity.status AS identity_status,
    principal.status AS principal_status,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    email.normalized_email
FROM identities AS identity
JOIN principals AS principal
  ON principal.id = identity.principal_id
JOIN verified_emails AS email
  ON email.principal_id = principal.id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = sqlc.arg(tenant_id)
 AND membership.id = sqlc.arg(membership_id)
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE identity.id = sqlc.arg(identity_id)
  AND identity.provider = sqlc.arg(provider)
  AND identity.issuer = sqlc.arg(issuer)
  AND identity.subject = sqlc.arg(subject)
  AND principal.id = sqlc.arg(principal_id)
FOR SHARE OF identity, principal, membership, access;

-- name: LookupOIDCReauthentication :one
SELECT
    principal.id AS principal_id,
    principal.status AS principal_status,
    session.id AS session_id,
    session.status AS session_status,
    session_grant.id AS grant_id,
    session_grant.status AS grant_status,
    session_grant.version AS grant_version,
    session_grant.tenant_id,
    session.reauthenticated_at
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
JOIN tenant_access AS access ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle ON lifecycle.tenant_id = membership.tenant_id
WHERE principal.id = sqlc.arg(principal_id)
  AND session.idle_expires_at > statement_timestamp()
  AND session.absolute_expires_at > statement_timestamp()
  AND membership.status = 'active'
  AND access.status = 'active'
  AND lifecycle.status = 'active'
  AND lifecycle.fresh_until > statement_timestamp();

-- name: LookupVerifiedEmailOwner :one
SELECT principal_id
FROM verified_emails
WHERE normalized_email = sqlc.arg(normalized_email);

-- name: LockOIDCLinkAuthentication :one
SELECT session.reauthenticated_at
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
WHERE principal.id = sqlc.arg(principal_id)
  AND principal.status = 'active'
  AND session.status = 'active'
  AND session.idle_expires_at > statement_timestamp()
  AND session.absolute_expires_at > statement_timestamp()
  AND session_grant.status = 'active'
  AND session_grant.version = sqlc.arg(expected_grant_version)
  AND membership.status = 'active'
  AND access.status = 'active'
  AND lifecycle.status = 'active'
  AND lifecycle.fresh_until > statement_timestamp()
FOR SHARE OF principal, session, session_grant, membership, access;

-- name: LookupOIDCIdentityOwner :one
SELECT principal_id
FROM identities
WHERE issuer = sqlc.arg(issuer) AND subject = sqlc.arg(subject)
FOR SHARE;

-- name: CreateOIDCIdentity :exec
INSERT INTO identities (
    id, principal_id, provider, issuer, subject, status, version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(principal_id), sqlc.arg(provider), sqlc.arg(issuer),
    sqlc.arg(subject), 'active', 1, sqlc.arg(created_at), sqlc.arg(updated_at)
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

-- name: LookupAPIKeyAuthorization :one
SELECT
    api_key.key_id,
    api_key.principal_id,
    api_key.status AS api_key_status,
    api_key.display_prefix,
    api_key.secret_digest,
    api_key.never_expires,
    api_key.expires_at,
    api_key.created_at,
    api_key.last_used_at,
    api_key.revoked_at,
    api_key.version AS api_key_version,
    principal.status AS principal_status,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
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
FROM api_keys AS api_key
JOIN workload_principals AS profile
  ON profile.tenant_id = api_key.tenant_id
 AND profile.principal_id = api_key.principal_id
JOIN principals AS principal
  ON principal.id = profile.principal_id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = profile.tenant_id
 AND membership.id = profile.membership_id
 AND membership.principal_id = profile.principal_id
JOIN tenant_access AS access
  ON access.tenant_id = profile.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = profile.tenant_id
WHERE api_key.tenant_id = sqlc.arg(tenant_id)
  AND api_key.key_id = sqlc.arg(key_id);

-- name: RecordAPIKeyUse :one
UPDATE api_keys
SET last_used_at = CASE
        WHEN last_used_at IS NULL OR last_used_at < sqlc.arg(observed_at)::timestamptz - interval '15 minutes'
        THEN sqlc.arg(observed_at)
        ELSE last_used_at
    END
WHERE tenant_id = sqlc.arg(tenant_id)
  AND key_id = sqlc.arg(key_id)
  AND (never_expires OR expires_at > sqlc.arg(observed_at))
RETURNING key_id;

-- name: GetTenantAccessStatusForAuthorization :one
SELECT status
FROM tenant_access
WHERE tenant_id = sqlc.arg(tenant_id);

-- name: GetTenantLifecycleFreshnessForAuthorization :one
SELECT fresh_until > statement_timestamp() AS lifecycle_fresh
FROM tenant_lifecycle_projections
WHERE tenant_id = sqlc.arg(tenant_id);

-- name: LookupRefreshSession :one
SELECT
    token.tenant_id,
    principal.id AS principal_id,
    principal.status AS principal_status,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at AS session_created_at,
    session_row.updated_at AS session_updated_at,
    grant_row.id AS grant_id,
    grant_row.membership_id,
    grant_row.status AS grant_status,
    grant_row.version AS grant_version,
    grant_row.created_at AS grant_created_at,
    grant_row.updated_at AS grant_updated_at,
    family.id AS family_id,
    family.status AS family_status,
    family.version AS family_version,
    family.created_at AS family_created_at,
    family.updated_at AS family_updated_at,
    token.id AS token_id,
    token.digest,
    token.status AS token_status,
    token.issued_at,
    token.expires_at,
    token.consumed_at,
    token.replaced_by
FROM refresh_tokens AS token
JOIN refresh_token_families AS family
  ON family.tenant_id = token.tenant_id
 AND family.id = token.family_id
JOIN session_grants AS grant_row
  ON grant_row.tenant_id = family.tenant_id
 AND grant_row.id = family.grant_id
JOIN sessions AS session_row
  ON session_row.id = grant_row.session_id
JOIN principals AS principal
  ON principal.id = session_row.principal_id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = grant_row.tenant_id
 AND membership.id = grant_row.membership_id
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE token.digest = sqlc.arg(refresh_digest);

-- name: LockRefreshSession :one
SELECT
    token.tenant_id,
    principal.id AS principal_id,
    principal.status AS principal_status,
    membership.status AS membership_status,
    access.status AS tenant_access_status,
    lifecycle.status AS lifecycle_status,
    lifecycle.fresh_until > statement_timestamp() AS lifecycle_fresh,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at AS session_created_at,
    session_row.updated_at AS session_updated_at,
    grant_row.id AS grant_id,
    grant_row.membership_id,
    grant_row.status AS grant_status,
    grant_row.version AS grant_version,
    grant_row.created_at AS grant_created_at,
    grant_row.updated_at AS grant_updated_at,
    family.id AS family_id,
    family.status AS family_status,
    family.version AS family_version,
    family.created_at AS family_created_at,
    family.updated_at AS family_updated_at,
    token.id AS token_id,
    token.digest,
    token.status AS token_status,
    token.issued_at,
    token.expires_at,
    token.consumed_at,
    token.replaced_by
FROM refresh_tokens AS token
JOIN refresh_token_families AS family
  ON family.tenant_id = token.tenant_id
 AND family.id = token.family_id
JOIN session_grants AS grant_row
  ON grant_row.tenant_id = family.tenant_id
 AND grant_row.id = family.grant_id
JOIN sessions AS session_row
  ON session_row.id = grant_row.session_id
JOIN principals AS principal
  ON principal.id = session_row.principal_id
JOIN tenant_memberships AS membership
  ON membership.tenant_id = grant_row.tenant_id
 AND membership.id = grant_row.membership_id
 AND membership.principal_id = principal.id
JOIN tenant_access AS access
  ON access.tenant_id = membership.tenant_id
JOIN tenant_lifecycle_projections AS lifecycle
  ON lifecycle.tenant_id = membership.tenant_id
WHERE token.digest = sqlc.arg(refresh_digest)
-- The runtime role intentionally has SELECT-only access to lifecycle projections.
-- Lock mutable authentication rows while re-reading lifecycle in this transaction;
-- do not broaden runtime privileges merely to obtain a row lock on the projection.
FOR UPDATE OF token, family, grant_row, session_row, principal, membership, access;

-- name: ConsumeRefreshToken :one
UPDATE refresh_tokens
SET status = 'consumed',
    consumed_at = sqlc.arg(consumed_at),
    replaced_by = sqlc.arg(replaced_by)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(token_id)
  AND family_id = sqlc.arg(family_id)
  AND digest = sqlc.arg(refresh_digest)
  AND status = 'active'
RETURNING id;

-- name: UpdateSessionIdleExpiry :one
UPDATE sessions
SET idle_expires_at = sqlc.arg(idle_expires_at),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(session_id)
  AND status = 'active'
  AND version = sqlc.arg(expected_version)
  AND idle_expires_at > sqlc.arg(updated_at)
  AND absolute_expires_at > sqlc.arg(updated_at)
RETURNING version;

-- name: RevokeRefreshFamilyForReuse :one
UPDATE refresh_token_families
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(family_id)
  AND grant_id = sqlc.arg(grant_id)
  AND status = 'active'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: RevokeActiveRefreshTokensForFamily :exec
UPDATE refresh_tokens
SET status = 'revoked'
WHERE tenant_id = sqlc.arg(tenant_id)
  AND family_id = sqlc.arg(family_id)
  AND status = 'active';

-- name: IncrementSessionGrantVersionForReuse :one
UPDATE session_grants
SET version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(grant_id)
  AND status = 'active'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: LookupLogoutSession :one
SELECT
    principal.id AS principal_id,
    principal.status AS principal_status,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at,
    session_row.updated_at
FROM refresh_tokens AS token
JOIN refresh_token_families AS family
  ON family.tenant_id = token.tenant_id
 AND family.id = token.family_id
JOIN session_grants AS grant_row
  ON grant_row.tenant_id = family.tenant_id
 AND grant_row.id = family.grant_id
JOIN sessions AS session_row
  ON session_row.id = grant_row.session_id
JOIN principals AS principal
  ON principal.id = session_row.principal_id
WHERE token.digest = sqlc.arg(refresh_digest);

-- name: LockLogoutSession :one
SELECT
    principal.id AS principal_id,
    principal.status AS principal_status,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at,
    session_row.updated_at
FROM refresh_tokens AS token
JOIN refresh_token_families AS family
  ON family.tenant_id = token.tenant_id
 AND family.id = token.family_id
JOIN session_grants AS grant_row
  ON grant_row.tenant_id = family.tenant_id
 AND grant_row.id = family.grant_id
JOIN sessions AS session_row
  ON session_row.id = grant_row.session_id
JOIN principals AS principal
  ON principal.id = session_row.principal_id
WHERE token.digest = sqlc.arg(refresh_digest)
FOR UPDATE OF token, family, grant_row, session_row, principal;

-- name: RevokeCurrentSession :one
UPDATE sessions
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(session_id)
  AND status = 'active'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: RevokeCurrentSessionGrants :exec
UPDATE session_grants
SET status = 'revoked',
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE session_id = sqlc.arg(session_id)
  AND status = 'active';

-- name: RevokeCurrentSessionFamilies :exec
UPDATE refresh_token_families AS family
SET status = 'revoked',
    version = family.version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE family.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM session_grants AS grant_row
    WHERE grant_row.tenant_id = family.tenant_id
      AND grant_row.id = family.grant_id
      AND grant_row.session_id = sqlc.arg(session_id)
  );

-- name: RevokeCurrentSessionRefreshTokens :exec
UPDATE refresh_tokens AS token
SET status = 'revoked'
WHERE token.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM refresh_token_families AS family
    JOIN session_grants AS grant_row
      ON grant_row.tenant_id = family.tenant_id
     AND grant_row.id = family.grant_id
    WHERE family.tenant_id = token.tenant_id
      AND family.id = token.family_id
      AND grant_row.session_id = sqlc.arg(session_id)
  );

-- name: AppendPrincipalSessionSecurityAuditEvent :exec
INSERT INTO iam_audit_events (
    tenant_id, event_id, actor_id, authentication_method, boundary,
    action, target_type, target_id, target_version, result, reason,
    request_id, correlation_id, decision_id, source_service,
    occurred_at, recorded_at
) VALUES (
    NULL, sqlc.arg(event_id), sqlc.arg(actor_id), sqlc.arg(authentication_method), 'principal',
    sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id),
    sqlc.arg(target_version), sqlc.arg(result), sqlc.arg(reason),
    sqlc.arg(request_id), sqlc.arg(correlation_id), sqlc.arg(decision_id),
    sqlc.arg(source_service), sqlc.arg(occurred_at), sqlc.arg(recorded_at)
);

-- name: LockSessionContinuity :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(session_id)::uuid::text, 2)
);

-- name: LookupTenantSwitch :one
SELECT
    source_grant.tenant_id AS source_tenant_id,
    target_membership.tenant_id AS target_tenant_id,
    principal.id AS principal_id,
    principal.status AS principal_status,
    target_membership.id AS target_membership_id,
    target_membership.status AS target_membership_status,
    target_access.status AS target_access_status,
    target_lifecycle.status AS target_lifecycle_status,
    target_lifecycle.fresh_until > statement_timestamp() AS target_lifecycle_fresh,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at AS session_created_at,
    session_row.updated_at AS session_updated_at,
    source_grant.id AS source_grant_id,
    source_grant.membership_id AS source_membership_id,
    source_grant.status AS source_grant_status,
    source_grant.version AS source_grant_version,
    source_grant.created_at AS source_grant_created_at,
    source_grant.updated_at AS source_grant_updated_at,
    target_grant.id AS target_grant_id,
    target_grant.status AS target_grant_status,
    target_grant.version AS target_grant_version,
    target_grant.created_at AS target_grant_created_at,
    target_grant.updated_at AS target_grant_updated_at,
    target_family.id AS target_family_id,
    target_family.status AS target_family_status,
    target_family.version AS target_family_version,
    target_family.created_at AS target_family_created_at,
    target_family.updated_at AS target_family_updated_at
FROM principals AS principal
JOIN sessions AS session_row
  ON session_row.id = sqlc.arg(session_id)
 AND session_row.principal_id = principal.id
JOIN session_grants AS source_grant
  ON source_grant.tenant_id = sqlc.arg(source_tenant_id)
 AND source_grant.id = sqlc.arg(source_grant_id)
 AND source_grant.session_id = session_row.id
JOIN tenant_memberships AS source_membership
  ON source_membership.tenant_id = source_grant.tenant_id
 AND source_membership.id = source_grant.membership_id
 AND source_membership.principal_id = principal.id
JOIN tenant_memberships AS target_membership
  ON target_membership.tenant_id = sqlc.arg(target_tenant_id)
 AND target_membership.principal_id = principal.id
 AND target_membership.status <> 'removed'
JOIN tenant_access AS target_access
  ON target_access.tenant_id = target_membership.tenant_id
JOIN tenant_lifecycle_projections AS target_lifecycle
  ON target_lifecycle.tenant_id = target_membership.tenant_id
LEFT JOIN session_grants AS target_grant
  ON target_grant.tenant_id = target_membership.tenant_id
 AND target_grant.session_id = session_row.id
 AND target_grant.status = 'active'
LEFT JOIN refresh_token_families AS target_family
  ON target_family.tenant_id = target_grant.tenant_id
 AND target_family.grant_id = target_grant.id
 AND target_family.status = 'active'
WHERE principal.id = sqlc.arg(principal_id);

-- name: LockTenantSwitchBoundary :one
SELECT
    source_grant.tenant_id AS source_tenant_id,
    target_membership.tenant_id AS target_tenant_id,
    principal.id AS principal_id,
    principal.status AS principal_status,
    target_membership.id AS target_membership_id,
    target_membership.status AS target_membership_status,
    target_access.status AS target_access_status,
    target_lifecycle.status AS target_lifecycle_status,
    target_lifecycle.fresh_until > statement_timestamp() AS target_lifecycle_fresh,
    session_row.id AS session_id,
    session_row.audience,
    session_row.status AS session_status,
    session_row.version AS session_version,
    session_row.authn_methods,
    session_row.device_name,
    session_row.idle_expires_at,
    session_row.absolute_expires_at,
    session_row.reauthenticated_at,
    session_row.created_at AS session_created_at,
    session_row.updated_at AS session_updated_at,
    source_grant.id AS source_grant_id,
    source_grant.membership_id AS source_membership_id,
    source_grant.status AS source_grant_status,
    source_grant.version AS source_grant_version,
    source_grant.created_at AS source_grant_created_at,
    source_grant.updated_at AS source_grant_updated_at
FROM principals AS principal
JOIN sessions AS session_row
  ON session_row.id = sqlc.arg(session_id)
 AND session_row.principal_id = principal.id
JOIN session_grants AS source_grant
  ON source_grant.tenant_id = sqlc.arg(source_tenant_id)
 AND source_grant.id = sqlc.arg(source_grant_id)
 AND source_grant.session_id = session_row.id
JOIN tenant_memberships AS source_membership
  ON source_membership.tenant_id = source_grant.tenant_id
 AND source_membership.id = source_grant.membership_id
 AND source_membership.principal_id = principal.id
JOIN tenant_memberships AS target_membership
  ON target_membership.tenant_id = sqlc.arg(target_tenant_id)
 AND target_membership.principal_id = principal.id
 AND target_membership.status <> 'removed'
JOIN tenant_access AS target_access
  ON target_access.tenant_id = target_membership.tenant_id
JOIN tenant_lifecycle_projections AS target_lifecycle
  ON target_lifecycle.tenant_id = target_membership.tenant_id
WHERE principal.id = sqlc.arg(principal_id)
-- tenant_lifecycle_projections is a read-only projection for the IAM runtime.
FOR UPDATE OF principal, session_row, source_grant, source_membership,
    target_membership, target_access;

-- name: LockActiveTargetGrant :one
SELECT id, membership_id, status, version, created_at, updated_at
FROM session_grants
WHERE tenant_id = sqlc.arg(tenant_id)
  AND session_id = sqlc.arg(session_id)
  AND status = 'active'
FOR UPDATE;

-- name: LockActiveRefreshFamily :one
SELECT id, grant_id, status, version, created_at, updated_at
FROM refresh_token_families
WHERE tenant_id = sqlc.arg(tenant_id)
  AND grant_id = sqlc.arg(grant_id)
  AND status = 'active'
FOR UPDATE;

-- name: LockActiveRefreshToken :one
SELECT id, family_id, digest, status, issued_at, expires_at, consumed_at, replaced_by
FROM refresh_tokens
WHERE tenant_id = sqlc.arg(tenant_id)
  AND family_id = sqlc.arg(family_id)
  AND status = 'active'
FOR UPDATE;

-- name: IncrementSessionGrantVersionForSwitch :one
UPDATE session_grants
SET version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(grant_id)
  AND session_id = sqlc.arg(session_id)
  AND membership_id = sqlc.arg(membership_id)
  AND status = 'active'
  AND version = sqlc.arg(expected_version)
RETURNING version;

-- name: ResolveWorkloadIdentity :one
SELECT binding.principal_id, binding.id AS binding_id,
       principal.version AS principal_version, binding.version AS binding_version
FROM workload_identity_bindings AS binding
JOIN workload_principals AS profile ON profile.principal_id = binding.principal_id
JOIN principals AS principal ON principal.id = profile.principal_id
WHERE binding.environment = sqlc.arg(environment) AND binding.trust_domain = sqlc.arg(trust_domain)
  AND binding.identity_kind = sqlc.arg(identity_kind) AND binding.identity_value = sqlc.arg(identity_value)
  AND binding.status = 'active' AND profile.owner_type = 'platform'
  AND principal.principal_type = 'workload' AND principal.status = 'active';

-- name: CheckWorkloadGrant :one
SELECT authority.version
FROM workload_grants AS authority
JOIN principals AS principal ON principal.id = authority.principal_id
JOIN workload_principals AS profile ON profile.principal_id = principal.id
JOIN workload_identity_bindings AS binding ON binding.principal_id = principal.id
WHERE authority.principal_id = sqlc.arg(principal_id)
  AND authority.environment = sqlc.arg(environment) AND authority.trust_domain = sqlc.arg(trust_domain)
  AND authority.audience = sqlc.arg(audience) AND authority.operation = sqlc.arg(operation)
  AND ((authority.audience = 'ani-iam' AND authority.scope = 'iam_ingress')
       OR (authority.audience = 'ani-session-gateway' AND authority.operation = 'session.create' AND authority.scope = 'delegated_session')
       OR (authority.audience = 'ani-notification-service' AND authority.operation IN ('notification.submit','notification.get_own') AND authority.scope = 'workload_notification'))
  AND authority.status = 'active'
  AND principal.principal_type = 'workload' AND principal.status = 'active'
  AND principal.version = sqlc.arg(principal_version) AND profile.owner_type = 'platform'
  AND binding.id = sqlc.arg(binding_id) AND binding.version = sqlc.arg(binding_version)
  AND binding.status = 'active';

-- name: LockTenantMutationResult :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));

-- name: FindTenantMutationResult :one
SELECT intent_digest, caller_principal_id, result, created_at, expires_at
FROM tenant_mutation_results
WHERE tenant_id = sqlc.arg(tenant_id) AND actor_id = sqlc.arg(actor_id)
  AND operation = sqlc.arg(operation) AND idempotency_key = sqlc.arg(idempotency_key);

-- name: SaveTenantMutationResult :exec
INSERT INTO tenant_mutation_results (tenant_id, actor_id, operation, idempotency_key,
    intent_digest, caller_principal_id, result, created_at, expires_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(actor_id), sqlc.arg(operation), sqlc.arg(idempotency_key),
    sqlc.arg(intent_digest), sqlc.narg(caller_principal_id), sqlc.arg(result), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: AppendWorkloadCredentialSecurityAuditEvent :exec
INSERT INTO iam_audit_events (tenant_id, event_id, actor_id, authentication_method, boundary, action,
    target_type, target_id, target_version, result, reason, request_id, correlation_id, decision_id,
    source_service, occurred_at, recorded_at, caller_principal_id, caller_binding_id, caller_binding_version, caller_grant_version)
VALUES (sqlc.narg(tenant_id), sqlc.arg(event_id), sqlc.arg(actor_id), 'workload_token', sqlc.arg(boundary), sqlc.arg(action),
    sqlc.arg(target_type), sqlc.arg(target_id), sqlc.arg(target_version), 'succeeded', 'CURRENT_AUTHORITY_VERIFIED',
    sqlc.arg(request_id), sqlc.arg(request_id), sqlc.arg(request_id), 'iam-service', sqlc.arg(now), sqlc.arg(now),
    sqlc.arg(caller_principal_id), sqlc.arg(caller_binding_id), sqlc.arg(caller_binding_version), sqlc.arg(caller_grant_version));
