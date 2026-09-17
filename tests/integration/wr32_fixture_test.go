//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func wr32RegistryPath(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("WR23_DLQ_REVIEW_REGISTRY"); path != "" {
		run, goal := isolatedRun(t)
		if goal != "wr23" || filepath.Clean(path) != filepath.Join(run, "private", "dlq-review-registry.json") {
			t.Fatal("DLQ review registry escaped its own WR23 run")
		}
		return path
	}
	return filepath.Join(findRepositoryRoot(t), "registrations/workload-targets.v1.json")
}

// Execute the exact preserved migrations before WR32, then apply later ones.
// This gives a differential role contract without replacing the dirty baseline.
func wr32HistoricalMigrations(t *testing.T, source, atlas string) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "migrations")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	names, err := filepath.Glob(filepath.Join(source, "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "202609140001_workload_target_registry.sql" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, filepath.Base(name)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(atlas, "migrate", "hash", "--dir", "file://"+directory)
	cmd.Env = append(os.Environ(), "ATLAS_NO_UPDATE_NOTIFIER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hash preserved migrations: %v: %s", err, output)
	}
	return root
}

func wr32RuntimePrivileges(t *testing.T, pool *pgxpool.Pool) map[string]struct{} {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT table_name,privilege_type FROM information_schema.role_table_grants WHERE grantee=current_user AND table_schema='public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[string]struct{}{}
	for rows.Next() {
		var table, privilege string
		if err := rows.Scan(&table, &privilege); err != nil {
			t.Fatal(err)
		}
		result[table+"/"+privilege] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func wr32Registry(t *testing.T) *workloadregistry.Registry {
	t.Helper()
	raw, err := os.ReadFile(wr32RegistryPath(t))
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
