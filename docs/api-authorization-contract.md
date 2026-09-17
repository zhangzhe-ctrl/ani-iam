# API 授权声明职责与切换核对规范

状态：2026-09-11 用户接受职责划分及规则固化方向，并要求先写入 IAM。本文件是长期协作规范；不表示所有接口已实现或已切换，也不修改既有 accepted ADR 的权限语义。

当前规范唯一维护在 IAM；ANI、Gateway 和其他服务后续接入时引用固定版本，避免各仓库复制后独立修改。具体接口仍由业务 owner 的契约定义，不能因规范存放在 IAM 而转移业务所有权。实施顺序以 [spec](../.scratch/ani-iam-workload-refoundation/spec.md) 和 [ticket-plan](../.scratch/ani-iam-workload-refoundation/ticket-plan.md) 为准。

## 1. 所有权与变更责任

| 内容 | 主责 | 责任与边界 |
| --- | --- | --- |
| REST 路径、请求、响应、operationId、业务语义 | 接口所属业务 owner | 维护 owner OpenAPI；遵循 ANI 当前 Core/Services API 边界，不因 Gateway 承载路由而转移领域所有权 |
| 接口需要的 resource/actions/scope 和资源检查条件 | 业务 owner，IAM 共同评审 | 明确操作的权限含义、授权边界与敏感性；不能只按 URL、HTTP 方法或历史行为猜填 |
| 声明格式、身份词汇、权限命名、版本和错误约定 | IAM 与 API/Gateway 维护者 | 共同维护可验证的规则及生成契约；涉及已有语义变化时按 accepted ADR 和具体事项处理 |
| 合法 Permission 目录 | owner 声明生成，IAM 按固定版本消费 | IAM 只接收登记权限；管理员不得通过运行时接口创建任意 Permission 字符串 |
| Membership、Role、Role Binding、Session、Grant 和当前授权决定 | IAM | IAM 是授权状态唯一 writer；调用者不复制身份库或自行维护第二套用户授权事实 |
| 管理员能力范围、内置角色权限集合 | 产品/业务与安全责任方确定，IAM 实现 | 内置定义受版本和迁移管理；自定义角色通过受控接口管理，受当前角色管理限制约束 |
| 路由匹配、浏览器入口、调用 IAM、执行拒绝决定 | Gateway | 从固定路由策略确定操作，清除不可信身份头；客户端不能选择一个更宽松的 operationId |
| 真实 resourceTenant/Owner、资源状态、业务不变量 | 资源 owner | 用权威数据验证资源条件，保留执行、恢复和幂等；IAM 不接管 provider 或资源状态机 |
| 服务间直接 caller 与 delegated subject 检查 | IAM 提供固定契约/适配，各 receiver 执行 | 每个同步 hop 验证直接 Workload 身份与目标授权，并独立约束被服务主体；Gateway 身份不自动赋予用户管理员权限 |

职责划分与 [ADR-0009](adr/0009-generate-permissions-and-constrain-role-management.md)、[ADR-0022](adr/0022-unify-software-actors-as-workload-principals.md)及当前 spec 共同适用。

## 2. 三类记录及唯一来源

1. 本规范维护稳定规则和职责，不维护全量接口清单或当前完成状态。
2. 每个接口的实际认证/授权声明维护在 owner OpenAPI；共享错误、可信上下文和 obligation handler 定义可放在公共策略 manifest。公共 manifest 不重复维护每个接口权限。
3. JSON/Go operation registry 和 Permission 目录由契约生成；迁移核对清单引用这些固定输入，补充旧到新映射、迁移决定、接入状态和证据，不另造可编辑的权限真相。

本次 IAM 写入不移动 ANI 的 OpenAPI、不改现有生成器。跨仓同步由后续已授权事项执行，并固定源码、契约、生成器、registry 和角色定义版本/摘要。

## 3. 声明规则

| 字段/概念 | 要求 |
| --- | --- |
| operationId | 显式、在相应 API 契约内唯一；聚合时检查冲突。它是操作标识，与业务 Permission 分离。重命名须检查路由、委托目标、审计与调用方引用 |
| security | 使用 OpenAPI 标准认证描述；认证方式不等于业务权限。公开接口必须显式分类和声明，不能通过缺省字段获得公开访问 |
| x-ani-auth-classification | 区分 public、authenticated、authorized；登录、回调、Refresh 等特殊流程仍须由 IAM 按自身协议验证输入，public 不表示可跳过流程验证 |
| x-ani-authn | 分别声明允许的 Principal 和 Credential 类型。目标 Principal 仅 human/workload；凭据类型使用对应冻结契约，不把旧 service/service_token 自动当作新模型别名 |
| x-ani-authz | 受保护操作声明 version、resource、actions、scope、obligations；遵循现有生成契约，不凭文档新增运行时字段 |
| resource/actions | 表达稳定业务能力，可由多个接口复用；新增名称应经过 owner/IAM 评审并进入目录。不能依据字段相似就合并权限或扩大授权 |
| scope | own/tenant/platform 表达项目授权边界，不是 OAuth scope。own 的主体/所有者关系须按接口明确；tenant 必须有可信目标 Tenant；platform 不由空 Tenant 或 Tenant 管理员身份推导 |
| actions 组合 | 多动作的全部满足/任一满足语义必须与固定契约和实际 evaluator 一致，并有相应用例。未明确时记录待冻结，不能自行把列表解释成更宽松的 OR |
| obligations | 声明额外资源条件及负责检查的 handler；资源事实来自 owner。空数组不免除 IAM 的 Tenant 边界校验、owner 数据隔离和业务不变量 |

保留现有 YAML 结构和自动生成方式。普通接口声明所需权限，由角色组合这些权限；特定管理业务仍可有额外身份、内置角色、重认证或审批约束。

**管理限制不能被普通 Permission 检查替代。** ADR-0009 当前限定内置 `tenant-admin` 管理 Tenant custom Roles 和 member Role Bindings；委派角色管理不在首期范围。不能通过创建一个含类似权限的自定义角色绕过该限制。最后可登录管理员、恢复 requester/approver、重认证和审计继续遵循当前已接受契约；本规范不重定义其计数或恢复流程。

以下是既有 Tenant 角色创建声明的节选，用于说明结构，不表示该接口当前已经通过完整业务链验收：

```yaml
operationId: createTenantIAMRole
security: [{ BearerAuth: [] }]
x-ani-auth-classification: authorized
x-ani-authn:
  principal_kinds: [human]
  credential_kinds: [access_token]
x-ani-authz:
  version: v1
  resource: iam.roles
  actions: [create]
  scope: tenant
  obligations: []
```

## 4. 注册、执行与角色生效

- 生成阶段检查声明完整、标识冲突、合法类型、认证描述一致性、handler owner 和生成物漂移。使用现有工具；发现实际缺口后在负责事项内补齐，不为本文件另造一套生成系统。
- 接口覆盖检查必须对照正式路由/必需 RPC inventory，不能只统计已进入 registry 的接口，否则无法发现遗漏入口。
- 注册未知、策略版本不匹配、授权依赖失败时目标链拒绝；用户身份有效但无权限返回权限拒绝。沿用冻结的 401/403/503/504 等错误映射，不为“默认拒绝”统一改成同一个错误。
- 当前迁移期的显式 legacy 范围按固定清单追踪。新增受保护接口不得因为漏声明而意外进入旧链；选中目标链后不得因拒绝/故障回退旧 Auth。完整 M1 必需目标链不得依赖旧 Auth。
- Gateway 执行入口授权，receiver 验证直接 caller/委托，资源 owner 检查真实对象和状态；网络位置、客户端 `x-ani-*`、前端隐藏按钮均不能建立 Authority。
- 角色变更、撤权和内置角色升级按 ADR-0009 及当前冻结契约执行：普通角色变更影响下一次授权决定；内置定义由显式 schema/seed migration 版本化升级并审计。现有长连接终止时限由其独立契约定义，不能从“下一次决定”推导统一断连 SLA。
- 对缓存、策略装载或角色迁移的修改必须给出相容性、失效和恢复方案；不能仅更新 YAML 就宣称现有 Session 或部署已经采用新规则。

## 5. 整体切换核对清单

沿用 [替换能力矩阵](../.scratch/ani-iam-workload-refoundation/capability-matrix.md)作为 C01–C14 分母，使用 [WR17 冻结矩阵](../.scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts/rpc-matrix.md)追溯旧到新能力。历史 evidence 保持不变；在负责实施事项的新 evidence 中展开逐接口核对表，并由能力矩阵链接最新结果。

| 每个接口必须记录 | 来源/填写规则 |
| --- | --- |
| 旧 method/path/RPC → 目标 method/path/RPC/operationId | 从固定旧、目标契约和实际路由清单对照；一对多/多对一显式记录 |
| 保留语义/有意改变/退役及理由 | 依据接受的替换决定；未决填 not_frozen，不按旧 bug 推导目标正确性 |
| owner、实际 caller/receiver、入口链 | owner 契约及正式 composition root；标明 legacy/target 的精确范围 |
| 认证分类、主体/凭据、resource/actions/scope、obligations | 从固定声明提取，附 registry revision 和来源摘要；避免人工重复录入漂移 |
| 角色/Grant 映射、内置角色版本、旧绑定迁移 | IAM 固定目录与迁移方案；列出额外管理 guard，不由权限名自动推断 |
| 真实资源条件、执行位置、业务限制 | owner 实现与接受契约；包含列表查询的数据隔离，不能只验证单对象端点 |
| 声明、生成、接入、成功/拒绝/故障/撤权/恢复证据 | 每项分别记录 pass/fail/not_verified、证据层级和链接；不把已声明等同已接入 |
| 固定版本组合、负责事项、剩余缺口与恢复入口 | 与该事项 baseline 和环境 manifest 对应；版本变化检查受影响证据 |

按适用性验证普通用户拒绝、同 Tenant 管理员允许、跨 Tenant 拒绝、Tenant 管理员不能做 Platform 操作、错误 Principal/Credential 拒绝、角色或绑定撤销、伪造身份头、未知策略/版本、依赖不可用和资源 owner 条件。公开认证接口使用各自的认证流程用例，不机械要求管理员授权。高风险管理动作另验证最后管理员、越权授予和审批约束。

切换前先完成权限含义和角色映射，再生成、接线、验证。工具可以发现缺失和漂移，不能替业务负责人决定谁应获得权限。每个不适用用例须记录理由，不能用“暂未实现”从必需分母移除。

WR-21–24 按原范围补齐相应接口和 caller；WR-25 汇总完整 M1；WR-26–28 处理原计划中的旧代码、前端和实际消费者切换；WR-29 处理独立资产退役。本规范不领取这些事项，也不扩大现有部署/删除授权。

## 6. 标准依据与证据边界

- [OpenAPI 3.1.1：Operation Object](https://spec.openapis.org/oas/v3.1.1.html#operation-object)定义 operationId 和操作描述；[Specification Extensions](https://spec.openapis.org/oas/v3.1.1.html#specification-extensions)允许 x- 扩展。ANI 的字段语义、目录生成和团队分工属于项目约定，不声称是 OpenAPI 强制要求。
- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)建议设计阶段明确访问关系、最小权限、默认拒绝、逐请求检查与授权测试。

文档检查通过只证明本规范的完整性与链接一致性。实际接口、跨服务链、M1/M2 和生产证据仍由负责事项记录。
