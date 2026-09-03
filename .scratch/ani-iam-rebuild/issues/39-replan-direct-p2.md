# 39: 重排 Direct P2 计划与事项

**What to build:** 在保留 CP0 负向证据和全部历史事项的前提下，把当前执行路线改写为 Direct P2：先验证目标纵向链路，再完成切换关键功能并前置测试环境切换/回退演练，最后补齐完整目标能力。

**Blocked by:** 04 / 接通真实旧 PG/RLS 与 Redis（负向调查已闭环）

**Status:** resolved

**Plan mapping:** Direct P2 replanning only

**Baseline:** ani-iam `05ba302661d593b608df070dd51cc063fc9f8023` 加当前未提交事项03/04现场；ANI 来源候选 Git object `0cedae825a489d936cf41815dc27f278f6d3213c`。ANI 当前工作树分支和未提交文件不作为基线输入。

**Scope:** 调研 `mattpocock/skills` 官方仓库中与规格、拆票、计划重排、事项 supersede/状态迁移相关的指导；调整当前规格与 tracker 入口、阶段计划、Direct P2 规格与事项依赖；建立旧事项到新事项的完整验收映射；在用户接受后发布新事项；诚实保留 `pass`、`fail`、`not_verified`。

**Out of scope:** IAM/ANI 业务代码、Proto/OpenAPI 实际契约、数据库 migration、依赖版本、容器、部署、切流、Credential 失效、数据重建、旧文件删除以及任何 P2 功能实现。

**Allowed paths:** `AGENTS.md`、`CLAUDE.md`、`.scratch/ani-iam-rebuild/spec.md`、`.scratch/ani-iam-rebuild/issues/**`、`.scratch/ani-iam-rebuild/evidence/39-replan-direct-p2/**`、`.scratch/ani-iam-p2-direct/**`、`docs/agents/issue-tracker.md`、`docs/plans/plan-iam-kratos-phased.md`、`docs/plans/plan-iam-service-refactor.md`。

**Forbidden paths:** `api/**`、`cmd/**`、`configs/**`、`internal/**`、`migrations/**`、`tests/**`、`go.mod`、`go.sum`、`deploy/**`、`../ANI/**`；不得安装外部 skill、不得提交、部署、切流或删除旧事项。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/39-replan-direct-p2/`

- [x] `mattpocock/skills` 结论来自固定官方仓库 revision，并保存可定位引用。
- [x] Direct P2 计划明确两个前置 Go/No-Go：目标纵向链路和切换关键功能整组演练。
- [x] 事项04保持负向 `FAIL / BLOCKED`，事项01–03历史证据不被改写。
- [x] 所有旧事项05–38均有明确状态和新事项映射；未实施事项没有伪装为 `resolved/pass`。
- [x] 新事项依赖图无环、DP2-01–20 均为 `ready-for-agent` 且没有 `claimed`，最终切换、删除和 Credential 失效没有被自动授权。
- [x] 当前 ANI 来源以精确 commit 固定；动态 `main`、当前分支和脏工作树不进入 Oracle。

**Verification:** 检查 Markdown 链接与状态集合；枚举所有旧/新事项并验证唯一映射、唯一 `claimed` 和依赖闭环；检查计划中的 Gate、停止条件、人工检查点和固定 Git 身份；`git diff --check`。

**Stop conditions:** 官方指导无法固定到不可变 revision；旧验收条件无法无损映射；Direct P2 的 Core 契约、测试依赖或切换边界需要新的用户决策；需要修改业务代码或执行任何破坏性动作。

**Recovery:** 只回退本事项允许路径中的计划/事项变更；事项01–04及其证据保持可读，当前业务代码现场保持不动。

## Result

`READY FOR HUMAN REVIEW`。

- 事项04以 `resolved` 关闭调查，但结果保持 `FAIL / BLOCKED`；CP0 未获得 Go。
- 依据官方 `mattpocock/skills@6654f6b60cd9d5be8b54c6fafe44346dabeb3b76`，方向改变后重新生成 tickets，不在旧票上继续扩写新路线。
- 本仓库为保留审计历史，没有物理删除旧事项；事项05–38以 `wontfix + Superseded by` 关闭，并通过 `ticket-mapping.md` 映射到 DP2-01–20 草案。
- 新规格和 20 张纵向 ticket graph 已形成；DP2-05 是目标纵向链路 Go/No-Go A，DP2-14 是切换关键功能整组演练 Go/No-Go B。
- 用户已明确接受规格、粒度、依赖和交付行为；DP2-01–20 已发布为 `ready-for-agent`。唯一 frontier 是 DP2-01，但它没有被领取或启动。
- 验证结果：旧事项05–38共 34 张，均为 `wontfix` 且含继任映射；已发布 DP2-01–20 编号完整、必需字段完整、依赖只指向更小编号、无环；旧到新映射解析到全部新编号；DP2-14/18/19 保留精确人工确认；`git diff --check` 通过。
- 未运行代码或真实依赖测试：本事项仅改变规格、计划和事项材料；业务代码、ANI、数据库、容器、部署和外部环境均未由本事项修改。

## Comments

- 2026-09-03：用户明确指示“关闭事项04的负向调查结果并重排计划，看看 mattpocock/skills 中有没有类似的指导”。事项04已以负向结果 `resolved`，随后领取本事项；本事项只改变计划、事项和入口材料。
- 2026-09-03：调研确认上游在 pivot 后保留 spec、丢弃 unfinished tickets，并在发布新 tickets 前要求用户确认拆分、依赖和交付行为。由于本仓库要求审计历史，采用“不删除旧票、建立完整映射、以 wontfix 关闭旧切片、新建独立 Direct P2 spec/ticket draft”的本地适配。
- 2026-09-03：重排和静态验证完成，事项转为 `ready-for-human`。人工接受后仍需重新领取一个有界计划事项来发布 DP2 tickets；接受本事项不自动开始 DP2-01。
- 2026-09-03：用户明确接受 Direct P2 规格与 ticket plan，并要求“发布新事项，但不启动 DP2-01”。当前没有其他 `claimed` 事项，因此重新领取事项39；本轮只发布事项和更新入口状态，不修改业务代码或外部状态。
- 2026-09-03：DP2-01–20 已发布并通过编号、状态、依赖、必需字段、映射和破坏性检查点验证；DP2-01 保持 `ready-for-agent`。事项39转为 `resolved`，没有领取后续事项。
