package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strconv"
	"strings"
)

type registryDocument struct {
	SchemaVersion  string `json:"schema_version"`
	PolicyRevision string `json:"policy_revision"`
	IAMReplacement struct {
		SourceOpenAPISHA256 map[string]string `json:"source_openapi_sha256"`
	} `json:"iam_replacement"`
	Operations []registryOperation `json:"operations"`
}

type registryOperation struct {
	OperationID string               `json:"operation_id"`
	IAMDecision string               `json:"iam_decision"`
	Permission  *registryPermission  `json:"permission"`
	Obligations []registryObligation `json:"obligations"`
}

type registryPermission struct {
	Scope    string   `json:"scope"`
	Resource string   `json:"resource"`
	Actions  []string `json:"actions"`
}

type registryObligation struct {
	Type    string `json:"type"`
	Handler string `json:"handler"`
}

type catalogPermission struct {
	Scope    string
	Resource string
	Action   string
}

func main() {
	input := flag.String("input", "", "ANI operation-registry.v1.json input")
	goOutput := flag.String("go-output", "", "generated Go policy output")
	sqlOutput := flag.String("sql-output", "", "generated permission catalog migration output")
	expectedSHA256 := flag.String("expected-sha256", "", "required input SHA-256")
	flag.Parse()
	if *input == "" || *goOutput == "" || *sqlOutput == "" || *expectedSHA256 == "" {
		fatalf("-input, -go-output, -sql-output and -expected-sha256 are required")
	}

	source, err := os.ReadFile(*input)
	if err != nil {
		fatalf("read registry: %v", err)
	}
	digest := sha256.Sum256(source)
	actualSHA256 := hex.EncodeToString(digest[:])
	if actualSHA256 != *expectedSHA256 {
		fatalf("registry SHA-256 = %s, want %s", actualSHA256, *expectedSHA256)
	}

	var document registryDocument
	if err := json.Unmarshal(source, &document); err != nil {
		fatalf("decode registry: %v", err)
	}
	policies, permissions, err := validateAndCollect(document)
	if err != nil {
		fatalf("validate registry: %v", err)
	}
	goSource, err := renderGo(document, actualSHA256, policies, permissions)
	if err != nil {
		fatalf("render Go: %v", err)
	}
	if err := os.WriteFile(*goOutput, goSource, 0o644); err != nil {
		fatalf("write Go output: %v", err)
	}
	if err := os.WriteFile(*sqlOutput, renderSQL(document, actualSHA256, permissions), 0o644); err != nil {
		fatalf("write SQL output: %v", err)
	}
}

func validateAndCollect(document registryDocument) ([]registryOperation, []catalogPermission, error) {
	if document.SchemaVersion != "ani.operation-policy/v1" || !strings.HasPrefix(document.PolicyRevision, "sha256:") ||
		document.IAMReplacement.SourceOpenAPISHA256["core-v1"] == "" || document.IAMReplacement.SourceOpenAPISHA256["services-v1"] == "" {
		return nil, nil, fmt.Errorf("unsupported registry identity")
	}
	policies := make([]registryOperation, 0, len(document.Operations))
	permissionSet := make(map[catalogPermission]struct{})
	operationIDs := make(map[string]struct{})
	for _, operation := range document.Operations {
		if operation.IAMDecision != "check_permission" {
			continue
		}
		if operation.OperationID == "" || operation.Permission == nil || operation.Permission.Resource == "" || len(operation.Permission.Actions) == 0 {
			return nil, nil, fmt.Errorf("operation %q has an incomplete permission", operation.OperationID)
		}
		if _, exists := operationIDs[operation.OperationID]; exists {
			return nil, nil, fmt.Errorf("duplicate operation %q", operation.OperationID)
		}
		operationIDs[operation.OperationID] = struct{}{}
		if _, err := scopeExpression(operation.Permission.Scope); err != nil {
			return nil, nil, fmt.Errorf("operation %q: %w", operation.OperationID, err)
		}
		for _, action := range operation.Permission.Actions {
			if strings.TrimSpace(action) == "" {
				return nil, nil, fmt.Errorf("operation %q has an empty action", operation.OperationID)
			}
			permissionSet[catalogPermission{Scope: operation.Permission.Scope, Resource: operation.Permission.Resource, Action: action}] = struct{}{}
		}
		for _, obligation := range operation.Obligations {
			if obligation.Type != "resource_tenant_match" || strings.TrimSpace(obligation.Handler) == "" {
				return nil, nil, fmt.Errorf("operation %q has unsupported obligation %#v", operation.OperationID, obligation)
			}
		}
		policies = append(policies, operation)
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].OperationID < policies[j].OperationID })
	permissions := make([]catalogPermission, 0, len(permissionSet))
	for permission := range permissionSet {
		permissions = append(permissions, permission)
	}
	sort.Slice(permissions, func(i, j int) bool {
		if permissions[i].Scope != permissions[j].Scope {
			return permissions[i].Scope < permissions[j].Scope
		}
		if permissions[i].Resource != permissions[j].Resource {
			return permissions[i].Resource < permissions[j].Resource
		}
		return permissions[i].Action < permissions[j].Action
	})
	if len(policies) == 0 || len(permissions) == 0 {
		return nil, nil, fmt.Errorf("registry contains no authorized policies")
	}
	return policies, permissions, nil
}

func renderGo(document registryDocument, sourceSHA256 string, policies []registryOperation, permissions []catalogPermission) ([]byte, error) {
	var output bytes.Buffer
	fmt.Fprintf(&output, "// Code generated by internal/data/cmd/genoperationregistry; DO NOT EDIT.\n")
	fmt.Fprintf(&output, "// Source operation-registry.v1.json SHA-256: %s.\n\n", sourceSHA256)
	fmt.Fprintln(&output, "package data")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, `import "github.com/zhangzhe-ctrl/ani-iam/internal/biz"`)
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "const (")
	fmt.Fprintf(&output, "\tTargetPolicyRevision = %s\n", strconv.Quote(document.PolicyRevision))
	fmt.Fprintf(&output, "\tTargetOperationRegistrySHA256 = %s\n", strconv.Quote(sourceSHA256))
	fmt.Fprintf(&output, "\tTargetCoreOpenAPISHA256 = %s\n", strconv.Quote(document.IAMReplacement.SourceOpenAPISHA256["core-v1"]))
	fmt.Fprintf(&output, "\tTargetServicesOpenAPISHA256 = %s\n", strconv.Quote(document.IAMReplacement.SourceOpenAPISHA256["services-v1"]))
	fmt.Fprintf(&output, "\tTargetAuthorizedOperationCount = %d\n", len(policies))
	tenantPermissions := 0
	for _, permission := range permissions {
		if permission.Scope == "tenant" {
			tenantPermissions++
		}
	}
	fmt.Fprintf(&output, "\tTargetTenantPermissionCount = %d\n", tenantPermissions)
	fmt.Fprintln(&output, ")")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "var generatedTargetPolicies = map[string]biz.AuthorizationPolicy{")
	for _, operation := range policies {
		scope, _ := scopeExpression(operation.Permission.Scope)
		fmt.Fprintf(&output, "\t%s: {OperationID: %s, Resource: %s, Actions: []string{%s}, Scope: %s", strconv.Quote(operation.OperationID), strconv.Quote(operation.OperationID), strconv.Quote(operation.Permission.Resource), quotedList(operation.Permission.Actions), scope)
		if len(operation.Obligations) > 0 {
			fmt.Fprint(&output, ", Obligations: []biz.AuthorizationObligation{")
			for index, obligation := range operation.Obligations {
				if index > 0 {
					fmt.Fprint(&output, ", ")
				}
				fmt.Fprintf(&output, "{Type: biz.AuthorizationObligationResourceTenantMatch, Handler: %s}", strconv.Quote(obligation.Handler))
			}
			fmt.Fprint(&output, "}")
		}
		fmt.Fprintln(&output, "},")
	}
	fmt.Fprintln(&output, "}")
	return format.Source(output.Bytes())
}

func renderSQL(document registryDocument, sourceSHA256 string, permissions []catalogPermission) []byte {
	var output bytes.Buffer
	fmt.Fprintln(&output, "-- Code generated by internal/data/cmd/genoperationregistry; DO NOT EDIT.")
	fmt.Fprintf(&output, "-- Source operation-registry.v1.json SHA-256: %s.\n", sourceSHA256)
	fmt.Fprintf(&output, "-- Policy revision: %s.\n\n", document.PolicyRevision)
	fmt.Fprintln(&output, "CREATE TABLE permission_catalog (")
	fmt.Fprintln(&output, "    scope text NOT NULL CHECK (scope IN ('tenant', 'platform', 'own')),")
	fmt.Fprintln(&output, "    resource text NOT NULL CHECK (resource <> ''),")
	fmt.Fprintln(&output, "    action text NOT NULL CHECK (action <> ''),")
	fmt.Fprintln(&output, "    PRIMARY KEY (scope, resource, action)")
	fmt.Fprintln(&output, ");")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "INSERT INTO permission_catalog (scope, resource, action) VALUES")
	for index, permission := range permissions {
		terminator := ","
		if index == len(permissions)-1 {
			terminator = ";"
		}
		fmt.Fprintf(&output, "    ('%s', '%s', '%s')%s\n", sqlQuote(permission.Scope), sqlQuote(permission.Resource), sqlQuote(permission.Action), terminator)
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "ALTER TABLE tenant_role_permissions")
	fmt.Fprintln(&output, "    ADD COLUMN scope text NOT NULL DEFAULT 'tenant',")
	fmt.Fprintln(&output, "    ADD CONSTRAINT tenant_role_permissions_tenant_scope CHECK (scope = 'tenant'),")
	fmt.Fprintln(&output, "    ADD CONSTRAINT tenant_role_permissions_catalog_fk")
	fmt.Fprintln(&output, "        FOREIGN KEY (scope, resource, action)")
	fmt.Fprintln(&output, "        REFERENCES permission_catalog (scope, resource, action) ON DELETE RESTRICT;")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "ALTER TABLE tenant_role_permissions ALTER COLUMN scope DROP DEFAULT;")
	return output.Bytes()
}

func scopeExpression(scope string) (string, error) {
	switch scope {
	case "tenant":
		return "biz.PermissionScopeTenant", nil
	case "platform":
		return "biz.PermissionScopePlatform", nil
	case "own":
		return "biz.PermissionScopeOwn", nil
	default:
		return "", fmt.Errorf("unsupported permission scope %q", scope)
	}
}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = strconv.Quote(value)
	}
	return strings.Join(quoted, ", ")
}

func sqlQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
