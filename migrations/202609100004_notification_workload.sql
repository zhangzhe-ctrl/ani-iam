-- WR20: owner-provisioned Workload-only Notification targets. Runtime remains
-- read-only for identities and Grants; no Tenant or Human delegation is added.
ALTER TABLE workload_grants DROP CONSTRAINT workload_grants_target;
ALTER TABLE workload_grants ADD CONSTRAINT workload_grants_target CHECK (
 (audience = 'ani-iam' AND scope = 'iam_ingress'
  AND operation ~ '^/(iam[.]v1[.][A-Za-z]+Service/[A-Za-z]+|grpc[.]health[.]v1[.]Health/Check)$')
 OR (audience = 'ani-session-gateway' AND scope = 'delegated_session' AND operation = 'session.create')
 OR (audience = 'ani-notification-service' AND scope = 'workload_notification'
     AND operation IN ('notification.submit', 'notification.get_own'))
);

UPDATE iam_schema_revision SET revision = '202609100004' WHERE singleton;
