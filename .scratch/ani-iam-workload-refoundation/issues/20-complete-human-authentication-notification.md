# 20: 完成 Human 认证与真实 Notification 密码动作链

**Status:** needs-triage

**Type:** task

**Blocked by:** 19

**阶段与能力：** M1；C02、C03、C04、C10，相关 C05/C11/C12/C13。

**授权边界：** 计划票，尚未领取；先冻结具体接口和测试依赖。

## 目标

让既有 Human 登录能力和密码安全动作通过正式 IAM/Gateway REST 工作，并通过真实 OIDC owner 与真实 Notification 进程完成外部依赖链；无需前端页面。

## 范围与非目标

- Password 登录、OIDC 登录/回调和显式 account link；Session、旋转 Refresh、Logout、SwitchTenant 以及冻结的认证状态/错误契约。按已接受 D08，OIDC 认证流程由 IAM 拥有，Gateway 只保留固定回调入口、Cookie 和转发；具体接口输入仍需冻结。
- Gateway REST 的 cookie/CSRF/redirect/token 传输契约，使用 HTTP runner/cookie jar 模拟浏览器请求；身份和会话规则留在 IAM。
- 冻结的密码变更/忘记/重置等动作使用真实 Notification 提交/状态接口和真实 SMTP sink；Notification 消费真实 IAM Workload 身份，提交失败、投递失败、重试与安全响应均可观察。
- PasswordAction、通知 outbox 与安全 Audit 在 IAM 本地事务中提交，由真实 dispatcher 重试提交 Notification；跨服务不做双写事务，Notification 持久接受、SMTP 投递与 action token 消费分别确认。
- 动作 token 与 session/refresh 失效、audit、用户枚举防护、链接绑定和重放限制遵守现有接受的语义；先维护契约必需能力，不引入新的通用工作流平台。
- 非目标：Console/BOSS UI、生产 SMTP/真实外部邮件发送、Core lifecycle/admin 全集、共享用户凭据失效、旧 Auth 删除、完整 disaster recovery。

## 固定输入与工作面

- IAM、ANI/Gateway、OIDC provider、Notification 正式服务及 SMTP sink 的版本/commit/tree/OCI、contract/config/seed/scenario 和隔离 manifest：`not_frozen`。
- 候选工作面：Human/Session usecase+storage、Gateway auth adapter、Notification caller adapter 与定向测试/config；精确 Allowed/Forbidden 文件清单 `not_frozen`。跨仓库仅纳入必要接口接线，不能扩展为通知服务重写。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与 WR-19 身份链证据。loopback discovery/fake Notification 只用于早期测试，不能作为本票最终成功证据。

## 验收与验证

1. Password/OIDC/link 各成功、无效凭据、code+PKCE/state/nonce/issuer/audience/redirect 校验、过期/重放/跨 flow、已绑定/冲突、owner 不可用均通过正式 Gateway→IAM 与真实 OIDC 进程验证；不按邮箱自动合并 Identity。
2. Refresh rotation/reuse/concurrent refresh、Logout、SwitchTenant、过期或禁用 Session/会员状态的拒绝行为与冻结契约一致；cookie 属性、CSRF/Origin、错误码及副作用可观测。
3. 密码动作从 REST 请求到 Notification 正式进程接收、SMTP sink 收到、动作完成以及旧状态按规则失效形成完整链；不把 Submit 接受等同投递成功。
4. 一次性 action token 重放、过期、重复提交、Notification 超时/投递失败/重试与审计事务边界通过定向测试，不留下越权或明文 Secret 证据。
5. 每项注明真实 owner/正式进程与不可变输入；无前端不影响本票通过，但无法用测试替身证明上述必需依赖通过。

## 恢复与停止

恢复隔离工作树、配置和专有测试会话/通知资源；不修改共享用户或发真实外部邮件。若通知失败需要跨库事务/双写、Human 能力缺失、身份绕过、cookie/CSRF 被弱化或发现接口语义未冻结，停止依赖路径并记录问题，不由 UI 修补后端契约。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/20-complete-human-authentication-notification/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，Human/Notification 正式链 `not_verified`。
