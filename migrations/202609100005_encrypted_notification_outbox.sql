-- WR20 candidate installs only into a fresh isolated database. Existing outbox
-- data requires a separately authorized conversion; never discard or guess it.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM notification_outbox) OR EXISTS (SELECT 1 FROM password_action_completions) THEN
  RAISE EXCEPTION 'WR20 encrypted outbox requires an empty table; owner conversion is not authorized';
 END IF;
END $$;
ALTER TABLE notification_outbox DROP COLUMN destination_email;
ALTER TABLE notification_outbox ADD COLUMN destination_key_version text;
ALTER TABLE notification_outbox ADD COLUMN destination_ciphertext bytea;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_payload CHECK (
 (status IN ('delivered','cancelled') AND destination_key_version IS NULL AND destination_ciphertext IS NULL)
 OR (status IN ('pending','claimed','attention_required') AND destination_key_version IS NOT NULL
     AND length(destination_key_version) BETWEEN 1 AND 64 AND destination_ciphertext IS NOT NULL AND octet_length(destination_ciphertext)>=29)
);
UPDATE iam_schema_revision SET revision = '202609100005' WHERE singleton;

ALTER TABLE password_action_completions ADD COLUMN request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint)=32);
