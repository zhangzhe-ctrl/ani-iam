-- Dedicated Platform-authorized recovery of one existing Tenant. No Bootstrap
-- or whole-Tenant lifecycle/access mutation is permitted by this operation.
CREATE TABLE tenant_admin_recovery_operations (
    tenant_id uuid NOT NULL REFERENCES tenant_access(tenant_id),
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
CREATE FUNCTION protect_tenant_admin_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER tenant_admin_recovery_identity BEFORE INSERT OR UPDATE ON tenant_admin_recovery_operations
FOR EACH ROW EXECUTE FUNCTION protect_tenant_admin_recovery();
GRANT SELECT,INSERT,UPDATE ON tenant_admin_recovery_operations TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130006' WHERE singleton;
