-- Bootstrap receipt is independent of lifecycle freshness and Tenant Access.
-- The transport/authority gate is deliberately not activated by this migration.
CREATE UNIQUE INDEX tenant_bootstrap_operation_global_id ON tenant_bootstrap_operations(id);

CREATE TABLE core_bootstrap_receipts (
    tenant_id uuid NOT NULL CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    event_id uuid NOT NULL CHECK (substring(event_id::text FROM 15 FOR 1)='7'),
    operation_id uuid NOT NULL,
    producer text NOT NULL CHECK (length(producer) BETWEEN 1 AND 128 AND producer=btrim(producer)),
    source_sequence bigint NOT NULL CHECK (source_sequence>0),
    payload_fingerprint text NOT NULL CHECK (payload_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    raw_payload bytea NOT NULL CHECK (octet_length(raw_payload) BETWEEN 1 AND 65536),
    raw_sha256 bytea NOT NULL CHECK (octet_length(raw_sha256)=32 AND raw_sha256=sha256(raw_payload)),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(tenant_id,event_id),
    UNIQUE(event_id),
    UNIQUE(producer,source_sequence),
    FOREIGN KEY(tenant_id,operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id)
);

CREATE FUNCTION protect_core_bootstrap_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'Core Bootstrap receipt is immutable' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM tenant_bootstrap_operations o
        WHERE o.tenant_id=NEW.tenant_id AND o.id=NEW.operation_id
        AND o.source_kind='core' AND o.payload_fingerprint=NEW.payload_fingerprint) THEN
        RAISE EXCEPTION 'Core Bootstrap receipt requires exact original intent' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_bootstrap_receipt_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_receipts
FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_receipt();
GRANT SELECT,INSERT ON core_bootstrap_receipts TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130009' WHERE singleton;
