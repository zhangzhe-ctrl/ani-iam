# 27: 在 M1 后接入 Console 与 BOSS 前端

**Status:** needs-triage

**Type:** task

**Blocked by:** 25

**阶段与能力：** M2 前置；R02，消费 C02–C07/C10/C13。

**授权边界：** 计划票，尚未领取；与 WR-26 概念上独立，但仍一次只有一个 claimed 事项。

## 目标

使 Console/BOSS 通过 Gateway 消费已验证的 IAM 接口契约，完成用户可见的认证、租户/成员/权限及后台管理流程。前端接入从 M1 之后开始。

## 范围与非目标

- 更新固定版本 client/SDK 和前端状态管理，按 Gateway REST 消费 Password/OIDC、Session/Refresh/Logout/SwitchTenant、密码动作以及各界面已有的管理功能。
- 遵守 HttpOnly refresh cookie/CSRF 与错误契约；Console Tenant 和 BOSS Platform 边界明确，前端不直连 IAM、不以本地角色标记承担最终授权。
- UI 范围以现有产品流程及冻结替换矩阵为限；后端缺陷回到明确 owner 修复，不在页面复制 IAM 权限/会话规则。
- 非目标：重新设计整套前端、创建无需求的管理 CRUD 页面、删除旧数据库/凭据/资产、生产切流、将 UI 测试倒置为 M1 前置。

## 固定输入与工作面

- Console、BOSS、ANI/Gateway、IAM commit/tree 与已接受 M1 契约/client/config/测试场景：`not_frozen`；精确 Allowed/Forbidden 前端及必要生成客户端路径 `not_frozen`。
- 开始前读取各仓库入口规则并核对必要工具/依赖可用；缺少前端仓库要求的分析条件时保持相关项 `not_verified`，不能以未经检查的接口推断替代。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与 WR-25；WR-26 是否完成不改变本票概念依赖，二者最终在 WR-28 合流。

## 验收与验证

1. 真实浏览器经真实 Gateway/IAM/OIDC/Notification 完成对应登录、刷新、退出、切租户、密码动作与管理流程；错误/过期/权限拒绝状态可理解且不泄露敏感内容。
2. refresh token 不进入可读持久化存储，cookie/CSRF/redirect、重复操作和会话失效行为与 M1 契约一致；不能靠绕过 Gateway 或旧 Auth 保持页面工作。
3. Console Tenant/BOSS Platform 与 delegation 规则由服务端拒绝用例证明，前端独立构建/受影响测试通过，记录所消费的固定契约和真实链证据。

## 恢复与停止

恢复本票隔离前端分支/client/config，不覆盖源工作树或切换共享站点。发现必须修改未冻结后端语义、引入明文可读 refresh token、绕过 Gateway、或新页面需求超出替换范围时停止相关流程并记录差异。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/27-integrate-console-boss/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，前端接入 `not_verified`。
