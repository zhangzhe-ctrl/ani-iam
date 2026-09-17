package data

import (
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoreNATSConsumerBoundary(t *testing.T) {
	for _, prefix := range []string{"_INBOX.iam-gov-20260916-073847.iam", "_INBOX.private_01.iam"} {
		if !coreConsumerInbox.MatchString(prefix) {
			t.Fatal("valid task inbox rejected")
		}
	}
	for _, prefix := range []string{"_WR23.old.consumer", "_INBOX.>.iam", "_INBOX.*.iam", "_INBOX..iam", "_INBOX.run.producer", "_INBOX.run.iam.extra"} {
		if coreConsumerInbox.MatchString(prefix) {
			t.Fatal("unscoped or legacy inbox accepted")
		}
	}
	good := nats.ConsumerConfig{Name: "IAM", Durable: "IAM", DeliverPolicy: nats.DeliverAllPolicy, AckPolicy: nats.AckExplicitPolicy, AckWait: 30 * time.Second, MaxDeliver: -1, FilterSubjects: coreNATSSubjects(), MaxAckPending: 1, MaxWaiting: 4, MaxRequestBatch: 1, MaxRequestExpires: 2 * time.Second, Replicas: 1}
	if !validCoreNATSConsumer(good, "IAM") {
		t.Fatal("expected exact consumer")
	}
	for _, change := range []func(*nats.ConsumerConfig){func(c *nats.ConsumerConfig) { c.FilterSubjects = []string{"ani.integration.tenant.>"} }, func(c *nats.ConsumerConfig) { c.AckPolicy = nats.AckNonePolicy }, func(c *nats.ConsumerConfig) { c.DeliverSubject = "other.inbox" }, func(c *nats.ConsumerConfig) { c.MaxDeliver = 10 }, func(c *nats.ConsumerConfig) { c.HeadersOnly = true }, func(c *nats.ConsumerConfig) { c.MemoryStorage = true }, func(c *nats.ConsumerConfig) { c.InactiveThreshold = time.Minute }, func(c *nats.ConsumerConfig) { c.Durable = "other" }} {
		bad := good
		change(&bad)
		if validCoreNATSConsumer(bad, "IAM") {
			t.Fatal("unsafe consumer accepted")
		}
	}
	key, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	defer key.Wipe()
	public, _ := key.PublicKey()
	seed, _ := key.Seed()
	defer clear(seed)
	file := filepath.Join(t.TempDir(), "consumer.seed")
	if os.WriteFile(file, seed, 0600) != nil {
		t.Fatal("private file")
	}
	cfg := CoreNATSConfiguration{SeedFile: file, Authority: CoreBrokerConfiguration{ConsumerNKey: public}}
	loaded, err := coreNATSSigningKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := loaded.Sign([]byte("fresh challenge"))
	loaded.Wipe()
	if err != nil || key.Verify([]byte("fresh challenge"), signature) != nil {
		t.Fatal("NKey proof")
	}
	if os.Chmod(file, 0644) != nil {
		t.Fatal("chmod")
	}
	if _, err = coreNATSSigningKey(cfg); err == nil {
		t.Fatal("world-readable seed")
	}
	if os.Chmod(file, 0600) != nil {
		t.Fatal("chmod")
	}
	cfg.Authority.ConsumerNKey = "UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err = coreNATSSigningKey(cfg); err == nil {
		t.Fatal("replaced identity")
	}
}
