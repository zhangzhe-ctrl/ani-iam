# 最终删除与 zero-reference 清单

本清单冻结“需要删除什么”和“何时才允许删除”，不授权当前删除。所有 runtime 删除至少受 DP2-14 整组回退演练、DP2-18 最终测试切换和 DP2-19 精确人工删除确认约束。当前 DP2-01 只证明固定来源中引用存在且可扫描。

## 扫描口径

未来 zero-reference 扫描必须在专用 ANI worktree 的精确提交上运行，且至少包含 runtime、Proto、generated client/mock、OpenAPI、SDK、前端、测试、构建、installer 和 deploy。以下历史路径允许保留旧名称，不计 runtime 引用：

- `docs/adr/**`
- `docs/development-records/**`
- `.scratch/**`
- 明确标为 archive/historical 的 evidence 或 migration snapshot

任何未在上述排除集中的命中都必须解释或阻止删除。不能通过扩大排除路径让门禁变绿。

| ID | 精确删除目标/搜索词 | 类型 | replacement gate | zero-reference 条件 | 历史例外 |
| --- | --- | --- | --- | --- | --- |
| D001 | `repo/api/proto/auth/v1/auth_service.proto` 与 `auth.v1.AuthService` generated pb/grpc/mock | runtime contract | DP2-03 目标 descriptor；DP2-13 callers；DP2-17 delete readiness | 非历史 `rg -n 'auth\.v1\.AuthService' repo` 为零，descriptor 无旧 service | ADR/evidence 可保留 full method |
| D002 | `repo/services/auth-service/**` runtime | runtime service | DP2-13 五类调用方；DP2-14 回退；DP2-18 切换；DP2-19 确认 | build、go.work、deploy、client、route、test 对目录/二进制引用为零 | archived source snapshot 可保留 |
| D003 | Gateway `ValidateToken`, old `CheckPermission`, `CheckPermissionV2` clients/middleware | runtime caller | DP2-02 registry；DP2-03 target client；DP2-13 Gateway E2E | Gateway 非历史代码/测试对三个旧 method/type 引用为零 | historical evidence |
| D004 | Envoy old Auth client/`ValidateToken` | runtime caller | DP2-10 target credential；DP2-13 Envoy E2E | Envoy code/config/test/generated 对旧 Proto和method引用为零 | development records |
| D005 | Inference old `IssueServiceToken` client | runtime caller | DP2-11 mTLS/SPIFFE token；DP2-13 Inference E2E | Inference code/config/test 对旧 Proto和method引用为零 | development records |
| D006 | `AUTH_SERVICE_ADDR`, `AUTH_SERVICE_GRPC_ADDR`, `ani-auth-service.ani-system.svc.cluster.local:9101` | config/runtime | DP2-13 caller E2E；DP2-14 rehearsal；DP2-19 | code/config/deploy/installer 非历史引用为零 | historical docs/evidence |
| D007 | `AUTH_SERVICE_MINT_SECRET`, `AUTH_SERVICE_MINT_CREDENTIALS` | secret/config | DP2-11 workload identity；DP2-19 | config struct、env lookup、manifest、Secret key和测试引用为零 | redacted historical evidence |
| D008 | old `AUTH_JWT_*` 与旧 Auth signing config | secret/config | DP2-08 target key ring/KMS；DP2-13；DP2-19 | runtime/config/deploy/installer引用为零 | historical key-name evidence；不得保留 secret |
| D009 | old `AUTH_OIDC_*` 与旧 Auth Dex client config | config/runtime | DP2-07 target OIDC E2E；DP2-13；DP2-19 | runtime/config/deploy/installer旧变量引用为零 | historical evidence |
| D010 | Gateway `ANI_AUTH_MODE`, dev bypass, legacy fallback | runtime behavior | DP2-02 fail-closed registry；DP2-05 no-default-allow；DP2-13 | variable、branch、default identity、fallback和对应测试引用为零 | historical evidence |
| D011 | credential input `X-API-Key` | public/runtime contract | DP2-02 auth method registry；DP2-10 API Key E2E | OpenAPI、Gateway、SDK、frontend、test runtime输入引用为零 | docs describing removal |
| D012 | Inference `CORE_SERVICE_TOKEN` fallback | runtime behavior | DP2-11 dependency failure；DP2-13 | config/env/manifest/code branch/test引用为零 | historical evidence |
| D013 | Inference `X-Dev-Tenant-ID`, `X-Dev-Principal-Kind`, `X-Dev-Service-Scope` | runtime behavior | DP2-11 workload identity；DP2-13 spoofing tests | non-historical code/config/test引用为零 | development records |
| D014 | `jwt:blocklist:` Redis key 与 `jwt_blocklist` table/adapter | storage/runtime | DP2-04 target schema；DP2-08 Grant revoke/reuse；DP2-19 | runtime/migration/config/test对key/table/adapter引用为零 | historical RLS/Redis evidence |
| D015 | old `refresh_tokens` table shape、`ani_refresh_` 与 non-rotating JSON refresh | storage/contract | DP2-03 target contract；DP2-04 schema；DP2-08 browser/rotation | old table/query/prefix/JSON response/frontend storage引用为零 | historical migration/evidence |
| D016 | shared Auth `users`, `roles`, `user_roles`, `api_keys` tables and Auth RLS policies | storage/runtime | DP2-04 empty target DB；DP2-10 target API Key；DP2-18 cutover；DP2-19 | IAM runtime不查询共享表；目标migration无旧表/RLS；旧schema删除需DP2-19 | archived migration/evidence |
| D017 | nullable Tenant/Platform boundary and old generic `deleted_at` IAM behavior | storage/domain | DP2-04 schema/invariant；DP2-09 authorization | target IAM code/schema/query没有null/special Tenant或generic soft-delete path | ADR/evidence可描述旧行为 |
| D018 | 28 个 core-v1 IAM-related 旧 path/schema/handler中被替换项 | public contract | DP2-02 精确 breaking/owner diff；DP2-13 caller E2E；DP2-17 | deletion manifest逐operation核对；被删operation在route/handler/SDK/UI为零 | historical OpenAPI snapshots |
| D019 | 31 个 `/api/v1/svc` IAM重叠入口 | public contract | DP2-02 精确 breaking；DP2-13 consumers；DP2-17 | OpenAPI route、Gateway route、Services handler、SDK/UI runtime引用为零 | historical docs/evidence |
| D020 | `TransferOwnership`, `tenant-owner`, generated transfer schema | public/domain | DP2-02 breaking inventory；DP2-17 | OpenAPI/generated SDK/frontend/runtime/test引用为零 | accepted plan与历史records |
| D021 | Console/BOSS localStorage/sessionStorage access/refresh token keys | browser/runtime | DP2-08 Cookie/CSRF/rotation；DP2-13 browser E2E | token storage读写、migration helper和JSON refresh token引用为零 | docs/test fixtures不得含真实token |
| D022 | BOSS role-claim default allow | browser/runtime | DP2-02 authorized classification；DP2-13 deny E2E | default-allow branch和role-as-authority检查引用为零 | historical evidence |
| D023 | auth-service Deployment/Service/ServiceAccount/image/build/go.work | deploy/build | DP2-14 rollback；DP2-18 cutover；DP2-19 explicit approval | manifest、Makefile、go.work、image target和Service selector引用为零 | archived deploy snapshot |
| D024 | installer profiles与offline artifacts中的旧 Auth component | install/artifact | DP2-14 clean rehearsal；DP2-20 clean install | active profile、chart values、artifact manifest与install tests引用为零 | release history |
| D025 | old migration directory checksum as IAM gate | build/storage | DP2-04 complete target `atlas.sum` and empty replay | target IAM CI/runtime完全不消费旧 shared migration目录 | historical migration snapshot |
| D026 | Gateway business DB/runtime/service composition | runtime ownership | DP2-13 thin Gateway；DP2-17 zero-reference | Gateway composition root无业务DB、Tenant/Quota Saga或本地JWT verifier | architecture records |
| D027 | `tenant-service` runtime 与已迁移给 Core/IAM 的重叠能力 | runtime ownership | DP2-13/15-17 ownership completion；DP2-18；DP2-19 | operation/handler/client/data owner矩阵无tenant-service owner；runtime/build/deploy引用为零 | historical records |
| D028 | P1 canary/differential runtime入口与 legacy compatibility facade | runtime | DP2-13 target-only；DP2-14 no-code-fallback；DP2-19 | non-historical code/config/test/build引用为零 | ani-iam `.scratch` archived evidence |

## 当前固定对象初始扫描

以下命令用于证明删除目标在固定来源中确实存在，并为后续零引用比较提供相同搜索词。DP2-01 的期望是产生非零命中；零命中只有在相应 replacement gate 后才是删除完成证据。

```bash
git -C /home/chabking/workspace/ANI grep -n 'auth\.v1\.AuthService' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'AUTH_SERVICE_ADDR' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'AUTH_SERVICE_GRPC_ADDR' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'AUTH_SERVICE_MINT_SECRET' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'X-API-Key' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'ANI_AUTH_MODE' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'jwt:blocklist:' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI grep -n 'tenant-owner' 0cedae825a489d936cf41815dc27f278f6d3213c -- repo
```

初始扫描本身不删除任何文件。完整 replacement matrix 见 [replacement-matrix.md](replacement-matrix.md)，其中每个旧行为都明确关联至少一个删除条件。

## 恢复边界

DP2-14 以前，旧 Auth 只作为固定部署级回退目标保留，目标代码不得 fallback。DP2-18 的最终测试切换需要单独 snapshot/seed/Credential 失效确认；DP2-19 的每个删除目标需要单独精确确认。DP2-01 当前恢复只需撤销本证据和事项状态，不接触 ANI、部署、数据库或 credential。
