package biz

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCoreBootstrapCanonicalIntent(t *testing.T) {
	for _, vector := range []struct{ email, digest, encoded string }{
		{"admin@example.test", "47297b1d7149d5ed5f3ab22efdb28270cc47cc4d3b39c1ebac9f0d6383a7653e", "admin@example.test"},
		{"a&b@example.test", "d0a15042f230f162dd35b10ea1a659047a2ffc6a68590d8baa25110dd834a4ab", `a\u0026b@example.test`},
	} {
		t.Run(vector.email, func(t *testing.T) {
			i := CoreBootstrapIntent{TenantID: uuid.MustParse("0198f062-b76d-7f2a-b0ad-50a417bf1f70"), OperationID: uuid.MustParse("0198f062-b76d-7892-b978-baa342c53020"), NormalizedEmail: vector.email, Locale: "en-US", Fingerprint: "sha256:" + vector.digest}
			raw, err := i.CanonicalPayload()
			want := `{"locale":"en-US","normalized_email":"` + vector.encoded + `","operation_id":"0198f062-b76d-7892-b978-baa342c53020","tenant_id":"0198f062-b76d-7f2a-b0ad-50a417bf1f70"}`
			if err != nil || string(raw) != want {
				t.Fatalf("canonical intent mismatch: %v", err)
			}
			for name, change := range map[string]func(*CoreBootstrapIntent){
				"different tenant":         func(i *CoreBootstrapIntent) { i.TenantID = uuid.MustParse("0198f062-b76d-77da-98fa-65f26fc01e17") },
				"different operation":      func(i *CoreBootstrapIntent) { i.OperationID = i.TenantID },
				"unverified normalization": func(i *CoreBootstrapIntent) { i.NormalizedEmail = " Admin@example.test " },
				"different locale":         func(i *CoreBootstrapIntent) { i.Locale = "zh-CN" },
				"unknown locale":           func(i *CoreBootstrapIntent) { i.Locale = "unknown" },
			} {
				t.Run(name, func(t *testing.T) {
					bad := i
					change(&bad)
					if _, err := bad.CanonicalPayload(); !errors.Is(err, ErrCoreBootstrapInvalid) {
						t.Fatal("changed identity accepted")
					}
				})
			}
		})
	}
}
