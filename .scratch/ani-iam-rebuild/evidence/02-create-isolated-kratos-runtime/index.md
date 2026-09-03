# 事项02证据索引

状态：`PASS`

采集日期：2026-09-02（Asia/Shanghai）

基线：事项01已接受的 ANI `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be` Oracle；本事项没有修改 ANI 或连接旧 Auth 写路径。

| 证据 | 状态 | 定位 |
| --- | --- | --- |
| Kratos CLI、模板提交、生成命令与原始文件摘要 | `pass` | [generated-baseline.md](generated-baseline.md) |
| 官方 scaffold 到最终实现的保留/删除/替换 | `pass` | [scaffold-delta.md](scaffold-delta.md) |
| Kratos core/contrib 采用项、未采用项与剩余薄自实现 | `pass` | [component-coverage.md](component-coverage.md) |
| Proto 生成器、固定插件与可复现生成 | `pass` | [codegen.md](codegen.md) |
| Go/直接依赖精确版本与校验和 | `pass` | [dependency-baseline.md](dependency-baseline.md) |
| Kratos 生命周期、探针、metrics 与优雅停止 | `pass` | [verification.md](verification.md) |
| loopback 与 `cp0-isolated` typed config 边界 | `pass` | [isolation.md](isolation.md) |
| CycloneDX 1.6 SBOM，34 个第三方运行时组件 | `pass` | [bom.cdx.json](bom.cdx.json) |
| 运行时依赖许可证 inventory | `pass` | [licenses.md](licenses.md) |
| `govulncheck@v1.7.0` 已知漏洞扫描 | `pass`；模板依赖两次安全替换 | [vulnerabilities.md](vulnerabilities.md) |

## 结论边界

- 本事项只证明 CP0-0 运行外壳成立，不证明旧 Auth 14 RPC、PostgreSQL/RLS、Redis、Dex 或调用方兼容；这些属于后续事项。
- Go 1.25.7 原生 runner、额外 lint、Race、Fuzz、专项并发、性能、HA、备份恢复与生产 soak 保持 `not_verified`。
- 没有创建数据库、Redis namespace、Dex client、NATS 资源、容器或部署，也没有切流、删除或修改 Credential。
