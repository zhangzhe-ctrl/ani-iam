# DP2-02 contract-first RED

The `/tmp/ani-direct-p2-01-05` paths below are preserved as the exact historical RED commands and output. The worktree was later moved, with user authorization and identity/diff verification, to `/home/chabking/workspace/ANI-direct-p2-01-05`.

Result: `fail`

Command:

```text
cd /tmp/ani-direct-p2-01-05/repo
python3 services/ani-gateway/tools/dp2_operation_registry_test.py
```

Observed result before the target policy source or generated registry existed:

```text
....E..
======================================================================
ERROR: test_real_contract_is_complete (__main__.ContractTest.test_real_contract_is_complete)
----------------------------------------------------------------------
dp2_operation_registry.ContractError: cannot read /tmp/ani-direct-p2-01-05/repo/api/openapi/operation-policy.v1.yaml: [Errno 2] No such file or directory: '/tmp/ani-direct-p2-01-05/repo/api/openapi/operation-policy.v1.yaml'

----------------------------------------------------------------------
Ran 7 tests in 0.452s

FAILED (errors=1)
```

This is the intended pre-contract failure: the fixed ANI baseline had no complete target policy input, no strict per-operation owner/exposure/authn/authz/obligation inventory, and no target registry artifact.

## Review hardening RED

Result: `fail`

Command:

```text
cd /tmp/ani-direct-p2-01-05
/tmp/ani-direct-p2-01-05-cache/contract-venv/bin/python repo/services/ani-gateway/tools/dp2_operation_registry_test.py
```

Observed result after converting the independent Spec review findings into gates:

```text
Ran 20 tests in 1.790s
FAILED (failures=2, errors=3)
```

The five failures prove that the first generated contract did not yet reject a missing OpenAPI owner/handler, did not reject duplicate declared Gateway handlers, omitted the explicit one-RPC decision kind, did not enumerate the policy mismatch/unregistered 503 reasons, and did not trace the accepted DP2-01 deletion IDs `D001-D028`. These failures are preserved before the corresponding source and generator corrections.
