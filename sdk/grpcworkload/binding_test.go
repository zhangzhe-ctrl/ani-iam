package grpcworkload

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func bindingTarget() *Target {
	return &Target{Method: "/ani.session.v1.SessionService/CreateSession", Audience: "ani-session-gateway", Operation: "session.create", Describe: func(proto.Message) (RequestScope, error) {
		return RequestScope{TenantID: "tenant", SubjectID: "human", ResourceID: "resource", SourceOperation: "createInstanceExecSession", Mode: "exec"}, nil
	}}
}

func TestBindingCoversActualNestedRequestAndRejectsUnknownFields(t *testing.T) {
	target := bindingTarget()
	request, err := structpb.NewStruct(map[string]any{"command": []any{"sh", "-c", "printf test"}, "tty": true, "rows": 24, "cols": 80, "idempotency": "id-1"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := target.bind(request, "revision")
	if err != nil {
		t.Fatal(err)
	}
	same, err := target.bind(proto.Clone(request), "revision")
	if err != nil || !proto.Equal(a, same) {
		t.Fatal("deterministic request did not keep its binding")
	}
	for _, field := range []string{"command", "tty", "rows", "cols", "idempotency"} {
		t.Run(field, func(t *testing.T) {
			changed := proto.Clone(request).(*structpb.Struct)
			changed.Fields[field] = structpb.NewStringValue("changed")
			b, err := target.bind(changed, "revision")
			if err != nil || bytes.Equal(a.GetRequestSha256(), b.GetRequestSha256()) {
				t.Fatal("changed request retained its digest")
			}
		})
	}
	unknown := proto.Clone(request).(*structpb.Struct)
	unknown.Fields["command"].ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	if _, err := target.bind(unknown, "revision"); err == nil {
		t.Fatal("nested unknown field accepted")
	}
	changedMethod := *target
	changedMethod.Method = "/another.Service/Call"
	b, err := changedMethod.bind(request, "revision")
	if err != nil || bytes.Equal(a.GetRequestSha256(), b.GetRequestSha256()) {
		t.Fatal("RPC name is not in digest")
	}
}

func TestDescribeCannotMutateRequest(t *testing.T) {
	target := bindingTarget()
	target.Describe = func(m proto.Message) (RequestScope, error) {
		m.(*structpb.Struct).Fields["mutated"] = structpb.NewBoolValue(true)
		return RequestScope{TenantID: "t", SubjectID: "s", ResourceID: "r", SourceOperation: "op", Mode: "m"}, nil
	}
	m, _ := structpb.NewStruct(map[string]any{})
	if _, err := target.bind(m, "revision"); err == nil {
		t.Fatal("mutating Describe was accepted")
	}
}

func TestSubjectContextIsOpaqueClonedAndRedacted(t *testing.T) {
	subject := Subject{principal: &iamv1.PrincipalContext{PrincipalId: "human"}, credential: "test-secret-that-must-not-format"}
	for _, text := range []string{fmt.Sprint(subject), fmt.Sprintf("%+v", subject), fmt.Sprintf("%#v", subject)} {
		if strings.Contains(text, subject.credential) {
			t.Fatal("credential formatting leak")
		}
	}
	clone := subject.Principal()
	clone.PrincipalId = "another"
	got, ok := SubjectFromContext(WithSubject(context.Background(), subject))
	if !ok || got.Principal().GetPrincipalId() != "human" {
		t.Fatal("subject identity was mutable through snapshot")
	}
	if _, ok := SubjectFromContext(WithSubject(context.Background(), Subject{})); ok {
		t.Fatal("unverified zero subject accepted")
	}
}
