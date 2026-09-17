package biz

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestCoreBootstrapAdministrationRechecksAfterAuthorityDependency(t *testing.T) {
	for _, name := range []string{"token_expired", "session_expired", "permission_revoked", "receipt_expired", "unchanged"} {
		t.Run(name, func(t *testing.T) {
			auth, reader, claims, _ := platformAuthTestSetup(t)
			const op = "reissueTenantIAMBootstrapInvitation"
			registry := platformAuthTestRegistry{AuthorizationPolicy{OperationID: op, Scope: PermissionScopePlatform, Resource: "iam.tenant-bootstrap", Actions: []string{"reissue"}}}
			cap := PlatformCapability{claims: claims, operation: op, revision: registry.Revision(), reason: "INVITATION_REISSUE", decisionID: uuid.NewString(), caller: DirectCaller{Target: WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/ReissueTenantBootstrapInvitation"}}}
			cap.caller.Identity.PrincipalID = uuid.Must(uuid.NewV7())
			fields := ReissueTenantBootstrapCommand{OperationID: uuid.Must(uuid.NewV7()), ExpectedVersion: 2, ReasonCode: cap.reason, IdempotencyKey: "receipt"}
			identity, err := platformMutationIdentity(cap, op, "receipt", fields)
			if err != nil {
				t.Fatal(err)
			}
			result := PlatformMutationResult{TargetID: fields.OperationID, TargetVersion: 3, AuditEventID: uuid.Must(uuid.NewV7())}
			raw, _ := json.Marshal(result)
			clock := &mutableNotificationClock{now: auth.clock.Now()}
			tx := &platformMutationTestTx{state: reader.state, stored: StoredMutation{Identity: identity, Result: raw, CreatedAt: clock.now.Add(-time.Hour), ExpiresAt: clock.now.Add(time.Hour)}}
			u := NewPlatformAdministrationUsecase(platformMutationTestUOW{tx}, registry, nil, platformTestIDs{}, clock)
			before := func(PlatformAdministrationTransaction, time.Time) error {
				switch name {
				case "token_expired":
					clock.now = claims.ExpiresAt
				case "session_expired":
					tx.state.IdleExpiresAt = clock.now
				case "permission_revoked":
					tx.state.PermissionAllowed = false
				case "receipt_expired":
					tx.stored.ExpiresAt = clock.now.Add(time.Second)
					clock.now = clock.now.Add(time.Second)
				}
				return nil
			}
			got, err := u.mutateWithPrecondition(context.Background(), cap, op, "receipt", fields, before, func(PlatformAdministrationTransaction, time.Time) (PlatformMutationResult, error) {
				panic("receipt replay executed mutation")
			})
			if name == "unchanged" {
				if err != nil || got.AuditEventID != result.AuditEventID {
					t.Fatal("current receipt", err)
				}
				return
			}
			want := ErrPlatformAdministrationDenied
			if name == "token_expired" {
				want = ErrInvalidCredential
			}
			if name == "receipt_expired" {
				want = ErrIdempotencyExpired
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			if name != "receipt_expired" && tx.lookups != 0 {
				t.Fatal("stale authority reached receipt")
			}
		})
	}
}
