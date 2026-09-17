//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
	"github.com/zhangzhe-ctrl/ani-iam/internal/data"
	"strings"
	"sync"
	"testing"
	"time"
)

// Credential verification and asynchronous source authority are explicit
// doubles. Platform role/session/Grant checks and all effects use real PG;
// this fixture does not claim a formal OIDC, mTLS or NATS receiver chain.
type wr23ReissueToken struct{ claims biz.AccessTokenClaims }

func (v wr23ReissueToken) Verify(context.Context, string) (biz.AccessTokenClaims, error) {
	return v.claims, nil
}

type wr23DelayedReissueAuthority struct{ inner biz.CoreBootstrapAuthorizer }

func (a wr23DelayedReissueAuthority) AuthorizeCoreBootstrap(ctx context.Context, source biz.CoreBootstrapSource) (biz.CoreBootstrapExecutionAuthorization, error) {
	timer := time.NewTimer(31 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return biz.CoreBootstrapExecutionAuthorization{}, ctx.Err()
	case <-timer.C:
	}
	return a.inner.AuthorizeCoreBootstrap(ctx, source)
}

func TestWR23CoreBootstrapReissue(t *testing.T) {
	e := newPostgresEnvironment(t)
	ctx := context.Background()
	admin := mustPool(t, e.migrationDSN(primaryDB))
	defer admin.Close()
	provisioner := mustPool(t, postgresDSN(provisionerRole, e.provisionerPass, e.host, primaryDB, "wr23-dispatch-provisioner"))
	defer provisioner.Close()
	owner, manifest := newBootstrapManifest(t, strings.Repeat("a", 64))
	for _, method := range []string{"GetTenantBootstrap", "ReissueTenantBootstrapInvitation", "RetryTenantBootstrapJob"} {
		manifest.Workloads[0].Grants = append(manifest.Workloads[0].Grants, biz.BootstrapWorkloadGrant{ID: mustV7(t), Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
	}
	second := manifest.Workloads[0]
	second.PrincipalID = mustV7(t)
	second.BindingID = mustV7(t)
	second.Name = "bootstrap-dispatcher"
	second.DNSIdentity = "bootstrap-dispatcher.wr19.test"
	second.Grants = append([]biz.BootstrapWorkloadGrant(nil), second.Grants...)
	for i := range second.Grants {
		second.Grants[i].ID = mustV7(t)
	}
	manifest.Workloads = append(manifest.Workloads, second)
	if _, err := biz.NewWorkloadBootstrap(data.NewWorkloadBootstrapRepository(data.NewData(provisioner), wr32Registry(t)), data.NewSystemClock(), wr32Registry(t)).Provision(ctx, owner, manifest); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("generate isolated key")
	}
	protector, err := data.NewOutboxProtector("wr23-reissue", map[string][]byte{"wr23-reissue": key})
	if err != nil {
		t.Fatal(err)
	}
	d := data.NewData(e.runtimePool, protector)
	catalog, err := data.NewTargetPermissionCatalog(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		tenant, operation uuid.UUID
		scope             biz.TenantScope
		producer          string
		projection        biz.CoreLifecycleProjectionRepository
		queue             biz.CoreBootstrapQueue
		worker            *biz.CoreBootstrapWorker
		authority         *wr23BootstrapAuthority
	}
	newFixture := func(t *testing.T, producer string, start int64) fixture {
		t.Helper()
		if producer == "" {
			producer = "core.dispatch." + mustV7(t).String()
		}
		f := fixture{tenant: mustV7(t), operation: mustV7(t), producer: producer}
		f.scope = mustTenantScope(t, f.tenant)
		f.projection, err = data.NewCoreLifecycleProjectionRepository(d, producer)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.projection.InitializeShadow(ctx); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: producer, Kind: "lifecycle", SourceSequence: start + 1, OccurredAt: now, RawPayload: []byte(`{"fixture":"dispatcher"}`), LifecycleVersion: 1, Status: "active", Reason: "created", EffectiveAt: now}); err != nil {
			t.Fatal(err)
		}
		intent := biz.CoreBootstrapIntent{TenantID: f.tenant, OperationID: f.operation, NormalizedEmail: "invited@example.test", Locale: "en-US"}
		raw, _ := json.Marshal(map[string]string{"tenant_id": f.tenant.String(), "operation_id": f.operation.String(), "normalized_email": intent.NormalizedEmail, "locale": intent.Locale})
		sum := sha256.Sum256(raw)
		intent.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
		event := mustV7(t)
		if _, err = data.NewCoreBootstrapReceiptRepository(d).Receive(ctx, biz.CoreBootstrapDelivery{Intent: intent, EventID: event, Producer: producer, SourceSequence: start + 2, OccurredAt: now, RawPayload: raw}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: event, TenantID: f.tenant, Producer: producer, Kind: "bootstrap", SourceSequence: start + 2, OccurredAt: now, RawPayload: raw}); err != nil {
			t.Fatal(err)
		}
		f.authority = &wr23BootstrapAuthority{producer: manifest.Workloads[0].PrincipalID, executor: second.PrincipalID, producerVersion: 1, executorVersion: 1}
		uow, _ := data.NewCoreBootstrapWorkUnitOfWork(d, producer)
		f.worker, err = biz.NewCoreBootstrapWorker(uow, f.authority, catalog, data.NewUUIDv7Generator(), data.NewSecretGenerator(), data.NewSystemClock())
		if err != nil {
			t.Fatal(err)
		}
		f.queue, err = data.NewCoreBootstrapQueue(d, producer)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	registry, err := data.NewTargetOperationRegistry(data.TargetPolicyRevision)
	if err != nil {
		t.Fatal(err)
	}
	human, member, role, session, grant := mustV7(t), mustV7(t), mustV7(t), mustV7(t), mustV7(t)
	// This component's only Human graph is explicitly seeded; no login proof.
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO principals(id,principal_type,status,version,created_at,updated_at) VALUES($1,'human','active',1,now(),now())`, []any{human}},
		{`INSERT INTO platform_memberships(id,principal_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,now(),now())`, []any{member, human}},
		{`INSERT INTO platform_roles(id,code,display_name,system_role,system_definition_version,version,created_at,updated_at) VALUES($1,'wr23-reissue','WR23 reissue',false,1,1,now(),now())`, []any{role}},
		{`INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.tenant-bootstrap','read',now()),($1,'platform','iam.tenant-bootstrap','reissue',now()),($1,'platform','iam.tenant-bootstrap','retry',now())`, []any{role}},
		{`INSERT INTO platform_role_bindings(id,membership_id,role_id,version,created_at,updated_at) VALUES($1,$2,$3,1,now(),now())`, []any{mustV7(t), member, role}},
		{`INSERT INTO sessions(id,principal_id,audience,status,device_name,idle_expires_at,absolute_expires_at,version,created_at,updated_at,reauthenticated_at,authn_methods) VALUES($1,$2,'boss','active','component',now()+interval '1 hour',now()+interval '2 hours',1,now(),now(),now(),ARRAY['oidc'])`, []any{session, human}},
		{`INSERT INTO platform_session_grants(id,session_id,principal_id,membership_id,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,'active',1,now(),now())`, []any{grant, session, human, member}},
	}
	for _, s := range seed {
		if _, err = admin.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	claims := biz.AccessTokenClaims{Boundary: biz.AccessBoundaryPlatform, Audience: biz.AudienceBoss, Subject: human, SessionID: session, GrantID: grant, GrantVersion: 1, ExpiresAt: time.Now().Add(time.Hour), AuthnMethods: []biz.AuditAuthenticationMethod{biz.AuditAuthenticationMethodOIDC}}
	auth, err := biz.NewPlatformAuthorizationUsecase(registry, wr23ReissueToken{claims}, data.NewPlatformAuthorizationReader(d), data.NewUUIDv7Generator(), data.NewSystemClock())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := biz.NewWorkloadAuthentication(data.NewWorkloadIdentityReader(d)).Authenticate(ctx, biz.VerifiedWorkloadPeer{Environment: owner.Environment, TrustDomain: owner.TrustDomain, IdentityKind: "x509_dns", IdentityValue: manifest.Workloads[0].DNSIdentity})
	if err != nil {
		t.Fatal(err)
	}
	capability := func(t *testing.T, method, operation string, id uuid.UUID) biz.PlatformCapability {
		t.Helper()
		caller, err := biz.NewWorkloadAuthorization(data.NewWorkloadGrantReader(d, wr32Registry(t)), wr32Registry(t)).Authorize(ctx, identity, biz.WorkloadTarget{Audience: "ani-iam", Operation: "/iam.v1.IAMAdminService/" + method})
		if err != nil {
			t.Fatal(err)
		}
		reason := "INVITATION_REISSUE"
		if operation == "retryTenantIAMBootstrapJob" {
			reason = "BOOTSTRAP_JOB_RETRY"
		}
		cap, err := auth.AuthorizeAdministration(biz.WithDirectCaller(ctx, caller), biz.CheckPermissionCommand{RawCredential: "component-verified-token", OperationID: operation, PolicyRevision: registry.Revision(), TargetResourceID: id.String()}, method, reason)
		if err != nil {
			t.Fatal(err)
		}
		return cap
	}
	newAdmin := func(f fixture, store *data.Data) *biz.PlatformAdministrationUsecase {
		return biz.NewPlatformAdministrationUsecase(data.NewPlatformAdministrationUnitOfWork(store), registry, biz.NewPermissionCatalogReader(catalog, registry.Revision()), data.NewUUIDv7Generator(), data.NewSystemClock()).WithInvitationSecrets(data.NewSecretGenerator()).WithCoreBootstrapAdministration(f.producer, f.authority)
	}
	start := func(t *testing.T) fixture {
		t.Helper()
		f := newFixture(t, "", 0)
		if _, err := f.worker.Reconcile(ctx, f.scope, f.operation); err != nil {
			t.Fatal(err)
		}
		return f
	}
	command := func(f fixture, version int64) biz.ReissueTenantBootstrapCommand {
		return biz.ReissueTenantBootstrapCommand{OperationID: f.operation, ExpectedVersion: version, ReasonCode: "INVITATION_REISSUE", IdempotencyKey: uuid.NewString()}
	}
	snapshot := func(t *testing.T, f fixture) string {
		t.Helper()
		var s string
		err := e.runtimePool.QueryRow(ctx, `SELECT json_build_object('operation',(SELECT row_to_json(o) FROM tenant_bootstrap_operations o WHERE o.tenant_id=$1 AND o.id=$2),'invitation',(SELECT row_to_json(i) FROM tenant_invitations i WHERE i.tenant_id=$1),'deliveries',(SELECT count(*) FROM tenant_invitation_outbox WHERE tenant_id=$1),'audits',(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1))::text`, f.tenant, f.operation).Scan(&s)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	expire := func(t *testing.T, f fixture) {
		t.Helper()
		if _, err := admin.Exec(ctx, `UPDATE tenant_invitations SET expires_at=created_at+interval '1 microsecond',version=version+1 WHERE tenant_id=$1`, f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	getVersion := func(t *testing.T, f fixture) int64 {
		t.Helper()
		var v int64
		if err := e.runtimePool.QueryRow(ctx, `SELECT version FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2`, f.tenant, f.operation).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	newCap := func(t *testing.T, f fixture) biz.PlatformCapability {
		return capability(t, "ReissueTenantBootstrapInvitation", "reissueTenantIAMBootstrapInvitation", f.operation)
	}
	t.Run("rotate_same_invitation_token_and_atomic_role_delivery_audits", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		cap := newCap(t, f)
		var original string
		if err := e.runtimePool.QueryRow(ctx, `SELECT payload_fingerprint FROM tenant_bootstrap_operations WHERE tenant_id=$1 AND id=$2`, f.tenant, f.operation).Scan(&original); err != nil {
			t.Fatal(err)
		}
		outbox, err := data.NewIdentityNotificationOutbox(d, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		old, ok, err := outbox.ClaimIdentityNotification(ctx, biz.IdentityNotificationTenantInvitation, time.Now().UTC(), time.Minute)
		if err != nil || !ok || old.TenantID != f.tenant {
			t.Fatal("original encrypted delivery", err)
		}
		if err = outbox.FinishIdentityNotification(ctx, old, biz.IdentityNotificationCancelled, "", time.Now().UTC(), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		c := command(f, 2)
		v, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c)
		if err != nil {
			t.Fatal(err)
		}
		inv := v.Bootstrap.Invitation
		if v.Bootstrap.ID != f.operation || v.Bootstrap.TenantID != f.tenant || v.Bootstrap.Version != 3 || inv.ID != old.SourceID || inv.DeliveryGeneration != 2 || inv.NormalizedEmail != "invited@example.test" {
			t.Fatal("reissue changed identity")
		}
		current, ok, err := outbox.ClaimIdentityNotification(ctx, biz.IdentityNotificationTenantInvitation, time.Now().UTC(), time.Minute)
		if err != nil || !ok || current.TenantID != f.tenant || current.SourceID != old.SourceID || current.Secret == old.Secret {
			t.Fatal("new generation missing independent secret", err)
		}
		var digest []byte
		var fp string
		var memberships, refs, audits int
		if err = e.runtimePool.QueryRow(ctx, `SELECT i.token_digest,o.payload_fingerprint,(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM tenant_invitation_roles WHERE tenant_id=$1),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.bootstrap.invitation.reissued' AND actor_id=$3 AND caller_principal_id=$4) FROM tenant_invitations i JOIN tenant_bootstrap_operations o ON o.tenant_id=i.tenant_id AND o.id=i.bootstrap_operation_id WHERE i.tenant_id=$1 AND o.id=$2`, f.tenant, f.operation, human, manifest.Workloads[0].PrincipalID).Scan(&digest, &fp, &memberships, &refs, &audits); err != nil {
			t.Fatal(err)
		}
		oldHash, newHash := sha256.Sum256([]byte(old.Secret)), sha256.Sum256([]byte(current.Secret))
		if string(digest) == string(oldHash[:]) || string(digest) != string(newHash[:]) || fp != original || memberships != 0 || refs != 1 || audits != 1 {
			t.Fatal("reissue credential, authority, payload or audit mismatch")
		}
		encoded, _ := json.Marshal(v)
		if strings.Contains(string(encoded), current.Secret) || strings.Contains(string(encoded), old.Secret) {
			t.Fatal("mutation receipt leaked a token")
		}
		if err = outbox.FinishIdentityNotification(ctx, current, biz.IdentityNotificationCancelled, "", time.Now().UTC(), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		getCap := capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", f.operation)
		view, err := u.GetTenantBootstrap(ctx, getCap, f.operation)
		if err != nil || view.Invitation.DeliveryGeneration != 2 || view.Version != 3 {
			t.Fatal("current operation metadata", err)
		}
		before := snapshot(t, f)
		again, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c)
		if err != nil || again.AuditEventID != v.AuditEventID || snapshot(t, f) != before {
			t.Fatal("retry did not preserve receipt", err)
		}
	})
	t.Run("concurrent_same_intent_one_effect_and_conflicting_intent", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		cap := newCap(t, f)
		c := command(f, 2)
		var wg sync.WaitGroup
		results := make([]biz.PlatformMutationResult, 5)
		for i := range results {
			wg.Go(func() {
				var err error
				results[i], err = u.ReissueTenantBootstrapInvitation(ctx, cap, c)
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		for _, v := range results {
			if v.AuditEventID != results[0].AuditEventID || v.Bootstrap.Invitation.DeliveryGeneration != 2 {
				t.Fatal("duplicate reissue")
			}
		}
		c.ExpectedVersion = 3
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c); !errors.Is(err, biz.ErrIdempotencyConflict) {
			t.Fatal("changed intent reused key", err)
		}
		if getVersion(t, f) != 3 {
			t.Fatal("operation advanced twice")
		}
	})
	t.Run("current_platform_and_source_authority_precede_receipt", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		cap := newCap(t, f)
		c := command(f, 2)
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, f)
		f.authority.deny = true
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
			t.Fatal("source revocation bypassed by receipt", err)
		}
		f.authority.deny = false
		if _, err := admin.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND action='reissue'`, role); err != nil {
			t.Fatal(err)
		}
		_, denied := u.ReissueTenantBootstrapInvitation(ctx, cap, c)
		if _, err := admin.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.tenant-bootstrap','reissue',now())`, role); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(denied, biz.ErrPlatformAdministrationDenied) || snapshot(t, f) != before {
			t.Fatal("Platform revocation bypass or effects", denied)
		}
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c); err != nil {
			t.Fatal("restored current authority cannot read same receipt", err)
		}
	})
	t.Run("old_expiry_lease_cannot_expire_reissued_generation", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		cap := newCap(t, f)
		expire(t, f)
		old, ok, err := f.queue.Claim(ctx)
		if err != nil || !ok || old.Generation != 1 {
			t.Fatal("old expiry lease", err)
		}
		if _, err = u.ReissueTenantBootstrapInvitation(ctx, cap, command(f, 2)); err != nil {
			t.Fatal(err)
		}
		if _, err = f.worker.ExpireInvitation(ctx, f.scope, f.operation, old.Generation); err != nil {
			t.Fatal(err)
		}
		if _, err = f.queue.Finish(ctx, old, "", false); err != nil {
			t.Fatal(err)
		}
		if getVersion(t, f) != 3 {
			t.Fatal("old lease expired new invitation")
		}
		expire(t, f)
		next, ok, err := f.queue.Claim(ctx)
		if err != nil || !ok || next.Generation != 2 || next.Attempt != 1 {
			t.Fatal("new generation lacks its own schedule", err)
		}
		if _, err = f.worker.ExpireInvitation(ctx, f.scope, f.operation, next.Generation); err != nil {
			t.Fatal(err)
		}
		if _, err = f.queue.Finish(ctx, next, "", false); err != nil {
			t.Fatal(err)
		}
		v, err := u.ReissueTenantBootstrapInvitation(ctx, cap, command(f, 4))
		if err != nil || v.Bootstrap.Invitation.DeliveryGeneration != 3 {
			t.Fatal("attention reissue failed", err)
		}
		var jobs, attempts int
		if err = e.runtimePool.QueryRow(ctx, `SELECT (SELECT count(*) FROM core_bootstrap_jobs WHERE tenant_id=$1 AND state='done'),(SELECT count(*) FROM core_bootstrap_attempts WHERE tenant_id=$1)`, f.tenant).Scan(&jobs, &attempts); err != nil || jobs != 2 || attempts != 2 {
			t.Fatal("history rewritten during reissue", err)
		}
		old.Generation = 3
		if _, err = f.queue.Finish(ctx, old, "", false); !errors.Is(err, biz.ErrCoreBootstrapLease) {
			t.Fatal("old lease crossed generation", err)
		}
	})
	t.Run("version_and_lifecycle_reject_without_side_effects", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		cap := newCap(t, f)
		before := snapshot(t, f)
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, command(f, 1)); !errors.Is(err, biz.ErrVersionConflict) {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if _, err := f.projection.Apply(ctx, biz.CoreProjectionMessage{EventID: mustV7(t), TenantID: f.tenant, Producer: f.producer, Kind: "lifecycle", SourceSequence: 3, OccurredAt: now, RawPayload: []byte(`{"fixture":"freeze"}`), LifecycleVersion: 2, Status: "frozen", Reason: "administrative_freeze", EffectiveAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, command(f, 2)); !errors.Is(err, biz.ErrTenantLifecycleBlocked) {
			t.Fatal("frozen Tenant reissued", err)
		}
		if snapshot(t, f) != before {
			t.Fatal("failed reissue changed state")
		}
	})
	t.Run("real_authority_wait_cannot_reuse_pre_wait_lifecycle_freshness", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d).WithCoreBootstrapAdministration(f.producer, wr23DelayedReissueAuthority{f.authority})
		cap := newCap(t, f)
		before := snapshot(t, f)
		began := time.Now()
		_, err := u.ReissueTenantBootstrapInvitation(ctx, cap, command(f, 2))
		if time.Since(began) < 31*time.Second || !errors.Is(err, biz.ErrTenantLifecycleStale) || snapshot(t, f) != before {
			t.Fatal("stale pre-wait lifecycle authorized reissue", err)
		}
	})
	t.Run("outbox_audit_and_receipt_failures_roll_back", func(t *testing.T) {
		for _, table := range []string{"tenant_invitation_outbox", "iam_audit_events", "platform_mutation_results"} {
			t.Run(table, func(t *testing.T) {
				f := start(t)
				u := newAdmin(f, d)
				cap := newCap(t, f)
				c := command(f, 2)
				before := snapshot(t, f)
				if _, err := admin.Exec(ctx, "REVOKE INSERT ON "+table+" FROM ani_iam_runtime"); err != nil {
					t.Fatal(err)
				}
				_, failure := u.ReissueTenantBootstrapInvitation(ctx, cap, c)
				if _, err := admin.Exec(ctx, "GRANT INSERT ON "+table+" TO ani_iam_runtime"); err != nil {
					t.Fatal(err)
				}
				if failure == nil || snapshot(t, f) != before {
					t.Fatal("failure left partial reissue", failure)
				}
				if _, err := u.ReissueTenantBootstrapInvitation(ctx, cap, c); err != nil {
					t.Fatal("retry after restore", err)
				}
			})
		}
	})
	t.Run("zero_capability_and_other_operation_cannot_cross_scope", func(t *testing.T) {
		f := start(t)
		u := newAdmin(f, d)
		before := snapshot(t, f)
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, biz.PlatformCapability{}, command(f, 2)); err == nil {
			t.Fatal("zero capability")
		}
		getCap := capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", f.operation)
		if _, err := u.ReissueTenantBootstrapInvitation(ctx, getCap, command(f, 2)); !errors.Is(err, biz.ErrPlatformAdministrationDenied) {
			t.Fatal("read capability mutated", err)
		}
		if snapshot(t, f) != before {
			t.Fatal("untrusted capability changed Tenant")
		}
		missing := mustV7(t)
		if _, err := u.GetTenantBootstrap(ctx, capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", missing), missing); !errors.Is(err, biz.ErrCoreBootstrapNotFound) {
			t.Fatal("missing Bootstrap operation", err)
		}
		other := start(t)
		otherU := newAdmin(other, d)
		view, err := otherU.GetTenantBootstrap(ctx, capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", other.operation), other.operation)
		if err != nil || view.TenantID != other.tenant || view.ID != other.operation || view.TenantID == f.tenant {
			t.Fatal("stored operation scope mismatch", err)
		}
	})

	// These cases share only the declared component setup above. Source and
	// token evidence remain explicit doubles; all effects and permissions use PG.
	retryCap := func(t *testing.T, f fixture) biz.PlatformCapability {
		return capability(t, "RetryTenantBootstrapJob", "retryTenantIAMBootstrapJob", f.operation)
	}
	retryCommand := func(t *testing.T, f fixture, kind biz.CoreBootstrapJobKind, generation int64, attempt int32) biz.RetryTenantBootstrapJobCommand {
		return biz.RetryTenantBootstrapJobCommand{OperationID: f.operation, Kind: kind, Generation: generation, ExpectedVersion: getVersion(t, f), ExpectedAttempt: attempt, ReasonCode: "BOOTSTRAP_JOB_RETRY", IdempotencyKey: uuid.NewString()}
	}
	exhaust := func(t *testing.T, f fixture, first, last int32) biz.CoreBootstrapJobClaim {
		t.Helper()
		var current biz.CoreBootstrapJobClaim
		for n := first; n <= last; n++ {
			value, ok, err := f.queue.Claim(ctx)
			if err != nil || !ok || value.Attempt != n {
				t.Fatal("claim isolated failure cycle", err)
			}
			current = value
			attention, err := f.queue.Finish(ctx, value, "dependency_unavailable", false)
			if err != nil || attention != (n == last) {
				t.Fatal("finish bounded failure cycle", err)
			}
			if n < last {
				if _, err := admin.Exec(ctx, `UPDATE core_bootstrap_jobs SET available_at=clock_timestamp() WHERE tenant_id=$1 AND operation_id=$2 AND kind=$3 AND generation=$4`, f.tenant, f.operation, string(value.Kind), value.Generation); err != nil {
					t.Fatal("advance only own component retry metadata", err)
				}
			}
		}
		return current
	}
	jobView := func(t *testing.T, f fixture) biz.CoreBootstrapJob {
		t.Helper()
		v, err := newAdmin(f, d).GetTenantBootstrap(ctx, capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", f.operation), f.operation)
		if err != nil || len(v.Jobs) != 1 {
			t.Fatal("bounded original job metadata", err)
		}
		return v.Jobs[0]
	}
	t.Run("job_recovery_concurrency_cycles_and_original_worker", func(t *testing.T) {
		f := newFixture(t, "", 0)
		old := exhaust(t, f, 1, 20)
		u := newAdmin(f, d)
		cap := retryCap(t, f)
		c := retryCommand(t, f, biz.CoreBootstrapInitialize, 1, 20)
		before := jobView(t, f)
		if before.State != "attention_required" || before.AttemptCount != 20 || before.CycleStartAttempt != 0 || before.LastError != "dependency_unavailable" {
			t.Fatal("attention metadata")
		}
		if _, err := admin.Exec(ctx, `UPDATE core_bootstrap_jobs SET state='pending',cycle_start_attempt=20 WHERE tenant_id=$1 AND operation_id=$2`, f.tenant, f.operation); err == nil {
			t.Fatal("manual reset bypassed recovery")
		}
		type answer struct {
			v   biz.PlatformMutationResult
			err error
		}
		ch := make(chan answer, 4)
		var wg sync.WaitGroup
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() { defer wg.Done(); v, err := u.RetryTenantBootstrapJob(ctx, cap, c); ch <- answer{v, err} }()
		}
		wg.Wait()
		close(ch)
		var receipt biz.PlatformMutationResult
		for result := range ch {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if receipt.Bootstrap == nil {
				receipt = result.v
			} else if receipt.AuditEventID != result.v.AuditEventID || receipt.Bootstrap.Jobs[0].RecoveryID != result.v.Bootstrap.Jobs[0].RecoveryID {
				t.Fatal("concurrent recovery produced multiple effects")
			}
		}
		recovery := receipt.Bootstrap.Jobs[0].RecoveryID
		if recovery.Version() != 7 || receipt.Bootstrap.Jobs[0].CycleStartAttempt != 20 || receipt.Bootstrap.Version != 1 || receipt.Bootstrap.Status != "pending" || receipt.Bootstrap.Invitation != nil {
			t.Fatal("technical recovery changed business state")
		}
		var attempts, recoveries, tenantAudits, platformAudits, members, access int
		var recordedActor uuid.UUID
		var reason string
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM core_bootstrap_attempts WHERE tenant_id=$1),(SELECT count(*) FROM core_bootstrap_job_recoveries WHERE tenant_id=$1),(SELECT count(*) FROM iam_audit_events WHERE tenant_id=$1 AND action='iam.bootstrap.job.retried'),(SELECT count(*) FROM iam_audit_events WHERE boundary='platform' AND target_id=$2 AND action='iam.platform.retryTenantIAMBootstrapJob'),(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),(SELECT count(*) FROM tenant_access WHERE tenant_id=$1)`, f.tenant, f.operation).Scan(&attempts, &recoveries, &tenantAudits, &platformAudits, &members, &access); err != nil || attempts != 20 || recoveries != 1 || tenantAudits != 0 || platformAudits != 1 || members != 0 || access != 0 {
			t.Fatal("recovery effects/audits not atomic", err)
		}
		if err := admin.QueryRow(ctx, `SELECT actor_id,reason_code FROM core_bootstrap_job_recoveries WHERE tenant_id=$1 AND id=$2`, f.tenant, recovery).Scan(&recordedActor, &reason); err != nil || recordedActor != human || reason != "BOOTSTRAP_JOB_RETRY" {
			t.Fatal("recovery actor or reason", err)
		}
		changed := c
		changed.ExpectedAttempt++
		if _, err := u.RetryTenantBootstrapJob(ctx, cap, changed); !errors.Is(err, biz.ErrIdempotencyConflict) {
			t.Fatal("changed retry intent reached cached receipt", err)
		}
		if _, err := f.queue.Finish(ctx, old, "dependency_unavailable", false); !errors.Is(err, biz.ErrCoreBootstrapLease) {
			t.Fatal("old lease settled recovered cycle", err)
		}
		for _, table := range []string{"core_bootstrap_attempts", "core_bootstrap_job_recoveries"} {
			if _, err := admin.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", f.tenant); err == nil {
				t.Fatal("immutable recovery history erased", table)
			}
		}
		exhaust(t, f, 21, 40)
		next := retryCommand(t, f, biz.CoreBootstrapInitialize, 1, 40)
		v, err := u.RetryTenantBootstrapJob(ctx, cap, next)
		if err != nil {
			t.Fatal("second controlled cycle", err)
		}
		var parent uuid.UUID
		var previous int32
		if err := admin.QueryRow(ctx, `SELECT previous_recovery_id,previous_attempt FROM core_bootstrap_job_recoveries WHERE tenant_id=$1 AND id=$2`, f.tenant, v.Bootstrap.Jobs[0].RecoveryID).Scan(&parent, &previous); err != nil || parent != recovery || previous != 40 {
			t.Fatal("recovery chain was reset", err)
		}
		claimed, ok, err := f.queue.Claim(ctx)
		if err != nil || !ok || claimed.Attempt != 41 || claimed.CycleStartAttempt != 40 || claimed.RecoveryID != v.Bootstrap.Jobs[0].RecoveryID {
			t.Fatal("next attempt lost recovery link", err)
		}
		if _, err = f.worker.Reconcile(ctx, f.scope, f.operation); err != nil {
			t.Fatal("original worker did not resume", err)
		}
		if _, err = f.queue.Finish(ctx, claimed, "", false); err != nil {
			t.Fatal(err)
		}
		if _, err = u.RetryTenantBootstrapJob(ctx, cap, retryCommand(t, f, biz.CoreBootstrapInitialize, 1, 41)); !errors.Is(err, biz.ErrCoreBootstrapConflict) {
			t.Fatal("completed job retried", err)
		}
		var immutable bool
		if err := admin.QueryRow(ctx, `SELECT bool_and(r.payload_fingerprint=o.payload_fingerprint AND r.source_event_id=j.source_event_id) FROM core_bootstrap_job_recoveries r JOIN tenant_bootstrap_operations o ON o.tenant_id=r.tenant_id AND o.id=r.operation_id JOIN core_bootstrap_jobs j ON j.tenant_id=r.tenant_id AND j.operation_id=r.operation_id AND j.kind=r.kind AND j.generation=r.generation WHERE r.tenant_id=$1`, f.tenant).Scan(&immutable); err != nil || !immutable {
			t.Fatal("source payload changed", err)
		}
	})
	t.Run("job_retry_authority_precedes_cached_receipt", func(t *testing.T) {
		f := newFixture(t, "", 0)
		exhaust(t, f, 1, 20)
		u := newAdmin(f, d)
		cap := retryCap(t, f)
		c := retryCommand(t, f, biz.CoreBootstrapInitialize, 1, 20)
		receipt, err := u.RetryTenantBootstrapJob(ctx, cap, c)
		if err != nil {
			t.Fatal(err)
		}
		f.authority.deny = true
		if _, err = u.RetryTenantBootstrapJob(ctx, cap, c); !errors.Is(err, biz.ErrCoreBootstrapAuthority) {
			t.Fatal("source revocation bypassed by receipt", err)
		}
		f.authority.deny = false
		if _, err = admin.Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id=$1 AND scope='platform' AND resource='iam.tenant-bootstrap' AND action='retry'`, role); err != nil {
			t.Fatal(err)
		}
		_, denied := u.RetryTenantBootstrapJob(ctx, cap, c)
		if _, err = admin.Exec(ctx, `INSERT INTO platform_role_permissions(role_id,scope,resource,action,created_at) VALUES($1,'platform','iam.tenant-bootstrap','retry',now())`, role); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(denied, biz.ErrPlatformAdministrationDenied) {
			t.Fatal("revoked retry privilege reached receipt", denied)
		}
		again, err := u.RetryTenantBootstrapJob(ctx, cap, c)
		if err != nil || again.AuditEventID != receipt.AuditEventID {
			t.Fatal("restored permission changed receipt", err)
		}
		reader := capability(t, "GetTenantBootstrap", "getTenantIAMBootstrap", f.operation)
		bad := c
		bad.ReasonCode = "INVITATION_REISSUE"
		if _, err = u.RetryTenantBootstrapJob(ctx, reader, bad); !errors.Is(err, biz.ErrPlatformAdministrationDenied) {
			t.Fatal("read authority became retry authority", err)
		}
	})
	t.Run("job_retry_failures_roll_back_recovery_audit_and_receipt", func(t *testing.T) {
		for _, table := range []string{"core_bootstrap_job_recoveries", "iam_audit_events", "platform_mutation_results"} {
			t.Run(table, func(t *testing.T) {
				f := newFixture(t, "", 0)
				exhaust(t, f, 1, 20)
				u := newAdmin(f, d)
				cap := retryCap(t, f)
				c := retryCommand(t, f, biz.CoreBootstrapInitialize, 1, 20)
				if _, err := admin.Exec(ctx, "REVOKE INSERT ON "+table+" FROM ani_iam_runtime"); err != nil {
					t.Fatal(err)
				}
				_, failed := u.RetryTenantBootstrapJob(ctx, cap, c)
				if _, err := admin.Exec(ctx, "GRANT INSERT ON "+table+" TO ani_iam_runtime"); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM core_bootstrap_job_recoveries WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil || count != 0 || failed == nil {
					t.Fatal("partial recovery after write failure", failed, err)
				}
				if v := jobView(t, f); v.State != "attention_required" || v.AttemptCount != 20 || v.RecoveryID != uuid.Nil {
					t.Fatal("failed recovery changed schedule")
				}
				if _, err := u.RetryTenantBootstrapJob(ctx, cap, c); err != nil {
					t.Fatal("restored dependency did not recover", err)
				}
			})
		}
	})
	t.Run("job_recovery_keeps_identity_and_old_expiry_generation_inert", func(t *testing.T) {
		f := start(t)
		expire(t, f)
		claimed, ok, err := f.queue.Claim(ctx)
		if err != nil || !ok || claimed.Kind != biz.CoreBootstrapExpire {
			t.Fatal(err)
		}
		if attention, err := f.queue.Finish(ctx, claimed, "intent_conflict", true); err != nil || !attention {
			t.Fatal("permanent task attention", err)
		}
		c := retryCommand(t, f, biz.CoreBootstrapExpire, 1, 1)
		u := newAdmin(f, d)
		if _, err = u.RetryTenantBootstrapJob(ctx, retryCap(t, f), c); err != nil {
			t.Fatal("exact expiry technical retry", err)
		}
		if _, err = u.ReissueTenantBootstrapInvitation(ctx, newCap(t, f), command(f, 2)); err != nil {
			t.Fatal(err)
		}
		c.ExpectedVersion = 3
		c.IdempotencyKey = uuid.NewString()
		if _, err = u.RetryTenantBootstrapJob(ctx, retryCap(t, f), c); !errors.Is(err, biz.ErrCoreBootstrapNotFound) {
			t.Fatal("old expiry generation revived", err)
		}
		observed, err := f.queue.Observe(ctx)
		if err != nil || observed.JobAttention != 0 || observed.InvitationAttention != 0 {
			t.Fatal("old generation remained actionable", err)
		}
	})
	t.Run("job_attention_is_observed_after_dispatcher_restart", func(t *testing.T) {
		f := newFixture(t, "", 0)
		exhaust(t, f, 1, 20)
		q, err := data.NewCoreBootstrapQueue(d, f.producer)
		if err != nil {
			t.Fatal(err)
		}
		dispatcher, err := biz.NewCoreBootstrapDispatcher(q, f.worker)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := q.Observe(ctx)
		if err != nil || observation.JobAttention != 1 || observation.InvitationAttention != 0 {
			t.Fatal("persisted attention not visible", err)
		}
		observed := false
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		dispatcher.RunObserved(call, func(err error) { t.Errorf("unexpected dispatcher error: %v", err) }, func(v biz.CoreBootstrapQueueObservation, err error) {
			if err != nil || v.JobAttention != 1 {
				t.Error("restart observation", err)
			}
			observed = true
			cancel()
		})
		if !observed {
			t.Fatal("dormant attention was not observed")
		}
	})
}
