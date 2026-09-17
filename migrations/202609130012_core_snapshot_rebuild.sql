-- Snapshot bases have exactly the accepted ID/status/version fields. Unknown
-- reason/effective time remain absent until a matching full event supplies them.
ALTER TABLE core_lifecycle_projection_rows ADD COLUMN snapshot_base boolean NOT NULL DEFAULT false;
DO $$ DECLARE c text; n integer:=0; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='core_lifecycle_projection_rows'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%effective_at IS NOT NULL%' LOOP
  EXECUTE format('ALTER TABLE core_lifecycle_projection_rows DROP CONSTRAINT %I',c); n:=n+1;
 END LOOP;
 IF n<>1 THEN RAISE EXCEPTION 'Expected one full lifecycle fact constraint'; END IF;
END $$;
ALTER TABLE core_lifecycle_projection_rows ADD CONSTRAINT core_projection_fact_shape CHECK (
 (lifecycle_version=0 AND status='' AND reason='' AND effective_at IS NULL AND repair_required AND NOT snapshot_base)
 OR (lifecycle_version>0 AND status<>'' AND ((snapshot_base AND reason='' AND effective_at IS NULL) OR (NOT snapshot_base AND reason<>'' AND effective_at IS NOT NULL))));
CREATE OR REPLACE FUNCTION protect_core_projection_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.producer<>OLD.producer OR NEW.generation_id<>OLD.generation_id OR NEW.tenant_id<>OLD.tenant_id
  OR NEW.lifecycle_version<OLD.lifecycle_version OR NEW.required_version<OLD.required_version
  OR (OLD.repair_required AND NOT NEW.repair_required)
  OR (NOT OLD.snapshot_base AND NEW.snapshot_base)
  OR (NEW.lifecycle_version=OLD.lifecycle_version AND (NEW.status<>OLD.status
    OR (NOT OLD.snapshot_base AND (NEW.reason<>OLD.reason OR NEW.effective_at IS DISTINCT FROM OLD.effective_at)))) THEN
  RAISE EXCEPTION 'Projection identity and unresolved gap cannot be rewritten' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

ALTER TABLE core_integration_receipts ADD COLUMN lifecycle_version bigint,
 ADD COLUMN lifecycle_status text, ADD COLUMN lifecycle_reason text, ADD COLUMN lifecycle_effective_at timestamptz;
ALTER TABLE core_integration_receipts DROP CONSTRAINT core_integration_receipts_outcome_check;
ALTER TABLE core_integration_receipts ADD CONSTRAINT core_integration_receipts_outcome_check CHECK
 (outcome IN ('applied','same_version','older','version_gap','awaiting_snapshot','heartbeat','bootstrap','snapshot_covered'));
-- Existing immutable receipts are not rewritten. Rebuild fails closed if a
-- required legacy Lifecycle receipt lacks decoded fields.
ALTER TABLE core_integration_receipts ADD CONSTRAINT core_receipt_lifecycle_shape CHECK (
 (lifecycle_version IS NULL AND lifecycle_status IS NULL AND lifecycle_reason IS NULL AND lifecycle_effective_at IS NULL)
 OR (kind='lifecycle' AND lifecycle_version>0 AND lifecycle_status IN ('active','frozen','disabled') AND lifecycle_reason<>'' AND lifecycle_effective_at IS NOT NULL));
CREATE FUNCTION require_core_receipt_lifecycle_fields() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.kind='lifecycle' AND (NEW.lifecycle_version IS NULL OR NEW.lifecycle_status IS NULL OR NEW.lifecycle_reason IS NULL OR NEW.lifecycle_effective_at IS NULL) THEN
  RAISE EXCEPTION 'Lifecycle receipt requires replayable decoded fields' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_receipt_lifecycle_fields BEFORE INSERT ON core_integration_receipts FOR EACH ROW EXECUTE FUNCTION require_core_receipt_lifecycle_fields();

CREATE TABLE core_lifecycle_rebuilds (
 producer text NOT NULL,
 snapshot_id uuid NOT NULL CHECK(snapshot_id<>'00000000-0000-0000-0000-000000000000'),
 generation_id uuid NOT NULL,
 base_generation_id uuid NOT NULL,
 consumer_id uuid NOT NULL CHECK(substring(consumer_id::text FROM 15 FOR 1)='7'),
 source_cut bigint NOT NULL CHECK(source_cut>=0),
 broker_after bigint NOT NULL CHECK(broker_after>=0),
 page_size integer NOT NULL CHECK(page_size BETWEEN 1 AND 500),
 expires_at timestamptz NOT NULL,
 next_token text NOT NULL DEFAULT '',
 last_tenant_id uuid,
 loaded_items bigint NOT NULL DEFAULT 0 CHECK(loaded_items>=0),
 applied_through bigint NOT NULL CHECK(applied_through>=source_cut),
 state text NOT NULL DEFAULT 'loading' CHECK(state IN ('loading','loaded','catching_up','activated')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(producer,snapshot_id),
 UNIQUE(producer,generation_id),
 FOREIGN KEY(producer,generation_id) REFERENCES core_lifecycle_generations(producer,id),
 FOREIGN KEY(producer,base_generation_id) REFERENCES core_lifecycle_generations(producer,id)
);
CREATE TABLE core_lifecycle_rebuild_pages (
 producer text NOT NULL,
 snapshot_id uuid NOT NULL,
 request_token text NOT NULL,
 page_fingerprint bytea NOT NULL CHECK(octet_length(page_fingerprint)=32),
 next_token text NOT NULL,
 item_count integer NOT NULL CHECK(item_count BETWEEN 0 AND 500),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(producer,snapshot_id,request_token),
 FOREIGN KEY(producer,snapshot_id) REFERENCES core_lifecycle_rebuilds(producer,snapshot_id)
);
CREATE FUNCTION protect_core_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_TABLE_NAME IN ('core_lifecycle_generations','core_lifecycle_rebuild_pages') OR TG_OP='DELETE' THEN
  RAISE EXCEPTION 'Core snapshot evidence is immutable' USING ERRCODE='23514';
 END IF;
 IF ROW(NEW.producer,NEW.snapshot_id,NEW.generation_id,NEW.base_generation_id,NEW.consumer_id,NEW.source_cut,NEW.broker_after,NEW.page_size,NEW.expires_at,NEW.created_at)
  IS DISTINCT FROM ROW(OLD.producer,OLD.snapshot_id,OLD.generation_id,OLD.base_generation_id,OLD.consumer_id,OLD.source_cut,OLD.broker_after,OLD.page_size,OLD.expires_at,OLD.created_at)
  OR NEW.loaded_items<OLD.loaded_items OR NEW.applied_through<OLD.applied_through
  OR (OLD.state<>'loading' AND ROW(NEW.next_token,NEW.last_tenant_id,NEW.loaded_items) IS DISTINCT FROM ROW(OLD.next_token,OLD.last_tenant_id,OLD.loaded_items))
  OR (OLD.state='activated' AND NEW IS DISTINCT FROM OLD)
  OR (OLD.state<>'loading' AND NEW.state='loading') OR (OLD.state='catching_up' AND NEW.state='loaded') THEN
  RAISE EXCEPTION 'Core rebuild identity or progress changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_generation_immutable BEFORE UPDATE OR DELETE ON core_lifecycle_generations FOR EACH ROW EXECUTE FUNCTION protect_core_snapshot_record();
CREATE TRIGGER core_rebuild_page_immutable BEFORE UPDATE OR DELETE ON core_lifecycle_rebuild_pages FOR EACH ROW EXECUTE FUNCTION protect_core_snapshot_record();
CREATE TRIGGER core_rebuild_identity BEFORE UPDATE OR DELETE ON core_lifecycle_rebuilds FOR EACH ROW EXECUTE FUNCTION protect_core_snapshot_record();
GRANT SELECT,INSERT ON core_lifecycle_rebuild_pages TO ani_iam_runtime;
GRANT SELECT,INSERT,UPDATE ON core_lifecycle_rebuilds TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130012' WHERE singleton;
