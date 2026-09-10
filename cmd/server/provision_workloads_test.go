package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestProvisionerFileBoundary(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "wr19-test"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	anchor := sha256.Sum256(der)
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	m := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: id(), Environment: "wr19", TrustDomain: "wr19.test", CASHA256: hex.EncodeToString(anchor[:]), ExpiresAt: now.Add(time.Minute), Workloads: []biz.BootstrapWorkload{{PrincipalID: id(), Name: "gateway", BindingID: id(), DNSIdentity: "gateway.wr19.test", Grants: []biz.BootstrapWorkloadGrant{{ID: id(), Audience: "ani-session-gateway", Operation: "session.create"}}}}}
	caPath, manifestPath, dsnPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "manifest.json"), filepath.Join(dir, "dsn")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dsnPath, []byte("postgres://invalid.invalid/test"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(m)
	read := func(raw []byte, env, approved string) error {
		t.Helper()
		if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if approved == "" {
			approved = hex.EncodeToString(digest[:])
		}
		_, err := readBootstrapFiles(manifestPath, approved, env, m.TrustDomain, caPath, dsnPath, now)
		return err
	}
	if err := read(raw, "wr19", ""); err != nil {
		t.Fatal(err)
	}
	if err := read(raw, "other", ""); err == nil {
		t.Fatal("self-reported environment accepted")
	}
	if err := read(raw, "wr19", strings.Repeat("0", 64)); err == nil {
		t.Fatal("unapproved bytes accepted")
	}
	unknown := append([]byte(`{"unknown":true,`), raw[1:]...)
	if err := read(unknown, "wr19", ""); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := read(append(raw, []byte(" {}")...), "wr19", ""); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if err := os.Chmod(dsnPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := read(raw, "wr19", ""); err == nil {
		t.Fatal("public credential file accepted")
	}
	target := filepath.Join(dir, "real-dsn")
	if err := os.Rename(dsnPath, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dsnPath); err != nil {
		t.Fatal(err)
	}
	if err := read(raw, "wr19", ""); err == nil {
		t.Fatal("symlink credential accepted")
	}
}
