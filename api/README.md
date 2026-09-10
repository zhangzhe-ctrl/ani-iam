# IAM public gRPC contract

Module: `github.com/zhangzhe-ctrl/ani-iam/api`. WR-19 candidate version: `v0.1.0-rc.1`, not published. Existing `iam/v1` Go import paths stay unchanged. The module contains DTOs and generated clients, with no server implementation or storage dependency. Existing Go 1.25 callers can use it.

The Workload contract separates a directly authenticated caller from its delegated subject. The receiver reports its verified TLS peer and exact normalized business request binding to IAM under its own verification Grant. A decision or typed response grants no ambient authority for another request. Request binding, expiry and fail-closed behavior are fixed in the authoritative WR-19 invocation contract.

Generation is pinned to buf 1.72.0, protoc-gen-go 1.36.12 and protoc-gen-go-grpc 1.6.2. Regenerate from `.proto` sources; do not edit generated Go files. Independent release/versioning does not mean this candidate is published or production-ready. Tests use a recorded temporary workspace containing the exact module snapshots.
