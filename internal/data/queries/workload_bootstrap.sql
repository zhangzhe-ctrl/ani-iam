-- name: LockWorkloadBootstrap :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(environment)::text, 0));

-- name: GetWorkloadBootstrapReceipt :one
SELECT intent_sha256, receipt FROM workload_bootstrap_receipts WHERE manifest_id = sqlc.arg(manifest_id);

-- name: GetWorkloadBootstrapEnvironmentReceipt :one
SELECT manifest_id FROM workload_bootstrap_receipts WHERE environment = sqlc.arg(environment);

-- name: InsertBootstrapPrincipal :exec
INSERT INTO principals (id, principal_type, status, version, created_at, updated_at)
VALUES (sqlc.arg(principal_id), 'workload', 'active', 1, sqlc.arg(now), sqlc.arg(now));

-- name: InsertBootstrapProfile :exec
INSERT INTO workload_principals (principal_id, owner_type, name, normalized_name, environment, trust_domain, version, created_at, updated_at)
VALUES (sqlc.arg(principal_id), 'platform', sqlc.arg(name), sqlc.arg(name), sqlc.arg(environment), sqlc.arg(trust_domain), 1, sqlc.arg(now), sqlc.arg(now));

-- name: InsertBootstrapBinding :exec
INSERT INTO workload_identity_bindings (id, principal_id, environment, trust_domain, identity_kind, identity_value, status, version, created_at, updated_at)
VALUES (sqlc.arg(binding_id), sqlc.arg(principal_id), sqlc.arg(environment), sqlc.arg(trust_domain), 'x509_dns', sqlc.arg(identity_value), 'active', 1, sqlc.arg(now), sqlc.arg(now));

-- name: InsertBootstrapGrant :exec
INSERT INTO workload_grants (id, principal_id, environment, trust_domain, audience, operation, scope, status, version, created_at, updated_at)
VALUES (sqlc.arg(grant_id), sqlc.arg(principal_id), sqlc.arg(environment), sqlc.arg(trust_domain), sqlc.arg(audience), sqlc.arg(operation), sqlc.arg(scope), 'active', 1, sqlc.arg(now), sqlc.arg(now));

-- name: InsertWorkloadBootstrapReceipt :exec
INSERT INTO workload_bootstrap_receipts (manifest_id, environment, trust_domain, ca_sha256, intent_sha256, receipt, audit_event_id, completed_at)
VALUES (sqlc.arg(manifest_id), sqlc.arg(environment), sqlc.arg(trust_domain), sqlc.arg(ca_sha256), sqlc.arg(intent_sha256), sqlc.arg(receipt), sqlc.arg(audit_event_id), sqlc.arg(now));

-- name: InsertWorkloadBootstrapAudit :exec
INSERT INTO iam_audit_events (event_id, authentication_method, boundary, action, target_type, target_id, target_version, result, reason,
    request_id, correlation_id, decision_id, source_service, occurred_at, recorded_at, provisioner_role, bootstrap_manifest_id)
VALUES (sqlc.arg(event_id), 'workload_provisioner', 'platform', 'workload.bootstrap', 'workload_bootstrap', sqlc.arg(manifest_id), 1, 'succeeded', 'REVIEWED_MANIFEST_CONSUMED',
    sqlc.arg(request_id), sqlc.arg(request_id), sqlc.arg(intent_digest), 'ani-iam-provisioner', sqlc.arg(now), sqlc.arg(now), current_user, sqlc.arg(manifest_id));
