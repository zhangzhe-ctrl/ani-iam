package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func runWorkloadRegistryInstaller(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("install-workload-registry", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("registry-file", "", "reviewed target declarations")
	approved := fs.String("approved-registry-sha256", "", "independently reviewed exact file SHA256")
	previous := fs.String("previous-registry-sha256", "", "exact current DB registration digest, or empty")
	dsnPath := fs.String("dsn-file", "", "private restricted migrator credential file")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return biz.ErrWorkloadBootstrapInvalid
	}
	registry, err := workloadregistry.Load(*path, *approved)
	if err != nil {
		return biz.ErrWorkloadBootstrapInvalid
	}
	secret, err := readBootstrapFile(*dsnPath, 16<<10, true)
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(string(secret)))
	if err != nil {
		return biz.ErrWorkloadBootstrapInvalid
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(call, cfg)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer pool.Close()
	if err := data.InstallWorkloadRegistry(call, data.NewData(pool), registry, *previous); err != nil {
		return err
	}
	revision, err := data.WorkloadPolicyRevision(registry)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		RegistrySHA256 string `json:"registry_sha256"`
		PolicyRevision string `json:"policy_revision"`
	}{registry.Digest(), revision})
}
