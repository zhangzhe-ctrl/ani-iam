package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
)

var ErrInvalidMutualTLSConfiguration = errors.New("invalid gRPC mutual TLS configuration")

func LoadMutualTLSServerConfig(config *conf.Server_GRPC_TLS) (*tls.Config, error) {
	if config == nil {
		return nil, ErrInvalidMutualTLSConfiguration
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC server certificate: %w", err)
	}
	clientCAPEM, err := os.ReadFile(config.ClientCaFile)
	if err != nil {
		return nil, fmt.Errorf("read gRPC client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCAPEM) {
		return nil, fmt.Errorf("%w: client CA contains no certificates", ErrInvalidMutualTLSConfiguration)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}
