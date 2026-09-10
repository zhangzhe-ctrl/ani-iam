package main

import (
	"strings"
	"testing"
)

func TestRenderGoPreservesCredentialAndPrincipalRestrictions(t *testing.T) {
	document := registryDocument{SchemaVersion: "ani.operation-policy/v1", PolicyRevision: "sha256:test"}
	document.IAMReplacement.SourceOpenAPISHA256 = map[string]string{"core-v1": "core", "services-v1": "services"}
	document.Operations = []registryOperation{{
		OperationID: "createInstance", IAMDecision: "check_permission",
		Permission: &registryPermission{Scope: "tenant", Resource: "instances", Actions: []string{"create"}},
		Authn:      registryAuthentication{CredentialKinds: []string{"access_token", "api_key"}, PrincipalKinds: []string{"human", "workload"}},
	}}
	policies, permissions, err := validateAndCollect(document)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := renderGo(document, "registry", policies, permissions)
	if err != nil {
		t.Fatal(err)
	}
	source := string(generated)
	for _, expected := range []string{
		`CredentialKinds: []biz.CredentialKind{biz.CredentialKindAccessToken, biz.CredentialKindAPIKey}`,
		`PrincipalKinds: []biz.PrincipalType{biz.PrincipalTypeHuman, biz.PrincipalTypeWorkload}`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("generated source does not contain %q:\n%s", expected, source)
		}
	}
}
