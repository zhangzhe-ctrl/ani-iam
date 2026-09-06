package conf

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

const IsolatedProfile = "direct-p2-isolated"

func (c *Bootstrap) Validate() error {
	if c == nil {
		return fmt.Errorf("bootstrap config is required")
	}
	if c.Profile != IsolatedProfile {
		return fmt.Errorf("runtime profile must be %q", IsolatedProfile)
	}
	if c.Server == nil || c.Server.Grpc == nil || c.Server.Admin == nil {
		return fmt.Errorf("grpc and admin server config are required")
	}
	if err := validateListener("grpc", c.Server.Grpc.Network, c.Server.Grpc.Addr, c.Server.Grpc.Timeout); err != nil {
		return err
	}
	if err := validateGRPCTLS(c.Server.Grpc.Tls); err != nil {
		return err
	}
	if err := validateListener("admin", c.Server.Admin.Network, c.Server.Admin.Addr, c.Server.Admin.Timeout); err != nil {
		return err
	}
	if c.Server.Grpc.Addr == c.Server.Admin.Addr && c.Server.Grpc.Addr != "127.0.0.1:0" && c.Server.Grpc.Addr != "[::1]:0" {
		return fmt.Errorf("grpc and admin listeners must use distinct addresses")
	}
	if err := validateDuration("shutdown", c.Server.ShutdownTimeout, 30*time.Second); err != nil {
		return err
	}
	return validateRuntime(c.Runtime)
}

func validateGRPCTLS(config *Server_GRPC_TLS) error {
	if config == nil {
		return fmt.Errorf("gRPC mutual TLS config is required")
	}
	for name, path := range map[string]string{
		"certificate": config.CertificateFile,
		"private key": config.PrivateKeyFile,
		"client CA":   config.ClientCaFile,
	} {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("gRPC TLS %s file must be an absolute path", name)
		}
	}
	identity := strings.TrimSpace(config.GatewayClientDnsName)
	if identity == "" || identity != config.GatewayClientDnsName || strings.ContainsAny(identity, " /\t\r\n") {
		return fmt.Errorf("gRPC Gateway client DNS name must be a non-empty DNS identity")
	}
	return nil
}

func validateListener(name, network, address string, timeout *durationpb.Duration) error {
	if network != "tcp" {
		return fmt.Errorf("%s network must be tcp", name)
	}
	if err := validateLoopbackEndpoint(name, address); err != nil {
		return err
	}
	return validateDuration(name, timeout, 30*time.Second)
}

func validateLoopbackEndpoint(name, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s address: %w", name, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s address must use a literal loopback IP", name)
	}
	if port == "" {
		return fmt.Errorf("%s address requires a port", name)
	}
	return nil
}

func validateDuration(name string, value *durationpb.Duration, maximum time.Duration) error {
	if value == nil {
		return fmt.Errorf("%s timeout is required", name)
	}
	if err := value.CheckValid(); err != nil {
		return fmt.Errorf("%s timeout: %w", name, err)
	}
	duration := value.AsDuration()
	if duration <= 0 || duration > maximum {
		return fmt.Errorf("%s timeout must be within 1ns..%s", name, maximum)
	}
	return nil
}

func validateRuntime(runtime *Runtime) error {
	if runtime == nil || runtime.Postgresql == nil || runtime.Redis == nil || runtime.AccessToken == nil {
		return fmt.Errorf("PostgreSQL, Redis, and access-token runtime config are required")
	}
	if err := validatePostgreSQL(runtime.Postgresql); err != nil {
		return err
	}
	if err := validateRedis(runtime.Redis); err != nil {
		return err
	}
	if err := validateAccessToken(runtime.AccessToken); err != nil {
		return err
	}
	revision := strings.TrimSpace(runtime.PolicyRevision)
	if !strings.HasPrefix(revision, "sha256:") {
		return fmt.Errorf("policy revision must be a sha256 digest")
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(revision, "sha256:"))
	if err != nil || len(digest) != 32 {
		return fmt.Errorf("policy revision must be a sha256 digest")
	}
	return nil
}

func validatePostgreSQL(postgresql *PostgreSQL) error {
	dsn := strings.TrimSpace(postgresql.Dsn)
	if dsn == "" {
		return fmt.Errorf("PostgreSQL DSN is required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return fmt.Errorf("PostgreSQL DSN must use postgres or postgresql scheme")
	}
	if parsed.User == nil || parsed.User.Username() != "ani_iam_runtime" {
		return fmt.Errorf("PostgreSQL DSN must name the restricted runtime role")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return fmt.Errorf("PostgreSQL DSN must name the isolated database")
	}
	return validateLoopbackEndpoint("PostgreSQL", parsed.Host)
}

func validateRedis(redis *Redis) error {
	if err := validateLoopbackEndpoint("Redis", redis.Addr); err != nil {
		return err
	}
	if redis.Database < 0 {
		return fmt.Errorf("Redis database must be non-negative")
	}
	namespace := strings.TrimSpace(redis.Namespace)
	if namespace == "" || namespace != redis.Namespace || strings.ContainsAny(namespace, " \t\r\n") {
		return fmt.Errorf("Redis namespace must be a non-empty token")
	}
	if redis.LoginLimit == 0 {
		return fmt.Errorf("Redis login limit must be positive")
	}
	if err := validateDuration("Redis login window", redis.LoginWindow, 24*time.Hour); err != nil {
		return err
	}
	if err := validateDuration("Redis dial", redis.DialTimeout, 5*time.Second); err != nil {
		return err
	}
	if err := validateDuration("Redis read", redis.ReadTimeout, 5*time.Second); err != nil {
		return err
	}
	return validateDuration("Redis write", redis.WriteTimeout, 5*time.Second)
}

func validateAccessToken(accessToken *AccessToken) error {
	if accessToken.Issuer != "ani-iam" {
		return fmt.Errorf("access-token issuer must be ani-iam")
	}
	if strings.TrimSpace(accessToken.ActiveKeyId) == "" {
		return fmt.Errorf("access-token active key ID is required")
	}
	if strings.TrimSpace(accessToken.PrivateKeyFile) == "" || !filepath.IsAbs(accessToken.PrivateKeyFile) {
		return fmt.Errorf("access-token private-key file must be an absolute path")
	}
	return nil
}
