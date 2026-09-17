-- name: ReadCoreSnapshotCut :one
SELECT snapshot_source_cut FROM core_lifecycle_generations WHERE producer=sqlc.arg(producer) AND id=sqlc.arg(generation_id);

-- name: BeginCoreSnapshotGeneration :exec
INSERT INTO core_lifecycle_generations(producer,id,snapshot_source_cut) VALUES(sqlc.arg(producer),sqlc.arg(generation_id),sqlc.arg(source_cut));

-- name: BeginCoreSnapshotRebuild :exec
INSERT INTO core_lifecycle_rebuilds(producer,snapshot_id,generation_id,base_generation_id,consumer_id,source_cut,broker_after,page_size,expires_at,applied_through,broker_authority_sha256)
VALUES(sqlc.arg(producer),sqlc.arg(snapshot_id),sqlc.arg(generation_id),sqlc.arg(base_generation_id),sqlc.arg(consumer_id),sqlc.arg(source_cut),sqlc.arg(broker_after),sqlc.arg(page_size),sqlc.arg(expires_at),sqlc.arg(source_cut),sqlc.arg(broker_authority_sha256));

-- name: LockCoreSnapshotRebuild :one
SELECT *,expires_at>clock_timestamp() AS unexpired FROM core_lifecycle_rebuilds
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) FOR UPDATE;

-- name: GetCoreSnapshotPageReceipt :one
SELECT page_fingerprint FROM core_lifecycle_rebuild_pages WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) AND request_token=sqlc.arg(request_token);

-- name: InsertCoreSnapshotFact :exec
INSERT INTO core_lifecycle_projection_rows(producer,generation_id,tenant_id,lifecycle_version,required_version,status,reason,repair_required,snapshot_base)
VALUES(sqlc.arg(producer),sqlc.arg(generation_id),sqlc.arg(tenant_id),sqlc.arg(version),sqlc.arg(version),sqlc.arg(status),'',false,true);

-- name: RecordCoreSnapshotPage :exec
INSERT INTO core_lifecycle_rebuild_pages(producer,snapshot_id,request_token,page_fingerprint,next_token,item_count)
VALUES(sqlc.arg(producer),sqlc.arg(snapshot_id),sqlc.arg(request_token),sqlc.arg(page_fingerprint),sqlc.arg(next_token),sqlc.arg(item_count));

-- name: AdvanceCoreSnapshotPage :exec
UPDATE core_lifecycle_rebuilds SET next_token=sqlc.arg(next_token),last_tenant_id=sqlc.narg(last_tenant_id),loaded_items=loaded_items+sqlc.arg(item_count)::bigint,
 state=CASE WHEN sqlc.arg(next_token)::text='' THEN 'loaded' ELSE 'loading' END
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: ListCoreSnapshotIncrements :many
SELECT * FROM core_integration_receipts WHERE producer=sqlc.arg(producer) AND kind='lifecycle'
 AND source_sequence>sqlc.arg(after_sequence) AND source_sequence<=sqlc.arg(through_sequence)
ORDER BY source_sequence LIMIT 500;

-- name: AdvanceCoreSnapshotReplay :exec
UPDATE core_lifecycle_rebuilds SET applied_through=sqlc.arg(applied_through),state='catching_up'
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: CoreSnapshotCoversCurrent :one
SELECT NOT EXISTS(
 SELECT 1 FROM core_lifecycle_projection_rows old LEFT JOIN core_lifecycle_projection_rows replacement
 ON replacement.producer=old.producer AND replacement.tenant_id=old.tenant_id AND replacement.generation_id=sqlc.arg(new_generation)
 WHERE old.producer=sqlc.arg(producer) AND old.generation_id=sqlc.arg(old_generation)
 AND (replacement.tenant_id IS NULL OR replacement.lifecycle_version<old.required_version OR replacement.repair_required
 OR (old.lifecycle_version=replacement.lifecycle_version AND old.status<>replacement.status)
 OR (old.status='disabled' AND replacement.status<>'disabled'))
) AND NOT EXISTS(SELECT 1 FROM core_lifecycle_projection_rows r WHERE r.producer=sqlc.arg(producer) AND r.generation_id=sqlc.arg(new_generation) AND (r.repair_required OR r.lifecycle_version=0)) AS covered;

-- name: ActivateCoreSnapshotShadow :exec
UPDATE core_lifecycle_pipelines SET generation_id=sqlc.arg(generation_id),highest_sequence=GREATEST(highest_sequence,sqlc.arg(source_cut)::bigint) WHERE producer=sqlc.arg(producer);

-- name: MarkCoreSnapshotActivated :exec
UPDATE core_lifecycle_rebuilds SET state='activated' WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id);

-- name: CoreSnapshotCursorUnexpired :one
SELECT sqlc.arg(expires_at)::timestamptz>clock_timestamp() AS unexpired;

-- name: AbandonUnavailableCoreSnapshots :exec
UPDATE core_lifecycle_rebuilds SET state='abandoned',abandoned_reason=CASE
 WHEN base_generation_id<>sqlc.arg(generation_id)::uuid THEN 'superseded_generation'
 WHEN state='loading' AND expires_at<=clock_timestamp() THEN 'cursor_expired' ELSE 'authority_changed' END
WHERE producer=sqlc.arg(producer) AND state IN ('loading','loaded','catching_up')
 AND (base_generation_id<>sqlc.arg(generation_id)::uuid OR (state='loading' AND expires_at<=clock_timestamp())
 OR (sqlc.arg(current_authority_sha256)::text<>'' AND broker_authority_sha256<>sqlc.arg(current_authority_sha256)));

-- name: AbandonCoreSnapshotAuthority :exec
UPDATE core_lifecycle_rebuilds SET state='abandoned',abandoned_reason='authority_changed'
WHERE producer=sqlc.arg(producer) AND snapshot_id=sqlc.arg(snapshot_id) AND state IN ('loading','loaded','catching_up');

-- name: ReadPendingCoreSnapshot :one
SELECT * FROM core_lifecycle_rebuilds WHERE producer=sqlc.arg(producer)
 AND state IN ('loading','loaded','catching_up') ORDER BY created_at,snapshot_id LIMIT 1;

-- name: CoreSnapshotRecoveryNeeded :one
SELECT EXISTS(SELECT 1 FROM core_lifecycle_projection_rows r WHERE r.producer=sqlc.arg(producer)
 AND r.generation_id=sqlc.arg(generation_id) AND r.repair_required)
 OR NOT COALESCE((SELECT b.state='activated' AND b.created_at>clock_timestamp()-interval '1 hour'
 FROM core_lifecycle_rebuilds b WHERE b.producer=sqlc.arg(producer)
 ORDER BY b.created_at DESC,b.snapshot_id DESC LIMIT 1),false) AS needed;
