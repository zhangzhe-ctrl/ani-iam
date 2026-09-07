//go:build integration

package integration_test

import (
	"context"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

func (allowingIntegrationLoginThrottle) CheckRefresh(context.Context, biz.RefreshThrottleAttempt) error {
	return nil
}

func (*recordingAccessTokenIssuer) Verify(context.Context, string) (biz.AccessTokenClaims, error) {
	return biz.AccessTokenClaims{}, biz.ErrAuthorizationCredentialInvalid
}
