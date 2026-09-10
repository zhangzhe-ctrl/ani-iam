# 17: 冻结隔离接口替换契约与输入

**Status:** resolved

**Type:** task

**Blocked by:** 16

**阶段与能力：** M1 前置文档；C01–C14 的能力、调用方和证据契约。

**授权边界：** 2026-09-10 用户的 WR-17/18 Goal 明确授权依序执行；本票先完成只读取证、契约/输入冻结与隔离准备。产品修改和重型验证在本票 resolved 后由 WR-18 承载。

## 本次领取与精确写入范围

- 产品基线：`cd38cd90bca3e9d83af09a381051b895d82ae94f`，tree `a3668037d43bff9a3d938b9cf07500402e4ebd70`；已核对一致。WR-16 resolved，当前无其他 claimed。
- 唯一事项状态位置：`/home/chabking/workspace/ani-iam/.scratch/ani-iam-workload-refoundation/issues/`。独立产品工作树计划为 `/home/chabking/workspace/ani-iam-wr17-18`（detached 固定基线）；其中事项副本只作来源快照，不另行领取或维护状态。
- 精确允许现有文档：本票、`../issues/18-refound-human-workload-runtime.md`、`../spec.md`、`../ticket-plan.md`、`../capability-matrix.md`、`../decisions.md`；必要时同步 `docs/adr/0022-unify-software-actors-as-workload-principals.md`、`docs/plans/plan-workload-principal-refoundation.md`、`docs/agents/scaffolding-and-codegen.md`。所有路径相对 IAM 根或本票，未列产品路径只读。
- 允许新建 evidence/17 下 `baseline.json`、`before/`、`current-documents.json`、`rpc-inventory.json`、`rpc-matrix.md`、`contracts.md`、`implementation-scope.json`、`isolation-manifest.json`、`remote-preflight.txt`、`freeze-review.md`、`verify_freeze.py`、`source-input.json`、`source-input.tar.gz`；快照和校验属于本票准备范围。允许创建上述隔离 worktree 并带入当前有效文档。
- WR-17 不创建远端资源、不启动重测试。SSH 只读探测工具/CPU/内存/磁盘/活动容器，Network 仅作为执行机制参考。未来重任务仅在 ubuntu，以 `GOMAXPROCS=2` / `-p=2` 串行执行。
- 基线记录覆盖领取前 531 个已跟踪/未忽略文件的 SHA-256 与模式；本票原文另存 before，历史 evidence 和其他 dirty 文件保持原样。
- 验收：本票原验收五项及 Goal 的四组交付；D01/D02 未获接受前只完成无依赖部分。检查链接、能力完整性、精确文件范围、冻结输入、原始文件保护，所有产品运行结果保持 not_verified。
- 恢复：只从本票 before 恢复本票增量；保留原工作树及隔离工作树，不 reset/stash、不提交/推送、不删除已有资源。基线漂移、未接受语义或范围外修改需停止依赖部分并说明。

## 目标

把“能替换旧 Auth 与身份管理接口”冻结成可检查的最小完整能力矩阵、调用链、契约差异和隔离输入，先解决会改变首条实现链语义的决定。保留 Human/Workload 正交模型、IAM/Core 所有权、每跳认证、本地事务与审计不变量。

## 范围与非目标

- 对照 [能力矩阵](../capability-matrix.md) 将每个必需入口映射到 owner、正式 RPC/REST、成功/拒绝/故障/恢复场景、真实消费者和后续票。包含旧 Auth 已有 Human 能力，不能只冻结 Workload Token。
- 输出可审阅的 Proto/REST/事件差异、权限与 caller registry、最小 receiver 校验方案、Workload 可信引导与首管理员可达路径。文档必须分开已接受语义、待决定语义与待冻结 artifact，见 [决定表](../decisions.md)。
- D08 已由用户接受：OIDC 认证流程归 IAM，Gateway 只处理固定回调入口、Cookie 和转发；本票冻结其具体接口及错误/安全边界，不重新把 owner 标成 pending。D01 receiver 决定只阻塞其相关实现链，不能阻塞 WR-16 文档完成。
- 首条 mandatory 参考链固定为 Gateway→Session；确定直接 caller、Delegated Subject、audience、boundary、权限、失败语义及必要 Grant Scope × Evidence 组合。完整 LineageRegistry/PONR/长期恢复平台不作为本票的普遍前置。
- 冻结隔离 manifest：各仓库 commit/tree、OCI digest、生成器/contract/registry/config/seed/scenario 摘要、运行身份/namespace/网络/独立 DB 与 Redis/NATS 资源名、Secret 引用与清理所有权。只记录非敏感引用，不复制凭据。
- 逐票列出最小精确 Allowed/Forbidden paths、依赖版本、运行 gate 和恢复边界；后续新发现的契约差异走显式差异评审，不强迫所有未来功能一次性设计完毕。
- 非目标：修改产品 Proto/生成物/schema/runtime、运行 DB/集群或部署、前端对接、旧代码/数据/凭据删除、流量切换、通用管理 CRUD、Production Ready。

## 固定输入与工作面

- 实际使用的 IAM、ANI、Session、Notification 不可变输入及 Network 只读执行参考已固定于 isolation-manifest.json；未参与本阶段运行的 Inference/Envoy 不虚构固定输入。
- 本票文档写入范围见领取记录；WR-18 精确118路径、输入/生成输出及禁止动作已落入 implementation-scope.json。
- `not_frozen` 是待完成的取证/固定工作，不一律代表需要人工决定；仅 [决定表](../decisions.md) 中明确需要语义选择的项阻塞其依赖路径，不自动阻塞无关文档工作。

## 验收与验证

1. C01–C14 无孤立能力，每个 mandatory 项均有真实 owner、场景、负责票、判定条件；M1 明确允许旧 Core 代码和 UI 尚存。
2. 首条 Gateway→Session 契约及必要信任决定可实施；委托上下文不能增加直接 caller 权限，不允许身份自报或 legacy fallback。
3. 检查当前正式 runtime/schema/消费者差距，列出必须改动的文件面和不可变输入；生成差异、权限 registry、错误/cookie/CSRF、幂等/audit 约束可逐项审阅。
4. 检查隔离 manifest 无共享可变数据/流量/Secret，固定测试可复现；fake/fixture 只能支持早期层，不被记为真实 owner/进程通过。
5. 文档链接、状态/依赖、能力覆盖和范围检查通过；本票不运行部署，产品运行结果一律 `not_verified`。

## 恢复与停止

按领取时保存的文档快照逐文件恢复本票增量，保留历史和其他工作树改动。发现已接受语义冲突、必须猜测 receiver/信任/authority，或需要产品/外部状态变更时，停下依赖部分并记录精确问题；不扩写一整套恢复平台来消除局部不确定性。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts/`

## 结果

已完成独立 worktree、531文件基线/67份文档before、154文件明确源码快照、69方法逐项盘点，以及能力/RPC矩阵、最小合同、118路径工作面和隔离manifest准备。只读检查为 pass，见 [freeze-review](../evidence/17-freeze-api-replacement-contracts/freeze-review.md)。本轮 D01 与 D02 同步部分已获明确接受，四组冻结材料闭合；Core 异步不纳入本票实施前置。最终静态复核 pass，五项文档验收完成，本票 resolved。所有重型/真实运行验证仍为not_verified。

## Comments

- 2026-09-10：用户WR-17/18 Goal授权已核对；领取前无其他claimed，产品HEAD/tree一致。原工作树为文档/事项/evidence权威位置，独立产品worktree为`/home/chabking/workspace/ani-iam-wr17-18`，其中事项副本仅为快照。
- 2026-09-10：复核发现除11个白名单阻断外，Admin忽略既有credential字段并信任x-ani actor/scope；GetTenantAccess无业务授权。已加入WR-18必要修复范围；缺失Platform授权的两个TenantAccess方法不计本阶段已交付，保留WR-22/M1验收。
- 2026-09-10：ubuntu只读预检成功；Network活动kind/工具/任务目录未改变。所有重任务待WR-18，在独立IAM工具/run目录串行，不允许本机自动fallback。
- 2026-09-10，Goal续轮复核2：D01/D02仍pending；补齐真实Gateway exec/console源操作到Session目标模式的不可扩权映射、证书轮换下的幂等意图稳定性、Platform信任配置直接SQL越权负向要求与隔离fixture身份。增强校验器后static pass，原522个受保护文件保持一致；没有live远端run，不将对话等待记为verified wait。WR-17未resolved，WR-18未领取。
- 2026-09-10，Goal阻塞复核3：Goal正文、决定表、基线/快照/源代码保护复核一致，D01/D02依旧未接受。同一等待用户信任选择的前置连续三轮存在，独立准备已完成且无live run；按Goal规则标为blocked，保留本票claimed及全部材料。WR-18保持needs-triage，产品和远程运行未开始；待明确选择后恢复原Goal。

- 2026-09-10，用户接受后恢复：D01 online 与 D02 最小同步委托已接受并记录来源；Core async 单独 pending。公共 Adapter 和最小可运行接入示例列为 WR-19 首链验收，不在本 Goal 提前实施。精确范围及冻结材料复核后继续 WR-18。

- 最终验收：verify_freeze.py 与 git diff --check 均退出0；522受保护文件、67初始快照、69方法/27可用目标及118文件范围匹配。产品与真实运行保持 not_verified。
