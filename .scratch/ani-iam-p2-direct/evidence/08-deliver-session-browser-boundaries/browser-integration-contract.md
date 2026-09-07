# DP2-08 Gateway / Console / BOSS browser integration contract

Status: frozen for DP2-13 caller adaptation by the 2026-09-08 human decisions

This document freezes the browser-facing behavior that consumes the DP2-08 IAM
backend. It does not claim that Gateway, Console, BOSS, or a browser currently
implements or passes this behavior. Those runtime results remain
`not_verified` until DP2-13.

The JSON shapes and operation semantics below preserve the DP2-02 public
contract. The human owner accepted the audience-qualified `/auth/{audience}/*`
route family because the current shared `/auth/*` routes cannot also satisfy the
accepted requirement that Console and BOSS cookies have different constrained
Paths. This decision freezes the caller contract but does not modify or expand
any ANI path in DP2-08. DP2-13 may move each shared operation ID only after its
own Allowed paths, a current exact ANI SHA, the diff from the previously observed
`50f7b422707c2ab78462bd9bb8186bae018a14fe`, and the unified registry-repair
window are separately presented and accepted. The path parameter is an enum of
`console` and `boss`; it does not create duplicate operation IDs. This routing
adaptation does not change IAM semantics. Until that caller work is completed,
the ANI OpenAPI/registry and all runtime route evidence remain `not_verified`.
The IAM gRPC messages are internal transport only: Gateway may receive a raw
Refresh Token from IAM solely so it can emit `Set-Cookie`; it must never copy
that value to a public response body.

## 1. Fixed browser routes

| Operation | Method and route | Public credential input | Successful public result |
| --- | --- | --- | --- |
| Password login | `POST /auth/{audience}/password/login` | password body; no Token; path audience and body audience must match exactly | Access Token response plus selected-audience Refresh/CSRF `Set-Cookie` |
| Begin OIDC | `POST /auth/{audience}/oidc/begin` | no Token; path audience and body audience must match exactly | fixed-provider authorization URL and opaque state |
| OIDC callback | `GET /auth/{audience}/oidc/callback?code=...&state=...` | single-use OIDC code/state bound to the same path audience | selected-audience Refresh/CSRF `Set-Cookie`, then `303` to a fixed application page |
| Refresh | `POST /auth/{audience}/refresh` | only the path audience's Refresh Cookie plus CSRF Cookie/Header | replacement Refresh `Set-Cookie`; Access Token response without Refresh Token |
| Logout | `POST /auth/{audience}/logout` | only the path audience's Refresh Cookie plus CSRF Cookie/Header; expired Access Token is irrelevant | matching cookies cleared; `204`, including an unknown/already-revoked Session |
| Console switch Tenant | `POST /auth/console/switch-tenant` | Console bearer Access Token, Console Refresh Cookie and CSRF Cookie/Header; body contains only `tenant_id` | target-boundary Refresh `Set-Cookie`; target-boundary Access Token response |

Gateway rejects an unknown audience path, a path/body audience mismatch, the
opposite cookie, or both audience cookies before IAM is called. BOSS operations
on their `/auth/boss/...` expansion remain fail-closed until the Platform Grant
owner delivers and verifies that capability.

Every actual mutation attempt sends a newly generated `Idempotency-Key` header.
DP2-08 provides no durable replay result for that key and callers must not retry
Refresh or SwitchTenant by reusing it. A lost successful response is therefore
not recoverable through idempotency: presenting the old Refresh Token again is
reuse and produces the generic expired-session outcome. Logout remains
semantically idempotent regardless of the key, so a fresh-key retry still
succeeds without revealing prior Session state. Same-key replay, payload
conflict and logical 24-hour expiry are explicitly `not_verified` until DP2-16
delivers the unified Idempotency Ledger.

## 2. Console flows

### 2.1 Password login

1. Send `POST /auth/console/password/login` with `credentials: "include"`, a fresh
   `Idempotency-Key`, and body `{account, password, audience: "console",
   boundary: {tenant: {tenant_id}}, device_name}`.
2. Gateway removes any client-supplied internal identity/source-IP metadata,
   derives the trusted source address, verifies the exact browser Origin, and
   invokes IAM.
3. On success Gateway writes `ani_console_refresh` and `ani_console_csrf`,
   removes the internal Refresh Token from the public payload, and returns only
   `IAMAccessTokenResponse` with `Cache-Control: no-store`.
4. Console retains `access_token` and its expiry only in the current JavaScript
   process memory.

### 2.2 OIDC login and callback

1. Send `POST /auth/console/oidc/begin` with `credentials: "include"`, a fresh
   `Idempotency-Key`, and `{audience: "console", boundary: {tenant: ...}}`.
2. Navigate to the returned authorization URL. Do not persist state, code,
   verifier, or Token data in browser storage.
3. The Provider returns to the exact registered callback
   `https://console.example.test/auth/console/oidc/callback`. Gateway consumes the code
   and state once, calls IAM, sets the Console Refresh and CSRF cookies, then
   sends `303 Location` to a fixed Console page. The final `Location` contains
   no code, state, Access Token, or Refresh Token.
4. Because a `303` cannot safely deliver an Access Token, the destination page
   obtains its in-memory Access Token with one CSRF-protected
   `POST /auth/console/refresh`.

The callback is an IdP top-level navigation and therefore does not require the
Console Origin header. It is protected by exact callback URI registration,
single-use state/nonce/PKCE, and a fixed post-callback `Location`. OIDC begin and
all browser credential mutations use the Origin policy in section 5.

### 2.3 Refresh

1. About 60 seconds before Access Token expiry, acquire the Console refresh
   lock described in section 6.
2. Send `POST /auth/console/refresh` with `credentials: "include"`,
   `X-CSRF-Token` equal to the Console CSRF cookie, and a fresh
   `Idempotency-Key`. Send no Refresh Token in JSON or a custom header.
3. Gateway accepts only `ani_console_refresh`, forwards the raw credential and
   trusted proof fields to IAM, writes the rotated cookie, strips the internal
   Refresh Token from JSON, and returns the new Access Token and non-sensitive
   Session/Grant summary.
4. Replace the in-memory Access Token atomically and notify sibling tabs. A
   missing or malformed `Set-Cookie` makes the refresh fail closed even if an
   Access Token body was received.

### 2.4 Logout

Send `POST /auth/console/logout` with the Console Refresh and CSRF proof. Gateway must
allow this path when the Access Token is absent or expired. On every terminal
`204` or credential-invalid `401`, clear both Console cookies with an exactly
matching cookie tuple and erase the in-memory Access Token. Repeating logout is
successful and must not reveal whether the Session previously existed.

### 2.5 Switch Tenant

Send `POST /auth/console/switch-tenant` with the current in-memory Console bearer,
Console Refresh and CSRF proof, a fresh `Idempotency-Key`, and JSON body
`{"tenant_id":"<uuid>"}`. The browser never supplies a trusted Tenant header.
Gateway forwards only the bearer and requested target to IAM; IAM revalidates
the bearer source Grant, Principal, Session, target Tenant Access, fresh
Lifecycle projection, and target Membership before creating, resuming, or
rotating exactly one target boundary. Gateway replaces only the Console Refresh
cookie and returns only the target Access Token response.

## 3. BOSS flows and current boundary

BOSS is assigned the parallel `/auth/boss/password/login`,
`/auth/boss/oidc/begin`, `/auth/boss/oidc/callback`, `/auth/boss/refresh`, and
`/auth/boss/logout` edge routes, with `audience: "boss"`, a Platform boundary,
the BOSS cookie tuple, the exact BOSS Origin, and the fixed callback
`https://boss.example.test/auth/boss/oidc/callback`. Its intended maxima are a
10-minute Access Token, 30-minute idle Session, and 8-hour absolute Session.

The current Password composition rejects BOSS authentication, the checked-in
OIDC configuration is Console-only, and DP2-08 deliberately rejects a BOSS
audience carried by a Tenant Grant. Platform/BOSS authentication, lifetime
enforcement through a real Platform Grant, exact error mapping, and caller
evidence are therefore `not_verified`; DP2-13 must not invent a Platform Grant
or turn a Tenant Grant into Platform authority. A missing Platform/BOSS IAM
capability must fail closed and be returned to the owning IAM ticket rather than
bypassed in Gateway; DP2-08 does not claim a verified public `503` mapping for
every unavailable BOSS entry.

`POST /auth/console/switch-tenant` accepts only the Console Refresh Cookie in the frozen
public contract. BOSS must not send `ani_boss_refresh` to that route or reuse a
Console cookie. BOSS tenant-entry behavior remains disabled and
`not_verified` until an accepted Platform-to-Tenant contract exists.

## 4. Exact cookie contract

`Domain` is omitted for every cookie, making it host-only. Console cookies use
`Path=/auth/console`; BOSS cookies use `Path=/auth/boss`. Login/callback may set
the cookie for its audience family, refresh/logout receive only that family,
and ordinary product or opposite-audience authentication routes receive none.

| Audience | Cookie | Path | Domain | Secure | HttpOnly | SameSite | Max-Age |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Console | `ani_console_refresh` | `/auth/console` | omitted | true | true | `Lax` | remaining whole seconds to Session absolute expiry, capped at 2,592,000 |
| Console | `ani_console_csrf` | `/auth/console` | omitted | true | false | `Lax` | identical deadline to the Console Refresh cookie |
| BOSS | `ani_boss_refresh` | `/auth/boss` | omitted | true | true | `Lax` | remaining whole seconds to Session absolute expiry, capped at 28,800 |
| BOSS | `ani_boss_csrf` | `/auth/boss` | omitted | true | false | `Lax` | identical deadline to the BOSS Refresh cookie |

Gateway also writes an `Expires` value equal to the server-side absolute
deadline. On deletion it emits the same Name, host, Path, Secure, HttpOnly, and
SameSite tuple with `Max-Age=0` and an expired `Expires` value. It never sets a
parent-domain cookie. Production hostnames are a deployment input and remain
`not_verified`; weakening to a shared Domain is forbidden.

Refresh Tokens appear only in the two HttpOnly cookies. They are forbidden in
JSON, URLs, `Location`, browser storage, traces, metrics, exception text, access
logs, analytics, Redux/devtools state, or BroadcastChannel payloads. Gateway
must redact `Cookie` and `Set-Cookie` wholesale. Access Tokens may appear only
in the HTTPS response and in live JavaScript memory; they are forbidden in
localStorage, sessionStorage, IndexedDB, Cache Storage, service-worker durable
state, script-readable cookies, URLs, and logs.

The cookie name selects the audience. Gateway must reject a request containing
both Refresh cookies, a cookie on the wrong audience route, or a returned IAM
Session whose audience differs from the selected cookie. It may never fall
back from one audience cookie to the other.

## 5. Origin, Referer, and CSRF proof

The test-track exact origins are:

- Console: `https://console.example.test`
- BOSS: `https://boss.example.test`

Production values require separate explicit configuration and evidence. No
wildcards, suffix matching, reflected origins, `null`, scheme downgrade,
default-port normalization shortcuts, or request-controlled additions are
allowed. CORS credential responses echo the one matched Origin and set
`Access-Control-Allow-Credentials: true` plus `Vary: Origin`; `*` is forbidden.

For OIDC begin, password login, refresh, logout, and switch:

1. If `Origin` exists, parse it as an origin and require exact serialized
   scheme/host/port equality with the route audience allowlist.
2. Only when `Origin` is absent, parse `Referer` and compare its origin by the
   same exact rule. Do not compare with substring, prefix, suffix, or regex.
3. Reject missing/malformed proof, `Origin: null`, multiple Origin values, and
   conflicting Origin/Referer. Gateway supplies the verified value to IAM's
   internal `origin` field where that field exists; it never trusts an origin
   copied from JSON.

At Session establishment Gateway generates 32 cryptographically random bytes
and base64url-encodes them without padding for the audience CSRF cookie.
Refresh, logout, and switch require `X-CSRF-Token` to be byte-for-byte equal to
that cookie under constant-time comparison. Missing cookie, missing header,
duplicate header, empty value, or mismatch is rejected before IAM is called.
The CSRF value has no authentication authority and is never accepted instead
of the Refresh cookie or bearer.

## 6. In-memory Token and multi-tab algorithm

Each audience uses separate names:

- Web Lock: `ani-auth-refresh:console:v1` or `ani-auth-refresh:boss:v1`
- BroadcastChannel: `ani-auth-session:console:v1` or
  `ani-auth-session:boss:v1`

Before proactive refresh or a 401 recovery, a tab acquires the audience Web
Lock. Inside the lock it first consumes any newer successful refresh epoch
broadcast by another tab. If none exists, it performs exactly one refresh.
After success it broadcasts `{type, epoch, access_token, expires_at,
session_id, grant_id, grant_version}`; receivers keep the payload only in
memory. Refresh Tokens and CSRF values are never broadcast. If Web Locks are
unavailable, BroadcastChannel leader election must serialize epochs; absence
of both mechanisms is an unsupported browser for authenticated use, not
permission to race refresh calls.

On a protected request `401`, the client performs at most one refresh and then
replays only that original request once. A second `401`, refresh `401`, channel
reuse/expired event, or lost strict-rotation response clears in-memory state
and displays the single public message `Session expired`; it does not disclose
reuse detection. A state-changing protected request may be replayed
automatically only when its owning operation already has independently verified
idempotency for the exact original key. Otherwise the client must not
automatically replay it. DP2-16, not DP2-08, owns the uniform 24-hour rule.

On `503`/`504`, the client does not recursively refresh, does not replay the
original protected request, and does not loop across tabs. It retains an
unexpired in-memory Access Token, shows a transient service-unavailable state,
and permits only an explicit user retry or a separately bounded scheduler
attempt. A login or mutation `503` is not converted to `401`.

## 7. Ownership boundary

| Owner | Required behavior |
| --- | --- |
| IAM / DP2-08 | PostgreSQL-authoritative Session/Grant/Family/Token state; Access claims and lifetime caps; atomic rotation, boundary-scoped reuse response, idempotent current-Session logout, target revalidation, transactional Audit; raw Refresh returned only over internal transport |
| Gateway / DP2-13 | exact Origin/Referer and CSRF enforcement; trusted source-IP injection; cookie selection/set/clear; public JSON stripping and no-store; gRPC error mapping; auth-route-only cookie consumption |
| Console / DP2-13 | Console routes/cookies; memory-only Access Token; proactive/single-401 refresh; multi-tab serialization; generic reuse and bounded 503 UX |
| BOSS / DP2-13 plus owning Platform work | BOSS route/cookie isolation and UX; must fail closed while Platform authentication is unavailable |
| Deployment / later platform work | real origins/hosts, TLS, secrets, ingress cookie behavior, browser matrix, rotation and production CORS evidence |

DP2-13 is an adapter ticket. It must not change IAM Session, Grant, Refresh,
reuse, logout, switch, audience, claim, or lifetime semantics. A mismatch is
returned to the owning IAM contract/implementation ticket.

## 8. Acceptance matrix

| Case | Expected result | DP2-08 evidence status |
| --- | --- | --- |
| Console Refresh rotates once and returns same Grant version | one replacement edge; old digest consumed; new digest active | `pass` at IAM unit and real PostgreSQL boundary |
| Reuse old Console token after successful rotation | only that Family revoked; only that Grant version increments; public credential error is generic | `pass` at IAM unit and real PostgreSQL boundary |
| Concurrent Refresh of one token | one success, one generic reuse failure, affected boundary revoked | `pass` with real PostgreSQL/Redis |
| Reuse in Tenant A while same Session has Tenant B | Tenant B Grant/Family remain active and unchanged | `pass` with real PostgreSQL |
| Audit insert fails during rotation | token consumption and replacement insert roll back | `pass` with real PostgreSQL |
| Redis unavailable on Refresh | fail closed as dependency unavailable before PostgreSQL mutation | `pass` at IAM unit and real Redis adapter boundary |
| PostgreSQL unavailable on Refresh | fail closed as dependency unavailable | `pass` with stopped real PostgreSQL pool |
| Logout after Access expiry | current Session and its Grants/Families/Tokens revoked; other Session active; one Audit only | `pass` with real PostgreSQL |
| Switch to suspended target Membership | denied and no target Grant created | `pass` with real PostgreSQL |
| Console lifetime caps | Access 15m, idle 7d, absolute 30d, capped by absolute expiry | `pass` at IAM business boundary |
| Logout repeated with a fresh key | succeeds without revealing whether the Session was already revoked; only the first transition audits | `pass` at IAM unit and real PostgreSQL boundary |
| Same-key replay/conflict/24-hour expiry for Refresh or SwitchTenant | no replay promise in DP2-08; each actual attempt uses a fresh key | `not_verified`, owned by DP2-16 unified Idempotency Ledger |
| BOSS lifetime caps through a Platform Grant | Access 10m, idle 30m, absolute 8h | `not_verified`; Platform Grant is not in DP2-08 |
| Gateway never exposes internal Refresh Token in JSON/logs | exact cookie and redaction behavior | `not_verified`, DP2-13 |
| Origin/Referer and CSRF enforcement | exact allowlist and cookie/header match | `not_verified`, DP2-13 |
| OIDC callback `Set-Cookie` and fixed `303` | no Token/code in final Location | `not_verified`, DP2-13 |
| Console multi-tab refresh | one epoch, no reuse race, memory-only Access Token | `not_verified`, DP2-13 browser E2E |
| 401/503/reuse user experience | one refresh/replay, bounded transient error, generic expired state | `not_verified`, DP2-13 browser E2E |
| BOSS Password/OIDC end to end | isolated BOSS cookie and Platform boundary | `not_verified`; current IAM entry capability is unavailable |

No frontend, Gateway, browser, deployment, or production-CORS row in this
matrix may be promoted to `pass` using IAM unit or storage tests.
