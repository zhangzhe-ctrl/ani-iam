-- WR22: Platform authority has independent relations, never a sentinel Tenant.
CREATE TABLE platform_memberships (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    principal_id uuid NOT NULL REFERENCES principals(id),
    status text NOT NULL CHECK (status IN ('active','suspended','removed')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE(id,principal_id)
);
CREATE UNIQUE INDEX platform_memberships_current_principal ON platform_memberships(principal_id) WHERE status<>'removed';
CREATE TRIGGER platform_membership_human BEFORE INSERT OR UPDATE ON platform_memberships
FOR EACH ROW EXECUTE FUNCTION require_human_principal();
CREATE TRIGGER first_administrator_completion_human BEFORE INSERT OR UPDATE ON first_administrator_completions
FOR EACH ROW EXECUTE FUNCTION require_human_principal();

CREATE TABLE platform_roles (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    code text NOT NULL UNIQUE CHECK (code<>''),
    display_name text NOT NULL CHECK (display_name<>''),
    system_role boolean NOT NULL,
    system_definition_version bigint NOT NULL CHECK (system_definition_version>0),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE TABLE platform_role_permissions (
    role_id uuid NOT NULL REFERENCES platform_roles(id),
    scope text NOT NULL CHECK (scope='platform'),
    resource text NOT NULL,
    action text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY(role_id,scope,resource,action),
    FOREIGN KEY(scope,resource,action) REFERENCES permission_catalog(scope,resource,action)
);
CREATE TABLE platform_role_bindings (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    membership_id uuid NOT NULL REFERENCES platform_memberships(id),
    role_id uuid NOT NULL REFERENCES platform_roles(id),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE(membership_id,role_id)
);
CREATE TABLE platform_administrator_guard (
    singleton boolean PRIMARY KEY CHECK (singleton),
    version bigint NOT NULL CHECK (version>0)
);
INSERT INTO platform_administrator_guard VALUES(true,1);

-- A Grant must bind a BOSS Session and Membership of the same Human.
ALTER TABLE sessions ADD CONSTRAINT sessions_platform_owner_audience UNIQUE(id,principal_id,audience);
CREATE TABLE platform_session_grants (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    session_id uuid NOT NULL,
    principal_id uuid NOT NULL,
    audience text NOT NULL DEFAULT 'boss' CHECK (audience='boss'),
    membership_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('active','revoked','expired')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    FOREIGN KEY(session_id,principal_id,audience) REFERENCES sessions(id,principal_id,audience),
    FOREIGN KEY(membership_id,principal_id) REFERENCES platform_memberships(id,principal_id)
);
CREATE UNIQUE INDEX platform_session_grants_active_session ON platform_session_grants(session_id) WHERE status='active';
CREATE TABLE platform_refresh_token_families (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    grant_id uuid NOT NULL REFERENCES platform_session_grants(id),
    status text NOT NULL CHECK (status IN ('active','revoked','expired')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX platform_refresh_families_active_grant ON platform_refresh_token_families(grant_id) WHERE status='active';
CREATE TABLE platform_refresh_tokens (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    family_id uuid NOT NULL REFERENCES platform_refresh_token_families(id),
    digest bytea NOT NULL UNIQUE CHECK (octet_length(digest)=32),
    status text NOT NULL CHECK (status IN ('active','consumed','revoked','expired')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at>issued_at),
    consumed_at timestamptz,
    replaced_by uuid,
    UNIQUE(id,family_id),
    CHECK ((status='consumed' AND consumed_at IS NOT NULL AND replaced_by IS NOT NULL)
        OR (status<>'consumed' AND consumed_at IS NULL AND replaced_by IS NULL)),
    FOREIGN KEY(replaced_by,family_id) REFERENCES platform_refresh_tokens(id,family_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX platform_refresh_tokens_active_family ON platform_refresh_tokens(family_id) WHERE status='active';

CREATE TABLE platform_mutation_results (
    actor_principal_id uuid NOT NULL REFERENCES principals(id),
    operation text NOT NULL CHECK (operation IN ('createPlatformIAMRole','updatePlatformIAMRole','deletePlatformIAMRole',
        'updatePlatformIAMMember','removePlatformIAMMember','bindPlatformIAMRole','unbindPlatformIAMRole',
        'updateTenantAccess','createPlatformIAMInvitation','cancelPlatformIAMInvitation','resendPlatformIAMInvitation',
        'acceptPlatformIAMInvitation','requestRecoveryBootstrap','approveRecoveryBootstrap','executeRecoveryBootstrap',
        'requestRestoreTenantAdmin','approveRestoreTenantAdmin','executeRestoreTenantAdmin')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
    result jsonb NOT NULL CHECK (jsonb_typeof(result)='object'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at=created_at+interval '24 hours'),
    PRIMARY KEY(actor_principal_id,operation,idempotency_key)
);

CREATE FUNCTION protect_platform_relation_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'Platform relation identity is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='platform_memberships' THEN
        IF NEW.principal_id IS DISTINCT FROM OLD.principal_id OR (OLD.status='removed' AND NEW.status<>'removed') THEN
            RAISE EXCEPTION 'Platform Membership owner is immutable and removed Membership cannot revive' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='platform_session_grants' THEN
        IF (NEW.session_id,NEW.membership_id,NEW.principal_id,NEW.audience) IS DISTINCT FROM
           (OLD.session_id,OLD.membership_id,OLD.principal_id,OLD.audience) THEN
            RAISE EXCEPTION 'Platform Grant boundary and owner are immutable' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='platform_refresh_token_families' THEN
        IF NEW.grant_id IS DISTINCT FROM OLD.grant_id THEN
            RAISE EXCEPTION 'Platform Refresh Family owner is immutable' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='platform_refresh_tokens' THEN
        IF (NEW.family_id,NEW.digest) IS DISTINCT FROM (OLD.family_id,OLD.digest) THEN
            RAISE EXCEPTION 'Platform Refresh Token owner is immutable' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='platform_role_bindings' THEN
        IF (NEW.membership_id,NEW.role_id) IS DISTINCT FROM (OLD.membership_id,OLD.role_id) THEN
            RAISE EXCEPTION 'Platform Role Binding identity is immutable' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER platform_membership_identity BEFORE UPDATE ON platform_memberships FOR EACH ROW EXECUTE FUNCTION protect_platform_relation_identity();
CREATE TRIGGER platform_grant_identity BEFORE UPDATE ON platform_session_grants FOR EACH ROW EXECUTE FUNCTION protect_platform_relation_identity();
CREATE TRIGGER platform_family_identity BEFORE UPDATE ON platform_refresh_token_families FOR EACH ROW EXECUTE FUNCTION protect_platform_relation_identity();
CREATE TRIGGER platform_token_identity BEFORE UPDATE ON platform_refresh_tokens FOR EACH ROW EXECUTE FUNCTION protect_platform_relation_identity();
CREATE TRIGGER platform_binding_identity BEFORE UPDATE ON platform_role_bindings FOR EACH ROW EXECUTE FUNCTION protect_platform_relation_identity();

GRANT SELECT,INSERT,UPDATE ON platform_memberships,platform_roles,platform_role_bindings,
    platform_session_grants,platform_refresh_token_families,platform_refresh_tokens TO ani_iam_runtime;
GRANT SELECT,INSERT,DELETE ON platform_role_permissions TO ani_iam_runtime;
GRANT DELETE ON platform_roles,platform_role_bindings TO ani_iam_runtime;
GRANT SELECT,UPDATE ON platform_administrator_guard TO ani_iam_runtime;
GRANT SELECT,INSERT ON platform_mutation_results TO ani_iam_runtime;

ALTER TABLE iam_audit_events DROP CONSTRAINT iam_audit_events_boundary_scope;
ALTER TABLE iam_audit_events ADD CONSTRAINT iam_audit_events_boundary_scope CHECK (
    (boundary='tenant' AND tenant_id IS NOT NULL)
    OR (boundary='principal' AND tenant_id IS NULL)
    OR (boundary='platform' AND tenant_id IS NULL AND authentication_method IN ('password','oidc','anonymous','workload_provisioner','administrator_provisioner'))
);
UPDATE iam_schema_revision SET revision='202609130002' WHERE singleton;
