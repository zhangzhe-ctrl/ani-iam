package data

import "github.com/zhangzhe-ctrl/ani-iam/internal/runtimeendpoint"

// ValidDeploymentAddress validates routing only; TLS and target authority are independent.
func ValidDeploymentAddress(address string) bool { return runtimeendpoint.ValidAddress(address) }
func ValidHTTPSOrigin(origin string) bool        { return runtimeendpoint.ValidHTTPSOrigin(origin) }
