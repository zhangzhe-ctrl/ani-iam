-- name: ReadPendingTenantSnapshot :one
SELECT * FROM tenant_snapshot_rebuilds WHERE producer=sqlc.arg(producer) AND state IN ('loading','loaded','catching_up') ORDER BY created_at,snapshot_id LIMIT 1;

-- name: LockTenantSnapshot :one
SELECT * FROM tenant_snapshot_rebuilds WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) FOR UPDATE;

-- name: BeginTenantSnapshot :exec
INSERT INTO tenant_snapshot_rebuilds(producer,snapshot_id,generation_id,base_generation_id,epoch,reader_id,reader_binding_id,watermark,total_count,page_size,captured_at,expires_at,first_token,next_token,applied_through,authority_sha256)
VALUES(sqlc.arg(producer),sqlc.arg(snapshot_id),sqlc.arg(generation_id),sqlc.narg(base_generation_id),sqlc.arg(epoch),sqlc.arg(reader_id),sqlc.arg(reader_binding_id),sqlc.arg(watermark),sqlc.arg(total_count),sqlc.arg(page_size),sqlc.arg(captured_at),sqlc.arg(expires_at),sqlc.arg(first_token),sqlc.arg(first_token),sqlc.arg(watermark),sqlc.arg(authority_sha256));

-- name: AbandonTenantSnapshot :exec
UPDATE tenant_snapshot_rebuilds SET state='abandoned',abandoned_reason=sqlc.arg(reason),updated_at=clock_timestamp()
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) AND state IN ('loading','loaded','catching_up');

-- name: ReadTenantSnapshotPage :one
SELECT * FROM tenant_snapshot_pages WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) AND request_token=sqlc.arg(request_token);

-- name: InsertTenantSnapshotFact :exec
INSERT INTO tenant_lifecycle_facts(producer,generation_id,tenant_id,tenant_version,business_status,snapshot_base)
VALUES(sqlc.arg(producer),sqlc.arg(generation_id),sqlc.arg(tenant_id),sqlc.arg(tenant_version),sqlc.arg(business_status),true);

-- name: AppendTenantSnapshotPage :exec
INSERT INTO tenant_snapshot_pages(producer,snapshot_id,request_token,page_sha256,page_payload,next_token,item_count)
VALUES(sqlc.arg(producer),sqlc.arg(snapshot_id),sqlc.arg(request_token),sqlc.arg(page_sha256),sqlc.arg(page_payload),sqlc.arg(next_token),sqlc.arg(item_count));

-- name: AdvanceTenantSnapshotPage :exec
UPDATE tenant_snapshot_rebuilds SET next_token=sqlc.arg(next_token),last_tenant_id=sqlc.narg(last_tenant_id),loaded_items=loaded_items+sqlc.arg(item_count)::bigint,
 state=CASE WHEN sqlc.arg(next_token)::text='' THEN 'loaded' ELSE 'loading' END,updated_at=clock_timestamp()
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: ListTenantSnapshotIncrements :many
SELECT * FROM tenant_integration_receipts WHERE producer=sqlc.arg(producer) AND epoch=sqlc.arg(epoch) AND kind='lifecycle' AND source_sequence>sqlc.arg(after_sequence)
ORDER BY source_sequence LIMIT 500;

-- name: TenantSnapshotHighest :one
SELECT greatest(COALESCE(max(source_sequence) FILTER(WHERE kind='lifecycle'),0),COALESCE(max(committed_sequence) FILTER(WHERE kind='heartbeat'),0))::bigint AS highest
FROM tenant_integration_receipts WHERE producer=sqlc.arg(producer) AND epoch=sqlc.arg(epoch);

-- name: AdvanceTenantSnapshotReplay :exec
UPDATE tenant_snapshot_rebuilds SET applied_through=sqlc.arg(applied_through),state='catching_up',updated_at=clock_timestamp()
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: TenantSnapshotCoversCurrent :one
SELECT NOT EXISTS(
 SELECT 1 FROM tenant_lifecycle_facts old LEFT JOIN tenant_lifecycle_facts replacement
 ON replacement.producer=old.producer AND replacement.tenant_id=old.tenant_id AND replacement.generation_id=sqlc.arg(new_generation)
 WHERE old.producer=sqlc.arg(producer) AND old.generation_id=sqlc.narg(old_generation)
 AND (replacement.tenant_id IS NULL OR replacement.tenant_version<old.tenant_version
 OR (old.tenant_version=replacement.tenant_version AND old.business_status<>replacement.business_status)
 OR (old.business_status='disabled' AND replacement.business_status<>'disabled'))
)::boolean AS covered;

-- name: ActivateTenantSnapshot :exec
UPDATE tenant_lifecycle_pipelines SET generation_id=sqlc.arg(generation_id),epoch=sqlc.arg(epoch),applied_sequence=sqlc.arg(applied_sequence),highest_sequence=sqlc.arg(applied_sequence),snapshot_required=false,
 heartbeat_id=NULL,heartbeat_epoch=NULL,heartbeat_observed_at=NULL,heartbeat_received_at=NULL,committed_sequence=0,published_sequence=0,updated_at=clock_timestamp()
WHERE producer=sqlc.arg(producer);

-- name: MarkTenantSnapshotActivated :exec
UPDATE tenant_snapshot_rebuilds SET state='activated',updated_at=clock_timestamp() WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: ReadTenantLifecycleGeneration :one
SELECT * FROM tenant_lifecycle_generations WHERE producer=sqlc.arg(producer) AND id=sqlc.arg(id);
