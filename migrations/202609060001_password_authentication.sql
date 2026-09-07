-- DP2-06 adds global Human password actions and their purpose-specific
-- notification outbox. Action bearer tokens are minted only at dispatch and
-- are never stored in these relations.

ALTER TABLE iam_audit_events
    DROP CONSTRAINT iam_audit_events_pkey,
    DROP CONSTRAINT iam_audit_events_authentication_method_allowed,
    DROP CONSTRAINT iam_audit_events_boundary_tenant,
    ALTER COLUMN tenant_id DROP NOT NULL,
    ALTER COLUMN actor_id DROP NOT NULL;

ALTER TABLE iam_audit_events
    ADD CONSTRAINT iam_audit_events_pkey PRIMARY KEY (event_id),
    ADD CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
        authentication_method IN (
            'password', 'oidc', 'api_key', 'service_token', 'internal',
            'anonymous', 'password_action'
        )
    ),
    ADD CONSTRAINT iam_audit_events_boundary_scope CHECK (
        (boundary = 'tenant' AND tenant_id IS NOT NULL)
        OR (boundary = 'principal' AND tenant_id IS NULL)
    ),
    ADD CONSTRAINT iam_audit_events_actor_scope CHECK (
        (authentication_method = 'anonymous' AND actor_id IS NULL)
        OR (authentication_method <> 'anonymous' AND actor_id IS NOT NULL)
    );

CREATE TABLE password_action_requests (
    operation_id uuid PRIMARY KEY,
    account_digest bytea NOT NULL CHECK (octet_length(account_digest) = 32),
    audience text NOT NULL CHECK (audience IN ('console', 'boss')),
    principal_id uuid,
    purpose text CHECK (purpose IN ('setup', 'reset')),
    expires_at timestamptz NOT NULL,
    idempotency_key text NOT NULL UNIQUE CHECK (btrim(idempotency_key) <> ''),
    created_at timestamptz NOT NULL,
    CONSTRAINT password_action_requests_uuid_v7 CHECK (substring(operation_id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT password_action_requests_expiry CHECK (expires_at > created_at),
    CONSTRAINT password_action_requests_target_binding CHECK (
        (principal_id IS NULL AND purpose IS NULL)
        OR (principal_id IS NOT NULL AND purpose IS NOT NULL)
    ),
    CONSTRAINT password_action_requests_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE TABLE password_actions (
    operation_id uuid PRIMARY KEY,
    principal_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('setup', 'reset')),
    status text NOT NULL CHECK (status IN ('active', 'replaced', 'consumed', 'expired')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    replaced_by uuid,
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT password_actions_state_fields CHECK (
        (status = 'active' AND consumed_at IS NULL AND replaced_by IS NULL)
        OR (status = 'replaced' AND consumed_at IS NULL AND replaced_by IS NOT NULL)
        OR (status = 'consumed' AND consumed_at IS NOT NULL AND replaced_by IS NULL)
        OR (status = 'expired' AND consumed_at IS NULL AND replaced_by IS NULL)
    ),
    CONSTRAINT password_actions_request_fk FOREIGN KEY (operation_id)
        REFERENCES password_action_requests (operation_id) ON DELETE RESTRICT,
    CONSTRAINT password_actions_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT,
    CONSTRAINT password_actions_replacement_fk FOREIGN KEY (replaced_by)
        REFERENCES password_actions (operation_id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED
);

CREATE UNIQUE INDEX password_actions_one_active_principal_idx
    ON password_actions (principal_id)
    WHERE status = 'active';

CREATE INDEX password_actions_principal_status_idx
    ON password_actions (principal_id, status, operation_id);

CREATE TABLE notification_outbox (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    principal_id uuid NOT NULL,
    intent text NOT NULL CHECK (intent IN ('password_setup', 'password_reset')),
    destination_email text NOT NULL CHECK (
        destination_email = lower(btrim(destination_email)) AND destination_email <> ''
    ),
    status text NOT NULL CHECK (status IN ('pending', 'claimed', 'delivered', 'cancelled', 'attention_required')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    available_at timestamptz NOT NULL,
    claimed_at timestamptz,
    delivered_at timestamptz,
    notification_id text,
    version bigint NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT notification_outbox_uuid_v7 CHECK (substring(id::text FROM 15 FOR 1) = '7'),
    CONSTRAINT notification_outbox_delivery_state CHECK (
        (status = 'pending' AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL)
        OR (status = 'claimed' AND claimed_at IS NOT NULL AND delivered_at IS NULL AND notification_id IS NULL)
        OR (status = 'delivered' AND claimed_at IS NOT NULL AND delivered_at IS NOT NULL AND notification_id IS NOT NULL)
        OR (status IN ('cancelled', 'attention_required') AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL)
    ),
    CONSTRAINT notification_outbox_action_fk FOREIGN KEY (operation_id)
        REFERENCES password_actions (operation_id) ON DELETE RESTRICT,
    CONSTRAINT notification_outbox_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

CREATE INDEX notification_outbox_dispatch_idx
    ON notification_outbox (status, available_at, id);

CREATE TABLE password_action_completions (
    idempotency_key text PRIMARY KEY CHECK (btrim(idempotency_key) <> ''),
    operation_id uuid NOT NULL UNIQUE,
    principal_id uuid NOT NULL,
    credential_version bigint NOT NULL CHECK (credential_version > 0),
    completed_at timestamptz NOT NULL,
    CONSTRAINT password_action_completions_action_fk FOREIGN KEY (operation_id)
        REFERENCES password_actions (operation_id) ON DELETE RESTRICT,
    CONSTRAINT password_action_completions_principal_fk FOREIGN KEY (principal_id)
        REFERENCES principals (id) ON DELETE RESTRICT
);

GRANT SELECT, INSERT, UPDATE ON TABLE
    password_action_requests,
    password_actions,
    notification_outbox,
    password_action_completions
TO ani_iam_runtime;

GRANT INSERT, UPDATE ON TABLE identities, password_credentials TO ani_iam_runtime;
GRANT UPDATE ON TABLE sessions, session_grants, refresh_token_families, refresh_tokens TO ani_iam_runtime;
