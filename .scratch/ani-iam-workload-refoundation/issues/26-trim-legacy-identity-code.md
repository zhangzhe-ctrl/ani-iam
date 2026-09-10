# 26: 在隔离集成分支裁剪 Core 与 Auth 旧身份代码

**Status:** needs-triage

**Type:** task

**Blocked by:** 25

**阶段与能力：** M2 前置；R01，回归 C01–C14。

**授权边界：** 计划票，尚未领取；仅代码裁剪，不包含数据库、Credential、部署资产或流量删除。

## 目标

在 M1 接口替换就绪后，将隔离集成分支上的身份/权限唯一 writer 收敛到 IAM，直接裁剪 Core/Auth 的旧身份实现。此项不依赖 Console/BOSS 前端完成。

## 范围与非目标

- 以已接受 M1 矩阵和目标 caller 清单识别旧 auth-service、Core user/credential/membership/role writer、旧 gateway/client/compat wiring 与对应废弃测试/生成输入的精确代码清单。
- 裁剪已被 IAM 接管的实现，更新保留调用方只依赖固定 IAM 契约；代码可通过保留基线恢复，不建立长期双写/fallback。
- Core Tenant 生命周期、业务元信息/Quota、业务资源 owner 与既有业务数据关系继续保留。涉及 schema/业务关系变更时单独列明迁移设计，本票不执行库表/数据删除。
- 代码范围内做零旧调用/零旧身份 writer 检查，重新验证 M1 后端链；不将全部历史文档或无关符号清零作为目标。
- 非目标：前端接入、部署/生产流量切换、运行数据库表/数据删除、Credential 失效、旧镜像/Secret/备份资产退役、完整 DR 平台。

## 固定输入与工作面

- M1 接受的 commit/tree/证据集、ANI/Core/Gateway/Auth 与必要 caller 的隔离集成分支输入：`not_frozen`；不得直接覆盖用户源工作树或用移动分支代替。
- 精确代码删除清单、保留列表、contract/generator 输入、Allowed/Forbidden paths：`not_frozen`，领取前形成可审阅目标。其他仓库不得因“旧身份”关键词自动纳入。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与 WR-25 接受结果；代码裁剪与 WR-29 资产/数据动作严格分离。

## 验收与验证

1. 固定集成分支不存在目标 runtime 的旧 Auth 调用和旧身份 writer；IAM 是身份/访问关系唯一 writer，Core 业务 owner 保持完整。
2. 受影响构建、生成一致性和必要测试通过；M1 固定接口回归在真实 owner/caller 上通过，无兼容回落或双写。
3. 给出精确移除/保留代码清单及验证结果，证明没有执行数据库、凭据、共享流量或部署资产动作；UI 尚未接入不影响本票。

## 恢复与停止

按冻结代码基线恢复本票隔离分支增量，不 reset/stash 用户工作树。发现删除触及 Core 业务 owner、未被 M1 覆盖的必需接口、真实运行数据关系或必须执行难恢复动作时，停止该部分，形成精确后续目标与适用确认，不用代码裁剪授权删除数据。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/26-trim-legacy-identity-code/`

## 结果

尚未执行；固定输入/精确删除清单 `not_frozen`，代码裁剪与回归 `not_verified`。
