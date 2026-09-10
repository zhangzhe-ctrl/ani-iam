-- WR-18 platform trust is owner-provisioned, runtime read-only. No auto-bootstrap.
CREATE TABLE workload_identity_bindings (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    principal_id uuid NOT NULL,
    environment text NOT NULL,
    trust_domain text NOT NULL,
    identity_kind text NOT NULL CHECK (identity_kind = 'x509_dns'),
    identity_value text NOT NULL CHECK (identity_value = lower(btrim(identity_value))
        AND identity_value <> '' AND identity_value !~ '[[:space:]*/]'),
    status text NOT NULL CHECK (status IN ('active', 'revoked')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (environment, trust_domain, identity_kind, identity_value),
    UNIQUE (id, principal_id),
    FOREIGN KEY (principal_id, environment, trust_domain)
        REFERENCES workload_principals (principal_id, environment, trust_domain) ON DELETE RESTRICT
);

CREATE TABLE workload_grants (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    principal_id uuid NOT NULL,
    environment text NOT NULL,
    trust_domain text NOT NULL,
    audience text NOT NULL CHECK (audience = 'ani-iam'),
    operation text NOT NULL CHECK (operation ~ '^/(iam[.]v1[.][A-Za-z]+Service/[A-Za-z]+|grpc[.]health[.]v1[.]Health/Check)$'),
    scope text NOT NULL CHECK (scope = 'iam_ingress'),
    status text NOT NULL CHECK (status IN ('active', 'revoked')),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (principal_id, audience, operation, scope),
    FOREIGN KEY (principal_id, environment, trust_domain)
        REFERENCES workload_principals (principal_id, environment, trust_domain) ON DELETE RESTRICT
);
GRANT SELECT ON workload_identity_bindings, workload_grants TO ani_iam_runtime;

CREATE FUNCTION require_human_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM principals WHERE id = NEW.principal_id AND principal_type = 'human') THEN
        RAISE EXCEPTION 'Human identity and Session require Human Principal' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.principal_id IS DISTINCT FROM OLD.principal_id THEN
        RAISE EXCEPTION 'Human identity and Session owner are immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER verified_email_human BEFORE INSERT OR UPDATE ON verified_emails
FOR EACH ROW EXECUTE FUNCTION require_human_principal();
CREATE TRIGGER identity_human BEFORE INSERT OR UPDATE ON identities
FOR EACH ROW EXECUTE FUNCTION require_human_principal();
CREATE TRIGGER password_credential_human BEFORE INSERT OR UPDATE ON password_credentials
FOR EACH ROW EXECUTE FUNCTION require_human_principal();
CREATE TRIGGER session_human BEFORE INSERT OR UPDATE ON sessions
FOR EACH ROW EXECUTE FUNCTION require_human_principal();

ALTER TABLE identities ADD CONSTRAINT identities_id_principal_unique UNIQUE (id, principal_id);
ALTER TABLE password_credentials ADD CONSTRAINT password_identity_same_human
    FOREIGN KEY (identity_id, principal_id) REFERENCES identities (id, principal_id) ON DELETE RESTRICT;

ALTER TABLE iam_audit_events DROP CONSTRAINT iam_audit_events_authentication_method_allowed;
ALTER TABLE iam_audit_events ADD CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
    authentication_method IN ('password', 'oidc', 'api_key', 'workload_token', 'internal', 'anonymous', 'password_action')
);
ALTER TABLE iam_audit_events
    ADD COLUMN caller_principal_id uuid REFERENCES workload_principals (principal_id) ON DELETE RESTRICT,
    ADD COLUMN caller_binding_id uuid,
    ADD COLUMN caller_binding_version bigint,
    ADD COLUMN caller_grant_version bigint,
    ADD CONSTRAINT audit_caller_binding FOREIGN KEY (caller_binding_id, caller_principal_id)
        REFERENCES workload_identity_bindings (id, principal_id) ON DELETE RESTRICT,
    ADD CONSTRAINT audit_caller_shape CHECK (
        (caller_principal_id IS NULL AND caller_binding_id IS NULL
         AND caller_binding_version IS NULL AND caller_grant_version IS NULL)
        OR (caller_principal_id IS NOT NULL AND caller_binding_id IS NOT NULL
            AND caller_binding_version IS NOT NULL AND caller_binding_version > 0
            AND caller_grant_version IS NOT NULL AND caller_grant_version > 0)
    );

-- Runtime checks the exact final revision before becoming Ready. It never writes
-- this owner-controlled marker or invokes migration on startup.
CREATE TABLE iam_schema_revision (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision text NOT NULL
);
GRANT SELECT ON iam_schema_revision TO ani_iam_runtime;
