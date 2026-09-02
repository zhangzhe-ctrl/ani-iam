---
status: accepted
---

# Separate Tenant Lifecycle ownership from IAM access control

Core Control owns Tenant Lifecycle because a Tenant is a platform resource whose creation and state transitions coordinate with quota and other control-plane capabilities. IAM owns Tenant Access, Tenant Membership, roles, credentials, and authorization; it may deny access based on authoritative lifecycle state but must not write that state. Authentication may still succeed when a Tenant is not active, while ordinary tenant operations are denied and platform recovery and audit remain available. This keeps resource lifecycle and security suspension as separate concepts and prevents `iam-service` from becoming a second Tenant lifecycle writer.
