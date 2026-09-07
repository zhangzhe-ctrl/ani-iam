package tests_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
)

func TestPasswordLoginContractCarriesTrustedSourceIP(t *testing.T) {
	fields := (&iamv1.PasswordLoginRequest{}).ProtoReflect().Descriptor().Fields()
	field := fields.ByName("source_ip")
	if field == nil {
		t.Fatal("PasswordLoginRequest.source_ip is missing")
	}
	if field.Number() != 7 || field.Kind() != protoreflect.StringKind {
		t.Fatalf("source_ip number/kind = %d/%s, want 7/string", field.Number(), field.Kind())
	}
}
