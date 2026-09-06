package data

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/alexedwards/argon2id"
)

func TestArgon2idPasswordHasherUsesFrozenPHCParameters(t *testing.T) {
	const password = "test-only password with spaces"
	hasher := NewArgon2idPasswordHasher()

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Fatalf("PHC field count = %d, hash = %q", len(parts), encoded)
	}
	if parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=4" {
		t.Fatalf("PHC algorithm/version/parameters = %q/%q/%q", parts[1], parts[2], parts[3])
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	tag, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Fatalf("decode tag: %v", err)
	}
	if len(salt) != 16 || len(tag) != 32 {
		t.Fatalf("salt/tag bytes = %d/%d, want 16/32", len(salt), len(tag))
	}
	if strings.Contains(encoded, password) {
		t.Fatal("encoded password contains plaintext")
	}

	valid, err := hasher.Verify(encoded, password)
	if err != nil || !valid {
		t.Fatalf("Verify(correct) = %v, %v", valid, err)
	}
	valid, err = hasher.Verify(encoded, "wrong password")
	if err != nil || valid {
		t.Fatalf("Verify(wrong) = %v, %v", valid, err)
	}
}

func TestArgon2idPasswordHasherRejectsMalformedPHC(t *testing.T) {
	hasher := NewArgon2idPasswordHasher()

	valid, err := hasher.Verify("not-a-phc-hash", "test-only password")
	if err == nil || valid {
		t.Fatalf("Verify(malformed) = %v, %v, want false and error", valid, err)
	}
}

func TestArgon2idPasswordHasherRejectsNonTargetParameters(t *testing.T) {
	encoded, err := argon2id.CreateHash("test-only password", &argon2id.Params{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	if err != nil {
		t.Fatalf("CreateHash(weak fixture) error = %v", err)
	}

	valid, err := NewArgon2idPasswordHasher().Verify(encoded, "test-only password")
	if err == nil || valid {
		t.Fatalf("Verify(non-target parameters) = %v, %v, want false and error", valid, err)
	}
}
