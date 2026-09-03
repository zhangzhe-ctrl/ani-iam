package data

import "testing"

func TestValidateRedisNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantError bool
	}{
		{name: "isolated", namespace: "cp0:04:run-123:"},
		{name: "empty", namespace: "", wantError: true},
		{name: "wrong issue", namespace: "cp0:03:run-123:", wantError: true},
		{name: "missing run id", namespace: "cp0:04::", wantError: true},
		{name: "missing delimiter", namespace: "cp0:04:run-123", wantError: true},
		{name: "glob", namespace: "cp0:04:run-*:", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRedisNamespace(test.namespace)
			if (err != nil) != test.wantError {
				t.Fatalf("validateRedisNamespace(%q) error = %v", test.namespace, err)
			}
		})
	}
}
