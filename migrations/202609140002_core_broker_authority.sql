-- D02 non-secret Platform configuration. The environment retains NKey seeds.
-- Registration is separate from Grants; runtime cannot modify either.
CREATE TABLE core_broker_bindings (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    principal_id uuid NOT NULL,
    environment text NOT NULL,
    trust_domain text NOT NULL,
    broker_name text NOT NULL CHECK (broker_name ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$'),
    account_name text NOT NULL CHECK (account_name ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$'),
    nkey_public text NOT NULL CHECK (nkey_public ~ '^U[A-Z2-7]{55}$'),
    status text NOT NULL CHECK (status IN ('active','revoked')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (broker_name,account_name,nkey_public),
    UNIQUE (id,principal_id),
    UNIQUE (id,broker_name,account_name),
    FOREIGN KEY (principal_id,environment,trust_domain)
        REFERENCES workload_principals(principal_id,environment,trust_domain) ON DELETE RESTRICT
);
CREATE INDEX core_broker_binding_principal ON core_broker_bindings(principal_id);

CREATE TABLE core_broker_routes (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    broker_name text NOT NULL,
    account_name text NOT NULL,
    stream_name text NOT NULL CHECK (stream_name ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$'),
    subject text NOT NULL CHECK (subject IN ('ani.integration.tenant.lifecycle.v1','ani.integration.tenant.iam-bootstrap.v1','ani.integration.tenant.lifecycle-heartbeat.v1')),
    schema_major integer NOT NULL CHECK (schema_major=1),
    producer text NOT NULL CHECK (producer ~ '^[a-z0-9][a-z0-9.-]{1,127}$'),
    producer_binding_id uuid NOT NULL,
    target_sha256 text NOT NULL CHECK (target_sha256 ~ '^[a-f0-9]{64}$'),
    status text NOT NULL CHECK (status IN ('active','revoked')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (broker_name,account_name,stream_name,subject,schema_major),
    FOREIGN KEY (producer_binding_id,broker_name,account_name)
        REFERENCES core_broker_bindings(id,broker_name,account_name) ON DELETE RESTRICT
);

CREATE TABLE core_broker_grants (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    principal_id uuid NOT NULL,
    binding_id uuid NOT NULL,
    route_id uuid NOT NULL REFERENCES core_broker_routes(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('publish','receive','execute')),
    status text NOT NULL CHECK (status IN ('active','revoked')),
    version bigint NOT NULL CHECK (version>0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (binding_id,route_id,action),
    UNIQUE (id,binding_id,route_id),
    FOREIGN KEY (binding_id,principal_id) REFERENCES core_broker_bindings(id,principal_id) ON DELETE RESTRICT
);

-- Revoke and business commit serialize without granting runtime UPDATE merely
-- to take a tuple lock. All authority writes take the exclusive counterpart.
CREATE FUNCTION lock_core_broker_authority_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('ani-iam:core-broker-authority',0));
    RETURN NULL;
END;
$$;
CREATE TRIGGER core_broker_binding_change BEFORE INSERT OR UPDATE OR DELETE ON core_broker_bindings
FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();
CREATE TRIGGER core_broker_route_change BEFORE INSERT OR UPDATE OR DELETE ON core_broker_routes
FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();
CREATE TRIGGER core_broker_grant_change BEFORE INSERT OR UPDATE OR DELETE ON core_broker_grants
FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();

CREATE FUNCTION lock_core_broker_principal_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id uuid;
BEGIN
    IF TG_TABLE_NAME='principals' THEN owner_id:=OLD.id; ELSE owner_id:=OLD.principal_id; END IF;
    IF EXISTS (SELECT 1 FROM core_broker_bindings WHERE principal_id=owner_id) THEN
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'bound Broker Principal cannot be erased' USING ERRCODE='23514';
        END IF;
        PERFORM pg_advisory_xact_lock(hashtextextended('ani-iam:core-broker-authority',0));
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_broker_principal_change BEFORE UPDATE OR DELETE ON principals
FOR EACH ROW EXECUTE FUNCTION lock_core_broker_principal_change();
CREATE TRIGGER core_broker_profile_change BEFORE UPDATE OR DELETE ON workload_principals
FOR EACH ROW EXECUTE FUNCTION lock_core_broker_principal_change();

CREATE FUNCTION protect_core_broker_configuration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Broker configuration is retained' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND (
        NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at OR
        (to_jsonb(NEW)-ARRAY['status','version','updated_at']) IS DISTINCT FROM
        (to_jsonb(OLD)-ARRAY['status','version','updated_at'])) THEN
        RAISE EXCEPTION 'Broker identity and target are immutable; changes require a new version' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_broker_binding_identity BEFORE UPDATE OR DELETE ON core_broker_bindings
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_configuration();
CREATE TRIGGER core_broker_route_identity BEFORE UPDATE OR DELETE ON core_broker_routes
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_configuration();
CREATE TRIGGER core_broker_grant_identity BEFORE UPDATE OR DELETE ON core_broker_grants
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_configuration();

-- Broker transport evidence is attached only to the exact durable IAM fact.
-- Global heartbeat has no fabricated Tenant; all Tenant receipts retain scope.
ALTER TABLE core_integration_receipts ADD CONSTRAINT core_integration_receipt_broker_key
    UNIQUE(producer,source_sequence,event_id);
CREATE TABLE core_broker_authority_receipts (
    consumer_id uuid NOT NULL CHECK (substring(consumer_id::text FROM 15 FOR 1)='7'),
    broker_sequence bigint NOT NULL CHECK (broker_sequence>0),
    route_id uuid NOT NULL REFERENCES core_broker_routes(id) ON DELETE RESTRICT,
    route_version bigint NOT NULL CHECK (route_version>0),
    producer text NOT NULL,
    source_sequence bigint NOT NULL,
    event_id uuid NOT NULL,
    tenant_id uuid CHECK (substring(tenant_id::text FROM 15 FOR 1)='7'),
    raw_sha256 bytea NOT NULL CHECK (octet_length(raw_sha256)=32),
    producer_principal_id uuid NOT NULL,
    producer_binding_id uuid NOT NULL,
    producer_principal_version bigint NOT NULL CHECK (producer_principal_version>0),
    producer_binding_version bigint NOT NULL CHECK (producer_binding_version>0),
    producer_grant_id uuid NOT NULL REFERENCES core_broker_grants(id) ON DELETE RESTRICT,
    producer_grant_version bigint NOT NULL CHECK (producer_grant_version>0),
    executor_principal_id uuid NOT NULL,
    executor_binding_id uuid NOT NULL,
    executor_principal_version bigint NOT NULL CHECK (executor_principal_version>0),
    executor_binding_version bigint NOT NULL CHECK (executor_binding_version>0),
    executor_grant_id uuid NOT NULL REFERENCES core_broker_grants(id) ON DELETE RESTRICT,
    executor_grant_version bigint NOT NULL CHECK (executor_grant_version>0),
    execution_grant_id uuid REFERENCES core_broker_grants(id) ON DELETE RESTRICT,
    execution_grant_version bigint CHECK (execution_grant_version>0),
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(consumer_id,broker_sequence),
    CHECK (producer_principal_id<>executor_principal_id),
    CHECK ((execution_grant_id IS NULL)=(execution_grant_version IS NULL)),
    FOREIGN KEY(producer,source_sequence,event_id) REFERENCES core_integration_receipts(producer,source_sequence,event_id),
    FOREIGN KEY(producer_binding_id,producer_principal_id) REFERENCES core_broker_bindings(id,principal_id),
    FOREIGN KEY(executor_binding_id,executor_principal_id) REFERENCES core_broker_bindings(id,principal_id),
    FOREIGN KEY(producer_grant_id,producer_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id),
    FOREIGN KEY(executor_grant_id,executor_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id),
    FOREIGN KEY(execution_grant_id,executor_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id)
);
CREATE INDEX core_broker_receipt_tenant_event ON core_broker_authority_receipts(tenant_id,event_id,consumer_id,broker_sequence);

CREATE FUNCTION protect_core_broker_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Broker authority receipt is immutable' USING ERRCODE='23514'; END IF;
    IF NOT EXISTS (SELECT 1 FROM core_integration_receipts r
        WHERE r.producer=NEW.producer AND r.source_sequence=NEW.source_sequence AND r.event_id=NEW.event_id
        AND r.tenant_id IS NOT DISTINCT FROM NEW.tenant_id AND r.raw_sha256=NEW.raw_sha256) THEN
        RAISE EXCEPTION 'Broker evidence requires exact Tenant and original bytes' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_broker_receipt_identity BEFORE INSERT OR UPDATE OR DELETE ON core_broker_authority_receipts
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_receipt();

GRANT SELECT ON core_broker_bindings,core_broker_routes,core_broker_grants TO ani_iam_runtime,ani_iam_provisioner;
GRANT INSERT ON core_broker_bindings,core_broker_routes,core_broker_grants TO ani_iam_provisioner;
GRANT UPDATE(status,version,updated_at) ON core_broker_bindings,core_broker_routes,core_broker_grants TO ani_iam_provisioner;
GRANT SELECT,INSERT ON core_broker_authority_receipts TO ani_iam_runtime;

-- Poison/denied bytes are consumer-scoped transport evidence, not a Tenant
-- fact. A payload's claimed tenant_id cannot authorize inspection or replay.
CREATE TABLE core_broker_dlq (
    consumer_id uuid NOT NULL CHECK (substring(consumer_id::text FROM 15 FOR 1)='7'),
    id uuid NOT NULL CHECK (substring(id::text FROM 15 FOR 1)='7'),
    broker_sequence bigint NOT NULL CHECK (broker_sequence>0),
    broker_name text NOT NULL,
    account_name text NOT NULL,
    stream_name text NOT NULL,
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),
    raw_payload bytea NOT NULL CHECK (octet_length(raw_payload)<=65536),
    raw_sha256 bytea NOT NULL CHECK (octet_length(raw_sha256)=32 AND raw_sha256=sha256(raw_payload)),
    headers jsonb NOT NULL CHECK (jsonb_typeof(headers)='object' AND octet_length(headers::text)<=16384),
    delivery_count bigint NOT NULL CHECK (delivery_count>0),
    published_at timestamptz NOT NULL,
    last_error text NOT NULL CHECK (last_error IN ('invalid_event','authority_denied','event_conflict')),
    quarantined_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(consumer_id,id),
    UNIQUE(consumer_id,broker_sequence)
);
CREATE FUNCTION protect_core_broker_dlq() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'DLQ original bytes are immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER core_broker_dlq_identity BEFORE INSERT OR UPDATE OR DELETE ON core_broker_dlq
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_dlq();
GRANT SELECT,INSERT ON core_broker_dlq TO ani_iam_runtime;
-- Offline reviewed owner changes have an immutable database-authenticated
-- receipt. Registration and authorization consume separate manifests.
CREATE TABLE core_broker_administration_receipts (
    id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
    manifest_sha256 bytea NOT NULL CHECK (octet_length(manifest_sha256)=32),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest)='object'),
    mode text NOT NULL CHECK (mode IN ('register','grants','bindings')),
    reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 512),
    provisioner_role text NOT NULL CHECK (provisioner_role='ani_iam_provisioner'),
    completed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER core_broker_administration_identity BEFORE UPDATE OR DELETE ON core_broker_administration_receipts
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_dlq();
GRANT SELECT,INSERT ON core_broker_administration_receipts TO ani_iam_provisioner;
GRANT SELECT ON core_broker_administration_receipts TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609140002' WHERE singleton;
