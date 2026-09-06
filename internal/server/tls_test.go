package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
)

func TestLoadMutualTLSServerConfigRequiresVerifiedTLS13Clients(t *testing.T) {
	directory := t.TempDir()
	caCertificate, caKey := newTestCertificateAuthority(t)
	certificateFile, privateKeyFile := writeTestLeafCertificate(t, directory, caCertificate, caKey)
	clientCAFile := filepath.Join(directory, "client-ca.pem")
	writePEMFile(t, clientCAFile, "CERTIFICATE", caCertificate.Raw, 0o600)

	config, err := LoadMutualTLSServerConfig(&conf.Server_GRPC_TLS{
		CertificateFile: certificateFile,
		PrivateKeyFile:  privateKeyFile,
		ClientCaFile:    clientCAFile,
	})
	if err != nil {
		t.Fatalf("LoadMutualTLSServerConfig() error = %v", err)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Fatalf("TLS minimum version = %x, want TLS 1.3", config.MinVersion)
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert", config.ClientAuth)
	}
	if len(config.Certificates) != 1 || config.ClientCAs == nil || len(config.ClientCAs.Subjects()) != 1 {
		t.Fatalf("mTLS trust material = certificates:%d clientCAs:%v", len(config.Certificates), config.ClientCAs)
	}
}

func newTestCertificateAuthority(t *testing.T) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "DP2-05 test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, privateKey
}

func writeTestLeafCertificate(
	t *testing.T,
	directory string,
	caCertificate *x509.Certificate,
	caKey ed25519.PrivateKey,
) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ani-iam-service"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, publicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificateFile := filepath.Join(directory, "server.pem")
	privateKeyFile := filepath.Join(directory, "server-key.pem")
	writePEMFile(t, certificateFile, "CERTIFICATE", der, 0o600)
	writePEMFile(t, privateKeyFile, "PRIVATE KEY", privateKeyDER, 0o600)
	return certificateFile, privateKeyFile
}

func writePEMFile(t *testing.T, path, blockType string, contents []byte, mode os.FileMode) {
	t.Helper()
	encoded := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: contents})
	if err := os.WriteFile(path, encoded, mode); err != nil {
		t.Fatal(err)
	}
}
