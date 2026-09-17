package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/redis/go-redis/v9"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
)

func newBossAuthentication(ctx context.Context, runtime *conf.Runtime, postgres *data.Data, redisClient *redis.Client, throttle biz.LoginThrottle, tokens biz.OIDCTokenCodec, secrets biz.SecretGenerator, ids biz.IDGenerator, clock biz.Clock, registry *workloadregistry.Registry) (*biz.BossOIDCUsecase, *biz.PlatformSessionUsecase, error) {
	cfg := runtime.GetBossOidc()
	if cfg == nil {
		return nil, nil, nil
	}
	secret, err := data.LoadOIDCClientSecretFile(cfg.ClientSecretFile)
	if err != nil {
		return nil, nil, err
	}
	provider, err := data.NewCoreOSOIDCProvider(ctx, data.CoreOSOIDCProviderConfig{Name: cfg.Provider, IssuerURL: cfg.IssuerUrl, ClientID: cfg.ClientId, ClientSecret: secret, RedirectURIs: []string{cfg.LoginRedirectUri, cfg.IdentityLinkRedirectUri}, HTTPClient: &http.Client{Timeout: cfg.HttpTimeout.AsDuration()}})
	if err != nil {
		return nil, nil, err
	}
	store, err := data.NewRedisOIDCOperationStore(redisClient, runtime.Redis.Namespace+":boss")
	if err != nil {
		return nil, nil, err
	}
	catalog, err := data.NewTargetPermissionCatalog(runtime.PolicyRevision, registry)
	if err != nil {
		return nil, nil, err
	}
	login, err := biz.NewPlatformLoginUsecase(biz.PlatformLoginConfig{Environment: runtime.Environment, Provider: cfg.Provider, OIDCIssuer: cfg.IssuerUrl, AccessIssuer: runtime.AccessToken.Issuer}, data.NewPlatformLoginUnitOfWork(postgres), catalog, tokens, secrets, ids, clock)
	if err != nil {
		return nil, nil, err
	}
	oidc, err := biz.NewBossOIDCUsecase(biz.BossOIDCConfig{Provider: cfg.Provider, LoginRedirectURI: cfg.LoginRedirectUri}, provider, store, login, data.NewPlatformLoginAuditWriter(postgres), secrets, ids, clock)
	if err != nil {
		return nil, nil, err
	}
	redirect, err := url.Parse(cfg.LoginRedirectUri)
	if err != nil {
		return nil, nil, err
	}
	link, err := biz.NewBossIdentityLinkUsecase(biz.BossIdentityLinkConfig{Provider: cfg.Provider, Issuer: cfg.IssuerUrl, RedirectURI: cfg.IdentityLinkRedirectUri, RecentReauthentication: cfg.RecentReauthentication.AsDuration()}, provider, store, data.NewPlatformAuthorizationReader(postgres), data.NewPlatformIdentityLinkUnitOfWork(postgres), tokens, secrets, ids, clock)
	if err != nil {
		return nil, nil, err
	}
	oidc.WithIdentityLink(link)
	sessions, err := biz.NewPlatformSessionUsecase(redirect.Scheme+"://"+redirect.Host, runtime.AccessToken.Issuer, data.NewPlatformSessionUnitOfWork(postgres), throttle, tokens, secrets, ids, clock)
	if err != nil {
		return nil, nil, err
	}
	return oidc, sessions, nil
}
