-- name: LockCoreBrokerAdministration :exec
SELECT pg_advisory_xact_lock(hashtextextended('ani-iam:core-broker-authority',0));

-- name: ReadCoreBrokerAdministrationReceipt :one
SELECT manifest_sha256, completed_at FROM core_broker_administration_receipts WHERE id=sqlc.arg(id);

-- name: AppendCoreBrokerAdministrationReceipt :exec
INSERT INTO core_broker_administration_receipts(id,manifest_sha256,manifest,mode,reason,provisioner_role)
VALUES(sqlc.arg(id),sqlc.arg(manifest_sha256),sqlc.arg(manifest),sqlc.arg(mode),sqlc.arg(reason),current_user);

-- name: ReadCoreBrokerProvisionedPrincipal :one
SELECT p.id FROM principals p JOIN workload_principals w ON w.principal_id=p.id
WHERE p.id=sqlc.arg(principal_id) AND p.principal_type='workload' AND p.status='active'
 AND w.owner_type='platform' AND w.environment=sqlc.arg(environment) AND w.trust_domain=sqlc.arg(trust_domain);

-- name: RegisterCoreBrokerBinding :exec
INSERT INTO core_broker_bindings(id,principal_id,environment,trust_domain,broker_name,account_name,nkey_public,status,version)
VALUES(sqlc.arg(id),sqlc.arg(principal_id),sqlc.arg(environment),sqlc.arg(trust_domain),sqlc.arg(broker_name),sqlc.arg(account_name),sqlc.arg(nkey_public),'active',1);

-- name: RegisterCoreBrokerRoute :exec
INSERT INTO core_broker_routes(id,broker_name,account_name,stream_name,subject,schema_major,producer,producer_binding_id,target_sha256,status,version)
VALUES(sqlc.arg(id),sqlc.arg(broker_name),sqlc.arg(account_name),sqlc.arg(stream_name),sqlc.arg(subject),1,sqlc.arg(producer),sqlc.arg(producer_binding_id),sqlc.arg(target_sha256),'active',1);

-- name: ReadCoreBrokerRegisteredBinding :one
SELECT * FROM core_broker_bindings WHERE id=sqlc.arg(id);

-- name: ReadCoreBrokerRegisteredRoute :one
SELECT * FROM core_broker_routes WHERE id=sqlc.arg(id);

-- name: AuthorizeCoreBrokerGrant :exec
INSERT INTO core_broker_grants(id,principal_id,binding_id,route_id,action,status,version)
VALUES(sqlc.arg(id),sqlc.arg(principal_id),sqlc.arg(binding_id),sqlc.arg(route_id),sqlc.arg(action),'active',1);

-- name: ChangeCoreBrokerGrant :execrows
UPDATE core_broker_grants SET status=sqlc.arg(status),version=version+1,updated_at=clock_timestamp()
WHERE id=sqlc.arg(id) AND principal_id=sqlc.arg(principal_id) AND binding_id=sqlc.arg(binding_id)
 AND route_id=sqlc.arg(route_id) AND action=sqlc.arg(action) AND version=sqlc.arg(expected_version) AND status<>sqlc.arg(status);

-- name: ChangeCoreBrokerBinding :execrows
UPDATE core_broker_bindings SET status=sqlc.arg(status),version=version+1,updated_at=clock_timestamp()
WHERE id=sqlc.arg(id) AND principal_id=sqlc.arg(principal_id) AND version=sqlc.arg(expected_version)
 AND status<>sqlc.arg(status) AND environment=sqlc.arg(environment) AND trust_domain=sqlc.arg(trust_domain)
 AND broker_name=sqlc.arg(broker_name) AND account_name=sqlc.arg(account_name);
