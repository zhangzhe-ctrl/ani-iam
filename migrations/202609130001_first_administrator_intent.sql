-- WR22 D03: immutable owner intent. This migration grants no Human creation
-- or membership authority to the separately authenticated provisioner.
CREATE TABLE first_administrator_intents (
    intent_id uuid PRIMARY KEY CHECK (substring(intent_id::text FROM 15 FOR 1)='7'),
    environment text NOT NULL REFERENCES workload_bootstrap_receipts(environment),
    normalized_email text NOT NULL CHECK (normalized_email<>'' AND normalized_email=lower(normalized_email)),
    issuer text NOT NULL CHECK (issuer<>''),
    subject text NOT NULL CHECK (subject<>''),
    intent_sha256 bytea NOT NULL CHECK (octet_length(intent_sha256)=32),
    supersedes uuid UNIQUE,
    registered_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    audit_event_id uuid NOT NULL UNIQUE REFERENCES iam_audit_events(event_id) DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (environment,intent_id),
    UNIQUE (intent_id,audit_event_id),
    CHECK (expires_at>registered_at AND expires_at<=registered_at+interval '24 hours'),
    CHECK (supersedes IS NULL OR supersedes<>intent_id),
    FOREIGN KEY (environment,supersedes) REFERENCES first_administrator_intents(environment,intent_id)
);
CREATE UNIQUE INDEX first_administrator_environment_root ON first_administrator_intents(environment) WHERE supersedes IS NULL;

CREATE TABLE first_administrator_completions (
    environment text PRIMARY KEY,
    intent_id uuid NOT NULL UNIQUE,
    principal_id uuid NOT NULL UNIQUE REFERENCES principals(id),
    audit_event_id uuid NOT NULL UNIQUE REFERENCES iam_audit_events(event_id) DEFERRABLE INITIALLY DEFERRED,
    completed_at timestamptz NOT NULL,
    FOREIGN KEY (environment,intent_id) REFERENCES first_administrator_intents(environment,intent_id)
);
GRANT SELECT,INSERT ON first_administrator_intents TO ani_iam_provisioner;
GRANT SELECT ON first_administrator_completions TO ani_iam_provisioner;
GRANT SELECT ON first_administrator_intents TO ani_iam_runtime;
GRANT SELECT,INSERT ON first_administrator_completions TO ani_iam_runtime;

ALTER TABLE iam_audit_events
    ADD COLUMN first_administrator_intent_id uuid,
    DROP CONSTRAINT iam_audit_events_authentication_method_allowed,
    DROP CONSTRAINT iam_audit_events_boundary_scope,
    DROP CONSTRAINT iam_audit_events_actor_scope,
    DROP CONSTRAINT iam_audit_events_provisioner_scope,
    ADD CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
        authentication_method IN ('password','oidc','api_key','workload_token','internal','anonymous','password_action','workload_provisioner','administrator_provisioner')
    ),
    ADD CONSTRAINT iam_audit_events_boundary_scope CHECK (
        (boundary='tenant' AND tenant_id IS NOT NULL)
        OR (boundary='principal' AND tenant_id IS NULL)
        OR (boundary='platform' AND tenant_id IS NULL AND authentication_method IN ('workload_provisioner','administrator_provisioner'))
    ),
    ADD CONSTRAINT iam_audit_events_actor_scope CHECK (
        (authentication_method IN ('anonymous','workload_provisioner','administrator_provisioner') AND actor_id IS NULL)
        OR (authentication_method NOT IN ('anonymous','workload_provisioner','administrator_provisioner') AND actor_id IS NOT NULL)
    ),
    ADD CONSTRAINT iam_audit_events_provisioner_scope CHECK (
        (authentication_method='workload_provisioner' AND provisioner_role IS NOT NULL
         AND provisioner_role='ani_iam_provisioner' AND bootstrap_manifest_id IS NOT NULL
         AND first_administrator_intent_id IS NULL AND boundary='platform' AND caller_principal_id IS NULL
         AND action='workload.bootstrap' AND target_type='workload_bootstrap'
         AND target_id=bootstrap_manifest_id AND result='succeeded')
        OR (authentication_method='administrator_provisioner' AND provisioner_role IS NOT NULL
         AND provisioner_role='ani_iam_provisioner' AND bootstrap_manifest_id IS NULL
         AND first_administrator_intent_id IS NOT NULL AND boundary='platform' AND caller_principal_id IS NULL
         AND action='administrator.intent.register' AND target_type='first_administrator_intent'
         AND target_id=first_administrator_intent_id AND result='succeeded')
        OR (authentication_method NOT IN ('workload_provisioner','administrator_provisioner')
         AND provisioner_role IS NULL AND bootstrap_manifest_id IS NULL AND first_administrator_intent_id IS NULL)
    ),
    ADD CONSTRAINT audit_first_administrator_intent_fk FOREIGN KEY (first_administrator_intent_id,event_id)
        REFERENCES first_administrator_intents(intent_id,audit_event_id) DEFERRABLE INITIALLY DEFERRED;

CREATE OR REPLACE FUNCTION guard_workload_provisioner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='principals' THEN
        IF current_user='ani_iam_provisioner' AND NEW.principal_type<>'workload' THEN
            RAISE EXCEPTION 'provisioner may create only Workload Principal' USING ERRCODE='42501';
        END IF;
    ELSIF TG_TABLE_NAME='workload_principals' THEN
        IF current_user='ani_iam_provisioner' AND NEW.owner_type<>'platform' THEN
            RAISE EXCEPTION 'provisioner may create only Platform Workload' USING ERRCODE='42501';
        END IF;
    ELSIF TG_TABLE_NAME='iam_audit_events' THEN
        IF (NEW.authentication_method IN ('workload_provisioner','administrator_provisioner')) IS DISTINCT FROM
           (current_user='ani_iam_provisioner') THEN
            RAISE EXCEPTION 'provisioner attribution must match authenticated database role' USING ERRCODE='42501';
        END IF;
    END IF;
    RETURN NEW;
END
$$;
UPDATE iam_schema_revision SET revision='202609130001' WHERE singleton;
