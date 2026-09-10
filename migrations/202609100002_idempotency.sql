-- Results contain only explicitly typed, non-secret response fields. No TTL cleanup.
CREATE TABLE tenant_mutation_results (
    tenant_id uuid NOT NULL REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL REFERENCES principals (id) ON DELETE RESTRICT,
    operation text NOT NULL CHECK (operation IN (
        'createTenantWorkload', 'updateTenantWorkload', 'createIAMAPIKey', 'revokeIAMAPIKey',
        'updateTenantIAMMember', 'removeTenantIAMMember', 'bindTenantIAMRole', 'unbindTenantIAMRole',
        'updateTenantIAMAccess'
    )),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    intent_digest bytea NOT NULL CHECK (octet_length(intent_digest) = 32),
    caller_principal_id uuid REFERENCES workload_principals (principal_id) ON DELETE RESTRICT,
    result bytea NOT NULL CHECK (octet_length(result) <= 65536),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at = created_at + interval '24 hours'),
    PRIMARY KEY (tenant_id, actor_id, operation, idempotency_key),
    CONSTRAINT mutation_result_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid)
);
GRANT SELECT, INSERT ON tenant_mutation_results TO ani_iam_runtime;
INSERT INTO iam_schema_revision (singleton, revision) VALUES (true, '202609100002');
