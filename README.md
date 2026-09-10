# ANI IAM

独立的身份与访问控制服务，替换 ANI 的 auth-service 和用户/权限管理；Core 保留 Tenant 生命周期与 Quota，资源服务保留自己的业务状态与执行。

## 当前工作入口

1. [当前规格](.scratch/ani-iam-workload-refoundation/spec.md)：M1/M2、范围与架构。
2. [唯一事项图](.scratch/ani-iam-workload-refoundation/ticket-plan.md)：顺序、依赖和进度。
3. [接口能力矩阵](.scratch/ani-iam-workload-refoundation/capability-matrix.md)：替换需要通过哪些行为。
4. [有限未决点](.scratch/ani-iam-workload-refoundation/decisions.md)：技术选择与待冻结输入。

先在隔离环境验证正式接口，达到 **M1 接口替换就绪** 后，才裁剪 Core 的旧身份代码并启动前端对接；实际消费者切换和整体回归形成 **M2 实际替换完成**。旧资产清理及 Production Ready 分别验收。代码/历史证据存在不等于新目标已通过。

## 按需参考

- [CLAUDE.md](CLAUDE.md)：工作与分层规则。
- [CONTEXT.md](CONTEXT.md)、[ADR](docs/adr/)：词汇与已接受决定。
- [基础设计](docs/plans/plan-iam-service-refactor.md)、[Workload 模块设计](docs/plans/plan-workload-principal-refoundation.md)：稳定业务规则与机制设计。
- [历史路线索引](docs/plans/plan-iam-kratos-phased.md)、[Q1–Q300 历史追踪](docs/plans/plan-iam-decision-traceability.md)：只供追溯，不提供另一套执行顺序。

契约由各 owner 维护，跨仓库消费固定版本；IAM 不导入 ANI 内部代码。当前产品实现与新 Human/Workload 目标的差异由能力矩阵和后续事项验证，不用文档完成代替运行验收。
