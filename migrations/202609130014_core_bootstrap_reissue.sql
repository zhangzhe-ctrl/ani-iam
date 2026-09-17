-- Each invitation delivery generation has its own immutable scheduling
-- history. Reissue cannot rewrite a completed job or reuse an old lease.
ALTER TABLE core_bootstrap_jobs ADD COLUMN generation bigint NOT NULL DEFAULT 1 CHECK(generation>0 AND (kind='expire' OR generation=1));
ALTER TABLE core_bootstrap_attempts ADD COLUMN generation bigint NOT NULL DEFAULT 1 CHECK(generation>0);
ALTER TABLE core_bootstrap_attempts DROP CONSTRAINT core_bootstrap_attempts_tenant_id_operation_id_kind_fkey;
ALTER TABLE core_bootstrap_attempts DROP CONSTRAINT core_bootstrap_attempts_pkey;
ALTER TABLE core_bootstrap_jobs DROP CONSTRAINT core_bootstrap_jobs_pkey;
ALTER TABLE core_bootstrap_jobs ADD PRIMARY KEY(tenant_id,operation_id,kind,generation);
ALTER TABLE core_bootstrap_attempts ADD PRIMARY KEY(tenant_id,operation_id,kind,generation,attempt_number);
ALTER TABLE core_bootstrap_attempts ADD FOREIGN KEY(tenant_id,operation_id,kind,generation) REFERENCES core_bootstrap_jobs(tenant_id,operation_id,kind,generation);
CREATE OR REPLACE FUNCTION protect_core_bootstrap_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Bootstrap work history cannot be erased' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NOT EXISTS(SELECT 1 FROM core_bootstrap_receipts r WHERE r.tenant_id=NEW.tenant_id AND r.operation_id=NEW.operation_id AND r.event_id=NEW.source_event_id AND r.producer=NEW.producer)
   OR NEW.generation<1 OR (NEW.kind='initialize' AND NEW.generation<>1)
   OR (NEW.kind='expire' AND NOT EXISTS(SELECT 1 FROM core_bootstrap_worker_results w JOIN tenant_invitations i ON i.tenant_id=w.tenant_id AND i.id=w.invitation_id WHERE w.tenant_id=NEW.tenant_id AND w.operation_id=NEW.operation_id AND i.delivery_generation=NEW.generation))
   OR NEW.state<>'pending' OR NEW.attempt_count<>0 THEN
   RAISE EXCEPTION 'Bootstrap job requires its original receipt' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.tenant_id,NEW.operation_id,NEW.kind,NEW.generation,NEW.producer,NEW.source_event_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.operation_id,OLD.kind,OLD.generation,OLD.producer,OLD.source_event_id,OLD.created_at)
  OR NEW.attempt_count<OLD.attempt_count OR NEW.attempt_count>OLD.attempt_count+1
  OR OLD.state IN ('attention_required','done')
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
  AND j.state='claimed' AND j.lease_id=NEW.lease_id AND j.attempt_count=NEW.attempt_number AND j.lease_started_at=NEW.started_at) THEN
  RAISE EXCEPTION 'Bootstrap outcome requires exact current lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Extend the finite Platform mutation receipt catalog for this operation.
ALTER TABLE platform_mutation_results DROP CONSTRAINT platform_mutation_results_operation_check;
ALTER TABLE platform_mutation_results ADD CONSTRAINT platform_mutation_results_operation_check CHECK (operation IN (
 'createPlatformIAMRole','updatePlatformIAMRole','deletePlatformIAMRole',
 'updatePlatformIAMMember','removePlatformIAMMember','bindPlatformIAMRole','unbindPlatformIAMRole',
 'updateTenantAccess','createPlatformIAMInvitation','cancelPlatformIAMInvitation','resendPlatformIAMInvitation',
 'acceptPlatformIAMInvitation','requestRecoveryBootstrap','approveRecoveryBootstrap','executeRecoveryBootstrap',
 'requestRestoreTenantAdmin','approveRestoreTenantAdmin','executeRestoreTenantAdmin',
 'reissueTenantIAMBootstrapInvitation'));

-- New finite Platform permissions are catalogued, never auto-bound to a Role.
INSERT INTO permission_catalog(scope,resource,action) VALUES
 ('platform', 'iam.tenant-bootstrap', 'read'),
 ('platform', 'iam.tenant-bootstrap', 'reissue');
UPDATE iam_schema_revision SET revision='202609130014' WHERE singleton;
