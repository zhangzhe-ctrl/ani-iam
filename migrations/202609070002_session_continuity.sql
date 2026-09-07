-- DP2-08 makes one Refresh Token active per Family and records the exact
-- replacement edge while retaining consumed digests through Session expiry.

ALTER TABLE refresh_tokens
    ADD COLUMN replaced_by uuid,
    ADD CONSTRAINT refresh_tokens_consumed_replacement CHECK (
        (status = 'consumed' AND consumed_at IS NOT NULL AND replaced_by IS NOT NULL)
        OR (status <> 'consumed' AND consumed_at IS NULL AND replaced_by IS NULL)
    ),
    ADD CONSTRAINT refresh_tokens_replacement_fk
        FOREIGN KEY (tenant_id, replaced_by)
        REFERENCES refresh_tokens (tenant_id, id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX refresh_tokens_one_active_family_idx
    ON refresh_tokens (tenant_id, family_id)
    WHERE status = 'active';
