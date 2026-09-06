package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
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

const startupDependencyTimeout = 5 * time.Second

func buildApp(bc *conf.Bootstrap, logger *slog.Logger) (*kratos.App, error) {
	if err := bc.Validate(); err != nil {
		return nil, err
	}
	runtime := bc.Runtime
	privateKey, err := data.LoadEd25519PrivateKeyFile(runtime.AccessToken.PrivateKeyFile)
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
	})
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, fmt.Errorf("configure Redis login throttle: %w", err)
	}
	postgresData := data.NewData(postgresPool)
	ids := data.NewUUIDv7Generator()
	authentication := biz.NewAuthenticationUsecase(
		data.NewPostgresPasswordLoginReader(postgresData),
		data.NewArgon2idPasswordHasher(),
		throttle,
		data.NewPostgresLoginUnitOfWork(postgresData),
		tokenCodec,
		data.NewSecretGenerator(),
		ids,
		clock,
	)
	authorization := biz.NewAuthorizationUsecase(
		registry,
		tokenCodec,
		data.NewPostgresAuthorizationReader(postgresData),
		ids,
		clock,
	)
	authenticationService := service.NewAuthenticationService(authentication)
	authorizationService := service.NewAuthorizationService(authorization)
	adminService := service.NewIAMAdminService()

	readiness := server.NewReadiness()
	observability, err := server.NewObservability(Name, Version, readiness)
	if err != nil {
		_ = closeRuntime(context.Background())
		return nil, err
	}
	workloadIdentity, err := server.NewGatewayWorkloadIdentityMiddleware(bc.Server.Grpc.Tls.GatewayClientDnsName)
	if err != nil {
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
	closeRuntime func(context.Context) error,
	stopTimeout time.Duration,
) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{"runtime.profile": conf.IsolatedProfile}),
		kratos.Logger(logger),
		kratos.Server(grpcServer, adminServer),
		kratos.AfterStart(func(context.Context) error {
			readiness.Set(true)
			return nil
		}),
		kratos.BeforeStop(func(context.Context) error {
			readiness.Set(false)
			return nil
		}),
		kratos.AfterStop(closeRuntime),
		kratos.AfterStop(observability.Shutdown),
		kratos.StopTimeout(stopTimeout),
	)
}
