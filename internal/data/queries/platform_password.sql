-- name: ReadPlatformPassword :one
SELECT p.id AS principal_id,p.status AS principal_status,i.id AS identity_id,(i.status='active')::boolean AS identity_active,
 e.normalized_email,COALESCE(m.id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS membership_id,
 COALESCE(m.status,'removed')::text AS membership_status,c.password_hash,c.version AS credential_version,c.locked_until
FROM verified_emails e JOIN principals p ON p.id=e.principal_id AND p.principal_type='human'
JOIN password_credentials c ON c.principal_id=p.id JOIN identities i ON i.id=c.identity_id AND i.principal_id=p.id AND i.provider='password'
LEFT JOIN platform_memberships m ON m.principal_id=p.id AND m.status<>'removed'
WHERE e.normalized_email=sqlc.arg(account);

-- name: LockPlatformPassword :one
SELECT p.id AS principal_id,p.status AS principal_status,i.id AS identity_id,(i.status='active')::boolean AS identity_active,
 e.normalized_email,COALESCE(m.id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS membership_id,
 COALESCE(m.status,'removed')::text AS membership_status,c.password_hash,c.version AS credential_version,c.locked_until
FROM verified_emails e JOIN principals p ON p.id=e.principal_id AND p.principal_type='human'
JOIN password_credentials c ON c.principal_id=p.id JOIN identities i ON i.id=c.identity_id AND i.principal_id=p.id AND i.provider='password'
LEFT JOIN platform_memberships m ON m.principal_id=p.id AND m.status<>'removed'
WHERE e.normalized_email=sqlc.arg(account) FOR UPDATE OF c FOR SHARE OF p,i;
