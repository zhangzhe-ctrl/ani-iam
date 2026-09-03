# CLAUDE.md

## 仓库定位与当前状态

这是独立的 ANI IAM 重构项目。仓库当前承载由完整 Q1–Q300 grilling 结论综合出的规格、计划、领域词汇和架构决策；运行时仅完成事项02的隔离骨架，兼容业务尚未开始。

ANI 兼容性基线固定为 `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`。该 Git 身份已验证，但 Proto、PostgreSQL/RLS、Redis、Dex、Gateway、Envoy 和 Inference 的兼容性证据仍需在首个基线事项中采集。禁止用动态 `HEAD`、`main`、`latest` 或另一个提交替代。

## 必读顺序

1. `.scratch/ani-iam-rebuild/spec.md`：当前完整规格和用户故事。
2. `docs/plans/plan-iam-service-refactor.md`：目标系统、不变量、契约和验收。
3. `docs/plans/plan-iam-kratos-phased.md`：阶段边界、入口/出口条件和删除顺序。
4. `docs/plans/plan-iam-decision-traceability.md`：Q1–Q300 覆盖与限定。
5. `CONTEXT.md`：领域术语。
6. 与当前改动相关的 `docs/adr/`。
7. 若开始实施，读取 `.scratch/ani-iam-rebuild/issues/` 中当前 `claimed` 事项。

## 权威顺序

发生冲突时，按以下顺序处理：

1. 用户在当前对话中的明确决定；
2. 当前规格；
3. 两份核心计划和决策追踪矩阵；
4. 已接受的 ADR；
5. 当前实现和测试。

实现与文档冲突时，不得静默选择一方：记录差异，判断它属于待实现目标、过期文档还是新决策，并在继续前修正权威材料或事项。

## 事项与授权

只读盘点、诊断、评审和方案分析可以直接进行。任何改变仓库、数据库、集群、凭据或外部系统状态的工作，都必须由一个状态为 `claimed` 的本地事项承载。事项至少包含：

- 目标与非目标；
- 固定提交或不可变 Artifact 摘要；
- 允许和禁止修改的范围；
- 真实依赖与前置证据；
- 验收、测试和证据路径；
- 停止条件与恢复方法。

同一时间只推进一个会改变状态的事项。文档完成、提交 Git Commit 或启动 Agent 会话都不自动授权实施。

删除、切流、Credential 失效、数据重建、生产操作或其他难以恢复的动作，必须在执行前获得针对精确目标和动作的人工确认。Agent 不得代替用户填写批准、豁免或结果接受。

## 架构边界

- IAM 是独立项目；可以复用 PostgreSQL、Redis、Dex、NATS、Kubernetes 等共享基础设施，但不得导入 ANI/Core 内部实现。
- Core 拥有 Tenant 生命周期和平台级资源所有权；IAM 拥有 Principal、Membership、Role、Policy、Session、Credential、Token、API Key 和 IAM 安全审计。
- ANI 的公开 REST 契约以 `repo/api/openapi/v1.yaml` 为准。Console/BOSS 通过 Gateway 调用，不能绕过 Gateway 直连 IAM。
- Gateway、Envoy 和 Inference 只能消费已发布的 IAM 契约；跨项目依赖固定到不可变版本和摘要，不能动态解析 `main` 或 `latest`。
- Core 到 IAM 的集成使用明确的生命周期事件、心跳与 bootstrap 协议；禁止双写共享业务状态。

## 实现纪律

- 官方生成器能初始化的 scaffold、契约或客户端必须先固定版本并生成基线，禁止手工仿制；按 ADR 裁剪时记录生成来源与 `保留/删除/替换` 差异，缺少这些证据不得关闭事项。详见 `docs/agents/scaffolding-and-codegen.md`。
- Kratos core/contrib 已提供的 lifecycle、config、logging、transport、middleware、health 或 observability 能力必须直接采用；运行时事项必须列明采用项、未采用项及理由，自实现只限领域规则或经证实的框架缺口。
- 目标实现遵循 Kratos `api`、`service`、`biz`、`data`、`server`、`conf` 分层；业务规则保持框架无关。
- P2 目标数据库不使用 PostgreSQL RLS，但 Tenant-owned 表必须通过 `tenant_id`、复合唯一键/外键、显式事务上下文和负向测试保证隔离。
- 权限检查保持 `{resource, actions, scope}` 语义；所有状态变更必须定义幂等、审计、失败和重试行为。
- 验收必须区分 `pass`、`fail` 和 `not_verified`。未运行的真实依赖测试不能写成通过。
- 功能完成不等于生产就绪。HA、备份恢复、生产密钥、保留策略、容量、故障演练和生产切流仍是独立的未来范围。

## 完成与交接

完成事项前，必须运行与风险相称的测试和静态检查，记录实际执行的命令、结果、未验证项、恢复状态和后续依赖。若事项被阻塞，保持现场可恢复，并把阻塞证据写回事项，而不是扩大范围绕过门禁。

## Agent skills

### Issue tracker

事项使用 `.scratch/<feature-slug>/issues/` 下的一文件一事项本地 Markdown 工作流。参见 `docs/agents/issue-tracker.md`。

### Triage labels

本仓库使用 `needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix` 五个标准状态角色。参见 `docs/agents/triage-labels.md`。

### Domain docs

本仓库使用 single-context 领域文档布局：根目录 `CONTEXT.md` 与 `docs/adr/`。参见 `docs/agents/domain.md`。

### Scaffolding and codegen

框架初始化与代码生成遵循 generator-first 流程。参见 `docs/agents/scaffolding-and-codegen.md`。
