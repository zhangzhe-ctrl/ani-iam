package data

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type CoreNATSConfiguration struct {
	URL         string                  `json:"url"`
	ServerName  string                  `json:"server_name"`
	InboxPrefix string                  `json:"inbox_prefix"`
	SeedFile    string                  `json:"seed_file"`
	CAFile      string                  `json:"ca_file"`
	Authority   CoreBrokerConfiguration `json:"authority"`
}
type coreNATSConsumer struct {
	config       CoreNATSConfiguration
	connection   *nats.Conn
	js           nats.JetStreamContext
	subscription *nats.Subscription
	refused      atomic.Bool
	mu           sync.Mutex
	pendingID    uuid.UUID
	pending      *nats.Msg
}

var coreConsumerInbox = regexp.MustCompile(`^_INBOX[.][A-Za-z0-9_-]{1,96}[.]iam$`)

func coreNATSSigningKey(c CoreNATSConfiguration) (nkeys.KeyPair, error) {
	if !filepath.IsAbs(c.SeedFile) {
		return nil, biz.ErrCoreBrokerAuthority
	}
	info, err := os.Lstat(c.SeedFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 512 {
		return nil, biz.ErrCoreBrokerAuthority
	}
	file, err := os.Open(c.SeedFile)
	if err != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, biz.ErrCoreBrokerAuthority
	}
	raw, err := io.ReadAll(io.LimitReader(file, 513))
	if err != nil || len(raw) > 512 {
		return nil, biz.ErrCoreBrokerAuthority
	}
	defer clear(raw)
	key, err := nkeys.FromSeed(bytes.TrimSpace(raw))
	if err != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	public, err := key.PublicKey()
	if err != nil || public != c.Authority.ConsumerNKey || !nkeys.IsValidPublicUserKey(public) {
		key.Wipe()
		return nil, biz.ErrCoreBrokerAuthority
	}
	return key, nil
}

// The connection is bound to a reviewed environment account and exclusive
// route. Check validates the broker/stream/consumer state; environment ACL
// application evidence is a separate prerequisite, never inferred from a
// message header or an INFO response that does not expose account identity.
func NewCoreNATSConsumer(ctx context.Context, c CoreNATSConfiguration) (biz.CoreBrokerConsumer, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "tls" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || c.ServerName == "" || !coreConsumerInbox.MatchString(c.InboxPrefix) || c.Authority.Validate() != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	ca, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, biz.ErrCoreBrokerAuthority
	}
	key, err := coreNATSSigningKey(c)
	if err != nil {
		return nil, err
	}
	key.Wipe()
	consumer := &coreNATSConsumer{config: c}
	connection, err := nats.Connect(c.URL, nats.Name(c.Authority.BrokerName+"-iam-consumer"), nats.Nkey(c.Authority.ConsumerNKey, func(nonce []byte) ([]byte, error) {
		key, err := coreNATSSigningKey(c)
		if err != nil {
			return nil, err
		}
		defer key.Wipe()
		return key.Sign(nonce)
	}),
		nats.Secure(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: c.ServerName, RootCAs: roots}), nats.TLSHandshakeFirst(), nats.IgnoreDiscoveredServers(), nats.CustomInboxPrefix(c.InboxPrefix),
		nats.Timeout(2*time.Second), nats.ReconnectWait(time.Second), nats.MaxReconnects(-1), nats.ReconnectBufSize(0), nats.PingInterval(5*time.Second), nats.MaxPingsOutstanding(2), nats.PermissionErrOnSubscribe(true),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			if errors.Is(err, nats.ErrAuthorization) || errors.Is(err, nats.ErrPermissionViolation) {
				consumer.refused.Store(true)
			}
		}))
	if err != nil {
		return nil, biz.ErrPersistenceUnavailable
	}
	consumer.connection = connection
	js, err := connection.JetStream(nats.MaxWait(2 * time.Second))
	if err != nil {
		connection.Close()
		return nil, biz.ErrPersistenceUnavailable
	}
	consumer.js = js
	if err = consumer.Check(ctx); err != nil {
		connection.Close()
		return nil, err
	}
	subscription, err := js.PullSubscribe("", c.Authority.Consumer, nats.Bind(c.Authority.Stream, c.Authority.Consumer), nats.ManualAck())
	if err != nil {
		connection.Close()
		return nil, biz.ErrPersistenceUnavailable
	}
	consumer.subscription = subscription
	return consumer, nil
}

func coreNATSSubjects() []string {
	subjects := []string{tenantbootstrap.Subject, governancev1.HeartbeatSubject, governancev1.LifecycleSubject}
	slices.Sort(subjects)
	return subjects
}
func validCoreNATSStream(c nats.StreamConfig, name string) bool {
	subjects := append([]string(nil), c.Subjects...)
	slices.Sort(subjects)
	return c.Name == name && slices.Equal(subjects, coreNATSSubjects()) && c.Retention == nats.LimitsPolicy && c.Storage == nats.FileStorage && c.Replicas == 1 && c.Mirror == nil && len(c.Sources) == 0 && c.SubjectTransform == nil && c.RePublish == nil && !c.Sealed && !c.NoAck && !c.AllowRollup && c.DenyDelete && c.DenyPurge && c.Discard == nats.DiscardNew && c.MaxMsgSize == 65536 && c.MaxBytes > 0 && c.MaxBytes <= 1<<30 && c.MaxAge >= 48*time.Hour && c.MaxAge <= 72*time.Hour
}
func validCoreNATSConsumer(c nats.ConsumerConfig, name string) bool {
	subjects := append([]string(nil), c.FilterSubjects...)
	slices.Sort(subjects)
	return c.Name == name && c.Durable == name && c.DeliverSubject == "" && c.DeliverGroup == "" && c.DeliverPolicy == nats.DeliverAllPolicy && c.AckPolicy == nats.AckExplicitPolicy && c.AckWait == 30*time.Second && c.MaxDeliver == -1 && len(c.BackOff) == 0 && c.FilterSubject == "" && slices.Equal(subjects, coreNATSSubjects()) && c.ReplayPolicy == nats.ReplayInstantPolicy && c.MaxAckPending == 1 && c.MaxWaiting == 4 && c.MaxRequestBatch == 1 && c.MaxRequestExpires == 2*time.Second && c.InactiveThreshold == 0 && c.Replicas == 1 && !c.MemoryStorage && !c.HeadersOnly && c.RateLimit == 0
}
func (c *coreNATSConsumer) Check(ctx context.Context) error {
	if c.refused.Load() {
		return biz.ErrCoreBrokerAuthority
	}
	if !c.connection.IsConnected() || c.connection.ConnectedServerName() != c.config.Authority.BrokerName {
		return biz.ErrPersistenceUnavailable
	}
	stream, err := c.js.StreamInfo(c.config.Authority.Stream, nats.Context(ctx))
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if !validCoreNATSStream(stream.Config, c.config.Authority.Stream) {
		return biz.ErrCoreBrokerAuthority
	}
	consumer, err := c.js.ConsumerInfo(c.config.Authority.Stream, c.config.Authority.Consumer, nats.Context(ctx))
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if !validCoreNATSConsumer(consumer.Config, c.config.Authority.Consumer) {
		return biz.ErrCoreBrokerAuthority
	}
	return nil
}
func (c *coreNATSConsumer) Next(ctx context.Context) (biz.CoreBrokerMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil {
		return biz.CoreBrokerMessage{}, biz.ErrCoreBrokerAuthority
	}
	if err := c.Check(ctx); err != nil {
		return biz.CoreBrokerMessage{}, err
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	messages, err := c.subscription.Fetch(1, nats.Context(call))
	if len(messages) != 1 {
		if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return biz.CoreBrokerMessage{}, context.DeadlineExceeded
		}
		return biz.CoreBrokerMessage{}, biz.ErrPersistenceUnavailable
	}
	message := messages[0]
	meta, err := message.Metadata()
	if err != nil || meta.Stream != c.config.Authority.Stream || meta.Consumer != c.config.Authority.Consumer || meta.Sequence.Stream == 0 || meta.Sequence.Stream > 1<<63-1 || meta.NumDelivered == 0 || meta.Timestamp.IsZero() {
		return biz.CoreBrokerMessage{}, biz.ErrCoreBrokerAuthority
	}
	id, err := uuid.NewV7()
	if err != nil {
		return biz.CoreBrokerMessage{}, biz.ErrPersistenceUnavailable
	}
	c.pendingID = id
	c.pending = message
	headers := map[string][]string{}
	for k, v := range message.Header {
		headers[k] = append([]string(nil), v...)
	}
	return biz.CoreBrokerMessage{DeliveryID: id, ConsumerID: c.config.Authority.ConsumerID, BrokerName: c.config.Authority.BrokerName, Account: c.config.Authority.Account, Stream: meta.Stream, Consumer: meta.Consumer, Subject: message.Subject, BrokerSequence: int64(meta.Sequence.Stream), DeliveryCount: meta.NumDelivered, PublishedAt: meta.Timestamp, Payload: append([]byte(nil), message.Data...), Headers: headers}, nil
}
func (c *coreNATSConsumer) finish(ctx context.Context, id uuid.UUID, ack bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil || id != c.pendingID {
		return biz.ErrCoreBrokerAuthority
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var err error
	if ack {
		err = c.pending.AckSync(nats.Context(call))
	} else {
		err = c.pending.NakWithDelay(time.Second, nats.Context(call))
	}
	c.pending = nil
	c.pendingID = uuid.Nil
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	return nil
}
func (c *coreNATSConsumer) Ack(ctx context.Context, id uuid.UUID) error {
	return c.finish(ctx, id, true)
}
func (c *coreNATSConsumer) Retry(ctx context.Context, id uuid.UUID) error {
	return c.finish(ctx, id, false)
}
func (c *coreNATSConsumer) Close() error { c.connection.Close(); return nil }
