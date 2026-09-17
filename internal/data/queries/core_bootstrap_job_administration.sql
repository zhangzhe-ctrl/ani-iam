-- Current scheduling metadata is bounded to initialize and the current
-- invitation generation. Older generations remain in immutable history.
-- name: ListCurrentCoreBootstrapJobs :many
SELECT j.kind,j.generation,j.state,j.attempt_count,j.cycle_start_attempt,j.last_error,j.available_at,j.recovery_id
FROM core_bootstrap_jobs j
WHERE j.tenant_id=sqlc.arg(tenant_id) AND j.operation_id=sqlc.arg(operation_id) AND j.producer=sqlc.arg(producer)
AND (j.kind='initialize' OR EXISTS(SELECT 1 FROM tenant_invitations i WHERE i.tenant_id=j.tenant_id AND i.bootstrap_operation_id=j.operation_id AND i.delivery_generation=j.generation))
ORDER BY j.kind LIMIT 2;

-- name: AppendCoreBootstrapJobRecovery :exec
INSERT INTO core_bootstrap_job_recoveries(tenant_id,id,operation_id,kind,generation,source_event_id,producer,payload_fingerprint,previous_attempt,previous_recovery_id,actor_id,reason_code)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(operation_id),sqlc.arg(kind),sqlc.arg(generation),sqlc.arg(source_event_id),sqlc.arg(producer),sqlc.arg(payload_fingerprint),sqlc.arg(previous_attempt),sqlc.narg(previous_recovery_id),sqlc.arg(actor_id),sqlc.arg(reason_code));

-- name: RetryCoreBootstrapJob :execrows
UPDATE core_bootstrap_jobs SET state='pending',cycle_start_attempt=attempt_count,recovery_id=sqlc.arg(recovery_id),available_at=clock_timestamp(),last_error='',updated_at=clock_timestamp()
WHERE tenant_id=sqlc.arg(tenant_id) AND operation_id=sqlc.arg(operation_id) AND kind=sqlc.arg(kind) AND generation=sqlc.arg(generation) AND producer=sqlc.arg(producer)
AND state='attention_required' AND attempt_count=sqlc.arg(expected_attempt);

-- name: ObserveCoreBootstrapQueue :one
SELECT
 (SELECT count(*) FROM core_bootstrap_jobs j JOIN tenant_bootstrap_operations o ON o.tenant_id=j.tenant_id AND o.id=j.operation_id
 WHERE j.producer=sqlc.arg(producer) AND j.state='attention_required' AND o.superseded_by IS NULL
 AND ((j.kind='initialize' AND o.status='pending') OR (j.kind='expire' AND o.status='waiting_for_principal_verification' AND EXISTS(SELECT 1 FROM tenant_invitations i WHERE i.tenant_id=j.tenant_id AND i.bootstrap_operation_id=j.operation_id AND i.delivery_generation=j.generation))))::bigint AS job_attention,
 (SELECT count(DISTINCT r.operation_id) FROM core_bootstrap_receipts r JOIN tenant_bootstrap_operations o ON o.tenant_id=r.tenant_id AND o.id=r.operation_id
 WHERE r.producer=sqlc.arg(producer) AND o.source_kind='core' AND o.status='attention_required' AND o.superseded_by IS NULL)::bigint AS invitation_attention;
