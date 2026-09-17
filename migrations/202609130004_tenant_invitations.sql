-- Invitation intent history is not an authorization relationship. Live role
-- references are separate, same-Tenant FKs and are released only when the
-- intent becomes terminal. No Role deletion cascades through an Invitation.
ALTER TABLE tenant_memberships ADD CONSTRAINT tenant_memberships_invitation_owner UNIQUE(tenant_id,id,principal_id);

CREATE TABLE tenant_invitations (
    tenant_id uuid NOT NULL REFERENCES tenant_access(tenant_id),
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    normalized_email text NOT NULL CHECK (normalized_email=lower(btrim(normalized_email)) AND length(normalized_email) BETWEEN 3 AND 320),
    role_ids uuid[] NOT NULL CHECK (cardinality(role_ids) BETWEEN 1 AND 100 AND array_position(role_ids,NULL) IS NULL),
    locale text NOT NULL CHECK (locale IN ('en-US','zh-CN')),
    status text NOT NULL CHECK (status IN ('pending','accepted','cancelled','expired')),
    token_digest bytea NOT NULL CHECK (octet_length(token_digest)=32),
    delivery_generation bigint NOT NULL CHECK (delivery_generation>0),
    expires_at timestamptz NOT NULL,
    version bigint NOT NULL CHECK (version>0),
    created_by uuid NOT NULL REFERENCES principals(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    accepted_membership_id uuid,
    accepted_principal_id uuid,
    PRIMARY KEY(tenant_id,id),
    UNIQUE(token_digest),
    CHECK (tenant_id<>'00000000-0000-0000-0000-000000000000'::uuid),
    CHECK (expires_at>created_at AND updated_at>=created_at),
    CHECK ((status='accepted' AND accepted_membership_id IS NOT NULL AND accepted_principal_id IS NOT NULL)
        OR (status<>'accepted' AND accepted_membership_id IS NULL AND accepted_principal_id IS NULL)),
    FOREIGN KEY(tenant_id,accepted_membership_id,accepted_principal_id) REFERENCES tenant_memberships(tenant_id,id,principal_id)
);
CREATE UNIQUE INDEX tenant_invitations_pending_email ON tenant_invitations(tenant_id,normalized_email) WHERE status='pending';
CREATE INDEX tenant_invitations_status_page ON tenant_invitations(tenant_id,status,id);

CREATE TABLE tenant_invitation_roles (
    tenant_id uuid NOT NULL,
    invitation_id uuid NOT NULL,
    role_id uuid NOT NULL,
    PRIMARY KEY(tenant_id,invitation_id,role_id),
    FOREIGN KEY(tenant_id,invitation_id) REFERENCES tenant_invitations(tenant_id,id),
    FOREIGN KEY(tenant_id,role_id) REFERENCES tenant_roles(tenant_id,id)
);
CREATE INDEX tenant_invitation_roles_role ON tenant_invitation_roles(tenant_id,role_id,invitation_id);

CREATE TABLE tenant_invitation_outbox (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    invitation_id uuid NOT NULL,
    delivery_generation bigint NOT NULL CHECK (delivery_generation>0),
    payload_key_version text,
    payload_ciphertext bytea,
    status text NOT NULL CHECK (status IN ('pending','claimed','delivered','cancelled','attention_required')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count>=0),
    available_at timestamptz NOT NULL,
    claimed_at timestamptz,
    delivered_at timestamptz,
    notification_id text,
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(tenant_id,id),
    UNIQUE(tenant_id,invitation_id,delivery_generation),
    FOREIGN KEY(tenant_id,invitation_id) REFERENCES tenant_invitations(tenant_id,id),
    CHECK ((status IN ('pending','claimed','attention_required') AND payload_key_version IS NOT NULL
        AND length(payload_key_version) BETWEEN 1 AND 64 AND payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext)>=29)
        OR (status IN ('delivered','cancelled') AND payload_key_version IS NULL AND payload_ciphertext IS NULL)),
    CHECK ((status='pending' AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL)
        OR (status='claimed' AND claimed_at IS NOT NULL AND delivered_at IS NULL AND notification_id IS NULL)
        OR (status='delivered' AND claimed_at IS NOT NULL AND delivered_at IS NOT NULL AND notification_id IS NOT NULL)
        OR (status IN ('cancelled','attention_required') AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL))
);
CREATE INDEX tenant_invitation_outbox_dispatch ON tenant_invitation_outbox(tenant_id,status,available_at,id);

CREATE FUNCTION guard_tenant_invitation_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.id<>OLD.id OR NEW.normalized_email<>OLD.normalized_email
        OR NEW.role_ids<>OLD.role_ids OR NEW.created_by<>OLD.created_by OR NEW.created_at<>OLD.created_at
        OR OLD.status IN ('accepted','cancelled') THEN
        RAISE EXCEPTION 'Invitation identity or terminal intent is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_invitation_identity BEFORE UPDATE ON tenant_invitations FOR EACH ROW EXECUTE FUNCTION guard_tenant_invitation_identity();

GRANT SELECT,INSERT,UPDATE ON tenant_invitations,tenant_invitation_outbox TO ani_iam_runtime;
GRANT SELECT,INSERT,DELETE ON tenant_invitation_roles TO ani_iam_runtime;
ALTER TABLE tenant_mutation_results DROP CONSTRAINT tenant_mutation_results_operation_check;
ALTER TABLE tenant_mutation_results ADD CONSTRAINT tenant_mutation_results_operation_check CHECK (operation IN (
    'createTenantWorkload','updateTenantWorkload','createIAMAPIKey','revokeIAMAPIKey',
    'updateTenantIAMMember','removeTenantIAMMember','bindTenantIAMRole','unbindTenantIAMRole',
    'updateTenantIAMAccess','createTenantIAMRole','updateTenantIAMRole','deleteTenantIAMRole',
    'createTenantIAMInvitation','resendTenantIAMInvitation','cancelTenantIAMInvitation','acceptTenantIAMInvitation'
));
UPDATE iam_schema_revision SET revision='202609130004' WHERE singleton;
