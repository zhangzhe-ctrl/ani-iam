# IAM 接口替换能力矩阵

本表定义 [M1/M2](spec.md) 验收分母；[事项图](ticket-plan.md)承载执行顺序。此处的 `not_verified` 指重排后新 Human/Workload 目标组合，既有局部实现和历史 pass 不被抹去，也不自动转成新基线通过。

## M1 必需能力

| ID | 能力 / owner | 主要验证内容 | 交付事项 | 新目标证据 |
| --- | --- | --- | --- | --- |
| C01 | 正式 runtime / IAM | 真正 cmd/server、middleware、TLS/config/readiness；空库迁移、sqlc clean、受限 role；所有声明可用 RPC 正式可达 | WR-18、WR-24 | not_verified |
| C02 | Human 密码认证 / IAM | 全局 Human、PasswordLogin、错误密码/未验证身份、改密/重置与撤销；认证与选定业务边界分开 | WR-20 | not_verified |
| C03 | OIDC 与 Identity / IAM、Dex | 真实 code+PKCE、state/nonce/issuer/audience、过期/重放/跨 flow；显式 Identity link，不按邮箱自动合并 | WR-20 | not_verified |
| C04 | Session 与浏览器协议 / IAM、Gateway | Refresh rotation/reuse、并发刷新、Logout、SwitchTenant、Tenant/Platform 分离；HTTP cookie jar 验 Set-Cookie/HttpOnly/Secure/SameSite/Path、CSRF/Origin、固定303回跳不含code/Token、401/503/reuse错误协议，不要求 UI | WR-20、WR-24 | not_verified |
| C05 | 权限与资源条件 / IAM、资源 owner | ValidatePrincipal/CheckPermission、Membership/Role 当前状态、两 Tenant 越权、真实 resourceTenant/Owner；默认拒绝和依赖不可用；Gateway 入口 one-call 与后续每跳认证区分 | WR-22、WR-23、WR-24 | not_verified |
| C06 | 身份管理 / IAM | TenantAccess、成员状态、Role/Binding、Invitation、Tenant/Platform admin；邀请验证后才建关系；最后管理员保护、已接受 RestoreTenantAdmin 与 Recovery Bootstrap 分别审批/重认证/审计，不把已有 Tenant 的管理员恢复伪装成重新 Bootstrap | WR-22、WR-23 | not_verified |
| C07 | Tenant Workload/API Key / IAM、Envoy | 不复制 Human 权限；固定 owner Tenant/单一当前 Membership；Key 一次揭示、撤销/过期、权限变更生效、跨 Tenant 拒绝 | WR-21、WR-24 | not_verified |
| C08 | Platform Workload/S2S / IAM、真实 caller/receiver | 可信初始身份、独立 Binding/Grant、无虚拟 Membership；WAT audience/operation/TTL/peer、撤销轮换；direct caller 与 subject 分开、委托不能扩权；首条 Gateway→Session | WR-19、WR-24 | not_verified |
| C09 | Tenant 开通与生命周期 / Core、IAM、NATS | Core Tenant/Quota/outbox 本地原子；IAM durable Bootstrap、已验证管理员及未知邮箱邀请；生命周期版本、独立 heartbeat/watermark、gap/Snapshot、重复乱序/重启/DLQ；资源端权威 guard；强制启用投影前满足 ADR-0004 的 24h shadow/p99≤5s/无未解决gap/一致性/full rebuild | WR-23 | not_verified |
| C10 | 通知业务链 / IAM、Notification | PasswordAction/Invitation 与 outbox/Audit 同事务；真实 dispatcher、Notification 持久接受、隔离 SMTP sink；响应丢失重放去重；投递与动作消费分别确认 | WR-20、WR-22、WR-23 | not_verified |
| C11 | 安全审计 / IAM | 变更与 Audit 原子回滚、append-only/redaction/查询隔离；List/Get 与至少180天已接受语义；不把普通成功资源访问变为 IAM 领域事件 | WR-18、WR-22、WR-24 | not_verified |
| C12 | 重试、并发与恢复 / 各业务 owner | 每个接口冻结 retry 语义；响应丢失、重复、版本冲突、进程重启、依赖中断；相称的定向 race/并发；无 Secret 重放泄露、无双写或旧 fallback | WR-17–24 | not_verified |
| C13 | 替换调用面 / Gateway、Envoy、Inference、Session | 真实 REST/gRPC/必要 HTTP caller，固定契约与 registry；401/403/503/504 等已冻结错误/超时；M1无需 Console/BOSS 产品 UI | WR-24 | not_verified |
| C14 | 安装、隔离与复现 / 各 owner | 确定固定版本和隔离 manifest、可重复 clean schema/受控引导、无共享测试状态；保留脱敏 evidence 和恢复入口 | WR-17、WR-18、WR-25 | not_verified |

C09 的投影正确性与强制启用分开记录：保留 ADR-0004 的 10s heartbeat、30s stale 和 24h shadow exit；若尚在已允许的 shadow 模式，投影强制拒绝必须记 not_verified，C09 和 M1 不能因此整体记 pass。WR-17 明确目标测试模式及其必需证据，M1 不用 fixture 或时间归一化冒充真实传播/观察证据。

WR-17 的[逐能力/RPC 冻结结果](evidence/17-freeze-api-replacement-contracts/rpc-matrix.md)细化目标 endpoint/operation 与 source consumer：旧能力 → 目标接口 → 保留/有意改变/退役 → 成功/拒绝/故障/恢复用例。该冻结前产品基线的 69 个声明中有 29 个 handler，其中 11 个被正式入口阻断，管理主体曾信任请求头。WR-18 已修复其有限范围内的入口和管理主体校验，27 个交付 RPC 经正式进程验证可达，42 个后续声明维持明确拒绝。Get/UpdateTenantAccess 所需 Platform Human 权限继续归 WR-22，当前明确拒绝且不算已交付。不得以旧 Auth 的 bug 或旧 Proto 的全量字节兼容作为目标正确性标准；也不得只跑一个成功登录就通过整个矩阵。

WR-18 [子集验证结果](evidence/18-refound-human-workload-runtime/README.md)：C01/C14 的单服务空库、受限 role、正式配置/mTLS/readiness/关闭与复现基础，C11/C12 的事务 Audit/持久幂等/故障恢复，以及受影响的 Human、OIDC、Session、Tenant 管理与 API Key 回归已有 pass。最后一轮真实依赖集成为 43 个顶层测试、含子用例 67 项通过，三个跨项目测试跳过。上述局部证据不涵盖各能力格的全部 owner、引导、浏览器协议或跨服务业务链，因此保留上表完整 M1 格为 not_verified，不缩减验收分母。

尚未实现的公开能力不得返回空成功。必需格中 skip、stub-only、未跑真实依赖、未固定最终版本均不算 pass。未实现高级能力可明确延期，但从本表删除能力须记录用户决定和影响，不能由某张实现票缩小自身 Allowed paths 后把能力消失。

## M1 后的交付

| ID | 内容 | 完成依据 | 事项 |
| --- | --- | --- | --- |
| R01 | Core/Auth 旧身份代码裁剪 | 逐路由/store/FK/消费者清单；用户/密码/成员/角色旧 writer 退出；Core Tenant/Quota 保留；隔离集成回归 | WR-26 |
| R02 | Console/BOSS 前端接入 | 产品流程、错误/就绪状态、刷新并发/多 Tab 与实际交互；后端能力由 M1 提供 | WR-27 |
| R03 | 实际替换 M2 | 最终目标版本、实际消费者接入、旧 writer 无运行依赖、整组切换与回归，精确恢复/确认 | WR-28 |
| R04 | 后续旧运行资产/数据清理 | 与源代码裁剪区分；精确目标、恢复方案与删除前验证、独立人工确认 | WR-29 |

M1 的“无旧依赖”限定目标运行路径，不要求此时 R01 全仓引用清零。R01/R02 均在 M1 后，可分别推进；真实流量切换与物理资产清理由后续事项承担。

## 明确不扩入 M1

- 前端产品开发、现有环境切流、旧表/凭据/部署删除。
- Network NET-02–04 的全部网络业务与真实 provider 数据面、全新 Compute 拆分；NET-AUTH 复用确定接口并按其独立事项验收。
- 已明确排除的 Purge、通用通知/集成总线、delegated role-admin、成员数 Quota/TCC、Human CLI/Device Flow、审计导出等高级能力。
- 生产 HA、全量负载/Fuzz、跨平台管理谱系与完整长期 DR。安全初始化、当前状态机定向并发、已接受业务恢复要求不会因此被豁免。

接口扩展的通过标准：一个新业务资源应主要改 owner 契约/权限声明及对应注册，不能要求各调用方另造 Token/权限模型，不能让 IAM 接管资源执行或复制 Membership。证据等级见 spec；WR-16 文档审查本身不产生功能 pass，WR-18 的实际子集证据见上文，后续完整接口验收仍按对应事项进行。
