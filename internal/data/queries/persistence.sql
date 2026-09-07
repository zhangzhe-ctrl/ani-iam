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
    id, operation_id, principal_id, intent, destination_email, status, available_at,
    version, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(operation_id), sqlc.arg(principal_id),
    sqlc.arg(intent), sqlc.arg(destination_email), 'pending', sqlc.arg(available_at), 1,
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
          outbox.intent, outbox.destination_email, outbox.attempt_count,
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
SELECT operation_id, principal_id, credential_version
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
    idempotency_key, operation_id, principal_id,
    credential_version, completed_at
) VALUES (
    sqlc.arg(idempotency_key), sqlc.arg(operation_id), sqlc.arg(principal_id),
    sqlc.arg(credential_version), sqlc.arg(completed_at)
);

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
