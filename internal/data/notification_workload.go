package data

import (
	"context"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload"
)

func NotificationWorkloadSource(issuer *biz.WorkloadInvocation, environment, trustDomain string) grpcworkload.WorkloadTokenSource {
	return func(ctx context.Context, target grpcworkload.WorkloadTarget) (string, error) {
		if issuer == nil {
			return "", biz.ErrPersistenceUnavailable
		}
		issued, err := issuer.IssueLocalWorkloadToken(ctx, biz.VerifiedWorkloadPeer{Environment: environment, TrustDomain: trustDomain, IdentityKind: "x509_dns", IdentityValue: notificationClientDNSName + "." + trustDomain}, biz.WorkloadTarget{Audience: target.Audience, Operation: target.Operation})
		if err != nil {
			return "", err
		}
		return issued.Value, nil
	}
}
