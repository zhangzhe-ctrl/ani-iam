package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrWorkloadBootstrapInvalid  = errors.New("Workload bootstrap input is invalid")
	ErrWorkloadBootstrapConflict = errors.New("Workload bootstrap conflicts with an existing intent or identity")
	ErrWorkloadBootstrapDenied   = errors.New("Workload bootstrap owner is not authorized")
	ErrWorkloadBootstrapExpired  = errors.New("Workload bootstrap intent has expired")
)

// WorkloadBootstrapManifest contains no credentials. Its authority comes from
// the separately authenticated provisioner, never from this document itself.
type WorkloadBootstrapManifest struct {
	Version     int                 `json:"version"`
	ManifestID  uuid.UUID           `json:"manifest_id"`
	Environment string              `json:"environment"`
	TrustDomain string              `json:"trust_domain"`
	CASHA256    string              `json:"ca_sha256"`
	ExpiresAt   time.Time           `json:"expires_at"`
	Workloads   []BootstrapWorkload `json:"workloads"`
}

type BootstrapWorkload struct {
	PrincipalID uuid.UUID                `json:"principal_id"`
	Name        string                   `json:"name"`
	BindingID   uuid.UUID                `json:"binding_id"`
	DNSIdentity string                   `json:"dns_identity"`
	Grants      []BootstrapWorkloadGrant `json:"grants"`
}

type BootstrapWorkloadGrant struct {
	ID        uuid.UUID `json:"id"`
	Audience  string    `json:"audience"`
	Operation string    `json:"operation"`
}

type WorkloadBootstrapOwner struct {
	Environment string
	TrustDomain string
	CASHA256    string
}

type WorkloadBootstrapIntent struct {
	Manifest WorkloadBootstrapManifest
	Digest   [32]byte
	Now      time.Time
}

// The receipt proves only consumption of this intent. It is not a credential
// and does not assert that these registrations remain enabled today.
type WorkloadBootstrapReceipt struct {
	ManifestID   uuid.UUID   `json:"manifest_id"`
	IntentSHA256 string      `json:"intent_sha256"`
	PrincipalIDs []uuid.UUID `json:"principal_ids"`
	CompletedAt  time.Time   `json:"completed_at"`
}

type WorkloadBootstrapRepository interface {
	Provision(context.Context, WorkloadBootstrapIntent) (WorkloadBootstrapReceipt, error)
}

type WorkloadBootstrap struct {
	repo  WorkloadBootstrapRepository
	clock Clock
}

func NewWorkloadBootstrap(repo WorkloadBootstrapRepository, clock Clock) *WorkloadBootstrap {
	return &WorkloadBootstrap{repo: repo, clock: clock}
}

var bootstrapName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var bootstrapDNS = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

func (u *WorkloadBootstrap) Provision(ctx context.Context, owner WorkloadBootstrapOwner, manifest WorkloadBootstrapManifest) (WorkloadBootstrapReceipt, error) {
	if u == nil || u.repo == nil || u.clock == nil {
		return WorkloadBootstrapReceipt{}, ErrPersistenceUnavailable
	}
	intent, err := ValidateWorkloadBootstrap(owner, manifest, u.clock.Now())
	if err != nil {
		return WorkloadBootstrapReceipt{}, err
	}
	return u.repo.Provision(ctx, intent)
}

// Validation canonicalizes only ordering and UTC time. Names, identities and
// targets must already be canonical, avoiding ambiguous grants or aliases.
func ValidateWorkloadBootstrap(owner WorkloadBootstrapOwner, m WorkloadBootstrapManifest, now time.Time) (WorkloadBootstrapIntent, error) {
	invalid := func() (WorkloadBootstrapIntent, error) { return WorkloadBootstrapIntent{}, ErrWorkloadBootstrapInvalid }
	if m.Version != 1 || m.ManifestID.Version() != 7 || m.ManifestID.Variant() != uuid.RFC4122 ||
		!bootstrapName.MatchString(m.Environment) || !bootstrapDNS.MatchString(m.TrustDomain) || len(m.TrustDomain) > 253 ||
		m.ExpiresAt.IsZero() || len(m.Workloads) < 1 || len(m.Workloads) > 16 {
		return invalid()
	}
	ca, err := hex.DecodeString(m.CASHA256)
	if err != nil || len(ca) != sha256.Size || m.CASHA256 != strings.ToLower(m.CASHA256) {
		return invalid()
	}
	if owner.Environment != m.Environment || owner.TrustDomain != m.TrustDomain || owner.CASHA256 != m.CASHA256 {
		return WorkloadBootstrapIntent{}, ErrWorkloadBootstrapDenied
	}
	// Expiry of an already consumed intent is checked only after receipt lookup.
	if m.ExpiresAt.After(now.Add(24 * time.Hour)) {
		return invalid()
	}
	ids := map[uuid.UUID]bool{m.ManifestID: true}
	names := map[string]bool{}
	dns := map[string]bool{}
	uniqueID := func(id uuid.UUID) bool {
		if id.Version() != 7 || id.Variant() != uuid.RFC4122 || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	m.Workloads = slices.Clone(m.Workloads)
	for i := range m.Workloads {
		w := &m.Workloads[i]
		if !uniqueID(w.PrincipalID) || !uniqueID(w.BindingID) || !bootstrapName.MatchString(w.Name) || names[w.Name] ||
			!bootstrapDNS.MatchString(w.DNSIdentity) || len(w.DNSIdentity) > 253 || !strings.HasSuffix(w.DNSIdentity, "."+m.TrustDomain) || dns[w.DNSIdentity] || len(w.Grants) < 1 || len(w.Grants) > 32 {
			return invalid()
		}
		names[w.Name] = true
		dns[w.DNSIdentity] = true
		targets := map[string]bool{}
		w.Grants = slices.Clone(w.Grants)
		for _, g := range w.Grants {
			key := g.Audience + "\x00" + g.Operation
			if !uniqueID(g.ID) || !bootstrapGrantAllowed(g) || targets[key] {
				return invalid()
			}
			targets[key] = true
		}
		slices.SortFunc(w.Grants, func(a, b BootstrapWorkloadGrant) int { return strings.Compare(a.ID.String(), b.ID.String()) })
	}
	slices.SortFunc(m.Workloads, func(a, b BootstrapWorkload) int {
		return strings.Compare(a.PrincipalID.String(), b.PrincipalID.String())
	})
	m.ExpiresAt = m.ExpiresAt.UTC()
	encoded, err := json.Marshal(m)
	if err != nil {
		return invalid()
	}
	return WorkloadBootstrapIntent{Manifest: m, Digest: sha256.Sum256(encoded), Now: now.UTC()}, nil
}

func bootstrapGrantAllowed(g BootstrapWorkloadGrant) bool {
	if g.Audience == "ani-session-gateway" {
		return g.Operation == "session.create"
	}
	if g.Audience != "ani-iam" {
		return false
	}
	switch g.Operation {
	case "/iam.v1.AuthenticationService/ValidatePrincipal", "/iam.v1.AuthenticationService/PasswordLogin", "/iam.v1.AuthenticationService/IssueWorkloadToken",
		"/iam.v1.AuthenticationService/IssueDelegation", "/iam.v1.AuthorizationService/CheckPermission",
		"/iam.v1.AuthorizationService/VerifyWorkloadInvocation", VerifySessionContinuationRPC,
		"/grpc.health.v1.Health/Check":
		return true
	}
	return false
}
