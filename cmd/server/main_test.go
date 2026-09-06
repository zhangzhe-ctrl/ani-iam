package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuntimeLoggerUsesKratosRedaction(t *testing.T) {
	var output bytes.Buffer
	logger := newRuntimeLogger(&output)
	logger.Info(
		"redaction check",
		"token", "secret-token",
		"args", "secret-payload",
		"postgresql.dsn", "secret-postgresql-dsn",
		"redis.password", "secret-redis-password",
		"private_key_file", "secret-key-path",
	)

	line := output.String()
	for _, secret := range []string{
		"secret-token",
		"secret-payload",
		"secret-postgresql-dsn",
		"secret-redis-password",
		"secret-key-path",
	} {
		if strings.Contains(line, secret) {
			t.Fatalf("runtime logger leaked filtered value %q: %s", secret, line)
		}
	}
	if strings.Count(line, `"***"`) != 5 {
		t.Fatalf("runtime logger leaked filtered values: %s", line)
	}
	if strings.Contains(Name, "cp0") || strings.Contains(Version, "cp0") {
		t.Fatalf("runtime identity remains on the historical CP0 profile: name=%q version=%q", Name, Version)
	}
}
