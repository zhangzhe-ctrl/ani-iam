package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func TestFirstAdministratorProvisionerFiles(t *testing.T) {
	dir := t.TempDir()
	manifestPath, dsnPath := filepath.Join(dir, "intent.json"), filepath.Join(dir, "dsn")
	now := time.Now()
	m := biz.FirstAdministratorManifest{Version: 1, IntentID: uuid.Must(uuid.NewV7()), Environment: "wr22", Email: "admin@example.test", Issuer: "https://idp.example.test", Subject: "exact-subject", ExpiresAt: now.Add(time.Hour)}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(dsnPath, []byte("postgres://invalid.invalid/test"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(raw []byte, environment, digest string) error {
		t.Helper()
		if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if digest == "" {
			sum := sha256.Sum256(raw)
			digest = hex.EncodeToString(sum[:])
		}
		_, _, err := readFirstAdministratorFiles(manifestPath, digest, environment, dsnPath, now)
		return err
	}
	if err := read(raw, "wr22", ""); err != nil {
		t.Fatal(err)
	}
	if read(raw, "wrong", "") == nil {
		t.Fatal("wrong owner accepted")
	}
	if read(raw, "wr22", strings.Repeat("0", 64)) == nil {
		t.Fatal("wrong exact digest accepted")
	}
	if read(append([]byte(`{"unknown":true,`), raw[1:]...), "wr22", "") == nil {
		t.Fatal("unknown manifest field accepted")
	}
	if read(append(raw, []byte(" {}")...), "wr22", "") == nil {
		t.Fatal("trailing JSON accepted")
	}
	if err := os.Chmod(dsnPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if read(raw, "wr22", "") == nil {
		t.Fatal("public provisioner credential file accepted")
	}
}
