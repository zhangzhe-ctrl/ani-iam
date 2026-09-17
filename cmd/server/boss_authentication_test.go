package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/workloadregistry"
	"google.golang.org/protobuf/types/known/durationpb"
)

// This is a composition test; real signed login and permission execution remain
// joint-process gates. Discovery is the only HTTP operation during construction.
func TestBossAuthenticationUsesDeploymentRegistry(t *testing.T) {
	path := filepath.Join("..", "..", "registrations", "governance-workload-targets.v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	registry, err := workloadregistry.Load(path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := data.WorkloadPolicyRevision(registry)
	if err != nil {
		t.Fatal(err)
	}
	if revision == data.TargetPolicyRevision {
		t.Fatal("fixture must extend the default permission catalog")
	}

	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer provider.Close()
	issuer = provider.URL
	secret := filepath.Join(t.TempDir(), "oidc.secret")
	if err = os.WriteFile(secret, []byte(strings.Repeat("x", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := &conf.Runtime{Environment: "wr33-boss-registry", PolicyRevision: revision, Redis: &conf.Redis{Namespace: "wr33:boss:registry"}, AccessToken: &conf.AccessToken{Issuer: "ani-iam"}, BossOidc: &conf.OIDC{
		Provider: "dex", IssuerUrl: issuer, ClientId: "ani-boss", ClientSecretFile: secret, LoginRedirectUri: "https://boss.example.test/auth/oidc/callback", IdentityLinkRedirectUri: "https://boss.example.test/auth/oidc/link/callback", HttpTimeout: durationpb.New(time.Second), RecentReauthentication: durationpb.New(10 * time.Minute),
	}}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	throttle, err := data.NewRedisLoginThrottle(client, data.RedisLoginThrottleConfig{Namespace: runtime.Redis.Namespace, Limit: 5, Window: 15 * time.Minute, BaseDelay: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	construct := func() (*biz.BossOIDCUsecase, *biz.PlatformSessionUsecase, error) {
		return newBossAuthentication(context.Background(), runtime, data.NewData(nil), client, throttle, &data.JWXAccessTokenCodec{}, data.NewSecretGenerator(), data.NewUUIDv7Generator(), data.NewSystemClock(), registry)
	}
	oidc, sessions, err := construct()
	if err != nil || oidc == nil || sessions == nil {
		t.Fatalf("BOSS with deployment registry: oidc=%v sessions=%v err=%v", oidc != nil, sessions != nil, err)
	}
	runtime.PolicyRevision = data.TargetPolicyRevision
	oidc, sessions, err = construct()
	var mismatch *biz.AuthorizationPolicyMismatchError
	if oidc != nil || sessions != nil || !errors.As(err, &mismatch) {
		t.Fatalf("stale BOSS policy was not rejected: %v", err)
	}
}
