# DP2-02 SDK and caller impact

## Console and BOSS generated TypeScript schemas

Result: `pass`

Pinned generator: `openapi-typescript 7.13.0`.

The generated Console and BOSS `core-schema.d.ts` files are byte-identical to a fresh generation from the target OpenAPI. Each has SHA-256 `d0e522b59245bb0a236ed17a9771fef8b56518f24165a83c0564cc51cbe14459`.

Both browser callers must adapt to the six operationId changes and the target browser credential contract:

- password login and refresh return an access token in JSON but rotate the refresh token only with `Set-Cookie`;
- Console and BOSS use separate refresh-cookie schemes;
- logout and refresh require the target CSRF/origin behavior;
- API Keys are owned by explicit Service Principals and use the renamed IAM operations;
- stable error handling must accept `401/403/409/429/503/504` reason codes from the target contract.

No Console/BOSS runtime switch or UI implementation occurs in DP2-02, so caller runtime compatibility is `not_verified`.

## Four-language Core SDK impact

Result: `pass`

The fixed baseline and target were generated in isolated task-owned directories with `repo/scripts/gen_sdk_alpha.py`, then checked with `repo/scripts/validate_sdk_alpha.py`.

| Metric | Fixed baseline | Target |
| --- | ---: | ---: |
| Operations | 236 | 295 |
| Schemas | 317 | 362 |
| Stable error codes | 33 | 42 |

The exact source diff is six generated files, 1321 insertions and 53 deletions:

- `repo/sdks/core/go/anisdk/client.go`
- `repo/sdks/core/java/src/main/java/com/kubercloud/ani/core/ApiClient.java`
- `repo/sdks/core/python/kubercloud_ani_core/client.py`
- `repo/sdks/core/sdk-metadata.json`
- `repo/sdks/core/typescript/src/index.mjs`
- `repo/sdks/core/typescript/src/index.ts`

Operation identities change by 64 additions and 5 removals at the generated symbol level: 59 added routes plus six renamed/missing operationIds, with one added `getBranding` symbol replacing a previously derived missing ID. Schema identities add 45 and remove none.

Target generated-file hashes:

| File | SHA-256 |
| --- | --- |
| Go client | `2d07cd17b16ad92c728e18b5c5b13431187db904d714fe8d6da967d0c59a1e69` |
| Java client | `ba0082b6c42bb07f8002e56ddb6a1744f14361dd86f2905ea7ab6438bb50d4f7` |
| Python client | `e8e3664560183a3fc96283d5f294d00bcb79b54cf97fc86b7f9d0b46e9289457` |
| SDK metadata | `349422566a1c24c6ade9dd42930ba9002f8ce714a761968224d14d089d8da74c` |
| TypeScript runtime | `9def646e21d2acf2c39f7d42af520d25346a068b9142706a5b654e81e91ad596` |
| TypeScript declarations | `663eca4860fb371c6d11ab368a794dc39f745dd9725e85253317ccd4d1ff43d6` |

Go, Python and TypeScript smoke checks are `pass`. Java source smoke is `pass`; Java compile/run is `not_verified` because no JDK is installed.

## Accepted allowed-path expansion

Result: `pass`

The original ticket allowed `repo/pkg/generated/**` but not `repo/sdks/**`. The user accepted the exact expansion for these six regenerated Core SDK files on 2026-09-04:

```text
repo/sdks/core/go/anisdk/client.go
repo/sdks/core/java/src/main/java/com/kubercloud/ani/core/ApiClient.java
repo/sdks/core/python/kubercloud_ani_core/client.py
repo/sdks/core/sdk-metadata.json
repo/sdks/core/typescript/src/index.mjs
repo/sdks/core/typescript/src/index.ts
```

All six outputs were copied from the isolated fresh generation into the dedicated ANI worktree and verified byte-identical. No other `repo/sdks/**` path changed.
