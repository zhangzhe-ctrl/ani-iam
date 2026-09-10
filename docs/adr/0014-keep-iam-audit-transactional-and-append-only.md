---
status: accepted
---

# Keep IAM audit transactional and append-only

ADR-0022 amends the Principal terminology. The exact actor union, seed/first-administrator flow and delegation telemetry contracts are frozen through [D02–D04](../../.scratch/ani-iam-workload-refoundation/decisions.md); those candidate mechanics do not become accepted through this ADR. The transaction, attribution, retention and visibility rules below remain accepted.

IAM security audit covers authentication success and failure, OIDC and Identity linking, password actions, Session and Token revocation, Refresh Token reuse, Invitations, Memberships, Roles and Role Bindings, API Keys, Workload Principals, Workload Identity and Grant lifecycle, Tenant Access, recovery operations, and Workload Access Token issuance. Ordinary successful product API requests remain in correlated Gateway and service access logs rather than becoming IAM domain events.

An IAM-local state change and its audit event commit in the same PostgreSQL transaction. If the audit row cannot be written, the business mutation rolls back. Authentication, refresh, password, Identity-link, and credential-creation operations also fail closed with `503` when their required security audit cannot be recorded; they do not succeed with only a best-effort log line.

Trusted initialization and the first-administrator path must satisfy the same local mutation/Audit atomicity and Secret redaction requirements. Their exact durable flow design is prepared under D03; no cross-store best-effort audit exception or hidden default administrator is introduced. The former Phase0/Finalize/PG-flow recipe remains historical candidate input.

Every event has a stable event identity, occurrence and recording times, actor identity, credential or authentication method, boundary, optional Tenant identity, action, target type and identity, result, stable reason code, request, correlation, and authorization decision identities, source service, and relevant object version. Details contain only allowlisted and redacted before-and-after fields. Passwords, Tokens, API Key secrets or hashes, complete email bodies, and arbitrary request or response payloads never enter audit storage.

Authenticated Human/Workload actors, failed unauthenticated attempts and pre-trust initialization provenance must be distinguishable. Failed authentication cannot be attributed to a guessed Principal, and seed/unauthenticated provenance is not another Principal or source of Authority. Direct transport caller, evaluated subject and asynchronous producer/executor attribution must not be collapsed. The exact typed variants and evidence-stage ordering are contract work under D02–D04; the previous detailed union is retained in the WR-16 before snapshot.

The P2 test environment guarantees that IAM security audit events remain queryable for at least 180 days; without an automated cleanup job, older events may remain longer and there is no day-181 deletion claim. If the project remains active through that horizon, a separately delivered deletion or retention capability must replace indefinite accumulation. Production retention, legal hold, export, and external archive requirements still belong to future Production Readiness scope.

Current evidence supports only an application-level append-only claim: normal IAM roles cannot update or delete audit rows and no mutation API exists. It does not claim cryptographic non-repudiation or protection from a database administrator. External WORM storage, signing, hash chains, and equivalent tamper evidence are deferred to Production Readiness.

Tenant `auditor` and `tenant-admin` Roles can query only allowlisted events in their Tenant. Platform recovery, internal credential details, and cross-Tenant security metadata require a dedicated Platform Auditor capability. Audit APIs expose only `ListAuditEvents` and `GetAuditEvent` with stable cursor pagination; bulk export, SIEM streaming, and archival are not part of the initial delivery.

IAM persists management mutations and high-risk or denied authorization decisions. A normal allowed authorization decision returns a `decision_id` that Gateway and downstream structured access logs carry for correlation, but IAM does not insert one database audit row for every successful product request.

Delegation evidence must not be written as a raw Secret into audit or ordinary logs. Existing decision/correlation identities connect accepted results to receiver access logs. Whether and how receipts are issued and attenuated remains D02 contract work; this ADR does not create a standalone authority-from-decision-ID operation.