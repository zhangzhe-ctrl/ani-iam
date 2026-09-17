package biz

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestRecoveryFingerprintBindsExactCanonicalIntent(t *testing.T) {
	tenant, target := uuid.MustParse("019ed06c-741b-7000-8000-000000000001"), uuid.MustParse("019ed06c-741b-7000-8000-000000000002")
	got, err := CanonicalRestoreTenantAdminFingerprint(tenant, target, "ADMIN_LOST")
	if err != nil || got != "sha256:ef544a7f8c0a1b1a1834a392750289aa04a4d96b0249df1d9e7773594ceace2a" {
		t.Fatal("canonical recovery fingerprint differs")
	}
	for _, input := range []struct {
		Tenant, Target uuid.UUID
		Reason         string
	}{{target, tenant, "ADMIN_LOST"}, {tenant, tenant, "ADMIN_LOST"}, {tenant, target, "IDENTITY_LOST"}} {
		other, err := CanonicalRestoreTenantAdminFingerprint(input.Tenant, input.Target, input.Reason)
		if err != nil || other == got {
			t.Fatal("changed recovery intent retained approval fingerprint")
		}
	}
	for _, reason := range []string{"", "admin_lost", "ADMIN LOST", "ADMIN_LOST\n", strings.Repeat("A", 129), "ADMIN_LOST\x00"} {
		if _, err := CanonicalRestoreTenantAdminFingerprint(tenant, target, reason); !errors.Is(err, ErrRecoveryInvalid) {
			t.Fatal("ambiguous recovery reason accepted")
		}
	}
	for _, id := range []uuid.UUID{uuid.Nil, uuid.New()} {
		if _, err := CanonicalRestoreTenantAdminFingerprint(id, target, "ADMIN_LOST"); !errors.Is(err, ErrRecoveryInvalid) {
			t.Fatal("noncanonical recovery Tenant accepted")
		}
	}
}
func TestRecoveryApprovalReferenceIsBoundedAndUnambiguous(t *testing.T) {
	for _, v := range []string{"CHANGE-2026-0001", "https://approval.example.test/change/1"} {
		if !validRecoveryReference(v) {
			t.Fatal("canonical external reference rejected")
		}
	}
	for _, v := range []string{"", " ref", "ref ", "ref\nline", "ref\x00", strings.Repeat("x", 257)} {
		if validRecoveryReference(v) {
			t.Fatal("ambiguous external approval reference accepted")
		}
	}
}
