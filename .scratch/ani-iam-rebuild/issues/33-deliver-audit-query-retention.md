# 33: 交付 Audit 查询与 180 天语义

**What to build:** 提供受控的 ListAuditEvents/GetAuditEvent 查询，使 Tenant 与 Platform 审计边界可验证，并把“至少在线可查 180 天”实现为明确的数据与测试语义。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；18 / 建立无 RLS 持久化基础；21 / 交付 Role、Binding 与 Membership 管理；31 / 交付 Platform Role、Invitation 与 Membership 管理；32 / 交付高风险 Tenant Admin 恢复

**Status:** wontfix

**Superseded by:** Direct P2 DP2-16；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-4

**Baseline:** Q191–Q200、Q241–Q252 与 ADR-0014/0017 的 Audit、查询、保留和测试环境清理决定。

**Scope:** append-only Audit Repository、List/Get、cursor pagination、Tenant auditor/admin allowlist、Platform Auditor 边界、180 天最小查询期、增长监控与测试环境清理负向保护。

**Out of scope:** Audit export、SIEM、WORM、密码学不可抵赖、自动 retention cleanup 和生产保留政策。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`../ANI/repo/api/openapi/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/deploy/**`；不得修改 16/17 已冻结契约、添加批量导出、审计修改/删除 API、自动清理 Job 或任意 payload 查询。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/33-deliver-audit-query-retention/`

- [ ] 只提供 ListAuditEvents/GetAuditEvent 与稳定 cursor pagination，不提供 export。
- [ ] tenant auditor/tenant-admin 只能查看本 Tenant 的 allowlisted 事件；Platform 恢复、内部 Credential 和跨 Tenant 元数据仅 Platform Auditor 可见。
- [ ] 普通 IAM Role 无 UPDATE/DELETE 权限且不存在审计 mutation API。
- [ ] 180 天表示最小在线查询期，不承诺第 181 天删除；缺少自动清理时监控行数、容量、增长率和最老记录。
- [ ] 测试环境手工清理不能删除 active、pending、unpublished、attention_required 或仍参与 reuse 检测的数据。

**Verification:** Tenant/Platform 越权负向、cursor、180 天边界、受限 Role、append-only 和允许/禁止清理测试通过。

**Stop conditions:** 查询必须使用跨边界 bypass、需要暴露敏感 details 或无法保证 append-only 权限。

**Recovery:** 回退查询入口和索引；不修改或删除既有审计事实。
