package workloadregistry

import (
	"encoding/json"
	"slices"
	"testing"
)

func authorityFixture() Document {
	d := fixture()
	for _, op := range []string{"snapshot.begin", "snapshot.page"} {
		d.Targets = append(d.Targets, Target{Audience: "sample-owner", Operation: op, HTTPMethod: "POST", HTTPPath: "/api/" + op, Mechanism: WorkloadOnly, GrantScope: "read", Enabled: true, ReceiverOperation: "sample.receive", AuthorityOperations: []string{"snapshot.begin", "snapshot.page"}})
	}
	return d
}
func TestAuthorityGroupIsFiniteExactAndImmutable(t *testing.T) {
	before := parseFixture(t, fixture())
	after := parseFixture(t, authorityFixture())
	for _, old := range before.Targets() {
		if before.Revision(old.Audience, old.Operation) != after.Revision(old.Audience, old.Operation) {
			t.Fatal("unrelated target revision changed")
		}
	}
	target, _ := after.Lookup("sample-owner", "snapshot.begin")
	target.AuthorityOperations[0] = "changed"
	all := after.Targets()
	for i := range all {
		if len(all[i].AuthorityOperations) > 0 {
			all[i].AuthorityOperations[0] = "changed"
		}
	}
	target, _ = after.Lookup("sample-owner", "snapshot.begin")
	if !slices.Equal(target.AuthorityOperations, []string{"snapshot.begin", "snapshot.page"}) {
		t.Fatal("mutable authority escaped")
	}
	for _, tc := range []struct {
		name   string
		change func(*Document)
	}{
		{"missing member", func(d *Document) { d.Targets = d.Targets[:4] }},
		{"asymmetric", func(d *Document) { d.Targets[4].AuthorityOperations = nil }},
		{"reversed", func(d *Document) { d.Targets[3].AuthorityOperations = []string{"snapshot.page", "snapshot.begin"} }},
		{"duplicate", func(d *Document) { d.Targets[3].AuthorityOperations = []string{"snapshot.begin", "snapshot.begin"} }},
		{"self absent", func(d *Document) { d.Targets[3].AuthorityOperations = []string{"a", "b"} }},
		{"single", func(d *Document) { d.Targets[3].AuthorityOperations = []string{"snapshot.begin"} }},
		{"arbitrary group", func(d *Document) {
			d.Targets[3].AuthorityOperations = []string{"snapshot.begin", "snapshot.page", "snapshot.third"}
		}},
		{"disabled", func(d *Document) { d.Targets[4].Enabled = false }},
		{"different audience", func(d *Document) { d.Targets[4].Audience = "different" }},
		{"different receiver", func(d *Document) {
			d.Targets = append(d.Targets, Target{Audience: "sample-owner", Operation: "other.receive", Mechanism: Receiver, GrantScope: "receiver", Enabled: true})
			d.Targets[4].ReceiverOperation = "other.receive"
		}},
		{"RPC group", func(d *Document) {
			d.Targets[3].HTTPMethod = ""
			d.Targets[3].HTTPPath = ""
			d.Targets[3].RPC = "/sample.v1.Owner/Begin"
		}},
		{"receiver group", func(d *Document) { d.Targets[0].AuthorityOperations = []string{"sample.receive", "snapshot.begin"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := authorityFixture()
			tc.change(&d)
			raw, _ := json.Marshal(d)
			if _, err := Parse(raw, digest(raw)); err == nil {
				t.Fatal("invalid group admitted")
			}
		})
	}
}
