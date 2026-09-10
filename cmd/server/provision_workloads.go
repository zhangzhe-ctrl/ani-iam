package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

type bootstrapFiles struct {
	manifest biz.WorkloadBootstrapManifest
	owner    biz.WorkloadBootstrapOwner
	dsn      string
}

func runWorkloadProvisioner(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("provision-workloads", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "reviewed non-secret manifest path")
	approved := fs.String("approved-manifest-sha256", "", "independently approved exact manifest SHA-256")
	env := fs.String("environment", "", "owner-controlled environment")
	trust := fs.String("trust-domain", "", "owner-controlled trust domain")
	ca := fs.String("ca-file", "", "owner-controlled trust anchor certificate")
	dsnFile := fs.String("dsn-file", "", "private provisioner credential file")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return biz.ErrWorkloadBootstrapInvalid
	}
	inputs, err := readBootstrapFiles(*manifest, *approved, *env, *trust, *ca, *dsnFile, time.Now())
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(inputs.dsn)
	if err != nil {
		return biz.ErrWorkloadBootstrapDenied
	}
	// This administrative CLI is bounded and never logs connection details.
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
	u := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(pool)), data.NewSystemClock())
	receipt, err := u.Provision(operationCtx, inputs.owner, inputs.manifest)
	if err != nil {
		return err
	}
	if json.NewEncoder(out).Encode(receipt) != nil {
		return errors.New("bootstrap receipt could not be returned; retry the same reviewed manifest")
	}
	return nil
}

func readBootstrapFiles(manifestPath, approved, env, trust, caPath, dsnPath string, now time.Time) (bootstrapFiles, error) {
	invalid := func() (bootstrapFiles, error) { return bootstrapFiles{}, biz.ErrWorkloadBootstrapInvalid }
	if len(approved) != 64 || approved != strings.ToLower(approved) {
		return invalid()
	}
	raw, err := readBootstrapFile(manifestPath, 128<<10, false)
	if err != nil {
		return invalid()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != approved {
		return bootstrapFiles{}, biz.ErrWorkloadBootstrapDenied
	}
	var manifest biz.WorkloadBootstrapManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil {
		return invalid()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid()
	}
	certRaw, err := readBootstrapFile(caPath, 32<<10, false)
	if err != nil {
		return invalid()
	}
	block, rest := pem.Decode(certRaw)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return invalid()
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return invalid()
	}
	caDigest := sha256.Sum256(cert.Raw)
	owner := biz.WorkloadBootstrapOwner{Environment: env, TrustDomain: trust, CASHA256: hex.EncodeToString(caDigest[:])}
	if _, err = biz.ValidateWorkloadBootstrap(owner, manifest, now); err != nil {
		return bootstrapFiles{}, err
	}
	secret, err := readBootstrapFile(dsnPath, 16<<10, true)
	if err != nil || len(bytes.TrimSpace(secret)) == 0 {
		return invalid()
	}
	return bootstrapFiles{manifest: manifest, owner: owner, dsn: strings.TrimSpace(string(secret))}, nil
}

func readBootstrapFile(path string, limit int64, private bool) ([]byte, error) {
	if path == "" {
		return nil, biz.ErrWorkloadBootstrapInvalid
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || (private && info.Mode().Perm()&0o077 != 0) {
		return nil, biz.ErrWorkloadBootstrapInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, biz.ErrWorkloadBootstrapInvalid
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, biz.ErrWorkloadBootstrapInvalid
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, biz.ErrWorkloadBootstrapInvalid
	}
	return b, nil
}
