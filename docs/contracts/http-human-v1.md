# HTTP direct caller and local Human subject

The owner fixes an exact enabled HTTP target in the reviewed workload registry.
Its subject operationId and permission are declared separately in owner OpenAPI.
No caller-supplied operation, scope, principal or tenant header is authority.

`grpcworkload.Client.AuthorizeHTTPPlatformHuman(request, target, sourceOperation)` checks the real
TLS 1.3 mTLS direct caller, WAT, target revision and current receiver/caller Grants,
then checks the one `Authorization: Bearer ...` credential with the existing
Platform Human decision. Empty tenant does not select Platform implicitly.

`AuthorizeHTTPTenantHuman(request, target, AuthorizationRequest{TenantID: ..., SourceOperation: ...})`
uses the same caller check and existing Tenant decision. The requested tenant is
mandatory, canonical and matched against the current Human Session boundary.
Resource ownership remains with the owner through the existing CheckResource
obligation. Credential in that argument must be empty. SourceOperation is fixed
by the owner route declaration, never read from a caller-selected field.

Both return a cloned request with both credentials removed, the verified direct
caller, and an opaque authorized Human Subject with the decision ID for audit.
They do not cache decisions, create a delegation, infer identities from headers,
or change business idempotency. Every replay requires another current check.
Proxy credentials, cookies, delegation metadata, missing/duplicate credentials,
unknown routes, revision mismatch and online denial fail closed.

The existing Workload-only Snapshot entry remains separate and rejects Human
credentials. Governance business policy operations are the five operationIds
declared by its owner OpenAPI, with separate permissions and explicit Platform/Tenant scope. IAM generates
their policies from that immutable input; the HTTP adapter contains no service
names or copied owner DTOs.

Verification is local SDK scope with a real TLS handshake and a fake online IAM
client until the joint environment proves the complete current Grant/Session
chain. This document is not a claim of joint acceptance.
