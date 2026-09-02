---
status: accepted
---

# Separate Core Control and IAM databases

Core Control and IAM use separate databases so that Tenant Lifecycle and identity/access data have independent ownership and failure boundaries. IAM stores the stable Tenant ID in Tenant Access, Membership, and Role Binding records but does not create a cross-service foreign key or writable Tenant copy; referential integrity is maintained by the provisioning protocol, idempotent reconciliation, and lifecycle checks rather than shared-schema coupling.

Before Core Control exists, CP0 and isolated prototypes may use explicitly marked fixture Tenant IDs only. In future P2, Core creates or seeds every authoritative Tenant ID before issuing the Bootstrap command that creates IAM-side data. IAM may seed its own test Principals and system Roles but never creates an authoritative or parallel Tenant ID.
