---
status: accepted
---

# Use durable Session Grants instead of a JWT Blocklist

P2 does not maintain a per-`jti` JWT Blocklist. PostgreSQL is authoritative for Session, boundary grant, and Refresh Token state, and every Access Token authorization validates that current state online. This replaces the old PostgreSQL-plus-Redis `jwt_blocklist` contract rather than copying it into the new project.

`sessions` own the Human Principal, authentication methods, normalized device metadata, global status, activity deadlines, and absolute expiry. `session_grants` own one Tenant or Platform boundary and a monotonically increasing version. `refresh_token_families` belong to one Grant, and single-use `refresh_tokens` belong to one Family and record issuance, consumption, replacement, and revocation state. Access Tokens identify their Session Grant and version; a mismatch or inactive row fails closed.

Refresh Token reuse revokes the affected Family and increments its Boundary Grant version, invalidating every Access Token issued from that Grant. Other boundary Grants in the same Principal Session remain active. Consumed Refresh Token hashes remain available until the parent Session's absolute expiry so reuse can still be detected; raw tokens are never retained.

At most one active Refresh Token Family exists for a Session and boundary. Reentering or switching to the same boundary rotates or resumes that Grant rather than accumulating Families. Separate device Sessions remain independent and are not subject to a fixed count limit.

Redis is not an authorization source of truth. Ordinary protected requests continue by checking PostgreSQL when Redis is unavailable. Login, refresh, OIDC, or other flows that require unavailable distributed rate-limit or temporary state return `503`; no replica-local fallback weakens replay or abuse protection.

Session listings retain a user-assigned or normalized device type, authentication methods, creation and sampled recent-activity times, and truncated or hashed network information. IAM does not retain full raw IP addresses or User-Agent strings as long-lived Session metadata.

Logout is idempotent. Repeating a request against the same already revoked Session succeeds without revealing whether the Session previously existed, and only the first effective state transition produces the revocation audit event.

API Keys and Service Tokens remain distinct. An API Key is a long-lived external SDK credential of a single-Tenant Service Principal. A Service Token is issued only to an allowlisted internal workload authenticated with mTLS or SPIFFE, is bound to an audience and permitted operation subset, expires within five minutes, cannot refresh, and creates no Session. Console users cannot mint it and internal workloads do not substitute API Keys for it.
