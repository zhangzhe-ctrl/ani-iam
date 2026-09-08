package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	kratos "github.com/go-kratos/kratos/v3"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/internal/server"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

const (
	startupDependencyTimeout  = 5 * time.Second
	apiKeyUsageBatchSize      = 256
	apiKeyMaintenanceInterval = biz.APIKeyUsageMaxLag / 15
	apiKeyMaintenanceTimeout  = 5 * time.Second
)

type passwordActionNotificationDispatcher interface {
	DispatchNext(context.Context) (bool, error)
}

type apiKeyUsageFlusher interface {
	Flush(context.Context) (int, error)
}

type apiKeyOperationalSnapshotSink interface {
	SetAPIKeyOperationalSnapshot(biz.APIKeyOperationalSnapshot) error
}

type apiKeyMaintenanceWorker struct {
	flusher             apiKeyUsageFlusher
	snapshotReader      biz.APIKeyOperationalSnapshotReader
	snapshotSink        apiKeyOperationalSnapshotSink
	clock               biz.Clock
	maintenanceInterval time.Duration
	operationTimeout    time.Duration
	logger              *slog.Logger
	context             context.Context
	cancel              context.CancelFunc
	done                chan struct{}
	mu                  sync.Mutex
	started             bool
}

func newAPIKeyMaintenanceWorker(
	flusher apiKeyUsageFlusher,
	snapshotReader biz.APIKeyOperationalSnapshotReader,
	snapshotSink apiKeyOperationalSnapshotSink,
	clock biz.Clock,
	maintenanceInterval time.Duration,
	operationTimeout time.Duration,
	logger *slog.Logger,
) (*apiKeyMaintenanceWorker, error) {
	if flusher == nil || snapshotReader == nil || snapshotSink == nil || clock == nil ||
		maintenanceInterval <= 0 || operationTimeout <= 0 || logger == nil {
		return nil, errors.New("API key maintenance worker configuration is required")
	}
	workerContext, cancel := context.WithCancel(context.Background())
	return &apiKeyMaintenanceWorker{
		flusher:             flusher,
		snapshotReader:      snapshotReader,
		snapshotSink:        snapshotSink,
		clock:               clock,
		maintenanceInterval: maintenanceInterval,
		operationTimeout:    operationTimeout,
		logger:              logger,
		context:             workerContext,
		cancel:              cancel,
		done:                make(chan struct{}),
	}, nil
}

func (w *apiKeyMaintenanceWorker) Start(context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return errors.New("API key maintenance worker already started")
	}
	w.started = true
	w.mu.Unlock()
	defer close(w.done)

	for {
		select {
		case <-w.context.Done():
			return nil
		default:
		}
		w.maintain()
		timer := time.NewTimer(w.maintenanceInterval)
		select {
		case <-w.context.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (w *apiKeyMaintenanceWorker) maintain() {
	flushContext, cancelFlush := context.WithTimeout(w.context, w.operationTimeout)
	w.flushUsage(flushContext)
	cancelFlush()

	snapshotContext, cancelSnapshot := context.WithTimeout(w.context, w.operationTimeout)
	defer cancelSnapshot()
	snapshot, err := w.snapshotReader.GetAPIKeyOperationalSnapshot(snapshotContext, w.clock.Now().UTC())
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			w.logger.Warn("API key operational snapshot refresh failed")
		}
		return
	}
	if err := w.snapshotSink.SetAPIKeyOperationalSnapshot(snapshot); err != nil {
		w.logger.Warn("API key operational snapshot publication failed")
	}
}

func (w *apiKeyMaintenanceWorker) flushUsage(ctx context.Context) {
	// BatchSize bounds each PostgreSQL pass, not the amount drained per
	// lifecycle iteration. Continue until empty so sustained traffic cannot
	// turn the batch size into an artificial per-minute throughput ceiling.
	for {
		processed, err := w.flusher.Flush(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				w.logger.Warn("API key usage flush failed")
			}
			return
		}
		if processed == 0 {
			return
		}
		if processed < 0 {
			w.logger.Warn("API key usage flush returned invalid state")
			return
		}
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.Canceled) {
				w.logger.Warn("API key usage flush deadline exceeded")
			}
			return
		default:
		}
	}
}

func (w *apiKeyMaintenanceWorker) Stop(ctx context.Context) error {
	w.cancel()
	w.mu.Lock()
	started := w.started
	w.mu.Unlock()
	if !started {
		return nil
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type passwordActionNotificationWorker struct {
	dispatcher        passwordActionNotificationDispatcher
	dispatchInterval  time.Duration
	submissionTimeout time.Duration
	logger            *slog.Logger
	context           context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	mu                sync.Mutex
	started           bool
}

func newPasswordActionNotificationWorker(
	dispatcher passwordActionNotificationDispatcher,
	dispatchInterval time.Duration,
	submissionTimeout time.Duration,
	logger *slog.Logger,
) (*passwordActionNotificationWorker, error) {
	if dispatcher == nil || dispatchInterval <= 0 || submissionTimeout <= 0 || logger == nil {
		return nil, errors.New("password-action notification worker configuration is required")
	}
	workerContext, cancel := context.WithCancel(context.Background())
	return &passwordActionNotificationWorker{
		dispatcher:        dispatcher,
		dispatchInterval:  dispatchInterval,
		submissionTimeout: submissionTimeout,
		logger:            logger,
		context:           workerContext,
		cancel:            cancel,
		done:              make(chan struct{}),
	}, nil
}

func (w *passwordActionNotificationWorker) Start(context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return errors.New("password-action notification worker already started")
	}
	w.started = true
	w.mu.Unlock()
	defer close(w.done)

	for {
		select {
		case <-w.context.Done():
			return nil
		default:
		}
		dispatchContext, cancel := context.WithTimeout(w.context, w.submissionTimeout)
		processed, err := w.dispatcher.DispatchNext(dispatchContext)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Warn(
				"password-action notification dispatch failed",
				"retryable", errors.Is(err, biz.ErrPasswordActionNotificationRetryable),
			)
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(w.dispatchInterval)
		select {
		case <-w.context.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (w *passwordActionNotificationWorker) Stop(ctx context.Context) error {
	w.cancel()
	w.mu.Lock()
	started := w.started
	w.mu.Unlock()
	if !started {
		return nil
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newPasswordActionNotificationRuntime(
	config *conf.Notification,
	outbox biz.PasswordActionNotificationOutbox,
	tokens biz.PasswordActionTokenCodec,
	clock biz.Clock,
	logger *slog.Logger,
) (*passwordActionNotificationWorker, *data.NotificationGRPCClient, error) {
	if config == nil {
		return nil, nil, errors.New("Notification runtime configuration is required")
	}
	client, err := data.NewNotificationGRPCClient(data.NotificationGRPCClientConfig{
		Address:         config.Address,
		CertificateFile: config.CertificateFile,
		PrivateKeyFile:  config.PrivateKeyFile,
		ServerCAFile:    config.ServerCaFile,
		ServerDNSName:   config.ServerDnsName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure Notification gRPC client: %w", err)
	}
	closeClient := func() {
		_ = client.Close()
	}
	submitter, err := data.NewGRPCPasswordActionNotificationSubmitter(
		client,
		data.PasswordActionNotificationSubmitterConfig{
			ConsoleActionURLBase: config.ConsoleActionUrlBase,
			Locale:               config.Locale,
		},
	)
	if err != nil {
		closeClient()
		return nil, nil, fmt.Errorf("configure password-action Notification submitter: %w", err)
	}
	dispatcher, err := biz.NewPasswordActionNotificationDispatcher(outbox, tokens, submitter, clock)
	if err != nil {
		closeClient()
		return nil, nil, fmt.Errorf("configure password-action Notification dispatcher: %w", err)
	}
	worker, err := newPasswordActionNotificationWorker(
		dispatcher,
		config.DispatchInterval.AsDuration(),
		config.SubmissionTimeout.AsDuration(),
		logger,
	)
	if err != nil {
		closeClient()
		return nil, nil, fmt.Errorf("configure password-action Notification worker: %w", err)
	}
	return worker, client, nil
}

func newTenantIAMAdminRuntime(
	postgresData *data.Data,
	expectedPolicyRevision string,
	ids biz.IDGenerator,
	clock biz.Clock,
	apiKeyCreationLimiter biz.APIKeyCreationLimiter,
) (*service.IAMAdminService, error) {
	if apiKeyCreationLimiter == nil {
		return nil, errors.New("API key creation limiter is required")
	}
	permissionCatalog, err := data.NewTargetPermissionCatalog(expectedPolicyRevision)
	if err != nil {
		return nil, fmt.Errorf("configure target permission catalog: %w", err)
	}
	mutations := biz.NewTenantAuthorizationUsecase(
		data.NewPostgresTenantAuthorizationUnitOfWork(postgresData),
		permissionCatalog,
		ids,
		clock,
		apiKeyCreationLimiter,
	)
	return service.NewTenantIAMAdminService(
		data.NewPostgresTenantAuthorizationReader(postgresData),
		mutations,
	), nil
}

func buildApp(bc *conf.Bootstrap, logger *slog.Logger) (*kratos.App, error) {
	if err := bc.Validate(); err != nil {
		return nil, err
	}
	runtime := bc.Runtime
	privateKey, err := data.LoadEd25519PrivateKeyFile(runtime.AccessToken.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	oidcClientSecret, err := data.LoadOIDCClientSecretFile(runtime.Oidc.ClientSecretFile)
	if err != nil {
		return nil, err
	}
	clock := data.NewSystemClock()
	tokenCodec, err := data.NewJWXAccessTokenCodec(
		runtime.AccessToken.ActiveKeyId,
		privateKey,
		map[string]ed25519.PublicKey{
			runtime.AccessToken.ActiveKeyId: privateKey.Public().(ed25519.PublicKey),
		},
		runtime.AccessToken.Issuer,
		clock,
	)
	if err != nil {
		return nil, fmt.Errorf("configure access-token codec: %w", err)
	}
	registry, err := data.NewTargetOperationRegistry(runtime.PolicyRevision)
	if err != nil {
		return nil, fmt.Errorf("configure target operation registry: %w", err)
	}
	grpcTLS, err := server.LoadMutualTLSServerConfig(bc.Server.Grpc.Tls)
	if err != nil {
		return nil, err
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupDependencyTimeout)
	defer cancelStartup()
	postgresConfig, err := pgxpool.ParseConfig(runtime.Postgresql.Dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL runtime DSN")
	}
	postgresPool, err := pgxpool.NewWithConfig(startupContext, postgresConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL runtime pool: %w", err)
	}
	if err := postgresPool.Ping(startupContext); err != nil {
		postgresPool.Close()
		return nil, fmt.Errorf("connect PostgreSQL runtime: %w", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:         runtime.Redis.Addr,
		Username:     runtime.Redis.Username,
		Password:     runtime.Redis.Password,
		DB:           int(runtime.Redis.Database),
		DialTimeout:  runtime.Redis.DialTimeout.AsDuration(),
		ReadTimeout:  runtime.Redis.ReadTimeout.AsDuration(),
		WriteTimeout: runtime.Redis.WriteTimeout.AsDuration(),
	})
	if err := redisClient.Ping(startupContext).Err(); err != nil {
		_ = redisClient.Close()
		postgresPool.Close()
		return nil, fmt.Errorf("connect Redis runtime: %w", err)
	}
	closeRuntime := func(context.Context) error {
		redisError := redisClient.Close()
		postgresPool.Close()
		return redisError
	}

	throttle, err := data.NewRedisLoginThrottle(redisClient, data.RedisLoginThrottleConfig{
		Namespace: runtime.Redis.Namespace,
		Limit:     int64(runtime.Redis.LoginLimit),
		Window:    runtime.Redis.LoginWindow.AsDuration(),
		BaseDelay: time.Second,
	})
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Redis login throttle: %w", err)
	}
	postgresData := data.NewData(postgresPool)
	ids := data.NewUUIDv7Generator()
	secrets := data.NewSecretGenerator()
	apiKeyCreationLimiter, err := data.NewRedisAPIKeyCreationLimiter(redisClient, data.RedisAPIKeyCreationLimiterConfig{
		Namespace: runtime.Redis.Namespace,
		Limit:     biz.APIKeyCreationRateLimit,
		Window:    biz.APIKeyCreationRateWindow,
	})
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Redis API key creation limiter: %w", err)
	}
	apiKeyUsageAggregator, err := data.NewRedisAPIKeyUsageAggregator(redisClient, postgresData, data.RedisAPIKeyUsageConfig{
		Namespace: runtime.Redis.Namespace,
		BatchSize: apiKeyUsageBatchSize,
	})
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Redis API key usage aggregator: %w", err)
	}
	adminService, err := newTenantIAMAdminRuntime(postgresData, runtime.PolicyRevision, ids, clock, apiKeyCreationLimiter)
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, err
	}
	oidcProvider, err := data.NewCoreOSOIDCProvider(startupContext, data.CoreOSOIDCProviderConfig{
		Name:         runtime.Oidc.Provider,
		IssuerURL:    runtime.Oidc.IssuerUrl,
		ClientID:     runtime.Oidc.ClientId,
		ClientSecret: oidcClientSecret,
		RedirectURIs: []string{runtime.Oidc.LoginRedirectUri, runtime.Oidc.IdentityLinkRedirectUri},
		HTTPClient:   &http.Client{Timeout: runtime.Oidc.HttpTimeout.AsDuration()},
	})
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure OIDC provider: %w", err)
	}
	oidcOperations, err := data.NewRedisOIDCOperationStore(redisClient, runtime.Redis.Namespace)
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Redis OIDC operation store: %w", err)
	}
	oidcUsecase, err := biz.NewOIDCUsecase(
		biz.OIDCUsecaseConfig{
			Provider:                runtime.Oidc.Provider,
			LoginRedirectURI:        runtime.Oidc.LoginRedirectUri,
			IdentityLinkRedirectURI: runtime.Oidc.IdentityLinkRedirectUri,
			RecentReauthentication:  runtime.Oidc.RecentReauthentication.AsDuration(),
		},
		oidcProvider,
		oidcOperations,
		data.NewPostgresOIDCReader(postgresData),
		data.NewPostgresOIDCUnitOfWork(postgresData),
		tokenCodec,
		secrets,
		ids,
		clock,
	)
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure OIDC use case: %w", err)
	}
	authentication := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(postgresData),
		data.NewArgon2idPasswordHasher(),
		throttle,
		data.NewPostgresLoginUnitOfWork(postgresData),
		tokenCodec,
		secrets,
		ids,
		clock,
		apiKeyUsageAggregator,
	)
	authorization := biz.NewAuthorizationUsecase(
		registry,
		tokenCodec,
		data.NewPostgresAuthorizationReader(postgresData),
		ids,
		clock,
		apiKeyUsageAggregator,
	)
	notificationWorker, notificationClient, err := newPasswordActionNotificationRuntime(
		runtime.Notification,
		data.NewPostgresPasswordActionNotificationOutbox(postgresData),
		tokenCodec,
		clock,
		logger,
	)
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, err
	}
	closeNotification := func(context.Context) error {
		return notificationClient.Close()
	}
	authenticationService := service.NewAuthenticationService(authentication, oidcUsecase)
	authorizationService := service.NewAuthorizationService(authorization)

	readiness := server.NewReadiness()
	observability, err := server.NewObservability(Name, Version, readiness)
	if err != nil {
		_ = closeNotification(context.Background())
		_ = closeRuntime(context.Background())
		return nil, err
	}
	apiKeyMaintenanceWorker, err := newAPIKeyMaintenanceWorker(
		apiKeyUsageAggregator,
		data.NewPostgresAPIKeyOperationalSnapshotReader(postgresData),
		observability,
		clock,
		apiKeyMaintenanceInterval,
		apiKeyMaintenanceTimeout,
		logger,
	)
	if err != nil {
		_ = observability.Shutdown(context.Background())
		_ = closeNotification(context.Background())
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure API key maintenance worker: %w", err)
	}
	workloadIdentity, err := server.NewGatewayWorkloadIdentityMiddleware(bc.Server.Grpc.Tls.GatewayClientDnsName)
	if err != nil {
		_ = observability.Shutdown(context.Background())
		_ = closeNotification(context.Background())
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Gateway workload identity: %w", err)
	}
	middlewares := append(observability.ServerMiddleware(logger), workloadIdentity)
	grpcServer, err := server.NewTargetGRPCServer(
		bc.Server.Grpc,
		grpcTLS,
		authenticationService,
		authorizationService,
		adminService,
		middlewares...,
	)
	if err != nil {
		_ = observability.Shutdown(context.Background())
		_ = closeNotification(context.Background())
		_ = closeRuntime(context.Background())
		return nil, err
	}
	adminServer := server.NewAdminServer(bc.Server.Admin, readiness, observability.Gatherer(), middlewares...)
	return newApp(
		logger,
		grpcServer,
		adminServer,
		readiness,
		observability,
		notificationWorker,
		apiKeyMaintenanceWorker,
		closeNotification,
		closeRuntime,
		bc.Server.ShutdownTimeout.AsDuration(),
	), nil
}

func newApp(
	logger *slog.Logger,
	grpcServer *kratosgrpc.Server,
	adminServer *kratoshttp.Server,
	readiness *server.Readiness,
	observability *server.Observability,
	notificationWorker *passwordActionNotificationWorker,
	apiKeyMaintenanceWorker *apiKeyMaintenanceWorker,
	closeNotification func(context.Context) error,
	closeRuntime func(context.Context) error,
	stopTimeout time.Duration,
) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{"runtime.profile": conf.IsolatedProfile}),
		kratos.Logger(logger),
		kratos.Server(grpcServer, adminServer, notificationWorker, apiKeyMaintenanceWorker),
		kratos.AfterStart(func(context.Context) error {
			readiness.Set(true)
			return nil
		}),
		kratos.BeforeStop(func(context.Context) error {
			readiness.Set(false)
			return nil
		}),
		kratos.AfterStop(closeNotification),
		kratos.AfterStop(closeRuntime),
		kratos.AfterStop(observability.Shutdown),
		kratos.StopTimeout(stopTimeout),
	)
}
