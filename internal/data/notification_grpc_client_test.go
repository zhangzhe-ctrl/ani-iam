package data

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationv1 "github.com/zhangzhe-ctrl/ani-notification-service/api/notification/v1"
)

func TestNotificationGRPCClientUsesMutualTLSAndVerifiesServerIdentity(t *testing.T) {
	pki := newNotificationClientTestPKI(t, "ani-notification", "ani-iam")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverTLS := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pki.serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pki.clientCAs,
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	handler := &mutualTLSNotificationService{}
	notificationv1.RegisterNotificationServiceServer(server, handler)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serveDone
	})

	client, err := NewNotificationGRPCClient(NotificationGRPCClientConfig{
		Address:         listener.Addr().String(),
		CertificateFile: pki.clientCertificateFile,
		PrivateKeyFile:  pki.clientPrivateKeyFile,
		ServerCAFile:    pki.serverCAFile,
		ServerDNSName:   "ani-notification",
	})
	if err != nil {
		t.Fatalf("NewNotificationGRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := client.SubmitNotification(ctx, &notificationv1.SubmitNotificationRequest{})
	if err != nil || response.GetReceipt().GetNotificationId() == "" {
		t.Fatalf("SubmitNotification() = %#v, %v", response, err)
	}
	if !handler.sawIAMClient.Load() {
		t.Fatal("server did not authenticate the ani-iam client DNS identity")
	}

	wrongNameClient, err := NewNotificationGRPCClient(NotificationGRPCClientConfig{
		Address:         listener.Addr().String(),
		CertificateFile: pki.clientCertificateFile,
		PrivateKeyFile:  pki.clientPrivateKeyFile,
		ServerCAFile:    pki.serverCAFile,
		ServerDNSName:   "wrong-notification",
	})
	if err != nil {
		t.Fatalf("NewNotificationGRPCClient(wrong name) error = %v", err)
	}
	t.Cleanup(func() { _ = wrongNameClient.Close() })
	wrongNameContext, wrongNameCancel := context.WithTimeout(context.Background(), time.Second)
	defer wrongNameCancel()
	if _, err := wrongNameClient.SubmitNotification(wrongNameContext, &notificationv1.SubmitNotificationRequest{}); err == nil {
		t.Fatal("Notification client accepted a server certificate for the wrong DNS identity")
	}
}

func TestNotificationGRPCClientFailsClosedOnInvalidConfiguration(t *testing.T) {
	tests := []NotificationGRPCClientConfig{
		{},
		{Address: "notification.example.test:443", CertificateFile: "/missing/client.crt", PrivateKeyFile: "/missing/client.key", ServerCAFile: "/missing/ca.crt", ServerDNSName: "ani-notification"},
		{Address: "127.0.0.1:443", CertificateFile: "relative/client.crt", PrivateKeyFile: "/missing/client.key", ServerCAFile: "/missing/ca.crt", ServerDNSName: "ani-notification"},
		{Address: "127.0.0.1:443", CertificateFile: "/missing/client.crt", PrivateKeyFile: "/missing/client.key", ServerCAFile: "/missing/ca.crt", ServerDNSName: "ani notification"},
		{Address: "127.0.0.1:443", CertificateFile: "/missing/client.crt", PrivateKeyFile: "/missing/client.key", ServerCAFile: "/missing/ca.crt", ServerDNSName: "ANI-Notification"},
	}
	for index, config := range tests {
		if client, err := NewNotificationGRPCClient(config); err == nil || client != nil {
			t.Fatalf("case %d NewNotificationGRPCClient() = %#v, %v", index, client, err)
		}
	}
}

func TestNotificationGRPCClientRejectsAmbiguousClientIdentity(t *testing.T) {
	tests := []struct {
		name    string
		dnsName string
		mutate  func(*x509.Certificate)
	}{
		{name: "another workload", dnsName: "other-service"},
		{name: "multiple DNS names", dnsName: "ani-iam", mutate: func(certificate *x509.Certificate) {
			certificate.DNSNames = append(certificate.DNSNames, "other-service")
		}},
		{name: "otherName", dnsName: "ani-iam", mutate: func(certificate *x509.Certificate) {
			certificate.ExtraExtensions = []pkix.Extension{notificationSubjectAltNameExtension(t, "ani-iam", []byte{0xa0, 0x00})}
		}},
		{name: "registeredID", dnsName: "ani-iam", mutate: func(certificate *x509.Certificate) {
			certificate.ExtraExtensions = []pkix.Extension{notificationSubjectAltNameExtension(t, "ani-iam", []byte{0x88, 0x03, 0x2a, 0x03, 0x04})}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pki := newNotificationClientTestPKI(t, "ani-notification", test.dnsName, test.mutate)
			client, err := NewNotificationGRPCClient(NotificationGRPCClientConfig{
				Address:         "127.0.0.1:1",
				CertificateFile: pki.clientCertificateFile,
				PrivateKeyFile:  pki.clientPrivateKeyFile,
				ServerCAFile:    pki.serverCAFile,
				ServerDNSName:   "ani-notification",
			})
			if client != nil {
				_ = client.Close()
			}
			if err == nil {
				t.Fatal("NewNotificationGRPCClient() accepted an ambiguous client workload identity")
			}
		})
	}
}

type mutualTLSNotificationService struct {
	notificationv1.UnimplementedNotificationServiceServer
	sawIAMClient atomic.Bool
}

func (s *mutualTLSNotificationService) SubmitNotification(
	ctx context.Context,
	_ *notificationv1.SubmitNotificationRequest,
) (*notificationv1.SubmitNotificationResponse, error) {
	transportPeer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "peer missing")
	}
	tlsInfo, ok := transportPeer.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) != 1 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "verified client chain missing")
	}
	leaf := tlsInfo.State.VerifiedChains[0][0]
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "ani-iam" {
		return nil, status.Error(codes.Unauthenticated, "client identity mismatch")
	}
	s.sawIAMClient.Store(true)
	return &notificationv1.SubmitNotificationResponse{Receipt: &notificationv1.NotificationReceipt{
		NotificationId: "cf8867ce-3314-4c84-98d0-6e9fd985d62a",
		RequestId:      "0198f062-b76d-7001-9000-000000000111",
		StoredAt:       timestamppb.New(time.Now().UTC()),
	}}, nil
}

type notificationClientTestPKI struct {
	serverCertificate     tls.Certificate
	clientCAs             *x509.CertPool
	clientCertificateFile string
	clientPrivateKeyFile  string
	serverCAFile          string
}

func newNotificationClientTestPKI(
	t *testing.T,
	serverDNSName string,
	clientDNSName string,
	clientMutate ...func(*x509.Certificate),
) notificationClientTestPKI {
	t.Helper()
	now := time.Now().UTC()
	caKey := newNotificationTestPrivateKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "DP2-06 Notification test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverCertificate := newNotificationTestLeaf(t, big.NewInt(2), serverDNSName, x509.ExtKeyUsageServerAuth, caCertificate, caKey)
	clientCertificate := newNotificationTestLeaf(t, big.NewInt(3), clientDNSName, x509.ExtKeyUsageClientAuth, caCertificate, caKey, clientMutate...)
	directory := t.TempDir()
	clientCertificateFile := filepath.Join(directory, "client.crt")
	clientPrivateKeyFile := filepath.Join(directory, "client.key")
	serverCAFile := filepath.Join(directory, "server-ca.crt")
	if err := os.WriteFile(clientCertificateFile, clientCertificate.CertificatePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clientPrivateKeyFile, clientCertificate.PrivateKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverCAFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		t.Fatal("append client CA")
	}
	return notificationClientTestPKI{
		serverCertificate:     serverCertificate.Certificate,
		clientCAs:             clientCAs,
		clientCertificateFile: clientCertificateFile,
		clientPrivateKeyFile:  clientPrivateKeyFile,
		serverCAFile:          serverCAFile,
	}
}

type notificationTestLeaf struct {
	Certificate    tls.Certificate
	CertificatePEM []byte
	PrivateKeyPEM  []byte
}

func newNotificationTestLeaf(
	t *testing.T,
	serial *big.Int,
	dnsName string,
	usage x509.ExtKeyUsage,
	ca *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	mutate ...func(*x509.Certificate),
) notificationTestLeaf {
	t.Helper()
	key := newNotificationTestPrivateKey(t)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    ca.NotBefore,
		NotAfter:     ca.NotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	for _, apply := range mutate {
		if apply != nil {
			apply(template)
		}
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return notificationTestLeaf{Certificate: certificate, CertificatePEM: certificatePEM, PrivateKeyPEM: privateKeyPEM}
}

func notificationSubjectAltNameExtension(t testing.TB, dnsName string, extraGeneralName []byte) pkix.Extension {
	t.Helper()
	dns, err := asn1.Marshal(asn1.RawValue{Class: 2, Tag: 2, Bytes: []byte(dnsName)})
	if err != nil {
		t.Fatal(err)
	}
	value, err := asn1.Marshal(asn1.RawValue{Tag: 16, IsCompound: true, Bytes: append(dns, extraGeneralName...)})
	if err != nil {
		t.Fatal(err)
	}
	return pkix.Extension{Id: asn1.ObjectIdentifier{2, 5, 29, 17}, Value: value}
}

func newNotificationTestPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
