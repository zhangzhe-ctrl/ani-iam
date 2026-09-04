# DP2-02 verification commands

ANI worktree: `/home/chabking/workspace/ANI-direct-p2-01-05`

Historical commands below retain their original `/tmp/ani-direct-p2-01-05` paths as executed evidence. On 2026-09-04 the exact worktree was moved with explicit user authorization, repaired in the parent Git repository, and verified with unchanged branch/HEAD plus unchanged staged-diff SHA-256 `867dccce0ac648967e5ee2188fc46ee6e08b059bb37f3d8fb3fd0a6600508993`.

Fixed ANI source: `0cedae825a489d936cf41815dc27f278f6d3213c`

## Target contract and generation gates

| Result | Command | Observed result |
| --- | --- | --- |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/python services/ani-gateway/tools/dp2_operation_registry_test.py` | `Ran 20 tests in 2.436s`, `OK` |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/python services/ani-gateway/tools/dp2_operation_registry.py --check` | `pass`; committed JSON and gofmt-normalized Go equal a fresh generation |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/python services/ani-gateway/tools/dp2_openapi_breaking_test.py` | `Ran 4 tests in 0.006s`, `OK` |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/python services/ani-gateway/tools/dp2_openapi_breaking.py --check` | `pass`; the fixed-object comparison equals the committed report |
| `pass` | `env PATH=/tmp/ani-direct-p2-01-05-cache/contract-venv/bin:/usr/local/bin:/usr/bin:/bin make validate-openapi-spec` | 15 validator tests, Core OpenAPI and Services OpenAPI all passed |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/openapi-spec-validator api/openapi/v1.yaml` | `api/openapi/v1.yaml: OK` |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/openapi-typescript/node_modules/.bin/openapi-typescript api/openapi/v1.yaml -o /tmp/ani-direct-p2-01-05-cache/console-final.d.ts` | pinned `openapi-typescript 7.13.0` generated successfully |
| `pass` | `cmp -s /tmp/ani-direct-p2-01-05-cache/console-final.d.ts frontends/console/src/api/core-schema.d.ts` | byte-identical |
| `pass` | `cmp -s /tmp/ani-direct-p2-01-05-cache/console-final.d.ts frontends/boss/src/api/core-schema.d.ts` | byte-identical |
| `pass` | `python scripts/validate_sdk_alpha.py` in the isolated target SDK directory | Go, Python and TypeScript smoke passed; Java source smoke passed |
| `not_verified` | Java SDK compile/run smoke | no JDK is installed; source-only smoke ran |
| `pass` | `env GOTMPDIR=/tmp/ani-direct-p2-01-05-cache/go-tmp go test ./services/ani-gateway/internal/authz -run 'TestTarget' -count=1` | target registry, fail-closed lookup, typed obligations, stable error and trusted-header tests passed |
| `pass` | `make validate-architecture` | component import guard and inference legacy-control-plane retirement gate passed |
| `pass` | `make test-python` | Python service syntax gate passed |
| `pass` | `env GOTMPDIR=/tmp/ani-direct-p2-01-05-cache/go-tmp go test ./pkg/... ./services/ani-gateway/... ./services/auth-service/... ./services/envoy-authz-adapter/... ./services/model-service/... ./services/inference-service/... ./services/task-service/... ./services/reconcile-worker/... -timeout 120s` | pre-checkpoint run outside the restricted socket sandbox passed for the complete listed package set |
| `fail` | the same full Go command rerun inside the restricted socket sandbox after context restoration | `pkg/adapters/runtime` panicked when `httptest` tried `listen tcp6 [::1]:0: socket: operation not permitted`; all reported Gateway/auth/IAM-adjacent packages passed or were cached; this is an execution-sandbox diagnostic, not a target-contract failure |

The first attempt to repeat the full Go run outside the sandbox after context restoration did not start because the approval service returned HTTP 404. No workaround or external state change was attempted.

## Final post-migration rerun

| Result | Command | Observed result |
| --- | --- | --- |
| `pass` | target registry tests and both registry/breaking `--check` commands from the durable worktree | 20 registry tests and 4 breaking tests passed; both deterministic checks printed `pass` |
| `pass` | `make validate-openapi-spec` and direct `openapi-spec-validator api/openapi/v1.yaml` | 15 tests passed; Core/Services specs and target Core spec reported valid |
| `pass` | pinned `openapi-typescript 7.13.0` fresh generation plus two `cmp -s` checks | Console and BOSS schemas were byte-identical to the fresh output |
| `pass` | `python scripts/validate_sdk_alpha.py` in `/tmp/ani-direct-p2-01-05-cache/sdk-alpha-final.mL0EME/repo` | Core/Services Go, Python and TypeScript smoke passed; Java source smoke passed; all six approved Core SDK files were byte-identical to this fresh output |
| `not_verified` | Java SDK compile/run smoke | JDK remains unavailable |
| `fail` | first focused Go rerun using the nearly full task `/tmp` cache | build stopped with `disk quota exceeded`; no repository file changed |
| `pass` | focused target Go test after deleting only regenerable task-owned caches | `ok github.com/kubercloud/ani/services/ani-gateway/internal/authz` |
| `pass` | `make validate-architecture` | component import and inference legacy-control-plane guards passed |
| `pass` | `make test-python` | Python service syntax gate passed |
| `pass` | `make validate-doc-entrypoints` | document entrypoint guard and all 5 tests passed |
| `pass` | `env GOTMPDIR=/home/chabking/.cache/ani-direct-p2-01-05-go-tmp go test ./pkg/... ./services/ani-gateway/... ./services/auth-service/... ./services/envoy-authz-adapter/... ./services/model-service/... ./services/inference-service/... ./services/task-service/... ./services/reconcile-worker/... -timeout 120s` | complete listed package set passed outside the restricted socket sandbox, including `pkg/adapters/runtime` |
| `fail` | `env GOTMPDIR=/home/chabking/.cache/ani-direct-p2-01-05-go-tmp make test` | stopped at the expected legacy `validate-auth-contract`: target operationIds are `logoutSession` and `revokeIAMAPIKey`, while the legacy assertion requires `logout` and `revokeAPIKey`; target replacement gates remain `pass` |

## Final commit gate

| Result | Command | Observed result |
| --- | --- | --- |
| `fail` | first compact pre-commit command from `/home/chabking/workspace/ANI-direct-p2-01-05` | stopped before running tests because the repository-relative Python path belongs under `repo/`; no repository file changed |
| `pass` | the same registry, breaking, OpenAPI, focused Go, architecture, Python, document-entrypoint and `git diff --cached --check` gates from `/home/chabking/workspace/ANI-direct-p2-01-05/repo` | 20 registry tests, 4 breaking tests, 15 OpenAPI tests and 5 document-entrypoint tests passed; both deterministic checks printed `pass`; Go and architecture gates passed |
| `pass` | isolated SDK validator plus six `cmp -s` checks | Go/Python/TypeScript and Java source smoke passed; all six approved Core SDK files were byte-identical; Java compile/run remained `not_verified` |
| `pass` | pinned `openapi-typescript 7.13.0` pre-commit generation plus Console/BOSS `cmp -s` | both generated schemas were byte-identical with SHA-256 `d0e522b59245bb0a236ed17a9771fef8b56518f24165a83c0564cc51cbe14459` |
| `pass` | staged path/diff/security audit | exactly 25 accepted paths; sorted path-list SHA-256 `2a97f2e56f14bdc5c6ac4dc0853df0634e39c5b051a5dd3599ef93049e60edeb`; original 15-path diff SHA-256 unchanged at `867dccce0ac648967e5ee2188fc46ee6e08b059bb37f3d8fb3fd0a6600508993`; no staged credential or temporary/build artifact signature found |
| `pass` | `git commit -m "feat(dp2-02): freeze public iam operation registry"` | local ANI commit `a221a7b50c2cfdb13f04c13f154338d836a48af3`; dedicated worktree clean afterward |

## Expected legacy replacement gates

These commands are retained as exact negative evidence. They are not target-contract gates and are not reported as `pass`.

| Result | Command | Exact stopping assertion | Replacement |
| --- | --- | --- | --- |
| `fail` | `make validate-gateway-authz` | legacy generator requires `action`, `boundary`, `principal_kinds`; target contract uses `actions`, `scope`, `obligations` | `dp2_operation_registry_test.py` plus deterministic target registry generation |
| `fail` | `make validate-core-api-compatibility` | requires `/admin/tenants/{tenant_id}/transfer-ownership` | accepted Direct P2 deletion/replacement manifest; no `tenant-owner` target contract |
| `fail` | `make validate-auth-contract` | requires operationIds `logout` and `revokeAPIKey` | exact breaking report records `logoutSession` and `revokeIAMAPIKey` |

## Fixed identity and toolchain checks

| Result | Command | Observed result |
| --- | --- | --- |
| `pass` | `git -C /home/chabking/workspace/ANI cat-file -e 0cedae825a489d936cf41815dc27f278f6d3213c^{commit}` | object exists |
| `pass` | `git -C /home/chabking/workspace/ANI rev-parse 0cedae825a489d936cf41815dc27f278f6d3213c^{tree}` | `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8` |
| `pass` | `sha256sum .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/deletion-manifest.md` | `f68d16af8bc9fa5540e5c5de538997b746f2368029e070d11aff98a6776bd088` |
| `pass` | tool version checks | Go `go1.26.7-X:nodwarf5`; Python `3.14.7`; PyYAML `6.0.3`; openapi-typescript `7.13.0` |

## Runtime boundary

| Result | Item | Reason |
| --- | --- | --- |
| `pass` | Public routes encode zero IAM decisions; protected routes encode exactly one `validate_principal` or `check_permission` decision | generated contract and target helper tests |
| `not_verified` | target registry wired into the running Gateway | runtime cutover is outside DP2-02 and is exercised by DP2-05 |
| `not_verified` | 59 newly added API operations have concrete production handlers | DP2-02 freezes unique logical handler identities; later implementation tickets create the handlers |
| `not_verified` | existing legacy Gateway chain makes at most one IAM RPC | current legacy chain is not the target runtime and is deliberately not changed in this ticket |
