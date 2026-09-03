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
			Grpc:            &Server_GRPC{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
			Admin:           &Server_Admin{Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(time.Second)},
			ShutdownTimeout: durationpb.New(5 * time.Second),
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
		{name: "hostname is not a literal boundary", mutate: func(c *Bootstrap) { c.Server.Admin.Addr = "localhost:19091" }},
		{name: "missing shutdown timeout", mutate: func(c *Bootstrap) { c.Server.ShutdownTimeout = nil }},
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
