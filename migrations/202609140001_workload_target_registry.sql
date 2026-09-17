-- Reviewed synchronous registration replaces the historical service-name CHECK.
-- These are Platform configuration records, not Tenant-owned business data.
CREATE TABLE workload_target_registrations (
    audience text NOT NULL CHECK (audience ~ '^[a-z0-9][a-z0-9.-]{0,127}$'),
    operation text NOT NULL CHECK (
        operation ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'
        OR operation ~ '^/[A-Za-z][A-Za-z0-9_.]{0,191}/[A-Za-z][A-Za-z0-9_]{0,63}$'),
    scope text NOT NULL CHECK (scope ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$'),
    target_sha256 bytea NOT NULL CHECK (octet_length(target_sha256) = 32),
    bundle_sha256 bytea NOT NULL CHECK (octet_length(bundle_sha256) = 32),
    enabled boolean NOT NULL,
    PRIMARY KEY (audience, operation),
    UNIQUE (audience, operation, scope)
);

-- Preserve existing Grants during an upgrade, but admit no unreviewed target.
-- Offline registration must replace these disabled zero-digest placeholders.
INSERT INTO workload_target_registrations
    (audience, operation, scope, target_sha256, bundle_sha256, enabled)
SELECT DISTINCT audience, operation, scope,
       decode(repeat('00',32),'hex'), decode(repeat('00',32),'hex'), false
FROM workload_grants;

ALTER TABLE workload_grants DROP CONSTRAINT workload_grants_target;
ALTER TABLE workload_grants ADD CONSTRAINT workload_grants_registered_target
    FOREIGN KEY (audience, operation, scope)
    REFERENCES workload_target_registrations (audience, operation, scope);

-- Append-only configuration security record. Installation uses the existing
-- restricted migrator identity; normal runtime cannot install or modify it.
CREATE TABLE workload_registry_installations (
    id uuid PRIMARY KEY,
    previous_sha256 bytea CHECK (previous_sha256 IS NULL OR octet_length(previous_sha256) = 32),
    installed_sha256 bytea NOT NULL CHECK (octet_length(installed_sha256) = 32),
    installed_by text NOT NULL CHECK (installed_by = 'ani_iam_migrator'),
    installed_at timestamptz NOT NULL
);

REVOKE ALL ON workload_target_registrations, workload_registry_installations
    FROM PUBLIC, ani_iam_runtime, ani_iam_provisioner;
GRANT SELECT ON workload_target_registrations TO ani_iam_runtime, ani_iam_provisioner;
UPDATE iam_schema_revision SET revision = '202609140001' WHERE singleton;
