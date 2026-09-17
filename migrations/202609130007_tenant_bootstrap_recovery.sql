-- IAM Bootstrap intent/projection persistence is independent of Tenant Access.
-- Only the dedicated approved recovery path creates Access in this migration's
-- application slice. Core event receipt and worker wiring remain a later gate.
ALTER TABLE tenant_lifecycle_projections DROP CONSTRAINT tenant_lifecycle_projections_tenant_fk;

CREATE TABLE tenant_bootstrap_recovery_operations (
    tenant_id uuid NOT NULL CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    target_principal_id uuid NOT NULL REFERENCES verified_emails(principal_id),
    requester_principal_id uuid NOT NULL REFERENCES verified_emails(principal_id),
    approver_principal_id uuid REFERENCES verified_emails(principal_id),
    approval_reference text UNIQUE CHECK (length(approval_reference) BETWEEN 1 AND 256),
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128 AND reason_code ~ '^[A-Z0-9_.-]+$'),
    payload_fingerprint text NOT NULL CHECK (payload_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    status text NOT NULL CHECK (status IN ('pending_approval','approved','executed','expired','rejected')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    approved_at timestamptz,
    executed_at timestamptz,
    membership_id uuid,
    PRIMARY KEY(tenant_id,id),
    UNIQUE(id),
    UNIQUE(tenant_id,id,target_principal_id),
    CHECK (updated_at>=created_at),
    CHECK (approved_at IS NULL OR (approved_at>=created_at AND approved_at<=updated_at)),
    CHECK (approver_principal_id IS NULL OR approver_principal_id<>requester_principal_id),
    CHECK ((approved_at IS NULL AND approval_reference IS NULL AND approver_principal_id IS NULL AND expires_at=created_at+interval '1 hour')
        OR (approved_at IS NOT NULL AND approval_reference IS NOT NULL AND approver_principal_id IS NOT NULL AND expires_at=approved_at+interval '1 hour')),
    CHECK (status NOT IN ('approved','executed') OR approved_at IS NOT NULL),
    CHECK (status<>'pending_approval' OR approved_at IS NULL),
    CHECK ((status='executed' AND executed_at IS NOT NULL AND membership_id IS NOT NULL AND executed_at<expires_at)
        OR (status<>'executed' AND executed_at IS NULL AND membership_id IS NULL)),
    FOREIGN KEY(tenant_id,membership_id,target_principal_id) REFERENCES tenant_memberships(tenant_id,id,principal_id)
);
CREATE FUNCTION protect_tenant_bootstrap_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'pending_approval' OR NEW.version<>1 THEN
            RAISE EXCEPTION 'Recovery must start with an unapproved intent' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.id<>OLD.id OR NEW.target_principal_id<>OLD.target_principal_id
        OR NEW.requester_principal_id<>OLD.requester_principal_id OR NEW.reason_code<>OLD.reason_code
        OR NEW.payload_fingerprint<>OLD.payload_fingerprint OR NEW.created_at<>OLD.created_at
        OR OLD.status IN ('executed','expired','rejected')
        OR (OLD.approved_at IS NOT NULL AND (NEW.approved_at IS DISTINCT FROM OLD.approved_at
            OR NEW.approver_principal_id IS DISTINCT FROM OLD.approver_principal_id
            OR NEW.approval_reference IS DISTINCT FROM OLD.approval_reference OR NEW.expires_at<>OLD.expires_at)) THEN
        RAISE EXCEPTION 'Recovery intent and consumed approval are immutable' USING ERRCODE='23514';
    END IF;
    IF NOT ((OLD.status='pending_approval' AND NEW.status IN ('approved','expired','rejected'))
        OR (OLD.status='approved' AND NEW.status IN ('executed','expired','rejected')))
        OR NEW.version<>OLD.version+1 THEN
        RAISE EXCEPTION 'Invalid recovery transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_bootstrap_recovery_identity BEFORE INSERT OR UPDATE ON tenant_bootstrap_recovery_operations
FOR EACH ROW EXECUTE FUNCTION protect_tenant_bootstrap_recovery();
GRANT SELECT,INSERT,UPDATE ON tenant_bootstrap_recovery_operations TO ani_iam_runtime;

CREATE TABLE tenant_bootstrap_operations (
    tenant_id uuid NOT NULL CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    source_kind text NOT NULL CHECK (source_kind IN ('core','recovery')),
    intended_email text NOT NULL CHECK (length(intended_email) BETWEEN 3 AND 320 AND intended_email=lower(btrim(intended_email))),
    intended_principal_id uuid REFERENCES verified_emails(principal_id),
    payload_fingerprint text NOT NULL CHECK (payload_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload)='object'),
    status text NOT NULL CHECK (status IN ('pending','waiting_for_principal_verification','succeeded','attention_required','superseded')),
    version bigint NOT NULL CHECK (version>0),
    recovery_request_id uuid,
    supersedes uuid,
    superseded_by uuid,
    membership_id uuid,
    principal_id uuid,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(tenant_id,id),
    CHECK (updated_at>=created_at),
    CHECK ((source_kind='core' AND recovery_request_id IS NULL AND intended_principal_id IS NULL)
        OR (source_kind='recovery' AND recovery_request_id IS NOT NULL AND recovery_request_id=id AND intended_principal_id IS NOT NULL)),
    CHECK ((status='succeeded' AND membership_id IS NOT NULL AND principal_id IS NOT NULL)
        OR (status<>'succeeded' AND membership_id IS NULL AND principal_id IS NULL)),
    CHECK (source_kind<>'recovery' OR principal_id=intended_principal_id),
    CHECK ((status='superseded' AND superseded_by IS NOT NULL) OR (status<>'superseded' AND superseded_by IS NULL)),
    CHECK (supersedes IS NULL OR supersedes<>id),
    CHECK (superseded_by IS NULL OR superseded_by<>id),
    FOREIGN KEY(tenant_id,recovery_request_id,intended_principal_id) REFERENCES tenant_bootstrap_recovery_operations(tenant_id,id,target_principal_id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(tenant_id,supersedes) REFERENCES tenant_bootstrap_operations(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(tenant_id,superseded_by) REFERENCES tenant_bootstrap_operations(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(tenant_id,membership_id,principal_id) REFERENCES tenant_memberships(tenant_id,id,principal_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX tenant_bootstrap_one_current ON tenant_bootstrap_operations(tenant_id) WHERE superseded_by IS NULL;
ALTER TABLE tenant_invitations ADD COLUMN bootstrap_operation_id uuid;
ALTER TABLE tenant_invitations ADD CONSTRAINT tenant_invitation_bootstrap_fk
    FOREIGN KEY(tenant_id,bootstrap_operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX tenant_invitation_one_bootstrap_intent ON tenant_invitations(tenant_id,bootstrap_operation_id) WHERE bootstrap_operation_id IS NOT NULL;

CREATE FUNCTION protect_tenant_bootstrap_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.version<>1 OR NOT ((NEW.source_kind='core' AND NEW.status='pending')
            OR (NEW.source_kind='recovery' AND NEW.status='succeeded')) THEN
            RAISE EXCEPTION 'Invalid initial Bootstrap operation' USING ERRCODE='23514';
        END IF;
        IF NEW.source_kind='recovery' AND NOT EXISTS(SELECT 1 FROM tenant_bootstrap_recovery_operations r
            WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.recovery_request_id AND r.target_principal_id=NEW.intended_principal_id
            AND r.status='approved' AND r.expires_at>statement_timestamp()) THEN
            RAISE EXCEPTION 'Bootstrap recovery requires current approval' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.id<>OLD.id OR NEW.source_kind<>OLD.source_kind
        OR NEW.intended_email<>OLD.intended_email OR NEW.intended_principal_id IS DISTINCT FROM OLD.intended_principal_id
        OR NEW.payload_fingerprint<>OLD.payload_fingerprint OR NEW.payload<>OLD.payload
        OR NEW.recovery_request_id IS DISTINCT FROM OLD.recovery_request_id OR NEW.supersedes IS DISTINCT FROM OLD.supersedes
        OR NEW.created_at<>OLD.created_at OR OLD.status IN ('succeeded','superseded')
        OR NEW.version<>OLD.version+1 THEN
        RAISE EXCEPTION 'Bootstrap identity and terminal result are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_bootstrap_operation_identity BEFORE INSERT OR UPDATE ON tenant_bootstrap_operations
FOR EACH ROW EXECUTE FUNCTION protect_tenant_bootstrap_operation();

-- These two approved recovery purposes share only immutable reference uniqueness.
CREATE TABLE iam_recovery_approval_references (
    approval_reference text PRIMARY KEY CHECK (length(approval_reference) BETWEEN 1 AND 256),
    tenant_id uuid NOT NULL,
    restore_operation_id uuid,
    bootstrap_operation_id uuid,
    created_at timestamptz NOT NULL,
    CHECK ((restore_operation_id IS NOT NULL AND bootstrap_operation_id IS NULL)
        OR (restore_operation_id IS NULL AND bootstrap_operation_id IS NOT NULL)),
    UNIQUE(tenant_id,restore_operation_id),
    UNIQUE(tenant_id,bootstrap_operation_id),
    FOREIGN KEY(tenant_id,restore_operation_id) REFERENCES tenant_admin_recovery_operations(tenant_id,id),
    FOREIGN KEY(tenant_id,bootstrap_operation_id) REFERENCES tenant_bootstrap_recovery_operations(tenant_id,id)
);
INSERT INTO iam_recovery_approval_references(approval_reference,tenant_id,restore_operation_id,created_at)
SELECT approval_reference,tenant_id,id,approved_at FROM tenant_admin_recovery_operations WHERE approval_reference IS NOT NULL;
CREATE FUNCTION require_recovery_approval_reference() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.approval_reference IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM iam_recovery_approval_references r WHERE r.approval_reference=NEW.approval_reference AND r.tenant_id=NEW.tenant_id
        AND ((TG_TABLE_NAME='tenant_admin_recovery_operations' AND r.restore_operation_id=NEW.id)
            OR (TG_TABLE_NAME='tenant_bootstrap_recovery_operations' AND r.bootstrap_operation_id=NEW.id))) THEN
        RAISE EXCEPTION 'Recovery reference must bind its exact purpose and operation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_admin_recovery_approval_reference BEFORE UPDATE ON tenant_admin_recovery_operations
FOR EACH ROW EXECUTE FUNCTION require_recovery_approval_reference();
CREATE TRIGGER tenant_bootstrap_recovery_approval_reference BEFORE UPDATE ON tenant_bootstrap_recovery_operations
FOR EACH ROW EXECUTE FUNCTION require_recovery_approval_reference();
GRANT SELECT,INSERT ON iam_recovery_approval_references TO ani_iam_runtime;
GRANT SELECT,INSERT,UPDATE ON tenant_bootstrap_operations TO ani_iam_runtime;
CREATE FUNCTION protect_invitation_bootstrap_link() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.bootstrap_operation_id IS DISTINCT FROM OLD.bootstrap_operation_id THEN
        RAISE EXCEPTION 'Invitation Bootstrap identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_invitation_bootstrap_identity BEFORE UPDATE ON tenant_invitations
FOR EACH ROW EXECUTE FUNCTION protect_invitation_bootstrap_link();
UPDATE iam_schema_revision SET revision='202609130007'  WHERE singleton;
