-- name: LockPlatformAdministrator :exec
SELECT singleton FROM platform_administrator_guard WHERE singleton FOR UPDATE;

-- name: LookupPlatformOIDC :one
SELECT i.id AS identity_id,(i.status='active')::boolean AS identity_active,p.id AS principal_id,p.status AS principal_status,
    COALESCE(m.id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS membership_id,
    COALESCE(m.status,'removed')::text AS membership_status,COALESCE(e.normalized_email,'')::text AS normalized_email
FROM identities i JOIN principals p ON p.id=i.principal_id AND p.principal_type='human'
LEFT JOIN verified_emails e ON e.principal_id=p.id
LEFT JOIN platform_memberships m ON m.principal_id=p.id AND m.status<>'removed'
WHERE i.provider=sqlc.arg(provider) AND i.issuer=sqlc.arg(issuer) AND i.subject=sqlc.arg(subject)
FOR SHARE OF i,p;

-- name: LookupFirstAdministratorCandidate :one
SELECT i.*,
    EXISTS(SELECT 1 FROM first_administrator_completions c WHERE c.environment=i.environment)::boolean AS completed,
    EXISTS(SELECT 1 FROM platform_roles r JOIN platform_role_bindings b ON b.role_id=r.id
      JOIN platform_memberships m ON m.id=b.membership_id JOIN principals p ON p.id=m.principal_id
      WHERE r.code='platform-admin' AND r.system_role AND m.status='active' AND p.principal_type='human' AND p.status='active')::boolean AS administrator_present
FROM first_administrator_intents i WHERE i.environment=sqlc.arg(environment)
AND NOT EXISTS(SELECT 1 FROM first_administrator_intents s WHERE s.environment=i.environment AND s.supersedes=i.intent_id);

-- name: PlatformBootstrapIdentityAvailable :one
SELECT (NOT EXISTS(SELECT 1 FROM verified_emails WHERE normalized_email=sqlc.arg(normalized_email))
AND NOT EXISTS(SELECT 1 FROM identities WHERE issuer=sqlc.arg(issuer) AND subject=sqlc.arg(subject)))::boolean AS available;

-- name: GetPlatformAdministratorRole :one
SELECT * FROM platform_roles WHERE code='platform-admin';

-- name: GetPlatformRolePermissionSet :many
SELECT scope,resource,action FROM platform_role_permissions WHERE role_id=sqlc.arg(role_id) ORDER BY scope,resource,action;

-- name: InsertPlatformAdministratorRole :exec
INSERT INTO platform_roles(id,code,display_name,system_role,system_definition_version,version,created_at,updated_at)
VALUES(sqlc.arg(id),'platform-admin',sqlc.arg(display_name),true,1,1,sqlc.arg(now),sqlc.arg(now));

-- name: InsertPlatformRolePermission :exec
INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at)
VALUES(sqlc.arg(role_id),'platform',sqlc.arg(resource),sqlc.arg(action),sqlc.arg(now));

-- name: CreatePlatformHuman :exec
INSERT INTO principals(id,principal_type,status,version,created_at,updated_at)
VALUES(sqlc.arg(id),'human','active',1,sqlc.arg(now),sqlc.arg(now));

-- name: CreatePlatformVerifiedEmail :exec
INSERT INTO verified_emails(principal_id,normalized_email,verified_at,created_at,updated_at)
VALUES(sqlc.arg(principal_id),sqlc.arg(normalized_email),sqlc.arg(now),sqlc.arg(now),sqlc.arg(now));

-- name: CreatePlatformMembership :exec
INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(principal_id),'active',1,sqlc.arg(now),sqlc.arg(now));

-- name: CreatePlatformRoleBinding :exec
INSERT INTO platform_role_bindings(id,membership_id,role_id,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(membership_id),sqlc.arg(role_id),1,sqlc.arg(now),sqlc.arg(now));

-- name: CompleteFirstAdministratorIntent :exec
INSERT INTO first_administrator_completions(environment,intent_id,principal_id,audit_event_id,completed_at)
VALUES(sqlc.arg(environment),sqlc.arg(intent_id),sqlc.arg(principal_id),sqlc.arg(audit_event_id),sqlc.arg(now));

-- name: CreatePlatformSessionGrant :exec
INSERT INTO platform_session_grants(id,session_id,principal_id,membership_id,status,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(session_id),sqlc.arg(principal_id),sqlc.arg(membership_id),'active',1,sqlc.arg(now),sqlc.arg(now));

-- name: CreatePlatformRefreshFamily :exec
INSERT INTO platform_refresh_token_families(id,grant_id,status,version,created_at,updated_at)
VALUES(sqlc.arg(id),sqlc.arg(grant_id),'active',1,sqlc.arg(now),sqlc.arg(now));

-- name: CreatePlatformRefreshToken :exec
INSERT INTO platform_refresh_tokens(id,family_id,digest,status,issued_at,expires_at)
VALUES(sqlc.arg(id),sqlc.arg(family_id),sqlc.arg(digest),'active',sqlc.arg(issued_at),sqlc.arg(expires_at));

-- name: AppendPlatformHumanAudit :exec
INSERT INTO iam_audit_events(event_id,actor_id,authentication_method,boundary,action,target_type,target_id,target_version,result,reason,
request_id,correlation_id,decision_id,source_service,occurred_at,recorded_at,caller_principal_id,caller_binding_id,caller_binding_version,caller_grant_version)
VALUES(sqlc.arg(event_id),sqlc.arg(actor_id),sqlc.arg(authentication_method),'platform',sqlc.arg(action),sqlc.arg(target_type),sqlc.arg(target_id),sqlc.arg(target_version),
sqlc.arg(result),sqlc.arg(reason),sqlc.arg(request_id),sqlc.arg(correlation_id),sqlc.arg(decision_id),'ani-iam',sqlc.arg(occurred_at),sqlc.arg(recorded_at),sqlc.narg(caller_principal_id),sqlc.narg(caller_binding_id),sqlc.narg(caller_binding_version),sqlc.narg(caller_grant_version));
