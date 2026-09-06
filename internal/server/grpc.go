package server

import (
	"crypto/tls"
	"errors"

	"github.com/go-kratos/kratos/v3/middleware"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
)

func NewGRPCServer(c *conf.Server_GRPC, tlsConfig *tls.Config, middlewares ...middleware.Middleware) (*kratosgrpc.Server, error) {
	if tlsConfig == nil || tlsConfig.MinVersion < tls.VersionTLS13 ||
		tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert || len(tlsConfig.Certificates) != 1 || tlsConfig.ClientCAs == nil {
		return nil, ErrInvalidMutualTLSConfiguration
	}
	return kratosgrpc.NewServer(
		kratosgrpc.Network(c.Network),
		kratosgrpc.Address(c.Addr),
		kratosgrpc.Timeout(c.Timeout.AsDuration()),
		kratosgrpc.Middleware(middlewares...),
		kratosgrpc.TLSConfig(tlsConfig),
		kratosgrpc.DisableReflection(),
	), nil
}

// NewTargetGRPCServer registers the complete frozen IAM service inventory. The
// individual service implementations may intentionally return Unimplemented
// for methods outside the currently claimed vertical slice.
func NewTargetGRPCServer(
	c *conf.Server_GRPC,
	tlsConfig *tls.Config,
	authentication iamv1.AuthenticationServiceServer,
	authorization iamv1.AuthorizationServiceServer,
	admin iamv1.IAMAdminServiceServer,
	middlewares ...middleware.Middleware,
) (*kratosgrpc.Server, error) {
	if authentication == nil || authorization == nil || admin == nil {
		return nil, errors.New("all target IAM gRPC services are required")
	}
	server, err := NewGRPCServer(c, tlsConfig, middlewares...)
	if err != nil {
		return nil, err
	}
	iamv1.RegisterAuthenticationServiceServer(server, authentication)
	iamv1.RegisterAuthorizationServiceServer(server, authorization)
	iamv1.RegisterIAMAdminServiceServer(server, admin)
	return server, nil
}
