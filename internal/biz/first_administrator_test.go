package biz

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFirstAdministratorExactIntentValidation(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	valid := FirstAdministratorManifest{Version: 1, IntentID: uuid.Must(uuid.NewV7()), Environment: "wr22", Email: "admin@example.test", Issuer: "https://idp.example.test", Subject: "exact-subject", ExpiresAt: now.Add(time.Hour)}
	a, err := ValidateFirstAdministrator("wr22", valid, now)
	if err != nil {
		t.Fatal(err)
	}
	zone := valid
	zone.ExpiresAt = zone.ExpiresAt.In(time.FixedZone("same-instant", 8*3600))
	b, err := ValidateFirstAdministrator("wr22", zone, now)
	if err != nil || a.Digest != b.Digest {
		t.Fatal("equivalent UTC instants must have the same digest")
	}
	for name, change := range map[string]func(*FirstAdministratorManifest){
		"noncanonical email": func(m *FirstAdministratorManifest) { m.Email = "Admin@example.test" },
		"invalid email":      func(m *FirstAdministratorManifest) { m.Email = "invalid" },
		"non-v7 ID":          func(m *FirstAdministratorManifest) { m.IntentID = uuid.New() },
		"self supersession":  func(m *FirstAdministratorManifest) { m.Supersedes = m.IntentID },
		"excess duration":    func(m *FirstAdministratorManifest) { m.ExpiresAt = now.Add(25 * time.Hour) },
		"issuer credentials": func(m *FirstAdministratorManifest) { m.Issuer = "https://user:secret@example.test" },
		"issuer query":       func(m *FirstAdministratorManifest) { m.Issuer += "?query=value" },
		"remote http":        func(m *FirstAdministratorManifest) { m.Issuer = "http://example.test" },
		"empty subject":      func(m *FirstAdministratorManifest) { m.Subject = "" },
		"subject whitespace": func(m *FirstAdministratorManifest) { m.Subject = " exact-subject" },
	} {
		t.Run(name, func(t *testing.T) {
			m := valid
			change(&m)
			if _, err := ValidateFirstAdministrator("wr22", m, now); !errors.Is(err, ErrFirstAdministratorInvalid) {
				t.Fatal("invalid owner intent accepted")
			}
		})
	}
	if _, err := ValidateFirstAdministrator("wrong", valid, now); !errors.Is(err, ErrFirstAdministratorDenied) {
		t.Fatal("manifest cannot choose the owner's environment")
	}
	// Persisted same-intent receipts can be read after expiration. The repository
	// distinguishes retry from new registration and rejects expired new intents.
	if _, err := ValidateFirstAdministrator("wr22", valid, now.Add(2*time.Hour)); err != nil {
		t.Fatal("receipt retry preempted before persisted-state check")
	}
}
