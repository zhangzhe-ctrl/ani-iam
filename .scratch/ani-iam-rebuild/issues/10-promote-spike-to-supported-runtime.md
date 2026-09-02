# 10: 将 Spike 晋升为受支持的 iam-service

**What to build:** 把通过 CP0 的隔离 Runtime 转为可持续构建和维护的 iam-service，同时保持旧 Auth 外部语义不变。

**Blocked by:** 09 / 完成三调用方门禁与 CP0 判定

**Status:** ready-for-agent

**Plan mapping:** P1-0

**Baseline:** 人工接受的 CP0 Go 证据与固定依赖。

**Scope:** 移除实验捷径、稳定构建/配置/启动方式、独立 CI、依赖固定和 CP0 全矩阵回归。

**Out of scope:** 目标 P2 契约、Schema、授权或调用方切流。

**Allowed paths:** `go.mod`、`go.sum`、`cmd/server/**`、`internal/**`、`configs/**`、`tests/**`、CI/构建配置，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得引入 P2 目标语义。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/10-promote-spike-to-supported-runtime/`

- [ ] Runtime 可从干净环境构建、配置、启动和测试。
- [ ] CP0 全矩阵在晋升后保持通过，没有隐藏实验开关或本机路径依赖。
- [ ] 项目不导入 ANI 内部实现，跨项目依赖均固定到不可变版本。
- [ ] 支持边界和仍属兼容临时代码的部分被明确标识。

**Verification:** 独立 CI、clean build、CP0 regression、依赖和漏洞检查通过。

**Stop conditions:** 晋升需要改变旧 wire、数据或安全语义。

**Recovery:** 回退到 CP0 固定 Runtime；旧 Auth 继续承载流量。
