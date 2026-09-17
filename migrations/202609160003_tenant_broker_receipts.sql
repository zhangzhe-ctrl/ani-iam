-- The historical Core receipt/approval tables are retained unchanged. New
-- runtime deliveries bind only to TenantLifecycle receipts. Broker identities,
-- exact routes and current authority still use the existing security module.
ALTER TABLE tenant_integration_receipts ADD UNIQUE(producer,event_id,epoch);
ALTER TABLE core_broker_routes DROP CONSTRAINT core_broker_routes_subject_check;
ALTER TABLE core_broker_routes ADD CONSTRAINT core_broker_routes_subject_check CHECK(subject IN (
 'ani.integration.tenant.lifecycle.v1','ani.integration.tenant.iam-bootstrap.v1','ani.integration.tenant.lifecycle-heartbeat.v1',
 'governance.tenant.lifecycle.v1','iam.tenant.bootstrap.v1','governance.tenant.heartbeat.v1'));
-- Existing historical configuration remains auditable; no new old-protocol
-- registration is accepted. The application only accepts the three new routes.
CREATE FUNCTION require_tenant_broker_route() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.subject NOT IN ('governance.tenant.lifecycle.v1','iam.tenant.bootstrap.v1','governance.tenant.heartbeat.v1') THEN
  RAISE EXCEPTION 'only the current Tenant owner contract can be registered' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER tenant_broker_route_contract BEFORE INSERT ON core_broker_routes
 FOR EACH ROW EXECUTE FUNCTION require_tenant_broker_route();

-- Bootstrap keeps the existing invitation/worker storage shape. source_kind
-- 'core' is a historical storage discriminator, not a wire owner or decoder.
-- Epoch makes the creating Lifecycle reference unambiguous across owner restart.
ALTER TABLE core_bootstrap_receipts ADD COLUMN source_epoch uuid CHECK(substring(source_epoch::text FROM 15 FOR 1)='7');
ALTER TABLE core_bootstrap_receipts DROP CONSTRAINT core_bootstrap_receipts_producer_source_sequence_key;
CREATE UNIQUE INDEX core_bootstrap_historical_position ON core_bootstrap_receipts(producer,source_sequence) WHERE source_epoch IS NULL;
CREATE UNIQUE INDEX tenant_bootstrap_epoch_position ON core_bootstrap_receipts(producer,source_epoch,source_sequence) WHERE source_epoch IS NOT NULL;
CREATE FUNCTION bind_tenant_bootstrap_epoch() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE document jsonb; BEGIN
 IF NEW.source_epoch IS NOT NULL THEN
  document:=convert_from(NEW.raw_payload,'UTF8')::jsonb;
  IF document->>'source_epoch' IS DISTINCT FROM NEW.source_epoch::text THEN
   RAISE EXCEPTION 'Bootstrap receipt epoch must match original bytes' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER tenant_bootstrap_epoch BEFORE INSERT ON core_bootstrap_receipts
 FOR EACH ROW EXECUTE FUNCTION bind_tenant_bootstrap_epoch();

CREATE TABLE tenant_broker_authority_receipts (
    consumer_id uuid NOT NULL CHECK (substring(consumer_id::text FROM 15 FOR 1)='7'),
    broker_sequence bigint NOT NULL CHECK (broker_sequence>0),
    route_id uuid NOT NULL REFERENCES core_broker_routes(id) ON DELETE RESTRICT,
    route_version bigint NOT NULL CHECK (route_version>0),
    producer text NOT NULL,
    source_sequence bigint NOT NULL CHECK(source_sequence>=0),
    epoch uuid NOT NULL CHECK(substring(epoch::text FROM 15 FOR 1)='7'),
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
    UNIQUE(tenant_id,consumer_id,broker_sequence),
    CHECK (producer_principal_id<>executor_principal_id),
    CHECK ((execution_grant_id IS NULL)=(execution_grant_version IS NULL)),
    FOREIGN KEY(producer,event_id,epoch) REFERENCES tenant_integration_receipts(producer,event_id,epoch),
    FOREIGN KEY(producer_binding_id,producer_principal_id) REFERENCES core_broker_bindings(id,principal_id),
    FOREIGN KEY(executor_binding_id,executor_principal_id) REFERENCES core_broker_bindings(id,principal_id),
    FOREIGN KEY(producer_grant_id,producer_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id),
    FOREIGN KEY(executor_grant_id,executor_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id),
    FOREIGN KEY(execution_grant_id,executor_binding_id,route_id) REFERENCES core_broker_grants(id,binding_id,route_id)
);
CREATE INDEX tenant_broker_receipt_tenant_event ON tenant_broker_authority_receipts(tenant_id,event_id,consumer_id,broker_sequence);

CREATE FUNCTION protect_tenant_broker_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Broker authority receipt is immutable' USING ERRCODE='23514'; END IF;
    IF NOT EXISTS (SELECT 1 FROM tenant_integration_receipts r
        WHERE r.producer=NEW.producer AND r.epoch=NEW.epoch AND r.source_sequence=NEW.source_sequence AND r.event_id=NEW.event_id
        AND r.tenant_id IS NOT DISTINCT FROM NEW.tenant_id AND r.raw_sha256=NEW.raw_sha256) THEN
        RAISE EXCEPTION 'Broker evidence requires exact Tenant and original bytes' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tenant_broker_receipt_identity BEFORE INSERT OR UPDATE OR DELETE ON tenant_broker_authority_receipts
FOR EACH ROW EXECUTE FUNCTION protect_tenant_broker_receipt();

GRANT SELECT,INSERT ON tenant_broker_authority_receipts TO ani_iam_runtime;

CREATE TABLE tenant_bootstrap_broker_approvals (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text FROM 15 FOR 1)='7'),
 consumer_id uuid NOT NULL,
 broker_sequence bigint NOT NULL,
 source_event_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('initialize','expire')),
 generation bigint NOT NULL CHECK(generation>0 AND (kind='expire' OR generation=1)),
 authority_sha256 text NOT NULL CHECK(authority_sha256 ~ '^[a-f0-9]{64}$'),
 recovery_id uuid,
 effect_audit_id uuid,
 actor_id uuid NOT NULL REFERENCES principals(id),
 reason_code text NOT NULL CHECK(reason_code ~ '^[A-Z0-9_.-]{1,128}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,consumer_id,broker_sequence) REFERENCES tenant_broker_authority_receipts(tenant_id,consumer_id,broker_sequence),
 FOREIGN KEY(tenant_id,source_event_id) REFERENCES core_bootstrap_receipts(tenant_id,event_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id),
 FOREIGN KEY(tenant_id,recovery_id) REFERENCES core_bootstrap_job_recoveries(tenant_id,id),
 FOREIGN KEY(tenant_id,effect_audit_id) REFERENCES iam_audit_events(tenant_id,event_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((recovery_id IS NOT NULL AND recovery_id=id AND effect_audit_id IS NULL) OR (effect_audit_id IS NOT NULL AND effect_audit_id=id AND recovery_id IS NULL AND kind='expire'))
);
CREATE FUNCTION protect_tenant_bootstrap_broker_approval() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Bootstrap broker approval is immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM principals WHERE id=NEW.actor_id AND principal_type='human' AND status='active')
 OR NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r JOIN tenant_broker_authority_receipts a
   ON a.tenant_id=r.tenant_id AND a.event_id=r.event_id
   WHERE r.tenant_id=NEW.tenant_id AND r.event_id=NEW.source_event_id AND r.operation_id=NEW.operation_id
   AND a.consumer_id=NEW.consumer_id AND a.broker_sequence=NEW.broker_sequence)
 OR (NEW.recovery_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM core_bootstrap_job_recoveries r
   WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.recovery_id AND r.operation_id=NEW.operation_id
   AND r.source_event_id=NEW.source_event_id AND r.kind=NEW.kind AND r.generation=NEW.generation
   AND r.actor_id=NEW.actor_id AND r.reason_code=NEW.reason_code))
 OR (NEW.effect_audit_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM tenant_invitations i
   WHERE i.tenant_id=NEW.tenant_id AND i.bootstrap_operation_id=NEW.operation_id
   AND i.delivery_generation=NEW.generation AND i.status='pending')) THEN
  RAISE EXCEPTION 'Bootstrap broker approval requires the original authorized recovery' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER tenant_bootstrap_broker_approval_identity BEFORE INSERT OR UPDATE OR DELETE ON tenant_bootstrap_broker_approvals FOR EACH ROW EXECUTE FUNCTION protect_tenant_bootstrap_broker_approval();
GRANT SELECT,INSERT ON tenant_bootstrap_broker_approvals TO ani_iam_runtime;
