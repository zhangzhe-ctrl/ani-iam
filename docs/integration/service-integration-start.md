# 普通服务接入 IAM：起点与检查项

本文提供当前源码的接入入口，供第一个通用服务试接。它不是该服务已经接入或通过验收的证明。具体服务的接口、资源归属、注册清单和运行凭据在接入事项中分别固定。

## 固定模块

使用Go 1.26.7（该组合已验证），在服务自己的模块目录中固定：

```sh
GOWORK=off go get github.com/zhangzhe-ctrl/ani-iam/api@v0.0.1-wr33.4 github.com/zhangzhe-ctrl/ani-iam/sdk@v0.0.1-wr33.4 github.com/zhangzhe-ctrl/ani-iam/workloadregistry@v0.0.1-wr33.4
go mod verify
```

上面命令以[交付记录](wr33-delivery.md)中相应远端tag已发布为前提。服务端代码不需要依赖IAM根模块，也不需要导入Governance/Notification或IAM internal。不要加入指向任务目录的replace、go.work或file GOPROXY。

## 先选调用语义

| 场景 | 入口与要求 |
| --- | --- |
| 纯服务到服务调用，没有Human主体 | `grpcworkload.NewWorkloadOnlyClient`、`RegisteredWorkloadTarget`、`WorkloadOnlyCallerInterceptor`；接收方在正式handler入口调用`VerifyCaller`并检查错误，再执行业务。不要虚构Human、Tenant Session或delegation。 |
| 服务代表已授权Human或API Key主体调用资源服务 | `grpcworkload.NewClient`、`Authorize`/`AuthorizeForReceiver`、`WithSubject`、`DialCaller`；接收方使用`ReceiverInterceptor`或`ReceiverInterceptorWithOwnerCheck`，从`VerifiedFromContext`取经验证的上下文。 |
| HTTP或长连接 | 分别阅读`sdk/grpcworkload/http.go`、`http_human.go`、`continuation.go`的合同，再在独立事项确定真实入口与持续授权策略；不能把gRPC unary结论直接继承过来。 |

## 接入方需提供的配置与业务事实

1. 服务owner声明精确RPC method（或HTTP方法/路径）、audience、operation及调用语义，按[API授权声明规则](../api-authorization-contract.md)形成审核过的有限目标注册；使用`workloadregistry.Load(path, expectedSHA256)`校验实际内容。注册声明只描述目标，不授予权限。
2. 调用方和接收方各自拥有独立Workload身份、证书/私钥、CA、environment和trust domain，以及IAM地址和正确TLS server name。用IAM正式管理接口配置相应Binding和最小Grant；不能共享IAM自身身份或从Header推断身份。
3. 调用方用SDK获取短期WAT及适用的delegation。接收方必须检查真实mTLS peer，并通过IAM在线复核当前身份、目标和权限；未注册、撤权、身份/目标不匹配或IAM不可用时拒绝，不添加本地放行或旧Auth回退。
4. 资源服务从自己的权威存储读取resourceTenant/owner并落实SDK给出的owner obligation；不能直接相信请求里的tenant_id。业务幂等、事务、资源执行和恢复仍由资源owner负责。
5. 密钥、刷新凭据及服务配置秘密由部署系统提供，不提交到Git。SDK的身份校验与健康检查不等于业务链验收。

## 第一个服务的验收清单

- 正确独立身份及精确Grant下，从实际调用进程到实际接收进程完成一个真实业务操作。
- 未注册接口、错误audience/operation、错误或缺失mTLS、撤销Binding/Grant均拒绝。
- 若存在Human/API Key主体，验证跨Tenant拒绝和owner资源归属检查；纯Workload路径不凭空添加Human上下文。
- IAM不可用时拒绝；恢复后允许；状态变化类请求的响应丢失不会绕过业务幂等或被SDK自动重放。
- 保存调用方/接收方/IAM的固定版本、实际配置摘要、允许和拒绝结果；标清未覆盖的HTTP/长连接及部署场景。

这份清单用于后续普通服务自己的接入事项；不要为试接改变当前J16观察Tenant、注册表、Grant、Pod或数据。
