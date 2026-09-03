# 冻结 Auth descriptor

## 来源

- ANI Commit：`963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`
- Auth source：`api/proto/auth/v1/auth_service.proto`
- Common source：`api/proto/common/v1/common.proto`
- 本地隔离副本：`internal/compat/authv1/proto/**`

本地 Auth source SHA-256 为 `aabcc72b10bd2b89591eaf706b4cf2659b98a8b5b4e3dbf92b3e387938bc33ec`，与事项01冻结值一致。测试还固定去除 source info 后的 `auth/v1/auth_service.proto` `FileDescriptorProto` SHA-256：`39db0d6c1a937221afd0b91a6903c5fe1515abff37b41b26069cb57b0ee44248`。

## 独立 descriptor diff

使用 Buf `1.60.0` 分别从固定 Git object 与本地 compat source 构建 `FileDescriptorSet`，两边均排除 source info 后进行二进制比较：

```text
baseline.binpb  sha256 64f73f4c6934a98ad63d29aa162f7ff5d729270f04d3af2eeb61cc41c1ddbcb0
current.binpb   sha256 64f73f4c6934a98ad63d29aa162f7ff5d729270f04d3af2eeb61cc41c1ddbcb0
cmp result: pass
```

复现时必须从 Git object 读取，不得读取 ANI 工作树：

```bash
tmp_dir=$(mktemp -d /tmp/ani-iam-issue03-descriptor.XXXXXX)
git -C ../ANI/repo archive 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be -- ./api/proto/auth/v1/auth_service.proto ./api/proto/common/v1/common.proto | tar -x -C "$tmp_dir"
buf build "$tmp_dir/api/proto" --as-file-descriptor-set --exclude-source-info -o "$tmp_dir/baseline.binpb"
buf build internal/compat/authv1/proto --as-file-descriptor-set --exclude-source-info -o "$tmp_dir/current.binpb"
sha256sum "$tmp_dir/baseline.binpb" "$tmp_dir/current.binpb"
cmp "$tmp_dir/baseline.binpb" "$tmp_dir/current.binpb"
```

## 生成

- Buf CLI：`1.60.0`
- `protoc-gen-go`：`v1.36.11`
- `protoc-gen-go-grpc`：`v1.6.2`
- 配置：`internal/compat/authv1/buf.yaml`、`buf.gen.yaml`

生成后的摘要：

```text
19bcf99d2482475128bcd5854c983837ad1627c7e69dd558a0544724b1394a4c  auth_service.pb.go
80e95a4922d194d9cb22a31c3624810c9e0193e1c2f19f013b1e02025b608eca  auth_service_grpc.pb.go
36263264440e54734bf64820318f088e76a236e0718e4dd5142815bee8f4164d  commonv1/common.pb.go
```

旧契约不满足 Buf 的 response 命名、request/response unique 和 unused import 规则。`buf.yaml` 只对这四类会要求改写冻结 wire/source 的规则作显式例外，其余 `STANDARD` lint 继续执行。
