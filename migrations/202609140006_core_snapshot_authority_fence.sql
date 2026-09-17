-- A pending cut cannot span a Broker authority revocation/restoration cycle.
-- Empty fingerprints belong only to historical isolated component inputs;
-- the formal repository always supplies the exact current authority digest.
ALTER TABLE core_lifecycle_rebuilds ADD COLUMN broker_authority_sha256 text NOT NULL DEFAULT ''
 CHECK(broker_authority_sha256='' OR broker_authority_sha256 ~ '^[0-9a-f]{64}$');
ALTER TABLE core_lifecycle_rebuilds DROP CONSTRAINT core_rebuild_abandon_reason;
ALTER TABLE core_lifecycle_rebuilds ADD CONSTRAINT core_rebuild_abandon_reason CHECK
 ((state='abandoned' AND abandoned_reason IN ('cursor_expired','superseded_generation','authority_changed')) OR (state<>'abandoned' AND abandoned_reason=''));
CREATE FUNCTION protect_core_snapshot_authority() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.broker_authority_sha256 IS DISTINCT FROM OLD.broker_authority_sha256 THEN
  RAISE EXCEPTION 'Core snapshot authority is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_rebuild_authority_identity BEFORE UPDATE ON core_lifecycle_rebuilds
 FOR EACH ROW EXECUTE FUNCTION protect_core_snapshot_authority();
UPDATE iam_schema_revision SET revision='202609140006' WHERE singleton;
