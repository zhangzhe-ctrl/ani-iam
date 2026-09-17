-- Core owns message recovery. These finite Platform permissions are available
-- for explicit Role/Binding management, never granted to built-in roles here.
INSERT INTO permission_catalog(scope,resource,action) VALUES
 ('platform', 'tenant-outbox', 'read'),
 ('platform', 'tenant-outbox', 'recover');
UPDATE iam_schema_revision SET revision='202609130015' WHERE singleton;
