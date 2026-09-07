-- DP2-07 permits explicit OIDC identities and records the authentication time
-- that a later access-token refresh must not advance.

ALTER TABLE identities
    DROP CONSTRAINT identities_provider_check,
    ADD CONSTRAINT identities_provider_check CHECK (provider IN ('password', 'dex'));

ALTER TABLE sessions
    ADD COLUMN reauthenticated_at timestamptz,
    ADD COLUMN authn_methods text[];

UPDATE sessions
SET reauthenticated_at = created_at
WHERE reauthenticated_at IS NULL;

UPDATE sessions
SET authn_methods = ARRAY['password']::text[]
WHERE authn_methods IS NULL;

ALTER TABLE sessions
	ALTER COLUMN reauthenticated_at SET NOT NULL,
	ALTER COLUMN authn_methods SET NOT NULL,
	ADD CONSTRAINT sessions_reauthentication_time CHECK (
		reauthenticated_at >= created_at AND reauthenticated_at <= updated_at
	),
	ADD CONSTRAINT sessions_authn_methods_allowed CHECK (
		cardinality(authn_methods) = 1
		AND authn_methods[1] IN ('password', 'oidc')
	);
