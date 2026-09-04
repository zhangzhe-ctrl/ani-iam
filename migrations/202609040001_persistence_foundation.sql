-- DP2-04 starts the target IAM schema from an empty database. The migration is
-- executed by ani_iam_migrator after the isolated bootstrap superuser creates
-- the migration and runtime roles. Runtime instances never execute migrations.

DO $$
BEGIN
    EXECUTE format(
        'REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC',
        current_database()
    );
END
$$;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO ani_iam_runtime;

CREATE TABLE principals (
    id uuid PRIMARY KEY,
    principal_type text NOT NULL CHECK (principal_type IN ('human', 'service')),
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT principals_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7')
);

CREATE TABLE tenant_access (
    tenant_id uuid PRIMARY KEY,
    status text NOT NULL CHECK (status IN ('bootstrap_pending', 'active', 'suspended')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT tenant_access_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid)
);

CREATE TABLE tenant_memberships (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    principal_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'suspended', 'removed')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT tenant_memberships_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT tenant_memberships_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT tenant_memberships_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT,
    CONSTRAINT tenant_memberships_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX tenant_memberships_one_live_principal_idx
    ON tenant_memberships (tenant_id, principal_id)
    WHERE status <> 'removed';

CREATE INDEX tenant_memberships_status_id_idx
    ON tenant_memberships (tenant_id, status, id);

CREATE TABLE tenant_roles (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    code text NOT NULL CHECK (code <> ''),
    system_role boolean NOT NULL,
    system_definition_version bigint NOT NULL CHECK (system_definition_version > 0),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, code),
    CONSTRAINT tenant_roles_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT tenant_roles_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT tenant_roles_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT
);

CREATE TABLE tenant_role_bindings (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    membership_id uuid NOT NULL,
    role_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, membership_id, role_id),
    CONSTRAINT tenant_role_bindings_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT tenant_role_bindings_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT tenant_role_bindings_membership_fk FOREIGN KEY (tenant_id, membership_id)
        REFERENCES tenant_memberships (tenant_id, id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT tenant_role_bindings_role_fk FOREIGN KEY (tenant_id, role_id)
        REFERENCES tenant_roles (tenant_id, id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX tenant_role_bindings_membership_id_idx
    ON tenant_role_bindings (tenant_id, membership_id, id);

CREATE TABLE iam_audit_events (
    tenant_id uuid NOT NULL,
    event_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    authentication_method text NOT NULL,
    boundary text NOT NULL,
    action text NOT NULL CHECK (action <> ''),
    target_type text NOT NULL CHECK (target_type <> ''),
    target_id uuid NOT NULL,
    target_version bigint NOT NULL CHECK (target_version > 0),
    result text NOT NULL CHECK (result IN ('succeeded', 'denied', 'failed')),
    reason text NOT NULL CHECK (reason <> ''),
    request_id text NOT NULL,
    correlation_id text NOT NULL,
    decision_id text NOT NULL,
    source_service text NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_id),
    CONSTRAINT iam_audit_events_uuid_v7 CHECK (substring(event_id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT iam_audit_events_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
        authentication_method IN ('password', 'oidc', 'api_key', 'service_token', 'internal')
    ),
    CONSTRAINT iam_audit_events_boundary_tenant CHECK (boundary = 'tenant'),
    CONSTRAINT iam_audit_events_request_required CHECK (request_id <> ''),
    CONSTRAINT iam_audit_events_correlation_required CHECK (correlation_id <> ''),
    CONSTRAINT iam_audit_events_decision_required CHECK (decision_id <> ''),
    CONSTRAINT iam_audit_events_source_required CHECK (source_service <> ''),
    CONSTRAINT iam_audit_events_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT,
    CONSTRAINT iam_audit_events_actor_fk FOREIGN KEY (actor_id)
        REFERENCES principals (id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX iam_audit_events_recorded_id_idx
    ON iam_audit_events (tenant_id, recorded_at DESC, event_id);

GRANT SELECT, INSERT, UPDATE ON TABLE
    principals,
    tenant_access,
    tenant_memberships,
    tenant_roles,
    tenant_role_bindings
TO ani_iam_runtime;

GRANT SELECT, INSERT ON TABLE iam_audit_events TO ani_iam_runtime;
