# 18: 重建 Human/Workload 持久化与正式 IAM runtime

**Status:** resolved

**Type:** task

**Blocked by:** 17

**阶段与能力：** M1；C01、C11/C12 的事务基础、C14，保留 C02–C05 已有能力。

**授权边界：** 2026-09-10 用户 WR-17/18 Goal 已授权顺序实施；WR-17 已 resolved，D01 与 D02 同步部分已接受；本票现按冻结清单领取，实施与远程验证均由原 Goal 授权。

## 目标

让正式 `cmd/server` 在隔离空库和受限 DB role 下运行 Human/Workload 模型，为后续纵向接口链提供可用的 contract→service→biz→data→storage→runtime。替换旧 Service 类型的同时保留已有 Human 登录、Session 与权限能力。

## 范围与非目标

- 仅实施 WR-17 冻结的首期必要契约、schema/migration、sqlc、显式 UoW、稳定错误映射与正式 composition/config；按固定生成器生成，业务层不导入 transport/data DTO。
- Human 与 Workload、owner/identity binding/credential/authority/execution context 分离；Tenant-owned 关系显式 tenant_id 与 tenant-preserving 复合约束，不依赖 RLS。
- 本地业务状态、安全审计及必要 durable 幂等结果遵守同一事务边界；为后续路径提供明确 repo/usecase，禁止跨库 FK、跨库事务和共享业务表。
- 受限运行 role 与迁移/owner fixture role 分离；正式进程启动、健康、配置拒绝和 graceful shutdown 使用现有 Kratos 能力。
- 非目标：完整 S2S/Envoy/Core/NATS/Notification E2E、部署到共享环境、重建已有数据库、完整管理 CRUD、候选 Lineage/PONR 平台、前端或旧资产删除。

## 固定输入与工作面

WR-17 已准备 [精确文件清单](../evidence/17-freeze-api-replacement-contracts/implementation-scope.json)、[合同](../evidence/17-freeze-api-replacement-contracts/contracts.md)、[RPC矩阵](../evidence/17-freeze-api-replacement-contracts/rpc-matrix.md)与[隔离manifest](../evidence/17-freeze-api-replacement-contracts/isolation-manifest.json)；D01/D02 适用决定已接受，WR-17 静态验收 pass，四组材料已冻结；运行结果仍须本票实际验证。

产品 worktree 固定 `/home/chabking/workspace/ani-iam-wr17-18`，detached `cd38cd90bca3e9d83af09a381051b895d82ae94f` / tree `a3668037d43bff9a3d938b9cf07500402e4ebd70`。唯一事项状态仍在原 `/home/chabking/workspace/ani-iam`，worktree 内文档只作来源快照。重任务在 SSH `ubuntu` 的专有 run 目录，默认 `GOMAXPROCS=2` / `-p=2` 串行；不可自动本地重跑。

本票交付27个现有 handler 的安全正式入口，包含所有已有 Human Password/OIDC/Session/权限路径和15个 Tenant管理 RPC。Get/UpdateTenantAccess 的 Platform Human 授权不具备，当前必须拒绝，完整能力仍归 WR-22/M1；不得将 Gateway 认证等同 Platform 管理权限。其余未实现声明保持不可用，不移出能力矩阵。

- 精确 Allowed paths 唯一清单：implementation-scope.json，SHA256 `cc480d1db2c8d82626a43a724198fe486facdfc93d483aadf363acfb80d77536`，118路径；existing/planned_create/rename/生成输入输出均已列明。不在清单中的产品文件只读。
- 文档允许同步本票、WR-17结果、spec/ticket-plan/capability-matrix/decisions 和清单内三个 docs 文件；只在原工作树维护事项状态。对应 evidence/18 为本票新增证据目录，保留 evidence/17 冻结材料及 before 不变。
- 冻结合同/矩阵/初始源码/工具及隔离fixture依赖取 evidence/17，运行时精确 source/tool/resource 清单在 evidence/18 另记。owner fixture 不代表 WR-19/22 正式引导。
- 当前库/凭据/流量、其他仓库与 Network 资源、commit/push/tag/PR、WR-19均禁止；不以测试失败自动扩入。
- 实施序列：目标 Proto/registry/schema 与生成 → 领域与持久化/UoW/幂等 → 正式入口业务鉴权与运行接线 → 真实依赖/正式进程验收。生成、编译、test/vet/race全部仅ubuntu执行。
- 验证：固定生成重复无差异；Go1.26.7下受影响包test/build/vet与定向race；真实新PG安装/受限角色/跨Tenant/事务Audit/幂等负向；真实Redis Human回归；正式cmd/server的TLS/RPC/config/readiness/health/shutdown。明确完整M1/跨服务链不在本票完成声明。

## 验收与验证

1. 固定输入从空库安装成功，正式 IAM 进程使用受限 role 完成真实持久化读写；缺少/错误权限必须拒绝，迁移 role 不进入运行配置。
2. 现有 Human Password/Session/权限回归通过；新 Human/Workload schema 不靠伪造兼容 service 类型或 fake composition 保持 green。
3. 跨 Tenant 关联/读取/写入、约束绕过、审计写入失败、业务中途失败与幂等冲突有真实 PostgreSQL 负向证据，失败不留下部分安全状态。
4. 运行正式进程与 config/health/shutdown 检查，相关单测、生成一致性与受影响包检查通过。此处保留的能力通过不代替 WR-20/24 的完整真实 caller 验收。
5. 记录 schema/contract/config/OCI 或二进制摘要、运行角色与清洁安装证据；没有运行的链保持 `not_verified`。

## 恢复与停止

仅恢复本票隔离工作树、进程及专有测试资源到冻结基线；清理前核对精确隔离目标和既定授权，不回写共享库。出现 Human 能力回退、运行 role 越权、事务/tenant 边界缺口、需要未冻结契约或其他仓库修改时，停止相关路径并记录差异；禁止临时 fallback。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/18-refound-human-workload-runtime/`

## 结果

验收 pass，详见 [WR-17/18 完成交接](../evidence/18-refound-human-workload-runtime/README.md)与[包含新增文件的产品 diff](../evidence/18-refound-human-workload-runtime/product.diff)。Human/Workload 持久化、正式 27 RPC 入口、当前 caller/actor 授权及业务/Audit/幂等事务基础已交付。固定生成重复一致，普通单测/契约、build/vet、定向 race、真实 PG/Redis/Dex 和正式 cmd/server 验证通过；最终集成 43 个顶层测试、含子用例 67 项通过、0 失败、3 个跨项目测试跳过。最终源码与验证快照逐文件一致，原仓库产品代码及保护文件未改，专有测试容器与正式进程已退出。完整 M1、正式引导和跨服务链仍 not_verified，WR-19 未领取。

## Comments

- 2026-09-10：WR-17 resolved 后领取，产品工作树与固定基线一致；无其他claimed。重任务使用专有IAM run，开始前重新只读预检VM。

- 2026-09-10：完成验收并 resolved。最终 run `wr17-18-20260910T021423Z-bca28bda` exit0，源码 SHA-256 `6727e79febbdec2d549601a7ccc966d399d0ac46e278bc0adb24ff2fc2b8f42f`；完整检查 run 与最后四个 fixture 变更的证据适用性见交接。未 commit/push/tag/PR，不开始 WR-19。
