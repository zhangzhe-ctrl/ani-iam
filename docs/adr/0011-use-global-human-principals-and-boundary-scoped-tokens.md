---
status: accepted
---

# Use global Human Principals and boundary-scoped tokens

The initial P2 contract removes `member_count` as an enforceable Quota dimension. It contains no dormant member-count TCC API, reservation table, mTLS call, reconciliation worker, or acceptance gate. IAM may report active member counts for product views, but a future member-limit feature requires a separate authorized scope and decision.

A Human Principal is global and may hold Memberships in several Tenants plus an optional Platform Membership. Its authenticated Session is Principal-wide, while each Access Token is scoped to exactly one Tenant boundary or to the Platform boundary. An explicit `SwitchTenant` operation revalidates Principal, Tenant Access, Lifecycle projection, Membership, and Session before issuing a Token for another Tenant. Tokens do not contain every Membership and do not accept a client Tenant header as authorization scope.

A Service Principal belongs to exactly one Tenant and cannot acquire Memberships in several Tenants or Platform access. Cross-Tenant integrations create separate Service Principals and credentials. Removing its only Membership disables the Service Principal and irreversibly revokes all of its API Keys. Removing a Human Membership revokes only Sessions and Tokens scoped to that Tenant; other Tenant and Platform access remains intact.

Verified email ownership is global. IAM normalizes an email by trimming surrounding whitespace, applying a consistent case normalization, and normalizing an internationalized domain name. It does not remove plus tags, collapse dots, or apply provider-specific aliases. Several login Identities may link to one Principal, but one normalized verified email cannot belong to several Principals.

Email equality never links accounts automatically. A signed-in user must recently reauthenticate and then complete an explicit Link Identity flow whose OIDC callback validates state, nonce, and PKCE. If the verified email is already owned by another Principal, linking fails and no automatic account merge or database-side repair occurs.

Open self-registration is not part of the initial delivery. A Human Principal is created only through a Tenant Invitation, Platform Invitation, or the controlled first-administrator Bootstrap. A new invitee first proves the Invitation and email ownership, then either sets a local password or authenticates through a configured OIDC Provider. IAM creates the Principal and Identity only after that proof and proceeds to accept the Invitation.

Console and BOSS retain separate public login entries but use the same Credential and password implementation. BOSS login additionally requires an active Platform Membership and produces a Platform-scoped Token; it cannot turn a Tenant login into Platform authority based on frontend routing.
