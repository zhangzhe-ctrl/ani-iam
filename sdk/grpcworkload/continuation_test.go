package grpcworkload

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type continuationClient struct {
	iamv1.AuthorizationServiceClient
	mutate func(*iamv1.VerifySessionContinuationResponse)
	err    error
	t      *testing.T
}

func (c *continuationClient) VerifySessionContinuation(ctx context.Context, r *iamv1.VerifySessionContinuationRequest, _ ...grpc.CallOption) (*iamv1.VerifySessionContinuationResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Second {
		c.t.Fatal("unbounded continuation check")
	}
	if c.err != nil {
		return nil, c.err
	}
	reply := &iamv1.VerifySessionContinuationResponse{Caller: &iamv1.DirectWorkloadCaller{PrincipalId: "gateway"}, Subject: testSubject().Principal(), Binding: proto.Clone(r.Binding).(*iamv1.InvocationBinding), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, nil
}
func TestContinuationOpaquePersistenceAndOnlineBinding(t *testing.T) {
	target := bindingTarget()
	message, _ := structpb.NewStruct(map[string]any{"idempotency": "owner-request"})
	binding, err := target.bind(message, "revision")
	if err != nil {
		t.Fatal(err)
	}
	verified := Verified{caller: &iamv1.DirectWorkloadCaller{PrincipalId: "gateway"}, subject: testSubject().Principal(), binding: binding, continuation: "private-reference-never-log", continuationExpires: time.Now().Add(time.Minute)}
	reference, err := verified.Continuation()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{reference, verified} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), verified.continuation) {
				t.Fatal("continuation formatting exposed reference")
			}
		}
	}
	private, err := reference.MarshalPrivate()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreContinuation(private)
	if err != nil {
		t.Fatal(err)
	}
	remote := &continuationClient{t: t}
	client := &Client{authorization: remote, cfg: ClientConfig{Timeout: 2 * time.Second}}
	if _, err := client.Recheck(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*iamv1.VerifySessionContinuationResponse){
		"changed digest":  func(r *iamv1.VerifySessionContinuationResponse) { r.Binding.RequestSha256[0] ^= 1 },
		"changed subject": func(r *iamv1.VerifySessionContinuationResponse) { r.Subject.PrincipalId = "other" },
		"missing caller":  func(r *iamv1.VerifySessionContinuationResponse) { r.Caller = nil },
		"expired": func(r *iamv1.VerifySessionContinuationResponse) {
			r.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
	} {
		t.Run(name, func(t *testing.T) {
			remote.mutate = mutate
			if _, err := client.Recheck(context.Background(), restored); status.Code(err) != codes.PermissionDenied {
				t.Fatal("inconsistent continuation reply admitted")
			}
		})
	}
	remote.mutate = nil
	remote.err = status.Error(codes.Unavailable, "IAM unavailable")
	if _, err := client.Recheck(context.Background(), restored); status.Code(err) != codes.Unavailable {
		t.Fatal("IAM failure did not fail closed")
	}
	remote.err = nil
	if _, err := client.Recheck(context.Background(), restored); err != nil {
		t.Fatal("dependency recovery failed")
	}
	if _, err := RestoreContinuation([]byte("not protobuf")); err == nil {
		t.Fatal("invalid persistence input accepted")
	}
}
