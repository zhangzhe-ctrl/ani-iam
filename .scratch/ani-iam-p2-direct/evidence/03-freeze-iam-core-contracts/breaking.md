# DP2-03 exact breaking assessment

## Immutable comparison points

- IAM start: `main@5ff9f3cfe083b3b911bb076450abbbb967e82a37`.
- ANI start: `codex/direct-p2-01-05@a221a7b50c2cfdb13f04c13f154338d836a48af3` in `/home/chabking/workspace/ANI-direct-p2-01-05`.
- The ANI comparison tree was archived from the start commit into the task-owned cache; no moving branch or existing ANI checkout was used.

## Source-level diff

| Change | Result/impact |
| --- | --- |
| Add `iam.v1.AuthenticationService` | 15 new methods; target replacement surface. |
| Add `iam.v1.AuthorizationService` | 1 new `CheckPermission` method; target one-decision surface. |
| Add `iam.v1.IAMAdminService` | 53 new management methods. |
| Add common IAM messages/enums/error contract | New target major. |
| Add Core-owned `tenant.integration.v1` events/messages | New lifecycle, heartbeat, bootstrap, envelope, and snapshot types. |
| Add Core-owned `TenantIAMIntegrationService` | 2 read-only snapshot methods. |
| Remove or rename a pre-existing target IAM/Core symbol | None; these versioned target packages did not exist at the two starts. |
| Change an existing ANI Proto package | None. Buf breaking against complete `a221a7b...` module: `pass`. |

## Semantic breaking boundary

This is a replacement contract, not an additive extension of legacy `auth.v1.AuthService`. At future cutover:

- legacy 14-RPC clients cannot call `iam.v1` without regeneration and explicit mapping;
- target descriptor intentionally excludes `auth.v1.AuthService`, `ValidateToken`, legacy `CheckPermissionV2`, old refresh/revoke semantics, and direct user/API-key ownership assumptions;
- Gateway must map the DP2-02 public operation registry to the target services and stable reasons;
- Core must publish the new versioned lifecycle/bootstrap facts and expose the read-only snapshot protocol before a real consumer is enabled.

No caller is switched, no remote artifact is published, and no legacy service is deleted in DP2-03. Those effects are `not_verified`.

## Producer-consumer impact

- ANI receives generated Core types, a Core descriptor, an independently written consumer test, a pinned IAM descriptor, and byte-identical fixtures.
- ani-iam receives generated IAM types/descriptor, a pinned Core descriptor, and independent descriptor/fixture tests.
- Both builds remain independent; neither imports the other's internal source tree.
- DP2-02 has 87 IAM-owned public operations while DP2-03 freezes 69 canonical internal RPCs. A public operation is not required to map one-to-one to an internal RPC. Exact Gateway operation-to-RPC runtime wiring is `not_verified` and remains a DP2-05/cutover gate.

## Recovery

After the accepted local commits, recovery uses new revert commits in reverse dependency order: ANI `573d3735934f74f9f1eb78818cddefefd9f575eb`, then ani-iam `1bdc3e3657c233b5a47be706f251a4529ec80b5b`. Do not reset, stash, amend, force, push, publish, deploy, or switch callers.
