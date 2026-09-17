package biz

import (
	"slices"

	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func ValidateRegisteredInvocation(registry *workloadregistry.Registry, b InvocationBinding) (workloadregistry.Target, workloadregistry.Source, error) {
	if b.Validate() != nil {
		return workloadregistry.Target{}, workloadregistry.Source{}, ErrInvocationInvalid
	}
	t, ok := registry.Lookup(b.Audience, b.Operation)
	if !ok || t.Mechanism != workloadregistry.Delegated || t.RPC != b.RPCMethod {
		return workloadregistry.Target{}, workloadregistry.Source{}, ErrInvocationInvalid
	}
	if !t.Enabled || b.TargetRevision != registry.Revision(b.Audience, b.Operation) {
		return workloadregistry.Target{}, workloadregistry.Source{}, ErrWorkloadPermissionDenied
	}
	s, ok := t.Source(b.SourceOperation, b.Mode)
	if !ok {
		return workloadregistry.Target{}, workloadregistry.Source{}, ErrInvocationInvalid
	}
	return t, s, nil
}

func workloadSourceMatchesPolicy(s workloadregistry.Source, p AuthorizationPolicy) bool {
	if p.OperationID != s.Operation || p.Scope != PermissionScopeTenant || p.Resource != s.Resource || len(p.Actions) != 1 || p.Actions[0] != s.Action || len(p.Obligations) != 1 || p.Obligations[0].Type != AuthorizationObligationResourceTenantMatch || p.Obligations[0].Handler != s.OwnerHandler || len(p.PrincipalKinds) != len(s.PrincipalKinds) || len(p.CredentialKinds) != len(s.CredentialKinds) {
		return false
	}
	for _, k := range p.PrincipalKinds {
		if !slices.Contains(s.PrincipalKinds, string(k)) {
			return false
		}
	}
	for _, k := range p.CredentialKinds {
		if !slices.Contains(s.CredentialKinds, string(k)) {
			return false
		}
	}
	return true
}

func registeredSubjectAllowed(s workloadregistry.Source, p TrustedPrincipalContext) bool {
	credential := ""
	switch p.Type {
	case PrincipalTypeHuman:
		credential = "access_token"
	case PrincipalTypeWorkload:
		credential = "api_key"
	}
	return s.AllowsSubject(string(p.Type), credential)
}
