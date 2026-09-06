package data

import (
	"fmt"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

const (
	TargetPolicyRevision          = "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa"
	TargetOperationRegistrySHA256 = "319bd3746098b79d29da18141872b97263c1d899da0306654eda9fad736c2ad2"
	TargetOpenAPISHA256           = "2466982a7e8f904c6bb6f7790588359c6faf9b230a39a0e28939fcbedc72d0e5"
)

type targetOperationRegistry struct {
	revision string
	policies map[string]biz.AuthorizationPolicy
}

func NewTargetOperationRegistry(expectedRevision string) (biz.AuthorizationPolicyRegistry, error) {
	if expectedRevision != TargetPolicyRevision {
		return nil, &biz.AuthorizationPolicyMismatchError{
			Expected: TargetPolicyRevision,
			Actual:   expectedRevision,
		}
	}
	return &targetOperationRegistry{
		revision: TargetPolicyRevision,
		policies: map[string]biz.AuthorizationPolicy{
			"listInstances": {
				OperationID: "listInstances",
				Resource:    "instances",
				Actions:     []string{"read"},
			},
		},
	}, nil
}

func (r *targetOperationRegistry) Revision() string {
	return r.revision
}

func (r *targetOperationRegistry) Lookup(operationID string) (biz.AuthorizationPolicy, bool) {
	policy, ok := r.policies[operationID]
	if !ok {
		return biz.AuthorizationPolicy{}, false
	}
	policy.Actions = append([]string(nil), policy.Actions...)
	return policy, true
}

func TargetRegistryIdentity() string {
	return fmt.Sprintf("policy=%s registry_sha256=%s openapi_sha256=%s", TargetPolicyRevision, TargetOperationRegistrySHA256, TargetOpenAPISHA256)
}

var _ biz.AuthorizationPolicyRegistry = (*targetOperationRegistry)(nil)
