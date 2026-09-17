-- name: GetPlatformAuditEvent :one
SELECT e.*, (CASE WHEN e.authentication_method IN ('workload_provisioner','administrator_provisioner') THEN 'initialization'
 WHEN p.principal_type='workload' THEN 'workload' WHEN p.principal_type='human' THEN 'principal' ELSE 'anonymous' END)::text AS actor_type
FROM iam_audit_events e LEFT JOIN principals p ON p.id=e.actor_id
WHERE e.event_id=sqlc.arg(event_id);

-- name: ListPlatformAuditEvents :many
SELECT e.*, (CASE WHEN e.authentication_method IN ('workload_provisioner','administrator_provisioner') THEN 'initialization'
 WHEN p.principal_type='workload' THEN 'workload' WHEN p.principal_type='human' THEN 'principal' ELSE 'anonymous' END)::text AS actor_type
FROM iam_audit_events e LEFT JOIN principals p ON p.id=e.actor_id
WHERE (sqlc.arg(tenant_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.tenant_id=sqlc.arg(tenant_id))
 AND (sqlc.arg(action)::text='' OR e.action=sqlc.arg(action))
 AND (sqlc.arg(result)::text='' OR e.result=sqlc.arg(result))
 AND (sqlc.arg(before_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.event_id<sqlc.arg(before_id))
ORDER BY e.event_id DESC LIMIT sqlc.arg(page_limit);
