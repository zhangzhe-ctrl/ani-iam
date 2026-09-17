//go:build integration

package integration_test

import (
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

// Historical integration fixtures explicitly use their frozen generated policy.
// Deployment assembly passes its independently validated owner registry instead.
func defaultPolicyAuthenticationReader(database *data.Data) biz.AuthenticationReader {
	registry, err := data.NewTargetOperationRegistry(data.TargetPolicyRevision)
	if err != nil {
		panic(err)
	}
	return data.NewPostgresPasswordLoginReader(database, registry)
}
