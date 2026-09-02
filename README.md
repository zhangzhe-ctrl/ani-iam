# ANI IAM 重构规划仓库

本仓库承载 ANI IAM 重构的当前规格、设计决策与实施计划。当前产物由完整的 Q1–Q300 grilling 结论综合而成；运行时实现尚未开始，文档完成也不等于授权修改代码、数据或外部环境。

## 当前状态

- Q1–Q300 决策：已综合进入当前规格和追踪矩阵
- ANI 兼容性基线：`main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`
- Git 身份：已验证；Proto、PostgreSQL/RLS、Redis、Dex 和三个调用方的兼容性证据仍待采集
- 实施状态：未开始
- 生产就绪：否；当前目标是测试/演示环境的功能交付

## 阅读顺序

1. [当前规格](.scratch/ani-iam-rebuild/spec.md)
2. [IAM Service 重构方案](docs/plans/plan-iam-service-refactor.md)
3. [Kratos 分阶段替换方案](docs/plans/plan-iam-kratos-phased.md)
4. [Q1–Q300 决策追踪矩阵](docs/plans/plan-iam-decision-traceability.md)
5. [领域词汇](CONTEXT.md)
6. [架构决策记录](docs/adr/)

## 实施方式

实现事项从当前规格生成到 `.scratch/ani-iam-rebuild/issues/`。只读分析可以直接进行；任何会改变仓库或外部状态的工作，必须先有一个状态为 `claimed` 的本地事项，写明目标、范围、固定基线、依赖、验收、测试、恢复和停止条件。同一时间只推进一个会改变状态的事项。

删除、切流、Credential 失效、数据重建、生产操作或其他难以恢复的动作，必须在执行前获得针对精确目标和动作的人工确认。

## 项目边界

IAM 是独立项目，可以复用 PostgreSQL、Redis、Dex、NATS 和 Kubernetes 等共享基础设施，但不得导入 ANI/Core 的内部实现。ANI 的公开 REST 契约仍由 `repo/api/openapi/v1.yaml` 定义；跨项目契约通过不可变版本和摘要固定，不动态跟随 `main` 或 `latest`。
