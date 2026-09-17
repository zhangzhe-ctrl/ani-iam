package workloadregistry

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func fixture() Document {
	return Document{Schema: Schema, Targets: []Target{
		{Audience: "sample-owner", Operation: "sample.receive", Mechanism: Receiver, GrantScope: "receiver", Enabled: true},
		{Audience: "sample-owner", Operation: "sample.submit", RPC: "/sample.v1.Owner/Submit", Mechanism: WorkloadOnly, GrantScope: "workload_only", ReceiverOperation: "sample.receive", Enabled: true},
		{Audience: "sample-owner", Operation: "sample.invoke", RPC: "/sample.v1.Owner/Invoke", Mechanism: Delegated, GrantScope: "delegated", ReceiverOperation: "sample.receive", Enabled: true, Sources: []Source{{Operation: "invokeSample", Mode: "execute", Resource: "samples", Action: "invoke", PrincipalKinds: []string{"human", "workload"}, CredentialKinds: []string{"access_token", "api_key"}, OwnerHandler: "sample.resource_tenant", OwnerCheck: "receiver"}}},
	}}
}
func parseFixture(t *testing.T, d Document) *Registry {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(raw, digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestRegistrationIsExactAndImmutable(t *testing.T) {
	r := parseFixture(t, fixture())
	if _, ok := r.Lookup("other", "sample.submit"); ok {
		t.Fatal("unknown audience accepted")
	}
	if _, ok := r.Method("/sample.v1.Owner/Unknown"); ok {
		t.Fatal("unknown RPC accepted")
	}
	target, ok := r.Method("/sample.v1.Owner/Invoke")
	if !ok || target.Operation != "sample.invoke" {
		t.Fatal(target)
	}
	s, ok := target.Source("invokeSample", "execute")
	if !ok || !s.AllowsSubject("human", "access_token") || s.AllowsSubject("workload", "workload_token") {
		t.Fatal(s)
	}
	if _, ok := target.Source("invokeSample", "changed"); ok {
		t.Fatal("mode mismatch")
	}
	target.Sources[0].PrincipalKinds[0] = "changed"
	copied, _ := r.Lookup("sample-owner", "sample.invoke")
	if !slices.Contains(copied.Sources[0].PrincipalKinds, "human") {
		t.Fatal("mutable registry escaped")
	}
}
func TestRejectedRegistrations(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*Document)
	}{
		{"unknown schema", func(d *Document) { d.Schema = "future" }},
		{"duplicate target", func(d *Document) { d.Targets = append(d.Targets, d.Targets[1]) }},
		{"duplicate RPC", func(d *Document) { d.Targets[2].RPC = d.Targets[1].RPC }},
		{"missing receiver", func(d *Document) { d.Targets[1].ReceiverOperation = "missing" }},
		{"caller as receiver", func(d *Document) { d.Targets[1].ReceiverOperation = "sample.invoke" }},
		{"disabled receiver", func(d *Document) { d.Targets[0].Enabled = false }},
		{"workload subject continuation", func(d *Document) { d.Targets[1].Continuation = true }},
		{"wildcard audience", func(d *Document) { d.Targets[1].Audience = "*" }},
		{"invalid mechanism", func(d *Document) { d.Targets[1].Mechanism = "script" }},
		{"RPC alias", func(d *Document) { d.Targets[1].RPC = "/sample.v1.Owner/Submit " }},
		{"receiver has RPC", func(d *Document) { d.Targets[0].RPC = "/sample.v1.Owner/Receive" }},
		{"direct operation mismatch", func(d *Document) { d.Targets[1].Mechanism = Direct }},
		{"duplicate source", func(d *Document) { d.Targets[2].Sources = append(d.Targets[2].Sources, d.Targets[2].Sources[0]) }},
		{"unknown credential", func(d *Document) { d.Targets[2].Sources[0].CredentialKinds = []string{"password"} }},
		{"unmatched credential principal", func(d *Document) { d.Targets[2].Sources[0].PrincipalKinds = []string{"human"} }},
		{"missing owner", func(d *Document) { d.Targets[2].Sources[0].OwnerCheck = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := fixture()
			tt.mutate(&d)
			raw, _ := json.Marshal(d)
			if _, err := Parse(raw, digest(raw)); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}
func TestExactBytesAndJSONInterpretation(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	for _, bad := range [][]byte{
		[]byte(strings.Replace(string(raw), `"schema":`, `"unknown":0,"schema":`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":`, `"schema":"shadow","schema":`, 1)),
		[]byte(strings.Replace(string(raw), `"audience":`, `"audience":"shadow","audience":`, 1)),
		append(slices.Clone(raw), []byte(` {}`)...),
		[]byte(`null`),
	} {
		if _, err := Parse(bad, digest(bad)); err == nil {
			t.Fatal("ambiguous or unsupported JSON accepted")
		}
	}
	if _, err := Parse(append(raw, ' '), digest(raw)); err == nil {
		t.Fatal("byte digest mismatch accepted")
	}
}
func TestTargetVersionHasLocalScope(t *testing.T) {
	d := fixture()
	before := parseFixture(t, d)
	d.Targets = append(d.Targets, Target{Audience: "next-owner", Operation: "/next.v1.Owner/Direct", RPC: "/next.v1.Owner/Direct", Mechanism: Direct, GrantScope: "direct", Enabled: true})
	after := parseFixture(t, d)
	if before.Digest() == after.Digest() || before.Revision("sample-owner", "sample.invoke") != after.Revision("sample-owner", "sample.invoke") {
		t.Fatal("unrelated registration changed existing target revision")
	}
	d.Targets[2].Enabled = false
	disabled := parseFixture(t, d)
	if disabled.Revision("sample-owner", "sample.invoke") == before.Revision("sample-owner", "sample.invoke") {
		t.Fatal("disabled target retained revision")
	}
}

func TestHTTPExactTransportAndExistingTargetRevision(t *testing.T) {
	before := parseFixture(t, fixture())
	d := fixture()
	target := Target{Audience: "sample-owner", Operation: "sample.read", HTTPMethod: "GET", HTTPPath: "/api/v1/internal/snapshot", Mechanism: WorkloadOnly, GrantScope: "read", ReceiverOperation: "sample.receive", Enabled: true}
	d.Targets = append(d.Targets, target)
	after := parseFixture(t, d)
	for _, old := range before.Targets() {
		if before.Revision(old.Audience, old.Operation) != after.Revision(old.Audience, old.Operation) {
			t.Fatal("unrelated target revision changed")
		}
	}
	if got, ok := after.HTTP(target.Audience, "GET", target.HTTPPath); !ok || got.Operation != target.Operation {
		t.Fatal("HTTP target absent")
	}
	if _, ok := after.HTTP(target.Audience, "POST", target.HTTPPath); ok {
		t.Fatal("method fallback")
	}
	if _, ok := after.Method(target.HTTPPath); ok {
		t.Fatal("HTTP endpoint masquerades as RPC")
	}
	for _, mutate := range []func(*Target){
		func(v *Target) { v.RPC = "/sample.v1.Owner/Other" },
		func(v *Target) { v.HTTPMethod = "get" },
		func(v *Target) { v.HTTPMethod = "DELETE" },
		func(v *Target) { v.HTTPPath = "/api//v1" },
		func(v *Target) { v.HTTPPath = "/api/../v1" },
		func(v *Target) { v.HTTPPath = "/api/%73napshot" },
		func(v *Target) { v.HTTPPath = "/api/{cursor}" },
		func(v *Target) { v.HTTPPath = "/api/snapshot?cursor=x" },
		func(v *Target) { v.HTTPPath = "/api/*" },
		func(v *Target) { v.Mechanism = Delegated },
		func(v *Target) { v.Mechanism = Receiver },
	} {
		bad := d
		bad.Targets = append([]Target(nil), d.Targets...)
		mutate(&bad.Targets[len(bad.Targets)-1])
		raw, _ := json.Marshal(bad)
		if _, err := Parse(raw, digest(raw)); err == nil {
			t.Fatal("ambiguous HTTP target accepted", bad.Targets[len(bad.Targets)-1])
		}
	}
	duplicate := d
	duplicate.Targets = append(append([]Target(nil), d.Targets...), target)
	duplicate.Targets[len(duplicate.Targets)-1].Operation = "sample.other"
	raw, _ := json.Marshal(duplicate)
	if _, err := Parse(raw, digest(raw)); err == nil {
		t.Fatal("duplicate HTTP endpoint admitted")
	}
}

func TestHTTPPathIsScopedToAudience(t *testing.T) {
	d := fixture()
	for _, audience := range []string{"owner-one", "owner-two"} {
		d.Targets = append(d.Targets, Target{Audience: audience, Operation: "receive", Mechanism: Receiver, GrantScope: "receiver", Enabled: true}, Target{Audience: audience, Operation: "read", HTTPMethod: "GET", HTTPPath: "/api/v1/snapshot", Mechanism: WorkloadOnly, GrantScope: "read", Enabled: true, ReceiverOperation: "receive"})
	}
	r := parseFixture(t, d)
	for _, audience := range []string{"owner-one", "owner-two"} {
		target, ok := r.HTTP(audience, "GET", "/api/v1/snapshot")
		if !ok || target.Audience != audience {
			t.Fatal("HTTP owner boundary lost")
		}
	}
	if _, ok := r.HTTP("unregistered", "GET", "/api/v1/snapshot"); ok {
		t.Fatal("unknown owner admitted")
	}
}
