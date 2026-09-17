package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/nats-io/nkeys"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
)

func TestTenantRuntimeConfigurationSingleContractAndDeploymentAddress(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	key := func() string {
		k, err := nkeys.CreateUser()
		if err != nil {
			t.Fatal(err)
		}
		defer k.Wipe()
		v, err := k.PublicKey()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	c := coreLifecycleConfiguration{Version: 2, ProjectionMode: "enforced"}
	c.Broker.Authority = data.CoreBrokerConfiguration{Environment: "wr33-runtime", TrustDomain: "wr33.test", BrokerName: "wr33-broker", Account: "WR33", Stream: "WR33_TENANT", Consumer: "IAM", ConsumerID: id(), ConsumerBindingID: id(), ConsumerNKey: key(), Producer: governancev1.Producer}
	binding, public := id(), key()
	for _, subject := range []string{governancev1.LifecycleSubject, tenantbootstrap.Subject, governancev1.HeartbeatSubject} {
		r := data.CoreBrokerRoute{ID: id(), Subject: subject, ProducerBindingID: binding, ProducerNKey: public}
		r.TargetSHA256 = c.Broker.Authority.RouteRevision(r)
		c.Broker.Authority.Routes = append(c.Broker.Authority.Routes, r)
	}
	c.Snapshot.ReaderID = id()
	c.Snapshot.Origin = "https://governance.namespace.svc:8443"
	c.Snapshot.IAMAddress = "iam.namespace.svc:9000"
	path := filepath.Join(t.TempDir(), "tenant-runtime.json")
	runtime := &conf.Runtime{Environment: c.Broker.Authority.Environment, TrustDomain: c.Broker.Authority.TrustDomain, TenantLifecycleFile: path}
	check := func(c coreLifecycleConfiguration, want bool) {
		t.Helper()
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		runtime.TenantLifecycleSha256 = hex.EncodeToString(sum[:])
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = readCoreLifecycleConfiguration(runtime)
		if (err == nil) != want {
			t.Fatalf("configuration acceptance: want %v, got %v", want, err)
		}
	}
	check(c, true)
	old := c
	old.Version = 1
	check(old, false)
	absent := c
	absent.Snapshot.ReaderID = uuid.Nil
	check(absent, false)
	bad := c
	bad.Snapshot.IAMAddress = "dns:///iam.namespace.svc:9000"
	check(bad, false)
	oldRoutes := c
	oldRoutes.Broker.Authority.Routes = append([]data.CoreBrokerRoute(nil), c.Broker.Authority.Routes...)
	oldRoutes.Broker.Authority.Routes[0].Subject = "ani.integration.tenant.lifecycle.v1"
	oldRoutes.Broker.Authority.Routes[0].TargetSHA256 = oldRoutes.Broker.Authority.RouteRevision(oldRoutes.Broker.Authority.Routes[0])
	check(oldRoutes, false)
}
