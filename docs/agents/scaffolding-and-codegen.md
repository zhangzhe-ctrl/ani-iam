# Scaffolding and Codegen

本文件规定框架 scaffold、Proto/OpenAPI、SDK、ORM 和其他官方生成物的初始化与变更方式。

## 强制原则

只要上游提供适用的官方生成器，就必须先用它生成基线，不得凭记忆手工仿制同名目录、入口或运行骨架。生成器只是起点；规格、已接受 ADR 和当前事项仍决定最终结构。

## 执行顺序

1. 在变更前确认当前事项为 `claimed`，且生成物、配置和证据路径都在 `Allowed paths` 内。
2. 查明并固定生成器版本、模板来源及不可变版本；不得使用未记录的 `latest`、动态分支或动态 `HEAD`。
3. 在临时目录运行官方生成命令，保留未裁剪基线用于比较，不直接覆盖已有工作树。
4. 对照当前规格和 ADR，逐项记录最终实现相对基线的 `保留/删除/替换`；没有依据的偏离先停止并请求决定。
5. 只将获准的结果移入仓库。生成文件不得手改；需要变化时修改输入或生成配置后重新生成。
6. 执行格式化、lint、可复现生成、构建、测试和与风险相称的安全检查，再更新事项结果。

## 必留证据

证据放在当前事项的 `Evidence path`，至少包含：

- 生成器名称和精确版本；
- 模板仓库、不可变提交或 Artifact 摘要；
- 实际生成命令和必要参数；
- 输入、生成配置及关键输出的校验和；
- 相对官方基线的 `保留/删除/替换` 清单及对应规格或 ADR；
- 重新生成无差异的验证结果，或所有预期差异的解释；
- 未运行或受环境限制的检查，明确标为 `not_verified`。

## IAM 当前固定生成输入

当前 WR-17/18 的工具/输入清单见 [isolation-manifest.json](../../.scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts/isolation-manifest.json)，精确输出清单见 [implementation-scope.json](../../.scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts/implementation-scope.json)。以下是 WR-18 领取后的实际命令约定；WR-17 只准备，不执行重任务。

- Go 1.26.7；Buf 1.72.0；protoc-gen-go v1.36.12；protoc-gen-go-grpc 1.6.2；sqlc 1.31.1；Atlas Community 1.3.0。可执行文件 SHA-256 必须核验。Buf 内置编译器承载 Proto 编译，不额外引入未固定的系统 protoc。
- Proto 输入：`api/iam/v1/{contract,authentication_service,authorization_service,iam_admin_service}.proto`，同目录的 buf.yaml/buf.gen.yaml/buf.lock；googleapis module commit/digest 由 buf.lock 固定。
- 在隔离源副本的 `api/iam/v1` 执行 `buf generate . --template buf.gen.yaml`；然后 `buf build . --as-file-descriptor-set --exclude-source-info -o iam_descriptor.pb`。
- 配置输入 `internal/conf/conf.proto`；在隔离输出目录使用 Buf v2 inline template、local protoc-gen-go、`paths=source_relative`，只生成 `internal/conf/conf.pb.go`。执行脚本须记录完整 template 与工作目录；不要使用不存在的 Makefile 或 Wire。
- registry 输入是由 WR-17 固定的 ANI 公共 source blob 及 WR-18 明确变换后的 IAM 目标候选 JSON；用仓库自己的 `go run ./internal/data/cmd/genoperationregistry -input <target.json> -expected-sha256 <target-sha> -go-output internal/data/generated_operation_policies.go -sql-output migrations/202609080002_permission_catalog.sql`。原 ANI 源摘要与新候选摘要分别记录，不静默映射旧 service 值。
- 从隔离源根执行 `sqlc generate -f sqlc.yaml`，输入 migrations/*.sql 与 internal/data/queries/persistence.sql；然后 `ATLAS_NO_UPDATE_NOTIFIER=1 atlas migrate hash --dir file://migrations` 和 `atlas migrate validate --dir file://migrations`。
- 相同输入在第二个干净临时输出目录重复以上生成，逐文件比对；远端产物先回传临时目录，校验本地输入未变后合入产品 worktree。禁止手改 *.pb.go、sqlcgen、generated_operation_policies.go 或 atlas.sum。

## 停止条件

出现下列任一情况不得自行绕过，也不得把事项标为 `resolved`：

- 官方生成器、模板版本或许可证无法确认；
- 生成器输出超出事项允许范围；
- 官方基线与规格或 ADR 冲突但没有明确取舍依据；
- 生成文件只能靠直接手改才能通过；
- 无法复现生成结果，或实际验证尚未完成却准备写成通过。

如果官方生成器确实不可用，应先把原因、替代方案和迁移成本写入事项，并取得用户明确决定后再采用替代实现。
