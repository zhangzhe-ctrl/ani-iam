-- Governance v1 has an epoch-scoped Lifecycle sequence. Heartbeat and IAM
-- Bootstrap identity are separate receipts, not slots in that sequence.
-- Historical Core relations and their immutable evidence are retained.
CREATE TABLE tenant_lifecycle_generations (
    producer text NOT NULL CHECK (length(producer) BETWEEN 1 AND 128 AND producer=btrim(producer)),
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    epoch uuid NOT NULL CHECK (substring(epoch::text FROM 15 FOR 1)='7'),
    snapshot_watermark bigint NOT NULL CHECK (snapshot_watermark>=0),
    snapshot_id uuid NOT NULL CHECK (substring(snapshot_id::text FROM 15 FOR 1)='7'),
    captured_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(producer,id),
    UNIQUE(producer,id,epoch),
    UNIQUE(producer,snapshot_id)
);

CREATE TABLE tenant_lifecycle_pipelines (
    producer text PRIMARY KEY CHECK (length(producer) BETWEEN 1 AND 128 AND producer=btrim(producer)),
    generation_id uuid,
    epoch uuid,
    applied_sequence bigint NOT NULL DEFAULT 0 CHECK (applied_sequence>=0),
    highest_sequence bigint NOT NULL DEFAULT 0 CHECK (highest_sequence>=applied_sequence),
    snapshot_required boolean NOT NULL DEFAULT true,
    heartbeat_id uuid,
    heartbeat_epoch uuid,
    committed_sequence bigint NOT NULL DEFAULT 0 CHECK (committed_sequence>=0),
    published_sequence bigint NOT NULL DEFAULT 0 CHECK (published_sequence BETWEEN 0 AND committed_sequence),
    heartbeat_observed_at timestamptz,
    heartbeat_received_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((generation_id IS NULL)=(epoch IS NULL)),
    CHECK (epoch IS NOT NULL OR (snapshot_required AND applied_sequence=0 AND highest_sequence=0)),
    CHECK ((heartbeat_id IS NULL AND heartbeat_epoch IS NULL AND heartbeat_observed_at IS NULL AND heartbeat_received_at IS NULL AND committed_sequence=0 AND published_sequence=0)
        OR (heartbeat_id IS NOT NULL AND heartbeat_epoch IS NOT NULL AND epoch IS NOT NULL AND heartbeat_epoch=epoch AND heartbeat_observed_at IS NOT NULL AND heartbeat_received_at IS NOT NULL AND heartbeat_observed_at<=heartbeat_received_at)),
    FOREIGN KEY(producer,generation_id,epoch) REFERENCES tenant_lifecycle_generations(producer,id,epoch)
);

CREATE TABLE tenant_integration_receipts (
    producer text NOT NULL REFERENCES tenant_lifecycle_pipelines(producer),
    event_id uuid NOT NULL CHECK (substring(event_id::text FROM 15 FOR 1)='7'),
    epoch uuid NOT NULL CHECK (substring(epoch::text FROM 15 FOR 1)='7'),
    kind text NOT NULL CHECK (kind IN ('lifecycle','heartbeat','bootstrap')),
    source_sequence bigint NOT NULL CHECK (source_sequence>=0),
    tenant_id uuid CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    raw_payload bytea NOT NULL CHECK (octet_length(raw_payload) BETWEEN 1 AND 65536),
    raw_sha256 bytea NOT NULL CHECK (octet_length(raw_sha256)=32 AND raw_sha256=sha256(raw_payload)),
    domain_sha256 bytea NOT NULL CHECK (octet_length(domain_sha256)=32),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    outcome text NOT NULL CHECK (outcome IN ('applied','covered','epoch_requires_snapshot','sequence_gap','tenant_version_gap','awaiting_snapshot','observed','older_heartbeat','bootstrap')),
    tenant_version bigint,
    business_status text,
    reason text,
    effective_at timestamptz,
    committed_sequence bigint,
    published_sequence bigint,
    bootstrap_operation_id uuid,
    PRIMARY KEY(producer,event_id),
    UNIQUE(producer,epoch,event_id),
    UNIQUE(tenant_id,producer,event_id),
    FOREIGN KEY(tenant_id,bootstrap_operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id),
    CHECK ((kind='lifecycle' AND tenant_id IS NOT NULL AND source_sequence>0 AND tenant_version IS NOT NULL AND tenant_version>0 AND business_status IS NOT NULL AND business_status IN ('active','frozen','disabled')
             AND reason IS NOT NULL AND length(btrim(reason)) BETWEEN 1 AND 1024 AND effective_at IS NOT NULL AND effective_at=occurred_at AND committed_sequence IS NULL AND published_sequence IS NULL AND bootstrap_operation_id IS NULL)
        OR (kind='heartbeat' AND tenant_id IS NULL AND source_sequence=0 AND tenant_version IS NULL AND business_status IS NULL AND reason IS NULL AND effective_at IS NULL
             AND committed_sequence IS NOT NULL AND committed_sequence>=0 AND published_sequence IS NOT NULL AND published_sequence BETWEEN 0 AND committed_sequence AND bootstrap_operation_id IS NULL)
        OR (kind='bootstrap' AND tenant_id IS NOT NULL AND source_sequence>0 AND tenant_version IS NULL AND business_status IS NULL AND reason IS NULL AND effective_at IS NULL
             AND committed_sequence IS NULL AND published_sequence IS NULL AND bootstrap_operation_id IS NOT NULL))
);
CREATE UNIQUE INDEX tenant_lifecycle_stream_position ON tenant_integration_receipts(producer,epoch,source_sequence) WHERE kind='lifecycle';

CREATE TABLE tenant_lifecycle_facts (
    producer text NOT NULL,
    generation_id uuid NOT NULL,
    tenant_id uuid NOT NULL CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    tenant_version bigint NOT NULL CHECK (tenant_version>0),
    business_status text NOT NULL CHECK (business_status IN ('active','frozen','disabled')),
    reason text NOT NULL DEFAULT '',
    effective_at timestamptz,
    snapshot_base boolean NOT NULL,
    PRIMARY KEY(producer,generation_id,tenant_id),
    FOREIGN KEY(producer,generation_id) REFERENCES tenant_lifecycle_generations(producer,id),
    CHECK (snapshot_base OR (length(btrim(reason)) BETWEEN 1 AND 1024 AND effective_at IS NOT NULL))
);

CREATE FUNCTION protect_tenant_lifecycle_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Tenant lifecycle evidence is immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_lifecycle_generation_immutable BEFORE INSERT OR UPDATE OR DELETE ON tenant_lifecycle_generations
FOR EACH ROW EXECUTE FUNCTION protect_tenant_lifecycle_evidence();
CREATE TRIGGER tenant_integration_receipt_immutable BEFORE INSERT OR UPDATE OR DELETE ON tenant_integration_receipts
FOR EACH ROW EXECUTE FUNCTION protect_tenant_lifecycle_evidence();

CREATE FUNCTION require_tenant_integration_fact() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE document jsonb;
BEGIN
    document=convert_from(NEW.raw_payload,'UTF8')::jsonb;
    IF document->>'producer' IS DISTINCT FROM NEW.producer THEN
        RAISE EXCEPTION 'Receipt producer differs from original bytes' USING ERRCODE='23514';
    END IF;
    IF NEW.kind='heartbeat' THEN
        IF document->>'heartbeat_id' IS DISTINCT FROM NEW.event_id::text OR document->>'epoch' IS DISTINCT FROM NEW.epoch::text
            OR (document->>'committed_sequence')::bigint IS DISTINCT FROM NEW.committed_sequence
            OR (document->>'published_sequence')::bigint IS DISTINCT FROM NEW.published_sequence
            OR (document->>'observed_at')::timestamptz IS DISTINCT FROM NEW.occurred_at THEN
            RAISE EXCEPTION 'Heartbeat receipt differs from original bytes' USING ERRCODE='23514';
        END IF;
    ELSE
        IF document->>'event_id' IS DISTINCT FROM NEW.event_id::text OR document->>'tenant_id' IS DISTINCT FROM NEW.tenant_id::text THEN
            RAISE EXCEPTION 'Tenant receipt differs from original bytes' USING ERRCODE='23514';
        END IF;
        IF NEW.kind='lifecycle' THEN
            IF document->>'epoch' IS DISTINCT FROM NEW.epoch::text OR (document->>'sequence')::bigint IS DISTINCT FROM NEW.source_sequence
                OR (document->>'tenant_version')::bigint IS DISTINCT FROM NEW.tenant_version OR document->>'business_status' IS DISTINCT FROM NEW.business_status
                OR document->>'reason' IS DISTINCT FROM NEW.reason OR (document->>'effective_at')::timestamptz IS DISTINCT FROM NEW.effective_at
                OR (document->>'occurred_at')::timestamptz IS DISTINCT FROM NEW.occurred_at THEN
                RAISE EXCEPTION 'Lifecycle receipt differs from original bytes' USING ERRCODE='23514';
            END IF;
        ELSE
            IF document->>'source_epoch' IS DISTINCT FROM NEW.epoch::text OR (document->>'lifecycle_sequence')::bigint IS DISTINCT FROM NEW.source_sequence
                OR document->>'operation_id' IS DISTINCT FROM NEW.bootstrap_operation_id::text OR (document->>'occurred_at')::timestamptz IS DISTINCT FROM NEW.occurred_at
                OR NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts b WHERE b.tenant_id=NEW.tenant_id AND b.event_id=NEW.event_id
                    AND b.operation_id=NEW.bootstrap_operation_id AND b.producer=NEW.producer AND b.raw_payload=NEW.raw_payload AND b.raw_sha256=NEW.raw_sha256) THEN
                RAISE EXCEPTION 'Bootstrap receipt requires exact original Tenant intent' USING ERRCODE='23514';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_integration_fact_binding BEFORE INSERT ON tenant_integration_receipts
FOR EACH ROW EXECUTE FUNCTION require_tenant_integration_fact();

-- This view is an observation seam until the target receiver, rebuild and
-- composition root are wired. It does not replace the current reader yet.
CREATE VIEW tenant_current_lifecycle_facts WITH (security_invoker=true) AS
SELECT f.*,p.epoch,p.applied_sequence,p.highest_sequence,p.snapshot_required,
       CASE WHEN NOT p.snapshot_required AND p.applied_sequence=p.highest_sequence
                  AND p.applied_sequence>=p.committed_sequence AND p.heartbeat_epoch=p.epoch
                  AND p.heartbeat_observed_at IS NOT NULL AND p.heartbeat_received_at IS NOT NULL
                  AND p.heartbeat_observed_at<=statement_timestamp() AND p.heartbeat_received_at<=statement_timestamp()
            THEN least(p.heartbeat_observed_at,p.heartbeat_received_at)+interval '30 seconds'
            ELSE '-infinity'::timestamptz END AS fresh_until
FROM tenant_lifecycle_facts f JOIN tenant_lifecycle_pipelines p
  ON p.producer=f.producer AND p.generation_id=f.generation_id;

GRANT SELECT,INSERT ON tenant_lifecycle_generations,tenant_integration_receipts TO ani_iam_runtime;
GRANT SELECT,INSERT,UPDATE ON tenant_lifecycle_pipelines,tenant_lifecycle_facts TO ani_iam_runtime;
GRANT SELECT ON tenant_current_lifecycle_facts TO ani_iam_runtime;
