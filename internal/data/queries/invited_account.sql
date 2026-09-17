-- name: InvitedAccountTenantBoundaries :many
SELECT DISTINCT tenant_id FROM tenant_invitations WHERE normalized_email=sqlc.arg(email) AND status='pending' AND expires_at>statement_timestamp() ORDER BY tenant_id;

-- name: InvitedAccountEligibility :one
SELECT NOT EXISTS(SELECT 1 FROM verified_emails e WHERE e.normalized_email=sqlc.arg(email))
 AND NOT EXISTS(SELECT 1 FROM identities p WHERE p.provider='password' AND p.issuer='ani-local' AND p.subject=sqlc.arg(email))
 AND (EXISTS(SELECT 1 FROM tenant_invitations t WHERE t.tenant_id=ANY(sqlc.arg(tenant_ids)::uuid[]) AND t.normalized_email=sqlc.arg(email) AND t.status='pending' AND t.expires_at>statement_timestamp())
 OR EXISTS(SELECT 1 FROM platform_invitations p WHERE p.normalized_email=sqlc.arg(email) AND p.status='pending' AND p.expires_at>statement_timestamp())) AS eligible;

-- name: FindInvitedAccountRequest :one
SELECT * FROM iam_invited_account_verifications WHERE request_idempotency_key=sqlc.arg(key) FOR UPDATE;

-- name: GetInvitedAccountVerification :one
SELECT * FROM iam_invited_account_verifications WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: SupersedeInvitedAccountVerifications :exec
UPDATE iam_invited_account_verifications SET status='superseded',version=version+1,updated_at=sqlc.arg(now)
WHERE account_digest=sqlc.arg(account_digest) AND status='pending';

-- name: CancelInactiveInvitedAccountDeliveries :exec
UPDATE iam_invited_account_outbox o SET status='cancelled',payload_key_version=NULL,payload_ciphertext=NULL,claimed_at=NULL,version=version+1,updated_at=sqlc.arg(now)
WHERE o.status IN ('pending','claimed','attention_required')
AND EXISTS(SELECT 1 FROM iam_invited_account_verifications c WHERE c.id=o.challenge_id AND c.account_digest=sqlc.arg(account_digest) AND c.status<>'pending');

-- name: CreateInvitedAccountVerification :exec
INSERT INTO iam_invited_account_verifications(id,account_digest,normalized_email,code_key_version,code_digest,caller_principal_id,request_idempotency_key,status,failed_attempts,version,created_at,updated_at,expires_at)
VALUES(sqlc.arg(id),sqlc.arg(account_digest),sqlc.narg(email),sqlc.narg(key_version),sqlc.narg(code_digest),sqlc.arg(caller_principal_id),sqlc.arg(key),'pending',0,1,sqlc.arg(now),sqlc.arg(now),sqlc.arg(expires_at));

-- name: CreateInvitedAccountDelivery :exec
INSERT INTO iam_invited_account_outbox(id,challenge_id,payload_key_version,payload_ciphertext,status,available_at,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(challenge_id),sqlc.arg(key_version),sqlc.arg(ciphertext),'pending',sqlc.arg(now),1,sqlc.arg(now),sqlc.arg(now));

-- name: FailInvitedAccountVerification :execrows
UPDATE iam_invited_account_verifications SET failed_attempts=failed_attempts+1,status=CASE WHEN failed_attempts=4 THEN 'exhausted' ELSE 'pending' END,version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='pending' AND failed_attempts<5;

-- name: ExpireInvitedAccountVerification :execrows
UPDATE iam_invited_account_verifications SET status='expired',version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='pending' AND expires_at<=statement_timestamp();

-- name: ConsumeInvitedAccountVerification :execrows
UPDATE iam_invited_account_verifications SET status='consumed',principal_id=sqlc.arg(principal_id),completion_key=sqlc.arg(key),completion_intent=sqlc.arg(intent),version=version+1,updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND version=sqlc.arg(expected_version) AND status='pending' AND failed_attempts<5 AND expires_at>sqlc.arg(now) AND expires_at>statement_timestamp();

-- name: CreateInvitedHuman :exec
INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES(sqlc.arg(id),'human','active',1,sqlc.arg(now),sqlc.arg(now));

-- name: CreateInvitedVerifiedEmail :exec
INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at) VALUES(sqlc.arg(principal_id),sqlc.arg(email),sqlc.arg(now),sqlc.arg(now),sqlc.arg(now));

-- name: AppendInvitedAccountAudit :exec
INSERT INTO iam_audit_events(tenant_id,event_id,actor_id,authentication_method,boundary,action,target_type,target_id,target_version,result,reason,request_id,correlation_id,decision_id,source_service,occurred_at,recorded_at,caller_principal_id,caller_binding_id,caller_binding_version,caller_grant_version)
VALUES(NULL,sqlc.arg(id),sqlc.narg(actor_id),sqlc.arg(authentication_method),'principal',sqlc.arg(action),'invited_account_verification',sqlc.arg(challenge_id),sqlc.arg(version),sqlc.arg(result),sqlc.arg(reason),sqlc.arg(request_id),sqlc.arg(correlation_id),sqlc.arg(decision_id),'ani-iam',sqlc.arg(now),sqlc.arg(now),sqlc.arg(caller_id),sqlc.arg(binding_id),sqlc.arg(binding_version),sqlc.arg(grant_version));
