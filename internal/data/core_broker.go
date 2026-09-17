package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	governancev1 "github.com/zhangzhe-ctrl/ani-governance/api/governance/v1"
	"github.com/zhangzhe-ctrl/ani-iam/sdk/tenantbootstrap"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data/sqlcgen"
)

type CoreBrokerRoute struct {
	ID                uuid.UUID `json:"id"`
	Subject           string    `json:"subject"`
	ProducerBindingID uuid.UUID `json:"producer_binding_id"`
	ProducerNKey      string    `json:"producer_nkey"`
	TargetSHA256      string    `json:"target_sha256"`
}
type CoreBrokerConfiguration struct {
	Environment       string            `json:"environment"`
	TrustDomain       string            `json:"trust_domain"`
	BrokerName        string            `json:"broker_name"`
	Account           string            `json:"account"`
	Stream            string            `json:"stream"`
	Consumer          string            `json:"consumer"`
	ConsumerID        uuid.UUID         `json:"consumer_id"`
	ConsumerBindingID uuid.UUID         `json:"consumer_binding_id"`
	ConsumerNKey      string            `json:"consumer_nkey"`
	Producer          string            `json:"producer"`
	Routes            []CoreBrokerRoute `json:"routes"`
}

var coreBrokerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)
var coreBrokerNKey = regexp.MustCompile(`^U[A-Z2-7]{55}$`)
var coreBrokerDomain = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)

// RouteRevision binds a reviewed, unique publisher mapping. It is independent
// of mutable IAM Grant versions and of the environment's applied ACL revision.
func (c CoreBrokerConfiguration) RouteRevision(route CoreBrokerRoute) string {
	raw, _ := json.Marshal(struct {
		ID          uuid.UUID `json:"id"`
		Environment string    `json:"environment"`
		TrustDomain string    `json:"trust_domain"`
		BrokerName  string    `json:"broker_name"`
		Account     string    `json:"account"`
		Stream      string    `json:"stream"`
		Subject     string    `json:"subject"`
		SchemaMajor int       `json:"schema_major"`
		Producer    string    `json:"producer"`
		Binding     uuid.UUID `json:"producer_binding_id"`
		NKey        string    `json:"producer_nkey"`
	}{route.ID, c.Environment, c.TrustDomain, c.BrokerName, c.Account, c.Stream, route.Subject, 1, c.Producer, route.ProducerBindingID, route.ProducerNKey})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c CoreBrokerConfiguration) Validate() error {
	if !coreBrokerDomain.MatchString(c.Environment) || !coreBrokerDomain.MatchString(c.TrustDomain) || !coreBrokerName.MatchString(c.BrokerName) || !coreBrokerName.MatchString(c.Account) || !coreBrokerName.MatchString(c.Stream) || !coreBrokerName.MatchString(c.Consumer) || c.ConsumerID.Version() != 7 || c.ConsumerBindingID.Version() != 7 || !coreBrokerNKey.MatchString(c.ConsumerNKey) || !biz.ValidCoreProjectionProducer(c.Producer) || len(c.Routes) != 3 {
		return biz.ErrCoreBrokerAuthority
	}
	expected := map[string]bool{governancev1.LifecycleSubject: false, tenantbootstrap.Subject: false, governancev1.HeartbeatSubject: false}
	ids := map[uuid.UUID]bool{}
	for _, r := range c.Routes {
		seen, ok := expected[r.Subject]
		if !ok || seen || ids[r.ID] || r.ID.Version() != 7 || r.ProducerBindingID.Version() != 7 || r.ProducerBindingID == c.ConsumerBindingID || !coreBrokerNKey.MatchString(r.ProducerNKey) || r.ProducerNKey == c.ConsumerNKey || r.TargetSHA256 != c.RouteRevision(r) {
			return biz.ErrCoreBrokerAuthority
		}
		expected[r.Subject] = true
		ids[r.ID] = true
	}
	return nil
}
func (c CoreBrokerConfiguration) route(subject string) (CoreBrokerRoute, error) {
	for _, r := range c.Routes {
		if r.Subject == subject {
			return r, nil
		}
	}
	return CoreBrokerRoute{}, biz.ErrCoreBrokerAuthority
}
func (c CoreBrokerConfiguration) message(m biz.CoreBrokerMessage) error {
	if m.ConsumerID != c.ConsumerID || m.Consumer != c.Consumer || m.BrokerName != c.BrokerName || m.Account != c.Account || m.Stream != c.Stream || m.BrokerSequence < 1 || m.DeliveryID.Version() != 7 || m.DeliveryCount < 1 || m.DeliveryCount > 1<<63-1 || m.PublishedAt.IsZero() || len(m.Subject) == 0 || len(m.Subject) > 256 || len(m.Payload) > 65536 {
		return biz.ErrCoreBrokerAuthority
	}
	return nil
}

type coreBrokerRepository struct {
	data   *Data
	config CoreBrokerConfiguration
}

func newCoreBrokerRepository(d *Data, c CoreBrokerConfiguration) (*coreBrokerRepository, error) {
	if d == nil || d.pool == nil || c.Validate() != nil {
		return nil, biz.ErrCoreBrokerAuthority
	}
	c.Routes = append([]CoreBrokerRoute(nil), c.Routes...)
	return &coreBrokerRepository{d, c}, nil
}
func NewCoreBrokerRepository(d *Data, c CoreBrokerConfiguration) (biz.CoreBrokerRepository, error) {
	return newCoreBrokerRepository(d, c)
}
func NewCoreBrokerBootstrapAuthorizer(d *Data, c CoreBrokerConfiguration) (biz.CoreBootstrapAuthorizer, error) {
	return newCoreBrokerRepository(d, c)
}

type coreBrokerAuthority struct {
	route                         CoreBrokerRoute
	producer, receiver, execution sqlcgen.ReadCurrentCoreBrokerGrantRow
}

func (r *coreBrokerRepository) current(ctx context.Context, q *sqlcgen.Queries, route CoreBrokerRoute) (coreBrokerAuthority, error) {
	result := coreBrokerAuthority{route: route}
	if err := q.LockCoreBrokerAuthority(ctx); err != nil {
		return result, mapPostgresError("lock broker current authority", err, nil)
	}
	lookup := func(binding uuid.UUID, nkey, action string) (sqlcgen.ReadCurrentCoreBrokerGrantRow, error) {
		v, err := q.ReadCurrentCoreBrokerGrant(ctx, sqlcgen.ReadCurrentCoreBrokerGrantParams{BindingID: binding, NkeyPublic: nkey, RouteID: route.ID, Action: action, Environment: r.config.Environment, TrustDomain: r.config.TrustDomain, BrokerName: r.config.BrokerName, AccountName: r.config.Account, StreamName: r.config.Stream, Subject: route.Subject, SchemaMajor: 1, TargetSha256: route.TargetSHA256})
		if errors.Is(err, pgx.ErrNoRows) {
			return v, biz.ErrCoreBrokerAuthority
		}
		if err != nil {
			return v, mapPostgresError("read current broker Grant", err, nil)
		}
		if v.Producer != r.config.Producer || v.ProducerBindingID != route.ProducerBindingID {
			return v, biz.ErrCoreBrokerAuthority
		}
		return v, nil
	}
	var err error
	if result.producer, err = lookup(route.ProducerBindingID, route.ProducerNKey, "publish"); err != nil {
		return result, err
	}
	if result.receiver, err = lookup(r.config.ConsumerBindingID, r.config.ConsumerNKey, "receive"); err != nil {
		return result, err
	}
	if result.producer.PrincipalID == result.receiver.PrincipalID {
		return result, biz.ErrCoreBrokerAuthority
	}
	if route.Subject == tenantbootstrap.Subject {
		if result.execution, err = lookup(r.config.ConsumerBindingID, r.config.ConsumerNKey, "execute"); err != nil {
			return result, err
		}
	}
	return result, nil
}
func (a coreBrokerAuthority) matches(v sqlcgen.TenantBrokerAuthorityReceipt) bool {
	p, c, x := a.producer, a.receiver, a.execution
	return v.RouteID == a.route.ID && v.RouteVersion == p.RouteVersion && v.ProducerPrincipalID == p.PrincipalID && v.ProducerBindingID == p.BindingID && v.ProducerPrincipalVersion == p.PrincipalVersion && v.ProducerBindingVersion == p.BindingVersion && v.ProducerGrantID == p.GrantID && v.ProducerGrantVersion == p.GrantVersion && v.ExecutorPrincipalID == c.PrincipalID && v.ExecutorBindingID == c.BindingID && v.ExecutorPrincipalVersion == c.PrincipalVersion && v.ExecutorBindingVersion == c.BindingVersion && v.ExecutorGrantID == c.GrantID && v.ExecutorGrantVersion == c.GrantVersion && ((x.GrantID == uuid.Nil && !v.ExecutionGrantID.Valid) || (x.GrantID != uuid.Nil && v.ExecutionGrantID.Valid && v.ExecutionGrantID.Bytes == x.GrantID && v.ExecutionGrantVersion.Valid && v.ExecutionGrantVersion.Int64 == x.GrantVersion))
}
func (r *coreBrokerRepository) Check(ctx context.Context) error {
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapPostgresError("begin broker readiness", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	for _, route := range r.config.Routes {
		if _, err := r.current(ctx, sqlcgen.New(tx), route); err != nil {
			return err
		}
	}
	return mapPostgresError("finish broker readiness", tx.Commit(ctx), nil)
}

func (r *coreBrokerRepository) Receive(ctx context.Context, message biz.CoreBrokerMessage, decoded biz.CoreBrokerDecoded) error {
	if err := r.config.message(message); err != nil {
		return err
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapPostgresError("begin broker receipt", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	sum := sha256.Sum256(message.Payload)
	prior, err := q.ReadCoreBrokerDLQPosition(ctx, sqlcgen.ReadCoreBrokerDLQPositionParams{ConsumerID: message.ConsumerID, BrokerSequence: message.BrokerSequence})
	if err == nil {
		if !bytes.Equal(prior.RawSha256, sum[:]) || prior.Subject != message.Subject {
			return biz.ErrCoreProjectionConflict
		}
		return biz.ErrCoreBrokerQuarantined
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return mapPostgresError("check quarantined delivery", err, nil)
	}
	if _, err = r.receiveInTransaction(ctx, q, message, decoded); err != nil {
		return err
	}
	return mapPostgresError("commit broker receipt and projection", tx.Commit(ctx), nil)
}

// Only ordinary durable receive and the capability-authorized DLQ transaction
// call this unexported helper. It never clears a quarantine or ACKs a delivery.
func (r *coreBrokerRepository) receiveInTransaction(ctx context.Context, q *sqlcgen.Queries, message biz.CoreBrokerMessage, decoded biz.CoreBrokerDecoded) (biz.TenantBrokerReceipt, error) {
	var result biz.TenantBrokerReceipt
	if err := r.config.message(message); err != nil {
		return result, err
	}
	m := decoded
	if m.Validate() != nil || !bytes.Equal(m.RawPayload, message.Payload) || m.Producer != r.config.Producer {
		return result, biz.ErrCoreProjectionInvalid
	}
	route, err := r.config.route(message.Subject)
	if err != nil {
		return result, err
	}
	kinds := map[string]string{governancev1.LifecycleSubject: "lifecycle", tenantbootstrap.Subject: "bootstrap", governancev1.HeartbeatSubject: "heartbeat"}
	if m.Kind != kinds[message.Subject] || (m.Kind == "bootstrap") != (decoded.Bootstrap != nil) {
		return result, biz.ErrCoreProjectionInvalid
	}
	sum := sha256.Sum256(message.Payload)
	authority, err := r.current(ctx, q, route)
	if err != nil {
		return result, err
	}
	if err = q.InitializeTenantLifecyclePipeline(ctx, sqlcgen.InitializeTenantLifecyclePipelineParams{Producer: r.config.Producer}); err != nil {
		return result, mapPostgresError("initialize Tenant pipeline", err, nil)
	}
	if _, err = q.LockTenantLifecyclePipeline(ctx, sqlcgen.LockTenantLifecyclePipelineParams{Producer: r.config.Producer}); err != nil {
		return result, mapPostgresError("lock Tenant receipt order", err, nil)
	}
	tenant := pgtype.UUID{}
	if m.TenantID != uuid.Nil {
		tenant = requiredPGUUID(m.TenantID)
	}
	prior, err := q.ReadCoreBrokerEventAuthority(ctx, sqlcgen.ReadCoreBrokerEventAuthorityParams{ConsumerID: message.ConsumerID, EventID: m.EventID, TenantID: tenant})
	if err == nil {
		if !authority.matches(prior) {
			return result, biz.ErrCoreBrokerAuthority
		}
		if prior.SourceSequence != m.Sequence || prior.Epoch != m.Epoch || !bytes.Equal(prior.RawSha256, sum[:]) {
			return result, biz.ErrCoreProjectionConflict
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, mapPostgresError("read immutable broker event authority", err, nil)
	}
	// The order is authority -> pipeline -> Tenant administration, matching the
	// worker. Bootstrap receipt, projection and authority evidence commit together.
	if b := decoded.Bootstrap; b != nil {
		if err = checkBootstrapCreatingFact(ctx, q, m.Producer, m.Epoch, m.TenantID, m.Sequence); err != nil {
			return result, err
		}
		if b.Validate() != nil || b.EventID != m.EventID || b.Intent.TenantID != m.TenantID || b.Producer != m.Producer || b.SourceSequence != m.Sequence || !b.OccurredAt.Equal(m.OccurredAt) || !bytes.Equal(b.RawPayload, message.Payload) {
			return result, biz.ErrCoreBootstrapInvalid
		}
		if _, err = receiveCoreBootstrap(ctx, q, *b, m.Epoch); err != nil {
			return result, err
		}
	}
	result, err = applyTenantIntegration(ctx, q, r.config.Producer, m)
	if err != nil {
		return result, err
	}
	previous, err := q.ReadCoreBrokerAuthorityReceipt(ctx, sqlcgen.ReadCoreBrokerAuthorityReceiptParams{ConsumerID: message.ConsumerID, BrokerSequence: message.BrokerSequence})
	if err == nil {
		if !authority.matches(previous) || previous.EventID != m.EventID || previous.Epoch != m.Epoch || previous.TenantID != tenant || !bytes.Equal(previous.RawSha256, sum[:]) {
			return result, biz.ErrCoreProjectionConflict
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		p, c, x := authority.producer, authority.receiver, authority.execution
		record := sqlcgen.AppendCoreBrokerAuthorityReceiptParams{ConsumerID: message.ConsumerID, BrokerSequence: message.BrokerSequence, RouteID: route.ID, RouteVersion: p.RouteVersion, Producer: m.Producer, SourceSequence: m.Sequence, Epoch: m.Epoch, EventID: m.EventID, TenantID: tenant, RawSha256: sum[:], ProducerPrincipalID: p.PrincipalID, ProducerBindingID: p.BindingID, ProducerPrincipalVersion: p.PrincipalVersion, ProducerBindingVersion: p.BindingVersion, ProducerGrantID: p.GrantID, ProducerGrantVersion: p.GrantVersion, ExecutorPrincipalID: c.PrincipalID, ExecutorBindingID: c.BindingID, ExecutorPrincipalVersion: c.PrincipalVersion, ExecutorBindingVersion: c.BindingVersion, ExecutorGrantID: c.GrantID, ExecutorGrantVersion: c.GrantVersion}
		if x.GrantID != uuid.Nil {
			record.ExecutionGrantID = requiredPGUUID(x.GrantID)
			record.ExecutionGrantVersion = pgtype.Int8{Int64: x.GrantVersion, Valid: true}
		}
		if err = q.AppendCoreBrokerAuthorityReceipt(ctx, record); err != nil {
			return result, mapPostgresError("append exact broker authority receipt", err, nil)
		}
	} else {
		return result, mapPostgresError("read broker position", err, nil)
	}
	return result, nil
}

func (r *coreBrokerRepository) Quarantine(ctx context.Context, m biz.CoreBrokerMessage, reason string) error {
	if err := r.config.message(m); err != nil {
		return err
	}
	if reason != "invalid_event" && reason != "authority_denied" && reason != "event_conflict" {
		return biz.ErrCoreProjectionInvalid
	}
	headers := m.Headers
	if headers == nil {
		headers = map[string][]string{}
	}
	encoded, err := json.Marshal(headers)
	if err != nil || len(encoded) > 16384 {
		return biz.ErrCoreProjectionInvalid
	}
	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapPostgresError("begin DLQ quarantine", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := sqlcgen.New(tx)
	sum := sha256.Sum256(m.Payload)
	prior, err := q.ReadCoreBrokerDLQPosition(ctx, sqlcgen.ReadCoreBrokerDLQPositionParams{ConsumerID: m.ConsumerID, BrokerSequence: m.BrokerSequence})
	if err == nil {
		if !bytes.Equal(prior.RawSha256, sum[:]) || prior.Subject != m.Subject {
			return biz.ErrCoreProjectionConflict
		}
		return mapPostgresError("commit duplicate quarantine", tx.Commit(ctx), nil)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return mapPostgresError("read DLQ original", err, nil)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return biz.ErrPersistenceUnavailable
	}
	if err = q.AppendCoreBrokerDLQ(ctx, sqlcgen.AppendCoreBrokerDLQParams{ConsumerID: m.ConsumerID, ID: id, BrokerSequence: m.BrokerSequence, BrokerName: m.BrokerName, AccountName: m.Account, StreamName: m.Stream, Subject: m.Subject, RawPayload: m.Payload, RawSha256: sum[:], Headers: encoded, DeliveryCount: int64(m.DeliveryCount), PublishedAt: requiredTimestamptz(m.PublishedAt), LastError: reason}); err != nil {
		return mapPostgresError("retain immutable DLQ original", err, nil)
	}
	// Preserve the trusted receiver configuration with the same durable original.
	// Old DLQ rows lacking this record remain inspectable, never backfilled.
	configuration, err := json.Marshal(r.config)
	if err != nil {
		return biz.ErrCoreBrokerAuthority
	}
	configurationHash := sha256.Sum256(configuration)
	if err = q.AppendCoreDLQContext(ctx, sqlcgen.AppendCoreDLQContextParams{ConsumerID: m.ConsumerID, EntryID: id, Configuration: configuration, ConfigurationSha256: configurationHash[:]}); err != nil {
		return mapPostgresError("retain immutable DLQ receiver attribution", err, nil)
	}
	return mapPostgresError("commit durable quarantine", tx.Commit(ctx), nil)
}

func (r *coreBrokerRepository) bootstrapSourceAuthority(ctx context.Context, q *sqlcgen.Queries, s biz.CoreBootstrapSource) (coreBrokerAuthority, sqlcgen.TenantBrokerAuthorityReceipt, error) {
	if err := q.LockCoreBrokerAuthority(ctx); err != nil {
		return coreBrokerAuthority{}, sqlcgen.TenantBrokerAuthorityReceipt{}, mapPostgresError("lock worker broker authority", err, nil)
	}
	previous, err := q.ReadCoreBootstrapBrokerAuthority(ctx, sqlcgen.ReadCoreBootstrapBrokerAuthorityParams{ConsumerID: r.config.ConsumerID, TenantID: requiredPGUUID(s.TenantID), EventID: s.EventID, OperationID: s.OperationID, PayloadFingerprint: s.Fingerprint, Producer: s.Producer, SourceSequence: s.SourceSequence})
	if errors.Is(err, pgx.ErrNoRows) {
		return coreBrokerAuthority{}, sqlcgen.TenantBrokerAuthorityReceipt{}, biz.ErrCoreBrokerAuthority
	}
	if err != nil {
		return coreBrokerAuthority{}, sqlcgen.TenantBrokerAuthorityReceipt{}, mapPostgresError("read Bootstrap broker authority", err, nil)
	}
	route, err := r.config.route(tenantbootstrap.Subject)
	if err != nil {
		return coreBrokerAuthority{}, sqlcgen.TenantBrokerAuthorityReceipt{}, err
	}
	current, err := r.current(ctx, q, route)
	if err != nil {
		return current, previous, err
	}
	return current, previous, nil
}

func (r *coreBrokerRepository) bootstrapAuthority(ctx context.Context, q *sqlcgen.Queries, s biz.CoreBootstrapSource) (coreBrokerAuthority, error) {
	current, previous, err := r.bootstrapSourceAuthority(ctx, q, s)
	if err != nil {
		return current, err
	}
	if !current.matches(previous) {
		return current, biz.ErrCoreBrokerAuthority
	}
	return current, nil
}

type coreBrokerTransactionKey struct{}
type coreBrokerTransaction struct {
	work     *coreBootstrapWorkTransaction
	data     *Data
	consumer uuid.UUID
	q        *sqlcgen.Queries
}

func (r *coreBrokerRepository) AuthorizeCoreBootstrap(ctx context.Context, s biz.CoreBootstrapSource) (biz.CoreBootstrapExecutionAuthorization, error) {
	if transaction, ok := ctx.Value(coreBrokerTransactionKey{}).(coreBrokerTransaction); ok && transaction.data == r.data && transaction.consumer == r.config.ConsumerID {
		var current coreBrokerAuthority
		var err error
		if transaction.work != nil {
			current, err = transaction.work.currentBrokerAuthority(ctx, s)
		} else {
			current, err = r.bootstrapAuthority(ctx, transaction.q, s)
		}
		if err != nil {
			return biz.CoreBootstrapExecutionAuthorization{}, err
		}
		return brokerExecutionAuthorization(s, current)
	}

	tx, err := r.data.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return biz.CoreBootstrapExecutionAuthorization{}, mapPostgresError("begin worker authorization", err, nil)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	current, err := r.bootstrapAuthority(ctx, sqlcgen.New(tx), s)
	if err != nil {
		return biz.CoreBootstrapExecutionAuthorization{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return biz.CoreBootstrapExecutionAuthorization{}, mapPostgresError("finish worker authorization", err, nil)
	}
	return brokerExecutionAuthorization(s, current)
}
func brokerExecutionAuthorization(s biz.CoreBootstrapSource, current coreBrokerAuthority) (biz.CoreBootstrapExecutionAuthorization, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return biz.CoreBootstrapExecutionAuthorization{}, biz.ErrPersistenceUnavailable
	}
	return biz.CoreBootstrapExecutionAuthorization{Source: s, ProducerPrincipalID: current.producer.PrincipalID, ExecutorPrincipalID: current.receiver.PrincipalID, ProducerVersion: current.producer.PrincipalVersion, ExecutorVersion: current.receiver.PrincipalVersion, DecisionID: id, ValidUntil: time.Now().UTC().Add(5 * time.Second)}, nil
}

// CoreBrokerProvisionManifest is an offline, non-secret configuration input.
// It neither provisions Principals nor carries credential material. Those
// identities must already exist through the reviewed Workload provisioner.
type CoreBrokerProvisionManifest struct {
	Version       int                       `json:"version"`
	ID            uuid.UUID                 `json:"id"`
	Mode          string                    `json:"mode"`
	Reason        string                    `json:"reason"`
	ExpiresAt     time.Time                 `json:"expires_at"`
	Configuration CoreBrokerConfiguration   `json:"configuration"`
	Bindings      []CoreBrokerBindingChange `json:"bindings,omitempty"`
	Grants        []CoreBrokerGrantChange   `json:"grants,omitempty"`
}
type CoreBrokerBindingChange struct {
	ID              uuid.UUID `json:"id"`
	PrincipalID     uuid.UUID `json:"principal_id"`
	NKeyPublic      string    `json:"nkey_public"`
	ExpectedVersion int64     `json:"expected_version"`
	Status          string    `json:"status"`
}
type CoreBrokerGrantChange struct {
	ID              uuid.UUID `json:"id"`
	PrincipalID     uuid.UUID `json:"principal_id"`
	BindingID       uuid.UUID `json:"binding_id"`
	RouteID         uuid.UUID `json:"route_id"`
	Action          string    `json:"action"`
	ExpectedVersion int64     `json:"expected_version"`
	Status          string    `json:"status"`
}
type CoreBrokerProvisionReceipt struct {
	ID          uuid.UUID `json:"id"`
	SHA256      string    `json:"sha256"`
	Mode        string    `json:"mode"`
	CompletedAt time.Time `json:"completed_at"`
}

func (m CoreBrokerProvisionManifest) Validate(now time.Time) error {
	c := m.Configuration
	if m.Version != 1 || m.ID.Version() != 7 || len(m.Reason) < 8 || len(m.Reason) > 512 || !m.ExpiresAt.After(now) || m.ExpiresAt.After(now.Add(24*time.Hour)) || c.Validate() != nil {
		return biz.ErrCoreBrokerAuthority
	}
	for _, ch := range m.Reason {
		if ch < 32 || ch == 127 {
			return biz.ErrCoreBrokerAuthority
		}
	}
	producer := c.Routes[0]
	for _, route := range c.Routes {
		if route.ProducerBindingID != producer.ProducerBindingID || route.ProducerNKey != producer.ProducerNKey {
			return biz.ErrCoreBrokerAuthority
		}
	}
	bindingKeys := map[uuid.UUID]string{producer.ProducerBindingID: producer.ProducerNKey, c.ConsumerBindingID: c.ConsumerNKey}
	seen := map[uuid.UUID]bool{}
	switch m.Mode {
	case "register", "bindings":
		if len(m.Grants) != 0 || len(m.Bindings) == 0 || len(m.Bindings) > 2 || (m.Mode == "register" && len(m.Bindings) != 2) {
			return biz.ErrCoreBrokerAuthority
		}
		principals := map[uuid.UUID]bool{}
		for _, b := range m.Bindings {
			if seen[b.ID] || principals[b.PrincipalID] || b.PrincipalID.Version() != 7 || bindingKeys[b.ID] != b.NKeyPublic || b.NKeyPublic == "" || (b.Status != "active" && b.Status != "revoked") || (m.Mode == "register" && (b.ExpectedVersion != 0 || b.Status != "active")) || (m.Mode == "bindings" && b.ExpectedVersion < 1) {
				return biz.ErrCoreBrokerAuthority
			}
			seen[b.ID] = true
			principals[b.PrincipalID] = true
		}
	case "grants":
		if len(m.Bindings) != 0 || len(m.Grants) == 0 || len(m.Grants) > 7 {
			return biz.ErrCoreBrokerAuthority
		}
		coordinates := map[string]bool{}
		for _, g := range m.Grants {
			key := g.BindingID.String() + g.RouteID.String() + g.Action
			routeFound := false
			for _, r := range c.Routes {
				if r.ID == g.RouteID {
					routeFound = true
					if g.Action == "execute" && r.Subject != tenantbootstrap.Subject {
						return biz.ErrCoreBrokerAuthority
					}
				}
			}
			if !routeFound || g.ID.Version() != 7 || g.PrincipalID.Version() != 7 || seen[g.ID] || coordinates[key] || g.ExpectedVersion < 0 || (g.Status != "active" && g.Status != "revoked") || (g.ExpectedVersion == 0 && g.Status != "active") || (g.Action == "publish" && g.BindingID != producer.ProducerBindingID) || ((g.Action == "receive" || g.Action == "execute") && g.BindingID != c.ConsumerBindingID) || (g.Action != "publish" && g.Action != "receive" && g.Action != "execute") {
				return biz.ErrCoreBrokerAuthority
			}
			seen[g.ID] = true
			coordinates[key] = true
		}
	default:
		return biz.ErrCoreBrokerAuthority
	}
	return nil
}

func ApplyCoreBrokerManifest(ctx context.Context, d *Data, raw []byte, approved, environment, trust string, now time.Time) (CoreBrokerProvisionReceipt, error) {
	result := CoreBrokerProvisionReceipt{}
	digest := sha256.Sum256(raw)
	if len(raw) == 0 || len(raw) > 128<<10 || approved != hex.EncodeToString(digest[:]) || d == nil || d.pool == nil {
		return result, biz.ErrCoreBrokerAuthority
	}
	var m CoreBrokerProvisionManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF || m.Configuration.Environment != environment || m.Configuration.TrustDomain != trust {
		return result, biz.ErrCoreBrokerAuthority
	}
	// Validate the shape on an idempotent retry, but expiration cannot undo an
	// already committed receipt. A new change always checks the real current time.
	if m.Validate(m.ExpiresAt.Add(-time.Second)) != nil {
		return result, biz.ErrCoreBrokerAuthority
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, biz.ErrPersistenceUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = requireRestrictedProvisioner(ctx, tx); err != nil {
		return result, err
	}
	q := sqlcgen.New(tx)
	if err = q.LockCoreBrokerAdministration(ctx); err != nil {
		return result, bootstrapPersistenceError(err)
	}
	previous, err := q.ReadCoreBrokerAdministrationReceipt(ctx, sqlcgen.ReadCoreBrokerAdministrationReceiptParams{ID: m.ID})
	if err == nil {
		if !bytes.Equal(previous.ManifestSha256, digest[:]) {
			return result, biz.ErrWorkloadBootstrapConflict
		}
		return CoreBrokerProvisionReceipt{m.ID, approved, m.Mode, previous.CompletedAt.Time}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, bootstrapPersistenceError(err)
	}
	if m.Validate(now) != nil {
		return result, biz.ErrCoreBrokerAuthority
	}
	c := m.Configuration
	currentPrincipal := func(id uuid.UUID) error {
		_, err := q.ReadCoreBrokerProvisionedPrincipal(ctx, sqlcgen.ReadCoreBrokerProvisionedPrincipalParams{PrincipalID: id, Environment: pgtype.Text{String: environment, Valid: true}, TrustDomain: pgtype.Text{String: trust, Valid: true}})
		if errors.Is(err, pgx.ErrNoRows) {
			return biz.ErrCoreBrokerAuthority
		}
		if err != nil {
			return bootstrapPersistenceError(err)
		}
		return nil
	}
	readBinding := func(id uuid.UUID) (sqlcgen.CoreBrokerBinding, error) {
		b, err := q.ReadCoreBrokerRegisteredBinding(ctx, sqlcgen.ReadCoreBrokerRegisteredBindingParams{ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return b, biz.ErrCoreBrokerAuthority
		}
		if err != nil {
			return b, bootstrapPersistenceError(err)
		}
		expected := c.ConsumerNKey
		if id == c.Routes[0].ProducerBindingID {
			expected = c.Routes[0].ProducerNKey
		} else if id != c.ConsumerBindingID {
			return b, biz.ErrCoreBrokerAuthority
		}
		if b.Environment != environment || b.TrustDomain != trust || b.BrokerName != c.BrokerName || b.AccountName != c.Account || b.NkeyPublic != expected {
			return b, biz.ErrCoreBrokerAuthority
		}
		return b, nil
	}
	if m.Mode == "register" {
		for _, b := range m.Bindings {
			if err = currentPrincipal(b.PrincipalID); err != nil {
				return result, err
			}
			if err = q.RegisterCoreBrokerBinding(ctx, sqlcgen.RegisterCoreBrokerBindingParams{ID: b.ID, PrincipalID: b.PrincipalID, Environment: environment, TrustDomain: trust, BrokerName: c.BrokerName, AccountName: c.Account, NkeyPublic: b.NKeyPublic}); err != nil {
				return result, bootstrapPersistenceError(err)
			}
		}
		for _, r := range c.Routes {
			if err = q.RegisterCoreBrokerRoute(ctx, sqlcgen.RegisterCoreBrokerRouteParams{ID: r.ID, BrokerName: c.BrokerName, AccountName: c.Account, StreamName: c.Stream, Subject: r.Subject, Producer: c.Producer, ProducerBindingID: r.ProducerBindingID, TargetSha256: r.TargetSHA256}); err != nil {
				return result, bootstrapPersistenceError(err)
			}
		}
	} else {
		// Reusing a target name with another registration is never a grant update.
		for _, r := range c.Routes {
			actual, e := q.ReadCoreBrokerRegisteredRoute(ctx, sqlcgen.ReadCoreBrokerRegisteredRouteParams{ID: r.ID})
			if e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return result, biz.ErrCoreBrokerAuthority
				}
				return result, bootstrapPersistenceError(e)
			}
			if actual.BrokerName != c.BrokerName || actual.AccountName != c.Account || actual.StreamName != c.Stream || actual.Subject != r.Subject || actual.SchemaMajor != 1 || actual.Producer != c.Producer || actual.ProducerBindingID != r.ProducerBindingID || actual.TargetSha256 != r.TargetSHA256 {
				return result, biz.ErrCoreBrokerAuthority
			}
		}
		for _, b := range m.Bindings {
			actual, e := readBinding(b.ID)
			if e != nil {
				return result, e
			}
			if actual.PrincipalID != b.PrincipalID {
				return result, biz.ErrCoreBrokerAuthority
			}
			if b.Status == "active" {
				if e = currentPrincipal(b.PrincipalID); e != nil {
					return result, e
				}
			}
			n, e := q.ChangeCoreBrokerBinding(ctx, sqlcgen.ChangeCoreBrokerBindingParams{ID: b.ID, PrincipalID: b.PrincipalID, Status: b.Status, ExpectedVersion: b.ExpectedVersion, Environment: environment, TrustDomain: trust, BrokerName: c.BrokerName, AccountName: c.Account})
			if e != nil {
				return result, bootstrapPersistenceError(e)
			}
			if n != 1 {
				return result, biz.ErrWorkloadBootstrapConflict
			}
		}
		for _, g := range m.Grants {
			b, e := readBinding(g.BindingID)
			if e != nil {
				return result, e
			}
			if b.PrincipalID != g.PrincipalID {
				return result, biz.ErrCoreBrokerAuthority
			}
			if g.Status == "active" {
				if b.Status != "active" {
					return result, biz.ErrCoreBrokerAuthority
				}
				if e = currentPrincipal(g.PrincipalID); e != nil {
					return result, e
				}
			}
			if g.ExpectedVersion == 0 {
				e = q.AuthorizeCoreBrokerGrant(ctx, sqlcgen.AuthorizeCoreBrokerGrantParams{ID: g.ID, PrincipalID: g.PrincipalID, BindingID: g.BindingID, RouteID: g.RouteID, Action: g.Action})
				if e != nil {
					return result, bootstrapPersistenceError(e)
				}
			} else {
				n, e := q.ChangeCoreBrokerGrant(ctx, sqlcgen.ChangeCoreBrokerGrantParams{ID: g.ID, PrincipalID: g.PrincipalID, BindingID: g.BindingID, RouteID: g.RouteID, Action: g.Action, Status: g.Status, ExpectedVersion: g.ExpectedVersion})
				if e != nil {
					return result, bootstrapPersistenceError(e)
				}
				if n != 1 {
					return result, biz.ErrWorkloadBootstrapConflict
				}
			}
		}
	}
	if err = q.AppendCoreBrokerAdministrationReceipt(ctx, sqlcgen.AppendCoreBrokerAdministrationReceiptParams{ID: m.ID, ManifestSha256: digest[:], Manifest: raw, Mode: m.Mode, Reason: m.Reason}); err != nil {
		return result, bootstrapPersistenceError(err)
	}
	receipt, err := q.ReadCoreBrokerAdministrationReceipt(ctx, sqlcgen.ReadCoreBrokerAdministrationReceiptParams{ID: m.ID})
	if err != nil {
		return result, bootstrapPersistenceError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, bootstrapPersistenceError(err)
	}
	return CoreBrokerProvisionReceipt{m.ID, approved, m.Mode, receipt.CompletedAt.Time}, nil
}
