package tenantbootstrap

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func request(t *testing.T) *iamv1.TenantBootstrapRequested {
	t.Helper()
	r := &iamv1.TenantBootstrapRequested{SchemaRevision: Revision, EventId: "01994f20-0000-7000-8000-000000000001", Producer: "ani-governance", TenantId: "01994f20-0000-7000-8000-000000000002", OperationId: "01994f20-0000-7000-8000-000000000003", SourceEpoch: "01994f20-0000-7000-8000-000000000004", LifecycleSequence: 1, OccurredAt: timestamppb.New(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)), IntendedAdministrator: &iamv1.TenantBootstrapAdministrator{NormalizedEmail: "admin@example.com", Locale: "zh-CN"}}
	var err error
	r.PayloadFingerprint, err = Fingerprint(r.TenantId, r.OperationId, r.IntendedAdministrator.NormalizedEmail, r.IntendedAdministrator.Locale)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoundTripAndIntentBinding(t *testing.T) {
	r := request(t)
	raw, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Parse(raw)
	if err != nil || !proto.Equal(r, decoded) {
		t.Fatalf("round trip: %v", err)
	}
	for _, mutate := range []func(*iamv1.TenantBootstrapRequested){
		func(r *iamv1.TenantBootstrapRequested) { r.TenantId = "01994f20-0000-7000-8000-000000000005" },
		func(r *iamv1.TenantBootstrapRequested) { r.OperationId = "01994f20-0000-7000-8000-000000000005" },
		func(r *iamv1.TenantBootstrapRequested) { r.IntendedAdministrator.NormalizedEmail = "other@example.com" },
		func(r *iamv1.TenantBootstrapRequested) { r.IntendedAdministrator.Locale = "en-US" },
	} {
		changed := proto.Clone(r).(*iamv1.TenantBootstrapRequested)
		mutate(changed)
		if Validate(changed) == nil {
			t.Fatal("changed identity intent accepted")
		}
	}
}

func TestRejectAmbiguousOrLegacyWire(t *testing.T) {
	raw, err := Marshal(request(t))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	text := compact.String()
	invalid := []string{
		strings.Replace(text, `"schema_revision":`, `"schema_revision":"iam-tenant-bootstrap-v1","schema_revision":`, 1),
		strings.Replace(text, `"schema_revision"`, `"schemaRevision"`, 1),
		strings.Replace(text, `"normalized_email"`, `"normalizedEmail"`, 1),
		strings.Replace(text, `"lifecycle_sequence":"1"`, `"lifecycle_sequence":1`, 1),
		strings.Replace(text, `"iam-tenant-bootstrap-v1"`, `"core-v1"`, 1),
		strings.Replace(text, `"normalized_email":"admin@example.com"`, `"normalized_email":"Admin@example.com"`, 1),
		text + `{}`, `{}`, `null`, strings.Repeat(" ", MaxPayloadBytes+1),
	}
	for i, value := range invalid {
		if value == text {
			t.Fatalf("invalid vector %d did not alter input", i)
		}
		if _, err := Parse([]byte(value)); err == nil {
			t.Fatalf("invalid vector %d accepted", i)
		}
	}
}

func TestNormalizeInvitationEmail(t *testing.T) {
	for input, want := range map[string]string{" Admin@EXAMPLE.com ": "admin@example.com", "USER@例子.测试": "user@xn--fsqu00a.xn--0zwm56d"} {
		actual, err := NormalizeEmail(input)
		if err != nil || actual != want {
			t.Fatalf("normalization %q: %q %v", input, actual, err)
		}
	}
	for _, input := range []string{"Admin <a@example.com>", "a b@example.com", "a@@example.com", "a@"} {
		if _, err := NormalizeEmail(input); err == nil {
			t.Fatalf("invalid email accepted %q", input)
		}
	}
}
