package biz

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestBootstrapRecoveryCannotReuseRestoreFingerprint(t *testing.T) {
	tenant, target := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	bootstrap, err := CanonicalRecoveryBootstrapFingerprint(tenant, target, "BOOTSTRAP_LOST")
	if err != nil {
		t.Fatal(err)
	}
	restore, err := CanonicalRestoreTenantAdminFingerprint(tenant, target, "BOOTSTRAP_LOST")
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap == restore || !strings.HasPrefix(bootstrap, "sha256:") {
		t.Fatal("recovery purpose is not hash-bound")
	}
	changed, _ := CanonicalRecoveryBootstrapFingerprint(tenant, uuid.Must(uuid.NewV7()), "BOOTSTRAP_LOST")
	if changed == bootstrap {
		t.Fatal("target identity not hash-bound")
	}
	if _, err = CanonicalRecoveryBootstrapFingerprint(tenant, target, "reason\ninjection"); err == nil {
		t.Fatal("ambiguous recovery reason accepted")
	}
}
