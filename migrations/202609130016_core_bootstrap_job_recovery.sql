-- A technical retry continues the original job and its immutable source.
-- Each authorized recovery begins another bounded cycle without resetting history.
ALTER TABLE core_bootstrap_jobs DROP CONSTRAINT core_bootstrap_jobs_attempt_count_check;
ALTER TABLE core_bootstrap_jobs ADD COLUMN cycle_start_attempt integer NOT NULL DEFAULT 0;
ALTER TABLE core_bootstrap_jobs ADD COLUMN recovery_id uuid;
ALTER TABLE core_bootstrap_jobs ADD CHECK(cycle_start_attempt>=0 AND attempt_count>=cycle_start_attempt AND attempt_count-cycle_start_attempt<=20);
ALTER TABLE core_bootstrap_attempts DROP CONSTRAINT core_bootstrap_attempts_attempt_number_check;
ALTER TABLE core_bootstrap_attempts ADD CHECK(attempt_number>0);
ALTER TABLE core_bootstrap_attempts ADD COLUMN recovery_id uuid;

CREATE TABLE core_bootstrap_job_recoveries (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text FROM 15 FOR 1)='7'),
 operation_id uuid NOT NULL,
 kind text NOT NULL,
 generation bigint NOT NULL,
 source_event_id uuid NOT NULL,
 producer text NOT NULL,
 payload_fingerprint text NOT NULL,
 previous_attempt integer NOT NULL CHECK(previous_attempt>0),
 previous_recovery_id uuid,
 actor_id uuid NOT NULL REFERENCES principals(id),
 reason_code text NOT NULL CHECK(reason_code ~ '^[A-Z0-9_.-]{1,128}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,operation_id,kind,generation,previous_attempt),
 FOREIGN KEY(tenant_id,operation_id,kind,generation) REFERENCES core_bootstrap_jobs(tenant_id,operation_id,kind,generation),
 FOREIGN KEY(tenant_id,source_event_id) REFERENCES core_bootstrap_receipts(tenant_id,event_id),
 FOREIGN KEY(tenant_id,previous_recovery_id) REFERENCES core_bootstrap_job_recoveries(tenant_id,id)
);
ALTER TABLE core_bootstrap_jobs ADD FOREIGN KEY(tenant_id,recovery_id) REFERENCES core_bootstrap_job_recoveries(tenant_id,id);
ALTER TABLE core_bootstrap_attempts ADD FOREIGN KEY(tenant_id,recovery_id) REFERENCES core_bootstrap_job_recoveries(tenant_id,id);

CREATE FUNCTION protect_core_bootstrap_job_recovery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Bootstrap job recovery is immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM core_bootstrap_jobs j JOIN tenant_bootstrap_operations o ON o.tenant_id=j.tenant_id AND o.id=j.operation_id
  WHERE j.tenant_id=NEW.tenant_id AND j.operation_id=NEW.operation_id AND j.kind=NEW.kind AND j.generation=NEW.generation
  AND j.source_event_id=NEW.source_event_id AND j.producer=NEW.producer AND o.payload_fingerprint=NEW.payload_fingerprint
  AND j.state='attention_required' AND j.attempt_count=NEW.previous_attempt AND j.recovery_id IS NOT DISTINCT FROM NEW.previous_recovery_id
  AND o.source_kind='core' AND o.superseded_by IS NULL
  AND ((j.kind='initialize' AND o.status='pending') OR (j.kind='expire' AND o.status='waiting_for_principal_verification'
   AND EXISTS(SELECT 1 FROM tenant_invitations i WHERE i.tenant_id=j.tenant_id AND i.bootstrap_operation_id=j.operation_id AND i.delivery_generation=j.generation AND i.status IN ('pending','expired'))))) THEN
  RAISE EXCEPTION 'Bootstrap recovery requires the current original job' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_bootstrap_job_recovery_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_job_recoveries FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_job_recovery();

CREATE OR REPLACE FUNCTION protect_core_bootstrap_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Bootstrap work history cannot be erased' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id AND r.event_id=NEW.source_event_id AND r.producer=NEW.producer)
   OR NEW.generation<1 OR (NEW.kind='initialize' AND NEW.generation<>1)
   OR (NEW.kind='expire' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_worker_results w JOIN tenant_invitations i ON i.tenant_id=w.tenant_id AND i.id=w.invitation_id WHERE w.tenant_id=NEW.tenant_id AND w.operation_id=NEW.operation_id AND i.delivery_generation=NEW.generation))
   OR NEW.state<>'pending' OR NEW.attempt_count<>0 OR NEW.cycle_start_attempt<>0 OR NEW.recovery_id IS NOT NULL THEN
   RAISE EXCEPTION 'Bootstrap job requires its original receipt' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.tenant_id,NEW.operation_id,NEW.kind,NEW.generation,NEW.producer,NEW.source_event_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.operation_id,OLD.kind,OLD.generation,OLD.producer,OLD.source_event_id,OLD.created_at) THEN
  RAISE EXCEPTION 'Bootstrap job source cannot change' USING ERRCODE='23514';
 END IF;
 IF OLD.state='attention_required' THEN
  IF NEW.state<>'pending' OR NEW.attempt_count<>OLD.attempt_count OR NEW.cycle_start_attempt<>OLD.attempt_count
   OR NEW.recovery_id IS NULL OR NEW.recovery_id IS NOT DISTINCT FROM OLD.recovery_id
   OR NOT EXISTS(SELECT 1 FROM core_bootstrap_job_recoveries r WHERE r.tenant_id=OLD.tenant_id AND r.id=NEW.recovery_id AND r.operation_id=OLD.operation_id AND r.kind=OLD.kind AND r.generation=OLD.generation AND r.previous_attempt=OLD.attempt_count AND r.previous_recovery_id IS NOT DISTINCT FROM OLD.recovery_id) THEN
   RAISE EXCEPTION 'Bootstrap job recovery requires immutable authorization evidence' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.state='done' OR NEW.cycle_start_attempt<>OLD.cycle_start_attempt OR NEW.recovery_id IS DISTINCT FROM OLD.recovery_id
  OR NEW.attempt_count<OLD.attempt_count OR NEW.attempt_count>OLD.attempt_count+1
  OR (OLD.state='claimed' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_attempts a WHERE a.tenant_id=OLD.tenant_id AND a.operation_id=OLD.operation_id AND a.kind=OLD.kind AND a.generation=OLD.generation AND a.attempt_number=OLD.attempt_count AND a.lease_id=OLD.lease_id))
  OR (NEW.state='claimed' AND (OLD.state<>'pending' OR NEW.attempt_count<>OLD.attempt_count+1))
  OR (NEW.state<>'claimed' AND NEW.attempt_count<>OLD.attempt_count) THEN
  RAISE EXCEPTION 'Bootstrap job identity or lease progression changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION protect_core_bootstrap_attempt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Bootstrap attempts are immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM core_bootstrap_jobs j WHERE j.tenant_id=NEW.tenant_id AND j.operation_id=NEW.operation_id AND j.kind=NEW.kind AND j.generation=NEW.generation
  AND j.state='claimed' AND j.lease_id=NEW.lease_id AND j.attempt_count=NEW.attempt_number AND j.lease_started_at=NEW.started_at AND j.recovery_id IS NOT DISTINCT FROM NEW.recovery_id) THEN
  RAISE EXCEPTION 'Bootstrap outcome requires exact current lease and recovery' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
GRANT SELECT,INSERT ON core_bootstrap_job_recoveries TO ani_iam_runtime;

ALTER TABLE platform_mutation_results DROP CONSTRAINT platform_mutation_results_operation_check;
ALTER TABLE platform_mutation_results ADD CONSTRAINT platform_mutation_results_operation_check CHECK(operation IN (
 'createPlatformIAMRole','updatePlatformIAMRole','deletePlatformIAMRole',
 'updatePlatformIAMMember','removePlatformIAMMember','bindPlatformIAMRole','unbindPlatformIAMRole',
 'updateTenantAccess','createPlatformIAMInvitation','cancelPlatformIAMInvitation','resendPlatformIAMInvitation',
 'acceptPlatformIAMInvitation','requestRecoveryBootstrap','approveRecoveryBootstrap','executeRecoveryBootstrap',
 'requestRestoreTenantAdmin','approveRestoreTenantAdmin','executeRestoreTenantAdmin',
 'reissueTenantIAMBootstrapInvitation','retryTenantIAMBootstrapJob'));
INSERT INTO permission_catalog(scope,resource,action) VALUES ('platform', 'iam.tenant-bootstrap', 'retry');
UPDATE iam_schema_revision SET revision='202609130016' WHERE singleton;
