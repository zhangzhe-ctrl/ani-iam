// Package grpcworkload provides IAM-authenticated unary gRPC calls. Resource
// ownership, DTO normalization and business idempotency remain with the owner.
package grpcworkload

import (
	"context"
	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/protobuf/proto"
	"time"
)

// Subject can only be obtained from an IAM authorization response. Its original
// credential stays private to the adapter and is never sent to a receiver.
type Subject struct {
	principal       *iamv1.PrincipalContext
	credential      string
	sourceOperation string
	policyRevision  string
	resourceID      string
	decisionID      string
}

func (s Subject) String() string   { return "IAM-authorized subject (credential redacted)" }
func (s Subject) GoString() string { return s.String() }

// DecisionID is an audit correlation identifier, never reusable authority.
func (s Subject) DecisionID() string { return s.decisionID }
func (s Subject) Principal() *iamv1.PrincipalContext {
	if s.principal == nil {
		return nil
	}
	return proto.Clone(s.principal).(*iamv1.PrincipalContext)
}

type subjectKey struct{}

func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}
func SubjectFromContext(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(subjectKey{}).(Subject)
	return s, ok && s.principal != nil && s.credential != ""
}

// Verified contains request-scoped, currently checked provenance. It carries no
// user credentials and has no public constructor. Its private continuation is
// usable only for receiver-authenticated online checks of this request.
type Verified struct {
	continuation        string
	continuationExpires time.Time
	caller              *iamv1.DirectWorkloadCaller
	subject             *iamv1.PrincipalContext
	binding             *iamv1.InvocationBinding
}

func (v Verified) String() string   { return "IAM-verified invocation (reference redacted)" }
func (v Verified) GoString() string { return v.String() }

func (v Verified) Caller() *iamv1.DirectWorkloadCaller {
	if v.caller == nil {
		return nil
	}
	return proto.Clone(v.caller).(*iamv1.DirectWorkloadCaller)
}
func (v Verified) Subject() *iamv1.PrincipalContext {
	if v.subject == nil {
		return nil
	}
	return proto.Clone(v.subject).(*iamv1.PrincipalContext)
}
func (v Verified) Binding() *iamv1.InvocationBinding {
	if v.binding == nil {
		return nil
	}
	return proto.Clone(v.binding).(*iamv1.InvocationBinding)
}

type verifiedKey struct{}

func VerifiedFromContext(ctx context.Context) (Verified, bool) {
	v, ok := ctx.Value(verifiedKey{}).(Verified)
	return v, ok && v.caller != nil && v.subject != nil && v.binding != nil
}
