-- name: AppendCoreBootstrapBrokerApproval :exec
INSERT INTO tenant_bootstrap_broker_approvals(tenant_id,id,consumer_id,broker_sequence,source_event_id,operation_id,kind,generation,authority_sha256,recovery_id,effect_audit_id,actor_id,reason_code)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(consumer_id),sqlc.arg(broker_sequence),sqlc.arg(source_event_id),sqlc.arg(operation_id),sqlc.arg(kind),sqlc.arg(generation),sqlc.arg(authority_sha256),sqlc.narg(recovery_id),sqlc.narg(effect_audit_id),sqlc.arg(actor_id),sqlc.arg(reason_code));

-- The current work loaded under the Tenant lock supplies kind/generation and
-- recovery identity; mutable caller input cannot broaden an approval.
-- name: HasCoreBootstrapBrokerApproval :one
SELECT EXISTS(SELECT 1 FROM tenant_bootstrap_broker_approvals a
 WHERE a.tenant_id=sqlc.arg(tenant_id) AND a.consumer_id=sqlc.arg(consumer_id)
 AND a.broker_sequence=sqlc.arg(broker_sequence) AND a.source_event_id=sqlc.arg(source_event_id)
 AND a.operation_id=sqlc.arg(operation_id) AND a.kind=sqlc.arg(kind) AND a.generation=sqlc.arg(generation)
 AND a.authority_sha256=sqlc.arg(authority_sha256) AND a.recovery_id IS NOT DISTINCT FROM sqlc.narg(recovery_id)) AS approved;
