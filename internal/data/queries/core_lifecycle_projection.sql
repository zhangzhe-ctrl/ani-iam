-- name: InitializeCoreProjectionPipeline :execrows
INSERT INTO core_lifecycle_pipelines(producer,generation_id)
VALUES(sqlc.arg(producer),sqlc.arg(generation_id)) ON CONFLICT(producer) DO NOTHING;

-- name: InitializeCoreProjectionGeneration :exec
INSERT INTO core_lifecycle_generations(producer,id) VALUES(sqlc.arg(producer),sqlc.arg(id));

-- name: LockCoreProjectionPipeline :one
SELECT * FROM core_lifecycle_pipelines WHERE producer=sqlc.arg(producer) FOR UPDATE;

-- name: GetCoreIntegrationReceipt :one
SELECT * FROM core_integration_receipts WHERE producer=sqlc.arg(producer) AND event_id=sqlc.arg(event_id);

-- name: GetCoreProjectionRow :one
SELECT * FROM core_lifecycle_projection_rows
WHERE producer=sqlc.arg(producer) AND generation_id=sqlc.arg(generation_id) AND tenant_id=sqlc.arg(tenant_id);

-- name: SaveCoreProjectionRow :exec
INSERT INTO core_lifecycle_projection_rows(producer,generation_id,tenant_id,lifecycle_version,required_version,status,reason,effective_at,repair_required,snapshot_base)
VALUES(sqlc.arg(producer),sqlc.arg(generation_id),sqlc.arg(tenant_id),sqlc.arg(lifecycle_version),sqlc.arg(required_version),sqlc.arg(status),sqlc.arg(reason),sqlc.narg(effective_at),sqlc.arg(repair_required),sqlc.arg(snapshot_base))
ON CONFLICT(producer,generation_id,tenant_id) DO UPDATE
SET lifecycle_version=EXCLUDED.lifecycle_version,required_version=EXCLUDED.required_version,status=EXCLUDED.status,reason=EXCLUDED.reason,effective_at=EXCLUDED.effective_at,repair_required=EXCLUDED.repair_required,snapshot_base=EXCLUDED.snapshot_base;

-- name: AppendCoreIntegrationReceipt :exec
INSERT INTO core_integration_receipts(producer,event_id,source_sequence,kind,tenant_id,raw_payload,raw_sha256,domain_fingerprint,occurred_at,outcome,lifecycle_version,lifecycle_status,lifecycle_reason,lifecycle_effective_at)
VALUES(sqlc.arg(producer),sqlc.arg(event_id),sqlc.arg(source_sequence),sqlc.arg(kind),sqlc.narg(tenant_id),sqlc.arg(raw_payload),sqlc.arg(raw_sha256),sqlc.arg(domain_fingerprint),sqlc.arg(occurred_at),sqlc.arg(outcome),sqlc.narg(lifecycle_version),sqlc.narg(lifecycle_status),sqlc.narg(lifecycle_reason),sqlc.narg(lifecycle_effective_at));

-- name: AdvanceCoreProjectionPipeline :one
UPDATE core_lifecycle_pipelines p SET
    highest_sequence=GREATEST(p.highest_sequence,sqlc.arg(source_sequence)::bigint),
    progress_at=CASE WHEN sqlc.arg(source_sequence)::bigint>p.contiguous_sequence
        THEN GREATEST(p.progress_at,LEAST(sqlc.arg(occurred_at)::timestamptz,clock_timestamp())) ELSE p.progress_at END,
    contiguous_sequence=CASE WHEN EXISTS(SELECT 1 FROM core_integration_receipts next
        WHERE next.producer=p.producer AND next.source_sequence=p.contiguous_sequence+1)
        THEN COALESCE((SELECT min(r.source_sequence) FROM core_integration_receipts r
            WHERE r.producer=p.producer AND r.source_sequence>p.contiguous_sequence
            AND r.source_sequence<GREATEST(p.highest_sequence,sqlc.arg(source_sequence)::bigint)
            AND NOT EXISTS(SELECT 1 FROM core_integration_receipts next WHERE next.producer=r.producer AND next.source_sequence=r.source_sequence+1)),
            GREATEST(p.highest_sequence,sqlc.arg(source_sequence)::bigint))
        ELSE p.contiguous_sequence END
WHERE p.producer=sqlc.arg(producer)
RETURNING contiguous_sequence,highest_sequence;

-- name: ReadCoreShadowTenant :one
SELECT producer,tenant_id,generation_id,epoch,applied_sequence,business_status AS status,tenant_version AS lifecycle_version,
 tenant_version AS required_version,reason,snapshot_base,effective_at,applied_sequence AS contiguous_sequence,highest_sequence,
 snapshot_required AS repair_required,(fresh_until>statement_timestamp()) AS fresh
FROM tenant_current_lifecycle_facts WHERE producer=sqlc.arg(producer) AND tenant_id=sqlc.arg(tenant_id);
