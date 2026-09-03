package conf

import (
	"fmt"
	"net"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

const IsolatedProfile = "cp0-isolated"

func (c *Bootstrap) Validate() error {
	if c == nil {
		return fmt.Errorf("bootstrap config is required")
	}
	if c.Profile != IsolatedProfile {
		return fmt.Errorf("runtime profile must be %q", IsolatedProfile)
	}
	if c.Server == nil || c.Server.Grpc == nil || c.Server.Admin == nil {
		return fmt.Errorf("grpc and admin server config are required")
	}
	if err := validateListener("grpc", c.Server.Grpc.Network, c.Server.Grpc.Addr, c.Server.Grpc.Timeout); err != nil {
		return err
	}
	if err := validateListener("admin", c.Server.Admin.Network, c.Server.Admin.Addr, c.Server.Admin.Timeout); err != nil {
		return err
	}
	if c.Server.Grpc.Addr == c.Server.Admin.Addr && c.Server.Grpc.Addr != "127.0.0.1:0" && c.Server.Grpc.Addr != "[::1]:0" {
		return fmt.Errorf("grpc and admin listeners must use distinct addresses")
	}
	return validateDuration("shutdown", c.Server.ShutdownTimeout, 30*time.Second)
}

func validateListener(name, network, address string, timeout *durationpb.Duration) error {
	if network != "tcp" {
		return fmt.Errorf("%s network must be tcp", name)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s address: %w", name, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s address must use a literal loopback IP", name)
	}
	if port == "" {
		return fmt.Errorf("%s address requires a port", name)
	}
	return validateDuration(name, timeout, 30*time.Second)
}

func validateDuration(name string, value *durationpb.Duration, maximum time.Duration) error {
	if value == nil {
		return fmt.Errorf("%s timeout is required", name)
	}
	if err := value.CheckValid(); err != nil {
		return fmt.Errorf("%s timeout: %w", name, err)
	}
	duration := value.AsDuration()
	if duration <= 0 || duration > maximum {
		return fmt.Errorf("%s timeout must be within 1ns..%s", name, maximum)
	}
	return nil
}
