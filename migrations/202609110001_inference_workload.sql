-- WR21 selected Inference caller and receiver grants; runtime remains read-only.
ALTER TABLE workload_grants DROP CONSTRAINT workload_grants_target;
ALTER TABLE workload_grants ADD CONSTRAINT workload_grants_target CHECK (
 (audience = 'ani-iam' AND scope = 'iam_ingress'
  AND operation ~ '^/(iam[.]v1[.][A-Za-z]+Service/[A-Za-z]+|grpc[.]health[.]v1[.]Health/Check)$')
 OR (audience = 'ani-session-gateway' AND scope = 'delegated_session' AND operation = 'session.create')
 OR (audience = 'ani-notification-service' AND scope = 'workload_notification'
     AND operation IN ('notification.submit', 'notification.get_own'))
 OR (audience = 'ani-inference-service' AND scope = 'workload_inference'
     AND operation IN ('inference.check_access', 'inference.receive_access'))
);

-- Explicit permission only; built-in roles receive no automatic grant.
INSERT INTO permission_catalog (scope, resource, action)
VALUES ('tenant', 'inference-services', 'invoke');

UPDATE iam_schema_revision SET revision = '202609110001' WHERE singleton;
