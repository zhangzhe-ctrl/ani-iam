# CP0 人工评审材料

状态：`READY FOR HUMAN REVIEW`

## 本事项给出的结论

冻结 Git 身份、14 RPC、旧 Auth 安全/存储静态语义、三调用方调用面和被否决旧门禁的替代断言已经可定位并可复跑。指定 SHA 上的 targeted unit gates 与旧两项 gate 当前通过。

真实 PostgreSQL/RLS、Redis、Dex 和三调用方 live E2E 没有被运行，统一为 `not_verified`。这不阻塞“固定 Oracle 材料”的人工评审，但它们仍是 CP0 的真实依赖硬门，不能支持 CP0 Go。

## CP0 允许的唯一语义范围

- 唯一自变量：Kratos transport/runtime。
- 保持冻结 `auth.v1.AuthService` 14 RPC wire、RS256/JWT claims、bcrypt、非旋转 Refresh、JTI blocklist、API Key input/hash/status、PG schema/runtime role/RLS、Redis key/TTL、Dex/OIDC、Gateway/Envoy/Inference 的请求/错误/副作用。
- 隔离项目与隔离配置；读取固定 snapshot；任何写路径只写隔离 PostgreSQL/Redis。
- 差分只归一化随机 ID、时间、Token 和 Secret；比较 gRPC code/detail、公开错误、Claims、PG/Redis durable state、TTL 与外部副作用。

## CP0 禁止

- P2 Proto/schema、Argon2id、无 RLS、Session Grant、旋转 Refresh、新 API Key 模型。
- Core Control/NATS/Lifecycle projection、Gateway one-call 目标重写、切流、部署或旧资产删除。
- `sa-token-go` 或同时改变 wire/schema/security 来掩盖框架差异。
- 用 fake、当前 HEAD、动态 main、另一个 caller 或静态证据替代真实依赖/live caller。

## 真实依赖清单

| 依赖 | CP0 最低隔离与证据 | 当前 |
| --- | --- | --- |
| PostgreSQL | 空隔离 DB、冻结 migrations、migration owner 与 `ani_app_user` 分离、真实 RLS two-Tenant/平台行为 | `not_verified` |
| Redis | 独立 namespace/DB，验证三类 key、TTL、消费/回填、依赖错误 | `not_verified` |
| Dex | 独立 client/identity，真实 auth-code/state/nonce/JWKS/callback | `not_verified` |
| Gateway | fixed registry/config；valid、success、401/403/429/503/timeout 与副作用 | unit `pass`；live `not_verified` |
| Envoy | ValidateToken、target Tenant、header stripping、401/404/429/503/timeout | unit `pass`；live `not_verified` |
| Inference | IssueServiceToken、audience/scope/TTL/cache、Core service-only chain | unit `pass`；live `not_verified` |

## 停止条件

- `<SHA>`、artifact digest 或 14 RPC inventory 不再一致。
- 真实依赖不能隔离，或只能以 fake、superuser、全局 Redis flush、共享 Dex identity 运行。
- 为通过 CP0 必须修改旧 wire/schema/security，或发现未列入 allowlist 的差异。
- 三 caller 中任何一个被另一个 caller 的结果替代。
- 需要超出事项卡 allow paths、修改 ANI、启动未批准外部状态、部署、切流或删除。
- Replacement gate 尚未人工接受，却试图领取事项02。

## 人工检查点

请人工明确接受或拒绝以下内容：

1. Git/Artifact 绑定是否足以作为唯一冻结输入；
2. 14 RPC 与 Credential/PG/Redis/Dex 静态 Oracle 是否完整；
3. 历史失败、当前已绿和 replacement assertion 的区分是否可接受；
4. `not_verified` 真实依赖是否应进入后续 CP0 事项，而不是在事项01启动环境；
5. 是否允许将事项01改为 `resolved`，从而让事项02进入 frontier。

未得到明确接受前，不领取事项02、不启动 Kratos、不生成 `decision: GO`。
