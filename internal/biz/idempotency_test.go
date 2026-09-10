package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mutationTestKey struct {
	Tenant, Actor  uuid.UUID
	Operation, Key string
}
type memoryMutationResults struct {
	rows map[mutationTestKey]StoredMutation
}

func (*memoryMutationResults) LockMutation(context.Context, TenantScope, MutationIdentity) error {
	return nil
}
func (m *memoryMutationResults) FindMutation(_ context.Context, scope TenantScope, identity MutationIdentity) (StoredMutation, bool, error) {
	tenant, err := scope.TenantID()
	if err != nil {
		return StoredMutation{}, false, err
	}
	row, ok := m.rows[mutationTestKey{tenant, identity.ActorID, identity.Operation, identity.Key}]
	return row, ok, nil
}
func (m *memoryMutationResults) SaveMutation(_ context.Context, scope TenantScope, row StoredMutation) error {
	tenant, err := scope.TenantID()
	if err != nil {
		return err
	}
	if m.rows == nil {
		m.rows = make(map[mutationTestKey]StoredMutation)
	}
	m.rows[mutationTestKey{tenant, row.Identity.ActorID, row.Identity.Operation, row.Identity.Key}] = row
	return nil
}

func TestMutationReplayNeverRevealsAPIKeySecretAndRejectsChangedIntent(t *testing.T) {
	scope, _ := NewTenantScope(uuid.MustParse("01993000-0000-7000-8000-000000000011"))
	actor := validTenantAuthorizationActor()
	identity, err := mutationIdentity(actor, "createIAMAPIKey", "same-request", "target-A")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	store := &memoryMutationResults{}
	writes := 0
	var first CreateAPIKeyResult
	err = executeMutation(context.Background(), store, scope, identity, now, &first, func() error {
		writes++
		first = CreateAPIKeyResult{Secret: "once-only-secret", APIKey: APIKey{ID: uuid.MustParse("01993000-0000-7000-8000-000000000012"), Digest: [32]byte{1}}}
		return nil
	})
	if err != nil || first.Secret == "" {
		t.Fatalf("first result: %v", err)
	}
	row, _, _ := store.FindMutation(context.Background(), scope, identity)
	if strings.Contains(string(row.Result), "secret") || strings.Contains(string(row.Result), "Digest") {
		t.Fatal("ledger contains credential material")
	}
	var replay CreateAPIKeyResult
	err = executeMutation(context.Background(), store, scope, identity, now.Add(time.Hour), &replay, func() error { writes++; return nil })
	if err != nil || writes != 1 || replay.Secret != "" || !replay.Replayed || replay.APIKey.ID != first.APIKey.ID {
		t.Fatalf("replay contract failed: %v, writes=%d", err, writes)
	}
	conflict, _ := mutationIdentity(actor, "createIAMAPIKey", "same-request", "target-B")
	err = executeMutation(context.Background(), store, scope, conflict, now, &replay, func() error { writes++; return nil })
	if !errors.Is(err, ErrIdempotencyConflict) || writes != 1 {
		t.Fatalf("changed intent: %v", err)
	}
	err = executeMutation(context.Background(), store, scope, identity, now.Add(24*time.Hour), &replay, func() error { writes++; return nil })
	if !errors.Is(err, ErrIdempotencyExpired) || writes != 1 {
		t.Fatalf("expired replay: %v", err)
	}
}

func TestMutationIntentKeepsCallerIdentityAcrossCertificateRotation(t *testing.T) {
	actor := validTenantAuthorizationActor()
	actor.DirectCaller.Identity.PrincipalID = uuid.MustParse("01993000-0000-7000-8000-000000000021")
	first, _ := mutationIdentity(actor, "bindTenantIAMRole", "key", "role")
	actor.DirectCaller.Identity.BindingID = uuid.MustParse("01993000-0000-7000-8000-000000000022")
	actor.DirectCaller.Identity.BindingVersion = 2
	actor.DirectCaller.GrantVersion = 3
	actor.DecisionID = "new-authentication-decision"
	second, _ := mutationIdentity(actor, "bindTenantIAMRole", "key", "role")
	if first.Intent != second.Intent {
		t.Fatal("rotation changed business intent")
	}
	actor.DirectCaller.Identity.PrincipalID = uuid.MustParse("01993000-0000-7000-8000-000000000023")
	third, _ := mutationIdentity(actor, "bindTenantIAMRole", "key", "role")
	if first.Intent == third.Intent {
		t.Fatal("a different caller reused intent")
	}
}

func TestMutationFailureDoesNotSaveResult(t *testing.T) {
	scope, _ := NewTenantScope(uuid.MustParse("01993000-0000-7000-8000-000000000031"))
	identity, _ := mutationIdentity(validTenantAuthorizationActor(), "bindTenantIAMRole", "key", "role")
	store := &memoryMutationResults{}
	var result TenantMembershipMutationResult
	err := executeMutation(context.Background(), store, scope, identity, time.Now(), &result, func() error { return ErrAuditConflict })
	if !errors.Is(err, ErrAuditConflict) || len(store.rows) != 0 {
		t.Fatalf("failed mutation saved a result: %v", err)
	}
}
