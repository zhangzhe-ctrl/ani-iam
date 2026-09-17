module github.com/zhangzhe-ctrl/ani-iam/examples/workload-grpc

go 1.25.0

require (
	github.com/zhangzhe-ctrl/ani-iam/sdk v0.0.0-20260911071951-e9f657f20b69
	github.com/zhangzhe-ctrl/ani-session-gateway/api v0.1.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/zhangzhe-ctrl/ani-iam/api v0.0.0-20260911071802-b9fde01ae781 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60 // indirect
)

// WR32: resolved only by the fixed local candidate module mapping.
require github.com/zhangzhe-ctrl/ani-iam/workloadregistry v0.0.0
