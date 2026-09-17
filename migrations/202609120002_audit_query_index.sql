-- Audit queries have no age cutoff and grant no UPDATE/DELETE privilege.
CREATE INDEX iam_audit_events_tenant_event_desc_idx
    ON iam_audit_events (tenant_id, event_id DESC) WHERE boundary='tenant';
