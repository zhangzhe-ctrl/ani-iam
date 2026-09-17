package main

import (
	"bytes"
	"strings"
	"testing"
)

const ownerPolicyFixture = `openapi: 3.0.3
security: [{workloadToken: []}]
paths:
  /v1/widgets:
    post:
      operationId: CreateWidget
      x-ani-workload-operation: widget.create
      security: [{workloadToken: [], BearerAuth: []}]
      x-ani-auth-classification: authorized
      x-ani-authn: {principal_kinds: [human], credential_kinds: [access_token]}
      x-ani-authz: {version: v1, resource: owner.widgets, actions: [create], scope: platform, obligations: []}
  /internal/snapshot:
    post:
      operationId: BeginSnapshot
      x-ani-operation: widget.snapshot
      x-ani-audience: owner
      x-ani-boundary: workload-only
`

func TestOwnerOpenAPIGeneratesDeclaredPermissionAndSeparateWorkloadRoute(t *testing.T) {
	generated, migration, err := generateOwnerPolicies([]byte(ownerPolicyFixture), strings.Repeat("a", 64), "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`Operation: "widget.create"`, `OperationID: "CreateWidget"`, `Resource: "owner.widgets"`, `biz.PermissionScopePlatform`, `biz.PrincipalTypeHuman`, `biz.CredentialKindAccessToken`} {
		if !bytes.Contains(generated, []byte(required)) {
			t.Fatalf("missing %s in %s", required, generated)
		}
	}
	if bytes.Contains(generated, []byte("BeginSnapshot")) || !bytes.Contains(migration, []byte("('platform','owner.widgets','create')")) || bytes.Contains(migration, []byte("CREATE TABLE")) {
		t.Fatal("snapshot became Human policy or additive migration rebuilt schema")
	}
	repeated, _, err := generateOwnerPolicies([]byte(ownerPolicyFixture), strings.Repeat("a", 64), "owner")
	if err != nil || !bytes.Equal(generated, repeated) {
		t.Fatal("non deterministic owner policy")
	}
}

func TestOwnerOpenAPIDoesNotGuessMissingOrAmbiguousAuthorization(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"x-ani-auth-classification: authorized", "x-ani-auth-classification: public"},
		{"credential_kinds: [access_token]", "credential_kinds: [api_key]"},
		{"principal_kinds: [human]", "principal_kinds: [workload]"},
		{"scope: platform", "scope: absent"},
		{"version: v1", "version: v2"},
		{"security: [{workloadToken: [], BearerAuth: []}]", "security: [{workloadToken: []}, {BearerAuth: []}]"},
		{"x-ani-workload-operation: widget.create", "x-ani-workload-operation: ''"},
		{"operationId: BeginSnapshot", "operationId: CreateWidget"},
		{"/v1/widgets:", "/v1/widgets/{id}:"},
		{"x-ani-audience: owner", "x-ani-audience: other"},
		{"x-ani-boundary: workload-only", "x-ani-boundary: workload-only\n      x-ani-authz: {version: v1, resource: x, actions: [read], scope: platform}"},
		{"x-ani-auth-classification: authorized", "x-ani-auth-classification: authorized\n      x-ani-auth-classification: public"},
	} {
		if _, _, err := generateOwnerPolicies([]byte(strings.Replace(ownerPolicyFixture, tc.from, tc.to, 1)), strings.Repeat("a", 64), "owner"); err == nil {
			t.Fatalf("unsafe owner input accepted: %s", tc.to)
		}
	}
}
