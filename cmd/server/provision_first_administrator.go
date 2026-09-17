package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func runFirstAdministratorProvisioner(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("provision-first-administrator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifestPath := fs.String("manifest", "", "reviewed non-secret intent manifest")
	approved := fs.String("approved-manifest-sha256", "", "independently approved exact manifest SHA-256")
	environment := fs.String("environment", "", "owner-controlled environment")
	dsnPath := fs.String("dsn-file", "", "private provisioner credential file")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return biz.ErrFirstAdministratorInvalid
	}
	m, dsn, err := readFirstAdministratorFiles(*manifestPath, *approved, *environment, *dsnPath, time.Now())
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return biz.ErrFirstAdministratorDenied
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	defer pool.Close()
	operationCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	u := biz.NewFirstAdministratorUsecase(data.NewFirstAdministratorRepository(data.NewData(pool)), data.NewSystemClock())
	receipt, err := u.Register(operationCtx, *environment, m)
	if err != nil {
		return err
	}
	if json.NewEncoder(out).Encode(receipt) != nil {
		return errors.New("administrator intent receipt could not be returned; retry the same reviewed manifest")
	}
	return nil
}

func readFirstAdministratorFiles(manifestPath, approved, environment, dsnPath string, now time.Time) (biz.FirstAdministratorManifest, string, error) {
	invalid := func() (biz.FirstAdministratorManifest, string, error) {
		return biz.FirstAdministratorManifest{}, "", biz.ErrFirstAdministratorInvalid
	}
	if len(approved) != 64 || approved != strings.ToLower(approved) {
		return invalid()
	}
	raw, err := readBootstrapFile(manifestPath, 16<<10, false)
	if err != nil {
		return invalid()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != approved {
		return biz.FirstAdministratorManifest{}, "", biz.ErrFirstAdministratorDenied
	}
	var m biz.FirstAdministratorManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil {
		return invalid()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid()
	}
	if _, err = biz.ValidateFirstAdministrator(environment, m, now); err != nil {
		return biz.FirstAdministratorManifest{}, "", err
	}
	secret, err := readBootstrapFile(dsnPath, 16<<10, true)
	if err != nil || len(bytes.TrimSpace(secret)) == 0 {
		return invalid()
	}
	return m, strings.TrimSpace(string(secret)), nil
}
