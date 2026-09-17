//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"sync"
	"testing"
	"time"
)

type wr22InvitationClock struct{ now time.Time }

func (c *wr22InvitationClock) Now() time.Time { return c.now }

func TestWR22TenantInvitationTransactions(t *testing.T) {
	environment := newPostgresEnvironment(t)
	ctx := context.Background()
	owner := mustPool(t, environment.migrationDSN(primaryDB))
	defer owner.Close()
	now := time.Now().UTC()
	tenant, other, admin, otherAdmin, member, otherMember, role, otherRole := mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, tenant, role, now, []uuid.UUID{admin}, []uuid.UUID{member})
	seedTenantAuthorizationBoundary(t, ctx, environment.runtimePool, other, otherRole, now, []uuid.UUID{otherAdmin}, []uuid.UUID{otherMember})
	scope, foreignScope := mustTenantScope(t, tenant), mustTenantScope(t, other)
	clock := &wr22InvitationClock{now}
	d := passwordActionTestData(t, environment.runtimePool)
	u := biz.NewTenantInvitationUsecase(data.NewPostgresTenantInvitationUnitOfWork(d), data.NewUUIDv7Generator(), data.NewSecretGenerator(), clock)
	actor := biz.TenantAuthorizationActor{PrincipalID: admin, AuthenticationMethod: biz.AuditAuthenticationMethodPassword, RequestID: "wr22-invitation-transaction", CorrelationID: "wr22-invitation-transaction", DecisionID: "wr22-invitation-transaction"}
	foreignActor := actor
	foreignActor.PrincipalID = otherAdmin
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal("catalog prerequisite")
	}
	roles := biz.NewTenantRoleUsecase(data.NewPostgresTenantRoleUnitOfWork(d), catalog, data.NewUUIDv7Generator(), clock)
	custom, err := roles.Create(ctx, scope, biz.CreateTenantRoleCommand{DisplayName: "Invitation role", Permissions: []biz.Permission{{Scope: biz.PermissionScopeTenant, Resource: "instances", Action: "read"}}, Actor: actor, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal("custom invitation Role prerequisite")
	}
	role = custom.Role.ID
	command := biz.CreateTenantInvitationCommand{Email: " New.Invitee+tag@Example.TEST ", RoleIDs: []uuid.UUID{role}, Actor: actor, IdempotencyKey: uuid.NewString()}
	var original biz.TenantInvitationMutationResult
	t.Run("concurrent_create_has_one_metadata_outbox_Audit_and_no_Membership", func(t *testing.T) {
		type result struct {
			value biz.TenantInvitationMutationResult
			err   error
		}
		out := make(chan result, 4)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); v, e := u.Create(ctx, scope, command); out <- result{v, e} }()
		}
		wg.Wait()
		close(out)
		for r := range out {
			if r.err != nil {
				t.Fatal("concurrent Invitation creation failed")
			}
			if original.Invitation.ID == uuid.Nil {
				original = r.value
			}
			if original.Invitation.ID != r.value.Invitation.ID || original.AuditEventID != r.value.AuditEventID {
				t.Fatal("idempotent Invitation replay changed outcome")
			}
		}
		inv := original.Invitation
		if inv.NormalizedEmail != "new.invitee+tag@example.test" || inv.Version != 1 || inv.DeliveryGeneration != 1 || inv.Locale != "en-US" || !inv.ExpiresAt.Equal(now.Add(7*24*time.Hour)) {
			t.Fatal("normalized invitation metadata differs")
		}
		var invitations, outboxes, audits, members, humans int
		if owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_invitations WHERE tenant_id=$1),(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.invitation.created'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM principals WHERE principal_type='human')`, tenant).Scan(&invitations, &outboxes, &audits, &members, &humans) != nil || invitations != 1 || outboxes != 1 || audits != 1 || members != 1 || humans != 2 {
			t.Fatal("Invitation created identity authority or failed atomic delivery intent")
		}
		var encrypted, receipt []byte
		if owner.QueryRow(ctx, `SELECT payload_ciphertext FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2`, tenant, inv.ID).Scan(&encrypted) != nil || bytes.Contains(encrypted, []byte(inv.NormalizedEmail)) {
			t.Fatal("Invitation outbox is not encrypted")
		}
		if owner.QueryRow(ctx, `SELECT result FROM tenant_mutation_results WHERE tenant_id=$1 AND operation='createTenantIAMInvitation' AND idempotency_key=$2`, tenant, command.IdempotencyKey).Scan(&receipt) != nil || bytes.Contains(receipt, []byte("ani_inv_t.")) || bytes.Contains(receipt, []byte("TokenDigest")) {
			t.Fatal("secret material entered ledger")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("pending_email_role_set_conflict_and_two_tenant_boundary", func(t *testing.T) {
		if _, err := roles.Delete(ctx, scope, biz.DeleteTenantRoleCommand{RoleID: role, ExpectedVersion: 1, Actor: actor, IdempotencyKey: uuid.NewString()}); !errors.Is(err, biz.ErrRoleInUse) {
			t.Fatal("pending Invitation did not protect custom Role")
		}
		c := command
		c.IdempotencyKey = uuid.NewString()
		reused, err := u.Create(ctx, scope, c)
		if err != nil || reused.Invitation.ID != original.Invitation.ID {
			t.Fatal("same role set replaced pending intent")
		}
		c.RoleIDs = []uuid.UUID{otherRole}
		if _, err = u.Create(ctx, scope, c); !errors.Is(err, biz.ErrIdempotencyConflict) {
			t.Fatal("same key changed role intent")
		}
		c.IdempotencyKey = uuid.NewString()
		if _, err = u.Create(ctx, scope, c); !errors.Is(err, biz.ErrInvitationConflict) {
			t.Fatal("different role set overwrote pending intent")
		}
		c.Email = "foreign-role@example.test"
		if _, err = u.Create(ctx, scope, c); !errors.Is(err, biz.ErrRoleNotFound) {
			t.Fatal("foreign Role accepted")
		}
		if _, err = u.Get(ctx, foreignScope, foreignActor, original.Invitation.ID); !errors.Is(err, biz.ErrInvitationNotFound) {
			t.Fatal("cross-Tenant Invitation read succeeded")
		}
		_, err = environment.runtimePool.Exec(ctx, `INSERT INTO tenant_invitation_roles(tenant_id,invitation_id,role_id) VALUES($1,$2,$3)`, tenant, original.Invitation.ID, otherRole)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23503" {
			t.Fatal("same-Tenant role FK did not reject foreign Role")
		}
	})
	t.Run("resend_rotates_digest_cancels_old_ciphertext_and_replays_metadata_only", func(t *testing.T) {
		var before []byte
		if owner.QueryRow(ctx, `SELECT token_digest FROM tenant_invitations WHERE tenant_id=$1 AND id=$2`, tenant, original.Invitation.ID).Scan(&before) != nil {
			t.Fatal("initial digest read")
		}
		c := biz.ChangeTenantInvitationCommand{ID: original.Invitation.ID, ExpectedVersion: 1, Actor: actor, IdempotencyKey: uuid.NewString()}
		sent, err := u.Resend(ctx, scope, c)
		if err != nil || sent.Invitation.Version != 2 || sent.Invitation.DeliveryGeneration != 2 {
			t.Fatal("resend did not advance one generation")
		}
		again, err := u.Resend(ctx, scope, c)
		if err != nil || again.AuditEventID != sent.AuditEventID {
			t.Fatal("resend replay changed delivery")
		}
		var after []byte
		var oldStatus string
		var cleared bool
		if owner.QueryRow(ctx, `SELECT token_digest FROM tenant_invitations WHERE tenant_id=$1 AND id=$2`, tenant, c.ID).Scan(&after) != nil || bytes.Equal(before, after) {
			t.Fatal("resend kept old token digest")
		}
		if owner.QueryRow(ctx, `SELECT status,payload_ciphertext IS NULL AND payload_key_version IS NULL FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2 AND delivery_generation=1`, tenant, c.ID).Scan(&oldStatus, &cleared) != nil || oldStatus != "cancelled" || !cleared {
			t.Fatal("obsolete delivery remains usable")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("Audit_failure_rolls_back_rotation_and_delivery", func(t *testing.T) {
		_, err := owner.Exec(ctx, `CREATE SEQUENCE wr22_invitation_fault_hits; GRANT USAGE,SELECT ON SEQUENCE wr22_invitation_fault_hits TO ani_iam_runtime; CREATE FUNCTION wr22_invitation_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='iam.invitation.resent' THEN PERFORM nextval('wr22_invitation_fault_hits'); RAISE EXCEPTION 'isolated Invitation Audit fault' USING ERRCODE='P0022'; END IF; RETURN NEW; END $$; CREATE TRIGGER wr22_invitation_fault BEFORE INSERT ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION wr22_invitation_fault()`)
		if err != nil {
			t.Fatal("own invitation Audit fault setup")
		}
		defer func() {
			if _, err := owner.Exec(ctx, `DROP TRIGGER wr22_invitation_fault ON iam_audit_events; DROP FUNCTION wr22_invitation_fault(); DROP SEQUENCE wr22_invitation_fault_hits`); err != nil {
				t.Fatal("own invitation Audit fault restoration")
			}
		}()
		_, err = u.Resend(ctx, scope, biz.ChangeTenantInvitationCommand{ID: original.Invitation.ID, ExpectedVersion: 2, Actor: actor, IdempotencyKey: uuid.NewString()})
		if err == nil {
			t.Fatal("Audit failure accepted rotation")
		}
		var hit bool
		var version, deliveries int
		if owner.QueryRow(ctx, `SELECT is_called FROM wr22_invitation_fault_hits`).Scan(&hit) != nil || !hit {
			t.Fatal("Audit fault not reached")
		}
		if owner.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1 AND invitation_id=$2) FROM tenant_invitations WHERE tenant_id=$1 AND id=$2`, tenant, original.Invitation.ID).Scan(&version, &deliveries) != nil || version != 2 || deliveries != 2 {
			t.Fatal("failed Audit left rotation or delivery")
		}
	})
	t.Run("cancel_and_resend_compare_version_with_one_winner", func(t *testing.T) {
		out := make(chan error, 2)
		c := biz.ChangeTenantInvitationCommand{ID: original.Invitation.ID, ExpectedVersion: 2, Actor: actor, IdempotencyKey: uuid.NewString()}
		go func() { _, err := u.Cancel(ctx, scope, c); out <- err }()
		c2 := c
		c2.IdempotencyKey = uuid.NewString()
		go func() { _, err := u.Resend(ctx, scope, c2); out <- err }()
		wins, conflicts := 0, 0
		for i := 0; i < 2; i++ {
			err := <-out
			if err == nil {
				wins++
			} else if errors.Is(err, biz.ErrVersionConflict) {
				conflicts++
			} else {
				t.Fatal("unexpected concurrent transition failure")
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("Invitation transition did not have one winner")
		}
	})
	t.Run("synthetic_seven_day_expiry_releases_live_refs_and_keeps_history", func(t *testing.T) {
		c := command
		c.Email = "expiry@example.test"
		c.IdempotencyKey = uuid.NewString()
		made, err := u.Create(ctx, scope, c)
		if err != nil {
			t.Fatal("expiry intent prerequisite")
		}
		clock.now = now.Add(8 * 24 * time.Hour)
		value, err := u.Get(ctx, scope, actor, made.Invitation.ID)
		if err != nil || value.Status != biz.InvitationExpired || value.Version != 2 || len(value.RoleIDs) != 1 {
			t.Fatal("expiry metadata did not preserve history")
		}
		var refs int
		if owner.QueryRow(ctx, `SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1 AND invitation_id=$2`, tenant, value.ID).Scan(&refs) != nil || refs != 0 {
			t.Fatal("terminal role references retained")
		}
		_, err = environment.runtimePool.Exec(ctx, `UPDATE tenant_invitations SET normalized_email='changed@example.test' WHERE tenant_id=$1 AND id=$2`, tenant, value.ID)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatal("Invitation identity history is mutable")
		}
		if err := u.ExpireForRole(ctx, scope, actor, role); err != nil {
			t.Fatal("expired reference reconciliation failed")
		}
		if _, err := roles.Delete(ctx, scope, biz.DeleteTenantRoleCommand{RoleID: role, ExpectedVersion: 1, Actor: actor, IdempotencyKey: uuid.NewString()}); err != nil {
			t.Fatal("terminal Invitation history permanently blocked Role deletion")
		}
	})
	if !t.Failed() {
		run, _ := isolatedRun(t)
		recordReference(t, run, map[string]any{"stage": "A21-transactions", "scope": "real PG component; public endpoints and Notification not_verified", "concurrency_Audit_ciphertext_boundary": "pass", "expiry": "synthetic eight-day clock"})
	}
}
