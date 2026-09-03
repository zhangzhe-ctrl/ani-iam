# 被否决旧门禁与替代检查

## Gate A：Gateway authz generated drift

| 字段 | 记录 |
| --- | --- |
| old_gate | `make validate-gateway-authz`，核心断言为从 Core OpenAPI 重生成 registry 后与 `zz_generated_core_policies.go` 无漂移，并要求 registered Core routes 被 registry 覆盖。 |
| historical failure | 先前 PR/分支运行曾输出 `generated gateway authz policy drift; run make gen-gateway-authz`。这是历史失败证据，不是本次冻结 SHA 的当前输出。 |
| current frozen rerun | 在 `git archive <SHA>` 快照中：generator tests 18/18 OK；`gateway authz registry: no drift`；route coverage 为 284 registered、234 registry、0 errors。 |
| invalid_reason | “旧 generated registry 必须永远维持旧 owner/route 形状”不能成为 P2 目标门禁；目标要求 OpenAPI 标注生成单一 operation/owner/policy registry，删除 legacy fallback，并允许受控删除旧 operation。历史 drift 也不能被重命名成当前 pass。 |
| target_contract | Public 0 次 IAM；Authenticated-only 一次 ValidatePrincipal；Authorized 一次 CheckPermission；未知 operation/digest mismatch fail closed；一个 operation 只有一个 Handler/Owner；P2 删除 legacy 推导与双轨。 |
| replacement_gate | `replacement_gate.py` 固定 SHA、Proto/generated/OpenAPI 摘要和 14 RPC；同时断言目标计划包含 generated operation registry、one-call 与无 fallback 不变量。实现阶段由事项16/26/35/37增加真正的 OpenAPI→registry clean diff、owner/obligation coverage 和 zero-reference gate。 |
| reviewer | 待人工接受本 evidence digest/内容；Agent 不填写。 |

## Gate B：protected Core transfer-ownership path

| 字段 | 记录 |
| --- | --- |
| old_gate | `make validate-core-api-compatibility`，Core v1 baseline 要求 `/admin/tenants/{tenant_id}/transfer-ownership` 与 `changeable-roles` 受保护路径继续存在。 |
| historical failure | 先前 PR/分支运行曾输出 `missing protected path /admin/tenants/{tenant_id}/transfer-ownership`。这是历史失败证据，不是本次冻结 SHA 的当前输出。 |
| current frozen rerun | 在 `git archive <SHA>` 快照中输出 `core api compatibility valid`；冻结 OpenAPI 与 compatibility baseline 都仍包含 transfer-ownership/changeable-roles。 |
| invalid_reason | 当前 accepted 目标明确不保留 `TransferOwnership`、`tenant-owner` 和重叠 Tenant User/Admin operation；把旧 path 永久存在作为 P2 合并条件与破坏性替换目标冲突。但 CP0/P1 仍需把其旧行为作为冻结 Oracle，不能提前删除。 |
| target_contract | IAM 的最后管理员不变量、`RestoreTenantAdmin` 与 Recovery Bootstrap 取代 owner transfer；Core 只拥有 Tenant Lifecycle/Quota，IAM 拥有 Membership/Role。 |
| replacement_gate | `replacement_gate.py` 同时断言冻结 OpenAPI 仍有旧 path（保证 CP0 Oracle）和当前目标计划明确排除 TransferOwnership/tenant-owner（保证不会误把旧 baseline 变成目标权威）。事项16冻结目标 OpenAPI/deletion manifest，事项21/32覆盖 last-admin/recovery，事项37执行 zero-reference 删除。 |
| reviewer | 待人工接受本 evidence digest/内容；Agent 不填写。 |

## 结论

两个旧门禁在指定基线已经转绿，状态不是“当前失败”。Replacement gate 的作用是把“CP0 必须冻结旧行为”与“P2 必须允许受控删除旧契约”分开，避免旧兼容门禁成为目标架构的永久否决权。

当前 replacement gate 只校验静态权威关系，状态 `pass`；人工接受仍待定，不能自行解锁事项02。
