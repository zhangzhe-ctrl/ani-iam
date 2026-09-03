# 36: 执行测试环境数据重建与整组切换

**What to build:** 在明确测试维护窗口从空库重建目标 IAM/Core 状态，使五类调用方整组切到目标契约，并让所有旧 Credential 明确失效。

**Blocked by:** 35 / 完成目标调用方与 UI E2E

**Status:** wontfix

**Superseded by:** Direct P2 DP2-14、DP2-18；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。本状态不授权任何切流、重建或 Credential 失效。

**Plan mapping:** P2-7

**Baseline:** 35 接受的契约/镜像/配置、明确数据清单、固定 migration/seed、测试环境快照和恢复步骤。

**Scope:** 冻结写入、快照、独立数据库 migration/seed、新签名 Key、Credential 失效、IAM/Core/调用方整组部署、冒烟和人工恢复。

**Out of scope:** 逐行线上迁移、长期双写、兼容视图、正式生产切流或生产就绪声明。

**Allowed paths:** `migrations/**`、`configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`internal/biz/**`、`internal/data/**`、`internal/service/**`、上述目录之外的 `../ANI/repo/**`；若发现实现缺口，必须停止并回开对应事项，不得在切流票中修改业务语义。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/36-rebuild-data-and-cut-over/`

- [ ] 执行前列出精确环境、数据、Credential、镜像、契约摘要、写入冻结和恢复动作并获得人工确认。
- [ ] Core 先生成/seed Tenant ID，IAM 只引用并建立明确批准的 Principal/Invitation/Bootstrap。
- [ ] 旧 Session、Refresh、Blocklist、API Key 全部失效，新库从空库重放成功。
- [ ] 五类调用方、认证、授权、API Key、Service Token、Lifecycle 和 Audit 冒烟全部通过。

**Verification:** 快照可读、migration/checksum、seed、Credential 负向、五类调用方和目标状态检查通过。

**Stop conditions:** 快照/恢复不可用、写入无法冻结、Artifact 漂移或任一目标冒烟失败。

**Recovery:** 使用固定镜像和 snapshot/reseed 人工恢复 P1 测试环境；结果如实标为 pass/fail/not_verified。

**Human checkpoint:** 所有破坏性写入、Credential 失效和切流前必须获得精确人工确认。
