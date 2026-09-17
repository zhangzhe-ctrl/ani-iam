package grpcworkload

import (
	"context"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// RegisteredWorkloadTarget reads the exact target from the reviewed declaration.
func RegisteredWorkloadTarget(r *workloadregistry.Registry, method string) (WorkloadTarget, error) {
	t, ok := r.Method(method)
	if !ok || !t.Enabled || t.Mechanism != workloadregistry.WorkloadOnly {
		return WorkloadTarget{}, ErrConfiguration
	}
	return WorkloadTarget{Audience: t.Audience, Operation: t.Operation, RPCMethod: t.RPC}, nil
}

func (c *Client) registeredTarget(t Target) (workloadregistry.Target, error) {
	r, ok := c.cfg.Registry.Lookup(t.Audience, t.Operation)
	if !ok || !r.Enabled || r.Mechanism != workloadregistry.Delegated || r.RPC != t.Method {
		return workloadregistry.Target{}, ErrConfiguration
	}
	return r, nil
}

func (c *Client) bindTarget(t Target, request proto.Message) (*iamv1.InvocationBinding, workloadregistry.Target, workloadregistry.Source, error) {
	r, err := c.registeredTarget(t)
	if err != nil {
		return nil, r, workloadregistry.Source{}, err
	}
	b, err := t.bind(request, c.cfg.PolicyRevision)
	if err != nil {
		return nil, r, workloadregistry.Source{}, err
	}
	b.TargetRevision = c.cfg.Registry.Revision(t.Audience, t.Operation)
	s, ok := r.Source(b.GetSourceOperationId(), b.GetMode())
	if !ok {
		return nil, r, s, ErrBinding
	}
	return b, r, s, nil
}

func registeredPrincipalAllowed(s workloadregistry.Source, p *iamv1.PrincipalContext) bool {
	if p == nil {
		return false
	}
	if p.GetPrincipalType() == iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN {
		return s.AllowsSubject("human", "access_token")
	}
	return p.GetPrincipalType() == iamv1.PrincipalType_PRINCIPAL_TYPE_WORKLOAD && len(p.GetAuthnMethods()) == 1 && p.GetAuthnMethods()[0] == iamv1.AuthnMethod_AUTHN_METHOD_API_KEY && s.AllowsSubject("workload", "api_key")
}

// AuthorizeForReceiver carries one declared owner obligation to that exact
// receiver. It cannot be reused for another target, even with the same source.
func (c *Client) AuthorizeForReceiver(ctx context.Context, r AuthorizationRequest, target WorkloadTarget) (Subject, error) {
	t, ok := c.cfg.Registry.Lookup(target.Audience, target.Operation)
	if !ok || !t.Enabled || t.RPC != target.RPCMethod || t.Mechanism != workloadregistry.Delegated || r.TenantID == "" || r.ResourceID == "" || r.CheckResource != nil {
		return Subject{}, ErrConfiguration
	}
	for _, s := range t.Sources {
		if s.Operation == r.SourceOperation && s.OwnerCheck == "receiver" {
			return c.authorize(ctx, r, &s, target)
		}
	}
	return Subject{}, status.Error(codes.PermissionDenied, "receiver obligation target is not registered")
}

func (c *Client) ReceiverInterceptorWithOwnerCheck(targets []Target, check func(context.Context, proto.Message, Verified) error) (grpc.UnaryServerInterceptor, error) {
	if check == nil {
		return nil, ErrConfiguration
	}
	return c.receiverInterceptor(targets, check)
}
