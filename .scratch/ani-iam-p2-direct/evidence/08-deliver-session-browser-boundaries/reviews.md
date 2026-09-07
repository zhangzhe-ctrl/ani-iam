# DP2-08 independent review record

Status: `pass`; final independent Standards and Ticket Spec reviews completed.

## Standards axis

Independent result: `fail`.

Blocking findings were:

1. The first handoff draft assigned both audience cookies `Path=/auth`, contrary
   to the accepted separate-name, separate-Path and separate-audience boundary.
2. Tenant Refresh and SwitchTenant accepted an anomalous `boss` Session despite
   the absence of a Platform Grant implementation.

Corrective work now present in the working tree:

- the handoff introduced separate `/auth/console/*` and `/auth/boss/*` edge
  route families with matching cookie Paths; the later human checkpoint accepted
  this caller design without authorizing ANI changes in DP2-08;
- Tenant Refresh and SwitchTenant now fail closed unless Session and claims use
  the Console audience, with targeted RED then GREEN regression tests;
- the duplicated Console/BOSS lifetime selection was consolidated into one
  policy function.

Non-blocking design smells recorded by the reviewer were the broad composed
authentication ports and large SQL-row conversion field groups. They do not
justify widening the claimed ticket or changing the accepted composition root.

## Ticket Spec axis

Independent result: `fail`.

Implementation findings were:

1. Tenant Refresh/SwitchTenant accepted BOSS audience state.
2. Cookie Paths were not audience-isolated.
3. DP2-08's new Session/Refresh version fields were left as zero values by the
   DP2-07 OIDC login path even though PostgreSQL persists `1` and `active`.

The BOSS and OIDC findings have targeted GREEN fixes. The Cookie fix initially
required a human routing decision because the frozen ANI OpenAPI currently uses
shared `/auth/*` routes and ANI is read-only in this ticket; that decision is
recorded below.

The review also exposed an authority conflict that the agent may not decide:

- the target plan says every public mutation eventually uses a unified durable
  24-hour idempotency ledger;
- DP2-16 explicitly owns delivery of that unified ledger;
- strict Refresh rotation says a lost response is not replayable and the old
  token's next presentation is reuse; storing a plaintext or encrypted
  replacement response is not accepted in DP2-08;
- the first handoff draft nevertheless allowed a byte-equivalent retry to reuse
  an idempotency key, while the current implementation has no replay ledger.

The recommended DP2-08 closure was to delete the false replay promise, treat each
actual Refresh/Switch attempt as a fresh key, preserve semantic idempotence for
Logout, and leave unified replay/conflict/expiry behavior to DP2-16. The human
acceptance of that narrowing is recorded below.

## Re-review gate

Second independent Standards and Spec reviews both verified:

- OIDC continuity objects match the durable `Version: 1` / `active` state;
- active and consumed BOSS-audience Tenant Refresh fail before mutation;
- BOSS SwitchTenant fails before mutation or token issuance;
- the shared lifetime policy removed the duplicated rule;
- BOSS Platform and browser evidence remain `not_verified`.

The human owner accepted both closure recommendations on 2026-09-08:

- the unified 24-hour ledger remains owned by DP2-16; DP2-08 makes no same-key
  replay promise, Refresh/Switch use a fresh key per actual attempt, and Logout
  retains only its ledger-independent semantic idempotence;
- `/auth/{audience}/*` with Console `Path=/auth/console` and BOSS
  `Path=/auth/boss` is the frozen DP2-13 caller design, without expanding or
  modifying any ANI path in DP2-08.

The handoff and ticket boundaries now reflect those decisions.

## Final independent review

Review fixed point:
`d51a1ec9f72a288f489f0ac97ad76ab0de8e5b33`. Because DP2-08 remained an
uncommitted ticket-local worktree, each reviewer inspected the tracked diff from
that exact point plus every non-ignored untracked file; the commit list was
empty.

### Standards

Final result: `pass`; zero documented-standard blockers.

The reviewer confirmed that the BOSS active/consumed Refresh and Switch paths
fail before mutation, OIDC continuity state matches persistence, one lifetime
policy owns both audiences, the handoff records the accepted route/Ledger
boundaries, and there is no ANI diff. Three judgement-only smells were reported:

- the composed authentication ports remain broad for some test fakes
  (`Refused Bequest` / `Shotgun Surgery`);
- two PostgreSQL row projections retain large field groups (`Data Clumps` /
  `Duplicated Code`);
- one unused Refresh-Family audit target constant was `Speculative Generality`.

The unused constant was removed in the review refactor. The first two do not
violate a repository rule and are not grounds to widen DP2-08.

### Ticket Spec

Final result: `pass`; zero missing/partial requirements, scope creep findings or
incorrect implementations. The reviewer explicitly verified the OIDC state,
BOSS fail-closed paths, historical removed-Membership exclusion, Allowed paths,
and the honest DP2-13/DP2-16 deferrals. No frontend or ANI evidence was promoted
to `pass`.
