-- Original Bootstrap operations remain the durable work source. This table
-- owns only the init/expiry scheduling state, not identities or authority.
CREATE TABLE core_bootstrap_jobs (
 tenant_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('initialize','expire')),
 producer text NOT NULL,
 source_event_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','claimed','attention_required','done')),
 attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 20),
 lease_id uuid,
 lease_started_at timestamptz,
 lease_until timestamptz,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id,kind),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES tenant_bootstrap_operations(tenant_id,id),
 FOREIGN KEY(tenant_id,source_event_id) REFERENCES core_bootstrap_receipts(tenant_id,event_id),
 CHECK((state='claimed' AND lease_id IS NOT NULL AND lease_started_at IS NOT NULL AND lease_until>lease_started_at)
 OR (state<>'claimed' AND lease_id IS NULL AND lease_started_at IS NULL AND lease_until IS NULL))
);
CREATE INDEX core_bootstrap_job_due ON core_bootstrap_jobs(producer,available_at,tenant_id) WHERE state IN ('pending','claimed');
CREATE TABLE core_bootstrap_attempts (
 tenant_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 kind text NOT NULL,
 attempt_number integer NOT NULL CHECK(attempt_number BETWEEN 1 AND 20),
 lease_id uuid NOT NULL UNIQUE CHECK(substring(lease_id::text FROM 15 FOR 1)='7'),
 outcome text NOT NULL CHECK(outcome IN ('completed','deferred','retry','attention_required','lease_expired','recovered_completion')),
 error_code text NOT NULL CHECK(error_code IN ('','authority_unavailable','lifecycle_stale','lifecycle_blocked','invalid_intent','intent_conflict','dependency_unavailable','execution_failed','lease_expired')),
 started_at timestamptz NOT NULL,
 finished_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id,kind,attempt_number),
 FOREIGN KEY(tenant_id,operation_id,kind) REFERENCES core_bootstrap_jobs(tenant_id,operation_id,kind),
 CHECK(finished_at>=started_at)
);
CREATE FUNCTION protect_core_bootstrap_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Bootstrap work history cannot be erased' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id AND r.event_id=NEW.source_event_id AND r.producer=NEW.producer)
   OR NEW.state<>'pending' OR NEW.attempt_count<>0 THEN
   RAISE EXCEPTION 'Bootstrap job requires its original receipt' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.tenant_id,NEW.operation_id,NEW.kind,NEW.producer,NEW.source_event_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.operation_id,OLD.kind,OLD.producer,OLD.source_event_id,OLD.created_at)
  OR NEW.attempt_count<OLD.attempt_count OR NEW.attempt_count>OLD.attempt_count+1
  OR OLD.state IN ('attention_required','done')
  OR (OLD.state='claimed' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_attempts a WHERE a.tenant_id=OLD.tenant_id AND a.operation_id=OLD.operation_id AND a.kind=OLD.kind AND a.attempt_number=OLD.attempt_count AND a.lease_id=OLD.lease_id))
  OR (NEW.state='claimed' AND (OLD.state<>'pending' OR NEW.attempt_count<>OLD.attempt_count+1))
  OR (NEW.state<>'claimed' AND NEW.attempt_count<>OLD.attempt_count) THEN
  RAISE EXCEPTION 'Bootstrap job identity or lease progression changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_bootstrap_job_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_jobs FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_job();
CREATE FUNCTION protect_core_bootstrap_attempt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Bootstrap attempts are immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM core_bootstrap_jobs j WHERE j.tenant_id=NEW.tenant_id AND j.operation_id=NEW.operation_id AND j.kind=NEW.kind
  AND j.state='claimed' AND j.lease_id=NEW.lease_id AND j.attempt_count=NEW.attempt_number AND j.lease_started_at=NEW.started_at) THEN
  RAISE EXCEPTION 'Bootstrap outcome requires exact current lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_bootstrap_attempt_identity BEFORE INSERT OR UPDATE OR DELETE ON core_bootstrap_attempts FOR EACH ROW EXECUTE FUNCTION protect_core_bootstrap_attempt();
GRANT SELECT,INSERT,UPDATE ON core_bootstrap_jobs TO ani_iam_runtime;
GRANT SELECT,INSERT ON core_bootstrap_attempts TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130013' WHERE singleton;
