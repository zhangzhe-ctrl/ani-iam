-- DP2-05 adds only the persistent state required by the target tracer bullet:
-- password authentication, one tenant grant, one permission, access-token
-- online validation and a refresh-token record. It deliberately adds no RLS,
-- legacy compatibility tables, OIDC, API keys, invitations or platform roles.

CREATE TABLE verified_emails (
    principal_id uuid PRIMARY KEY,
    normalized_email text NOT NULL UNIQUE CHECK (normalized_email = lower(btrim(normalized_email)) AND normalized_email <> ''),
    verified_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT verified_emails_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE TABLE identities (
    id uuid PRIMARY KEY,
    principal_id uuid NOT NULL,
    provider text NOT NULL CHECK (provider = 'password'),
    issuer text NOT NULL CHECK (issuer <> ''),
    subject text NOT NULL CHECK (subject <> ''),
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (issuer, subject),
    CONSTRAINT identities_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT identities_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE TABLE password_credentials (
    principal_id uuid PRIMARY KEY,
    identity_id uuid NOT NULL UNIQUE,
    password_hash text NOT NULL CHECK (password_hash <> ''),
    algorithm text NOT NULL CHECK (algorithm = 'argon2id'),
    failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    locked_until timestamptz,
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT password_credentials_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT,
    CONSTRAINT password_credentials_identity_fk FOREIGN KEY (identity_id)
        REFERENCES identities (id) ON DELETE RESTRICT
);

CREATE TABLE tenant_lifecycle_projections (
    tenant_id uuid PRIMARY KEY,
    status text NOT NULL CHECK (status IN ('active', 'suspended', 'terminated')),
    lifecycle_version bigint NOT NULL CHECK (lifecycle_version > 0),
    effective_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    fresh_until timestamptz NOT NULL CHECK (fresh_until >= observed_at),
    CONSTRAINT tenant_lifecycle_projections_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT tenant_lifecycle_projections_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT
);

CREATE TABLE tenant_role_permissions (
    tenant_id uuid NOT NULL,
    role_id uuid NOT NULL,
    resource text NOT NULL CHECK (resource <> ''),
    action text NOT NULL CHECK (action <> ''),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, role_id, resource, action),
    CONSTRAINT tenant_role_permissions_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT tenant_role_permissions_role_fk FOREIGN KEY (tenant_id, role_id)
        REFERENCES tenant_roles (tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX tenant_role_permissions_lookup_idx
    ON tenant_role_permissions (tenant_id, resource, action, role_id);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    principal_id uuid NOT NULL,
    audience text NOT NULL CHECK (audience IN ('console', 'boss')),
    status text NOT NULL CHECK (status IN ('active', 'revoked', 'expired')),
    device_name text NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT sessions_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT sessions_expiry_order CHECK (idle_expires_at <= absolute_expires_at),
    CONSTRAINT sessions_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE INDEX sessions_principal_status_idx
    ON sessions (principal_id, status, id);

CREATE TABLE session_grants (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    session_id uuid NOT NULL,
    membership_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'revoked', 'expired')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT session_grants_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT session_grants_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT session_grants_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT,
    CONSTRAINT session_grants_session_fk FOREIGN KEY (session_id)
        REFERENCES sessions (id) ON DELETE RESTRICT,
    CONSTRAINT session_grants_membership_fk FOREIGN KEY (tenant_id, membership_id)
        REFERENCES tenant_memberships (tenant_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX session_grants_one_active_boundary_idx
    ON session_grants (tenant_id, session_id)
    WHERE status = 'active';

CREATE INDEX session_grants_session_lookup_idx
    ON session_grants (tenant_id, session_id, id);

CREATE TABLE refresh_token_families (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    grant_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'revoked', 'expired')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT refresh_token_families_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT refresh_token_families_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT refresh_token_families_grant_fk FOREIGN KEY (tenant_id, grant_id)
        REFERENCES session_grants (tenant_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX refresh_token_families_one_active_grant_idx
    ON refresh_token_families (tenant_id, grant_id)
    WHERE status = 'active';

CREATE TABLE refresh_tokens (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    family_id uuid NOT NULL,
    digest bytea NOT NULL CHECK (octet_length(digest) = 32),
    status text NOT NULL CHECK (status IN ('active', 'consumed', 'revoked', 'expired')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > issued_at),
    consumed_at timestamptz,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (digest),
    CONSTRAINT refresh_tokens_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT refresh_tokens_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT refresh_tokens_family_fk FOREIGN KEY (tenant_id, family_id)
        REFERENCES refresh_token_families (tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX refresh_tokens_family_status_idx
    ON refresh_tokens (tenant_id, family_id, status, id);

GRANT SELECT ON TABLE
    verified_emails,
    identities,
    password_credentials,
    tenant_lifecycle_projections,
    tenant_role_permissions
TO ani_iam_runtime;

GRANT SELECT, INSERT ON TABLE
    sessions,
    session_grants,
    refresh_token_families,
    refresh_tokens
TO ani_iam_runtime;
