# Typed Config 代码生成

## 固定工具与输入

```text
Buf CLI:         v1.72.0
Buf Linux x86_64 sha256:
                 8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af
plugin:          buf.build/protocolbuffers/go:v1.36.11
input:           internal/conf/conf.proto
input sha256:    8c024a2210a1f376a92e7ad93787ca893c54496f4fd6ec5fce8c880874140862
output:          internal/conf/conf.pb.go
output sha256:   2b28bb4dbb35a45f7c410f26ebce3fe17d5c763599cd5ed6d6d7bc396de13960
```

生成配置固定在本事项证据目录的 `buf.yaml` 与 `buf.gen.yaml`。`buf.yaml` 仅豁免内部 Kratos config 常见布局所需的 package directory/version 两条规则，其余使用 `STANDARD` lint。

## 实际命令

```bash
buf lint --config .scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/buf.yaml internal/conf/conf.proto
buf generate internal/conf/conf.proto --template .scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/buf.gen.yaml
```

lint 通过。生成前后 `conf.pb.go` 的 SHA-256 均为 `2b28bb4d...`，证明同一固定输入和插件复跑无差异；文件头记录 `protoc-gen-go v1.36.11`，生成文件没有手工修改。
