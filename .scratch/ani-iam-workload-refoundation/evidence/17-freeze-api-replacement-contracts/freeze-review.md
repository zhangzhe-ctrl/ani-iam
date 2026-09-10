# WR-17 冻结准备审查

记录时点：2026-09-10（Asia/Shanghai）。下列历史复核保留；本轮用户已接受 D01 与 D02 同步部分，必要人工选择闭合。WR-17 完成静态验收后才关闭并领取 WR-18。

## 四组材料

1. [RPC inventory](rpc-inventory.json) + [能力/RPC矩阵](rpc-matrix.md)：逐方法复核69个声明、29个 handler、11个正式白名单阻断；保留C01–C14全部能力。WR-18 交付27个安全正式入口，两个 Platform TenantAccess 接口的缺失授权归 WR-22，明确不可用。
2. [最小合同](contracts.md)：Human/Workload正交关系、Tenant不可变/约束、当前 caller/subject 授权、一次揭示 Secret 与持久幂等、Gateway→Session 有限方案及引导职责。D01 与 D02 同步部分已接受；Core 异步单列为后续未决，不阻塞 WR-18。
3. [精确工作面](implementation-scope.json)：118个明确路径（现有/计划新建分别标记），包括必要 rename、官方生成输出、测试与远程辅助脚本；不是118项全仓重写要求，也不授权未列出的产品改动。新平台管理、跨仓集成、切流/已有数据/凭据动作未进入。
4. [隔离manifest](isolation-manifest.json) + [只读远端预检](remote-preflight.txt)：实际 ubuntu 主机 i-8yg2l7u8、4核、预检可用内存5530MiB、可用磁盘223GiB。Network kind容器保持原样。Go1.26.7/sqlc1.31.1可复用，Buf/Atlas按IAM固定版本和摘要另行准备。PG/Redis/Dex输入固定digest、随机loopback端口、专有资源与独立凭据；WR-17 没有启动容器/进程/测试。

## 证据与限制

| 检查 | 等级/结果 | 依据 |
| --- | --- | --- |
| 产品基线 HEAD/tree | static / pass | baseline.json；原产品未修改，独立 worktree 为相同 detached commit |
| 原有文件保护 | static / pass | 531个初始文件；排除9个获准文档后522个文件哈希/模式完全未变；67份 before 文档快照匹配初始哈希 |
| 本地源码输入快照 | static / pass | source-input.json / tar.gz；154个明确产品/测试输入，包SHA256 `24c3b60bee330c9455ac9ea2a3ac296f3abefe20cce381bc770afe96720a2feb`；未传远端，不是WR-18最终源码或验证结果 |
| 事项纪律/能力覆盖 | static / pass | verify_freeze.py 核对唯一claimed、依赖、69方法、27目标入口与C01–C14覆盖 |
| 其他仓库来源 | static / pass | ANI、Session、Notification固定commit/tree与所需artifact；Network两个helper含已有dirty改动，实际文件SHA独立记录且只读参考，不能冒充其提交版本 |
| IAM生成工具准备 | static / prepared | 版本/期望摘要已固定；Buf1.72/Atlas Community1.3安装和正式生成仍未执行 |
| D01/D02适用决定 | decision / pass | 本轮用户两条“接受”分别对应在线校验与最小同步委托；异步 Core 仍 pending |
| 首管理员/Workload可信初始化 | not_verified | 仅最小方案/职责划分；owner fixture不等于正式引导 |
| WR-18生成/编译/真实PG/Redis/正式进程 | not_verified | 尚未领取实施，未运行任何重任务 |
| WR-19首链/M1/M2/生产 | not_verified | 不在本票完成声明内 |

只读校验命令：`python3 .scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts/verify_freeze.py`；`git diff --check`。代码和真实依赖验证的执行位置固定ubuntu，不作本机fallback。

## 关键代码差异与取舍

- 补白名单之前要修复真实授权：Admin 的 credential 字段已有定义但当前实现忽略它，不能继续从普通 x-ani Header 建Authority。Tenant Admin 必须重新依当前IAM Session/Membership/Role验证，且直接Gateway的Workload身份/Grant独立。
- Get/UpdateTenantAccess 的现有owner registry声明Platform权限；WR-18不改成Tenant权限、不把Gateway当超级管理员。相应完整Platform能力留在WR-22/M1分母，当前拒绝。
- 新库实现Human/Workload基础及现有Tenant自动化；PlatformWorkload信任只读基础由owner fixture验证，真实引导归WR-19；完整首PlatformHuman归WR-22。无service→workload运行兼容分支。
- 所有适用mutation的幂等结果必须与业务和Audit一起提交。Key首次响应丢失可查询/重放非敏感结果，不能重新揭示Secret或偷偷创建第二把Key。

## 历史阻塞审查（保留当时状态）

第二次 Goal 续轮复核：上一轮分类为 progress（已建立权威准备材料/快照）；本轮重新读取 Goal 正文并核验当前源码、唯一 claimed 和待决定状态。新增首链 source operation→target operation/mode 约束、证书轮换不改变幂等意图的规则、Platform 信任配置的直接 SQL 写入拒绝要求，并固定隔离 fixture 身份。verify_freeze.py 现在直接对照当前 Proto 声明、Go handler 定义、正式白名单与 ANI 实际 Session 源路由，静态校验继续 pass。D01/D02 仍无明确回复；当前没有运行中的远端任务可等待，不能把对话等待记为 verified wait。已完成安全独立准备，依赖合同和 WR-18 继续等待选择。

D01/D02 回复后仅更新其明确选择的范围，核对合同影响并完成最终静态复核。满足 WR-17 验收才能 resolved；随后领取WR-18，在独立产品worktree按清单实施、在ubuntu串行验证。当前没有需要恢复的远程运行动作；保留本地快照、原dirty文档和独立worktree即可继续。

第三次 Goal 阻塞复核：再次读取完整 Goal 正文，D01/D02 当前仍为 pending，未收到用户选择。前两轮完成了独立准备工作；本轮分类为阻塞复核，不声称新增产品进展或 verified wait。当前静态校验、原文件保护、源快照及产品 worktree 未改动检查均通过。未解决的唯一继续执行前置是用户对 D01 receiver 和 D02 最小委托信任边界的明确决定；正文禁止以推荐/未回复替代接受，并要求 WR-17 完成后才能领取 WR-18。独立准备已完成，无可等待的已启动任务或可继续的获准产品动作。因此达到连续三轮同一阻塞的条件，将 Goal 标为 blocked，保留 WR-17 claimed 和 WR-18 needs-triage；不把任何票标 resolved。收到用户决定后按原范围恢复，不重新创建 Goal 或丢弃快照。

## 本轮接受与最终冻结审查

用户本轮明确接受 D01 在线 receiver 和 D02 Gateway 自身身份/权限加 IAM 短期委托，已同步 decisions、ADR-0022、设计与首链合同。接受仅覆盖同步链，Core 异步留 WR-23；长连接建立后的终止/重检查合同留 WR-19 启用前。普通服务接入成本现在作为首链可复用 Adapter 的验收要求，当前未实现，不能算通过。

四组材料闭合，implementation-scope.json 状态 frozen_for_wr18，记录精确合同/矩阵/隔离/初始源码摘要；118路径及原产品基线不变。D03 的 WR-18 基础/fixture 与 WR-19/22 真实引导分开，D04 冻结当前27方法范围，D05 固定实际输入；后续产品运行证据全部 not_verified。最终静态检查通过后关闭 WR-17，按已授权 Goal 领取 WR-18；不新增信任选择或扩大至其他仓库。
