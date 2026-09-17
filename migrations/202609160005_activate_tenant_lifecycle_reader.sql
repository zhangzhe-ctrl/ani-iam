-- The generic authorization reader keeps its established row contract. Its
-- single source becomes the reviewed TenantLifecycle pipeline. Historical Core
-- data is retained, with no UNION, fallback or service-name authorization rule.
CREATE OR REPLACE VIEW current_tenant_lifecycle WITH (security_invoker=true) AS
SELECT producer,generation_id,tenant_id,tenant_version AS lifecycle_version,
 tenant_version AS required_version,business_status AS status,reason,effective_at,
 snapshot_required AS repair_required,snapshot_base,tenant_version AS version,
 applied_sequence AS contiguous_sequence,highest_sequence,fresh_until
FROM tenant_current_lifecycle_facts WHERE producer=current_setting('ani_iam.tenant_producer',true);
UPDATE iam_schema_revision SET revision='202609160005' WHERE singleton;

-- A new Bootstrap integration receipt must bind the epoch-aware original;
-- the nullable historical field never supplies new runtime authority.
CREATE FUNCTION require_tenant_bootstrap_receipt_epoch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.kind='bootstrap' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r
  WHERE r.tenant_id=NEW.tenant_id AND r.event_id=NEW.event_id AND r.operation_id=NEW.bootstrap_operation_id
   AND r.producer=NEW.producer AND r.source_epoch=NEW.epoch AND r.source_sequence=NEW.source_sequence) THEN
  RAISE EXCEPTION 'Bootstrap integration requires exact Tenant epoch reference' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER tenant_bootstrap_receipt_epoch BEFORE INSERT ON tenant_integration_receipts
 FOR EACH ROW EXECUTE FUNCTION require_tenant_bootstrap_receipt_epoch();
