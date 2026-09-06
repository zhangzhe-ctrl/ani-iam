package data

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEd25519PrivateKeyFile(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	path := writePKCS8PrivateKey(t, privateKey)

	loaded, err := LoadEd25519PrivateKeyFile(path)
	if err != nil {
		t.Fatalf("LoadEd25519PrivateKeyFile() error = %v", err)
	}
	if !bytes.Equal(loaded, privateKey) {
		t.Fatal("LoadEd25519PrivateKeyFile() changed private key bytes")
	}
}

func TestLoadEd25519PrivateKeyFileRejectsUnsafeInput(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	_, ed25519Key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	tests := []struct {
		name     string
		contents []byte
	}{
		{name: "malformed PEM", contents: []byte("not a private key")},
		{name: "RSA instead of Ed25519", contents: marshalPKCS8PEM(t, rsaKey)},
		{name: "trailing data", contents: append(marshalPKCS8PEM(t, ed25519Key), []byte("unexpected")...)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "access-token-key.pem")
			if err := os.WriteFile(path, test.contents, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := LoadEd25519PrivateKeyFile(path); err == nil {
				t.Fatal("LoadEd25519PrivateKeyFile() unexpectedly succeeded")
			}
		})
	}
}

func writePKCS8PrivateKey(t *testing.T, key any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "access-token-key.pem")
	if err := os.WriteFile(path, marshalPKCS8PEM(t, key), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func marshalPKCS8PEM(t *testing.T, key any) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}
