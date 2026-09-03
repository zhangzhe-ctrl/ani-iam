package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	authv1 "github.com/zhangzhe-ctrl/ani-iam/internal/compat/authv1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/service"
)

func NewGRPCServer(c *conf.Server_GRPC, middlewares ...middleware.Middleware) *kratosgrpc.Server {
	return NewGRPCServerWithLegacyAuth(c, service.NewLegacyAuthService(), middlewares...)
}

func NewGRPCServerWithLegacyAuth(c *conf.Server_GRPC, authService authv1.AuthServiceServer, middlewares ...middleware.Middleware) *kratosgrpc.Server {
	server := kratosgrpc.NewServer(
		kratosgrpc.Network(c.Network),
		kratosgrpc.Address(c.Addr),
		kratosgrpc.Timeout(c.Timeout.AsDuration()),
		kratosgrpc.Middleware(middlewares...),
		kratosgrpc.DisableReflection(),
	)
	authv1.RegisterAuthServiceServer(server, authService)
	return server
}
