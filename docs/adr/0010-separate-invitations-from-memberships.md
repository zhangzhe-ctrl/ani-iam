---
status: accepted
---

# Separate Invitations from Memberships

A pending Invitation is an intent to add a Human Principal and is not a Tenant Membership. Tenant and Platform Invitations use their separate tables and may carry one or more Role identities; a Tenant Invitation must contain at least one Tenant Role and all referenced Roles must belong to the same Tenant.

For one Tenant and normalized email address, at most one pending Invitation exists. Repeating the same requested Role set returns its existing metadata without returning another secret. A different Role set returns `409 INVITATION_CONFLICT`; the caller must explicitly cancel or update the intent rather than silently overwrite it.

An Invitation expires after seven days by default. Resending keeps its identity, generates a new high-entropy token, immediately invalidates the prior token, calculates a new expiry, increments its delivery attempt, and writes an audit event. Invitation state and a notification outbox row commit in one IAM transaction. Email delivery is asynchronous and retryable; a delivery failure leaves the Invitation pending and exposes its delivery state to the administrator.

Acceptance requires an authenticated Human Principal that owns the same normalized verified email. Possession of the token alone is insufficient, and an already existing Principal is not added automatically. IAM maintains a globally unique verified-email ownership relation: several login Identities may link to one Principal, but the same normalized verified email cannot belong to several Principals. Matching email text never auto-links accounts; Identity linking remains an explicit secured flow.

Tenant Membership state is `active`, `suspended`, or `removed`; it has no `invited` state. Acceptance creates a new active Membership and all requested Role Bindings atomically after revalidating that each Role still exists, belongs to the Tenant, and is assignable. An Invitation stores Role identities rather than a Permission snapshot, so acceptance uses the Roles' current Permission sets. Reinviting a previously removed Principal creates a new Membership identity and cannot revive old Role Bindings.

The initial P2 delivery does not enforce a member-count quota. Invitation acceptance is therefore an IAM-local transaction and does not call Core Quota `Try`, `Confirm`, `Cancel`, or `Release`; it creates no cross-service member-count TCC, mTLS operation, reservation, or reconciliation worker. IAM still exposes an observable member count for Console and BOSS, but that count is not a product limit. A future member-quota feature requires its own decision and integration work rather than dormant TCC code in this release.

Accept, Cancel, and Resend lock the Invitation and perform a versioned conditional state transition. The first committed valid transition wins. Later repetitions return the recorded idempotent result or a stable conflict and cannot issue a usable second token or create a second Membership.
