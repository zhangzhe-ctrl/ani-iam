-- WR-18 clean-install Workload profile. Owner is independent of identity and authority.
ALTER TABLE tenant_memberships
    ADD CONSTRAINT tenant_memberships_tenant_id_principal_unique UNIQUE (tenant_id, id, principal_id);

CREATE TABLE workload_principals (
    principal_id uuid PRIMARY KEY REFERENCES principals (id) ON DELETE RESTRICT,
    owner_type text NOT NULL CHECK (owner_type IN ('tenant', 'platform')),
    tenant_id uuid REFERENCES tenant_access (tenant_id) ON DELETE RESTRICT,
    membership_id uuid,
    environment text,
    trust_domain text,
    name text NOT NULL CHECK (name = lower(btrim(name)) AND name <> ''),
    normalized_name text NOT NULL CHECK (normalized_name = name),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, principal_id),
    UNIQUE (tenant_id, membership_id),
    UNIQUE (tenant_id, normalized_name),
    UNIQUE (environment, trust_domain, normalized_name),
    UNIQUE (principal_id, environment, trust_domain),
    CONSTRAINT workload_owner_shape CHECK (
        (owner_type = 'tenant' AND tenant_id IS NOT NULL
         AND tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid
         AND environment IS NULL AND trust_domain IS NULL)
        OR (owner_type = 'platform' AND tenant_id IS NULL AND membership_id IS NULL
            AND environment IS NOT NULL AND environment = btrim(environment) AND environment <> ''
            AND trust_domain IS NOT NULL AND trust_domain = lower(btrim(trust_domain)) AND trust_domain <> '')
    ),
    CONSTRAINT workload_membership_fk FOREIGN KEY (tenant_id, membership_id, principal_id)
        REFERENCES tenant_memberships (tenant_id, id, principal_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

-- Evaluate final transaction state, including insert-then-remove within one UoW.
CREATE FUNCTION check_workload_relations() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    subject_id uuid;
    principal_kind text;
    principal_state text;
    profile workload_principals%ROWTYPE;
BEGIN
    IF TG_TABLE_NAME = 'principals' THEN
        subject_id := COALESCE(NEW.id, OLD.id);
    ELSE
        subject_id := COALESCE(NEW.principal_id, OLD.principal_id);
    END IF;
    SELECT principal_type, status INTO principal_kind, principal_state FROM principals WHERE id = subject_id;
    IF principal_kind IS NULL THEN RETURN NULL; END IF;
    SELECT * INTO profile FROM workload_principals WHERE principal_id = subject_id;
    IF principal_kind = 'human' THEN
        IF profile.principal_id IS NOT NULL THEN
            RAISE EXCEPTION 'human cannot have Workload profile' USING ERRCODE = '23514';
        END IF;
        RETURN NULL;
    END IF;
    IF profile.principal_id IS NULL THEN
        RAISE EXCEPTION 'Workload requires its owner profile' USING ERRCODE = '23514';
    END IF;
    IF profile.owner_type = 'platform' THEN
        IF EXISTS (SELECT 1 FROM tenant_memberships WHERE principal_id = subject_id) THEN
            RAISE EXCEPTION 'Platform Workload cannot have Tenant membership' USING ERRCODE = '23514';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM tenant_memberships
                   WHERE principal_id = subject_id
                     AND (tenant_id <> profile.tenant_id
                          OR (status <> 'removed' AND id IS DISTINCT FROM profile.membership_id))) THEN
            RAISE EXCEPTION 'Workload membership violates immutable Tenant owner' USING ERRCODE = '23514';
        END IF;
        IF profile.membership_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM tenant_memberships WHERE tenant_id = profile.tenant_id
            AND id = profile.membership_id AND principal_id = subject_id AND status <> 'removed'
        ) THEN
            RAISE EXCEPTION 'current Workload membership must not be removed' USING ERRCODE = '23514';
        END IF;
        IF principal_state = 'active' AND profile.membership_id IS NULL THEN
            RAISE EXCEPTION 'active Workload requires current membership' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NULL;
END
$$;
CREATE CONSTRAINT TRIGGER principals_workload_relations AFTER INSERT OR UPDATE ON principals
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_workload_relations();
CREATE CONSTRAINT TRIGGER workload_profile_relations AFTER INSERT OR UPDATE OR DELETE ON workload_principals
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_workload_relations();
CREATE CONSTRAINT TRIGGER workload_membership_relations AFTER INSERT OR UPDATE OR DELETE ON tenant_memberships
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_workload_relations();

CREATE FUNCTION protect_workload_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF current_user = 'ani_iam_runtime'
       AND (CASE WHEN TG_OP = 'DELETE' THEN OLD.owner_type ELSE NEW.owner_type END) = 'platform' THEN
        RAISE EXCEPTION 'runtime cannot mutate Platform trust profile' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.principal_id, NEW.owner_type, NEW.tenant_id, NEW.name, NEW.environment, NEW.trust_domain)
        IS DISTINCT FROM (OLD.principal_id, OLD.owner_type, OLD.tenant_id, OLD.name, OLD.environment, OLD.trust_domain) THEN
        RAISE EXCEPTION 'Workload identity and owner are immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER workload_profile_write_guard BEFORE INSERT OR UPDATE OR DELETE ON workload_principals
FOR EACH ROW EXECUTE FUNCTION protect_workload_profile();

CREATE FUNCTION protect_principal_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE subject_id uuid;
BEGIN
    subject_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END;
    IF current_user = 'ani_iam_runtime' AND EXISTS (
        SELECT 1 FROM workload_principals WHERE principal_id = subject_id AND owner_type = 'platform'
    ) THEN
        RAISE EXCEPTION 'runtime cannot mutate Platform Principal' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'UPDATE' AND (NEW.id, NEW.principal_type) IS DISTINCT FROM (OLD.id, OLD.principal_type) THEN
        RAISE EXCEPTION 'Principal identity and type are immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER principal_identity_write_guard BEFORE INSERT OR UPDATE OR DELETE ON principals
FOR EACH ROW EXECUTE FUNCTION protect_principal_identity();

-- Serialise relation changes through the stable Principal; constraints cannot be
-- bypassed by two concurrent transactions changing profile and Membership.
CREATE FUNCTION lock_membership_principal() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.tenant_id, NEW.id, NEW.principal_id)
        IS DISTINCT FROM (OLD.tenant_id, OLD.id, OLD.principal_id) THEN
        RAISE EXCEPTION 'Membership identity is immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.status = 'removed' AND NEW.status <> 'removed' THEN
        RAISE EXCEPTION 'removed Membership cannot be reactivated' USING ERRCODE = '23514';
    END IF;
    PERFORM 1 FROM principals WHERE id = NEW.principal_id FOR UPDATE;
    RETURN NEW;
END
$$;
CREATE TRIGGER membership_principal_lock BEFORE INSERT OR UPDATE ON tenant_memberships
FOR EACH ROW EXECUTE FUNCTION lock_membership_principal();

CREATE TABLE api_keys (
    tenant_id uuid NOT NULL,
    key_id uuid NOT NULL,
    principal_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'revoked')),
    display_prefix text NOT NULL CHECK (display_prefix LIKE 'ani\_%' ESCAPE '\'),
    secret_digest bytea NOT NULL CHECK (octet_length(secret_digest) = 32),
    never_expires boolean NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    version bigint NOT NULL CHECK (version > 0),
    PRIMARY KEY (tenant_id, key_id),
    UNIQUE (key_id),
    UNIQUE (secret_digest),
    CONSTRAINT api_keys_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT api_keys_expiry_mode CHECK (
        (never_expires AND expires_at IS NULL)
        OR (NOT never_expires AND expires_at IS NOT NULL AND expires_at > created_at)
    ),
    CONSTRAINT api_keys_revocation_state CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    ),
    CONSTRAINT api_keys_tenant_workload_fk FOREIGN KEY (tenant_id, principal_id)
        REFERENCES workload_principals (tenant_id, principal_id) ON DELETE RESTRICT
);

CREATE INDEX api_keys_principal_key_idx
    ON api_keys (tenant_id, principal_id, key_id);

CREATE INDEX api_keys_principal_created_at_idx
    ON api_keys (tenant_id, principal_id, created_at DESC);

CREATE FUNCTION protect_api_key_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.tenant_id, NEW.key_id, NEW.principal_id, NEW.secret_digest, NEW.display_prefix,
        NEW.never_expires, NEW.expires_at, NEW.created_at)
        IS DISTINCT FROM (OLD.tenant_id, OLD.key_id, OLD.principal_id, OLD.secret_digest, OLD.display_prefix,
        OLD.never_expires, OLD.expires_at, OLD.created_at) THEN
        RAISE EXCEPTION 'API Key identity and credential are immutable' USING ERRCODE = '23514';
    END IF;
    IF OLD.status = 'revoked' AND (NEW.status <> 'revoked' OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'revoked API Key cannot be reactivated' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER api_key_identity_write_guard BEFORE UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION protect_api_key_identity();

CREATE FUNCTION enforce_active_api_key_boundary() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    principal_state text;
    membership_state text;
BEGIN
    IF NEW.status = 'active' THEN
        SELECT principal.status, membership.status
          INTO principal_state, membership_state
          FROM workload_principals profile
          JOIN principals principal ON principal.id = profile.principal_id
          JOIN tenant_memberships membership
            ON membership.tenant_id = profile.tenant_id
           AND membership.id = profile.membership_id
           AND membership.principal_id = profile.principal_id
         WHERE profile.tenant_id = NEW.tenant_id
           AND profile.principal_id = NEW.principal_id;
        IF principal_state IS DISTINCT FROM 'active'
           OR membership_state IS DISTINCT FROM 'active' THEN
            RAISE EXCEPTION 'active API key requires an active tenant workload boundary'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER api_keys_active_tenant_workload_boundary
AFTER INSERT OR UPDATE ON api_keys
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_active_api_key_boundary();

GRANT SELECT, INSERT, UPDATE ON TABLE workload_principals TO ani_iam_runtime;
GRANT SELECT, INSERT, UPDATE ON TABLE api_keys TO ani_iam_runtime;
