-- This is the durable result of one local reconciliation transaction. The
-- injected broker authority checker and dispatcher are not enabled here.
CREATE TABLE core_bootstrap_worker_results (
    tenant_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    source_event_id uuid NOT NULL,
    producer_principal_id uuid NOT NULL REFERENCES workload_principals(principal_id),
    executor_principal_id uuid NOT NULL REFERENCES workload_principals(principal_id),
    producer_version bigint NOT NULL CHECK (producer_version>0),
    executor_version bigint NOT NULL CHECK (executor_version>0),
    decision_id uuid NOT NULL CHECK (substring(decision_id::text FROM 15 FOR 1)='7'),
    role_id uuid NOT NULL,
    invitation_id uuid NOT NULL,
    delivery_id uuid NOT NULL,
    audit_id uuid NOT NULL REFERENCES iam_audit_events(event_id) DEFERRABLE INITIALLY DEFERRED,
    created_at timestamptz NOT NULL,
    PRIMARY KEY(tenant_id,operation_id),
    CHECK (producer_principal_id<>executor_principal_id),
    FOREIGN KEY(tenant_id,operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id),
    FOREIGN KEY(tenant_id,source_event_id) REFERENCES core_bootstrap_receipts(tenant_id,event_id),
    FOREIGN KEY(tenant_id,role_id) REFERENCES tenant_roles(tenant_id,id),
    FOREIGN KEY(tenant_id,invitation_id) REFERENCES tenant_invitations(tenant_id,id),
    FOREIGN KEY(tenant_id,delivery_id) REFERENCES tenant_invitation_outbox(tenant_id,id)
);
CREATE FUNCTION protect_core_bootstrap_worker_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'Bootstrap worker result is immutable' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM tenant_bootstrap_operations o
        JOIN tenant_invitations i ON i.tenant_id=o.tenant_id AND i.bootstrap_operation_id=o.id
        JOIN core_bootstrap_receipts r ON r.tenant_id=o.tenant_id AND r.operation_id=o.id
        JOIN tenant_invitation_outbox d ON d.tenant_id=i.tenant_id AND d.invitation_id=i.id
        WHERE o.tenant_id=NEW.tenant_id AND o.id=NEW.operation_id AND o.source_kind='core'
        AND o.status='waiting_for_principal_verification' AND o.superseded_by IS NULL
        AND i.id=NEW.invitation_id AND i.role_ids=ARRAY[NEW.role_id] AND i.created_by=NEW.executor_principal_id
        AND i.normalized_email=o.intended_email AND r.event_id=NEW.source_event_id AND d.id=NEW.delivery_id) THEN
        RAISE EXCEPTION 'Bootstrap result must bind the exact operation and invitation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_bootstrap_worker_result_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_worker_results
FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_worker_result();
CREATE FUNCTION require_core_bootstrap_worker_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM iam_audit_events a WHERE a.event_id=NEW.audit_id
        AND a.tenant_id=NEW.tenant_id AND a.boundary='tenant' AND a.actor_id=NEW.executor_principal_id
        AND a.authentication_method='internal' AND a.caller_principal_id IS NULL
        AND a.action='iam.bootstrap.invitation.created' AND a.target_type='tenant_bootstrap'
        AND a.target_id=NEW.operation_id AND a.target_version=2 AND a.result='succeeded'
        AND a.decision_id=NEW.decision_id::text AND a.request_id=NEW.source_event_id::text
        AND a.correlation_id=NEW.operation_id::text) THEN
        RAISE EXCEPTION 'Bootstrap effects require their exact executor audit' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER core_bootstrap_worker_audit AFTER INSERT ON core_bootstrap_worker_results
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_core_bootstrap_worker_audit();
GRANT SELECT,INSERT ON core_bootstrap_worker_results TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130011' WHERE singleton;
