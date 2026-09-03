# 16: 冻结公开 OpenAPI 与 Operation Registry

**What to build:** 冻结目标公开 IAM 路径及其唯一 Owner、认证分类、Permission、错误和 obligation，使 Gateway 能从契约生成并 fail closed。

**Blocked by:** 15 / 观察并下线旧 Auth Runtime

**Status:** wontfix

**Superseded by:** Direct P2 DP2-02；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。需求继续存在，旧执行切片不再领取。

**Plan mapping:** P2-0

**Baseline:** 当前规格、核心方案、ANI 固定 Commit 与 P1 已接受证据。

**Scope:** ANI 公网 OpenAPI、operation 标注、生成 registry/policy revision、SDK 影响、错误与 obligation 映射。

**Out of scope:** IAM 业务实现、调用方切流、旧接口删除、Core 运行时修改。

**Allowed paths:** `../ANI/repo/api/openapi/**`、`../ANI/repo/services/ani-gateway/**` 中的生成器/registry/契约测试、`../ANI/repo/pkg/generated/**`、`../ANI/repo/frontends/console/src/api/**`、`../ANI/repo/frontends/boss/src/api/**`，以及本事项和其证据目录。

**Forbidden paths:** `internal/**`、`migrations/**`、`deploy/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/deploy/**`；不得实现业务或执行切流。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/16-freeze-public-openapi-registry/`

- [ ] 每个目标 operation 只有一个 Gateway Handler 和一个后端 Owner。
- [ ] Public、Authenticated-only、Authorized 分类完整；未知、缺失标注或版本不匹配 fail closed。
- [ ] Permission 与 typed obligation 可生成，缺失 obligation Handler 的 operation 不可注册。
- [ ] API/SDK breaking 结果和固定删除清单可审查。

**Verification:** OpenAPI lint/generation、registry completeness、owner/obligation 和 breaking checks 通过。

**Stop conditions:** Owner/Permission/obligation 不唯一，或需要在契约冻结时提前删除运行能力。

**Recovery:** 回退未发布的契约变更，不改变 P1 Runtime。

**Human checkpoint:** 领取本事项前确认进入 P2 设计阶段；发布 breaking 契约另需确认。
