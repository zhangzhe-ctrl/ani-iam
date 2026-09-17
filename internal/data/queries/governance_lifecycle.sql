-- name: InitializeTenantLifecyclePipeline :exec
INSERT INTO tenant_lifecycle_pipelines(producer) VALUES(sqlc.arg(producer)) ON CONFLICT DO NOTHING;

-- name: LockTenantLifecyclePipeline :one
SELECT * FROM tenant_lifecycle_pipelines WHERE producer=sqlc.arg(producer) FOR UPDATE;

-- name: ReadTenantIntegrationReceipt :one
SELECT * FROM tenant_integration_receipts WHERE producer=sqlc.arg(producer) AND event_id=sqlc.arg(event_id);

-- name: ReadTenantLifecyclePosition :one
SELECT * FROM tenant_integration_receipts WHERE producer=sqlc.arg(producer) AND epoch=sqlc.arg(epoch)
 AND source_sequence=sqlc.arg(source_sequence) AND kind='lifecycle';

-- name: AppendTenantIntegrationReceipt :exec
INSERT INTO tenant_integration_receipts(producer,event_id,epoch,kind,source_sequence,tenant_id,raw_payload,raw_sha256,domain_sha256,occurred_at,outcome,
 tenant_version,business_status,reason,effective_at,committed_sequence,published_sequence,bootstrap_operation_id)
VALUES(sqlc.arg(producer),sqlc.arg(event_id),sqlc.arg(epoch),sqlc.arg(kind),sqlc.arg(source_sequence),sqlc.narg(tenant_id),
 sqlc.arg(raw_payload),sqlc.arg(raw_sha256),sqlc.arg(domain_sha256),sqlc.arg(occurred_at),sqlc.arg(outcome),sqlc.narg(tenant_version),sqlc.narg(business_status),sqlc.narg(reason),sqlc.narg(effective_at),
 sqlc.narg(committed_sequence),sqlc.narg(published_sequence),sqlc.narg(bootstrap_operation_id));

-- name: ReadTenantLifecycleFact :one
SELECT * FROM tenant_lifecycle_facts WHERE producer=sqlc.arg(producer) AND generation_id=sqlc.arg(generation_id) AND tenant_id=sqlc.arg(tenant_id);

-- name: WriteTenantLifecycleFact :exec
INSERT INTO tenant_lifecycle_facts(producer,generation_id,tenant_id,tenant_version,business_status,reason,effective_at,snapshot_base)
VALUES(sqlc.arg(producer),sqlc.arg(generation_id),sqlc.arg(tenant_id),sqlc.arg(tenant_version),sqlc.arg(business_status),sqlc.arg(reason),sqlc.arg(effective_at),sqlc.arg(snapshot_base))
ON CONFLICT(producer,generation_id,tenant_id) DO UPDATE SET tenant_version=EXCLUDED.tenant_version,business_status=EXCLUDED.business_status,
 reason=EXCLUDED.reason,effective_at=EXCLUDED.effective_at,snapshot_base=EXCLUDED.snapshot_base;

-- name: AdvanceTenantLifecyclePipeline :exec
UPDATE tenant_lifecycle_pipelines SET applied_sequence=sqlc.arg(applied_sequence),highest_sequence=sqlc.arg(highest_sequence),snapshot_required=sqlc.arg(snapshot_required),
 heartbeat_id=sqlc.narg(heartbeat_id),heartbeat_epoch=sqlc.narg(heartbeat_epoch),committed_sequence=sqlc.arg(committed_sequence),published_sequence=sqlc.arg(published_sequence),
 heartbeat_observed_at=sqlc.narg(heartbeat_observed_at),heartbeat_received_at=sqlc.narg(heartbeat_received_at),updated_at=clock_timestamp()
WHERE producer=sqlc.arg(producer);

-- name: ReadTenantLifecycleClock :one
SELECT clock_timestamp()::timestamptz AS received_at;

-- name: ReadTenantLifecycleProjection :one
SELECT * FROM tenant_current_lifecycle_facts WHERE producer=sqlc.arg(producer) AND tenant_id=sqlc.arg(tenant_id);

-- name: CreateTenantLifecycleGeneration :exec
INSERT INTO tenant_lifecycle_generations(producer,id,epoch,snapshot_watermark,snapshot_id,captured_at)
VALUES(sqlc.arg(producer),sqlc.arg(id),sqlc.arg(epoch),sqlc.arg(snapshot_watermark),sqlc.arg(snapshot_id),sqlc.arg(captured_at));
