package biz

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

type dlqTestTx struct {
	PlatformAdministrationTransaction
	state                                             PlatformAuthorizationState
	entry                                             CoreDLQEntry
	receipt                                           StoredMutation
	found                                             bool
	received, lookups, audits, attempts, sourceChecks int
	sourceError, receiveError, auditError             error
	onSource                                          func()
}

func (t *dlqTestTx) LookupAuthority(context.Context, PlatformCapability, string, []string) (PlatformAuthorizationState, error) {
	return t.state, nil
}
func (t *dlqTestTx) LoadPlatformCoreDLQ(context.Context, PlatformCapability, uuid.UUID, uuid.UUID) (CoreDLQEntry, error) {
	return t.entry, nil
}
func (t *dlqTestTx) CheckPlatformCoreDLQSource(context.Context, PlatformCapability, CoreDLQEntry) (string, error) {
	t.sourceChecks++
	if t.onSource != nil {
		t.onSource()
	}
	return strings.Repeat("a", 64), t.sourceError
}
func (t *dlqTestTx) FindPlatformMutation(context.Context, PlatformCapability, MutationIdentity) (StoredMutation, bool, error) {
	t.lookups++
	return t.receipt, t.found, nil
}
func (t *dlqTestTx) ReceivePlatformCoreDLQ(_ context.Context, _ PlatformCapability, c ReplayCoreDLQCommand, _ CoreBrokerDecoded) (CoreDLQReplayResult, error) {
	t.received++
	return CoreDLQReplayResult{ConsumerID: c.ConsumerID, EntryID: c.EntryID, RawSHA256: c.ExpectedRawSHA256, Outcome: "received", AuthoritySHA256: strings.Repeat("a", 64)}, t.receiveError
}
func (t *dlqTestTx) AppendAudit(context.Context, PlatformCapability, SecurityAuditEvent) error {
	t.audits++
	return t.auditError
}
func (t *dlqTestTx) AppendPlatformCoreDLQAttempt(context.Context, PlatformCapability, ReplayCoreDLQCommand, MutationIdentity, CoreDLQReplayResult, string) error {
	t.attempts++
	return nil
}
func (t *dlqTestTx) SavePlatformMutation(_ context.Context, _ PlatformCapability, r StoredMutation) error {
	t.receipt = r
	t.found = true
	return nil
}

type dlqTestUOW struct {
	tx        *dlqTestTx
	completed bool
}

func (u *dlqTestUOW) WithinPlatformAdministration(_ context.Context, _ PlatformCapability, fn func(PlatformAdministrationTransaction) error) error {
	err := fn(u.tx)
	u.completed = true
	return err
}

type dlqTestDecoder struct {
	calls   int
	payload []byte
}

func (d *dlqTestDecoder) Decode(m CoreBrokerMessage) (CoreBrokerDecoded, error) {
	d.calls++
	d.payload = append([]byte(nil), m.Payload...)
	return CoreBrokerDecoded{}, nil
}

type dlqTestFailures struct {
	uow   *dlqTestUOW
	calls int
	code  string
	err   error
}

func (f *dlqTestFailures) RecordCoreDLQFailure(_ context.Context, _ PlatformCapability, _ ReplayCoreDLQCommand, _ MutationIdentity, _ SecurityAuditEvent, _ uuid.UUID, code string) error {
	if !f.uow.completed {
		panic("failure audit preceded business rollback")
	}
	f.calls++
	f.code = code
	return f.err
}
func dlqTestSetup(t *testing.T) (*PlatformAdministrationUsecase, PlatformCapability, ReplayCoreDLQCommand, *dlqTestTx, *dlqTestDecoder, *dlqTestFailures) {
	t.Helper()
	auth, reader, claims, _ := platformAuthTestSetup(t)
	const op = "replayCoreIAMDLQEntry"
	registry := platformAuthTestRegistry{AuthorizationPolicy{OperationID: op, Scope: PermissionScopePlatform, Resource: "iam.dlq", Actions: []string{"replay"}}}
	cap := PlatformCapability{claims: claims, operation: op, revision: registry.Revision(), reason: "WR23_RECOVERY", decisionID: uuid.NewString(), caller: DirectCaller{Target: WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/ReplayCoreDLQEntry"}}}
	cap.caller.Identity.PrincipalID = uuid.Must(uuid.NewV7())
	c := ReplayCoreDLQCommand{ConsumerID: uuid.Must(uuid.NewV7()), EntryID: uuid.Must(uuid.NewV7()), ExpectedRawSHA256: strings.Repeat("b", 64), ReasonCode: cap.reason, IdempotencyKey: "dlq-one"}
	tx := &dlqTestTx{state: reader.state, entry: CoreDLQEntry{ID: c.EntryID, ConsumerID: c.ConsumerID, RawSHA256: c.ExpectedRawSHA256, Message: CoreBrokerMessage{Payload: []byte("immutable-original")}}}
	uow := &dlqTestUOW{tx: tx}
	decoder := &dlqTestDecoder{}
	failures := &dlqTestFailures{uow: uow}
	u := NewPlatformAdministrationUsecase(uow, registry, nil, platformTestIDs{}, auth.clock).WithCoreDLQAdministration(decoder, failures)
	return u, cap, c, tx, decoder, failures
}
func TestCoreDLQReplayReceiptRechecksCurrentAuthority(t *testing.T) {
	for _, kind := range []string{"current", "broker_revoked", "human_revoked_after_broker", "expired", "changed_intent"} {
		t.Run(kind, func(t *testing.T) {
			u, cap, c, tx, decoder, failure := dlqTestSetup(t)
			identity, err := platformMutationIdentity(cap, cap.operation, c.IdempotencyKey, c)
			if err != nil {
				t.Fatal(err)
			}
			previous := CoreDLQReplayResult{ConsumerID: c.ConsumerID, EntryID: c.EntryID, AttemptID: uuid.Must(uuid.NewV7()), AuditID: uuid.Must(uuid.NewV7()), AttemptNumber: 1, RawSHA256: c.ExpectedRawSHA256, Outcome: "received"}
			raw, _ := json.Marshal(previous)
			tx.receipt = StoredMutation{Identity: identity, Result: raw, CreatedAt: u.clock.Now(), ExpiresAt: u.clock.Now().Add(time.Hour)}
			tx.found = true
			tx.entry.LastAttempt = 9
			var want error
			switch kind {
			case "broker_revoked":
				tx.sourceError = ErrCoreBrokerAuthority
				want = ErrCoreBrokerAuthority
			case "human_revoked_after_broker":
				tx.onSource = func() { tx.state.PermissionAllowed = false }
				want = ErrPlatformAdministrationDenied
			case "expired":
				tx.receipt.ExpiresAt = u.clock.Now()
				want = ErrIdempotencyExpired
			case "changed_intent":
				tx.receipt.Identity.Intent[0] ^= 1
				want = ErrIdempotencyConflict
			}
			got, err := u.ReplayCoreDLQEntry(context.Background(), cap, c)
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			if tx.received != 0 || decoder.calls != 0 || tx.attempts != 0 {
				t.Fatal("cached result executed another receive")
			}
			if want == nil {
				if !got.Replayed || got.AttemptID != previous.AttemptID || failure.calls != 0 {
					t.Fatal("committed receipt was not reused")
				}
			} else if failure.calls != 1 {
				t.Fatal("failure evidence missing")
			}
			if (kind == "broker_revoked" || kind == "human_revoked_after_broker") && tx.lookups != 0 {
				t.Fatal("stale authority reached cached result")
			}
		})
	}
}
func TestCoreDLQReplayFailureEvidenceAndOriginal(t *testing.T) {
	for _, kind := range []string{"success", "source_conflict", "attempt_conflict", "receive_failure", "audit_failure", "failure_audit_unavailable"} {
		t.Run(kind, func(t *testing.T) {
			u, cap, c, tx, decoder, failure := dlqTestSetup(t)
			var want error
			fault := errors.New("storage fault")
			switch kind {
			case "source_conflict":
				c.ExpectedRawSHA256 = strings.Repeat("c", 64)
				want = ErrCoreDLQConflict
			case "attempt_conflict":
				c.ExpectedAttempt = 1
				want = ErrCoreDLQConflict
			case "receive_failure":
				tx.receiveError = fault
				want = fault
			case "audit_failure":
				tx.auditError = fault
				want = fault
			case "failure_audit_unavailable":
				tx.receiveError = fault
				failure.err = fault
				want = ErrCoreDLQAuditUnavailable
			}
			got, err := u.ReplayCoreDLQEntry(context.Background(), cap, c)
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			if want == nil {
				if got.AttemptNumber != 1 || tx.attempts != 1 || tx.audits != 1 || !tx.found || failure.calls != 0 || string(decoder.payload) != "immutable-original" {
					t.Fatal("missing linked committed result")
				}
			} else {
				if got.AttemptID != uuid.Nil || failure.calls != 1 || tx.found {
					t.Fatal("failed request reported success or lost failure evidence")
				}
			}
			if (kind == "source_conflict" || kind == "attempt_conflict") && decoder.calls != 0 {
				t.Fatal("precondition failure decoded or received event")
			}
		})
	}
}
func TestCoreDLQCursorBindsActorConsumerAndOperation(t *testing.T) {
	_, cap, c, _, _, _ := dlqTestSetup(t)
	page, _, err := coreDLQPage(cap, "listCoreIAMDLQEntries", c.ConsumerID, uuid.Nil, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	raw := coreDLQNext(page)
	if _, _, err = coreDLQPage(cap, "listCoreIAMDLQEntries", c.ConsumerID, uuid.Nil, raw, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err = coreDLQPage(cap, "getCoreIAMDLQEntry", c.ConsumerID, c.EntryID, raw, 1); err == nil {
		t.Fatal("cursor changed operation")
	}
	if _, _, err = coreDLQPage(cap, "listCoreIAMDLQEntries", uuid.Must(uuid.NewV7()), uuid.Nil, raw, 1); err == nil {
		t.Fatal("cursor changed consumer")
	}
	cap.claims.Subject = uuid.Must(uuid.NewV7())
	if _, _, err = coreDLQPage(cap, "listCoreIAMDLQEntries", c.ConsumerID, uuid.Nil, raw, 1); err == nil {
		t.Fatal("cursor changed actor")
	}
}
