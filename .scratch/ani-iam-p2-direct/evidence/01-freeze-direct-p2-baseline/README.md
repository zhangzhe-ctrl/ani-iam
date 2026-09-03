# DP2-01 Direct P2 来源与替换基线

状态：人工已于 2026-09-03 接受；DP2-01 为 `resolved`，尚未领取 DP2-02/03。

固定 ANI 来源为 commit `0cedae825a489d936cf41815dc27f278f6d3213c`、tree `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8`。它 is not a runnable compatibility oracle：历史真实依赖已经证明旧 Auth RLS 对受限 runtime role 的同 Tenant 正向写入返回 `SQLSTATE 42501`。

## 人工覆盖后的 IAM 起点

用户明确把手动选择的当前 checkout 作为权威起点。实际起点是 `main@c27ae04c3837e0886773ba016c9cded4169bf1ed`，tree `dc89c0d0e4bdd049370257dffb0a65ed12218452`，取代 Goal 原写定的 `codex/direct-p2-01-05@dde4f3e7a38ebd1cb80808838523156c9a80fd49`。这项覆盖已原样记录在 DP2-01 ticket，不把两个起点误记为相同。

## 证据索引

| 证据 | 内容 | 结果 |
| --- | --- | --- |
| [固定来源与契约](source-and-contracts.md) | commit/tree/hash、236 OpenAPI operation、59 IAM-related operation、旧 14 RPC | `pass` |
| [当前调用方与运行边界](callers-and-runtime.md) | Gateway、Envoy、Inference、Console、BOSS | `pass`：静态盘点；真实 E2E 为 `not_verified` |
| [存储与基础设施](storage-and-infrastructure.md) | 38 migration、role/RLS、Redis、Dex/OIDC、地址与部署 | `pass`：静态盘点；旧 checksum 与 RLS 为 `fail` |
| [历史 RLS 负向证据](historical-rls-negative.md) | 事项04真实 PostgreSQL/Redis 结果 | `FAIL / BLOCKED` |
| [替换矩阵](replacement-matrix.md) | 60 个旧行为的四类决定、owner、gate、删除条件、DP2影响 | `pass` |
| [删除清单](deletion-manifest.md) | 28 个 runtime/contract/storage/build/deploy 删除目标与历史例外 | `pass` |
| [验证记录](verification.md) | 实际命令、结果、Git状态与恢复 | `pass`；人工检查点已接受 |

机械生成物：

- [source-manifest.tsv](source-manifest.tsv)：68 个关键文件的 Git blob/SHA-256/bytes；
- [migration-files.tsv](migration-files.tsv)：38 个 SQL migration 及 31/7 Atlas覆盖状态；
- [openapi-operations.tsv](openapi-operations.tsv)：236 个主 OpenAPI operation；
- [related-iam-operations.tsv](related-iam-operations.tsv)：28 个 core-v1 + 31 个 services-v1 IAM-related operation；
- [auth-service-rpcs.tsv](auth-service-rpcs.tsv)：旧 `auth.v1.AuthService` 14 RPC；
- [generate_fixed_inventory.py](generate_fixed_inventory.py)：只从固定 Git object 生成上述清单；
- [verify_evidence.py](verify_evidence.py)：复验 object/hash、完整性、链接、范围与 staged path。

## 基线摘要

- 主公网 OpenAPI 有 236 个 operation：8 Public、5 个旧 `x-ani-authz` generated、223 legacy；只有 `refreshToken` 和 `getBranding` 的 operationId 是派生值。
- 两份固定 OpenAPI 暴露 59 个 IAM 替换相关 operation；`/api/v1/svc` 的 31 个重叠入口和主契约 28 个入口必须在 DP2-02 形成精确 breaking/owner 结论。
- Gateway 当前 public 为 0 次旧 Auth 调用，但 protected route 是两次调用；还存在 dev bypass、legacy字符串推导、`X-API-Key` 和不完整 Header trust boundary。目标是 Public 0、Authenticated-only 1、Authorized 1。
- Envoy 直接调用旧 ValidateToken；Inference 使用共享 mint secret、静态 token fallback 和 dev header；Console/BOSS 把 access/refresh token 放入 Web Storage；BOSS 缺 role claim 时存在前端默认 allow。
- 固定旧数据库与 Core/Services 共库，Auth RLS defective，runtime role模型与目标不符；38 个 SQL migration 只有 31 个进入 `atlas.sum`。
- Redis 三类 key 是 OIDC state 10 分钟、JTI blocklist 按剩余 Access TTL、API Key rate 1 分钟；Refresh 在 PostgreSQL 中且不轮换。
- Dex/OIDC provider 语义保留，但旧 in-memory/static-client 配置和 Auth实现被目标 PKCE/verified-email/Identity-Link 替换。
- 60 个行为已全部归入且只归入一个类别：8 保留语义、24 目标实现替换、3 允许 breaking 漂移、25 最终删除。

## 当前结果与限制

| 判断 | 结果 |
| --- | --- |
| 固定 commit/tree 可读 | `pass` |
| 关键 hash/清单可重新生成 | `pass` |
| 14 RPC completeness | `pass` |
| caller/operation completeness | `pass` |
| replacement/deletion completeness | `pass` |
| 历史事项04旧 RLS | `fail`，保持 `FAIL / BLOCKED` |
| 当前固定 object 的 live PostgreSQL/Redis/Dex | `not_verified` |
| DP2-02/03 contracts | `not_verified` |
| DP2-04 target database | `not_verified` |
| DP2-05 vertical slice | `not_verified` |

人工接受已完成；下一步只按固定提交协议收口 DP2-01，随后验证并创建专用 ANI worktree，再领取 DP2-02。接受不授权 push、发布、部署、切流、数据重建、Credential 失效或旧资产删除。
