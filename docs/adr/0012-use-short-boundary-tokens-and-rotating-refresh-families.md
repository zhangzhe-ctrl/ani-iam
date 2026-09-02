---
status: accepted
---

# Use short boundary tokens and rotating Refresh Token Families

Console Access Tokens expire after fifteen minutes. A Console Session expires after seven days of inactivity or thirty days absolutely, whichever occurs first. BOSS Access Tokens expire after ten minutes, and a BOSS Session expires after thirty minutes of inactivity or eight hours absolutely. These are target contract maxima rather than unconstrained deployment settings.

An Access Token contains only stable verification and request context: issuer, subject, audience, issue and expiry times, token identity, Session identity, Principal type, boundary type, optional Tenant identity for a Tenant boundary, and authentication methods. Roles, Permissions, the Principal's other Memberships, and client-selected Tenant identities are not token claims. Online authorization evaluates their current state.

Each Refresh Token Family belongs to one Session and exactly one Tenant or Platform boundary. `SwitchTenant` reauthorizes the requested Tenant and establishes a separate Family for that boundary; a Refresh Token for one Tenant cannot request another Tenant. Refresh Tokens are single-use. A successful refresh rotates the secret atomically, and reuse of an already consumed token revokes its Family as previously decided.

A Human Principal may have any number of concurrent Sessions. IAM records enough device or client metadata and recent activity to list Sessions and revoke one explicitly; it does not silently delete the oldest Session at an arbitrary fixed count. Ordinary logout revokes the current Session, including its boundary Families, while the explicit global operation revokes all Sessions.

Completing a password reset revokes every Session, Refresh Token Family, and Access Token of that Human Principal. It does not delete OIDC Identities or affect another Principal's credentials. Password setup and reset tokens expire after thirty minutes, are single-use, and bind the Principal, action purpose, and originating operation. Creating a replacement action token invalidates the previous token.

Password login applies rate limits to both the normalized account key and source IP. Failures produce increasing delay and five consecutive failures lock password authentication for fifteen minutes; successful password authentication clears the failure state. Public responses do not reveal whether the account exists.

P2 OIDC login and Identity linking use Authorization Code Flow with mandatory PKCE S256. State, nonce, and code verifier expire after ten minutes, are single-use, and reject callback replay. An OIDC email becomes verified ownership only when the configured trusted Provider's issuer, audience, signature, nonce, and `email_verified=true` claim all validate.

P2 signs Access and Service JWTs asymmetrically with keys held by a Secret Manager or KMS. Issuance identifies the active key by `kid`; retired public keys remain available to IAM validation at least until every Token they could have signed has expired. Key activation and retirement require audit evidence and a verification gate. IAM still exposes no unused public JWKS endpoint unless a separately accepted offline consumer requires one.
