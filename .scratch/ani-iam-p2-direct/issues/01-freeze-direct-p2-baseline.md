# 01: 冻结 Direct P2 来源与替换基线

**What to build:** 固定 Direct P2 使用的 ANI 来源、当前调用面、旧 RLS 缺陷、接受的契约漂移和最终删除清单，形成后续事项唯一可复现的来源基线。

**Blocked by:** 00 / 恢复干净 Kratos 实现基线

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-0 / source baseline

**Baseline:** 00 恢复并验证的干净 Kratos 实现树；已接受的 Direct P2 规格与 ticket plan；ANI Git object `0cedae825a489d936cf41815dc27f278f6d3213c`；历史事项01–04及其证据。

**Scope:** 从精确 Git object 盘点 OpenAPI、Auth 14 RPC、Gateway/Envoy/Inference/Console/BOSS 调用、migration/RLS/Redis/Dex；记录保留行为、目标替换、允许漂移、replacement gates 和 zero-reference 删除清单。

**Out of scope:** 修改 ANI/IAM 业务代码、契约、migration、依赖、部署或真实环境；修复旧 RLS；生成任何 Go 判定。

**Allowed paths:** 本事项、`.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/**`、必要的 `docs/plans/**` 引用修正。

**Read-only inputs:** 当前仓库；`../ANI/repo/**` 仅通过固定 Git object 读取。

**Forbidden paths:** `api/**`、`cmd/**`、`configs/**`、`internal/**`、`migrations/**`、`tests/**`、`deploy/**`、`go.mod`、`go.sum`、`../ANI/repo/**` 的工作树写入；不得使用动态 `main`、当前分支或脏工作树代替来源对象。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/`

- [x] 来源 object、tree、关键文件 hash 和读取命令可复现。
- [x] 当前五类调用方、旧 14 RPC、公开 operation 与数据依赖有完整清单。
- [x] 事项04的 RLS `FAIL / BLOCKED` 被记录为来源缺陷，不被改成 pass。
- [x] 每个旧行为明确为保留、目标替换、允许 breaking 漂移或最终删除，并列出 replacement gate。
- [x] 删除清单区分 runtime 引用与可保留的历史 ADR/evidence。

**Verification:** `git cat-file`/`git show`、descriptor/OpenAPI/调用方静态盘点、hash 清单和 zero-reference 初始扫描；所有结论绑定精确 object。

**Stop conditions:** 来源 object 不可验证；调用面无法穷举；接受漂移需要新的产品决定；必须读取动态工作树才能得出结论。

**Recovery:** 仅回退本事项和证据材料；不改变任何运行代码、数据或外部系统。

**Human checkpoint:** 人工接受基线证据后才可领取 02 或 03；接受不自动领取下一事项。

## Claim record (2026-09-03)

- **人工覆盖后的起点：** 用户明确确认当前手动选择的 checkout 可作为权威起点；实际工作树为 `/home/chabking/workspace/ani-iam`、分支 `main`、提交 `c27ae04c3837e0886773ba016c9cded4169bf1ed`、tree `dc89c0d0e4bdd049370257dffb0a65ed12218452`。这取代 Goal 最初写定的 `codex/direct-p2-01-05@dde4f3e7a38ebd1cb80808838523156c9a80fd49` 起点，不把两者误记为相同状态。
- **初始工作树：** `git status --short`、`git diff --check` 与 staged diff 均为空；`go.mod`、`go.sum`、`api`、`cmd`、`configs`、`internal`、`tests` 相对固定 Kratos 实现基线 `05ba302661d593b608df070dd51cc063fc9f8023` 零差异。
- **ANI 只读来源：** commit `0cedae825a489d936cf41815dc27f278f6d3213c` 存在，tree 为 `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8`；本事项只通过 `git show`、`git ls-tree`、`git cat-file` 或 `git archive` 读取该对象。
- **依赖身份：** `go 1.25.7`；Kratos `v3.0.0`；Kratos OTel contrib `v3.0.0-20260515082355-1ddb58e407c5`；gRPC `v1.82.1`；Protobuf `v1.36.11`；执行器为 `go1.26.7-X:nodwarf5 linux/amd64`。`go.mod` SHA-256 `6cac45c0f25d023b150beb8c97d3a64b2481e53385e19a31045852bebbc1ef8e`，`go.sum` SHA-256 `1b41b52ad891bdb1d3dd04b2b7790eb810a46dbc2d0f22aebfde6e66ef2decc5`。
- **允许路径：** 仅本事项、`.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/**`，以及确有必要且先证明的 `docs/plans/**` 引用修正；不修改 ANI 工作树或 IAM runtime、契约、migration、依赖与部署路径。
- **执行计划：** 固定对象与关键 hash → OpenAPI/14 RPC/五类调用方盘点 → PostgreSQL/RLS、Redis、Dex/OIDC 与地址/部署盘点 → 旧行为分类、replacement gate 与 zero-reference 删除清单 → 完整性和链接门禁 → 人工基线检查点。
- **测试计划：** 复验 commit/tree/hash；以 descriptor/Proto 静态盘点证明 14 RPC 完整；以 OpenAPI operation 与代码/配置引用交叉核对调用面；运行删除清单字段完整性、Markdown/link、`git diff --check` 和 staged-path 审计。静态证据不升级为真实运行证据。
- **恢复方法：** 未经人工接受不提交；恢复只删除本事项新建的证据文件并把本事项状态恢复为 `ready-for-agent`，不触及 ANI、runtime、数据或外部系统。

## Evidence result (accepted)

- **索引：** `.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/README.md`。
- **静态基线门禁：** `pass`。固定 object/tree、68 个关键 source hash、38 个 migration/31 个 Atlas覆盖项、236 个主 OpenAPI operation、59 个 IAM-related operation、旧 14 RPC、五类调用方、60 个替换行为和 28 个删除目标均已由 `verify_evidence.py` 复验。
- **旧存储事实：** 历史事项04保持 `FAIL / BLOCKED`；当前固定 object 的 live PostgreSQL/Redis/Dex 为 `not_verified`，没有把静态来源盘点升级为真实依赖结论。
- **范围与 Git（人工检查点快照）：** `pass`。当时只有本事项和其 evidence path 有变更，staged path 为空，未 commit，未创建 ANI 修改 worktree。
- **人工结论：** 用户于 2026-09-03 明确接受 DP2-01 基线；本事项据此标记为 `resolved`。
- **下一步：** 按 Goal 协议提交本事项，创建隔离 ANI worktree，并在确认唯一 claimed 纪律后领取 DP2-02。
