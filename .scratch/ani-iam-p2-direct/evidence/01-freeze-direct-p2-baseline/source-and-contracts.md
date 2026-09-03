# 固定来源与契约盘点

## 结果边界

本证据只盘点固定 ANI Git object，不把 ANI 当前 checkout、动态 `main` 或任意远端状态当作输入。固定对象可读取，静态清单可复现；它不是可运行兼容 Oracle。真实旧存储结论见 [historical-rls-negative.md](historical-rls-negative.md)。

| 项目 | 固定值 | 结果 |
| --- | --- | --- |
| ANI repository | `/home/chabking/workspace/ANI` | `pass` |
| commit | `0cedae825a489d936cf41815dc27f278f6d3213c` | `pass` |
| tree | `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8` | `pass` |
| 主公网 OpenAPI | `repo/api/openapi/v1.yaml` | `pass` |
| 旧 Auth Proto | `repo/api/proto/auth/v1/auth_service.proto` | `pass` |
| 固定对象的真实运行兼容性 | 旧 RLS 正向路径已知不可用 | `fail` |

## 完整读取命令

以下命令是本事项唯一认可的 ANI 来源读取方式：

```bash
git -C /home/chabking/workspace/ANI cat-file -e \
  0cedae825a489d936cf41815dc27f278f6d3213c^{commit}
git -C /home/chabking/workspace/ANI rev-parse \
  0cedae825a489d936cf41815dc27f278f6d3213c^{tree}
git -C /home/chabking/workspace/ANI ls-tree -r --full-tree \
  0cedae825a489d936cf41815dc27f278f6d3213c -- repo
git -C /home/chabking/workspace/ANI show \
  0cedae825a489d936cf41815dc27f278f6d3213c:repo/api/openapi/v1.yaml
git -C /home/chabking/workspace/ANI show \
  0cedae825a489d936cf41815dc27f278f6d3213c:repo/api/proto/auth/v1/auth_service.proto
git -C /home/chabking/workspace/ANI archive --format=tar \
  0cedae825a489d936cf41815dc27f278f6d3213c -- repo
python3 .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/generate_fixed_inventory.py
```

`git archive` 只产生固定对象的只读分析副本；它不读取或修改 `/home/chabking/workspace/ANI/repo`。生成器也只执行固定 commit 的 `cat-file`、`rev-parse` 和 `show`。

## 关键文件清单

[source-manifest.tsv](source-manifest.tsv) 固定 68 个关键输入，每行包含 Git blob、内容 SHA-256 和 byte 数；[migration-files.tsv](migration-files.tsv) 另行覆盖全部 38 个 SQL migration。关键摘要如下：

| 输入 | Git blob | SHA-256 |
| --- | --- | --- |
| `repo/api/openapi/v1.yaml` | `2219b5159794cae858b59c339cf6cce3c394f134` | `b6a2dc1f9c596555fcc164240d7a5533a04a128b8e19a4d7af1c53e41ab9415b` |
| `repo/api/openapi/services/v1.yaml` | `4dd27b7108a1eaa51f419ca63b30f6c2891fc50a` | `43d91489cb851c3e1ac1ffef2ec167391ae210e2da7d794bcb22ffe2d057aad6` |
| `repo/api/proto/auth/v1/auth_service.proto` | `eba8d5842efe75a70c3d11ec3ddcd929e4457c39` | `aabcc72b10bd2b89591eaf706b4cf2659b98a8b5b4e3dbf92b3e387938bc33ec` |
| `repo/pkg/generated/pb/auth/v1/auth_service_grpc.pb.go` | `d5d048bd076d2642838fdf2c231ce579eaa13a93` | `d6912aeab75d01f837a94e4c936454af1b32dc4bc64853d5fe3c851e97348acc` |
| `repo/services/ani-gateway/internal/authz/zz_generated_core_policies.go` | `3c974f4277df6ca37ff4679d89474d5856f055b0` | `194e0b86f65e1962a2b1d3afe9d8e40954a5939f873c3925b8d4f57beb9900a6` |
| `repo/deploy/migrations/atlas.sum` | 见完整清单 | `175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af` |

四份机械清单自身的 SHA-256：

| 清单 | SHA-256 |
| --- | --- |
| `source-manifest.tsv` | `3c0df2fd53d91a7d91930d72aa1bf847d4158f3724885b7cbd5d9ccf53477849` |
| `migration-files.tsv` | `e863ffc96f86679dae90bbee7db1bf24e4f1b75b8eb9f25e4ca02e2fa78da7ea` |
| `openapi-operations.tsv` | `e5f1a2025df723705d0aa37eb09c88379a0a589e724490915076b438195a6bfc` |
| `related-iam-operations.tsv` | `5f8cd36dd12ed79db7903b10ce2747e14eef60fe268d83347b797342a870e217` |
| `auth-service-rpcs.tsv` | `281fcdc00dab5f5193148a5df219ab747cc2bb650627d37f73d25a92427353ef` |

## 旧 `auth.v1.AuthService`

Proto 固定位置为 `repo/api/proto/auth/v1/auth_service.proto:13-42`。生成的 descriptor 对应 `repo/pkg/generated/pb/auth/v1/auth_service.pb.go` 和 `auth_service_grpc.pb.go`。服务端只注册旧 `auth.v1.AuthService`，位置为 `repo/services/auth-service/internal/service/auth_service.go`。

[auth-service-rpcs.tsv](auth-service-rpcs.tsv) 严格按 Proto 声明顺序固定以下 14 个 full method：

1. `/auth.v1.AuthService/Login`
2. `/auth.v1.AuthService/PlatformPasswordLogin`
3. `/auth.v1.AuthService/BeginOIDCLogin`
4. `/auth.v1.AuthService/CompleteOIDCLogin`
5. `/auth.v1.AuthService/RefreshToken`
6. `/auth.v1.AuthService/RevokeToken`
7. `/auth.v1.AuthService/ValidateToken`
8. `/auth.v1.AuthService/ValidatePrincipal`
9. `/auth.v1.AuthService/IssueServiceToken`
10. `/auth.v1.AuthService/CheckPermission`
11. `/auth.v1.AuthService/CheckPermissionV2`
12. `/auth.v1.AuthService/CreateAPIKey`
13. `/auth.v1.AuthService/ListAPIKeys`
14. `/auth.v1.AuthService/RevokeAPIKey`

目标不是扩展或注册这个 service。DP2-03 的 descriptor 必须只包含 `AuthenticationService`、`AuthorizationService`、`IAMAdminService`，且不得包含 `auth.v1.AuthService`。

## 公网 OpenAPI 与 operation

[openapi-operations.tsv](openapi-operations.tsv) 对主公网 OpenAPI 的所有 HTTP operation 做了一次无遗漏解析：

| 指标 | 数量 | 解释 |
| --- | ---: | --- |
| operation 总数 | 236 | method + path 唯一，operationId 唯一 |
| Public | 8 | 有效 security 为 `[]` |
| 已有 `x-ani-authz` | 5 | 旧生成标注，不能直接等同目标 registry 完成 |
| legacy 默认分类 | 223 | 缺少 `x-ani-authz`，旧 Gateway 走推导或 legacy 行为 |

仅两个 operation 缺少显式 `operationId`：`POST /auth/refresh` 派生为 `refreshToken`，`GET /branding` 派生为 `getBranding`。旧生成器还保留 `GET /tasks/{task_id} -> getTask` 的兼容派生表项，但固定 OpenAPI 已显式声明 `getTask`；DP2-02 应删除这类过时例外并要求每个 operation 显式且唯一。

Public 8 项为五个认证入口 `passwordLogin`、`platformPasswordLogin`、`beginOIDCLogin`、`completeOIDCLogin`、`refreshToken`，以及 `getBranding`、`healthCheck`、`readinessCheck`。其中 Refresh 在目标浏览器契约下仍需 Cookie、Origin/Referer 与 CSRF，并不能因旧 OpenAPI 的 `security: []` 被理解为无安全要求。

当前主 OpenAPI 的 Auth operation 共 9 个：上述五个入口，加 `logout`、`createAPIKey`、`listAPIKeys`、`revokeAPIKey`。后三类受保护入口仍属于 223 个 legacy operation，缺目标 owner/authn/authz/obligation 定义。

固定对象中的五个旧 generated operation 是 `listQuotaMeta`、`streamInstanceLogs`、`getPlatformMeteringUsage`、`listAsyncTasks`、`getTask`。其标注可作为输入事实，不是 DP2-02 对 236 项完整分类的替代。

旧脚本在固定 archive 上运行时：

```bash
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate_gateway_authz_drift.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate_core_gateway_authz_routes.py
```

第一条输出 `no drift detected`；第二条报告 297 个 Gateway 注册路由、236 个 registry 路由、0 个 error。它们只证明旧生成物与旧断言相互一致。旧断言允许 unannotated operation 进入 `legacy`、不覆盖 `/api/v1/svc`，也不证明唯一后端 owner、obligation handler 或“受保护 operation 最多一次 IAM decision”，因此目标门禁状态为 `not_verified`。DP2-02 的 replacement gate 必须重新生成全部分类并 fail closed。

## 相关 IAM operation 面

[related-iam-operations.tsv](related-iam-operations.tsv) 固定 59 个与 IAM 替换直接相关的 operation：主 `core-v1` 28 个，`services-v1` 31 个。它暴露两个需要在 DP2-02 精确 breaking checkpoint 处理的事实：

- 主公网 `repo/api/openapi/v1.yaml` 已包含 Auth、Platform User、Tenant User/Role 管理；
- `repo/api/openapi/services/v1.yaml` 另有 `/api/v1/svc` 下的 Platform Admin、Tenant Admin、Member、Role、SSO 重叠入口，且这些 operation 的 effective security 为 `[]`。

目标公网契约只能以主 `repo/api/openapi/v1.yaml` 为唯一来源。旧路径、operationId、schema 和所有权可按 ADR 0001 发生 breaking 漂移，但必须在 DP2-02 展示精确 diff，并让每个保留 operation 只有一个 Gateway Handler 和一个后端 owner。`TransferOwnership` 与 `tenant-owner` 不进入目标契约；固定两份 OpenAPI 中未找到对应 operation，只有既有生成 SDK/前端 schema 残留引用，列入最终 zero-reference 删除范围。

## 后续 replacement gate

| Gate | 必须证明 | 当前结果 |
| --- | --- | --- |
| DP2-02 | 236 个主 OpenAPI operation 均有唯一 operationId/handler/owner/classification，obligation 完整，未知或 revision mismatch fail closed | `not_verified` |
| DP2-03 | 目标三个 gRPC service、稳定 ErrorInfo、Core lifecycle/bootstrap/snapshot contract 与不可变 descriptor | `not_verified` |
| DP2-04 | 新空库、无 RLS、受限非 owner runtime role、显式 TenantScope/复合约束/UoW/Audit | `not_verified` |
| DP2-05 | 真实 PostgreSQL/Redis 与目标 Gateway 的最小纵向链路 | `not_verified` |
