# DP2-03 producer-consumer fixtures

| Fixture | Producer/consumer purpose | Result |
| --- | --- | --- |
| `iam_password_login.v1.json` | ANI-side consumer decodes `iam.v1.PasswordLoginRequest` from pinned IAM descriptor. | `pass` |
| `iam_check_permission.v1.json` | ANI-side consumer decodes one protected-operation decision request, including DP2-02 policy revision. | `pass` |
| `core_tenant_lifecycle_changed.v1.json` | IAM-side consumer decodes a complete Core lifecycle fact with schema major and monotonic aggregate version. | `pass` |
| `core_tenant_iam_bootstrap_requested.v1.json` | IAM-side consumer decodes Core bootstrap intent and verifies canonical payload SHA-256. | `pass` |
| `core_tenant_lifecycle_heartbeat.v1.json` | IAM-side consumer decodes the pipeline progress heartbeat. | `pass` |
| `core_tenant_lifecycle_snapshot_page.v1.json` | IAM-side consumer decodes a cursor-bound snapshot page and version. | `pass` |
| `iam_error_contract.v1.json` | Both repositories validate all 19 IAM error reasons, gRPC codes, ErrorInfo domain, and required metadata. | `pass` |
| `core_error_contract.v1.json` | Both repositories validate all 7 Core integration error reasons, gRPC codes, ErrorInfo domain, and required metadata. | `pass` |

Both repositories assert the exact eight-file inventory; an extra or missing fixture fails. They reject unknown JSON fields, construct messages from the pinned peer descriptor, and round-trip `google.rpc.ErrorInfo`. Every non-unspecified error enum value has exactly one fixture rule, and every fixture rule maps to a declared enum value.

The eight fixtures and `contract_pins.json` are byte-identical across repositories. Result: `pass`.

The tests share no internal Go package and use no database, broker, runtime server, or mutable sibling worktree. Real producer publication and consumer processing are `not_verified`.
