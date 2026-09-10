//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// All resources belong to this exact remote run; no shared DSN, port, namespace,
// container, volume or cleanup selector is accepted.
func isolatedContainer(t *testing.T, kind, port string) testcontainers.CustomizeRequestOption {
	t.Helper()
	runDir, goal := isolatedRun(t)
	name := "ani-iam-" + filepath.Base(runDir) + "-" + kind + "-" + uuid.NewString()
	writeRecord := func(event, id string, details ...map[string]string) error {
		record := map[string]string{"event": event, "container_id": id, "name": name, "kind": kind, "run": filepath.Base(runDir), "test": t.Name(), "port": port}
		for _, fields := range details {
			for key, value := range fields {
				record[key] = value
			}
		}
		content, err := json.Marshal(record)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(runDir, "resources.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(append(content, '\n'))
		return err
	}
	return func(req *testcontainers.GenericContainerRequest) error {
		if req.Reuse || req.Name != "" || req.HostConfigModifier != nil || len(req.Mounts) != 0 {
			return fmt.Errorf("unexpected shared container configuration")
		}
		req.Name = name
		if req.Labels == nil {
			req.Labels = map[string]string{}
		}
		req.Labels["ani.goal"] = goal
		req.Labels["ani.run_id"] = filepath.Base(runDir)
		req.HostConfigModifier = func(config *dockercontainer.HostConfig) {
			config.PublishAllPorts = false
			config.PortBindings = network.PortMap{network.MustParsePort(port): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: ""}}}
		}
		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PostCreates: []testcontainers.ContainerHook{func(_ context.Context, c testcontainers.Container) error {
				return writeRecord("created", c.GetContainerID())
			}},
			PostReadies: []testcontainers.ContainerHook{func(ctx context.Context, c testcontainers.Container) error {
				inspected, err := c.Inspect(ctx)
				if err != nil {
					return err
				}
				if inspected.NetworkSettings == nil {
					return fmt.Errorf("missing isolated network state")
				}
				bindings := inspected.NetworkSettings.Ports
				if len(bindings) != 1 {
					return fmt.Errorf("unexpected exposed port count")
				}
				endpoint := ""
				for _, rows := range bindings {
					for _, b := range rows {
						endpoint = b.HostIP.String() + ":" + b.HostPort
						if b.HostIP != netip.MustParseAddr("127.0.0.1") || b.HostPort == "" {
							return fmt.Errorf("port is not bound exclusively to loopback")
						}
					}
				}
				return writeRecord("ready_loopback_verified", c.GetContainerID(), map[string]string{"endpoint": endpoint, "image_id": inspected.Image})
			}},
			PostTerminates: []testcontainers.ContainerHook{func(_ context.Context, c testcontainers.Container) error {
				return writeRecord("terminated", c.GetContainerID())
			}},
		})
		return writeRecord("planned", "")
	}
}

func newIsolatedRedis(t *testing.T, ctx context.Context) (*redis.Client, testcontainers.Container) {
	t.Helper()
	password := randomPassword(t)
	recordFixtureSecrets(t, map[string]string{"redis": password})
	c, err := testcontainers.Run(ctx, redisImage,
		testcontainers.WithCmd("redis-server", "--requirepass", password),
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute)),
		isolatedContainer(t, "redis", "6379/tcp"),
	)
	if err != nil {
		t.Fatalf("start isolated Redis: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := testcontainers.TerminateContainer(c, testcontainers.StopContext(closeCtx)); err != nil {
			t.Errorf("terminate registered Redis: %v", err)
		}
	})
	endpoint, err := c.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("resolve isolated Redis endpoint: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: normalizeProcessE2ELoopbackAddress(t, endpoint), Password: password, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("authenticate isolated Redis: %v", err)
	}
	return client, c
}

// References alone are returned as evidence. Values remain in per-fixture
// private directories on the VM so repeated fixtures never overwrite a secret.
func recordFixtureSecrets(t *testing.T, values map[string]string) {
	t.Helper()
	run, _ := isolatedRun(t)
	directory := filepath.Join(run, "private", "fixture-"+uuid.NewString())
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	references := map[string]string{}
	for purpose, value := range values {
		if purpose != "provisioner-db" && purpose != "runtime-db" && purpose != "migration-db" && purpose != "redis" && purpose != "dex-client" && purpose != "dex-user" {
			t.Fatal("unknown fixture credential purpose")
		}
		file := filepath.Join(directory, purpose+".secret")
		if err := os.WriteFile(file, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		references[purpose] = file
	}
	encoded, err := json.Marshal(map[string]any{"test": t.Name(), "references": references})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(run, "credential-references.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		t.Fatal(err)
	}
}

func isolatedRun(t *testing.T) (string, string) {
	t.Helper()
	for _, entry := range []struct{ variable, prefix, goal string }{
		{"WR19_RUN_DIR", "/home/ubuntu/workspace/ani-iam-runs/wr19-", "wr19"},
		{"WR18_RUN_DIR", "/home/ubuntu/workspace/ani-iam-runs/wr17-18-", "wr17-18"},
	} {
		run := os.Getenv(entry.variable)
		if run == "" {
			continue
		}
		if !strings.HasPrefix(run, entry.prefix) || filepath.Clean(run) != run || strings.Contains(strings.TrimPrefix(run, entry.prefix), "/") {
			t.Fatal("integration resource directory is not a registered isolated run")
		}
		return run, entry.goal
	}
	t.Fatal("integration resources require a registered remote WR19/WR18 run")
	return "", ""
}
