package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/protobuf/encoding/protojson"
)

// This finite maintenance entry opens one owner-issued durable cut. The
// formal coordinator retains responsibility for pages, catch-up and activation.
// No broker consumer, Human delegation, Bootstrap retry or enforcement switch
// is constructed by this command.
func runCoreSnapshotBegin(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("begin-tenant-snapshot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("config-file", "", "exact runtime JSON")
	approved := fs.String("approved-config-sha256", "", "reviewed runtime bytes")
	key := fs.String("request-key", "", "stable owner Snapshot idempotency key")
	size := fs.Int("page-size", 100, "bounded Snapshot page size")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *size < 1 || *size > 100 || *key == "" {
		return biz.ErrCoreProjectionInvalid
	}
	raw, err := readBootstrapFile(*file, 1<<20, true)
	if err != nil {
		return biz.ErrCoreProjectionInvalid
	}
	defer clear(raw)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != *approved {
		return biz.ErrCoreProjectionInvalid
	}
	var configuration conf.Bootstrap
	if protojson.Unmarshal(raw, &configuration) != nil || configuration.Runtime == nil || configuration.Runtime.Postgresql == nil {
		return biz.ErrCoreProjectionInvalid
	}
	runtime := configuration.Runtime
	c, err := readCoreLifecycleConfiguration(runtime)
	if err != nil || c == nil {
		return biz.ErrCoreProjectionInvalid
	}
	registry, err := workloadregistry.Load(runtime.WorkloadRegistryFile, runtime.WorkloadRegistrySha256)
	if err != nil {
		return biz.ErrCoreProjectionInvalid
	}
	cfg, err := pgxpool.ParseConfig(runtime.Postgresql.Dsn)
	if err != nil {
		return biz.ErrPersistenceUnavailable
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
	d := data.NewData(pool)
	if data.ValidateRuntimeFoundation(call, d) != nil {
		return biz.ErrPersistenceUnavailable
	}
	files := grpcworkload.TLSFiles{CertificateFile: c.Snapshot.CertificateFile, PrivateKeyFile: c.Snapshot.PrivateKeyFile, CAFile: c.Snapshot.CAFile}
	a := c.Broker.Authority
	identity, err := grpcworkload.NewWorkloadOnlyClient(grpcworkload.ClientConfig{Registry: registry, Address: c.Snapshot.IAMAddress, ServerName: c.Snapshot.IAMServerName, Environment: a.Environment, TrustDomain: a.TrustDomain, TLS: files, Timeout: 2 * time.Second})
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	defer identity.Close()
	source, closeHTTP, err := data.NewGovernanceSnapshotHTTPClient(data.GovernanceSnapshotHTTPConfiguration{Origin: c.Snapshot.Origin, ServerName: c.Snapshot.ServerName, ReaderID: c.Snapshot.ReaderID, TLS: files, Registry: registry, TokenSource: identity.HTTPTokenSource})
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	defer closeHTTP()
	readerDNS, err := snapshotReaderIdentity(c.Snapshot.CertificateFile)
	if err != nil {
		return err
	}
	repository, err := data.NewGovernanceSnapshotRecoveryRepository(d, data.GovernanceSnapshotRecoveryConfiguration{Broker: a, ReaderID: c.Snapshot.ReaderID, ReaderIdentity: readerDNS, Registry: registry})
	if err != nil {
		return err
	}
	cursor, err := source.Begin(call, *key, *size)
	if err != nil {
		return err
	}
	build, err := repository.Begin(call, cursor)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"snapshot_id": cursor.ID, "generation_id": build.GenerationID, "epoch": cursor.Epoch, "watermark": cursor.Watermark, "state": build.State})
}
