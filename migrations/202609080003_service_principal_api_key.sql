-- DP2-10 adds the single-Tenant Service Principal profile and its API Keys.

ALTER TABLE tenant_memberships
    ADD CONSTRAINT tenant_memberships_tenant_id_principal_unique
        UNIQUE (tenant_id, id, principal_id);

CREATE TABLE service_principals (
    principal_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    membership_id uuid NOT NULL,
    name text NOT NULL CHECK (name = btrim(name) AND name <> ''),
    normalized_name text NOT NULL CHECK (normalized_name = lower(btrim(normalized_name)) AND normalized_name <> ''),
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, principal_id),
    UNIQUE (tenant_id, membership_id),
    UNIQUE (tenant_id, normalized_name),
    CONSTRAINT service_principals_nonzero_tenant CHECK (tenant_id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT service_principals_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT,
    CONSTRAINT service_principals_membership_fk FOREIGN KEY (tenant_id, membership_id, principal_id)
        REFERENCES tenant_memberships (tenant_id, id, principal_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE FUNCTION enforce_service_principal_boundary() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    principal_kind text;
    principal_state text;
    profile_tenant uuid;
    profile_membership uuid;
BEGIN
    SELECT principal_type, status
      INTO principal_kind, principal_state
      FROM principals
     WHERE id = NEW.principal_id;

    IF principal_kind = 'service' THEN
        SELECT tenant_id, membership_id
          INTO profile_tenant, profile_membership
          FROM service_principals
         WHERE principal_id = NEW.principal_id;
        IF profile_tenant IS NULL
           OR (NEW.status <> 'removed' AND (NEW.tenant_id <> profile_tenant OR NEW.id <> profile_membership))
           OR (NEW.status = 'removed' AND principal_state <> 'disabled') THEN
            RAISE EXCEPTION 'service principal membership violates fixed tenant boundary'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER tenant_memberships_service_principal_boundary
AFTER INSERT OR UPDATE ON tenant_memberships
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_service_principal_boundary();

CREATE FUNCTION enforce_service_principal_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    principal_kind text;
BEGIN
    SELECT principal_type INTO principal_kind
      FROM principals
     WHERE id = NEW.principal_id;
    IF principal_kind <> 'service' THEN
        RAISE EXCEPTION 'service principal profile requires a service principal'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER service_principals_profile_kind
AFTER INSERT OR UPDATE ON service_principals
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_service_principal_profile();

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
    CONSTRAINT api_keys_service_principal_fk FOREIGN KEY (tenant_id, principal_id)
        REFERENCES service_principals (tenant_id, principal_id) ON DELETE RESTRICT
);

CREATE INDEX api_keys_principal_key_idx
    ON api_keys (tenant_id, principal_id, key_id);

CREATE INDEX api_keys_principal_created_at_idx
    ON api_keys (tenant_id, principal_id, created_at DESC);

CREATE FUNCTION enforce_active_api_key_boundary() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    principal_state text;
    membership_state text;
BEGIN
    IF NEW.status = 'active' THEN
        SELECT principal.status, membership.status
          INTO principal_state, membership_state
          FROM service_principals profile
          JOIN principals principal ON principal.id = profile.principal_id
          JOIN tenant_memberships membership
            ON membership.tenant_id = profile.tenant_id
           AND membership.id = profile.membership_id
           AND membership.principal_id = profile.principal_id
         WHERE profile.tenant_id = NEW.tenant_id
           AND profile.principal_id = NEW.principal_id;
        IF principal_state IS DISTINCT FROM 'active'
           OR membership_state IS DISTINCT FROM 'active' THEN
            RAISE EXCEPTION 'active API key requires an active service principal boundary'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

CREATE CONSTRAINT TRIGGER api_keys_active_service_principal_boundary
AFTER INSERT OR UPDATE ON api_keys
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_active_api_key_boundary();

GRANT SELECT, INSERT, UPDATE ON TABLE service_principals TO ani_iam_runtime;
GRANT SELECT, INSERT, UPDATE ON TABLE api_keys TO ani_iam_runtime;
