# DP2-02 breaking checkpoint package

Result: `pass` — exact breaking diff and the two path expansions accepted by the user on 2026-09-04.

## Final ANI commit scope

The original contract patch contained these 15 ticket-allowed paths:

```text
repo/api/openapi/iam-replacement.v1.yaml
repo/api/openapi/openapi-breaking.v1.json
repo/api/openapi/operation-policy.v1.yaml
repo/api/openapi/operation-registry.v1.json
repo/api/openapi/v1.yaml
repo/frontends/boss/src/api/core-schema.d.ts
repo/frontends/console/src/api/core-schema.d.ts
repo/services/ani-gateway/internal/authz/target_operation_registry.go
repo/services/ani-gateway/internal/authz/target_operation_registry_test.go
repo/services/ani-gateway/internal/authz/target_trusted_headers.go
repo/services/ani-gateway/internal/authz/zz_generated_target_operation_registry.go
repo/services/ani-gateway/tools/dp2_openapi_breaking.py
repo/services/ani-gateway/tools/dp2_openapi_breaking_test.py
repo/services/ani-gateway/tools/dp2_operation_registry.py
repo/services/ani-gateway/tools/dp2_operation_registry_test.py
```

## Exact expansion requested at the same checkpoint

Six generated Core SDK files:

```text
repo/sdks/core/go/anisdk/client.go
repo/sdks/core/java/src/main/java/com/kubercloud/ani/core/ApiClient.java
repo/sdks/core/python/kubercloud_ani_core/client.py
repo/sdks/core/sdk-metadata.json
repo/sdks/core/typescript/src/index.mjs
repo/sdks/core/typescript/src/index.ts
```

Four ANI Feature-batch documentation closure paths:

```text
repo/development-records/DP2-02-public-iam-operation-registry.md
repo/development-records/README.md
repo/CURRENT-SPRINT.md
ANI-06-开发计划.md
```

The user accepted all ten expansion paths. The six SDK outputs are byte-identical to an isolated fresh generation, and the four ANI Feature-batch documents close the repository's required documentation loop. The final ANI commit therefore contains exactly 25 paths: the 15 original paths plus these ten approved paths.

Accepted local ANI commit: `a221a7b50c2cfdb13f04c13f154338d836a48af3` (`feat(dp2-02): freeze public iam operation registry`). The dedicated ANI worktree was clean immediately after the commit.

## Caller impact

- Gateway: target policy is explicit and fail closed, but runtime wiring is `not_verified` until DP2-05.
- Console and BOSS: generated schema impact is present; UI/runtime consumption is `not_verified`.
- Four-language Core SDK: isolated generated diff and smoke are `pass`; exact worktree inclusion is `pass` after path approval and byte comparison.

## Recovery

Recovery is a new revert commit against exact local SHA `a221a7b50c2cfdb13f04c13f154338d836a48af3`. Do not reset, stash, amend, force, push, deploy or switch traffic.

No current route, caller, deployment, credential, database, remote artifact or legacy runtime has been changed.
