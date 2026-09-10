# 25: 验收 M1 隔离接口替换就绪

**Status:** needs-triage

**Type:** task

**Blocked by:** 24

**阶段与能力：** M1；C01–C14 完整验收。

**授权边界：** 计划票，尚未领取；M1 通过不自动授权 WR-26–29 或外部状态动作。

## 目标

以可复现的正式进程接口证据确认 IAM 已具备替换 Auth 与身份管理所需的全部能力，让后续旧代码裁剪和前端接入有清楚的准入点。结论限定为“隔离接口替换就绪”。

## 范围与非目标

- 对 [能力矩阵](../capability-matrix.md) C01–C14 逐项核对正式接口、真实 owner、成功/拒绝/故障/恢复与证据摘要，整合 WR-18–24 的当前输入证据。
- 在冻结的独立 namespace/网络、DB/Redis/NATS、Secret 引用、镜像和 seed/scenario 下，从空库安装正式 IAM 及 mandatory caller/owner，重放固定接口矩阵。需明确验证资源的创建、保留、恢复/清理归属。
- 证明目标链不调用旧 Auth、不写 Core 旧身份表；Core Tenant/Quota 等业务 owner 仍可被正常调用。旧 Core 身份代码/UI 尚存和全仓旧引用允许保留到后续票。
- 缺陷只做证据分类与精确归属；需要产品修复时按唯一事项规则明确剩余工作，不以接受票掩盖未完成能力。
- 非目标：前端完成、全仓 zero-reference、Core 旧身份代码删除、切换真实流量、数据库/凭据/资产删除、生产 SLA/容量/HA/长期 DR。

## 固定输入与工作面

- 参与仓库 commit/tree、OCI/contract/config/registry/seed/scenario 摘要、集群/namespace UID/网络/数据资源及运行 ID：`not_frozen`；必须对应最终实际接受输入，不能用移动 HEAD 或不同 SHA 的 CI 代替。
- 候选工作面：接口验收 harness、冻结隔离 manifest、当前 effort 证据与验收文档；精确 Allowed/Forbidden 文件清单 `not_frozen`。
- 读取 [规格](../spec.md)、[决定表](../decisions.md) 与前置票。fake/static/unit、真实依赖、正式进程/真实 caller、部署及 production 证据分级。

## 验收与验证

1. 每项 mandatory 能力全部达到矩阵要求的真实证据级别且 `pass`；任何必需 `fail`/`not_verified` 均不得宣布 M1 完成。
2. 正式 IAM+Gateway REST（无前端）、OIDC、Notification+SMTP sink、Envoy/Inference/Session、真实 Core/NATS/bootstrap 等必需 owner/caller 完成固定矩阵；不以部分 smoke 或测试替身替代。
3. 独立空库安装和固定场景可重复，运行 role 受限，审计/幂等/tenant 隔离与依赖故障门禁通过；目标无旧 Auth 调用/旧身份写入有观察证据。
4. 核对最终 commit/tree/contract/config 与所引用证据一致，差异需定向重新验证；产出明确接受清单、边界、未覆盖 production 项和下一阶段输入。
5. 用户验收结论不能由 agent 代填。技术 gate 通过只形成可供接受的 M1 结果；后续事项仍需自身范围、输入及适用授权。

## 恢复与停止

保留脱敏 evidence bundle 与可复现场景，仅按既定授权处置专用隔离资源。发现共享状态、输入漂移、秘密泄露风险或 mandatory 证据缺口时停止受影响验收并报告 `fail`/`not_verified`，不通过降低能力清单宣告完成。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/25-accept-isolated-api-replacement/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，M1 `not_verified`。
