# ANI IAM

独立的身份与访问控制服务。IAM拥有Human/Workload、凭据、Session、Membership、Role/Binding和权限判定；独立Governance拥有Tenant业务及生命周期，资源服务保留自己的资源状态、执行和幂等。

本分支交付IAM/Governance接入候选，供独立服务集成。联合验收目前15/16通过，真实24小时观察仍待完成及复核；完整Auth替换就绪M1和实际替换M2尚未完成。源码和模块发布不代表已部署到消费方环境。

- [普通服务接入起点](docs/integration/service-integration-start.md)：固定依赖、调用模型、需要准备的身份与权限、最小验证范围。
- [交付版本与验证边界](docs/integration/wr33-delivery.md)：源码来源、模块版本及当前验收状态。
- [API授权声明规则](docs/api-authorization-contract.md)、[SDK合同](sdk/README.md)、[Workload authority合同](docs/contracts/workload-authority-v1.md)。SDK README中的WR19/WR20段落是历史背景，当前消费版本以交付文档为准。
- [CLAUDE.md](CLAUDE.md)、[CONTEXT.md](CONTEXT.md)、[ADR](docs/adr/)：贡献规则、领域术语及已接受设计。

普通服务依赖公开API/SDK/注册表，不引用IAM的internal包，也不复制IAM数据库或身份实现。实际接入请使用独立身份、独立测试空间和固定合同；不要复用正在进行24小时验收的专属环境。
