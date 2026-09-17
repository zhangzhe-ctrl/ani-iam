-- name: AppendCoreDLQContext :exec
INSERT INTO core_broker_dlq_context(consumer_id,entry_id,configuration,configuration_sha256)
VALUES(sqlc.arg(consumer_id),sqlc.arg(entry_id),sqlc.arg(configuration),sqlc.arg(configuration_sha256));

-- name: ReadCoreDLQContext :one
SELECT * FROM core_broker_dlq_context WHERE consumer_id=sqlc.arg(consumer_id) AND entry_id=sqlc.arg(entry_id);

-- name: ReadCoreDLQEntry :one
SELECT d.*,COALESCE((SELECT max(a.attempt_number) FROM core_broker_dlq_attempts a WHERE a.consumer_id=d.consumer_id AND a.entry_id=d.id),0)::bigint AS last_attempt
FROM core_broker_dlq d WHERE d.consumer_id=sqlc.arg(consumer_id) AND d.id=sqlc.arg(entry_id);

-- name: ListCoreDLQEntries :many
SELECT d.id FROM core_broker_dlq d WHERE d.consumer_id=sqlc.arg(consumer_id) AND d.id>sqlc.arg(after_id)
ORDER BY d.id LIMIT sqlc.arg(page_limit);

-- name: ListCoreDLQAttempts :many
SELECT * FROM core_broker_dlq_attempts WHERE consumer_id=sqlc.arg(consumer_id) AND entry_id=sqlc.arg(entry_id)
 AND attempt_number>sqlc.arg(after_attempt) ORDER BY attempt_number LIMIT sqlc.arg(page_limit);

-- name: AppendCoreDLQAttempt :exec
INSERT INTO core_broker_dlq_attempts(consumer_id,entry_id,id,attempt_number,raw_sha256,request_hash,actor_id,reason_code,outcome,projection_outcome,error_code,authority_sha256,audit_event_id)
VALUES(sqlc.arg(consumer_id),sqlc.arg(entry_id),sqlc.arg(id),sqlc.arg(attempt_number),sqlc.arg(raw_sha256),sqlc.arg(request_hash),sqlc.arg(actor_id),sqlc.arg(reason_code),sqlc.arg(outcome),sqlc.arg(projection_outcome),sqlc.arg(error_code),sqlc.arg(authority_sha256),sqlc.arg(audit_event_id));

-- name: LockCoreDLQHumanAuthority :one
SELECT p.id FROM platform_session_grants g
JOIN sessions s ON s.id=g.session_id AND s.principal_id=g.principal_id AND s.audience='boss'
JOIN platform_memberships m ON m.id=g.membership_id AND m.principal_id=g.principal_id
JOIN principals p ON p.id=g.principal_id AND p.principal_type='human'
WHERE p.id=sqlc.arg(principal_id) AND s.id=sqlc.arg(session_id) AND g.id=sqlc.arg(grant_id)
FOR SHARE OF p,s,g,m;
