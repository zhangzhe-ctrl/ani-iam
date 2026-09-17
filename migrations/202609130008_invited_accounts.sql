-- Global Human onboarding intent. It has no Tenant authority or Membership.
CREATE TABLE iam_invited_account_verifications (
 id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
 account_digest bytea NOT NULL CHECK (octet_length(account_digest)=32),
 normalized_email text CHECK (normalized_email=lower(btrim(normalized_email)) AND length(normalized_email) BETWEEN 3 AND 320),
 code_key_version text,
 code_digest bytea,
 caller_principal_id uuid NOT NULL REFERENCES principals(id),
 request_idempotency_key text NOT NULL UNIQUE CHECK (length(request_idempotency_key) BETWEEN 1 AND 128),
 status text NOT NULL CHECK (status IN ('pending','superseded','consumed','exhausted','expired')),
 failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
 version bigint NOT NULL CHECK (version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 principal_id uuid REFERENCES verified_emails(principal_id),
 completion_key text UNIQUE CHECK (length(completion_key) BETWEEN 1 AND 128),
 completion_intent bytea CHECK (octet_length(completion_intent)=32),
 CHECK (expires_at=created_at+interval '10 minutes' AND updated_at>=created_at),
 CHECK ((normalized_email IS NULL AND code_key_version IS NULL AND code_digest IS NULL)
  OR (normalized_email IS NOT NULL AND code_key_version IS NOT NULL AND length(code_key_version) BETWEEN 1 AND 64 AND code_digest IS NOT NULL AND octet_length(code_digest)=32)),
 CHECK ((status='consumed' AND principal_id IS NOT NULL AND completion_key IS NOT NULL AND completion_intent IS NOT NULL AND normalized_email IS NOT NULL AND updated_at<expires_at AND failed_attempts<5)
  OR (status<>'consumed' AND principal_id IS NULL AND completion_key IS NULL AND completion_intent IS NULL)),
 CHECK ((status='exhausted' AND failed_attempts=5) OR (status<>'exhausted' AND failed_attempts<5))
);
CREATE INDEX tenant_invitation_pending_recipient ON tenant_invitations(normalized_email,tenant_id) WHERE status='pending';
CREATE UNIQUE INDEX iam_invited_account_one_pending ON iam_invited_account_verifications(account_digest) WHERE status='pending';

CREATE FUNCTION protect_invited_account_verification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending' OR NEW.version<>1 OR NEW.failed_attempts<>0 THEN
   RAISE EXCEPTION 'Invalid initial verification intent' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.id,NEW.account_digest,NEW.normalized_email,NEW.code_key_version,NEW.code_digest,NEW.caller_principal_id,NEW.request_idempotency_key,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM (OLD.id,OLD.account_digest,OLD.normalized_email,OLD.code_key_version,OLD.code_digest,OLD.caller_principal_id,OLD.request_idempotency_key,OLD.created_at,OLD.expires_at)
  OR OLD.status<>'pending' OR NEW.version<>OLD.version+1 OR NEW.failed_attempts<OLD.failed_attempts OR NEW.failed_attempts>OLD.failed_attempts+1
  OR (NEW.status='pending' AND NEW.failed_attempts<>OLD.failed_attempts+1)
  OR (NEW.status='consumed' AND NEW.failed_attempts<>OLD.failed_attempts) THEN
  RAISE EXCEPTION 'Verification identity or consumed intent is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER invited_account_verification_identity BEFORE INSERT OR UPDATE ON iam_invited_account_verifications FOR EACH ROW EXECUTE FUNCTION protect_invited_account_verification();

CREATE TABLE iam_invited_account_outbox (
 id uuid PRIMARY KEY CHECK (substring(id::text FROM 15 FOR 1)='7'),
 challenge_id uuid NOT NULL UNIQUE REFERENCES iam_invited_account_verifications(id),
 payload_key_version text,
 payload_ciphertext bytea,
 status text NOT NULL CHECK (status IN ('pending','claimed','delivered','cancelled','attention_required')),
 attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count>=0),
 available_at timestamptz NOT NULL,
 claimed_at timestamptz,
 delivered_at timestamptz,
 notification_id text,
 version bigint NOT NULL CHECK (version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK ((status IN ('pending','claimed','attention_required') AND payload_key_version IS NOT NULL AND length(payload_key_version) BETWEEN 1 AND 64 AND payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext)>=29)
  OR (status IN ('delivered','cancelled') AND payload_key_version IS NULL AND payload_ciphertext IS NULL)),
 CHECK ((status='pending' AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL)
  OR (status='claimed' AND claimed_at IS NOT NULL AND delivered_at IS NULL AND notification_id IS NULL)
  OR (status='delivered' AND claimed_at IS NOT NULL AND delivered_at IS NOT NULL AND notification_id IS NOT NULL)
  OR (status IN ('cancelled','attention_required') AND claimed_at IS NULL AND delivered_at IS NULL AND notification_id IS NULL))
);
CREATE INDEX iam_invited_account_outbox_dispatch ON iam_invited_account_outbox(status,available_at,id);
CREATE FUNCTION protect_invited_account_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id<>OLD.id OR NEW.challenge_id<>OLD.challenge_id OR NEW.created_at<>OLD.created_at OR OLD.status IN ('delivered','cancelled') OR NEW.version<>OLD.version+1
  OR (NEW.status NOT IN ('delivered','cancelled') AND (NEW.payload_key_version,NEW.payload_ciphertext) IS DISTINCT FROM (OLD.payload_key_version,OLD.payload_ciphertext)) THEN
  RAISE EXCEPTION 'Verification delivery identity is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER invited_account_delivery_identity BEFORE UPDATE ON iam_invited_account_outbox FOR EACH ROW EXECUTE FUNCTION protect_invited_account_delivery();

ALTER TABLE iam_audit_events DROP CONSTRAINT iam_audit_events_authentication_method_allowed;
ALTER TABLE iam_audit_events ADD CONSTRAINT iam_audit_events_authentication_method_allowed CHECK (
 authentication_method IN ('password','oidc','api_key','workload_token','internal','anonymous','password_action','workload_provisioner','administrator_provisioner','email_verification')
);
ALTER TABLE iam_audit_events ADD CONSTRAINT iam_email_verification_audit_scope CHECK (
 authentication_method<>'email_verification' OR (boundary='principal' AND tenant_id IS NULL AND actor_id IS NOT NULL AND action='iam.account.created' AND target_type='invited_account_verification' AND result='succeeded')
);
GRANT SELECT,INSERT,UPDATE ON iam_invited_account_verifications,iam_invited_account_outbox TO ani_iam_runtime;
UPDATE iam_schema_revision SET revision='202609130008' WHERE singleton;
