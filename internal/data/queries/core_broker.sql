-- name: LockCoreBrokerAuthority :exec
SELECT pg_advisory_xact_lock_shared(hashtextextended('ani-iam:core-broker-authority',0));

-- name: ReadCurrentCoreBrokerGrant :one
SELECT p.id AS principal_id,b.id AS binding_id,g.id AS grant_id,
       p.version AS principal_version,b.version AS binding_version,g.version AS grant_version,
       r.version AS route_version,r.producer,r.producer_binding_id,r.target_sha256
FROM core_broker_grants g
JOIN core_broker_bindings b ON b.id=g.binding_id AND b.principal_id=g.principal_id
JOIN principals p ON p.id=b.principal_id
JOIN workload_principals w ON w.principal_id=p.id
JOIN core_broker_routes r ON r.id=g.route_id
WHERE b.id=sqlc.arg(binding_id) AND r.id=sqlc.arg(route_id) AND g.action=sqlc.arg(action)
  AND b.nkey_public=sqlc.arg(nkey_public)
  AND b.environment=sqlc.arg(environment) AND b.trust_domain=sqlc.arg(trust_domain)
  AND w.environment=b.environment AND w.trust_domain=b.trust_domain AND w.owner_type='platform'
  AND p.principal_type='workload' AND p.status='active'
  AND b.status='active' AND g.status='active' AND r.status='active'
  AND b.broker_name=sqlc.arg(broker_name) AND b.account_name=sqlc.arg(account_name)
  AND r.broker_name=b.broker_name AND r.account_name=b.account_name
  AND r.stream_name=sqlc.arg(stream_name) AND r.subject=sqlc.arg(subject) AND r.schema_major=sqlc.arg(schema_major)
  AND r.target_sha256=sqlc.arg(target_sha256)
  AND (g.action<>'publish' OR r.producer_binding_id=b.id);

-- name: AppendCoreBrokerAuthorityReceipt :exec
INSERT INTO tenant_broker_authority_receipts(
    consumer_id,broker_sequence,route_id,route_version,producer,source_sequence,epoch,event_id,tenant_id,raw_sha256,
    producer_principal_id,producer_binding_id,producer_principal_version,producer_binding_version,producer_grant_id,producer_grant_version,
    executor_principal_id,executor_binding_id,executor_principal_version,executor_binding_version,executor_grant_id,executor_grant_version,
    execution_grant_id,execution_grant_version)
VALUES(sqlc.arg(consumer_id),sqlc.arg(broker_sequence),sqlc.arg(route_id),sqlc.arg(route_version),sqlc.arg(producer),sqlc.arg(source_sequence),sqlc.arg(epoch),sqlc.arg(event_id),sqlc.narg(tenant_id),sqlc.arg(raw_sha256),
    sqlc.arg(producer_principal_id),sqlc.arg(producer_binding_id),sqlc.arg(producer_principal_version),sqlc.arg(producer_binding_version),sqlc.arg(producer_grant_id),sqlc.arg(producer_grant_version),
    sqlc.arg(executor_principal_id),sqlc.arg(executor_binding_id),sqlc.arg(executor_principal_version),sqlc.arg(executor_binding_version),sqlc.arg(executor_grant_id),sqlc.arg(executor_grant_version),
    sqlc.narg(execution_grant_id),sqlc.narg(execution_grant_version));

-- name: ReadCoreBrokerAuthorityReceipt :one
SELECT * FROM tenant_broker_authority_receipts
WHERE consumer_id=sqlc.arg(consumer_id) AND broker_sequence=sqlc.arg(broker_sequence);

-- name: ReadCoreBootstrapBrokerAuthority :one
SELECT a.* FROM tenant_broker_authority_receipts a
JOIN core_bootstrap_receipts r ON r.tenant_id=a.tenant_id AND r.event_id=a.event_id
WHERE a.consumer_id=sqlc.arg(consumer_id) AND a.tenant_id=sqlc.arg(tenant_id) AND a.event_id=sqlc.arg(event_id)
  AND r.operation_id=sqlc.arg(operation_id) AND r.payload_fingerprint=sqlc.arg(payload_fingerprint) AND a.producer=sqlc.arg(producer) AND a.source_sequence=sqlc.arg(source_sequence)
ORDER BY a.broker_sequence LIMIT 1;

-- name: ReadCoreBrokerDLQPosition :one
SELECT * FROM core_broker_dlq
WHERE consumer_id=sqlc.arg(consumer_id) AND broker_sequence=sqlc.arg(broker_sequence);

-- name: AppendCoreBrokerDLQ :exec
INSERT INTO core_broker_dlq(consumer_id,id,broker_sequence,broker_name,account_name,stream_name,subject,raw_payload,raw_sha256,headers,delivery_count,published_at,last_error)
VALUES(sqlc.arg(consumer_id),sqlc.arg(id),sqlc.arg(broker_sequence),sqlc.arg(broker_name),sqlc.arg(account_name),sqlc.arg(stream_name),sqlc.arg(subject),sqlc.arg(raw_payload),sqlc.arg(raw_sha256),sqlc.arg(headers),sqlc.arg(delivery_count),sqlc.arg(published_at),sqlc.arg(last_error));

-- name: ReadCoreBrokerEventAuthority :one
SELECT * FROM tenant_broker_authority_receipts
WHERE consumer_id=sqlc.arg(consumer_id) AND event_id=sqlc.arg(event_id)
  AND tenant_id IS NOT DISTINCT FROM sqlc.narg(tenant_id)
ORDER BY broker_sequence LIMIT 1;
