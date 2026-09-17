-- name: ReadWorkloadAuthorityGrant :one
SELECT authority.id, authority.version
FROM workload_grants AS authority
JOIN workload_target_registrations AS registration
  ON registration.audience=authority.audience AND registration.operation=authority.operation
 AND registration.scope=authority.scope
JOIN principals AS principal ON principal.id=authority.principal_id
JOIN workload_principals AS profile ON profile.principal_id=principal.id
JOIN workload_identity_bindings AS binding ON binding.principal_id=principal.id
WHERE authority.principal_id=sqlc.arg(principal_id)
 AND authority.environment=sqlc.arg(environment) AND authority.trust_domain=sqlc.arg(trust_domain)
 AND authority.audience=sqlc.arg(audience) AND authority.operation=sqlc.arg(operation)
 AND registration.enabled AND registration.target_sha256=sqlc.arg(target_sha256)
 AND authority.status='active'
 AND principal.principal_type='workload' AND principal.status='active'
 AND principal.version=sqlc.arg(principal_version) AND profile.owner_type='platform'
 AND binding.id=sqlc.arg(binding_id) AND binding.version=sqlc.arg(binding_version)
 AND binding.status='active';
