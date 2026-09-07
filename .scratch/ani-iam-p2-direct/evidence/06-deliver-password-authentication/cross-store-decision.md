# DP2-06 PostgreSQL/Redis decision checkpoint

## Result

`accepted` by the Owner on `2026-09-07` for DP2-06 closure under the exact
conservative boundary below. This is not a claim of strict PostgreSQL/Redis
atomicity. Full gates, review, exact staging, and a local DP2-06 commit are
authorized; push, deployment, and cluster action remain unauthorized.

## Facts

PostgreSQL owns durable credential failures, lock state, Session/Grant/Refresh
state, and Security Audit. Redis owns the normalized-account and source-IP
abuse-control windows. These systems have no shared transaction.

The reviewed implementation is deliberately conservative:

1. Redis `Check` fails before credential verification or a login mutation.
2. An invalid known credential commits its durable failure and Audit before
   recording the Redis failure. If that Redis write then fails, the response is
   dependency 503 while the durable failure remains. This can only tighten the
   account; it cannot create a Session or authenticate a caller.
3. A valid credential commits Session/Grant/Refresh/Audit before resetting the
   Redis account bucket. If reset fails, the committed login is returned as
   success and the old bucket remains until a later successful reset or its
   fifteen-minute TTL. Returning 503 after the commit would invite a client
   retry that could create a second Session while the first already exists.
4. Source-IP failure state is intentionally not cleared by an account's
   successful login.

The implementation also now provides increasing Redis cooldowns and masks a
durable PostgreSQL lock as the same public invalid-credential result used for an
unknown account. Those independent review blockers are closed.

## Why strict zero-partial-state is not implementable in the current model

Changing call order only moves the crash window. Redis-first can leave a Redis
mutation when PostgreSQL fails; PostgreSQL-first can leave a PostgreSQL
mutation when Redis fails. A compensating call can itself fail. A truthful
strict guarantee therefore needs a durable, idempotent coordination protocol,
including recovery after process death and idempotent replay of login results.
Password login currently cannot persist raw refresh or access tokens, so such a
protocol also needs an approved response-recovery design rather than a local
retry trick.

## Owner decision

The user explicitly approved the recommended boundary:

```text
批准推荐边界：接受安全保守的有限跨存储状态，并继续完整门禁、审查和 DP2-06 提交。
```

DP2-06 therefore distinguishes unauthorized partial success from conservative
security-state persistence:

- Redis pre-check outage remains dependency 503 with no login mutation.
- A post-Audit Redis failure may retain the durable failed-attempt/Audit and
  return dependency 503.
- A post-login Redis account-reset failure may retain the bounded old bucket
  but does not invalidate or turn the already committed login into an error.
- Full cross-store durable coordination, repair observability, and login-result
  replay are deferred to an explicitly scoped future issue and remain
  `not_verified`; they cannot be claimed as strict atomicity here.

This decision does not widen Allowed paths or authorize a durable coordination
subsystem, push, deployment, certificate issuance, Secret mounting, rotation,
or any cluster action.
