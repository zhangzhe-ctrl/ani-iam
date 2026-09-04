package data_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestUUIDv7Generator(t *testing.T) {
	t.Parallel()

	generator := data.NewUUIDv7Generator()
	seen := make(map[uuid.UUID]struct{}, 128)
	for range 128 {
		id, err := generator.NewID()
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if id == uuid.Nil || id.Version() != 7 {
			t.Fatalf("NewID() = %s (version %d), want non-zero UUIDv7", id, id.Version())
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("NewID() produced duplicate %s", id)
		}
		seen[id] = struct{}{}
	}
}
