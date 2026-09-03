# 01: 冻结 Direct P2 来源与替换基线

**What to build:** 固定 Direct P2 使用的 ANI 来源、当前调用面、旧 RLS 缺陷、接受的契约漂移和最终删除清单，形成后续事项唯一可复现的来源基线。

**Blocked by:** 00 / 恢复干净 Kratos 实现基线

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-0 / source baseline

**Baseline:** 00 恢复并验证的干净 Kratos 实现树；已接受的 Direct P2 规格与 ticket plan；ANI Git object `0cedae825a489d936cf41815dc27f278f6d3213c`；历史事项01–04及其证据。

**Scope:** 从精确 Git object 盘点 OpenAPI、Auth 14 RPC、Gateway/Envoy/Inference/Console/BOSS 调用、migration/RLS/Redis/Dex；记录保留行为、目标替换、允许漂移、replacement gates 和 zero-reference 删除清单。

**Out of scope:** 修改 ANI/IAM 业务代码、契约、migration、依赖、部署或真实环境；修复旧 RLS；生成任何 Go 判定。

**Allowed paths:** 本事项、`.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/**`、必要的 `docs/plans/**` 引用修正。

**Read-only inputs:** 当前仓库；`../ANI/repo/**` 仅通过固定 Git object 读取。

**Forbidden paths:** `api/**`、`cmd/**`、`configs/**`、`internal/**`、`migrations/**`、`tests/**`、`deploy/**`、`go.mod`、`go.sum`、`../ANI/repo/**` 的工作树写入；不得使用动态 `main`、当前分支或脏工作树代替来源对象。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/`

- [ ] 来源 object、tree、关键文件 hash 和读取命令可复现。
- [ ] 当前五类调用方、旧 14 RPC、公开 operation 与数据依赖有完整清单。
- [ ] 事项04的 RLS `FAIL / BLOCKED` 被记录为来源缺陷，不被改成 pass。
- [ ] 每个旧行为明确为保留、目标替换或删除，并列出 replacement gate。
- [ ] 删除清单区分 runtime 引用与可保留的历史 ADR/evidence。

**Verification:** `git cat-file`/`git show`、descriptor/OpenAPI/调用方静态盘点、hash 清单和 zero-reference 初始扫描；所有结论绑定精确 object。

**Stop conditions:** 来源 object 不可验证；调用面无法穷举；接受漂移需要新的产品决定；必须读取动态工作树才能得出结论。

**Recovery:** 仅回退本事项和证据材料；不改变任何运行代码、数据或外部系统。

**Human checkpoint:** 人工接受基线证据后才可领取 02 或 03；接受不自动领取下一事项。
