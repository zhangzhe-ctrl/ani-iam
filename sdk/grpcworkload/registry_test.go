package grpcworkload

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"os"
	"testing"
)

func registryFixture(t *testing.T) *workloadregistry.Registry {
	t.Helper()
	raw, err := os.ReadFile("testdata/workload-targets.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	r, err := workloadregistry.Parse(raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
