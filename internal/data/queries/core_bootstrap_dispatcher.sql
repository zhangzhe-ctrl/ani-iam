-- Discovery returns only exact work identifiers. All following locks and writes
-- carry Tenant/operation/kind and the configured producer.
-- name: DiscoverCoreBootstrapJobs :many
WITH eligible AS (
 SELECT r.tenant_id,r.operation_id,'initialize'::text AS kind,1::bigint AS generation,min(r.source_sequence) AS source_sequence
 FROM core_bootstrap_receipts r JOIN tenant_bootstrap_operations o ON o.tenant_id=r.tenant_id AND o.id=r.operation_id
 JOIN tenant_integration_receipts s ON s.producer=r.producer AND s.epoch=r.source_epoch AND s.event_id=r.event_id AND s.kind='bootstrap' AND s.tenant_id=r.tenant_id
 WHERE r.producer=sqlc.arg(producer) AND o.source_kind='core' AND o.status='pending' AND o.superseded_by IS NULL
 GROUP BY r.tenant_id,r.operation_id
 UNION ALL
 SELECT r.tenant_id,r.operation_id,'expire'::text,i.delivery_generation AS generation,min(r.source_sequence)
 FROM core_bootstrap_receipts r JOIN tenant_bootstrap_operations o ON o.tenant_id=r.tenant_id AND o.id=r.operation_id
 JOIN core_bootstrap_worker_results w ON w.tenant_id=o.tenant_id AND w.operation_id=o.id
 JOIN tenant_invitations i ON i.tenant_id=w.tenant_id AND i.id=w.invitation_id
 WHERE r.producer=sqlc.arg(producer) AND o.source_kind='core' AND o.status='waiting_for_principal_verification' AND o.superseded_by IS NULL
 AND i.status IN ('pending','expired') AND i.expires_at<=clock_timestamp()
 GROUP BY r.tenant_id,r.operation_id,i.delivery_generation
), candidates AS (
 SELECT e.tenant_id,e.operation_id,e.kind,e.generation,e.source_sequence FROM eligible e
 LEFT JOIN core_bootstrap_jobs j ON j.tenant_id=e.tenant_id AND j.operation_id=e.operation_id AND j.kind=e.kind AND j.generation=e.generation
 WHERE j.tenant_id IS NULL OR (j.state='pending' AND j.available_at<=clock_timestamp())
 UNION
 SELECT j.tenant_id,j.operation_id,j.kind,j.generation,r.source_sequence FROM core_bootstrap_jobs j
 JOIN core_bootstrap_receipts r ON r.tenant_id=j.tenant_id AND r.event_id=j.source_event_id
 WHERE j.producer=sqlc.arg(producer) AND j.state='claimed' AND j.lease_until<=clock_timestamp()
)
SELECT tenant_id,operation_id,kind,generation FROM candidates ORDER BY source_sequence,tenant_id,kind LIMIT 32;

-- name: CreateCoreBootstrapJob :exec
INSERT INTO core_bootstrap_jobs(tenant_id,operation_id,kind,generation,producer,source_event_id)
VALUES(sqlc.arg(tenant_id),sqlc.arg(operation_id),sqlc.arg(kind),sqlc.arg(generation),sqlc.arg(producer),sqlc.arg(source_event_id));

-- name: LockCoreBootstrapJob :one
SELECT *,COALESCE(lease_until>clock_timestamp(),false)::boolean AS lease_current,available_at<=clock_timestamp() AS due,clock_timestamp()::timestamptz AS observed_at
FROM core_bootstrap_jobs WHERE tenant_id=sqlc.arg(tenant_id) AND operation_id=sqlc.arg(operation_id) AND kind=sqlc.arg(kind) AND generation=sqlc.arg(generation) AND producer=sqlc.arg(producer) FOR UPDATE;

-- name: ReadCoreBootstrapInvitation :one
SELECT i.* FROM core_bootstrap_worker_results w JOIN tenant_invitations i ON i.tenant_id=w.tenant_id AND i.id=w.invitation_id
WHERE w.tenant_id=sqlc.arg(tenant_id) AND w.operation_id=sqlc.arg(operation_id);

-- name: ClaimCoreBootstrapJob :exec
UPDATE core_bootstrap_jobs SET state='claimed',attempt_count=attempt_count+1,lease_id=sqlc.arg(lease_id),lease_started_at=clock_timestamp(),lease_until=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp()
WHERE tenant_id=sqlc.arg(tenant_id) AND operation_id=sqlc.arg(operation_id) AND kind=sqlc.arg(kind) AND generation=sqlc.arg(generation) AND producer=sqlc.arg(producer);

-- name: AppendCoreBootstrapAttempt :exec
INSERT INTO core_bootstrap_attempts(tenant_id,operation_id,kind,generation,attempt_number,lease_id,outcome,error_code,started_at,recovery_id)
VALUES(sqlc.arg(tenant_id),sqlc.arg(operation_id),sqlc.arg(kind),sqlc.arg(generation),sqlc.arg(attempt_number),sqlc.arg(lease_id),sqlc.arg(outcome),sqlc.arg(error_code),sqlc.arg(started_at),sqlc.narg(recovery_id));

-- name: FinishCoreBootstrapJob :exec
UPDATE core_bootstrap_jobs SET state=sqlc.arg(state),available_at=sqlc.arg(available_at),last_error=sqlc.arg(error_code),lease_id=NULL,lease_started_at=NULL,lease_until=NULL,updated_at=clock_timestamp()
WHERE tenant_id=sqlc.arg(tenant_id) AND operation_id=sqlc.arg(operation_id) AND kind=sqlc.arg(kind) AND generation=sqlc.arg(generation) AND producer=sqlc.arg(producer);

-- name: MarkCoreBootstrapInvitationAttention :execrows
UPDATE tenant_bootstrap_operations o SET status='attention_required',version=version+1,updated_at=sqlc.arg(now)
WHERE o.tenant_id=sqlc.arg(tenant_id) AND o.id=sqlc.arg(operation_id) AND o.source_kind='core' AND o.status='waiting_for_principal_verification'
AND o.version=sqlc.arg(expected_version) AND o.superseded_by IS NULL
AND EXISTS(SELECT 1 FROM tenant_invitations i WHERE i.tenant_id=o.tenant_id AND i.bootstrap_operation_id=o.id AND i.id=sqlc.arg(invitation_id)
 AND i.status='expired' AND i.expires_at<=clock_timestamp());
