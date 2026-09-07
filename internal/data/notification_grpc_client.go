package data

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
)

var ErrInvalidNotificationGRPCClientConfig = errors.New("invalid Notification gRPC client configuration")

const notificationClientDNSName = "ani-iam"

var oidNotificationSubjectAltName = asn1.ObjectIdentifier{2, 5, 29, 17}

type NotificationGRPCClientConfig struct {
	Address         string
	CertificateFile string
	PrivateKeyFile  string
	ServerCAFile    string
	ServerDNSName   string
}

// NotificationGRPCClient owns one authenticated connection to the frozen
// notification.v1 service. It deliberately has no insecure or metadata-based
// identity mode.
type NotificationGRPCClient struct {
	notificationv1.NotificationServiceClient
	connection *grpc.ClientConn
}

func NewNotificationGRPCClient(config NotificationGRPCClientConfig) (*NotificationGRPCClient, error) {
	if err := validateNotificationGRPCClientConfig(config); err != nil {
		return nil, err
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load Notification client certificate: %w", err)
	}
	if len(certificate.Certificate) == 0 {
		return nil, fmt.Errorf("%w: Notification client certificate chain is empty", ErrInvalidNotificationGRPCClientConfig)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse Notification client certificate: %w", err)
	}
	if !hasExtendedKeyUsage(leaf, x509.ExtKeyUsageClientAuth) {
		return nil, fmt.Errorf("%w: Notification certificate lacks client-auth usage", ErrInvalidNotificationGRPCClientConfig)
	}
	if !notificationCertificateHasSoleDNSIdentity(leaf, notificationClientDNSName) {
		return nil, fmt.Errorf("%w: Notification client certificate must have the sole DNS identity %q", ErrInvalidNotificationGRPCClientConfig, notificationClientDNSName)
	}
	serverCAPEM, err := os.ReadFile(config.ServerCAFile)
	if err != nil {
		return nil, fmt.Errorf("read Notification server CA: %w", err)
	}
	serverCAs := x509.NewCertPool()
	if !serverCAs.AppendCertsFromPEM(serverCAPEM) {
		return nil, fmt.Errorf("%w: Notification server CA contains no certificates", ErrInvalidNotificationGRPCClientConfig)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      serverCAs,
		ServerName:   config.ServerDNSName,
	}
	connection, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		return nil, fmt.Errorf("create Notification gRPC client: %w", err)
	}
	return &NotificationGRPCClient{
		NotificationServiceClient: notificationv1.NewNotificationServiceClient(connection),
		connection:                connection,
	}, nil
}

func (c *NotificationGRPCClient) Close() error {
	if c == nil || c.connection == nil {
		return nil
	}
	return c.connection.Close()
}

func validateNotificationGRPCClientConfig(config NotificationGRPCClientConfig) error {
	host, port, err := net.SplitHostPort(config.Address)
	if err != nil || port == "" {
		return fmt.Errorf("%w: Notification address must be host:port", ErrInvalidNotificationGRPCClientConfig)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: Notification address must use a literal loopback IP", ErrInvalidNotificationGRPCClientConfig)
	}
	for name, path := range map[string]string{
		"certificate": config.CertificateFile,
		"private key": config.PrivateKeyFile,
		"server CA":   config.ServerCAFile,
	} {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("%w: Notification %s file must be an absolute path", ErrInvalidNotificationGRPCClientConfig, name)
		}
	}
	identity := strings.TrimSpace(config.ServerDNSName)
	if identity == "" || identity != config.ServerDNSName || strings.ToLower(identity) != identity ||
		strings.ContainsAny(identity, " /\t\r\n") {
		return fmt.Errorf("%w: Notification server DNS name is invalid", ErrInvalidNotificationGRPCClientConfig)
	}
	return nil
}

func hasExtendedKeyUsage(certificate *x509.Certificate, expected x509.ExtKeyUsage) bool {
	for _, usage := range certificate.ExtKeyUsage {
		if usage == expected {
			return true
		}
	}
	return false
}

func notificationCertificateHasSoleDNSIdentity(certificate *x509.Certificate, expected string) bool {
	if certificate == nil || len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != expected ||
		len(certificate.IPAddresses) != 0 || len(certificate.EmailAddresses) != 0 || len(certificate.URIs) != 0 {
		return false
	}
	var subjectAltName []byte
	for _, extension := range certificate.Extensions {
		if !extension.Id.Equal(oidNotificationSubjectAltName) {
			continue
		}
		if subjectAltName != nil {
			return false
		}
		subjectAltName = extension.Value
	}
	if subjectAltName == nil {
		return false
	}
	var names asn1.RawValue
	rest, err := asn1.Unmarshal(subjectAltName, &names)
	if err != nil || len(rest) != 0 || names.Class != 0 || names.Tag != 16 || !names.IsCompound {
		return false
	}
	var identity asn1.RawValue
	rest, err = asn1.Unmarshal(names.Bytes, &identity)
	return err == nil && len(rest) == 0 &&
		identity.Class == 2 && identity.Tag == 2 && !identity.IsCompound &&
		string(identity.Bytes) == expected
}
