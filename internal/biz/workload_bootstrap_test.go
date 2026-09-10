package biz_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func bootstrapManifest(t *testing.T) (biz.WorkloadBootstrapOwner, biz.WorkloadBootstrapManifest, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	owner := biz.WorkloadBootstrapOwner{Environment: "wr19", TrustDomain: "wr19.test", CASHA256: strings.Repeat("a", 64)}
	manifest := biz.WorkloadBootstrapManifest{Version: 1, ManifestID: id(), Environment: owner.Environment, TrustDomain: owner.TrustDomain, CASHA256: owner.CASHA256, ExpiresAt: now.Add(time.Hour), Workloads: []biz.BootstrapWorkload{
		{PrincipalID: id(), Name: "gateway", BindingID: id(), DNSIdentity: "gateway.wr19.test", Grants: []biz.BootstrapWorkloadGrant{
			{ID: id(), Audience: "ani-iam", Operation: "/iam.v1.AuthenticationService/PasswordLogin"},
			{ID: id(), Audience: "ani-session-gateway", Operation: "session.create"},
		}},
	}}
	return owner, manifest, now
}

func TestBootstrapRejectsAuthorityAndIdentityAmbiguity(t *testing.T) {
	cases := map[string]func(*biz.WorkloadBootstrapOwner, *biz.WorkloadBootstrapManifest){
		"self reported environment": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) { m.Environment = "other" },
		"self reported trust anchor": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.CASHA256 = strings.Repeat("b", 64)
		},
		"wildcard identity": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].DNSIdentity = "*.wr19.test"
		},
		"foreign identity domain": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].DNSIdentity = "gateway.other.test"
		},
		"identity name alias": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].Name = " Gateway"
		},
		"wildcard authority": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].Grants[0].Operation = "*"
		},
		"unknown service authority": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].Grants[1].Audience = "notification"
		},
		"duplicate authority": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].Grants[1].Audience = m.Workloads[0].Grants[0].Audience
			m.Workloads[0].Grants[1].Operation = m.Workloads[0].Grants[0].Operation
		},
		"reused identity id": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.Workloads[0].BindingID = m.Workloads[0].PrincipalID
		},
		"unbounded expiry": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) {
			m.ExpiresAt = m.ExpiresAt.Add(48 * time.Hour)
		},
		"unknown version": func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) { m.Version = 2 },
		"empty batch":     func(o *biz.WorkloadBootstrapOwner, m *biz.WorkloadBootstrapManifest) { m.Workloads = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			o, m, now := bootstrapManifest(t)
			change(&o, &m)
			_, err := biz.ValidateWorkloadBootstrap(o, m, now)
			if err == nil {
				t.Fatal("accepted ambiguous or unauthorized bootstrap")
			}
		})
	}
}

func TestBootstrapIntentCanonicalizationAndReceiptExpiry(t *testing.T) {
	o, m, now := bootstrapManifest(t)
	a, err := biz.ValidateWorkloadBootstrap(o, m, now)
	if err != nil {
		t.Fatal(err)
	}
	m.Workloads[0].Grants[0], m.Workloads[0].Grants[1] = m.Workloads[0].Grants[1], m.Workloads[0].Grants[0]
	m.ExpiresAt = m.ExpiresAt.In(time.FixedZone("test", 3600))
	b, err := biz.ValidateWorkloadBootstrap(o, m, now)
	if err != nil || a.Digest != b.Digest {
		t.Fatal("ordering/timezone changed intent")
	}
	// Domain validation preserves the same intent after expiry; only a persisted
	// receipt can allow its retry. The repository rejects expired new intents.
	c, err := biz.ValidateWorkloadBootstrap(o, m, now.Add(2*time.Hour))
	if err != nil || c.Digest != a.Digest {
		t.Fatal("completed receipt cannot be retried after expiry")
	}
	m.Workloads[0].Grants[0].Operation = "/iam.v1.AuthorizationService/CheckPermission"
	_, err = biz.ValidateWorkloadBootstrap(o, m, now)
	if err != nil && !errors.Is(err, biz.ErrWorkloadBootstrapInvalid) {
		t.Fatal(err)
	}
}
