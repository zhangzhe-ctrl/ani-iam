# DP2-01 验证记录

采集日期：2026-09-03（Asia/Shanghai）

## 固定 object 与生成清单

执行：

```bash
git -C /home/chabking/workspace/ANI cat-file -e \
  0cedae825a489d936cf41815dc27f278f6d3213c^{commit}
git -C /home/chabking/workspace/ANI rev-parse \
  0cedae825a489d936cf41815dc27f278f6d3213c^{tree}
python3 .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/generate_fixed_inventory.py
sha256sum .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/source-manifest.tsv \
  .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/migration-files.tsv \
  .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/openapi-operations.tsv \
  .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/related-iam-operations.tsv \
  .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/auth-service-rpcs.tsv
```

结果：`pass`

```text
tree=552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8
fixed inventory generated: commit=0cedae825a489d936cf41815dc27f278f6d3213c tree=552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8 paths=68 migration_files=38 atlas_listed=31 operations=236 related_iam_operations=59 public=8 generated=5 legacy=223 rpcs=14
3c0df2fd53d91a7d91930d72aa1bf847d4158f3724885b7cbd5d9ccf53477849  source-manifest.tsv
e863ffc96f86679dae90bbee7db1bf24e4f1b75b8eb9f25e4ca02e2fa78da7ea  migration-files.tsv
e5f1a2025df723705d0aa37eb09c88379a0a589e724490915076b438195a6bfc  openapi-operations.tsv
5f8cd36dd12ed79db7903b10ce2747e14eef60fe268d83347b797342a870e217  related-iam-operations.tsv
281fcdc00dab5f5193148a5df219ab747cc2bb650627d37f73d25a92427353ef  auth-service-rpcs.tsv
```

## 旧 Gateway 静态门禁观察

固定 commit 的 `git archive` 副本路径为 `/tmp/ani-dp2-01-object.F6EBUP`。执行：

```bash
cd /tmp/ani-dp2-01-object.F6EBUP/repo
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate_gateway_authz_drift.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate_core_gateway_authz_routes.py
```

实际输出：

```text
gateway authz registry: no drift
Core gateway authz route coverage: 297 registered route(s), 236 registry route(s), 0 error(s)
```

这不是目标 gate 的 `pass`：旧脚本允许 223 个 operation 落入 legacy，不覆盖 services-v1 重叠入口，也不证明 owner/obligation/单次 IAM decision；目标结论为 `not_verified`，由 DP2-02 replacement gate 替代。

## 完整性与范围门禁

执行：

```bash
git branch --show-current
git rev-parse HEAD
git status --short --untracked-files=all
git diff --cached --name-status
git diff --check
git diff --cached --check
python3 .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/verify_evidence.py
```

结果：`pass`

```text
branch=main
HEAD=c27ae04c3837e0886773ba016c9cded4169bf1ed
gate: fixed object and IAM baseline
result: pass
gate: fixed inventory regeneration and hashes
result: pass
gate: 236 operations, 59 related operations, and 14 RPCs
result: pass
gate: replacement and deletion completeness
result: pass
gate: initial zero-reference search baseline
result: pass
gate: Markdown links and evidence text
result: pass
gate: workspace and staged-path scope
result: pass
overall: pass
human_checkpoint: required
```

Verifier 还确认：

- 60 个 replacement row 为连续 `B001-B060`，分类计数 8/24/3/25，每行字段和 DP2-02～05 impact 完整；
- 28 个 deletion row 为连续 `D001-D028`，每行 replacement gate 与 zero-reference 条件完整；
- 初始固定对象扫描命中：`auth.v1.AuthService=48`、`AUTH_SERVICE_ADDR=17`、`AUTH_SERVICE_GRPC_ADDR=18`、`AUTH_SERVICE_MINT_SECRET=3`、`X-API-Key=119`、`ANI_AUTH_MODE=206`、`jwt:blocklist:=3`、`tenant-owner=15`；
- Markdown 相对链接存在，无行尾空白，证据中未发现 private-key marker；
- 只有 DP2-01 为 `claimed`；当前所有变更都在 DP2-01 ticket 或其 evidence path；staged path 为空。

## 一次非门禁命令错误

曾将“固定 archive 内运行旧脚本”和“IAM Git 检查”误合并在 archive 工作目录执行。旧脚本正常输出，但相对 IAM 证据路径不存在，且 archive 不是 Git worktree，组合命令以 exit 2 结束。该命令没有写工作区或外部状态，不作为任何 gate 证据；随后从 `/home/chabking/workspace/ani-iam` 使用上面的正确命令重跑，结果为 `pass`。

## Git 与恢复状态

- 当前仅 DP2-01 ticket 和 evidence path 有变更；无 staged 文件。
- `git diff --check` 与 `git diff --cached --check` 结果为 `pass`。
- 未修改 ANI 当前 checkout，也未创建 `/tmp/ani-direct-p2-01-05` 修改 worktree。
- 未 commit、push、deploy、切流、重建数据、失效 Credential 或删除旧资产。
- 人工拒绝时，恢复只涉及删除本 evidence path 并把 DP2-01 恢复为 `ready-for-agent`；不触及 runtime、ANI 或外部系统。

## 仍未验证

| 项目 | 结果 |
| --- | --- |
| 当前固定 ANI object 的 live PostgreSQL/Redis/Dex | `not_verified` |
| 目标 OpenAPI/registry 与 precise breaking | `not_verified`，DP2-02 |
| 目标 IAM/Core descriptor/contracts | `not_verified`，DP2-03 |
| 目标无 RLS PostgreSQL | `not_verified`，DP2-04 |
| 目标 Gateway vertical slice | `not_verified`，DP2-05 |

历史旧 RLS 结果仍为 `FAIL / BLOCKED`，不因本静态基线门禁为 `pass` 而改变。

## 人工接受与提交前复验

用户于 2026-09-03 明确接受 DP2-01 基线。接受只解除本事项的本地提交和进入 DP2-02 的检查点，不授权 push、发布、部署、切流、数据重建、Credential 失效或旧资产删除。

提交前执行：

```bash
python3 .scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/verify_evidence.py --accepted
```

预期并实际要求的结尾为：

```text
overall: pass
human_checkpoint: accepted
```
