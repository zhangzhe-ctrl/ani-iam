# 00: 固化 CUTOVER-FITNESS-01 环境与证据方案

**What to build:** 基于只读源码与隔离测试集群探测，固定 `CUTOVER-FITNESS-01` 的双轨拓扑、不可变基线、半天时间盒、证据接口、安全边界与后续实施事项。

**Blocked by:** 无

**Status:** resolved

**Type:** enhancement

**Plan mapping:** Direct P2 verification companion / environment preflight

**Baseline:** ANI `main` 的远端精确对象、ani-iam 最后一个已提交 DP2-09 runtime、只读集群/registry preflight；不得使用动态 `main`、`latest` 或当前脏工作树作为构建输入。

**Scope:** 只读探测、本规格、本事项、后续半天搭建事项和 preflight 证据。

**Out of scope:** 构建/推送镜像、创建 namespace 或 Secret、部署、运行 live gate、修改 Direct P2 ticket graph、恢复 DP2-10 实现、切流、删除或清理。

**Allowed paths:** `.scratch/cutover-fitness-01/**`。

**Forbidden paths:** 除上述新目录外的整个 ani-iam 工作树；全部 ANI checkout/worktree；任何集群、registry、Git remote 或 Credential 状态。

**Evidence path:** `.scratch/cutover-fitness-01/evidence/00-environment-preflight/`

- [x] 固定 current/target 的 Git commit、tree 与 target overlay identity。
- [x] 固定集群、namespace、registry、存储和网络隔离边界。
- [x] 固定唯一 Module Interface、结果分级和最小双轨 smoke。
- [x] 固定半天退出条件和 DP2-10 原子 handoff，不重排 Direct P2。
- [x] 明确 `pass/fail/not_verified` 与所有需再次人工确认的 Credential/破坏性动作。

**Verification:** 文档内 SHA/路径与 Git object 一致；只改 Allowed paths；`git diff --check` 通过；不暂存既有 DP2-10 工作树改动。

**Stop conditions:** 动态基线无法解析为不可变对象；环境不允许隔离；方案要求修改 ANI `main`、混用共享依赖、读取/记录明文 Secret，或扩大到 DP2-10 业务实现。

**Recovery:** 本事项仅新增文档；若撤销，使用后续 revert commit，只撤销本事项精确提交，不 reset/stash/clean 当前 DP2-10 工作树。

## Comments

- 2026-09-08：用户授权先做环境探测并在探测后固化方案；本轮仅允许方案与证据文档，不执行环境搭建。
- 2026-09-08：`pass / resolved`。已核对 ANI/ani-iam commit/tree、target overlay digest、cluster identity/capacity/RBAC、registry 网络、StorageClass/CNI；独立 Standards/Spec 安全评审的 latest-main projection、NetworkPolicy DNS 语义、四小时时间盒和 DP2-10 handoff blocker 已修正。实际 overlay apply、image push/pull、PVC bind、NetworkPolicy enforcement 与 runtime 均保持 `not_verified`。事项 01 因缺少 task-scoped pull-only registry robot 输入而为 `needs-info`；本轮未写入 cluster/registry、未读取 Secret、未改 ANI，也未触碰 DP2-10 recovery state。
