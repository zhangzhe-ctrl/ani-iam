# 24: 验证 Gateway REST 与全部必需后台调用方

**Status:** ready-for-agent

**2026-09-16 执行修订：** 按新的 IAM/Governance Goal，在[完整检查点](../evidence/iam-governance-transition/CHECKPOINT.md)保存源码/模式/摘要、A/B/C/D 分母、发布现状及运行资源后释放本票领取。新增前置 WR33；本票受其阻塞，不在 frontier，产品执行继续暂停，未 resolved。下方旧 claimed/等待旧 Core 的描述均为历史，不是当前恢复命令。

**2026-09-16 规划补充：** 已生成 [IAM/Governance 并行实施及 WR24 恢复计划](../../../docs/plans/iam-governance-parallel-transition.md)。本次只有计划和导航修订，产品执行继续暂停、领取状态保持；共同合同后 IAM 有限适配与 Governance 实现并行，真实联合验收后由同一 Gateway 写入方先补齐 A，再推进 B 中 GOV-04 必需的身份交互及治理路由，分别验收。B/C/D 和完整 M1 分母保留；新 IAM 前置事项执行前先保存本票 checkpoint、释放领取并登记依赖，不同时 claim 两票。

**当前用户指令：** 先提交并推送代码到远端 `main`，然后等待 `ano-governance` 拆分完成再继续对接。此指令替代此前等待 Core 前置接线提前到 A 的恢复条件，并授权相关代码提交推送；发布仓库/候选范围正在确认，尚未提交或推送。当前 Gateway 依赖未发布候选的本地模块替换，发布前必须固定可获取的依赖并完成相应校验，不得直接发布测试路径。WR24 保持 `claimed`，原 Goal 为 `blocked`，均不表示完成；不恢复旧 Core 接线、不切流、不删除现场。见[当前交接](../evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/HANDOFF.md)。

**Type:** task

**Blocked by:** 20, 21, 22, 23, 33

**阶段与能力：** M1；C13，汇合 C01–C12 并完成 C12/C14 的接口级证据。

**授权边界：** 用户明确选择整个ANI仓库最新main中的ani-gateway，先完成登录适配；Envoy依赖入口暂缓。当前按下方本次领取记录推进，不把登录子集视为完整WR24。

## 2026-09-13 收口约束

- 领取前冻结逐接口清单：入口、operation、direct caller、receiver/owner、既有证据、必要接线差异、成功/拒绝/故障出口及是否最终启用。禁止只以“全部调用方”领取宽泛工作。
- 首批已由用户选定最新 ANI 的 Gateway Human 登录与 Session；Envoy 依赖入口暂缓。每条入口的必要 receiver/owner 保留；首批通过不等于原全量 M1。
- 后续启用范围须明确接入或退出；不得静默删除 mandatory 项或保留旧 Auth fallback。Network/Compute 新产品、全面 owner 重构不由本票授权。
- Workload 通用接入已由 WR32 单列，本票只消费固定结果；发现新安全机制需要时先记录差异，不重新在 IAM/SDK 加服务名分支。

## 目标

在无前端的隔离环境，通过真实 Gateway REST、Envoy、Inference、Session Gateway 和相关 owner 正式进程验证必需行为，形成 M1 能力矩阵的逐项真实接口证据。

## 范围与非目标

- 用 HTTP runner/cookie jar 验证 Gateway 的认证、cookie/CSRF、错误映射、permission gate、Tenant/Platform scope 和后台管理接口；Gateway 保持 DTO/协议转换，不复制 IAM 业务规则。
- C05 使用真实资源 owner 的 resourceTenant/Owner 条件证明资源权限；Gateway 入口 one-call 与后续每跳身份验证分别取证，不从请求中的 Tenant/owner 字段直接授权。
- 补齐 WR-19 首链之外仍 mandatory 的 Inference、Envoy、Session Gateway 调用；按实际调用 registry 检查每个同步 hop 的直接 caller 身份、authority、audience 与必要 delegation，不用服务名数量代替操作覆盖。
- 所有必需接口由正式 composition 和真实 owner 提供；OIDC、Notification+SMTP sink、Core/NATS/投影链沿用对应真实证据并运行组合路径，fake/fixture 只用于早期或明确非门禁资源。
- 按冻结 RetrySemantics，对安全状态变化执行响应丢失重试、幂等冲突、相关并发和依赖超时/不可用检查；只测风险相关组合，不引入无边界全量混沌平台。
- 汇总目标调用/写入观察点，证明隔离目标路径不访问旧 Auth 或旧身份 writer；没有 UI 和全仓旧引用不影响本票。
- 非目标：Console/BOSS 前端、全仓 zero-reference、旧 Core 代码裁剪、整组生产/共享流量切换、全面容量/HA/DR。

## 固定输入与工作面

- 实际 mandatory caller/owner 的 commit/tree/OCI/contract/registry/config/seed/scenario 与隔离 manifest：`not_frozen`；前置结果只有输入一致或差异重新验证后才能复用。
- 候选工作面：Gateway/Envoy/Inference/Session 等必要 adapter 和接口 harness、隔离 config、缺陷最小修复；精确 Allowed/Forbidden 文件清单 `not_frozen`。契约/owner 大缺口回到明确设计差异，不暗中扩大本票。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与 WR-18–23 证据。逐项列出谁是真实 owner、谁是 fixture、为何允许。

## 验收与验证

1. 每个 mandatory REST/RPC/消息行为有成功、拒绝、依赖失败与适用恢复场景；cookie/CSRF/error mapping 由 Gateway REST 实证，无 frontend helper 才能调用的隐藏依赖。
2. 每个必需 S2S hop 的 caller、credential、Grant、scope/evidence、audience 验证通过；错误身份/权限/租户、撤销状态和依赖失联均 fail closed。
3. 真实 caller 执行业务结果可观测，不能以 Pod Ready、HTTP 200、直接 IAM 单测或手造 ctx 作为完整成功；目标无旧 Auth fallback/旧身份表写入。
4. Refresh、Key/Grant 变更、Invitation/admin guard、lifecycle/bootstrap 等定向重试/并发无重复授权或遗漏审计；受影响源码检查/测试与 contract pin 一致性通过。
5. 每个能力和链分别记录 `pass`/`fail`/`not_verified`、复现命令、输入摘要与证据级别，不将局部通过概括为 M1 完成。

## 恢复与停止

恢复各专用隔离 caller/config 和测试资源，保留可重放但脱敏证据。若必需链只能 fake、fallback、越权上下文或跨库写入才工作，停止并记录能力失败；若新增大功能，先重排剩余范围，不能在汇总票重建服务。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/24-verify-backend-callers/`

## 结果

方案阶段历史结果：已固定最新 ANI 提交与 WR23 候选引用并形成后端对接方案。当前执行结果以下方最新记录为准；完整后台接口矩阵仍 `not_verified`。

2026-09-15T10:43:57.453995+00:00：用户确认以整个ANI最新main适配Gateway登录；WR24唯一claimed，先固定输入和逐接口/逐文件范围。当前准备清单见 `evidence/24-verify-backend-callers/wr24-gateway-login-20260915T104357Z/claim.json`。Envoy部分暂缓但不从M1分母删除。

2026-09-15：用户授权清理指定旧 Direct P2 缓存并要求基于 ANI 最新 main 形成后端对接方案。缓存清理 pass，源码工作区保留。见 [后端对接方案与 WR24 分批安排](../evidence/24-verify-backend-callers/wr24-gateway-login-20260915T104357Z/backend-integration-plan.md)。WR24 保持唯一 claimed，Envoy 延期项仍计入完整 M1 分母。

2026-09-15 本轮 Goal 授权：在同一 WR24 内按 A（登录/Session）→B（管理/权限/审计）→C（必要 caller/owner）顺序完成非 Envoy 集成，每批先冻结逐文件与逐接口范围。除源码阅读和编辑、SSH/文件传输外，全部执行位置明确改为 `fedora`；历史 ani-test-1/ubuntu 配置不作为执行入口。当前 [claim](../evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/claim.json) 记录环境、固定输入、预算和停止条件。预检已连通，远端查询 main 未前进；候选内容核验与产品实施尚在进行。D 继续 not_verified，WR24 不 resolved。

2026-09-15 用户后续补充：明确提供 `ani-test-1` 的真实 Kubernetes 集群运行 ANI 相关服务和对接工作，`fedora` 作为中间构建机。本轮执行位置以 [修订 01](../evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/execution-amendment-01.json) 为准：fedora 生成/构建/定向检查，ani-test-1 真实集群正式服务/接口验证；新建 WR24 私有 namespace，不复用或改写历史场景。

2026-09-15 本轮实施检查点：A 的六项接口、逐文件范围及必要生成物已冻结并实施；Fedora 的 registry 一致性、受影响 adapter/Gateway 定向测试与构建 pass，SDK/API 文档双目录生成一致性 pass。3902 个 WR23 候选文件按路径、内容、模式复验 pass。真实 Kubernetes 中 Atlas、受限 IAM roles、registry 安装和 Gateway Workload provision pass，但 Human/Session HTTP 验收尚未执行，A 不 complete，B/C 未开始，聚合门禁 not_verified。

当前恢复和剩余决定见 [活动检查点](../evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/CHECKPOINT.md)：用户已授权 CNI bug 影响测试时手动恢复。已限定恢复任务 `auth-stack-r2`，八个 PVC UID/volume 与原 Pod 均保留，共享 kcn/kubelet/containerd 未改。Gateway 空数据库及 POD_UID 接线已应用；真实进程暴露的 HTTPS 启动探针缺陷经 red/green 回归修复，runtime-build-05 已运行。

2026-09-15 后续真实接口进展：Dex 实际签名身份确认、正式首管理员 CLI 注册、BOSS 19 项定向 HTTPS 和 10 项 refresh 并发/reuse/丢弃响应重试全部 pass。OIDC 额外负向独立执行并保留预检失败。Console 所需真实 Core/NATS/Snapshot 前置链提前到 A 仍待用户顺序决定；不伪造投影、跨库 seed 或改变身份语义。余下 OIDC/故障/审计/legacy 观察、聚合门禁、完整 A/B/C 仍未验收。WR24 保持唯一 claimed，D 完整分母不变。

2026-09-15 独立故障/观察推进：`browser-oidc-negative-02` 的 7 项 nonce/PKCE/真实过期与恢复场景 pass；`dependency-01` 的 9 项 IAM 丢包 504、拒绝连接 503、实际包计数和原 Session/Grant 恢复场景 pass。BOSS/refresh 在旧 Auth 实际端口观察下重跑，计数零，仅作为该路径的运行证据。临时规则由 controller finally 和独立 ExecStopPost 双路径恢复，Pod/进程不变；产品源码仍匹配 46 文件检查点。PostgreSQL writer、审计原子性及其他剩余 A 仍 not_verified，未更改数据库日志设置或权限。精确新范围与脱敏证据见活动 HANDOFF，事项继续唯一 claimed。

2026-09-15 SQL/审计门禁推进：精确范围 `A-sql-atomicity-scope.json` 下，Fedora 保留解析器负例失败和修正后构建通过；真实集群 `sql-atomicity-01` 新增 9 项登录/refresh/logout 审计失败、事务回滚和恢复审计场景 pass。五表 count/hash 无变化与同事务实际修改语句到达审计失败点共同构成回滚证据。BOSS/refresh 重跑期间 Gateway 写入/SQL 错误消息为零，独立失败写校准为 1；只覆盖这些路径。审计 ACL、日志设置、auto-conf entries 经 controller finally 与独立 ExecStopPost 恢复，Pod/进程不变。112 份脱敏结果已导出，原始 SQL 私留节点。剩余 A、B/C、聚合和 D 状态不变，WR24 仍唯一 claimed。

2026-09-16 OIDC 声明门禁推进：`oidc-claims-01` 使用真实 Dex 签发的 issuer/audience 不匹配令牌，在原始 Dex 正常换码后仅注入 id_token 响应故障；两组各 5 项正式 Gateway 拒绝、签名与上游交换校准、精确替换、已有 Session 保留和恢复场景全部 pass。不是 fake IdP、手签令牌或 Console 接口验收。任务 nat 规则双路径恢复、辅助进程已停止，Pod/容器/PVC/Service/原 Dex 配置不变；46 份脱敏证据与固定 Dex 源码依据已记录。下一独立缺口为 refresh 429/Retry-After；Console 前置链顺序仍待确认，A/B/C 与聚合未完成，D 分母不变。

2026-09-16 refresh 限流门禁推进：`refresh-rate-01` 的 8 项真实 Gateway HTTP 检查 pass，确认五次 401 后第六次 429/AUTH_RATE_LIMITED、Retry-After 随时间递减，以及自然等待 902.111 秒后的 401 恢复。只读精确 Valkey key 的计数 7、TTL 对应和到期后计数 1 支持实际接口证据；没有修改 key/TTL/配置/时钟，独立 Session/Grant 前后不受影响。runner 与 coordinator 已终止成功，Pod/容器/配置不变。Fedora 定向构建与 15 个脱敏运行文件已归档。只读核对另外确认当前 ANI 缺少必要 Core lifecycle/Snapshot 接线，不能仅启动既有进程满足 Console；限定提前前置链仍待用户决定，B/C、聚合和 D 状态不变，WR24 仍唯一 claimed。

2026-09-16 Dex/Valkey 依赖门禁推进：`auth-dependency-01` 新增 19 项正式 Gateway HTTP、实际包归因和恢复检查 pass。真实 IAM→Dex REJECT/DROP 分别为 503/504，已消费 operation 不复活，新登录恢复；真实 IAM→Valkey REJECT 分别覆盖 begin/callback/refresh 的 503 与同请求恢复。已有独立 Session/Grant 保留。双路径清理恢复任务 Pod 原 filter/nat，所有辅助进程已终止，Pod/容器/Service/配置不变；39 个脱敏运行文件及 34 个 Fedora 构建文件已归档。剩余独立 BOSS 缺口明确为 begin 同键重复/并发及成功响应丢失重试、成功 logout 响应丢失后的重试；Console 前置链顺序仍待决定，完整 A、B/C、聚合未完成，D 分母不变。

2026-09-16 begin/logout 重试门禁推进：`browser-retry-01` 的 16 项真实浏览器检查与四组精确一次成功审计 pass。begin 同键重复、成功响应丢弃后重试与两个首次并发调用复用一个 operation；只允许一次实际登录。logout 成功响应丢弃后同键重试及两个并发请求均保持 204，不复活 access/refresh，不影响独立 Session；对应登录/退出各一条成功审计由只读事务观察确认。runner 已终止，Pod/容器/Service/配置不变；18 个脱敏运行文件与 32 个 Fedora 构建文件已归档。已定位的独立 BOSS 缺口完成，下一必要实施停在 Console 真实 Core/NATS/Snapshot 前置链的顺序决定；完整 A/B/C、最终覆盖和聚合未完成，WR24 仍唯一 claimed，D 不缩减。
