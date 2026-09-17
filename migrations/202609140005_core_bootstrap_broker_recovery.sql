-- A restored credential never changes an old receipt. An existing audited
-- Platform Retry/Reissue may approve one new execution generation instead.
ALTER TABLE core_broker_authority_receipts ADD UNIQUE(tenant_id,consumer_id,broker_sequence);
ALTER TABLE iam_audit_events ADD UNIQUE(tenant_id,event_id);
CREATE TABLE core_bootstrap_broker_approvals (
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
 FOREIGN KEY(tenant_id,consumer_id,broker_sequence) REFERENCES core_broker_authority_receipts(tenant_id,consumer_id,broker_sequence),
 FOREIGN KEY(tenant_id,source_event_id) REFERENCES core_bootstrap_receipts(tenant_id,event_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id),
 FOREIGN KEY(tenant_id,recovery_id) REFERENCES core_bootstrap_job_recoveries(tenant_id,id),
 FOREIGN KEY(tenant_id,effect_audit_id) REFERENCES iam_audit_events(tenant_id,event_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((recovery_id IS NOT NULL AND recovery_id=id AND effect_audit_id IS NULL) OR (effect_audit_id IS NOT NULL AND effect_audit_id=id AND recovery_id IS NULL AND kind='expire'))
);
CREATE FUNCTION protect_core_bootstrap_broker_approval() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Bootstrap broker approval is immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM principals WHERE id=NEW.actor_id AND principal_type='human' AND status='active')
 OR NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r JOIN core_broker_authority_receipts a
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
CREATE TRIGGER core_bootstrap_broker_approval_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_broker_approvals FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_broker_approval();
GRANT SELECT,INSERT ON core_bootstrap_broker_approvals TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609140005' WHERE singleton;
