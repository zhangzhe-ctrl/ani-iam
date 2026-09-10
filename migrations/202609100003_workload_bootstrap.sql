-- WR-19: the environment owner creates ani_iam_provisioner independently.
-- The runtime neither owns this credential nor obtains its table privileges.
GRANT USAGE ON SCHEMA public TO ani_iam_provisioner;

ALTER TABLE workload_grants
    DROP CONSTRAINT workload_grants_audience_check,
    DROP CONSTRAINT workload_grants_operation_check,
    DROP CONSTRAINT workload_grants_scope_check,
    ADD CONSTRAINT workload_grants_target CHECK (
        (audience = 'ani-iam' AND scope = 'iam_ingress'
         AND operation ~ '^/(iam[.]v1[.][A-Za-z]+Service/[A-Za-z]+|grpc[.]health[.]v1[.]Health/Check)$')
        OR (audience = 'ani-session-gateway' AND scope = 'delegated_session' AND operation = 'session.create')
    );

ALTER TABLE iam_audit_events
    ADD COLUMN provisioner_role text,
    ADD COLUMN bootstrap_manifest_id uuid,
    DROP CONSTRAINT iam_audit_events_authentication_method_allowed,
    DROP CONSTRAINT iam_audit_events_boundary_scope,
    DROP CONSTRAINT iam_audit_events_actor_scope,
    ADD CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
        authentication_method IN ('password','oidc','api_key','workload_token','internal','anonymous','password_action','workload_provisioner')
    ),
    ADD CONSTRAINT iam_audit_events_boundary_scope CHECK (
        (boundary = 'tenant' AND tenant_id IS NOT NULL)
        OR (boundary = 'principal' AND tenant_id IS NULL)
        OR (boundary = 'platform' AND tenant_id IS NULL AND authentication_method = 'workload_provisioner')
    ),
    ADD CONSTRAINT iam_audit_events_actor_scope CHECK (
        (authentication_method IN ('anonymous','workload_provisioner') AND actor_id IS NULL)
        OR (authentication_method NOT IN ('anonymous','workload_provisioner') AND actor_id IS NOT NULL)
    ),
    ADD CONSTRAINT iam_audit_events_provisioner_scope CHECK (
        (authentication_method = 'workload_provisioner' AND provisioner_role IS NOT NULL
         AND provisioner_role = 'ani_iam_provisioner' AND bootstrap_manifest_id IS NOT NULL
         AND boundary = 'platform' AND caller_principal_id IS NULL
         AND action = 'workload.bootstrap' AND target_type = 'workload_bootstrap'
         AND target_id = bootstrap_manifest_id AND result = 'succeeded')
        OR (authentication_method <> 'workload_provisioner'
            AND provisioner_role IS NULL AND bootstrap_manifest_id IS NULL)
    );

CREATE TABLE workload_bootstrap_receipts (
    manifest_id uuid PRIMARY KEY CHECK (substring(manifest_id::text FROM 15 FOR 1) = '7'),
    environment text NOT NULL UNIQUE CHECK (environment <> ''),
    trust_domain text NOT NULL CHECK (trust_domain <> ''),
    ca_sha256 bytea NOT NULL CHECK (octet_length(ca_sha256) = 32),
    intent_sha256 bytea NOT NULL CHECK (octet_length(intent_sha256) = 32),
    receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    audit_event_id uuid NOT NULL UNIQUE REFERENCES iam_audit_events(event_id)
        ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    completed_at timestamptz NOT NULL
);
ALTER TABLE iam_audit_events ADD CONSTRAINT audit_bootstrap_receipt_fk
    FOREIGN KEY (bootstrap_manifest_id) REFERENCES workload_bootstrap_receipts(manifest_id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION guard_workload_provisioner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'principals' THEN
        IF current_user = 'ani_iam_provisioner' AND NEW.principal_type <> 'workload' THEN
            RAISE EXCEPTION 'provisioner may create only Workload Principal' USING ERRCODE = '42501';
        END IF;
    ELSIF TG_TABLE_NAME = 'workload_principals' THEN
        IF current_user = 'ani_iam_provisioner' AND NEW.owner_type <> 'platform' THEN
            RAISE EXCEPTION 'provisioner may create only Platform Workload' USING ERRCODE = '42501';
        END IF;
    ELSIF TG_TABLE_NAME = 'iam_audit_events' THEN
        IF (NEW.authentication_method = 'workload_provisioner') IS DISTINCT FROM
           (current_user = 'ani_iam_provisioner') THEN
            RAISE EXCEPTION 'provisioner attribution must match authenticated database role' USING ERRCODE = '42501';
        END IF;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER bootstrap_principal_guard BEFORE INSERT ON principals
    FOR EACH ROW EXECUTE FUNCTION guard_workload_provisioner();
CREATE TRIGGER bootstrap_profile_guard BEFORE INSERT ON workload_principals
    FOR EACH ROW EXECUTE FUNCTION guard_workload_provisioner();
CREATE TRIGGER bootstrap_audit_guard BEFORE INSERT ON iam_audit_events
    FOR EACH ROW EXECUTE FUNCTION guard_workload_provisioner();

GRANT SELECT, INSERT ON principals, workload_principals, workload_identity_bindings,
    workload_grants, workload_bootstrap_receipts, iam_audit_events TO ani_iam_provisioner;
-- Existing deferred Workload-relation guards need to inspect Membership rows.
-- They do not confer any Tenant write authority or access to credentials.
GRANT SELECT ON tenant_memberships TO ani_iam_provisioner;

UPDATE iam_schema_revision SET revision = '202609100003' WHERE singleton;
