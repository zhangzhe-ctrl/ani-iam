package grpcworkload

import (
	"context"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Continuation is an opaque receiver-only reference. Persist only through the
// owner's authenticated encryption, bound to its original session and request.
// A reference never authorizes a new business invocation or renews itself.
type Continuation struct {
	request *iamv1.VerifySessionContinuationRequest
	expires time.Time
}

func (c Continuation) String() string       { return "IAM continuation (redacted)" }
func (c Continuation) GoString() string     { return c.String() }
func (c Continuation) ExpiresAt() time.Time { return c.expires }

func (v Verified) Continuation() (Continuation, error) {
	if v.continuation == "" || v.binding == nil || !time.Now().Before(v.continuationExpires) {
		return Continuation{}, status.Error(codes.Unauthenticated, "current continuation is required")
	}
	return Continuation{request: &iamv1.VerifySessionContinuationRequest{Continuation: v.continuation, Binding: v.Binding()}, expires: v.continuationExpires}, nil
}

// MarshalPrivate exports secret bytes for authenticated owner storage only.
func (c Continuation) MarshalPrivate() ([]byte, error) {
	if c.request == nil || c.request.GetContinuation() == "" || c.request.GetBinding() == nil {
		return nil, status.Error(codes.Unauthenticated, "continuation is required")
	}
	return proto.Marshal(c.request)
}

// RestoreContinuation does not authorize anything. A successfully restored
// reference must still pass Recheck using this receiver's current IAM identity.
func RestoreContinuation(private []byte) (Continuation, error) {
	if len(private) == 0 || len(private) > 40000 {
		return Continuation{}, status.Error(codes.Unauthenticated, "invalid continuation")
	}
	r := new(iamv1.VerifySessionContinuationRequest)
	if proto.Unmarshal(private, r) != nil || len(r.ProtoReflect().GetUnknown()) != 0 || r.GetContinuation() == "" || r.GetBinding() == nil || len(r.GetBinding().ProtoReflect().GetUnknown()) != 0 {
		return Continuation{}, status.Error(codes.Unauthenticated, "invalid continuation")
	}
	return Continuation{request: r}, nil
}

// Recheck uses only the receiver's own mTLS identity and exact verification
// Grant. The reply is checked against the complete stored admission binding.
func (c *Client) Recheck(ctx context.Context, reference Continuation) (time.Time, error) {
	if reference.request == nil {
		return time.Time{}, status.Error(codes.Unauthenticated, "continuation is required")
	}
	call, cancel := c.deadline(ctx)
	defer cancel()
	r, err := c.authorization.VerifySessionContinuation(call, proto.Clone(reference.request).(*iamv1.VerifySessionContinuationRequest))
	if err != nil {
		return time.Time{}, err
	}
	b := reference.request.GetBinding()
	if !proto.Equal(r.GetBinding(), b) || r.GetCaller().GetPrincipalId() == "" || r.GetSubject().GetPrincipalId() != b.GetSubjectId() || r.GetSubject().GetBoundary().GetTenant().GetTenantId() != b.GetTenantId() || r.GetExpiresAt() == nil || r.GetExpiresAt().CheckValid() != nil || !time.Now().Before(r.GetExpiresAt().AsTime()) {
		return time.Time{}, status.Error(codes.PermissionDenied, "continuation does not match its admission")
	}
	return r.GetExpiresAt().AsTime(), nil
}
