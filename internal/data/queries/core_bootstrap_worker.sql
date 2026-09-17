-- name: GetCoreBootstrapWorkSource :one
SELECT r.* FROM core_bootstrap_receipts r
JOIN tenant_integration_receipts s ON s.producer=r.producer AND s.epoch=r.source_epoch AND s.event_id=r.event_id AND s.source_sequence=r.source_sequence
WHERE r.tenant_id=sqlc.arg(tenant_id) AND r.operation_id=sqlc.arg(operation_id) AND r.producer=sqlc.arg(producer)
AND s.tenant_id=r.tenant_id AND s.kind='bootstrap' AND s.raw_payload=r.raw_payload
ORDER BY r.source_sequence LIMIT 1;

-- name: GetCoreBootstrapWorkerResult :one
SELECT * FROM core_bootstrap_worker_results WHERE tenant_id=sqlc.arg(tenant_id) AND operation_id=sqlc.arg(operation_id);

-- name: LockCoreBootstrapWorkerPrincipal :one
SELECT p.id FROM principals p JOIN workload_principals w ON w.principal_id=p.id
WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='workload' AND p.status='active' AND p.version=sqlc.arg(expected_version)
AND w.owner_type='platform' AND sqlc.arg(valid_until)::timestamptz>clock_timestamp() FOR SHARE OF p;

-- name: CreateCoreBootstrapTenantInvitation :exec
INSERT INTO tenant_invitations(tenant_id,id,normalized_email,role_ids,locale,status,token_digest,delivery_generation,expires_at,version,created_by,created_at,updated_at,bootstrap_operation_id)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(normalized_email),sqlc.arg(role_ids),sqlc.arg(locale),'pending',sqlc.arg(token_digest),1,sqlc.arg(expires_at),1,sqlc.arg(created_by),sqlc.arg(now),sqlc.arg(now),sqlc.arg(operation_id));

-- name: MarkCoreBootstrapWaiting :execrows
UPDATE tenant_bootstrap_operations SET status='waiting_for_principal_verification',version=version+1,updated_at=sqlc.arg(now)
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(operation_id) AND source_kind='core' AND status='pending' AND version=sqlc.arg(expected_version) AND superseded_by IS NULL;

-- name: SaveCoreBootstrapWorkerResult :exec
INSERT INTO core_bootstrap_worker_results(tenant_id,operation_id,source_event_id,producer_principal_id,executor_principal_id,producer_version,executor_version,decision_id,role_id,invitation_id,delivery_id,audit_id,created_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(operation_id),sqlc.arg(source_event_id),sqlc.arg(producer_principal_id),sqlc.arg(executor_principal_id),sqlc.arg(producer_version),sqlc.arg(executor_version),sqlc.arg(decision_id),sqlc.arg(role_id),sqlc.arg(invitation_id),sqlc.arg(delivery_id),sqlc.arg(audit_id),sqlc.arg(now));
