-- One live projection read, selected from the process's reviewed Core producer
-- setting and its atomically active generation. No fallback or dual writer.
CREATE VIEW core_current_lifecycle_facts WITH (security_invoker=true) AS
SELECT r.*,r.lifecycle_version AS version,
       p.contiguous_sequence,p.highest_sequence,
       CASE WHEN r.lifecycle_version>0 AND NOT r.repair_required
                  AND p.contiguous_sequence=p.highest_sequence AND p.progress_at IS NOT NULL
            THEN p.progress_at+interval '30 seconds'
            ELSE '-infinity'::timestamptz END AS fresh_until
FROM core_lifecycle_projection_rows r JOIN core_lifecycle_pipelines p
 ON p.producer=r.producer AND p.generation_id=r.generation_id;
CREATE VIEW current_tenant_lifecycle WITH (security_invoker=true) AS
SELECT * FROM core_current_lifecycle_facts WHERE producer=current_setting('ani_iam.core_producer',true);
GRANT SELECT ON core_current_lifecycle_facts,current_tenant_lifecycle TO ani_iam_runtime;
-- Retain the historical relation without using or rewriting its data. A caller
-- cannot make missing current state appear fresh through a historical seed.
REVOKE SELECT ON tenant_lifecycle_projections FROM ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609140003' WHERE singleton;
