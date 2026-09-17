# 33: IAM 接入独立 Governance 单一目标合同

**Status:** claimed

**Type:** task

**Blocked by:** 20, 21, 22, 23, 32

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/iam-governance-transition/`

## 授权、目标与非目标

2026-09-16 用户 Goal 授权 IAM/Governance 并行计划 I1～I4 的 IAM 实施和唯一联合运行协调。Goal 原文：`/home/chabking/.codex/attachments/75cf1f85-0cc2-4c04-89bf-f85cd3067254/goal-objective.md`。
WR24 已保存完整 checkpoint 并释放领取；本票为唯一 claimed。完成单一 Bootstrap/Lifecycle/Snapshot 合同消费、部署配置、可靠投影/引导和真实联合验收，为 WR24 提供候选。

Governance 是 Tenant owner；IAM 是身份/Access/Membership/Role/Invitation/Session/权限 owner。只有一种新目标协议，无旧 Core 别名/双 decoder/fallback。沿用 NATS、REST Snapshot、mTLS/WAT、target registry、独立身份和当前 Grant 校验。Snapshot 不创建身份或成员，受邀账号激活与独立接受邀请保持分离。

非目标：恢复 WR24、ANI/Gateway 写入、GOV-04、WR25～29、全仓改写、切流、旧资产删除、生产。历史发布授权不扩大，精确发布集合未定不阻塞未发布候选开发。

## 基线和工作位置

[CHECKPOINT](../evidence/iam-governance-transition/CHECKPOINT.md)及 [checkpoint-summary.json](../evidence/iam-governance-transition/checkpoint/checkpoint-summary.json)冻结来源。IAM 来源 commit `6e9688bb002d1916bc894fd281ba1974f57b7eaa` + 完整 WR23 713 文件，文件清单摘要 `738858c7fa2bf93f8d8357ea4ca88bec8fc62e28e74df46a86273666d8112b89`；WR24 source-iam 全集另存，713 原文件零变化，新增仅临时 workspace/runner。默认当前文档与规则合入新候选；不覆盖默认工作树产品代码或旧候选。

本地候选根为 evidence 下 `candidate/iam/`。远端根 `fedora:/home/chabking/workspace/ani-iam-runs/wr33-governance-20260916T073847Z`，工作副本 `candidate/iam/`；完整原始输入在 `inputs/`，不可修改。

Governance 只读输入目录为 `/home/chabking/workspace/ani-governance/docs/execution/iam-governance-transition/`，由该 owner 交付 DTO/合同/归档/部署输入。尚未冻结的对方制品不计 pass。

## Allowed paths

默认 IAM 权威树仅：本事项、24 事项、ticket-plan/spec/decisions/capability-matrix；`CONTEXT.md`、`docs/adr/0002-separate-tenant-lifecycle-from-iam-access.md`、`docs/adr/0004-coordinate-tenant-lifecycle-and-iam-asynchronously.md`、并行计划，以及本 evidence 目录。历史 evidence 原文不改。

新候选完整文件复制是准备输入，不授权任意改写。候选产品改动的逐文件清单在 `allowed-paths.json` 冻结后开始：限实际 TenantLifecycle/Bootstrap 领域、data/SQL/迁移/service/composition、受校验 endpoint/配置、公开 API/SDK/registry、受影响测试及独立消费工具。每次必要依赖闭包变动先更新清单和理由，不扩大 owner 或语义。

Forbidden：任何 Governance/ANI/Notification/Session 仓库写入；旧 WR23/WR24 候选、历史证据、运行空间、凭据、数据库/PVC；未列 IAM 产品路径；本机生成/格式化/构建/测试/摘要/Git；旧 ubuntu 执行入口。

## 执行边界、预算和资源

本机只做源码/文档阅读编辑及 SSH 传输。全部生成、格式化、构建、测试、摘要、依赖消费在 Fedora。真实集群使用 ani-test-1 唯一协调，context `kubernetes-admin@ani-platform`，kube-system UID `be57b911-892c-4e75-aa9d-4a05d819c59e`；新 namespace `iam-gov-20260916-073847`。实际资源创建前保存 UID/归属、配置摘要、身份/数据库/消息隔离清单，秘密只留受控远端文件/Secret。

双方共用 flock `~/workspace/.locks/iam-governance-heavy.lock`，不删除他人锁。总 CPU 4、RAM 8192 MiB、available 下限 4096 MiB、磁盘 60 GiB；Go GOMAXPROCS=2、-p=2、GOMEMLIMIT=1536MiB。一次一重型阶段，常驻/观察计入总预算，运行前重验。

## 有界检查点和验收

1. I1：先交 IAM-owned Bootstrap wire、durable receipt/worker/结果查询及身份流程；公开 API/SDK/registry 独立不可变归档。消费 Gov 自有 Lifecycle/heartbeat/Snapshot DTO 并相互复核。integration-manifest 只引用制品，不复制 DTO。
2. I2：替换旧 Snapshot path/audience/operation、loopback 与 WR23 inbox 假设；配置校验、TLS 身份、精确 ACL/ACK/重投/DLQ/producer 归因不降低。
3. I3：真实 IAM PostgreSQL 幂等、事务审计、并发、crash/restart、租户谓词/复合 FK/负向隔离；投影版本/gap/新鲜度、全量重建和增量衔接、邀请/查询及 receipt/worker/replay 当前授权。独立干净目录解析依赖，不依赖旧任务相对路径。
4. I4：同一固定组合正式 Governance 创建→真实 NATS→IAM→正式结果；真实 Notification/SMTP→邮箱验证/激活→独立接受邀请→管理员关系；冻结/解冻/禁用、错误身份/目标/权限/租户及恢复、Snapshot reader/cut、DLQ/依赖故障/restart。连续真实 24h、10s heartbeat、30s stale、p99≤5s、无未解释 gap/授权差异及相关真实执行端拒绝/恢复。

逐项记录 pass/fail/not_verified、命令/退出码/精确候选与证据等级。替身只能局部验证。I1/I2/I3 局部通过不代替 I4；双方 runtime_ready/candidate_ready 等字段不代替事项 Status。全部必需门禁满足才 resolved/关闭 Goal。

## 恢复与停止

保留旧源码与联合现场；重连本 run 先查 manifest、退出码、UID/锁，不重复 bootstrap 或重建数据库。失败原始记录保留，新修复用新阶段目录。任何一方不得自行拆除联合环境。
新身份/权限语义、合同或基线冲突、超 owner/Allowed paths、预算/隔离不足或需降低安全时，仅暂停受影响分支、保存证据并继续有界独立工作。删除/切流/Credential 失效/既有数据重建/生产另需精确人工确认；本票不执行。

## 当前结果

WR24 checkpoint 和唯一领取转换已完成。I1/I2/I3/I4 均 not_verified；已收 Governance 首版合同提案，正在复核。24h 尚未启动；未发布，未创建联合运行资源。
