package conf

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func validConfig() *Bootstrap {
	return &Bootstrap{
		Profile: IsolatedProfile,
		Server: &Server{
			Grpc: &Server_GRPC{
				Network: "tcp",
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
				Tls: &Server_GRPC_TLS{
					CertificateFile:      "/run/secrets/ani-iam-grpc.crt",
					PrivateKeyFile:       "/run/secrets/ani-iam-grpc.key",
					ClientCaFile:         "/run/secrets/ani-iam-client-ca.crt",
					GatewayClientDnsName: "ani-gateway",
				},
			},
			Admin:           &Server_Admin{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
			ShutdownTimeout: durationpb.New(5 * time.Second),
		},
		Runtime: &Runtime{
			Postgresql: &PostgreSQL{Dsn: "postgresql://ani_iam_runtime@127.0.0.1:5432/ani_iam?sslmode=disable"},
			Redis: &Redis{
				Addr:         "127.0.0.1:6379",
				Database:     0,
				Namespace:    "ani-iam:dp2-05",
				LoginLimit:   5,
				LoginWindow:  durationpb.New(15 * time.Minute),
				DialTimeout:  durationpb.New(500 * time.Millisecond),
				ReadTimeout:  durationpb.New(500 * time.Millisecond),
				WriteTimeout: durationpb.New(500 * time.Millisecond),
			},
			AccessToken: &AccessToken{
				Issuer:         "ani-iam",
				ActiveKeyId:    "dp2-05-ed25519-1",
				PrivateKeyFile: "/run/secrets/ani-iam-access-token-ed25519.pem",
			},
			Notification: &Notification{
				Address:              "127.0.0.1:29090",
				CertificateFile:      "/run/secrets/ani-iam-notification-client.crt",
				PrivateKeyFile:       "/run/secrets/ani-iam-notification-client.key",
				ServerCaFile:         "/run/secrets/ani-notification-server-ca.crt",
				ServerDnsName:        "ani-notification",
				ConsoleActionUrlBase: "https://console.example.test/password-action",
				Locale:               "en-US",
				DispatchInterval:     durationpb.New(250 * time.Millisecond),
				SubmissionTimeout:    durationpb.New(5 * time.Second),
			},
			Oidc: &OIDC{
				Provider:                "dex",
				IssuerUrl:               "https://dex.example.test/dex",
				ClientId:                "ani-console",
				ClientSecretFile:        "/run/secrets/ani-iam-dex-client-secret",
				LoginRedirectUri:        "https://console.example.test/auth/oidc/callback",
				IdentityLinkRedirectUri: "https://console.example.test/auth/oidc/link/callback",
				RecentReauthentication:  durationpb.New(10 * time.Minute),
				HttpTimeout:             durationpb.New(5 * time.Second),
			},
			PolicyRevision: "sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa",
		},
	}
}

func TestBootstrapValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Bootstrap)
		ok     bool
	}{
		{name: "isolated loopback", ok: true},
		{name: "wrong profile", mutate: func(c *Bootstrap) { c.Profile = "legacy-auth" }},
		{name: "externally reachable grpc", mutate: func(c *Bootstrap) { c.Server.Grpc.Addr = "0.0.0.0:19090" }},
		{name: "missing grpc mutual TLS", mutate: func(c *Bootstrap) { c.Server.Grpc.Tls = nil }},
		{name: "grpc certificate path is relative", mutate: func(c *Bootstrap) { c.Server.Grpc.Tls.CertificateFile = "server.crt" }},
		{name: "grpc private-key path is relative", mutate: func(c *Bootstrap) { c.Server.Grpc.Tls.PrivateKeyFile = "server.key" }},
		{name: "grpc client CA path is relative", mutate: func(c *Bootstrap) { c.Server.Grpc.Tls.ClientCaFile = "client-ca.crt" }},
		{name: "gateway workload identity missing", mutate: func(c *Bootstrap) { c.Server.Grpc.Tls.GatewayClientDnsName = "" }},
		{name: "hostname is not a literal boundary", mutate: func(c *Bootstrap) { c.Server.Admin.Addr = "localhost:19091" }},
		{name: "missing shutdown timeout", mutate: func(c *Bootstrap) { c.Server.ShutdownTimeout = nil }},
		{name: "missing runtime", mutate: func(c *Bootstrap) { c.Runtime = nil }},
		{name: "database DSN missing", mutate: func(c *Bootstrap) { c.Runtime.Postgresql.Dsn = "" }},
		{name: "database user is not the restricted runtime role", mutate: func(c *Bootstrap) {
			c.Runtime.Postgresql.Dsn = "postgresql://iam_runtime@127.0.0.1:5432/ani_iam?sslmode=disable"
		}},
		{name: "database host is not isolated", mutate: func(c *Bootstrap) {
			c.Runtime.Postgresql.Dsn = "postgresql://ani_iam_runtime@database.internal/ani_iam"
		}},
		{name: "Redis host is not isolated", mutate: func(c *Bootstrap) { c.Runtime.Redis.Addr = "redis.internal:6379" }},
		{name: "Redis namespace missing", mutate: func(c *Bootstrap) { c.Runtime.Redis.Namespace = "" }},
		{name: "Redis login limit invalid", mutate: func(c *Bootstrap) { c.Runtime.Redis.LoginLimit = 0 }},
		{name: "Redis login window missing", mutate: func(c *Bootstrap) { c.Runtime.Redis.LoginWindow = nil }},
		{name: "access-token issuer missing", mutate: func(c *Bootstrap) { c.Runtime.AccessToken.Issuer = "" }},
		{name: "access-token active key missing", mutate: func(c *Bootstrap) { c.Runtime.AccessToken.ActiveKeyId = "" }},
		{name: "access-token private-key path missing", mutate: func(c *Bootstrap) { c.Runtime.AccessToken.PrivateKeyFile = "" }},
		{name: "notification config missing", mutate: func(c *Bootstrap) { c.Runtime.Notification = nil }},
		{name: "notification address is not isolated", mutate: func(c *Bootstrap) { c.Runtime.Notification.Address = "notification.internal:443" }},
		{name: "notification certificate path is relative", mutate: func(c *Bootstrap) { c.Runtime.Notification.CertificateFile = "client.crt" }},
		{name: "notification private-key path is relative", mutate: func(c *Bootstrap) { c.Runtime.Notification.PrivateKeyFile = "client.key" }},
		{name: "notification server CA path is relative", mutate: func(c *Bootstrap) { c.Runtime.Notification.ServerCaFile = "server-ca.crt" }},
		{name: "notification server identity differs", mutate: func(c *Bootstrap) { c.Runtime.Notification.ServerDnsName = "notification.internal" }},
		{name: "notification action URL is HTTP", mutate: func(c *Bootstrap) {
			c.Runtime.Notification.ConsoleActionUrlBase = "http://console.example.test/password-action"
		}},
		{name: "notification action URL has query", mutate: func(c *Bootstrap) { c.Runtime.Notification.ConsoleActionUrlBase += "?token=preconfigured" }},
		{name: "notification locale unsupported", mutate: func(c *Bootstrap) { c.Runtime.Notification.Locale = "en-GB" }},
		{name: "notification dispatch interval missing", mutate: func(c *Bootstrap) { c.Runtime.Notification.DispatchInterval = nil }},
		{name: "notification submission timeout missing", mutate: func(c *Bootstrap) { c.Runtime.Notification.SubmissionTimeout = nil }},
		{name: "OIDC config missing", mutate: func(c *Bootstrap) { c.Runtime.Oidc = nil }},
		{name: "OIDC provider differs", mutate: func(c *Bootstrap) { c.Runtime.Oidc.Provider = "google" }},
		{name: "OIDC issuer is not canonical", mutate: func(c *Bootstrap) { c.Runtime.Oidc.IssuerUrl = " https://dex.example.test/dex" }},
		{name: "OIDC issuer is insecure non-loopback", mutate: func(c *Bootstrap) { c.Runtime.Oidc.IssuerUrl = "http://dex.example.test/dex" }},
		{name: "OIDC HTTP issuer must use a literal loopback", mutate: func(c *Bootstrap) { c.Runtime.Oidc.IssuerUrl = "http://localhost:5556/dex" }},
		{name: "OIDC loopback issuer is allowed", ok: true, mutate: func(c *Bootstrap) { c.Runtime.Oidc.IssuerUrl = "http://127.0.0.1:5556/dex" }},
		{name: "OIDC client ID missing", mutate: func(c *Bootstrap) { c.Runtime.Oidc.ClientId = "" }},
		{name: "OIDC client secret path is relative", mutate: func(c *Bootstrap) { c.Runtime.Oidc.ClientSecretFile = "oidc-client-secret" }},
		{name: "OIDC login redirect is HTTP", mutate: func(c *Bootstrap) { c.Runtime.Oidc.LoginRedirectUri = "http://console.example.test/auth/oidc/callback" }},
		{name: "OIDC link redirect has query", mutate: func(c *Bootstrap) { c.Runtime.Oidc.IdentityLinkRedirectUri += "?next=account" }},
		{name: "OIDC redirects are not distinct", mutate: func(c *Bootstrap) { c.Runtime.Oidc.IdentityLinkRedirectUri = c.Runtime.Oidc.LoginRedirectUri }},
		{name: "OIDC recent reauthentication missing", mutate: func(c *Bootstrap) { c.Runtime.Oidc.RecentReauthentication = nil }},
		{name: "OIDC recent reauthentication too long", mutate: func(c *Bootstrap) { c.Runtime.Oidc.RecentReauthentication = durationpb.New(2 * time.Hour) }},
		{name: "OIDC HTTP timeout missing", mutate: func(c *Bootstrap) { c.Runtime.Oidc.HttpTimeout = nil }},
		{name: "OIDC HTTP timeout too long", mutate: func(c *Bootstrap) { c.Runtime.Oidc.HttpTimeout = durationpb.New(time.Minute) }},
		{name: "policy revision malformed", mutate: func(c *Bootstrap) { c.Runtime.PolicyRevision = "main" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := proto.Clone(validConfig()).(*Bootstrap)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			err := cfg.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}
