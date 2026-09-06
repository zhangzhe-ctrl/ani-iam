package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
)

func TestBuildAppFailsClosedWhenSigningKeyIsUnavailable(t *testing.T) {
	bootstrap := &conf.Bootstrap{
		Profile: conf.IsolatedProfile,
		Server: &conf.Server{
			Grpc: &conf.Server_GRPC{
				Network: "tcp",
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
				Tls: &conf.Server_GRPC_TLS{
					CertificateFile:      "/nonexistent/ani-iam-dp2-05-server.crt",
					PrivateKeyFile:       "/nonexistent/ani-iam-dp2-05-server.key",
					ClientCaFile:         "/nonexistent/ani-iam-dp2-05-client-ca.crt",
					GatewayClientDnsName: "ani-gateway",
				},
			},
			Admin: &conf.Server_Admin{
				Network: "tcp",
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
			},
			ShutdownTimeout: durationpb.New(time.Second),
		},
		Runtime: &conf.Runtime{
			Postgresql: &conf.PostgreSQL{Dsn: "postgresql://ani_iam_runtime@127.0.0.1:1/ani_iam?sslmode=disable"},
			Redis: &conf.Redis{
				Addr:         "127.0.0.1:1",
				Namespace:    "ani-iam:dp2-05:test",
				LoginLimit:   5,
				LoginWindow:  durationpb.New(15 * time.Minute),
				DialTimeout:  durationpb.New(100 * time.Millisecond),
				ReadTimeout:  durationpb.New(100 * time.Millisecond),
				WriteTimeout: durationpb.New(100 * time.Millisecond),
			},
			AccessToken: &conf.AccessToken{
				Issuer:         "ani-iam",
				ActiveKeyId:    "dp2-05-test",
				PrivateKeyFile: "/nonexistent/ani-iam-dp2-05-ed25519.pem",
			},
			PolicyRevision: data.TargetPolicyRevision,
		},
	}

	app, err := buildApp(bootstrap, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if app != nil {
		t.Fatal("buildApp() returned an app without its configured signing key")
	}
	if err == nil || !strings.Contains(err.Error(), "access-token private key") {
		t.Fatalf("buildApp() error = %v, want signing-key failure", err)
	}
}
