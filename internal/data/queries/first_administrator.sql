-- name: LockFirstAdministrator :exec
SELECT pg_advisory_xact_lock(hashtextextended('first-administrator:' || sqlc.arg(environment)::text,0));

-- name: GetFirstAdministratorCompletion :one
SELECT intent_id FROM first_administrator_completions WHERE environment=sqlc.arg(environment);

-- name: GetFirstAdministratorIntent :one
SELECT * FROM first_administrator_intents WHERE intent_id=sqlc.arg(intent_id);

-- name: GetFirstAdministratorLeaf :one
SELECT i.* FROM first_administrator_intents i
WHERE i.environment=sqlc.arg(environment)
AND NOT EXISTS (SELECT 1 FROM first_administrator_intents s WHERE s.environment=i.environment AND s.supersedes=i.intent_id);

-- name: InsertFirstAdministratorIntent :exec
INSERT INTO first_administrator_intents(intent_id,environment,normalized_email,issuer,subject,intent_sha256,supersedes,registered_at,expires_at,audit_event_id)
VALUES(sqlc.arg(intent_id),sqlc.arg(environment),sqlc.arg(normalized_email),sqlc.arg(issuer),sqlc.arg(subject),sqlc.arg(intent_sha256),sqlc.narg(supersedes),sqlc.arg(registered_at),sqlc.arg(expires_at),sqlc.arg(audit_event_id));

-- name: InsertFirstAdministratorIntentAudit :exec
INSERT INTO iam_audit_events(event_id,authentication_method,boundary,action,target_type,target_id,target_version,result,reason,
request_id,correlation_id,decision_id,source_service,occurred_at,recorded_at,provisioner_role,first_administrator_intent_id)
VALUES(sqlc.arg(event_id),'administrator_provisioner','platform','administrator.intent.register','first_administrator_intent',sqlc.arg(intent_id),1,'succeeded','REVIEWED_INTENT_REGISTERED',
sqlc.arg(request_id),sqlc.arg(request_id),sqlc.arg(intent_digest),'ani-iam-provisioner',sqlc.arg(now),sqlc.arg(now),current_user,sqlc.arg(intent_id));
