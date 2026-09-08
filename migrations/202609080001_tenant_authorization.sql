-- DP2-09 grants the runtime only the narrow delete capability required to
-- unbind a role. Permission definition writes remain migration-owned; runtime
-- role administration cannot invent catalog entries. The basic system Role
-- uses its immutable code as the display name; custom names arrive in DP2-15.

GRANT DELETE ON TABLE tenant_role_bindings TO ani_iam_runtime;
