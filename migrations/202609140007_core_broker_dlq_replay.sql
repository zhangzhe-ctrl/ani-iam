-- Immutable receiver attribution; no historical record is backfilled.
CREATE TABLE core_broker_dlq_context (
 consumer_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 configuration bytea NOT NULL CHECK(octet_length(configuration) BETWEEN 1 AND 16384),
 configuration_sha256 bytea NOT NULL CHECK(octet_length(configuration_sha256)=32 AND configuration_sha256=sha256(configuration)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,entry_id),
 FOREIGN KEY(consumer_id,entry_id) REFERENCES core_broker_dlq(consumer_id,id)
);
CREATE TRIGGER core_broker_dlq_context_identity BEFORE UPDATE OR DELETE ON core_broker_dlq_context
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_dlq();

-- Completed synchronous requests, not a queue or a durable Human approval.
CREATE TABLE core_broker_dlq_attempts (
 consumer_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text FROM 15 FOR 1)='7'),
 attempt_number bigint NOT NULL CHECK(attempt_number>0),
 raw_sha256 bytea NOT NULL CHECK(octet_length(raw_sha256)=32),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 actor_id uuid NOT NULL REFERENCES principals(id),
 reason_code text NOT NULL CHECK(reason_code ~ '^[A-Z0-9_.-]{1,128}$'),
 outcome text NOT NULL CHECK(outcome IN ('received','duplicate','covered','failed')),
 projection_outcome text NOT NULL DEFAULT '',
 error_code text NOT NULL DEFAULT '' CHECK(length(error_code)<=64),
 authority_sha256 text NOT NULL DEFAULT '' CHECK(authority_sha256='' OR authority_sha256 ~ '^[0-9a-f]{64}$'),
 audit_event_id uuid NOT NULL UNIQUE REFERENCES iam_audit_events(event_id) DEFERRABLE INITIALLY DEFERRED,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(consumer_id,entry_id,attempt_number),
 UNIQUE(id),
 FOREIGN KEY(consumer_id,entry_id) REFERENCES core_broker_dlq(consumer_id,id),
 CHECK((outcome='failed')=(error_code<>'')),
 CHECK(outcome='failed' OR authority_sha256<>'')
);
CREATE FUNCTION protect_core_broker_dlq_attempt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'DLQ attempts are immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM core_broker_dlq d WHERE d.consumer_id=NEW.consumer_id AND d.id=NEW.entry_id AND d.raw_sha256=NEW.raw_sha256)
  OR NEW.attempt_number<>(SELECT COALESCE(max(a.attempt_number),0)+1 FROM core_broker_dlq_attempts a WHERE a.consumer_id=NEW.consumer_id AND a.entry_id=NEW.entry_id)
  OR NOT EXISTS(SELECT 1 FROM iam_audit_events e WHERE e.event_id=NEW.audit_event_id AND e.actor_id=NEW.actor_id AND e.boundary='platform'
     AND e.action='iam.platform.replayCoreIAMDLQEntry' AND e.target_id=NEW.entry_id AND e.target_version=NEW.attempt_number
     AND e.reason=NEW.reason_code AND ((NEW.outcome='failed' AND e.result IN ('failed','denied')) OR (NEW.outcome<>'failed' AND e.result='succeeded'))) THEN
  RAISE EXCEPTION 'DLQ attempt requires original source and matching audit' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_broker_dlq_attempt_identity BEFORE INSERT OR UPDATE OR DELETE ON core_broker_dlq_attempts
FOR EACH ROW EXECUTE FUNCTION protect_core_broker_dlq_attempt();
GRANT SELECT,INSERT ON core_broker_dlq_context,core_broker_dlq_attempts TO ani_iam_runtime;

INSERT INTO permission_catalog(scope,resource,action) VALUES ('platform','iam.dlq','read'),('platform','iam.dlq','replay');
ALTER TABLE platform_mutation_results DROP CONSTRAINT platform_mutation_results_operation_check;
ALTER TABLE platform_mutation_results ADD CONSTRAINT platform_mutation_results_operation_check CHECK(operation IN (
 'createPlatformIAMRole','updatePlatformIAMRole','deletePlatformIAMRole',
 'updatePlatformIAMMember','removePlatformIAMMember','bindPlatformIAMRole','unbindPlatformIAMRole',
 'updateTenantAccess','createPlatformIAMInvitation','cancelPlatformIAMInvitation','resendPlatformIAMInvitation',
 'acceptPlatformIAMInvitation','requestRecoveryBootstrap','approveRecoveryBootstrap','executeRecoveryBootstrap',
 'requestRestoreTenantAdmin','approveRestoreTenantAdmin','executeRestoreTenantAdmin',
 'reissueTenantIAMBootstrapInvitation','retryTenantIAMBootstrapJob','replayCoreIAMDLQEntry'));
UPDATE iam_schema_revision SET revision='202609140007' WHERE singleton;
