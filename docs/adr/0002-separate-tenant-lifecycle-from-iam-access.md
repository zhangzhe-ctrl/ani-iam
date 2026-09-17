---
status: accepted
---

# Separate Tenant Lifecycle ownership from IAM access control

Amended by the user's 2026-09-16 IAM/Governance Goal: independent Governance replaces Core Control as the Tenant business and lifecycle owner. Historical WR23 contracts and verification remain evidence of their old fixed combination, not evidence for Governance.

Governance owns Tenant Lifecycle because a Tenant is a platform resource whose creation and state transitions belong to business governance. Later plan/quota capabilities remain with Governance, but are not prerequisites for first-slice Tenant creation. IAM owns Tenant Access, Tenant Membership, roles, credentials, and authorization; it may deny access based on authoritative lifecycle state but must not write that state. Authentication may still succeed when a Tenant is not active, while ordinary tenant operations are denied and platform recovery and audit remain available. This keeps resource lifecycle and security suspension as separate concepts and prevents `iam-service` from becoming a second Tenant lifecycle writer.
