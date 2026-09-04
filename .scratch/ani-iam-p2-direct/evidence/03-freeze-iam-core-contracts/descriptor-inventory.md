# DP2-03 descriptor inventory

## IAM descriptor

Artifact: `api/iam/v1/iam_descriptor.pb`

SHA-256: `df863beb3b095d1f01350c5334d80daf10cdf48083ce0e5663781171aa99a001`

Result: `pass`

### `iam.v1.AuthenticationService` — 15 methods

`BeginOIDCIdentityLink`, `BeginOIDCLogin`, `CompleteOIDCIdentityLink`, `CompleteOIDCLogin`, `CompletePasswordAction`, `IssueServiceToken`, `ListSessions`, `LogoutSession`, `PasswordLogin`, `RefreshSession`, `RequestPasswordAction`, `RevokeAllSessions`, `RevokeSession`, `SwitchTenant`, `ValidatePrincipal`.

### `iam.v1.AuthorizationService` — 1 method

`CheckPermission`.

### `iam.v1.IAMAdminService` — 53 methods

`AcceptPlatformInvitation`, `AcceptTenantInvitation`, `ApproveRecoveryBootstrap`, `ApproveRestoreTenantAdmin`, `BindPlatformRole`, `BindTenantRole`, `CancelPlatformInvitation`, `CancelTenantInvitation`, `CreateAPIKey`, `CreatePlatformInvitation`, `CreatePlatformRole`, `CreateServicePrincipal`, `CreateTenantInvitation`, `CreateTenantRole`, `DeletePlatformRole`, `DeleteTenantRole`, `ExecuteRecoveryBootstrap`, `ExecuteRestoreTenantAdmin`, `GetAuditEvent`, `GetPlatformAuditEvent`, `GetPlatformInvitation`, `GetPlatformMembership`, `GetPlatformRole`, `GetServicePrincipal`, `GetTenantAccess`, `GetTenantInvitation`, `GetTenantMembership`, `GetTenantRole`, `ListAPIKeys`, `ListAuditEvents`, `ListPlatformAuditEvents`, `ListPlatformInvitations`, `ListPlatformMemberships`, `ListPlatformRoles`, `ListServicePrincipals`, `ListTenantInvitations`, `ListTenantMemberships`, `ListTenantRoles`, `RemovePlatformMembership`, `RemoveTenantMembership`, `RequestRecoveryBootstrap`, `RequestRestoreTenantAdmin`, `ResendPlatformInvitation`, `ResendTenantInvitation`, `RevokeAPIKey`, `UnbindPlatformRole`, `UnbindTenantRole`, `UpdatePlatformMembership`, `UpdatePlatformRole`, `UpdateServicePrincipal`, `UpdateTenantAccess`, `UpdateTenantMembership`, `UpdateTenantRole`.

Target total: 3 services and 69 methods. `auth.v1.AuthService`: absent.

## Core descriptor

Artifact: `repo/pkg/generated/pb/tenant/integration/v1/tenant_iam_integration_descriptor.pb`

SHA-256: `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a`

Result: `pass`

### `tenant.integration.v1.TenantIAMIntegrationService` — 2 methods

`BeginTenantLifecycleSnapshot`, `ListTenantLifecycleSnapshotPage`.

The descriptor additionally owns the lifecycle, heartbeat, bootstrap, envelope, snapshot, and stable error enums/messages. It contains no lifecycle writer method.

Runtime service registration is `not_verified`; this inventory proves the frozen descriptor surface only.
