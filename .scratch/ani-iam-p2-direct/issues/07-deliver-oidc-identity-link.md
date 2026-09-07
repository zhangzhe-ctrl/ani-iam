# 07: 交付 OIDC 登录与 Identity Link

**What to build:** 交付 Authorization Code + PKCE 的目标 OIDC 登录与显式 Identity Link，防止按相同邮箱自动合并或接管账号。

**Blocked by:** 05 / 证明目标最小纵向链路（Go/No-Go A 已人工接受）

**Status:** claimed

**Type:** enhancement

**Plan mapping:** DP2-2 / OIDC

**Baseline:** 05 接受的目标链路、固定 OIDC/Identity 契约、Provider 与 verified-email 决策。

**Scope:** PKCE S256、state/nonce/verifier 单次使用、固定 callback、issuer/audience/signature、`email_verified`、显式 Link、近期重认证、Audit 和稳定错误。

**Out of scope:** 自动账号合并、动态 Redirect URI、公共 ANI JWKS、生产 Provider 扩展或旧 OIDC 兼容。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`deploy/**`、`../ANI/repo/**`；不得按邮箱自动关联 Principal、接受未验证邮箱、动态 callback 或泄露 state/nonce/verifier。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/`

- [x] 真实 Dex 成功链路和 issuer/audience/signature/nonce/PKCE 失败路径完整。
- [x] state、nonce、verifier 十分钟且单次消费，重放被拒绝。
- [x] 相同 verified email 属于其他 Principal 时稳定失败，不自动合并。
- [x] Link 要求已登录和近期重认证；状态变化与 Audit 原子。

**Verification:** 真实 Dex integration、攻击/重放、Identity 冲突、事务失败、错误映射和 Secret 扫描通过。

**Stop conditions:** Provider 无法隔离；实现要求自动邮箱关联、动态 redirect 或修改冻结契约。

**Recovery:** 删除隔离 OIDC Identity/临时状态，保留原 Human Principal。

## Claim record

- Claimed at: `2026-09-07T13:19:52+08:00`.
- Dependency status: DP2-05 is `resolved` after accepted Go/No-Go A, DP2-06 is `resolved`, DP2-08 through DP2-14 remain `ready-for-agent`, and no other Direct P2 ticket is `claimed`.
- IAM start: branch `codex/direct-p2-06-14`, starting commit and current HEAD `b52907dc4cb919dbbbe68768734e0a453b36b954`. The worktree and index were clean before the claim; the only claim-time diff is this ticket record.
- Frozen source: `api/iam/v1/authentication_service.proto`, accepted ADRs 0011-0015 and 0019-0021, and the DP2-2 sections of `docs/plans/plan-iam-service-refactor.md`; the frozen API remains forbidden to modify.

## Test plan

- Begin flow: exact configured provider and redirect URI, PKCE S256 challenge, opaque state, nonce and verifier generated without logging or durable plaintext storage, ten-minute expiry, idempotency behavior, and Redis dependency failure as `503` with no local fallback.
- Complete login: real pinned Dex discovery/code exchange/JWKS verification; issuer, audience, signature, nonce, state, redirect, expiry and single-consumption negatives; `email_verified=true`; existing active Identity only; no open registration or email-based automatic Principal linking.
- Identity link: authenticated Principal and recent reauthentication required at begin and completion; the same full OIDC proof; verified-email and issuer/subject conflicts fail without merge; Identity plus required Audit commit in one PostgreSQL transaction and roll back together on failure.
- Session result: OIDC login creates the same target Session/Grant/Refresh boundary model as Password login with `oidc` authn method, stable gRPC/ErrorInfo mapping, and no Token/code/state/nonce/verifier leakage in logs or evidence.
- Gates: focused unit tests, real isolated PostgreSQL/Redis/Dex integration, replay/concurrency and forced Audit rollback, full normal and race/integration suites, vet/module/contracts/generation/migration/SBOM/license/vulnerability/Secret scans, exact path audit, and independent Standards/Spec review.

## Recovery plan

Before commit, retain only allowed DP2-07 diffs and terminate task-owned PostgreSQL, Redis, and Dex resources. Recovery removes only the ticket's isolated OIDC operations and identities while preserving existing Human Principals and other credentials. After commit, use a new reviewed revert commit against the exact DP2-07 SHA. Never reset, stash, amend, rebase, force, push, deploy, touch ANI worktrees, or delete shared Provider state.

## Implementation checkpoint

- Path extension accepted by the user at `2026-09-07T14:56:10+08:00`: `go.mod`, `go.sum`, `internal/conf/conf.proto`, `internal/conf/conf.pb.go`, `internal/conf/validate.go`, `internal/conf/validate_test.go`, `cmd/server/app.go`, `cmd/server/app_test.go`, `internal/server/workload_identity.go`, and `internal/server/workload_identity_test.go`. This does not extend `api/**`, `deploy/**`, or any ANI worktree.

- `pass`: RED/GREEN unit slices cover exact redirects, ten-minute exclusive expiry, stable reauthentication error classification, verified-email policy, and explicit Identity Link behavior.
- `pass`: isolated real Redis proves digest-keyed, idempotent, ten-minute and single-use state/nonce/verifier storage without raw Redis keys; a 16-way concurrent consume admits exactly one callback and rejects all replays.
- `pass`: isolated restricted-role PostgreSQL proves OIDC Session/Grant/Refresh/Audit commit, Identity/Audit rollback, email conflict, and transaction-time rejection after the Identity or authenticated Session is disabled.
- `pass`: the frozen gRPC error matrix maps invalid redirect to `INVALID_ARGUMENT`, consumed state to `CREDENTIAL_INVALID`, reauthentication/identity conflicts to `PERMISSION_DENIED`, and provider failure to `IAM_UNAVAILABLE`.
- `pass`: the isolated Provider artifact is pinned to the multi-platform OCI index `ghcr.io/dexidp/dex:v2.40.0@sha256:3e35d5d0f7dbd33fbadc36a71ff58cf4097ab98d73d22f6cb9a6471a32e028af` (linux/amd64 manifest `sha256:59d8b5540fcf4e876b70fb5438321c594b84a5b4a255540e9bb41834fd797770`), resolved by a read-only registry manifest inspection on 2026-09-07.
- Required next seam: use the accepted `coreos/go-oidc/v3` plus `golang.org/x/oauth2` adapter, add typed OIDC config, wire it at the sole composition root, and authorize the four frozen OIDC RPCs for the authenticated Gateway workload. Hand-written OAuth/OIDC, test-only runtime wiring, and an unconfigured RPC allowlist are not acceptable substitutes.
- Exact path extension required before that seam can be implemented: `go.mod`, `go.sum`, `internal/conf/conf.proto`, `internal/conf/conf.pb.go`, `internal/conf/validate.go`, `internal/conf/validate_test.go`, `cmd/server/app.go`, `cmd/server/app_test.go`, `internal/server/workload_identity.go`, and `internal/server/workload_identity_test.go`. The existing `configs/**`, `internal/data/**`, and `tests/**` permissions already cover non-secret configuration, the provider adapter, and isolated real Dex tests.
- The exact path extension was accepted at the checkpoint above. Real Dex issuer/audience/signature/nonce/PKCE evidence and runnable server wiring are therefore authorized for this ticket, subject to the recorded gates, independent review, and exact staged-path audit before commit.

## Final pre-commit result

- `pass`: fixed `coreos/go-oidc/v3@v3.20.0` and `oauth2@v0.36.0` implement the real Provider seam; the pinned Dex OCI artifact completes the isolated Authorization Code + PKCE path, and token/JWKS dependency timeouts retain `DeadlineExceeded` for stable 504 mapping.
- `pass`: Redis stores digest-keyed ten-minute operations, rejects replay under sixteen concurrent consumers, and a whitespace-mutated state neither matches nor consumes the exact valid state.
- `pass`: restricted-role PostgreSQL proves Tenant-scoped existing-Identity login, two-Tenant negatives, explicit Identity Link, transaction-time state recheck, rollback, success/failure Audit, and authoritative Session `authn_methods` (`password` or `oidc`). No verified-email automatic linking exists.
- `pass`: exact-final-tree `go test -tags=integration ./... -count=1` passed with the real integration package in `132.483s`; the complete `-race` form passed with that package in `103.038s`.
- `pass`: vet, module verify/tidy, frozen contracts, fixed Buf/config/API generation, fixed sqlc, Atlas validate/hash, deterministic SBOM, new-dependency license review, affected-code vulnerability scan, current-tree Secret assessment, exact path audit, and Docker resource cleanup passed.
- `pass`: independent Standards and Spec reviewers report no remaining finding after all corrections.
- `not_verified`: BOSS/Platform OIDC, real ANI Gateway/browser callback integration, production Dex/Kubernetes/deployment/traffic, Secret rotation and Production Readiness remain outside this ticket. DP2-13 must stop for an Owner decision because the plan requires BOSS OIDC before Go/No-Go B while DP2-15 owns the absent Platform model and the active Goal forbids starting DP2-15.
- Pre-commit status remains `claimed`. Resolution and the exact feature commit SHA are recorded only after the reviewed feature payload is committed; no push or deployment is authorized.
