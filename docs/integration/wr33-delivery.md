# IAM/Governance 接入候选交付

本候选供普通服务接入IAM时引用公开合同与SDK。它不表示Auth完整替换完成：联合验收目前15/16通过，J16真实24小时观察和独立复核待完成；随后还有WR24及WR25的M1验收。

## 普通服务消费

| 模块 | 固定版本 | 用途 |
| --- | --- | --- |
| `github.com/zhangzhe-ctrl/ani-iam/api` | `v0.0.1-wr33.4` | 公开Proto和gRPC客户端 |
| `github.com/zhangzhe-ctrl/ani-iam/sdk` | `v0.0.1-wr33.4` | 真实mTLS、当前在线授权及调用方/接收方适配 |
| `github.com/zhangzhe-ctrl/ani-iam/workloadregistry` | `v0.0.1-wr33.4` | 校验有限目标注册及其内容摘要 |

三个目录保持原冻结contracts-r4的全部字节和文件模式，不因发布修订同版本的内容。模块内README/go.mod中关于“unpublished”的旧注释是冻结当时记录；实际可获取状态以Git远端tag和下述发布核验为准。

接入步骤从[普通服务接入起点](service-integration-start.md)开始。消费方不需要IAM根模块或Governance/Notification模块。

## IAM 服务端依赖边界

原candidate-r5固定依赖Gov API `v0.0.0-gov0103.4`及Notification `v0.0.1-wr33.1`，此前通过可校验的私有file GOPROXY构建和真实联合验收。它们不是已经发布的远端tag。

Governance已经公开`aaaa4b7300fec15e5ebde205fcfbbc822c647b68`源码分支；API九个文件与原冻结zip仅README文案不同，不能把此提交直接冒充原`gov0103.4`内容。Notification公开main `25e51e21339f03d4d60d28c03fdb55eeb92ce89d`缺少本次身份邮件/Proto等真实变更，不能用旧main顶替原固定候选。本次不越权写这两个仓库，IAM服务端的全远端依赖消费仍须独立处理；普通服务的三个IAM子模块不受这一服务端构建缺口影响。

## 来源与已有验证

- IAM Git基线`6e9688bb002d1916bc894fd281ba1974f57b7eaa`；candidate-r5源码归档SHA256 `f664c09a4bdb502fd547a0ca24803071ce815fe90969ddf9e9e70207e6e1b293`，777个文件。
- contracts-r4 manifest SHA256 `54d39239cdb638b0265841f960b6cd33edca0183c03d291f0edbc3d71ef574a3`；IAM运行二进制SHA256 `2c6027e488aaeb7104a4dccf2217971ca86aa5726db5baddeeaacd3dc9fdce7c`。
- 固定生成、根unit/race/vet、真实PostgreSQL权限/事务检查、文件proxy下无replace的干净消费和可重复构建已有pass。文件proxy消费不冒充Git远端模块消费。
- 本发布副本只更新根README及增加这两份接入/交付说明；所有Go产品代码、迁移和公开子模块保持冻结内容。历史`.scratch`记录保持基线，当前交付状态以本文件为准。
- J16观察使用原候选和原运行输入，不受Git提交或发布文档影响。新普通服务应使用独立接入环境。

## 发布核验

准备阶段：远端tag与全新网络消费尚待执行，不提前记pass。精确提交、tag和网络消费结果将在发布完成后追加。
