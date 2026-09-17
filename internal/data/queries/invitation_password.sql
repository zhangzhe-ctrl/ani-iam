-- name: ReadInvitationPassword :one
SELECT p.id AS principal_id,p.status AS principal_status,i.id AS identity_id,(i.status='active')::boolean AS identity_active,
e.normalized_email,c.password_hash,c.version AS credential_version,c.locked_until
FROM verified_emails e JOIN principals p ON p.id=e.principal_id AND p.principal_type='human'
JOIN password_credentials c ON c.principal_id=p.id
JOIN identities i ON i.id=c.identity_id AND i.principal_id=p.id AND i.provider='password' AND i.issuer='ani-local'
WHERE e.normalized_email=sqlc.arg(account);

-- name: LockInvitationPassword :one
SELECT p.id AS principal_id,p.status AS principal_status,i.id AS identity_id,(i.status='active')::boolean AS identity_active,
e.normalized_email,c.password_hash,c.version AS credential_version,c.locked_until
FROM verified_emails e JOIN principals p ON p.id=e.principal_id AND p.principal_type='human'
JOIN password_credentials c ON c.principal_id=p.id
JOIN identities i ON i.id=c.identity_id AND i.principal_id=p.id AND i.provider='password' AND i.issuer='ani-local'
WHERE e.normalized_email=sqlc.arg(account) FOR UPDATE OF c FOR SHARE OF p,i;

-- name: AppendInvitationPasswordDenial :exec
INSERT INTO iam_audit_events(event_id,authentication_method,boundary,action,target_type,target_id,target_version,result,reason,request_id,correlation_id,decision_id,source_service,occurred_at,recorded_at,caller_principal_id,caller_binding_id,caller_binding_version,caller_grant_version)
VALUES(sqlc.arg(id),'anonymous','principal','iam.invitation.authentication.denied','principal',sqlc.arg(target_id),sqlc.arg(version),'denied','CREDENTIAL_INVALID',sqlc.arg(request_id),sqlc.arg(correlation_id),sqlc.arg(decision_id),'ani-iam',sqlc.arg(now),sqlc.arg(now),sqlc.arg(caller_id),sqlc.arg(binding_id),sqlc.arg(binding_version),sqlc.arg(grant_version));
