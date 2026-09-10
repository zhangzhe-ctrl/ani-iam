# 28: 完成整组切换演练与 M2 系统替换验收

**Status:** needs-info

**Type:** task

**Blocked by:** 26, 27

**阶段与能力：** M2；R03，整合 R01/R02 与 C01–C14。

**授权边界：** 计划票，尚未领取；切流、Credential 失效、数据重建等难恢复动作必须按精确目标和动作另行获得人工确认。

## 目标

在旧代码裁剪和前端接入完成后，验证整组系统实际使用 IAM，并完成明确隔离目标上的部署/调用方切换与适用恢复演练，形成 M2“系统替换完成”结论。

## 范围与非目标

- 将 WR-26/27 产物与固定 IAM/owner/caller 组合到同一可审阅 manifest，列出 runtime、调用方、数据库/身份、配置和流量各自目标状态及回退/前向恢复边界。
- 准备并验证对当前动作必要的恢复证据：可恢复基线、精确对象、恢复入口可达性、停止/fence 条件及所需管理员/审计安全不变量。不能用“恢复后再说”绕过已接受安全门禁。
- 按 [决定表](../decisions.md) 仅采用已接受且适用的恢复机制；候选 Lineage/PONR/长期 DR 平台没有因旧草案出现而自动成为本票全量工程，具体缺口先决策再实施。
- 完成整组隔离切换、失效/故障场景与恢复演练，验证前端和全部 mandatory caller 均使用目标 IAM；最终目标保持何种运行状态在精确 manifest 中写明。
- 非目标：未列目标的生产操作、数据库/备份/Secret/镜像等旧资产退役、宣称 Production Ready；资产删除独立 WR-29。

## 固定输入与工作面

- WR-25 M1 接受集、WR-26/27 最终 commit/tree/OCI、deployment/config/identity/scenario 与目标 namespace/资源 UID/恢复 artifact：`not_frozen`。
- 精确 Allowed/Forbidden paths、环境范围、动作序列、执行/恢复责任与每个难恢复动作的人工确认记录：`not_frozen`。计划编写和只读取证可先做；未获适用确认不能执行该动作。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与前置证据；历史 CF-01 smoke/旧 cutover-window package 不可自动充当当前整组切换或删除后恢复证明。

## 验收与验证

1. 最终整组输入一致，Console/BOSS 与 Gateway/Envoy/Inference/Session/Core/Notification 等必需调用方按固定矩阵通过；目标无旧 Auth 调用或旧身份 writer。
2. 实际隔离切换和部署级故障/恢复在精确目标上有运行证据，权限/会话/审计/生命周期保持安全；没有实施的高风险动作保持 `not_verified`，不得写入“已接受/完成”。
3. M2 结果分别列出功能、部署、恢复及未覆盖 production 项；用户接受状态单独记录，不由 agent 替填。M2 通过不自动授权删除资产。

## 恢复与停止

只执行已接受的精确恢复方案；遇到目标漂移、共享资源、恢复材料不可用、管理员不可达或确认不匹配时，停止依赖动作，保留可诊断状态与脱敏证据。不能临时失效更多 Credential、删除资产或改造外部恢复系统来推进。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/28-complete-system-replacement/`

## 结果

尚未执行；固定输入/精确动作与适用确认 `not_frozen`，M2 `not_verified`。
