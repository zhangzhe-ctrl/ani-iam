-- name: GetCoreBootstrapReceipt :one
SELECT * FROM core_bootstrap_receipts
WHERE tenant_id=sqlc.arg(tenant_id) AND event_id=sqlc.arg(event_id);

-- name: CreateCoreBootstrapOperation :execrows
INSERT INTO tenant_bootstrap_operations(tenant_id,id,source_kind,intended_email,payload_fingerprint,payload,status,version,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),'core',sqlc.arg(intended_email),sqlc.arg(payload_fingerprint),sqlc.arg(payload),'pending',1,statement_timestamp(),statement_timestamp())
ON CONFLICT DO NOTHING;

-- name: GetCoreBootstrapOperation :one
SELECT * FROM tenant_bootstrap_operations
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) FOR UPDATE;

-- name: AppendCoreBootstrapReceipt :one
INSERT INTO core_bootstrap_receipts(tenant_id,event_id,operation_id,producer,source_sequence,source_epoch,payload_fingerprint,raw_payload,raw_sha256,occurred_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(event_id),sqlc.arg(operation_id),sqlc.arg(producer),sqlc.arg(source_sequence),sqlc.narg(source_epoch),sqlc.arg(payload_fingerprint),sqlc.arg(raw_payload),sqlc.arg(raw_sha256),sqlc.arg(occurred_at))
RETURNING received_at;
