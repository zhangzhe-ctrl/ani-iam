---
status: accepted
---

# Keep browser Refresh Tokens in HttpOnly cookies

Console and BOSS browser clients receive an Access Token in the authentication or refresh response and retain it only in process memory. A Refresh Token is delivered and rotated only through a host-scoped, `Secure`, `HttpOnly`, `SameSite=Lax` cookie; JavaScript cannot read it. Official browser clients do not persist either Token in localStorage, sessionStorage, IndexedDB, or a script-readable cookie.

Console and BOSS use different cookie names, constrained paths, and audiences. A Console cookie cannot refresh a Platform Token. The browser sends the relevant cookie automatically when the client calls the refresh route with credentials enabled; a successful response returns only the new Access Token and expiry metadata in JSON and uses `Set-Cookie` for the rotated Refresh Token.

Refresh, logout, and Tenant-switch operations validate both an allowlisted Origin or Referer and an independent CSRF token. They do not rely on SameSite alone. A valid Refresh cookie and CSRF proof can perform idempotent logout even after the Access Token has expired.

Only explicitly registered authentication routes may consume a Refresh cookie. Gateway strips it before forwarding ordinary product requests, so Core and Services never receive the browser Refresh credential.

OIDC redirect URIs require exact configured scheme, host, port, and path equality. Wildcards, request-controlled redirects, and partial host matching are rejected.

Gateway removes every client-supplied `x-ani-*` identity header before injecting the minimal IAM-verified context. Core and Services trust that context only over an authenticated Gateway mTLS workload connection and do not combine it with an independently client-supplied identity.

When IAM cannot determine authoritative resource ownership, it returns a typed Authorization Obligation. The owning Core or Services Handler loads the resource and checks its actual Tenant or owner. An operation lacking the required generated obligation Handler is not registrable; Gateway never queries a business database to approximate ownership.

The current test environment permits any Origin only on routes that do not use browser credentials, including bearer-token and API Key routes. Refresh, logout, Tenant-switch, and OIDC routes keep an explicit Origin allowlist even in the test environment. Production CORS remains a Production Readiness decision.

A successful refresh returns only the new Access Token, token type, expiry, and non-sensitive Session and boundary summary in JSON. It rotates the Refresh Token with `Set-Cookie`. The official frontend refreshes about one minute before Access Token expiry, falls back to a single refresh after an expiry `401`, allows only one refresh request at a time, and retries the original request at most once.

Refresh rotation is strict in the basic version. If the server consumes a Refresh Token but the rotated response is lost, presenting the old Token again is reuse: IAM revokes the Boundary Grant and the user signs in again. P2 does not store an encrypted replacement response or allow old and new Refresh Tokens to overlap.

Browser tabs coordinate refresh through Web Locks or BroadcastChannel so one browser Session does not race its single active Family. A separate, script-readable random CSRF cookie is echoed in `X-CSRF-Token` and checked together with Origin; it contains no authentication authority and is distinct from the HttpOnly Refresh cookie.

The Refresh cookie expires no later than its Session's absolute deadline, while server Session state remains authoritative. Non-browser clients receive no JSON Refresh Token in the basic version: external CLI and SDK automation use a Service Principal API Key, internal workloads use Service Tokens, and a future Human CLI login would require a separate Device Authorization Flow decision.

The fixed Gateway OIDC callback exchanges the code, creates the Session, sets the Refresh cookie, and responds with a `303` to a fixed Console or BOSS page. Tokens and authorization codes never enter the redirect URL. On refresh failure, `401` clears frontend Access Token state and returns to login, `503` shows a transient service failure without an infinite retry loop, and reuse presents a generic expired-session result without disclosing detection details.
