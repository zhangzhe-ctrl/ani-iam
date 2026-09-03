# 02: 冻结公开 OpenAPI 与 Operation Registry

**What to build:** 冻结目标公开 IAM 路径及其唯一 Owner、认证分类、Permission、错误和 typed obligation，使 Gateway 能从契约生成 registry 并 fail closed。

**Blocked by:** 01 / 冻结 Direct P2 来源与替换基线

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-0 / public contract

**Baseline:** 01 人工接受的来源/调用面/删除清单、当前 Direct P2 规格和目标设计。

**Scope:** ANI 公网 OpenAPI、`x-ani-authz`/exposure 标注、operation/owner/authn/authz/obligation、生成 registry/policy revision、稳定公开错误、SDK breaking 影响和 replacement gates。

**Out of scope:** IAM 业务实现、Core runtime、调用方切流、旧接口删除或兼容 fallback。

**Allowed paths:** `../ANI/repo/api/openapi/**`、`../ANI/repo/services/ani-gateway/**` 中生成器/registry/契约测试、`../ANI/repo/pkg/generated/**`、`../ANI/repo/frontends/console/src/api/**`、`../ANI/repo/frontends/boss/src/api/**`，以及本事项和证据目录。

**Forbidden paths:** 本仓库 `internal/**`、`migrations/**`、`deploy/**`；`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/deploy/**`；不得实现业务、切流或删除运行能力。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/02-freeze-public-openapi-registry/`

- [ ] 每个目标 operation 只有一个 Gateway Handler 和一个后端 Owner。
- [ ] Public、Authenticated-only、Authorized 分类完整，未知/缺失/版本不匹配 fail closed。
- [ ] Permission 和 typed obligation 可生成；缺少 obligation Handler 的 operation 不可注册。
- [ ] `401/403/409/429/503/504` 映射和 stable ErrorResponse 明确。
- [ ] OpenAPI/SDK breaking 与删除清单可独立审查。

**Verification:** OpenAPI lint/generation/breaking、registry completeness、owner/obligation、stable error 和 clean generated diff 检查通过。

**Stop conditions:** Owner、Permission 或 obligation 不唯一；需要在契约冻结时提前删除运行能力；生成物不能固定到精确摘要。

**Recovery:** 回退未发布契约和生成物；不改变当前调用方路由。

**Human checkpoint:** 任何 breaking 契约的提交、发布或调用方消费前，需要针对精确 diff 的人工确认。
