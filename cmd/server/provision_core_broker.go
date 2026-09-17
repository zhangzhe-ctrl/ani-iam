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
)

func runCoreBrokerProvisioner(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("provision-tenant-broker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "reviewed non-secret broker configuration or authorization manifest")
	approved := fs.String("approved-manifest-sha256", "", "independently reviewed exact bytes")
	environment := fs.String("environment", "", "owner-controlled environment")
	trust := fs.String("trust-domain", "", "owner-controlled trust domain")
	dsnFile := fs.String("dsn-file", "", "private restricted provisioner credential")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return biz.ErrCoreBrokerAuthority
	}
	raw, err := readBootstrapFile(*manifest, 128<<10, false)
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	secret, err := readBootstrapFile(*dsnFile, 16<<10, true)
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	defer clear(secret)
	cfg, err := pgxpool.ParseConfig(strings.TrimSpace(string(secret)))
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(call, cfg)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer pool.Close()
	receipt, err := data.ApplyCoreBrokerManifest(call, data.NewData(pool), raw, *approved, *environment, *trust, time.Now().UTC())
	if err != nil {
		return err
	}
	if json.NewEncoder(out).Encode(receipt) != nil {
		return biz.ErrPersistenceUnavailable
	}
	return nil
}
