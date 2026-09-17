package main

import (
	"bytes"
	"fmt"
	"go/format"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ownerOperation struct {
	ID                string                `yaml:"operationId"`
	WorkloadOperation string                `yaml:"x-ani-workload-operation"`
	SnapshotOperation string                `yaml:"x-ani-operation"`
	Audience          string                `yaml:"x-ani-audience"`
	Boundary          string                `yaml:"x-ani-boundary"`
	Classification    string                `yaml:"x-ani-auth-classification"`
	Security          []map[string][]string `yaml:"security"`
	Authn             struct {
		PrincipalKinds  []string `yaml:"principal_kinds"`
		CredentialKinds []string `yaml:"credential_kinds"`
	} `yaml:"x-ani-authn"`
	Authz *struct {
		Version     string   `yaml:"version"`
		Resource    string   `yaml:"resource"`
		Actions     []string `yaml:"actions"`
		Scope       string   `yaml:"scope"`
		Obligations []struct {
			Type    string `yaml:"type"`
			Handler string `yaml:"handler"`
		} `yaml:"obligations"`
	} `yaml:"x-ani-authz"`
}

type ownerPolicyRoute struct {
	Method, Path, WorkloadOperation string
	Policy                          registryOperation
}

// This is an input adapter for the existing permission generator. It reads only
// reviewed static routes and the existing x-ani-authn/authz declaration format.
// It never derives a Human permission from the verb, path or Workload grant.
func generateOwnerPolicies(raw []byte, digest, audience string) ([]byte, []byte, error) {
	var doc struct {
		OpenAPI  string                          `yaml:"openapi"`
		Security []map[string][]string           `yaml:"security"`
		Paths    map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`).MatchString(audience) || yaml.Unmarshal(raw, &doc) != nil || doc.OpenAPI != "3.0.3" || len(doc.Paths) == 0 {
		return nil, nil, fmt.Errorf("invalid owner document")
	}
	var routes []ownerPolicyRoute
	var operations []registryOperation
	seenIDs, seenTargets := map[string]bool{}, map[string]bool{}
	for route, methods := range doc.Paths {
		u, err := url.ParseRequestURI(route)
		if err != nil || u.Path != route || path.Clean(route) != route || strings.ContainsAny(route, "%?#{ }*\\\r\n\t") {
			return nil, nil, fmt.Errorf("non-static owner route")
		}
		for method, node := range methods {
			if method == "parameters" || method == "summary" || method == "description" {
				continue
			}
			if method != "get" && method != "post" {
				return nil, nil, fmt.Errorf("unsupported owner HTTP method")
			}
			var op ownerOperation
			if node.Decode(&op) != nil || op.ID == "" || seenIDs[op.ID] {
				return nil, nil, fmt.Errorf("missing or duplicate owner operation")
			}
			seenIDs[op.ID] = true
			security := op.Security
			if security == nil {
				security = doc.Security
			}
			if len(security) != 1 {
				return nil, nil, fmt.Errorf("ambiguous owner security")
			}
			wat, hasWAT := security[0]["workloadToken"]
			if !hasWAT || len(wat) != 0 {
				return nil, nil, fmt.Errorf("owner direct caller authentication missing")
			}
			if op.Boundary == "workload-only" {
				if op.Authz != nil || op.Classification != "" || len(op.Authn.PrincipalKinds)+len(op.Authn.CredentialKinds) != 0 || len(security[0]) != 1 ||
					op.SnapshotOperation == "" || op.Audience != audience || op.WorkloadOperation != "" || seenTargets[op.SnapshotOperation] {
					return nil, nil, fmt.Errorf("Workload-only route claims Human authority")
				}
				seenTargets[op.SnapshotOperation] = true
				continue
			}
			bearer, hasBearer := security[0]["BearerAuth"]
			if !hasBearer || len(bearer) != 0 || len(security[0]) != 2 || op.Classification != "authorized" || op.Authz == nil || op.Authz.Version != "v1" ||
				op.WorkloadOperation == "" || seenTargets[op.WorkloadOperation] || op.SnapshotOperation != "" || op.Boundary != "" ||
				len(op.Authn.PrincipalKinds) != 1 || op.Authn.PrincipalKinds[0] != "human" || len(op.Authn.CredentialKinds) != 1 || op.Authn.CredentialKinds[0] != "access_token" ||
				(op.Authz.Scope != "tenant" && op.Authz.Scope != "platform") {
				return nil, nil, fmt.Errorf("incomplete current Human declaration")
			}
			seenTargets[op.WorkloadOperation] = true
			policy := registryOperation{OperationID: op.ID, IAMDecision: "check_permission", Permission: &registryPermission{Scope: op.Authz.Scope, Resource: op.Authz.Resource, Actions: op.Authz.Actions}, Authn: registryAuthentication{PrincipalKinds: op.Authn.PrincipalKinds, CredentialKinds: op.Authn.CredentialKinds}}
			for _, obligation := range op.Authz.Obligations {
				policy.Obligations = append(policy.Obligations, registryObligation{Type: obligation.Type, Handler: obligation.Handler})
			}
			operations = append(operations, policy)
			routes = append(routes, ownerPolicyRoute{Method: strings.ToUpper(method), Path: route, WorkloadOperation: op.WorkloadOperation, Policy: policy})
		}
	}
	_, permissions, err := collectOperations(operations)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Policy.OperationID < routes[j].Policy.OperationID })
	var goSource, sql bytes.Buffer
	fmt.Fprintln(&goSource, "// Code generated by internal/data/cmd/genoperationregistry; DO NOT EDIT.")
	fmt.Fprintln(&goSource, "package data\n\nimport \"github.com/zhangzhe-ctrl/ani-iam/internal/biz\"")
	fmt.Fprintf(&goSource, "const OwnerOpenAPISHA256 = %s\n", strconv.Quote(digest))
	fmt.Fprintln(&goSource, "var generatedOwnerTargetPolicies = []ownerTargetPolicy{")
	for _, route := range routes {
		fmt.Fprintf(&goSource, "{Audience:%s, Operation:%s, Method:%s, Path:%s, Policy:biz.AuthorizationPolicy", strconv.Quote(audience), strconv.Quote(route.WorkloadOperation), strconv.Quote(route.Method), strconv.Quote(route.Path))
		renderPolicy(&goSource, route.Policy)
		fmt.Fprintln(&goSource, "},")
	}
	fmt.Fprintln(&goSource, "}")
	generated, err := format.Source(goSource.Bytes())
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintln(&sql, "-- Code generated by internal/data/cmd/genoperationregistry; DO NOT EDIT.")
	fmt.Fprintf(&sql, "-- Owner OpenAPI SHA-256: %s.\n", digest)
	fmt.Fprintln(&sql, "INSERT INTO permission_catalog (scope,resource,action) VALUES")
	for i, p := range permissions {
		suffix := ","
		if i == len(permissions)-1 {
			suffix = "\nON CONFLICT (scope,resource,action) DO NOTHING;"
		}
		fmt.Fprintf(&sql, "('%s','%s','%s')%s\n", sqlQuote(p.Scope), sqlQuote(p.Resource), sqlQuote(p.Action), suffix)
	}
	return generated, sql.Bytes(), nil
}
