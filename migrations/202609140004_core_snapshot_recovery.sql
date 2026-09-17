-- Preserve expired/superseded cuts and immutable page evidence. Recovery never
-- rewrites a cursor, fabricates a missing source receipt or enables enforcement.
ALTER TABLE core_lifecycle_rebuilds DROP CONSTRAINT core_lifecycle_rebuilds_state_check;
ALTER TABLE core_lifecycle_rebuilds ADD CONSTRAINT core_lifecycle_rebuilds_state_check
 CHECK(state IN ('loading','loaded','catching_up','activated','abandoned'));
ALTER TABLE core_lifecycle_rebuilds ADD COLUMN abandoned_reason text NOT NULL DEFAULT '',
 ADD CONSTRAINT core_rebuild_abandon_reason CHECK
 ((state='abandoned' AND abandoned_reason IN ('cursor_expired','superseded_generation')) OR (state<>'abandoned' AND abandoned_reason=''));
CREATE OR REPLACE FUNCTION protect_core_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_TABLE_NAME IN ('core_lifecycle_generations','core_lifecycle_rebuild_pages') OR TG_OP='DELETE' THEN
  RAISE EXCEPTION 'Core snapshot evidence is immutable' USING ERRCODE='23514';
 END IF;
 IF ROW(NEW.producer,NEW.snapshot_id,NEW.generation_id,NEW.base_generation_id,NEW.consumer_id,NEW.source_cut,NEW.broker_after,NEW.page_size,NEW.expires_at,NEW.created_at)
  IS DISTINCT FROM ROW(OLD.producer,OLD.snapshot_id,OLD.generation_id,OLD.base_generation_id,OLD.consumer_id,OLD.source_cut,OLD.broker_after,OLD.page_size,OLD.expires_at,OLD.created_at)
  OR NEW.loaded_items<OLD.loaded_items OR NEW.applied_through<OLD.applied_through
  OR (OLD.state<>'loading' AND ROW(NEW.next_token,NEW.last_tenant_id,NEW.loaded_items) IS DISTINCT FROM ROW(OLD.next_token,OLD.last_tenant_id,OLD.loaded_items))
  OR (OLD.state IN ('activated','abandoned') AND NEW IS DISTINCT FROM OLD)
  OR (OLD.state<>'loading' AND NEW.state='loading') OR (OLD.state='catching_up' AND NEW.state='loaded')
  OR (NEW.state='abandoned' AND ROW(NEW.next_token,NEW.last_tenant_id,NEW.loaded_items,NEW.applied_through)
      IS DISTINCT FROM ROW(OLD.next_token,OLD.last_tenant_id,OLD.loaded_items,OLD.applied_through)) THEN
  RAISE EXCEPTION 'Core rebuild identity or progress changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE INDEX core_pending_rebuild ON core_lifecycle_rebuilds(producer,created_at,snapshot_id)
 WHERE state IN ('loading','loaded','catching_up');
UPDATE iam_schema_revision SET revision='202609140004' WHERE singleton;
