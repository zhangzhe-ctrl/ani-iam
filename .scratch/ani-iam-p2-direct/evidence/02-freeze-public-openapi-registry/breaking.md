# DP2-02 exact OpenAPI breaking review

Result: `pass`

The machine-readable complete diff is `repo/api/openapi/openapi-breaking.v1.json`, generated directly from the immutable Git object and the target OpenAPI. Its SHA-256 is `cc45d4116b99a525ed58c29a1e33daae194c3cd8f54a6e36c9fdf9859e01be9a`.

## Fixed comparison

| Side | Identity |
| --- | --- |
| Source | ANI commit `0cedae825a489d936cf41815dc27f278f6d3213c`, tree `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8`, `repo/api/openapi/v1.yaml`, SHA-256 `b6a2dc1f9c596555fcc164240d7a5533a04a128b8e19a4d7af1c53e41ab9415b` |
| Target | `repo/api/openapi/v1.yaml`, SHA-256 `2466982a7e8f904c6bb6f7790588359c6faf9b230a39a0e28939fcbedc72d0e5` |

## Exact category counts

| Category | Count |
| --- | ---: |
| Source operations | 236 |
| Target operations | 295 |
| Added operations | 59 |
| Removed operations | 0 |
| Changed operationIds | 6 |
| Per-operation security changes | 222 |
| Parameter contract changes | 7 |
| Request contract changes | 5 |
| Response contract changes | 7 |
| Semantic operation changes | 236 |
| Added schemas | 45 |
| Removed schemas | 0 |
| Structurally changed existing schemas | 0 |
| Security-scheme changes | 3 |
| Shared component-contract changes | 12 |
| Top-level security changes | 1 |
| Other document-contract changes | 0 |
| Total breaking rows | 263 |

## Changed operationIds

| Method and path | Before | After |
| --- | --- | --- |
| `DELETE /auth/api-keys/{key_id}` | `revokeAPIKey` | `revokeIAMAPIKey` |
| `GET /auth/api-keys` | `listAPIKeys` | `listIAMAPIKeys` |
| `GET /branding` | missing | `getBranding` |
| `POST /auth/api-keys` | `createAPIKey` | `createIAMAPIKey` |
| `POST /auth/logout` | `logout` | `logoutSession` |
| `POST /auth/refresh` | missing | `refreshSession` |

## Added operations

No operation is removed. The 59 additions are:

- `DELETE /auth/sessions` — `revokeAllSessions`
- `DELETE /auth/sessions/{session_id}` — `revokeSession`
- `DELETE /iam/platform/members/{membership_id}` — `removePlatformIAMMember`
- `DELETE /iam/platform/members/{membership_id}/role-bindings/{role_id}` — `unbindPlatformIAMRole`
- `DELETE /iam/platform/roles/{role_id}` — `deletePlatformIAMRole`
- `DELETE /iam/tenants/{tenant_id}/members/{membership_id}` — `removeTenantIAMMember`
- `DELETE /iam/tenants/{tenant_id}/members/{membership_id}/role-bindings/{role_id}` — `unbindTenantIAMRole`
- `DELETE /iam/tenants/{tenant_id}/roles/{role_id}` — `deleteTenantIAMRole`
- `GET /auth/identity-links/oidc/callback` — `completeOIDCIdentityLink`
- `GET /auth/oidc/callback` — `completeOIDCCallback`
- `GET /auth/service-principals` — `listServicePrincipals`
- `GET /auth/service-principals/{principal_id}` — `getServicePrincipal`
- `GET /auth/sessions` — `listSessions`
- `GET /iam/audit-events` — `listIAMSecurityAuditEvents`
- `GET /iam/audit-events/{event_id}` — `getIAMSecurityAuditEvent`
- `GET /iam/platform/audit-events` — `listPlatformIAMSecurityAuditEvents`
- `GET /iam/platform/audit-events/{event_id}` — `getPlatformIAMSecurityAuditEvent`
- `GET /iam/platform/invitations` — `listPlatformIAMInvitations`
- `GET /iam/platform/invitations/{invitation_id}` — `getPlatformIAMInvitation`
- `GET /iam/platform/members` — `listPlatformIAMMembers`
- `GET /iam/platform/members/{membership_id}` — `getPlatformIAMMember`
- `GET /iam/platform/roles` — `listPlatformIAMRoles`
- `GET /iam/platform/roles/{role_id}` — `getPlatformIAMRole`
- `GET /iam/tenants/{tenant_id}/access` — `getTenantAccess`
- `GET /iam/tenants/{tenant_id}/invitations` — `listTenantIAMInvitations`
- `GET /iam/tenants/{tenant_id}/invitations/{invitation_id}` — `getTenantIAMInvitation`
- `GET /iam/tenants/{tenant_id}/members` — `listTenantIAMMembers`
- `GET /iam/tenants/{tenant_id}/members/{membership_id}` — `getTenantIAMMember`
- `GET /iam/tenants/{tenant_id}/roles` — `listTenantIAMRoles`
- `GET /iam/tenants/{tenant_id}/roles/{role_id}` — `getTenantIAMRole`
- `PATCH /auth/service-principals/{principal_id}` — `updateServicePrincipal`
- `PATCH /iam/platform/members/{membership_id}` — `updatePlatformIAMMember`
- `PATCH /iam/platform/roles/{role_id}` — `updatePlatformIAMRole`
- `PATCH /iam/tenants/{tenant_id}/access` — `updateTenantAccess`
- `PATCH /iam/tenants/{tenant_id}/members/{membership_id}` — `updateTenantIAMMember`
- `PATCH /iam/tenants/{tenant_id}/roles/{role_id}` — `updateTenantIAMRole`
- `POST /auth/identity-links/oidc/begin` — `beginOIDCIdentityLink`
- `POST /auth/password-actions` — `requestPasswordAction`
- `POST /auth/password-actions/complete` — `completePasswordAction`
- `POST /auth/service-principals` — `createServicePrincipal`
- `POST /auth/switch-tenant` — `switchTenant`
- `POST /iam/platform/invitations` — `createPlatformIAMInvitation`
- `POST /iam/platform/invitations/{invitation_id}/accept` — `acceptPlatformIAMInvitation`
- `POST /iam/platform/invitations/{invitation_id}/cancel` — `cancelPlatformIAMInvitation`
- `POST /iam/platform/invitations/{invitation_id}/resend` — `resendPlatformIAMInvitation`
- `POST /iam/platform/members/{membership_id}/role-bindings` — `bindPlatformIAMRole`
- `POST /iam/platform/recovery-bootstrap-requests` — `requestRecoveryBootstrap`
- `POST /iam/platform/recovery-bootstrap-requests/{operation_id}/approve` — `approveRecoveryBootstrap`
- `POST /iam/platform/recovery-bootstrap-requests/{operation_id}/execute` — `executeRecoveryBootstrap`
- `POST /iam/platform/restore-tenant-admin-requests` — `requestRestoreTenantAdmin`
- `POST /iam/platform/restore-tenant-admin-requests/{operation_id}/approve` — `approveRestoreTenantAdmin`
- `POST /iam/platform/restore-tenant-admin-requests/{operation_id}/execute` — `executeRestoreTenantAdmin`
- `POST /iam/platform/roles` — `createPlatformIAMRole`
- `POST /iam/tenants/{tenant_id}/invitations` — `createTenantIAMInvitation`
- `POST /iam/tenants/{tenant_id}/invitations/{invitation_id}/accept` — `acceptTenantIAMInvitation`
- `POST /iam/tenants/{tenant_id}/invitations/{invitation_id}/cancel` — `cancelTenantIAMInvitation`
- `POST /iam/tenants/{tenant_id}/invitations/{invitation_id}/resend` — `resendTenantIAMInvitation`
- `POST /iam/tenants/{tenant_id}/members/{membership_id}/role-bindings` — `bindTenantIAMRole`
- `POST /iam/tenants/{tenant_id}/roles` — `createTenantIAMRole`

## Schema changes

No existing schema is removed or structurally changed. The 45 added schemas are:

`IAMAPIKey`, `IAMAPIKeyCreateRequest`, `IAMAPIKeyCreateResponse`, `IAMAPIKeyListResponse`, `IAMAccessTokenResponse`, `IAMBoundary`, `IAMIdentityLinkBeginRequest`, `IAMInvitation`, `IAMInvitationAcceptRequest`, `IAMInvitationCreateRequest`, `IAMInvitationListResponse`, `IAMInvitationMutationRequest`, `IAMMembership`, `IAMMembershipListResponse`, `IAMMembershipUpdateRequest`, `IAMOIDCBeginRequest`, `IAMOIDCBeginResponse`, `IAMPasswordActionCompleteRequest`, `IAMPasswordActionRequest`, `IAMPasswordLoginRequest`, `IAMPlatformBoundary`, `IAMPrincipalSummary`, `IAMRecoveryApprovalRequest`, `IAMRecoveryExecuteRequest`, `IAMRecoveryOperation`, `IAMRecoveryRequest`, `IAMRole`, `IAMRoleBindingRequest`, `IAMRoleCreateRequest`, `IAMRoleListResponse`, `IAMRoleUpdateRequest`, `IAMSecurityAuditDetails`, `IAMSecurityAuditEvent`, `IAMSecurityAuditEventListResponse`, `IAMServicePrincipal`, `IAMServicePrincipalCreateRequest`, `IAMServicePrincipalListResponse`, `IAMServicePrincipalUpdateRequest`, `IAMSessionGrantSummary`, `IAMSessionListResponse`, `IAMSessionSummary`, `IAMSwitchTenantRequest`, `IAMTenantAccess`, `IAMTenantAccessUpdateRequest`, `IAMTenantBoundary`.

## Parameter, request and response changes

- `DELETE /auth/api-keys/{key_id}` adds required `Idempotency-Key`, makes `key_id` UUID, changes `200 {status: revoked}` to `204`, and adds stable `409/429/503/504` responses.
- `GET /auth/api-keys` replaces optional `user_id` with required UUID `service_principal_id` plus cursor/limit; response changes to `IAMAPIKeyListResponse` and adds `503/504`.
- `POST /auth/api-keys` adds required `Idempotency-Key`; request/response become `IAMAPIKeyCreateRequest`/`IAMAPIKeyCreateResponse`; adds `409/429/503/504`.
- `POST /auth/logout` adds required `Idempotency-Key` and optional `X-CSRF-Token`, removes the JSON body, changes `200 RevokeStatusResponse` to `204`, removes `403`, and adds `409/429/503/504`.
- `POST /auth/oidc/begin` adds required `Idempotency-Key`; request/response become `IAMOIDCBeginRequest`/`IAMOIDCBeginResponse`; adds `409/429/503/504`.
- `POST /auth/password/login` adds required `Idempotency-Key`; request becomes `IAMPasswordLoginRequest`; response changes from `TokenPairResponse` to `IAMAccessTokenResponse` plus `Set-Cookie`; `401` uses the shared response, `404` is removed, and `409/503/504` are added.
- `POST /auth/refresh` adds required `Idempotency-Key` and `X-CSRF-Token`, removes the JSON refresh-token body, changes the response to `IAMAccessTokenResponse` plus `Set-Cookie`, and adds `409/429/503/504`.

## Security and shared semantics

- Top-level security removes the `ApiKeyAuth` alternative and keeps canonical `BearerAuth`. API Keys remain bearer credentials under the target contract.
- `BearerAuth.bearerFormat` changes from `JWT` to `ANI-Access-Token-or-API-Key-or-Service-Token`.
- `ConsoleRefreshCookie` and `BossRefreshCookie` are added as separate cookie schemes.
- 222 retained operations receive explicit canonical security behavior; all 236 retained operations receive explicit owner/handler/classification and target semantic annotations where applicable.
- Shared `401/403/409/429/503/504` response meanings change to the stable IAM reason-code contract; `GatewayTimeout` and shared IAM parameters are added.

The exact before/after values for all 222 security rows, 236 semantic rows, 12 component rows and every request/response pointer are retained verbatim in the generated JSON report rather than abbreviated in this narrative.
