package biz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	ErrFirstAdministratorInvalid  = errors.New("first administrator intent is invalid")
	ErrFirstAdministratorDenied   = errors.New("first administrator provisioner is not authorized")
	ErrFirstAdministratorConflict = errors.New("first administrator intent conflicts with existing state")
	ErrFirstAdministratorExpired  = errors.New("first administrator intent has expired")
)

// This is an owner-reviewed intent, never an authentication credential.
type FirstAdministratorManifest struct {
	Version     int       `json:"version"`
	IntentID    uuid.UUID `json:"intent_id"`
	Environment string    `json:"environment"`
	Email       string    `json:"email"`
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"`
	ExpiresAt   time.Time `json:"expires_at"`
	Supersedes  uuid.UUID `json:"supersedes,omitempty"`
}

type FirstAdministratorIntent struct {
	Manifest FirstAdministratorManifest
	Digest   [32]byte
	Now      time.Time
}

type FirstAdministratorReceipt struct {
	IntentID     uuid.UUID `json:"intent_id"`
	AuditEventID uuid.UUID `json:"audit_event_id"`
	IntentSHA256 string    `json:"intent_sha256"`
	RegisteredAt time.Time `json:"registered_at"`
}

type FirstAdministratorRepository interface {
	Register(context.Context, FirstAdministratorIntent) (FirstAdministratorReceipt, error)
}

type FirstAdministratorUsecase struct {
	repo  FirstAdministratorRepository
	clock Clock
}

func NewFirstAdministratorUsecase(repo FirstAdministratorRepository, clock Clock) *FirstAdministratorUsecase {
	return &FirstAdministratorUsecase{repo: repo, clock: clock}
}

func (u *FirstAdministratorUsecase) Register(ctx context.Context, environment string, manifest FirstAdministratorManifest) (FirstAdministratorReceipt, error) {
	if u == nil || u.repo == nil || u.clock == nil {
		return FirstAdministratorReceipt{}, ErrPersistenceUnavailable
	}
	intent, err := ValidateFirstAdministrator(environment, manifest, u.clock.Now())
	if err != nil {
		return FirstAdministratorReceipt{}, err
	}
	return u.repo.Register(ctx, intent)
}

func ValidateFirstAdministrator(environment string, m FirstAdministratorManifest, now time.Time) (FirstAdministratorIntent, error) {
	invalid := func() (FirstAdministratorIntent, error) {
		return FirstAdministratorIntent{}, ErrFirstAdministratorInvalid
	}
	validID := func(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
	if m.Version != 1 || !validID(m.IntentID) || !bootstrapName.MatchString(m.Environment) ||
		(m.Supersedes != uuid.Nil && (!validID(m.Supersedes) || m.Supersedes == m.IntentID)) ||
		m.ExpiresAt.IsZero() || m.ExpiresAt.After(now.Add(24*time.Hour)) {
		return invalid()
	}
	if environment != m.Environment {
		return FirstAdministratorIntent{}, ErrFirstAdministratorDenied
	}
	email, err := normalizeOIDCEmail(m.Email)
	if err != nil || email != m.Email || len(email) > 254 {
		return invalid()
	}
	issuer, err := url.Parse(m.Issuer)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || m.Issuer != strings.TrimSpace(m.Issuer) {
		return invalid()
	}
	// HTTP is allowed only on a literal loopback IP for isolated IdP tests.
	ip := net.ParseIP(issuer.Hostname())
	if issuer.Scheme != "https" && !(issuer.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return invalid()
	}
	if m.Subject == "" || len(m.Subject) > 255 || strings.TrimSpace(m.Subject) != m.Subject || strings.IndexFunc(m.Subject, unicode.IsControl) >= 0 {
		return invalid()
	}
	m.ExpiresAt = m.ExpiresAt.UTC()
	raw, err := json.Marshal(m)
	if err != nil {
		return invalid()
	}
	// PostgreSQL timestamps retain microseconds. Use the same precision in the
	// first receipt and every later read, including concurrent process retries.
	return FirstAdministratorIntent{Manifest: m, Digest: sha256.Sum256(raw), Now: now.UTC().Truncate(time.Microsecond)}, nil
}
