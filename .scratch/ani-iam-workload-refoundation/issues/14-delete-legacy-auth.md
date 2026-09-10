# 14: 删除 legacy Auth 与不安全 S2S 路径

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-26 旧代码裁剪；WR-29 资产/数据删除及适用恢复门禁。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 经独立人工确认，在 zero-reference 后删除旧 Auth runtime/Proto/compat/RLS/schema、Service Principal/Token 残留和 plaintext/shared-secret/dev-header S2S 路径。

**Blocked by:** 13 / 重建测试数据并最终测试切换

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-14 / replaces DP2-19

**Scope:** human-approved deletion manifest、runtime/config/deploy/schema/contract/build/reference removal、target-era forward-repair artifacts、offline forensic source archive、post-delete clean install/E2E。WR-13 point-of-no-return约束继续有效。

**Non-goals:** 删除历史 ADR/evidence、生产资产、未列路径、邻接重构。

**Required human input before claim and deletion:** 先创建、完成并人工接受一个独立的持续long-term DR package/rotation/rehearsal前置事项，再更新本票依赖；WR-13已退役的cutover-window package不能满足该条件。随后对精确deletion manifest中每个repository path、owning service、deploy object、legacy table/Credential reference、target-era forward-repair artifact、offline forensic source archive、恢复步骤和不可恢复动作逐项确认；claim许可与实际delete许可分别记录，不能由前者推导后者。旧IAM auth snapshot/runtime/signing key、Service-era SPIRE/broker identity不得列为可运行恢复目标，人工确认也不能放宽WR-13 point-of-no-return。

**Acceptance:** active target zero-reference；历史evidence与offline source archive保留但不能进入build/deploy/runtime；clean build/install/migration与全部关键E2E在删除后通过；没有隐藏fallback。删除或故障恢复均不能恢复旧auth rows、旧signing trust、旧runtime，或重新enable旧SPIRE/broker identity；旧Credential持续negative。

**Verification:** pre/post reference scans、fixed manifests/digests、database/deploy inventory、all repo gates、five caller/S2S E2E、Secret scan；删除后及一次forward-repair rehearsal均验证全部旧Credential/identity negative、target新Credential positive，并证明offline archive未被active build/deploy引用。

**Stop conditions:** 独立long-term DR前置事项缺失或未通过真实rehearsal；任一引用、consumer、forward-repair dependency或target归属不清；人工确认不覆盖精确删除动作；恢复设计需要启动旧runtime、恢复旧auth snapshot/signing key或重新enable旧identity。

**Recovery:** 只在新架构上使用事先固定的target-era source/build/config/schema artifacts和独立前置事项已验证、持续更新的long-term DR package做forward-only repair，必要时从offline archive读取旧source作取证但不得部署。禁止恢复旧IAM auth snapshot/runtime/signing key、复用已退役的WR-13 cutover-window package或重新enable旧SPIRE/broker identity；数据需重建时签发全新Credential。无法保证该前向恢复的删除动作不得执行；额外人工确认也不能授权跨越point-of-no-return。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/14-delete-legacy-auth/`
