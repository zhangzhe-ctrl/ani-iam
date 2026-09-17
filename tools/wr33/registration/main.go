// Command registration composes one reviewed joint target document. It grants
// no authority and does not provision identities or alter a running deployment.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"gopkg.in/yaml.v3"
)

func main() {
	base := flag.String("base", "", "fixed IAM registration input")
	baseHash := flag.String("base-sha256", "", "required baseline digest")
	owner := flag.String("owner", "", "immutable owner OpenAPI")
	output := flag.String("output", "", "new registration document")
	flag.Parse()
	raw, err := os.ReadFile(*base)
	check(err)
	registry, err := workloadregistry.Parse(raw, *baseHash)
	check(err)
	source, err := os.ReadFile(*owner)
	check(err)
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != data.OwnerOpenAPISHA256 {
		panic("owner contract digest differs from generated policy")
	}
	var contract struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	check(yaml.Unmarshal(source, &contract))
	doc := workloadregistry.Document{Schema: workloadregistry.Schema}
	for _, target := range registry.Targets() {
		// This is the explicit transition composition, not a generic auth
		// service-name branch. The historical Core target is retired here.
		if target.Audience == "ani-core-control" {
			continue
		}
		if target.Audience == governancev1.Audience {
			panic("baseline already contains target owner")
		}
		doc.Targets = append(doc.Targets, target)
	}
	receiver := "governance.receiver"
	doc.Targets = append(doc.Targets, workloadregistry.Target{Audience: governancev1.Audience, Operation: receiver, Mechanism: workloadregistry.Receiver, GrantScope: "governance_receive", Enabled: true})
	routeCount := 0
	for path, methods := range contract.Paths {
		for method, node := range methods {
			if method != "get" && method != "post" {
				continue
			}
			var route struct {
				Workload            string   `yaml:"x-ani-workload-operation"`
				Snapshot            string   `yaml:"x-ani-operation"`
				AuthorityOperations []string `yaml:"x-ani-authority-operations"`
			}
			check(node.Decode(&route))
			operation := route.Workload
			if operation == "" {
				operation = route.Snapshot
			}
			if operation == "" {
				panic("owner route lacks target declaration")
			}
			verb := "POST"
			if method == "get" {
				verb = "GET"
			}
			if len(route.AuthorityOperations) != 0 && route.Snapshot == "" {
				panic("authority operation group requires an owner workload-only declaration")
			}
			doc.Targets = append(doc.Targets, workloadregistry.Target{Audience: governancev1.Audience, Operation: operation, HTTPMethod: verb, HTTPPath: path, Mechanism: workloadregistry.WorkloadOnly, GrantScope: operation, ReceiverOperation: receiver, Enabled: true, AuthorityOperations: route.AuthorityOperations})
			routeCount++
		}
	}
	if routeCount != 7 {
		panic("owner route denominator changed")
	}
	// Registry canonical iteration makes the artifact independent of Go map order.
	raw, err = json.Marshal(doc)
	check(err)
	digest = sha256.Sum256(raw)
	composed, err := workloadregistry.Parse(raw, hex.EncodeToString(digest[:]))
	check(err)
	doc.Targets = composed.Targets()
	raw, err = json.MarshalIndent(doc, "", "  ")
	check(err)
	raw = append(raw, '\n')
	digest = sha256.Sum256(raw)
	composed, err = workloadregistry.Parse(raw, hex.EncodeToString(digest[:]))
	check(err)
	revision, err := data.WorkloadPolicyRevision(composed)
	check(err)
	policies, err := data.NewTargetOperationRegistry(revision, composed)
	check(err)
	for _, operation := range []string{"CreateTenant", "GetTenant", "GetPlatformTenant", "ChangeTenantLifecycle", "GetOperation"} {
		if _, ok := policies.Lookup(operation); !ok {
			panic("missing owner source policy")
		}
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	check(err)
	_, err = file.Write(raw)
	check(err)
	check(file.Close())
	result := map[string]any{"registry_sha256": hex.EncodeToString(digest[:]), "policy_revision": revision, "owner_openapi_sha256": data.OwnerOpenAPISHA256, "owner_business_operations": 5, "owner_snapshot_operations": 2, "targets": len(doc.Targets), "grants_provisioned": false}
	encoded, err := json.MarshalIndent(result, "", "  ")
	check(err)
	fmt.Println(string(encoded))
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
