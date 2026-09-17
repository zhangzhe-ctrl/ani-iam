-- Custom role metadata and the narrow deletes used by explicit role removal.
-- Existing roles keep their code as the initial display name.
ALTER TABLE tenant_roles ADD COLUMN display_name text NOT NULL DEFAULT '';
UPDATE tenant_roles SET display_name = code;
GRANT DELETE ON TABLE tenant_roles, tenant_role_permissions TO ani_iam_runtime;
GRANT INSERT ON TABLE tenant_role_permissions TO ani_iam_runtime;
ALTER TABLE tenant_mutation_results DROP CONSTRAINT tenant_mutation_results_operation_check;
ALTER TABLE tenant_mutation_results ADD CONSTRAINT tenant_mutation_results_operation_check CHECK (operation IN (
    'createTenantWorkload', 'updateTenantWorkload', 'createIAMAPIKey', 'revokeIAMAPIKey',
    'updateTenantIAMMember', 'removeTenantIAMMember', 'bindTenantIAMRole', 'unbindTenantIAMRole',
    'updateTenantIAMAccess', 'createTenantIAMRole', 'updateTenantIAMRole', 'deleteTenantIAMRole'
));
