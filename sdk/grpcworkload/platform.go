package grpcworkload

import (
	"context"
	"strings"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// AuthorizePlatformHuman checks a local Platform source operation with IAM.
// It does not infer a Tenant, mint a delegation, or authorize a downstream hop.
func (c *Client) AuthorizePlatformHuman(ctx context.Context, r AuthorizationRequest) (Subject, error) {
	if strings.TrimSpace(r.Credential) == "" || r.SourceOperation == "" || r.TenantID != "" || r.ResourceID != "" || r.CheckResource != nil {
		return Subject{}, status.Error(codes.InvalidArgument, "Platform source request is invalid")
	}
	call, cancel := c.deadline(ctx)
	defer cancel()
	reply, err := c.authorization.CheckPermission(call, &iamv1.CheckPermissionRequest{Credential: &iamv1.BearerCredential{Value: strings.TrimSpace(r.Credential)}, OperationId: r.SourceOperation, PolicyRevision: c.cfg.PolicyRevision, Target: &iamv1.AuthorizationTarget{}})
	if err != nil {
		return Subject{}, err
	}
	d := reply.GetDecision()
	p := d.GetPrincipal()
	if !d.GetAllowed() || d.GetPolicyRevision() != c.cfg.PolicyRevision || len(d.GetObligations()) != 0 || p.GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN || p.GetPrincipalStatus() != iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE || p.GetBoundary().GetPlatform() == nil {
		return Subject{}, status.Error(codes.PermissionDenied, "Platform Human is not authorized")
	}
	for _, raw := range []string{d.GetDecisionId(), p.GetPrincipalId(), p.GetSessionId(), p.GetGrantId()} {
		id, e := uuid.Parse(raw)
		if e != nil || id == uuid.Nil || id.String() != raw {
			return Subject{}, status.Error(codes.PermissionDenied, "Platform decision is incomplete")
		}
	}
	return Subject{principal: proto.Clone(p).(*iamv1.PrincipalContext), credential: strings.TrimSpace(r.Credential), sourceOperation: r.SourceOperation, policyRevision: c.cfg.PolicyRevision, decisionID: d.GetDecisionId()}, nil
}
