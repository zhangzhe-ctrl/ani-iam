package grpcworkload

import (
	"crypto/sha256"
	"errors"
	"strings"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var ErrConfiguration = errors.New("IAM gRPC adapter configuration is invalid")
var ErrBinding = errors.New("IAM gRPC request binding is invalid")

// RequestScope is extracted by the owner from its normalized DTO. None of these
// user-controlled fields assert a trusted identity without IAM verification.
type RequestScope struct{ TenantID, SubjectID, ResourceID, SourceOperation, Mode string }

type Target struct {
	Method    string
	Audience  string
	Operation string
	// Describe validates the owner DTO and returns its actual scope. It must
	// not mutate the request; perform normalization before entering the adapter.
	Describe func(proto.Message) (RequestScope, error)
}

func (t Target) bind(request proto.Message, revision string) (*iamv1.InvocationBinding, error) {
	if t.Describe == nil || t.Audience == "" || t.Operation == "" || !strings.HasPrefix(t.Method, "/") || strings.ContainsAny(t.Method, "\x00\r\n") || revision == "" || request == nil {
		return nil, ErrBinding
	}
	m := request.ProtoReflect()
	if !m.IsValid() || hasUnknown(m) {
		return nil, ErrBinding
	}
	before, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return nil, ErrBinding
	}
	scope, err := t.Describe(request)
	if err != nil {
		return nil, ErrBinding
	}
	after, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil || string(before) != string(after) {
		return nil, ErrBinding
	}
	if scope.TenantID == "" || scope.SubjectID == "" || scope.ResourceID == "" || scope.SourceOperation == "" || scope.Mode == "" {
		return nil, ErrBinding
	}
	sum := sha256.New()
	_, _ = sum.Write([]byte("ani.grpc.invocation.v1\x00" + t.Method + "\x00" + string(m.Descriptor().FullName()) + "\x00"))
	_, _ = sum.Write(before)
	return &iamv1.InvocationBinding{Audience: t.Audience, OperationId: t.Operation, RpcMethod: t.Method, SourceOperationId: scope.SourceOperation, TenantId: scope.TenantID, SubjectId: scope.SubjectID, ResourceId: scope.ResourceID, Mode: scope.Mode, RequestSha256: sum.Sum(nil), PolicyRevision: revision}, nil
}

func hasUnknown(m protoreflect.Message) bool {
	if len(m.GetUnknown()) != 0 {
		return true
	}
	found := false
	m.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsMap() && field.MapValue().Kind() == protoreflect.MessageKind:
			value.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool { found = hasUnknown(v.Message()); return !found })
		case field.IsList() && field.Kind() == protoreflect.MessageKind:
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if hasUnknown(list.Get(i).Message()) {
					found = true
					break
				}
			}
		case !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind:
			found = hasUnknown(value.Message())
		}
		return !found
	})
	return found
}

func targetIndex(targets []Target) (map[string]Target, error) {
	if len(targets) == 0 {
		return nil, ErrConfiguration
	}
	result := make(map[string]Target, len(targets))
	for _, t := range targets {
		if t.Method == "" || t.Audience == "" || t.Operation == "" || t.Describe == nil {
			return nil, ErrConfiguration
		}
		if _, exists := result[t.Method]; exists {
			return nil, ErrConfiguration
		}
		result[t.Method] = t
	}
	return result, nil
}
