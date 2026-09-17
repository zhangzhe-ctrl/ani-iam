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
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

type coreLifecycleConfiguration struct {
	Version        int                        `json:"version"`
	ProjectionMode string                     `json:"projection_mode"`
	Broker         data.CoreNATSConfiguration `json:"broker"`
	Snapshot       struct {
		ReaderID        uuid.UUID `json:"reader_id"`
		Origin          string    `json:"origin"`
		ServerName      string    `json:"server_name"`
		IAMAddress      string    `json:"iam_address"`
		IAMServerName   string    `json:"iam_server_name"`
		CertificateFile string    `json:"certificate_file"`
		PrivateKeyFile  string    `json:"private_key_file"`
		CAFile          string    `json:"ca_file"`
	} `json:"snapshot"`
}

func readCoreLifecycleConfiguration(c *conf.Runtime) (*coreLifecycleConfiguration, error) {
	if c.TenantLifecycleFile == "" && c.TenantLifecycleSha256 == "" {
		return nil, nil
	}
	raw, err := readBootstrapFile(c.TenantLifecycleFile, 128<<10, false)
	if err != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != c.TenantLifecycleSha256 {
		return nil, biz.ErrCoreBrokerAuthority
	}
	var config coreLifecycleConfiguration
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || (config.ProjectionMode != "shadow" && config.ProjectionMode != "enforced") || config.Version != 2 || config.Snapshot.ReaderID.Version() != 7 || config.Snapshot.ReaderID.Variant() != uuid.RFC4122 || config.Broker.Authority.Validate() != nil || config.Broker.Authority.Environment != c.Environment || config.Broker.Authority.TrustDomain != c.TrustDomain {
		return nil, biz.ErrCoreBrokerAuthority
	}
	if !data.ValidHTTPSOrigin(config.Snapshot.Origin) {
		return nil, biz.ErrCoreProjectionInvalid
	}
	if !data.ValidDeploymentAddress(config.Snapshot.IAMAddress) {
		return nil, biz.ErrCoreProjectionInvalid
	}
	return &config, nil
}

type coreLifecycleRuntime struct {
	consumer         biz.CoreBrokerConsumer
	receiver         *biz.CoreBrokerReceiver
	dispatcher       *biz.CoreBootstrapDispatcher
	snapshot         biz.TenantSnapshotSource
	rebuild          *biz.TenantSnapshotCoordinator
	snapshotIdentity *grpcworkload.WorkloadOnlyClient
	closeHTTP        func()
	logger           *slog.Logger
	mu               sync.Mutex
	cancel           context.CancelFunc
	done             chan struct{}
	started, closed  bool
	probeMu          sync.Mutex
	probeCursor      biz.TenantSnapshotCursor
	probeKey         string
}

func newCoreLifecycleRuntime(ctx context.Context, c *coreLifecycleConfiguration, d *data.Data, registry *workloadregistry.Registry, policy string, ids biz.IDGenerator, secrets biz.SecretGenerator, clock biz.Clock, logger *slog.Logger) (*coreLifecycleRuntime, error) {
	if c == nil {
		return nil, nil
	}
	authority := c.Broker.Authority
	consumer, err := data.NewCoreNATSConsumer(ctx, c.Broker)
	if err != nil {
		return nil, err
	}
	r := &coreLifecycleRuntime{consumer: consumer, logger: logger, done: make(chan struct{})}
	success := false
	defer func() {
		if !success {
			_ = r.close()
		}
	}()
	repo, err := data.NewCoreBrokerRepository(d, authority)
	if err != nil {
		return nil, err
	}
	if err = repo.Check(ctx); err != nil {
		return nil, err
	}
	if r.receiver, err = biz.NewCoreBrokerReceiver(consumer, service.NewGovernanceBrokerDecoder(), repo); err != nil {
		return nil, err
	}
	uow, err := data.NewCoreBrokerBootstrapWorkUnitOfWork(d, authority)
	if err != nil {
		return nil, err
	}
	authorizer, err := data.NewCoreBrokerBootstrapAuthorizer(d, authority)
	if err != nil {
		return nil, err
	}
	catalog, err := data.NewTargetPermissionCatalog(policy, registry)
	if err != nil {
		return nil, err
	}
	worker, err := biz.NewCoreBootstrapWorker(uow, authorizer, catalog, ids, secrets, clock)
	if err != nil {
		return nil, err
	}
	queue, err := data.NewCoreBootstrapQueue(d, authority.Producer)
	if err != nil {
		return nil, err
	}
	if r.dispatcher, err = biz.NewCoreBootstrapDispatcher(queue, worker); err != nil {
		return nil, err
	}
	files := grpcworkload.TLSFiles{CertificateFile: c.Snapshot.CertificateFile, PrivateKeyFile: c.Snapshot.PrivateKeyFile, CAFile: c.Snapshot.CAFile}
	identity, err := grpcworkload.NewWorkloadOnlyClient(grpcworkload.ClientConfig{Registry: registry, Address: c.Snapshot.IAMAddress, ServerName: c.Snapshot.IAMServerName, Environment: authority.Environment, TrustDomain: authority.TrustDomain, TLS: files, Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	r.snapshotIdentity = identity
	if r.snapshot, r.closeHTTP, err = data.NewGovernanceSnapshotHTTPClient(data.GovernanceSnapshotHTTPConfiguration{Origin: c.Snapshot.Origin, ServerName: c.Snapshot.ServerName, ReaderID: c.Snapshot.ReaderID, TLS: files, Registry: registry, TokenSource: identity.HTTPTokenSource}); err != nil {
		return nil, err
	}
	readerDNS, err := snapshotReaderIdentity(c.Snapshot.CertificateFile)
	if err != nil {
		return nil, err
	}
	recovery, err := data.NewGovernanceSnapshotRecoveryRepository(d, data.GovernanceSnapshotRecoveryConfiguration{Broker: authority, ReaderID: c.Snapshot.ReaderID, ReaderIdentity: readerDNS, Registry: registry})
	if err != nil {
		return nil, err
	}
	if r.rebuild, err = biz.NewTenantSnapshotCoordinator(r.snapshot, recovery); err != nil {
		return nil, err
	}
	if _, err = recovery.NextRecovery(ctx); err != nil {
		return nil, err
	}
	success = true
	return r, nil
}
func (r *coreLifecycleRuntime) Start(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.closed {
		return biz.ErrCoreBrokerAuthority
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.started = true
	go func() {
		var workers sync.WaitGroup
		workers.Go(func() {
			r.run(ctx, "receiver", func(call context.Context) (bool, error) { err := r.receiver.ReceiveNext(call); return err == nil, err })
		})
		workers.Go(func() { r.run(ctx, "bootstrap", r.dispatcher.DispatchNext) })
		workers.Go(func() { r.run(ctx, "snapshot", r.rebuild.ReconcileNext) })
		workers.Wait()
		close(r.done)
	}()
	return nil
}
func (r *coreLifecycleRuntime) run(ctx context.Context, name string, next func(context.Context) (bool, error)) {
	lastCode := ""
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		worked, err := next(call)
		cancel()
		code := ""
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			code = "dependency_unavailable"
			if errors.Is(err, biz.ErrCoreBrokerAuthority) {
				code = "current_authority_denied"
			}
			if errors.Is(err, biz.ErrCoreBootstrapAttention) {
				code = "attention_required"
			}
		}
		if code != lastCode && r.logger != nil {
			r.logger.Info("Tenant lifecycle worker state", "worker", name, "state", code)
			lastCode = code
		}
		delay := 100 * time.Millisecond
		if code != "" {
			delay = time.Second
		}
		if worked && err == nil {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (r *coreLifecycleRuntime) Check(ctx context.Context) error {
	r.mu.Lock()
	running := r.started && !r.closed
	r.mu.Unlock()
	if !running {
		return biz.ErrPersistenceUnavailable
	}
	if err := r.receiver.Check(ctx); err != nil {
		return err
	}
	// A real owner read verifies TLS, WAT, exact read Grant and cursor binding.
	// The one-item health cursor is separate from any rebuild generation.
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	if r.probeCursor.ID == uuid.Nil || !time.Now().Before(r.probeCursor.ExpiresAt) {
		if r.probeKey == "" || r.probeCursor.ID != uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return biz.ErrPersistenceUnavailable
			}
			r.probeKey = id.String()
		}
		cursor, err := r.snapshot.Begin(ctx, r.probeKey, 1)
		if err != nil {
			return err
		}
		r.probeCursor = cursor
	}
	_, err := r.snapshot.Page(ctx, r.probeCursor, r.probeCursor.FirstToken)
	if errors.Is(err, biz.ErrTenantSnapshotExpired) || errors.Is(err, biz.ErrTenantLifecycleConflict) || errors.Is(err, biz.ErrWorkloadPermissionDenied) {
		r.probeCursor = biz.TenantSnapshotCursor{}
		r.probeKey = ""
	}
	return err
}
func (r *coreLifecycleRuntime) close() error {
	if r.closeHTTP != nil {
		r.closeHTTP()
	}
	var err error
	if r.snapshotIdentity != nil {
		err = r.snapshotIdentity.Close()
	}
	if r.consumer != nil {
		err = errors.Join(err, r.consumer.Close())
	}
	return err
}
func (r *coreLifecycleRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	started := r.started
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		select {
		case <-r.done:
		case <-ctx.Done():
			_ = r.close()
			return ctx.Err()
		}
	}
	return r.close()
}

// TLS verification and mTLS/WAT remain in the shared SDK. This reads only the
// public leaf already supplied to that client, to bind the local rebuild's
// reader checks to its exact registered DNS identity rather than another ID.
func snapshotReaderIdentity(certificateFile string) (string, error) {
	raw, err := readBootstrapFile(certificateFile, 1<<20, false)
	if err != nil {
		return "", biz.ErrCoreBrokerAuthority
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", biz.ErrCoreBrokerAuthority
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(leaf.DNSNames) != 1 || len(leaf.IPAddresses) != 0 || len(leaf.URIs) != 0 || len(leaf.EmailAddresses) != 0 {
		return "", biz.ErrCoreBrokerAuthority
	}
	return leaf.DNSNames[0], nil
}
