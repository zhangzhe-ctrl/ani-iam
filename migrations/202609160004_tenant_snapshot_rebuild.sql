-- Snapshot's existing mTLS identity and exact Begin/Page Grants are checked
-- under the same authority fence as broker replay. Writers serialize with the
-- shared lock; runtime receives no new authority-table mutation privilege.
CREATE TRIGGER tenant_snapshot_identity_change BEFORE INSERT OR UPDATE OR DELETE ON workload_identity_bindings
 FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();
CREATE TRIGGER tenant_snapshot_grant_change BEFORE INSERT OR UPDATE OR DELETE ON workload_grants
 FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();
CREATE TRIGGER tenant_snapshot_target_change BEFORE INSERT OR UPDATE OR DELETE ON workload_target_registrations
 FOR EACH STATEMENT EXECUTE FUNCTION lock_core_broker_authority_change();

CREATE TABLE tenant_snapshot_rebuilds (
 producer text NOT NULL,
 snapshot_id uuid NOT NULL CHECK(substring(snapshot_id::text FROM 15 FOR 1)='7'),
 generation_id uuid NOT NULL,
 base_generation_id uuid,
 epoch uuid NOT NULL CHECK(substring(epoch::text FROM 15 FOR 1)='7'),
 reader_id uuid NOT NULL REFERENCES workload_principals(principal_id),
 reader_binding_id uuid NOT NULL,
 watermark bigint NOT NULL CHECK(watermark>=0),
 total_count bigint NOT NULL CHECK(total_count>=0),
 page_size integer NOT NULL CHECK(page_size BETWEEN 1 AND 100),
 captured_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 first_token text NOT NULL CHECK(octet_length(first_token) BETWEEN 32 AND 256),
 next_token text NOT NULL CHECK(next_token='' OR octet_length(next_token) BETWEEN 32 AND 256),
 last_tenant_id uuid,
 loaded_items bigint NOT NULL DEFAULT 0 CHECK(loaded_items>=0 AND loaded_items<=total_count),
 applied_through bigint NOT NULL CHECK(applied_through>=watermark),
 state text NOT NULL DEFAULT 'loading' CHECK(state IN ('loading','loaded','catching_up','activated','abandoned')),
 authority_sha256 text NOT NULL CHECK(authority_sha256 ~ '^[a-f0-9]{64}$'),
 abandoned_reason text NOT NULL DEFAULT '' CHECK(abandoned_reason IN ('','cursor_expired','authority_changed','superseded_generation','increment_gap','conflicting_fact')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(producer,snapshot_id),
 UNIQUE(producer,generation_id),
 FOREIGN KEY(producer,generation_id) REFERENCES tenant_lifecycle_generations(producer,id),
 FOREIGN KEY(producer,base_generation_id) REFERENCES tenant_lifecycle_generations(producer,id),
 FOREIGN KEY(reader_binding_id,reader_id) REFERENCES workload_identity_bindings(id,principal_id),
 CHECK(expires_at>captured_at AND expires_at<=captured_at+interval '5 minutes'),
 CHECK(state NOT IN ('loaded','catching_up','activated') OR (loaded_items=total_count AND next_token='')),
 CHECK((state='abandoned')=(abandoned_reason<>''))
);
CREATE UNIQUE INDEX tenant_snapshot_single_pending ON tenant_snapshot_rebuilds(producer) WHERE state IN ('loading','loaded','catching_up');
CREATE TABLE tenant_snapshot_pages (
 producer text NOT NULL,
 snapshot_id uuid NOT NULL,
 request_token text NOT NULL CHECK(octet_length(request_token) BETWEEN 32 AND 256),
 page_sha256 bytea NOT NULL CHECK(octet_length(page_sha256)=32),
 page_payload bytea NOT NULL CHECK(sha256(page_payload)=page_sha256 AND octet_length(page_payload) BETWEEN 1 AND 65536),
 next_token text NOT NULL CHECK(next_token='' OR octet_length(next_token) BETWEEN 32 AND 256),
 item_count integer NOT NULL CHECK(item_count BETWEEN 0 AND 100),
 received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(producer,snapshot_id,request_token),
 FOREIGN KEY(producer,snapshot_id) REFERENCES tenant_snapshot_rebuilds(producer,snapshot_id)
);
CREATE TRIGGER tenant_snapshot_page_immutable BEFORE UPDATE OR DELETE ON tenant_snapshot_pages
 FOR EACH ROW EXECUTE FUNCTION protect_tenant_lifecycle_evidence();
CREATE FUNCTION protect_tenant_snapshot_cut() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (to_jsonb(NEW)-ARRAY['state','next_token','last_tenant_id','loaded_items','applied_through','abandoned_reason','updated_at'])
 IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['state','next_token','last_tenant_id','loaded_items','applied_through','abandoned_reason','updated_at']) THEN
  RAISE EXCEPTION 'Snapshot cut and reader are immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.state IN ('activated','abandoned') OR NEW.loaded_items<OLD.loaded_items OR NEW.applied_through<OLD.applied_through
 OR (OLD.state<>'loading' AND NEW.state='loading') OR (OLD.state='catching_up' AND NEW.state='loaded') THEN
  RAISE EXCEPTION 'Snapshot progress cannot regress or reopen' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER tenant_snapshot_cut_immutable BEFORE UPDATE OR DELETE ON tenant_snapshot_rebuilds
 FOR EACH ROW EXECUTE FUNCTION protect_tenant_snapshot_cut();
GRANT SELECT,INSERT ON tenant_snapshot_pages TO ani_iam_runtime;
GRANT SELECT,INSERT ON tenant_snapshot_rebuilds TO ani_iam_runtime;
GRANT UPDATE(state,next_token,last_tenant_id,loaded_items,applied_through,abandoned_reason,updated_at) ON tenant_snapshot_rebuilds TO ani_iam_runtime;
