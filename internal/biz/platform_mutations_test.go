package biz

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type platformMutationTestTx struct {
	PlatformAdministrationTransaction
	state   PlatformAuthorizationState
	stored  StoredMutation
	lookups int
}

func (t *platformMutationTestTx) LookupAuthority(context.Context, PlatformCapability, string, []string) (PlatformAuthorizationState, error) {
	return t.state, nil
}
func (t *platformMutationTestTx) FindPlatformMutation(context.Context, PlatformCapability, MutationIdentity) (StoredMutation, bool, error) {
	t.lookups++
	return t.stored, true, nil
}

type platformMutationTestUOW struct{ tx *platformMutationTestTx }

func (u platformMutationTestUOW) WithinPlatformAdministration(_ context.Context, _ PlatformCapability, fn func(PlatformAdministrationTransaction) error) error {
	return fn(u.tx)
}

func TestPlatformMutationRechecksAuthorityBeforeExactUnexpiredReceipt(t *testing.T) {
	auth, reader, claims, _ := platformAuthTestSetup(t)
	const op = "createPlatformIAMRole"
	registry := platformAuthTestRegistry{AuthorizationPolicy{OperationID: op, Scope: PermissionScopePlatform, Resource: "iam.platform-roles", Actions: []string{"create"}}}
	cap := PlatformCapability{claims: claims, operation: op, revision: registry.Revision(), reason: "PLATFORM_ADMIN_MUTATION", decisionID: uuid.NewString(), caller: DirectCaller{Target: WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/CreatePlatformRole"}}}
	cap.caller.Identity.PrincipalID = uuid.Must(uuid.NewV7())
	fields := struct{ Name string }{"role"}
	id, err := platformMutationIdentity(cap, op, "receipt", fields)
	if err != nil {
		t.Fatal(err)
	}
	result := PlatformMutationResult{TargetID: uuid.Must(uuid.NewV7()), TargetVersion: 1, AuditEventID: uuid.Must(uuid.NewV7())}
	raw, _ := json.Marshal(result)
	now := auth.clock.Now()
	tx := &platformMutationTestTx{state: reader.state, stored: StoredMutation{Identity: id, Result: raw, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour)}}
	u := NewPlatformAdministrationUsecase(platformMutationTestUOW{tx}, registry, nil, platformTestIDs{}, auth.clock)
	mutation := func(PlatformAdministrationTransaction, time.Time) (PlatformMutationResult, error) {
		panic("receipt replay executed mutation")
	}
	tx.state.PermissionAllowed = false
	if _, err = u.mutate(context.Background(), cap, op, "receipt", fields, mutation); !errors.Is(err, ErrPlatformAdministrationDenied) || tx.lookups != 0 {
		t.Fatal("stale authority read receipt")
	}
	tx.state.PermissionAllowed = true
	if got, err := u.mutate(context.Background(), cap, op, "receipt", fields, mutation); err != nil || got.AuditEventID != result.AuditEventID {
		t.Fatal("exact receipt did not replay")
	}
	tx.stored.ExpiresAt = now
	if _, err = u.mutate(context.Background(), cap, op, "receipt", fields, mutation); !errors.Is(err, ErrIdempotencyExpired) {
		t.Fatal("24h boundary replayed")
	}
	tx.stored.ExpiresAt = now.Add(time.Hour)
	cap.caller.Identity.PrincipalID = uuid.Must(uuid.NewV7())
	if _, err = u.mutate(context.Background(), cap, op, "receipt", fields, mutation); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("other Workload reused receipt")
	}
}
