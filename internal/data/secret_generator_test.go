package data

import (
	"encoding/base64"
	"testing"
)

func TestSecretGeneratorReturnsIndependent256BitOpaqueValues(t *testing.T) {
	generator := NewSecretGenerator()
	first, err := generator.NewSecret()
	if err != nil {
		t.Fatalf("NewSecret() first error = %v", err)
	}
	second, err := generator.NewSecret()
	if err != nil {
		t.Fatalf("NewSecret() second error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("decode opaque secret: %v", err)
	}
	if len(decoded) != 32 || first == second {
		t.Fatalf("secret bytes=%d equal=%t", len(decoded), first == second)
	}
}
