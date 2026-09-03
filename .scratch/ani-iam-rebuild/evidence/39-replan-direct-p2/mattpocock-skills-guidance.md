# mattpocock/skills 对方向变更后重排规格与事项的指导

## 研究边界与固定版本

- 仅检查官方仓库：[`mattpocock/skills`](https://github.com/mattpocock/skills)。
- 固定 revision：[`6654f6b60cd9d5be8b54c6fafe44346dabeb3b76`](https://github.com/mattpocock/skills/tree/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76)。
- 本地以 detached HEAD 读取该 revision；`git ls-remote ... HEAD` 与本地 `git rev-parse HEAD` 一致。
- 未安装任何 skill，也未读取第三方派生实现。

## 结论

上游有与本次“从 CP0/P1 路线改为 Direct P2”相近的指导，但没有一个名为“重排计划”或“supersede tickets”的完整标准流程。最接近的组合是：

1. 如果新目标仍有跨会话的重大决策迷雾，先用 `wayfinder` 重画 destination 和决策路线；如果当前对话已经完成决定，则跳过 `wayfinder`。
2. 用 `to-spec` 把已经决定的新方向综合成新的可执行规格。
3. 用 `to-tickets` 从该规格重新生成经过人工确认的纵向 tracer-bullet tickets。
4. `triage` 不属于这条主链；`to-tickets` 生成的 tickets 按上游定义已经是 `ready-for-agent`，不应再逐项 triage。

对旧材料，上游明确倾向“保留 spec、删除未完成 tickets”；同时 `wayfinder` 允许在新决定使旧票失效时更新或删除受影响 tickets。由于本仓库要求审计链且本地 Markdown 删除会丢失历史，适用于本仓库的推断是：**不覆写旧规格，不物理删除旧事项；保留为历史快照，将未执行且已被新路线替代的旧事项关闭并显式链接新事项，再建立一套新的 Direct P2 spec 和 tickets。**

## 上游明确指导

### 1. 方向改变后，unfinished tickets 是可丢弃的，spec 应保留

官方 `to-spec` 文档直接写明：发生 pivot 时，删除未完成 tickets 并保留 spec；随后又说明 tickets 是 disposable，而 spec 是保留原始决策理由的记录：

- [`docs/engineering/to-spec.md` L38-L45](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/to-spec.md#L38-L45)

这回答了“是否原地重写旧 tickets”：上游倾向不保留失效的未完成执行切片，而不是勉强修改它们继续执行。

但上游同时说明 spec 没有自动同步机制；实现带来新认知后它会变旧，真正需要长期保留的知识应进入 `CONTEXT.md` 和 ADR，而不是持续编辑 spec：

- [`docs/engineering/to-spec.md` L53-L57](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/to-spec.md#L53-L57)

因此，上游所说的“保留 spec”更像保留决策快照和来源记录，不等于要求把旧 spec 永久维护成最新执行权威。

### 2. Destination 改变时，应把失效内容移出当前路线

`wayfinder` 规定 destination 决定 scope。落在新 destination 之外的已存在 ticket 应关闭，并在 Out of scope 中记录理由；如果 destination 被重新绘制，原先排除的工作应作为 fresh effort 返回，而不是恢复旧路线：

- [`skills/engineering/wayfinder/SKILL.md` L95-L101](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/wayfinder/SKILL.md#L95-L101)

同一技能还明确要求：一个决定使 map 的其他部分失效时，更新或删除相关 tickets：

- [`skills/engineering/wayfinder/SKILL.md` L118-L127](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/wayfinder/SKILL.md#L118-L127)

不过，官方说明页对“已关闭决定后来证明错误”明确承认没有正式规则；当前实践是更新 map、修订受影响的开放 tickets，并在已关闭 tickets 上追加说明：

- [`docs/engineering/wayfinder.md` L83-L84](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/wayfinder.md#L83-L84)

所以不能把“旧事项必须标成 superseded”说成上游明文要求；上游甚至没有 `superseded` 状态。

### 3. `wayfinder`、`to-spec`、`to-tickets` 的衔接

`wayfinder` 适用于 destination 可命名、但通往 destination 的路线仍有多会话决策迷雾的工作。它只做规划和决定，不实施；当 map 清空后才交给 `to-spec`：

- [`skills/engineering/wayfinder/SKILL.md` L7-L13](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/wayfinder/SKILL.md#L7-L13)
- [`docs/engineering/wayfinder.md` L13-L19](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/wayfinder.md#L13-L19)

`to-spec` 不重新访谈，而是综合当前对话中已经做出的决定；如果 wayfinder 已清空，输入主 map，而不是单张决策票：

- [`skills/engineering/to-spec/SKILL.md` L7-L19](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/to-spec/SKILL.md#L7-L19)
- [`docs/engineering/to-spec.md` L47-L48](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/to-spec.md#L47-L48)

`to-tickets` 再把 plan/spec/conversation 切成纵向 tracer-bullet tickets；每张票必须是跨 schema、API、UI、tests 的窄而完整路径，可独立演示或验证，并且适合一个 fresh context：

- [`skills/engineering/to-tickets/SKILL.md` L25-L40](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/to-tickets/SKILL.md#L25-L40)

在发布前，`to-tickets` 必须把拆分、依赖边和交付行为交给用户确认；用户批准后才发布：

- [`skills/engineering/to-tickets/SKILL.md` L42-L63](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/to-tickets/SKILL.md#L42-L63)

上游主链因此是：

```text
wayfinder（仅在仍有多会话决策迷雾时）
  → to-spec
  → to-tickets
  → implement
  → code-review
```

官方 `to-spec` 文档也给出主链：

- [`docs/engineering/to-spec.md` L73-L81](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/to-spec.md#L73-L81)

### 4. `triage` 是另一条入口，不是生成事项后的附加步骤

`triage` 的五个标准 state roles 是：

- `needs-triage`：维护者需要评估；
- `needs-info`：等待报告者补充信息；
- `ready-for-agent`：规格完整，可由 AFK agent 获取；
- `ready-for-human`：需要人类实施；
- `wontfix`：不会执行并关闭。

每个 triaged issue 必须恰好有一个 state role；标准转换从 `needs-triage` 进入其余四个终态/等待态，维护者可以覆盖：

- [`skills/engineering/triage/SKILL.md` L24-L45](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/triage/SKILL.md#L24-L45)

`to-tickets` 生成的票在人工批准拆分之后，按定义直接带 `ready-for-agent`，并明确不得修改或关闭 parent issue：

- [`skills/engineering/to-tickets/SKILL.md` L58-L67](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/skills/engineering/to-tickets/SKILL.md#L58-L67)

官方说明进一步明确：不要对 `to-tickets` 生成的 tickets 再运行 triage；triage 是外部/inbound work 的入口，两条路线在 `ready-for-agent` 汇合：

- [`docs/engineering/to-tickets.md` L17-L23](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/to-tickets.md#L17-L23)
- [`docs/engineering/triage.md` L65-L71](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/triage.md#L65-L71)

上游也承认五状态缺少 `blocked`、`deferred`、`implemented` 等角色，目前尚未发布官方解决方案：

- [`docs/engineering/triage.md` L73-L77](https://github.com/mattpocock/skills/blob/6654f6b60cd9d5be8b54c6fafe44346dabeb3b76/docs/engineering/triage.md#L73-L77)

因此不能把 `needs-info` 当依赖阻塞态，也不能声称上游存在 `superseded` 状态。

## 对 ani-iam 的适用推断（不是上游明文）

### 1. 旧 spec、计划与事项

建议：

- 保留现有 CP0/P1/P2 spec、计划和事项作为旧方向的历史快照，不原地改写成 Direct P2，好让旧决策、事项03结果和事项04负向调查保持可追溯。
- 事项04已经完成其“真实依赖调查并得出 No-Go”的工作，应以本仓库的完成语义关闭，并保持 `FAIL / No-Go`；它不是 `wontfix`，因为调查本身已经执行完成。
- 尚未执行且被新路线替代的旧 tickets 不继续改写为新任务。上游在可删除的 tracker 中会删除这些 unfinished tickets；本仓库为了审计，应关闭它们，并写明“由 Direct P2 的某事项继承”，而不是物理删除。
- 旧需求并没有被拒绝，只是执行切片被替换，所以不应把这些概念写进 `.out-of-scope/`。若本地五状态必须选一个，`wontfix` 是最接近“此旧票不会执行”的状态，但关闭说明必须写明 `superseded by <new ticket>`，避免被理解成需求取消。这是本仓库对上游状态缺口的映射，不是官方规定。

### 2. 是否重新生成 tickets

建议重新生成，而不是对旧 16–38 做大规模原地改名和改依赖，理由是：

- destination 已从“CP0 → P1 → P2”变成“Direct P2，并提前验证切换可行性”；
- ticket 顺序和切片边界已经发生结构性变化；
- 上游将 tickets 视为 disposable execution slices，并允许失效票被更新或删除；
- 新 tickets 应按纵向、可演示、一个 fresh context 可完成的标准重新切分，尤其把“最小纵向架构证明”和“第一次整组切换/回退”作为早期 tracer bullets，而不是只按数据库、契约、服务层横向拆分。

为避免范围丢失，在关闭旧票前应建立一张旧验收条件到新票验收条件的完整映射；这是本仓库审计要求，不是上游 `to-tickets` 自带能力。

### 3. 新票的状态

严格按上游流程有两种一致做法：

1. 先只提交拆分草案，不创建 ticket 文件；人工批准粒度和依赖后，再发布为 `ready-for-agent`。
2. 如果当前用户对所展示的拆分、依赖和交付内容已经明确批准，则直接创建为 `ready-for-agent`，不再走 `needs-triage`。

如果本仓库选择“先创建所有新票，但统一标 `needs-triage` 等待审计”，这是有意采用的本地治理扩展，不能归因于 mattpocock/skills。上游更偏好“批准前不发布，批准后直接 ready-for-agent”。

即使新票是 `ready-for-agent`，也不等于所有票都可立即领取；应继续用 `Blocked by` 表达依赖，只领取 blockers 已完成的 frontier。上游承认 `ready-for-agent` 与被依赖阻塞之间存在状态表达缺口，因此执行器必须同时检查依赖边。

### 4. 这次是否需要先建 `wayfinder`

若以下问题仍未定，而且每个问题需要独立会话才能澄清，应该先建新的 Direct P2 wayfinder map：

- 第一次整组切换必须覆盖的现有调用能力集合；
- 可接受的数据重建、credential 失效和回退边界；
- 哪些完整 P2 功能可以放到第一次切换证明之后；
- ANI 最新 main 的契约漂移哪些被接受、哪些必须补齐。

如果这些已经由当前对话和现有计划明确决定，则按上游路由应跳过 wayfinder，直接生成新 spec，再由 `to-tickets` 拆分。`wayfinder` 不应被当成额外审批层，也不应在 map 中实施产品代码。

## 建议的本仓库操作顺序

以下顺序是基于上游规则和本仓库审计约束的组合推断：

1. 关闭事项04，保留 `FAIL / No-Go` 和证据，不改写成 PASS。
2. 保留旧 spec/计划/事项为历史，不物理删除。
3. 将 Direct P2 新 destination 与仍未决定的问题分开；只有存在真正跨会话 fog 才建立新 wayfinder map。
4. 从已决定的对话和必要的已清空 map 生成一份新的 Direct P2 spec。
5. 先展示纵向 tickets、交付行为和 blocking edges，取得人工批准。
6. 发布新 tickets 为 `ready-for-agent`，但仅 frontier 可被 claim。
7. 建立旧验收条件到新 tickets 的覆盖映射。
8. 映射审计通过后，关闭不会再执行的旧票，逐票链接替代它的新票；不要把继续存在的新目标写入 out-of-scope。
9. 停止，等待用户批准领取 Direct P2 的第一张实施票；计划完成本身不自动授权编码。

## 适用时的风险提示

- `to-spec` 明确要求在写 spec 前与用户确认测试 seams；不能因为方向已决定而跳过这个检查。
- `to-tickets` 要求发布前由用户确认粒度、阻塞边和拆分/合并；“自动生成后直接全量执行”不符合上游流程。
- `ready-for-agent` 是规格就绪状态，不是忽略 blockers 的执行授权。
- `wontfix` 的上游语义是“不会执行”，不是“需求由另一个 ticket 继续”；用于 supersession 时必须附加本仓库解释和反向链接。
- 已关闭决定如何更改，上游明确没有统一规则；本仓库必须依靠历史保留、追加说明和新旧映射维持审计性。
