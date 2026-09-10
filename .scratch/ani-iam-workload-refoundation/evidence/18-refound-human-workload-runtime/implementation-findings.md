# WR-18 实施发现

现有 Membership Handler 使用 `get/list/update/removeTenantIAMMembership(s)` 作为错误 operation ID；固定 owner registry 实际名称为 `get/list/update/removeTenantIAMMember(s)`。接入真实 IAM 权限评估时必须使用 owner 已登记名称，不能把旧错误字段当权限目录。本票按固定 registry 修正内部映射；没有改变 owner 权限或外部 registry。

首次远程生成 run `wr17-18-20260910T011051Z-60706e82` 在 sqlc 报 UpdateTenantWorkloadBaseStatus 的 version 歧义；源查询已限定为 principals.version，旧失败现场保留，后续新快照重跑。


## 2026-09-10 运行验证中的收敛

- Unit/contracts/cmd/tests 普通构建范围在 `wr17-18-20260910T014228Z-64eb15d7` 全部 pass；同一 run 的 integration 编译因残留 UpdateTenantWorkload.Name 测试字段失败，整体退出1，不能将整个 run 算通过。
- `wr17-18-20260910T014637Z-a92f151f`：真实 PostgreSQL 三组测试 pass，exit0，source SHA256 `74008f8fc9eb4bb684392e1b58e1ae17532afa224041a9c253b9a8c2b58a7b64`。覆盖空库安装/严格运行权限/no-RLS跨Tenant约束、Workload owner/Binding/Grant实时撤销、Human凭据边界、终态关系、并发幂等和 Audit/结果写失败时原子回滚。此结果不是当前后续修改的最终验收。
- 已将所有本票 PG/Redis/Dex 启动接入专有名称、run标签、loopback-only端口核对及资源登记；Redis每容器随机密码。首次正式进程被 localhost 配置拒绝，fixture已统一显式127.0.0.1。没有放宽正式隔离配置。
- UpdateTenantMembership 禁止借 removed 状态代替独立 RemoveTenantMembership 权限；成员removed及API Key revoked为不可重新激活终态，数据库约束和正式RPC负向测试覆盖。
- 正式管理变更在无x-request-id/x-correlation-id时暴露 ErrAuditRequestIDRequired，旧映射误呈现Unavailable。修正为复用IAM生成的验证Decision ID补齐追踪，不将metadata作为主体/权限。重放仍先执行当前业务授权。
- 原管理接口大回归已改为正式cmd/server、真实Human登录及真实数据库授权；不再由测试把x-ani-*Header当作可信管理员。
- 上述中间运行当时尚未完成正式进程、完整 Human/OIDC 回归、vet/race 和最终快照一致性，因此当时保持 claimed；后续验收结果见下。

## 最终收敛与验证

- Redis 故障注入曾使用 restart，Docker 自动分配的 host port 从 32834 变为 32835，导致恢复等待失败；诊断为 fixture endpoint 变化。现对本 Goal 精确登记容器 pause/unpause 保持 endpoint，正式 IAM 的不可用拒绝、健康和恢复测试通过；没有放宽依赖检查。
- 完整检查 run `wr17-18-20260910T020353Z-0f88e91f` 退出 0：固定生成重复一致、普通单测/契约、build、vet、四包 race 与真实依赖集成通过。
- 随后仅四个 integration fixture 文件对齐冻结资源名、canonical gateway 名称、实际 image/endpoint 与私有凭据引用，并随机生成 Dex 测试凭据；最终 run `wr17-18-20260910T021423Z-bca28bda` 重新编译运行全部集成，退出 0，43 个顶层测试、含子用例 67 项通过、0 失败、3 个跨项目测试跳过。最终源码 SHA-256 `6727e79febbdec2d549601a7ccc966d399d0ac46e278bc0adb24ff2fc2b8f42f`。
- 最终逐文件/模式、生成物、范围与来源保护检查 pass；实际检查适用性和跳过边界见 [完整交接](README.md)。此前失败 run 不被改写为 pass。
