package service

import (
	"context"
	"errors"

	iamv1 "github.com/zhangzhe-ctrl/ani-iam/api/iam/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionQueryUsecase interface {
	ListSessions(context.Context, biz.ListSessionsCommand) (biz.ListSessionsResult, error)
}

func (s *AuthenticationService) ListSessions(ctx context.Context, r *iamv1.ListSessionsRequest) (*iamv1.ListSessionsResponse, error) {
	if r == nil {
		return nil, invalidArgumentStatus("request", "session list request is required")
	}
	query, ok := s.authentication.(sessionQueryUsecase)
	if !ok {
		return nil, newIAMStatus(codes.Unavailable, "IAM_UNAVAILABLE", "session query is unavailable", nil)
	}
	v, err := query.ListSessions(ctx, biz.ListSessionsCommand{Credential: r.GetCredential().GetValue(), Cursor: r.GetPage().GetCursor(), Limit: int(r.GetPage().GetPageSize())})
	if errors.Is(err, biz.ErrSessionCursorInvalid) {
		return nil, invalidArgumentStatus("page", "session page is invalid")
	}
	if err != nil {
		return nil, mapIAMError(err, errorContext{OperationID: "listSessions", CredentialKind: "bearer", Dependency: "authentication"})
	}
	result := &iamv1.ListSessionsResponse{NextCursor: v.NextCursor, Sessions: make([]*iamv1.SessionSummary, 0, len(v.Sessions))}
	for _, item := range v.Sessions {
		s := item.Session
		summary := &iamv1.SessionSummary{SessionId: s.ID.String(), Status: sessionStatusToProto(s.Status), DeviceName: s.DeviceName, CreatedAt: timestamppb.New(s.CreatedAt), IdleExpiresAt: timestamppb.New(s.IdleExpiresAt), AbsoluteExpiresAt: timestamppb.New(s.AbsoluteExpiry)}
		for _, method := range s.AuthnMethods {
			if method == biz.AuditAuthenticationMethodPassword {
				summary.AuthnMethods = append(summary.AuthnMethods, iamv1.AuthnMethod_AUTHN_METHOD_PASSWORD)
			} else if method == biz.AuditAuthenticationMethodOIDC {
				summary.AuthnMethods = append(summary.AuthnMethods, iamv1.AuthnMethod_AUTHN_METHOD_OIDC)
			}
		}
		for _, g := range item.Grants {
			summary.Grants = append(summary.Grants, &iamv1.SessionGrantSummary{GrantId: g.Grant.ID.String(), Boundary: tenantBoundary(g.TenantID), Version: uint64(g.Grant.Version), Status: grantStatusToProto(g.Grant.Status)})
		}
		result.Sessions = append(result.Sessions, summary)
	}
	return result, nil
}
