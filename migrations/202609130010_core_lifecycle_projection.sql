-- Shadow generations do not mutate the current authorization projection.
-- Activation requires the separate authenticated/rebuild/real-24h gate.
CREATE TABLE core_lifecycle_pipelines (
    producer text PRIMARY KEY CHECK (length(producer) BETWEEN 1 AND 128 AND producer=btrim(producer)),
    generation_id uuid NOT NULL CHECK (substring(generation_id::text FROM 15 FOR 1)='7'),
    contiguous_sequence bigint NOT NULL DEFAULT 0 CHECK (contiguous_sequence>=0),
    highest_sequence bigint NOT NULL DEFAULT 0 CHECK (highest_sequence>=contiguous_sequence),
    progress_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE core_lifecycle_generations (
    producer text NOT NULL REFERENCES core_lifecycle_pipelines(producer) DEFERRABLE INITIALLY DEFERRED,
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    snapshot_source_cut bigint NOT NULL DEFAULT 0 CHECK (snapshot_source_cut>=0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(producer,id)
);
ALTER TABLE core_lifecycle_pipelines ADD CONSTRAINT core_pipeline_generation_fk
    FOREIGN KEY(producer,generation_id) REFERENCES core_lifecycle_generations(producer,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE core_lifecycle_projection_rows (
    producer text NOT NULL,
    generation_id uuid NOT NULL,
    tenant_id uuid NOT NULL CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    lifecycle_version bigint NOT NULL CHECK (lifecycle_version>=0),
    required_version bigint NOT NULL CHECK (required_version>=lifecycle_version),
    status text NOT NULL CHECK (status IN ('','active','frozen','disabled')),
    reason text NOT NULL,
    effective_at timestamptz,
    repair_required boolean NOT NULL,
    PRIMARY KEY(producer,generation_id,tenant_id),
    FOREIGN KEY(producer,generation_id) REFERENCES core_lifecycle_generations(producer,id),
    CHECK ((lifecycle_version=0 AND status='' AND reason='' AND effective_at IS NULL AND repair_required)
        OR (lifecycle_version>0 AND status<>'' AND reason<>'' AND effective_at IS NOT NULL)),
    CHECK (repair_required OR required_version=lifecycle_version)
);

-- Heartbeat is a global pipeline fact and therefore has no fabricated Tenant.
-- Tenant facts keep their Tenant ID in this shared immutable event envelope.
CREATE TABLE core_integration_receipts (
    producer text NOT NULL REFERENCES core_lifecycle_pipelines(producer),
    event_id uuid NOT NULL UNIQUE CHECK (substring(event_id::text FROM 15 FOR 1)='7'),
    source_sequence bigint NOT NULL CHECK (source_sequence>0),
    kind text NOT NULL CHECK (kind IN ('lifecycle','bootstrap','heartbeat')),
    tenant_id uuid CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    raw_payload bytea NOT NULL CHECK (octet_length(raw_payload) BETWEEN 1 AND 65536),
    raw_sha256 bytea NOT NULL CHECK (octet_length(raw_sha256)=32 AND raw_sha256=sha256(raw_payload)),
    domain_fingerprint bytea NOT NULL CHECK (octet_length(domain_fingerprint)=32),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    outcome text NOT NULL CHECK (outcome IN ('applied','same_version','older','version_gap','awaiting_snapshot','heartbeat','bootstrap')),
    PRIMARY KEY(producer,source_sequence),
    CHECK ((kind='heartbeat' AND tenant_id IS NULL) OR (kind<>'heartbeat' AND tenant_id IS NOT NULL))
);
CREATE FUNCTION protect_core_integration_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'Core source receipt is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW.kind='bootstrap' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts b
        WHERE b.tenant_id=NEW.tenant_id AND b.event_id=NEW.event_id AND b.producer=NEW.producer
        AND b.source_sequence=NEW.source_sequence AND b.raw_payload=NEW.raw_payload AND b.occurred_at=NEW.occurred_at) THEN
        RAISE EXCEPTION 'Bootstrap source progress requires its durable exact intent receipt' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_integration_receipt_identity BEFORE INSERT OR UPDATE OR DELETE ON core_integration_receipts
FOR EACH ROW EXECUTE FUNCTION protect_core_integration_receipt();

CREATE FUNCTION protect_core_projection_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.producer<>OLD.producer OR NEW.generation_id<>OLD.generation_id OR NEW.tenant_id<>OLD.tenant_id
        OR NEW.lifecycle_version<OLD.lifecycle_version OR NEW.required_version<OLD.required_version
        OR (OLD.repair_required AND NOT NEW.repair_required)
        OR (NEW.lifecycle_version=OLD.lifecycle_version AND (NEW.status<>OLD.status OR NEW.reason<>OLD.reason OR NEW.effective_at IS DISTINCT FROM OLD.effective_at)) THEN
        RAISE EXCEPTION 'Projection identity and unresolved gap cannot be rewritten' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_projection_identity BEFORE UPDATE ON core_lifecycle_projection_rows
FOR EACH ROW EXECUTE FUNCTION protect_core_projection_identity();
GRANT SELECT,INSERT ON core_integration_receipts,core_lifecycle_generations TO ani_iam_runtime;
GRANT SELECT,INSERT,UPDATE ON core_lifecycle_pipelines,core_lifecycle_projection_rows TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130010' WHERE singleton;
