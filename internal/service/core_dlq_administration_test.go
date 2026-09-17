package service

import (
	"errors"
	"testing"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCoreDLQOutcomeEvidenceErrorPrecedence(t *testing.T) {
	for _, original := range []error{biz.ErrCoreDLQConflict, biz.ErrCoreBrokerAuthority, biz.ErrPlatformAdministrationDenied} {
		s := status.Convert(mapIAMError(errors.Join(original, biz.ErrCoreDLQAuditUnavailable), errorContext{OperationID: "replayCoreIAMDLQEntry"}))
		if s.Code() != codes.Unavailable {
			t.Fatalf("missing outcome evidence classified as %s", s.Code())
		}
		found := false
		for _, detail := range s.Details() {
			if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Reason == "IAM_DLQ_AUDIT_UNAVAILABLE" {
				found = true
			}
		}
		if !found {
			t.Fatal("missing precise DLQ evidence-unavailable reason")
		}
	}
}

func TestCoreDLQStoredOriginalErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err  error
		code codes.Code
	}{
		{biz.ErrCoreProjectionInvalid, codes.InvalidArgument},
		{biz.ErrCoreBootstrapInvalid, codes.InvalidArgument},
		{biz.ErrCoreProjectionConflict, codes.Aborted},
		{biz.ErrCoreBootstrapConflict, codes.Aborted},
		{biz.ErrCoreBrokerAuthority, codes.Unavailable},
		{biz.ErrCoreDLQProvenance, codes.FailedPrecondition},
	} {
		if got := status.Code(mapIAMError(test.err, errorContext{OperationID: "replayCoreIAMDLQEntry"})); got != test.code {
			t.Errorf("error %v: got %s want %s", test.err, got, test.code)
		}
	}
}
