package grpcworkload

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthorizeHTTPPlatformHuman composes the existing current direct-caller check
// with the existing local Platform Human decision. The owner supplies the exact
// registered route and source operation from its fixed owner contract;
// neither value comes from a caller header or a path template.
// This is a local owner entry point, not a delegation or downstream-hop token.
func (c *Client) AuthorizeHTTPPlatformHuman(r *http.Request, target HTTPWorkloadTarget, sourceOperation string) (*http.Request, WorkloadCaller, Subject, error) {
	return c.authorizeHTTPHuman(r, target, AuthorizationRequest{SourceOperation: sourceOperation}, true)
}

// AuthorizeHTTPTenantHuman requires an explicit requested Tenant boundary. IAM
// validates that boundary against the Human credential. The owner must parse its
// canonical resource identifiers and perform any resource ownership obligation.
// Credential must be empty: it is read here from the one Bearer credential.
// SourceOperation comes from the owner's fixed route declaration.
func (c *Client) AuthorizeHTTPTenantHuman(r *http.Request, target HTTPWorkloadTarget, requested AuthorizationRequest) (*http.Request, WorkloadCaller, Subject, error) {
	if requested.Credential != "" || !canonicalHTTPUUID(requested.TenantID) {
		return nil, WorkloadCaller{}, Subject{}, status.Error(codes.InvalidArgument, "explicit Tenant boundary is required")
	}
	return c.authorizeHTTPHuman(r, target, requested, false)
}

func (c *Client) authorizeHTTPHuman(r *http.Request, target HTTPWorkloadTarget, requested AuthorizationRequest, platform bool) (*http.Request, WorkloadCaller, Subject, error) {
	if r == nil || c.cfg.PolicyRevision == "" || requested.SourceOperation == "" {
		return nil, WorkloadCaller{}, Subject{}, ErrConfiguration
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, WorkloadCaller{}, Subject{}, status.Error(codes.Unauthenticated, "one Human Bearer credential is required")
	}
	credential := strings.TrimPrefix(values[0], "Bearer ")
	if credential == "" || len(credential) > 16384 {
		return nil, WorkloadCaller{}, Subject{}, status.Error(codes.Unauthenticated, "invalid Human Bearer credential")
	}
	for _, ch := range credential {
		if ch < 33 || ch > 126 || ch == ',' {
			return nil, WorkloadCaller{}, Subject{}, status.Error(codes.Unauthenticated, "invalid Human Bearer credential")
		}
	}
	withoutHuman := r.Clone(r.Context())
	withoutHuman.Header.Del("Authorization")
	clean, caller, err := (&WorkloadOnlyClient{client: c}).VerifyHTTPCaller(withoutHuman, target)
	if err != nil {
		return nil, WorkloadCaller{}, Subject{}, err
	}
	requested.Credential = credential
	var subject Subject
	if platform {
		subject, err = c.AuthorizePlatformHuman(r.Context(), requested)
	} else {
		subject, err = c.Authorize(r.Context(), requested)
	}
	if err != nil {
		return nil, WorkloadCaller{}, Subject{}, err
	}
	p := subject.Principal()
	if p.GetPrincipalType() != iamv1.PrincipalType_PRINCIPAL_TYPE_HUMAN || p.GetPrincipalStatus() != iamv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE ||
		!canonicalHTTPUUID(p.GetPrincipalId()) || !canonicalHTTPUUID(p.GetSessionId()) || !canonicalHTTPUUID(p.GetGrantId()) || !canonicalHTTPUUID(subject.DecisionID()) {
		return nil, WorkloadCaller{}, Subject{}, status.Error(codes.PermissionDenied, "current Human Session decision is required")
	}
	return clean, caller, subject, nil
}

func canonicalHTTPUUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}
